# FreshPlate 食材库存模块技术方案

状态：待实现设计。编写日期：2026-10-19。仅定义个人库存，尚未实现扣减记餐。

## 1. 目标、依赖与边界

允许登录用户分批记录现有食材、数量、单位和到期日，查看临期、过期和用完的库存。依赖[食材基础数据模块](../food/FOOD_MODULE_TECHNICAL_DESIGN.md) v3；共用其认证、JSON、事务、时间、响应和分页约定。

同一种食材不同购买批次、保质期或单位分别保存，不自动合并。管理员只能维护自己的库存，没有查询他人库存的入口。

首版不做扫码、图片、购物清单、仓位、采购价格、库存流水、批量导入、邮件/系统推送、自动报废、自动扣减和记餐。临期提醒仅为页面计算与标记，不增加后台任务。

## 2. 数据模型

新增 `inventory_items`：

| 字段 | 类型 / 约束 | 含义 |
| --- | --- | --- |
| `id` | INTEGER PRIMARY KEY | 库存批次 ID |
| `user_id` | INTEGER NOT NULL，users 外键 RESTRICT | 所有者，取认证上下文 |
| `food_id` | INTEGER NOT NULL，foods 外键 RESTRICT | 标准食材，一旦创建不可修改 |
| `quantity_milli` | INTEGER NOT NULL，CHECK 0～1000000000 | 当前数量 ×1000 |
| `unit` | TEXT NOT NULL，CHECK IN ('g','ml','piece') | 原始单位，创建后不可修改 |
| `expires_on` | TEXT NULL | 严格 YYYY-MM-DD 到期日，无日期为未知 |
| `note` | TEXT NOT NULL DEFAULT '' | 备注，最多 500 码点 |
| `version` | INTEGER NOT NULL DEFAULT 1，CHECK >0 | 乐观版本 |
| `created_at/updated_at` | INTEGER NOT NULL | UTC Unix 秒 |
| `deleted_at` | INTEGER NULL | 逻辑删除；不参与日常查询或后续扣减 |

建立 `(user_id,id)` 和 `(user_id,expires_on,id)` 索引。所有资源查询和修改同时限定 user_id、id、deleted_at IS NULL。逻辑删除保留批次 ID，便于后续记餐关联；本期不提供恢复或回收站入口，也不把保留行当作库存流水。

不从请求接收 user_id、deleted_at、时间或 version 的新值。新增 version 由服务端设 1；编辑和删除的 version 为期望旧值。

## 3. 数量、单位及日期规则

### 3.1 数量契约

API `quantity` 使用 JSON 字符串，如 `"250"`、`"250.125"`，避免库存反复编辑引入浮点余量。只接受 ASCII 十进制，形式为 `0` 或非零整数开头，可带 1～3 位小数；拒绝正负号、指数、空白、前导零、数值类型和 null。

用 Go 标准库按整数部分及补齐的小数部分解析成 quantity_milli，在相乘前检查范围；不经过 float64。最大数量为 1000000 原单位。输出规范为无多余前导零、去掉小数末尾零的字符串，例如 1250 → `"1.25"`。同一解析/格式化 helper 供库存和食谱用量复用。

创建数量必须 >0；编辑允许 0 表示用完。允许 `"0.5"` 个，不强制个为整数。负库存永远不允许。UI 使用字符串输入和 inputmode=decimal，前端不得先用 Number 转换再提交。

### 3.2 单位与停用食材

库存的原单位与 quantity_milli 是事实数据，g/ml/piece 都可录入；换算未知不阻止录入。质量及营养估算读取 food 的显式换算信息，不写回库存数量。原单位不可更改，避免把“500毫升”误保存成“500克”；需要纠错时删除旧批次再创建。

新建时食材必须 active，在写事务内再次检查。已有食材停用后，仍显示库存名称和停用标记，允许减量、设零、修改备注/到期日或删除，不允许增加数量。停用不会解除忌口，也不会删除库存。

### 3.3 到期与临期

- `expires_on` 必须为真实公历日期，范围 1970-01-01～9999-12-31；Go `time.Parse("2006-01-02", ...)` 后严格比对格式。创建和编辑可记录已过期批次，界面明确提示。
- 统一按中国标准时间（UTC+8）日历日判断，避免服务器时区改变造成规则漂移；页面明确写“按中国标准时间日期判断”，不将 YYYY-MM-DD 通过浏览器时区转换。到期日当天仍标记“今日到期”，次日起为过期。
- `today` 由服务端注入时钟取得，每次请求只取一次：date < today 为 expired；today ≤ date ≤ today+3 天为 expiring；date > today+3 天为 fresh；null 为 unknown。
- 数量为 0 优先显示 depleted，不再显示临期警告；没有到期日不等于不会过期。逻辑删除记录不返回。
- `expiry_state` 为响应派生字段，取 fresh/expiring/expired/unknown/depleted，不持久化、不用定时任务更新。自然日期变化在刷新页面后生效。

SQL 筛选和列表派生共用同一个 today。所有分支都限定 `user_id=? AND deleted_at IS NULL`，再按以下条件筛选，避免用完批次同时进入日期状态：

| expiry_state | 追加条件 |
| --- | --- |
| depleted | `quantity_milli = 0` |
| expired | `quantity_milli > 0 AND expires_on < :today` |
| expiring | `quantity_milli > 0 AND expires_on BETWEEN :today AND :today_plus_3` |
| fresh | `quantity_milli > 0 AND expires_on > :today_plus_3` |
| unknown | `quantity_milli > 0 AND expires_on IS NULL` |

日期边界作为参数绑定；本表仅说明条件，不要求更改项目的 SQL 占位符风格。

## 4. API

所有路径相对 `/api`，均要求登录及完成强制改密。

| 方法与路径 | 行为 |
| --- | --- |
| `GET /inventory-items?q=&food_id=&expiry_state=&page=1&page_size=20` | 本人分页列表，id DESC；默认全部未删除批次，包含数量为零项 |
| `GET /inventory-items/:id` | 本人单批次详情 |
| `POST /inventory-items` | 新建批次，201 返回详情 |
| `PUT /inventory-items/:id` | 全量替换可变字段，200 返回详情 |
| `DELETE /inventory-items/:id` | 校验 version 后逻辑删除，204 |

q 为食材名称字面子串，最多 100 码点；food_id 为正整数；expiry_state 只接受上述五值，筛选条件与列表派生规则一致。不额外提供总质量/总数量，因为不同单位不能直接相加。列表 items 和 total 在同一读事务内取得，服务端日期贯穿该次筛选和响应。

POST 完整请求：

```json
{"food_id":1,"quantity":"6","unit":"piece","expires_on":"2026-10-25","note":"第一批"}
```

PUT 必须提交全部三个可变字段（quantity、expires_on、note）和当前 version，不接受 food_id/unit：

```json
{"quantity":"4.5","expires_on":"2026-10-25","note":"已用掉一部分","version":1}
```

DELETE 提交 JSON `{"version":2}`；仍适用 Content-Type、64 KiB 和身份断言规则。首次成功返回 204；已删除或不存在返回 404，不承诺在旧 version 下返回成功。

首版接受 DELETE body 的兼容性限制，适用当前本地同源 fetch 调用；后续引入代理、网关或其他客户端时须验证 body 可透传。通过既有认证、身份和媒体类型检查后，先解析请求并验证 version 为必填正整数，再查询资源是否存在；缺失、null、非整数或非正 version 返回 `400 INVALID_ARGUMENT`。代理丢弃 body 也只能导致拒绝，永不退化为无版本删除。不增加查询参数或 If-Match 的替代通道。

详情和列表项统一返回 id、food（id/name/status）、quantity、unit、expires_on、note、version、expiry_state、created_at、updated_at，不返回 user_id、内部千分单位或 deleted_at。日期 null 显式返回，列表无结果返回 items=[]。

错误：非法输入 400 INVALID_ARGUMENT；不存在、已删除、他人资源统一 404 NOT_FOUND；新建引用不存在或 disabled 食材为 400 FOOD_UNAVAILABLE；停用后尝试增量为 409 FOOD_DISABLED；版本冲突为 409 VERSION_CONFLICT；身份/账号及 DATABASE_BUSY 使用既有错误。不得通过错误区分他人库存是否存在。

## 5. 写入、一致性与后续扣减

写操作的 store 事务顺序：

1. `WithImmediate` 内复用现有会话检查，确认当前身份和非强制改密状态。只读和写入均以认证用户 ID 为边界。
2. 创建时校验 food active；编辑/删除按 `id/user_id/deleted_at` 读记录，再核对 version。
3. 编辑时读取关联食材最新状态，执行“停用只能减量”规则；校验日期和数量。
4. 条件更新限定 `id/user_id/version/deleted_at IS NULL`，数量、updated_at、version 同时改变，影响行数必须为 1。删除设 deleted_at 并 version+1。
5. 相同内容的 PUT 返回原详情，不加版本；业务失败全部回滚。创建和修改成功后返回事务中的最终记录。

两个标签页从 version=1 编辑时，只允许一次成功，另一次 409，防止把旧数量覆盖到新数量。删除也必须校验版本，避免删掉已经被其他页面修改的批次。任何库存修改（包括未来烹饪扣减）都必须增加同一 version。

将来烹饪确认应按批次 ID、user_id、版本/剩余量检查，在同一事务内完成扣减和记餐；本模块不提供“先扣库存再写记餐”的独立扣减接口。跨单位分配、临期优先选批次与记餐幂等由后续方案决定。

POST 不因相同内容自动合并：两批相同食材可能是真实库存。请求发出后网络失败时保存状态可能不明，前端提示先刷新核对，不能盲目重试。首版不增加创建幂等键；因此不能承诺网络异常后人工重提绝无重复批次。

本人库存不写入管理员资源审计，不泄露私人备注和库存内容。服务日志不记录完整请求体。

## 6. 前端与代码落点

- 新建 `InventoryView.vue`（`/inventory`），加入 `App.vue` 导航；列表显示食材、批次 ID、数量/单位、到期日、状态及编辑/删除入口。
- 页面提供名称、单食材和状态筛选，翻页与刷新；首版不另外开发 dashboard。临期/过期标签使用文字加颜色，不能只靠颜色表达。
- 新建表单使用分页 `GET /food-catalog` 检索 active 食材；显示名称及明确单位，库存页不自动推断个重。编辑锁定食材和单位，停用时显示“只允许减少数量”。
- 删除前显示食材和剩余数量并确认；409 保留草稿供核对、要求重新加载当前记录，不自动覆盖。账号变化则丢弃旧账号草稿，沿用现有身份处理。
- 完整处理 loading、空数据、失败、提交中、503 和网络状态不明；保存按钮禁用防连点，成功后刷新当前列表，删除当前页最后项时回退到有效页。
- 新增 `api/inventory.go`、`logic/inventory.go`、`store/inventory.go`；数量解析放 logic 的小 helper，供食谱直接复用，不新建抽象框架。DTO 与 store 响应类型分开，拒绝越权字段。
- 更新 `router.js`、Swagger 及接口文档；沿用现有 CSS 与 api/auth helper，不加依赖。

## 7. 迁移与交付顺序

食材模块 v3 → 新增 `004_inventory.sql` → v4。建立表、CHECK、外键及索引，同时在迁移器登记校验和、支持版本和结构检查。空库、v2/v3 升级路径都覆盖；既有用户无需补库存数据，空列表为正常状态。

不更改 foods、user_preferences 或认证表，不将 12 种食材自动变成任何人的库存。管理员 CLI 同样验证可打开 v4。停服、完整一致性备份、启动前迁移与回退要求沿用食材方案；不提供破坏性降级脚本。

实施顺序：数量/日期校验及测试 → 表和归属受限 store → API → 页面 → 隔离端到端及文档更新。

## 8. 验收与测试

| 层次 | 必须覆盖 |
| --- | --- |
| 数量 | 0、新建拒绝零、0.001、最大值、超限、指数、负号、空白、前导零、数字 JSON、多于三位小数；解析格式化往返不丢精度 |
| 日期 | 闰日/非法日期、中国标准时间跨日、今日、3天与4天边界、未知、数量零优先级；数量零即使有到期日也只匹配 depleted、已删除项不匹配任何状态；使用可注入时钟 |
| 数据库 | 多批次不合并、负数 CHECK、外键、逻辑删除不返回、版本竞争、错误回滚；用户外键不能删除 |
| 安全 | A 无法读取/修改/删除 B 的批次；管理员也不能；伪造 user_id/food_id/unit 修改失败；事务内撤销身份有效；DELETE 丢 body 或缺失/null/非法 version 在资源存在性检查前拒绝，不执行删除 |
| 食材关联 | 并发停用阻止新增；既有停用库存可减不可增；停用项读取保留；未知营养/换算不阻止原单位保存 |
| 前端 | 字符串数量不被浮点转换；冲突不覆盖；网络失败不自动新建；错误、空状态和键盘操作 |
| E2E | 用户 A 新增两批同食材→筛选临期→编辑→用完→删除；用户 B 看不到；管理员停用后 A 按规则处理 |
| 迁移回归 | 空库、v2/v3 升级、重复启动、数据保留、CLI；原用户与忌口测试仍通过 |

实施完成运行 Go test/vet/race、前端单测/构建、Playwright 和 CLI 验证，并记录实际结果。验收不包含烹饪扣减、饮食记录或营养计算；保质期只是用户录入日期的提醒，不判断食品是否仍可食用。
