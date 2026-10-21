# food / inventory / recipe 三模块实施总结

日期：2026-10-19。本文记录三模块（食材基础数据 v3、食材库存 v4、食谱管理 v5）从设计到验证的完整交付结果。所有数据为实际执行结果，非计划值。

## 1. 交付范围

| 模块 | 迁移 | 后端 | 前端 |
| --- | --- | --- | --- |
| 食材基础数据 | `003_foods.sql`（foods 重建增列 + `resource_audit_logs`） | `store/foods.go`、`store/resourceaudit.go`、`logic/foods.go`、`api/foods.go` | `FoodCatalogView.vue`、`FoodsView.vue`、`ResourceAuditView.vue` |
| 食材库存 | `004_inventory.sql` | `store/inventory.go`、`logic/inventory.go`、`api/inventory.go` | `InventoryView.vue` |
| 食谱管理 | `005_recipes.sql`（recipes / recipe_ingredients / recipe_allergens） | `store/recipes.go`、`logic/recipes.go`、`api/recipes.go` | `RecipesView.vue`、`RecipeDetailView.vue`、`RecipeEditView.vue`（admin 模式复用） |
| 公共 | `store/db.go` 迁移器重写为声明式顺序执行器（001→005）；`logic/quantity.go` 数量/日期共享 helper；`api.go`/`security.go` 请求体上限路由化（食谱 4 条写路由 256 KiB，其余 64 KiB，单一限制来源） | — | `router.js`/`App.vue` 接线；Swagger 标题改「FreshPlate API」并再生成（33 个操作） |

架构遵循 API → logic → store 分层；写路径统一 `WithImmediate` + `checkSession` + 版本条件更新（影响行数=1）+ 审计同事务；越权统一 404、角色不足 403；`GET /api/foods` 原契约逐字节保留。

2026-10-20 后续存储层调整：业务 CRUD 已迁移至 GORM，保留 modernc SQLite 连接、原 SQL 迁移与立即写事务；数据库版本仍为 v5，接口契约不变。单独验证结果见 [GORM 替换验证记录](GORM_MIGRATION_VERIFICATION.md)。

## 2. 测试用例与实施方案

- 用例基线：`docs/test/FOOD_TEST_CASES.md`（89 条）、`INVENTORY_TEST_CASES.md`、`RECIPE_TEST_CASES.md`，共约 270 条，均经覆盖度审查与修补（对照方案验收表）。
- 实施方案：`docs/test/TEST_EXECUTION_PLAN.md`——36 行「用例 ID 区段 → 实际测试文件 → 运行命令」映射（基于对测试代码用例 ID 标注的实测）。

## 3. 验证结果（2026-10-19 实测；2026-10-20 Review 修复后全量复测）

| 验证项 | 命令 | 结果 |
| --- | --- | --- |
| 后端构建/静态检查 | `go vet ./... && go build ./...`（server/ 下） | 通过 |
| 后端全量测试 | `go test -count=1 ./...` | 4 包 ok（api 70.5s / logic 2.2s / runtime 2.3s / store 7.6s），0 失败 |
| 竞态检测 | `go test -race -count=1 ./internal/...` | 4 包 ok，0 失败 |
| 前端单元测试 | `npm test`（web/ 下） | 59/59 通过（含数量换算共享模块 quantity.test.js 28 断言） |
| 前端构建 | `npm run build` | 成功（JS 196.63 kB / gzip 67.01 kB） |
| 三模块 E2E | `npm run test:e2e`（food/inventory/recipe spec） | 23/23 通过（FOOD-E2E 7 + INV-E2E 10 + REC-E2E 6；006/007/010 为三轮 Review 修复回归用例） |
| 全量 E2E 回归 | 同上（含 user/swagger spec） | 33/33（2026-10-20 第三轮修复后复测；既有 ID-001 为时序敏感用例，单独复跑亦可通过，见 §6） |
| CLI 验证 | `python3 scripts/verify_cli.py --go <go>` | 6 项断言全部通过（进程锁、权限 0600、会话恢复、密钥缺失拒绝启动等） |
| admin CLI 打开 v5 | 临时库迁移后运行 `admin init` | 通过：`schema_migrations` max=5，三模块表齐全，迁移幂等 |
| 冒烟 | 临时服务 127.0.0.1:18099 | 登录→建含营养食材（201）→food-catalog 分页（13 条，三页 5/5/3）全通过 |

环境注意：本机 shell 的 `GOROOT` 环境变量指向旧版 go 1.25.3，所有 Go 命令必须用 `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go`；macOS 12 无法安装 Playwright 捆绑 chromium-1248，E2E 需 `FRESHPLATE_E2E_CHROMIUM='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'` 与 `FRESHPLATE_E2E_GO=<goenv go>`；`verify_cli.py` 需系统 python3（≥3.7），aioAirflow5050 venv 的 3.6.10 不支持 `capture_output`。

## 4. 实施中发现并修复的缺陷

1. **`FoodsView.vue` 数字输入校验崩溃**（E2E 发现的真实缺陷）：`v-model` 对 `type="number"` 输入自动做 `looseToNumber` 转换，`validate()` 对 number 调 `.trim()` 抛 TypeError 被 Vue 静默吞掉，导致含营养信息的食材永远无法提交（无请求、无报错）。修复：校验前 `String(v ?? '').trim()` 归一。
2. **E2E 种子缺 `user_preferences` 行**：直接 SQL INSERT 用户绕过注册路径，`GET /api/me/preferences` 返回 500。修复：`seed-admin.py` 与 `store.CreateUser` 一致补插偏好行。

## 4bis. Review 修复记录（2026-10-20）

外部 Review 提出 5 个问题，经逐条对照代码全部确认属实并已修复，修复后全量复测通过：

| 级别 | 问题 | 修复 | 回归测试 |
| --- | --- | --- | --- |
| P1 | 保存菜谱返回旧数据（`UpdateRecipe` 复用更新前读出的旧 `recipeRow` 构造响应，仅补 version/status） | 更新成功后在同一事务内重新 `SELECT` 父行再构造详情（对齐 CreateRecipe 路径） | REC-REG-002 |
| P1 | 仅修改步骤时 PUT 不落库（`recipeWriteEqual` 漏比 steps） | 加入与审计比较一致的 `recipeStepsEqual(stepsJSONOf(old), in.Steps)` 比较 | REC-REG-001（无变化语义由既有 REC-REG-003=TestRecipeSTORE014 保持） |
| P2 | 菜谱步骤/原料删除按钮不可用（`removeAt` 对已解包数组访问 `.value`） | 改为直接 `list.splice(index, 1)` | REC-E2E-006（含 pageerror 断言） |
| P2 | 食物编辑请求竞态（`openEdit` 晚到响应覆盖当前表单） | 新增 `editSequence` 请求序号防护；openCreate/closeForm 亦作废在途请求 | 代码评审级修复（竞态难做确定性 E2E） |
| P2 | 小数数量转千分单位未去小数点（`"250.5"→"250.50"`、`"999999.999"` 被前端误拒） | 抽出共享模块 `web/src/quantity.js`（`m[2].slice(1)` 去点后补零），两视图统一引用；`compareQuantity` 一并收编 | INV-WEB-018 / REC-WEB-011（quantity.test.js，28 断言） |
| P1（第二轮 Review） | 食材详情加载失败后残留上一食材数据与版本，保存会以 A 的内容覆盖 B（版本号相同时静默命中） | `openEdit` 切换目标立即 `resetForm()` 清空字段与 version；新增 `formReady` 标志（仅当前目标详情成功加载为 true）；`save()` 编辑模式 formReady=false 拒绝提交；保存按钮同步禁用 | FOOD-E2E-007 |
| P1（第二轮 Review） | 保存期间（`await verifyForm` 网络往返中）切换编辑对象，提交目标被改为新对象，把 A 的内容 PUT 到 B | `save()` 在 await 前一次性快照 `target/targetVersion/isEdit`，await 后全部使用快照；保存期间列表「编辑」「停用/启用」按钮禁用 | FOOD-E2E-007（按钮禁用）+ 代码评审（竞态本身无确定性 E2E） |
| P2（第三轮 Review） | 食材 PUT/POST 省略字段被当作显式 null，静默清空已有营养/换算数据（DTO 指针无法区分键缺失与 null） | 新增 `decodeFoodInput`：解码后对 14 个必需键做存在性检查，缺键 400 并列全缺失字段；键存在值为 null 放行（显式未知语义不变）；requireFoodInput 继续兜底 allergens/complete 显式 null（防解引用 panic） | FOOD-API-021（缺键 400 且数据不清）/ 022（显式 null 清空语义保持）/ 023（allergens null 拒绝） |
| P2（第三轮 Review） | 库存删除失败提示被紧随的列表刷新清空（catch 先设提示后 `load()`，load 开头清提示） | 调换顺序：先 `await load()` 刷新取回最新状态，再写删除失败提示（操作结果优先展示） | INV-E2E-010（409 后提示可见、批次为最新值） |
| P2（第四轮 Review） | Swagger schema 与后端校验不一致：10 个可空字段未标必填，按 schema 构造的缺字段请求被后端 400 拒绝 | `foodInput` 10 字段补 `binding:"required"`（本项目 binding tag 仅为 Swagger 元数据、无运行时影响），保留 x-nullable；再生成 docs；新增防漂移测试断言 schema required 与后端 foodRequiredKeys 完全一致 | FOOD-API-024（含 14 键哨兵断言） |

## 5. 覆盖缺口（如实记录）

以下为实施方案审查识别、截至本文未补齐的自动化覆盖（详见 TEST_EXECUTION_PLAN.md 第五节）：

1. INV-SEC-009（事务内撤销身份）仅有 store 级覆盖，无 API 级专属用例。
2. REC-SEC-007（食谱写路径事务内降权/禁用复核）、REC-SEC-011（food 过敏原不可被食谱移除）、REC-SEC-013（未知营养不当作零）无显式断言（相关行为由相邻用例间接覆盖）。
3. WEB 组 34 条用例无组件级单测：项目约束不引入 @vue/test-utils，取舍为 E2E 覆盖主要交互；未被 E2E 场景合并的条目无自动化覆盖。
4. MIG 类 16 条由 `store/migrate_test.go` 共享迁移链测试间接覆盖（测试函数未逐条标注 INV-MIG/REC-MIG ID，`-run` 不可按该 ID 检索）。
5. REC-API-014/015 由相邻用例与共享夹具间接覆盖。
6. CLI 类用例由 `verify_cli.py` 黑盒承载，无用例 ID 级定位。

## 6. 遗留事项

1. **既有失败（非本次引入）**：`web/e2e/user-module.spec.js` 的 `FE-003/004 ID-001`（跨标签切换账号应清空旧表单）在干净 HEAD（3b0e83a，不含本次任何改动）的基线 worktree 中同样失败，证据确凿。现象：旧标签页被 `App.vue` 身份守卫重定向到 `/login` 而非原地显示旧账号。疑与 JWT 提交（3b0e83a）后的身份代次时序或运行浏览器的差异有关，建议单独排查。另：该 spec 存在负载敏感抖动——`ADM-001/002/004` 曾在与 CLI 验证并行的全量回归中失败、单独复跑即通过（6.0s）；跑 E2E 时建议避免并行重负载任务。
2. 本次全部改动留在工作树，未提交；由维护者审阅后决定提交方式。
3. 数据侧：12 条旧食材营养/过敏原为未知（符合设计），正式公共食谱与食材标注的来源授权需维护者人工核对，自动测试不能代替授权核查。
4. 后续模块（推荐、烹饪扣减、记餐、营养趋势）不在本次范围；库存扣减与食谱发布的数据基础已就绪。
