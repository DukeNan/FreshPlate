# FreshPlate 食材库存模块技术方案评审

状态：评审完成。评审日期：2026-10-19。评审对象：[INVENTORY_MODULE_TECHNICAL_DESIGN.md](INVENTORY_MODULE_TECHNICAL_DESIGN.md) 当前版本，评审时仓库 HEAD 为 `3b0e83a`。同批评审：[食材](../food/FOOD_MODULE_TECHNICAL_DESIGN_REVIEW.md)、[食谱](../recipe/RECIPE_MODULE_TECHNICAL_DESIGN_REVIEW.md)。

修订说明：2026-10-19 按确认清单修订方案及本评审；“已处理”指文档修订，不代表库存业务已经实现。

## 1. 总体结论

方案整体可实施：数量契约防浮点漂移、UTC 日历日判定避免时区漂移、统一 404 防枚举、版本条件更新完整。字段计数已修正、DELETE body 取舍已明确，状态筛选提示已补齐数量和逻辑删除条件，可按修订方案进入实现与验证。

## 2. 必须修正

### 2.1 PUT 可变字段计数错误

- 位置：设计文档 §4"PUT 必须提交全部**四个**可变字段和当前 version"。
- 可变字段只有 `quantity`、`expires_on`、`note` **三个**（`food_id`/`unit` 不可变）；示例 JSON 的 4 个 key 是含 version 的。
- 该计数错误会直接误导实现与测试用例编写，应改为"三个可变字段和当前 version"。
- 处理结果：方案 §4 已改为三个可变字段（quantity、expires_on、note）和 version。

## 3. 建议修改

### 3.1 DELETE 携带 JSON body 的兼容性风险

- 位置：设计文档 §4，`DELETE /inventory-items/:id` 提交 `{"version":2}`。
- DELETE body 存在代理、网关或客户端不透传的兼容性风险；当前本地同源 fetch 可明确接受该限制，但不能依赖所有未来部署都支持。
- 最终取舍：保留 JSON body，不增加 query/header 替代通道。方案 §4 明确在既有认证、身份和媒体类型检查后，先解析并验证 version，再检查资源存在性；body 丢失或 version 缺失/null/非法均拒绝，不会退化为无版本删除。新增代理时须另验兼容性。

## 4. 低优先级备注

- **expiry_state 筛选提示已纠正并纳入方案 §3.3**：所有状态都限定当前 user_id、deleted_at IS NULL；depleted 使用 quantity_milli=0，其余四个状态均增加 quantity_milli>0，再分别匹配 expired（日期<today）、expiring（today～today+3）、fresh（日期>today+3）、unknown（日期为空）。原提示遗漏数量条件，会把用完批次错误归入日期状态。
- **列表仅 id DESC**：后续可能需要按到期日排序；`(user_id, expires_on, id)` 索引已支持，v1 维持现状即可。

## 5. 跨文档一致性

| 核对项 | 结果 |
| --- | --- |
| 依赖食材模块 v3（003 先行），本模块 004 → v4 | ✅ 一致 |
| 数量契约（字符串 + 千分位 + 三位小数）与食谱共享同一 helper | ✅ 一致 |
| 单位集 g/ml/piece 与食材/食谱一致 | ✅ 一致 |
| 停用食材"已有库存可减不可增"与食谱的停用引用例外口径一致 | ✅ 一致 |
| 错误码（FOOD_UNAVAILABLE / FOOD_DISABLED / VERSION_CONFLICT）纳入共用体系 | ✅ 一致 |

## 6. 代码现状核对

本模块直接相关项均属实（三模块合并核对共 10 项，全表见[食材评审 §4](../food/FOOD_MODULE_TECHNICAL_DESIGN_REVIEW.md)）：

| # | 声称 | 结果 | 证据 |
| --- | --- | --- | --- |
| 1 | 迁移器需先扩展为顺序迁移（食材方案 §7.2），否则 004 不会执行 | 属实 | `server/internal/store/db.go:150` |
| 2 | 64 KiB 解码上限适用于本模块（库存载荷远小于上限，无冲突） | 属实 | `server/internal/api/api.go:111` |
| 5 | JWT：Bearer + `X-Expected-User-ID`；身份/改密错误沿用 | 属实 | `docs/user/JWT_AUTHENTICATION.md:7,9` |
| 8 | 分页默认 20、上限 100 | 属实 | `server/internal/api/users.go:47` |
| 9 | `WithImmediate` 单连接 BEGIN IMMEDIATE，写事务内复核可行 | 属实 | `server/internal/store/db.go:85,91` |

## 7. 方案亮点

- 数量字符串契约不经 float64 解析，杜绝库存反复编辑的浮点漂移。
- UTC 日历日判定 + 服务端注入时钟，临期状态可测试、不漂移。
- 诚实记录"无幂等键、网络异常重提可能产生重复批次"，不虚假承诺。
- 版本条件更新覆盖创建/编辑/删除全路径，双标签页竞争语义明确。

## 8. 修订追踪

| 评审项 | 状态 |
| --- | --- |
| 2.1 PUT 字段计数改为"三个可变字段" | 已处理：方案 §4 已修正 |
| 3.1 DELETE body 兼容性取舍 | 已明确：保留 body，version 必须在存在性检查前验证，丢失即拒绝 |
| 4 expiry_state 筛选提示 | 已处理：四类非耗尽状态增加正数量条件，所有分支排除逻辑删除 |
