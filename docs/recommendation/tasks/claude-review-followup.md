# Claude Review 后续处理

范围：处理 Claude Agent 首轮五条发现。用户明确要求修复接受项，并通过 Herdr 交由原 Claude Agent 独立复审驳回理由。已验证 HERDR_ENV=1，目标来自 agent list：FreshPlate 的 Claude pane w2:p4；未修改该 Agent 的模型、权限或终端布局。

## 接受项

1. 搜索长度：新增 `recommendationQueryError`，以 Unicode White_Space 规则与 Go TrimSpace 一致地裁剪，再按 Unicode 码点计数，超过 100 时在请求前提示。移除 HTML UTF-16 maxlength，避免误拦合法的 100 个补充平面字符。`load` 校验通过后才提交目标页码；非法搜索不改变已显示列表的页码。
2. 测试追溯：所有新增推荐 Go 测试函数补 RECO-LOGIC/STORE/API 注释；Vitest 标题补 RECO-WEB；E2E 保留既有 RECO-E2E 并新增边界验证；Swagger 用例补 RECO-COMPAT。`docs/test/RECOMMENDATION_TEST_CASES.md` 逐函数编号，`TEST_EXECUTION_PLAN.md` 记录编号区间与命令；参数化子用例继承定义处编号。
4. 单位标签：详情普通原料和推荐匹配明细统一使用 `unitLabel`，删除旧的内联映射。

## 驳回理由与独立复审

3. 直接复用 readPreferences 会引入逐忌口食材的查询，把固定五次查询变为 5+N；基础 JSON 解析重复可以以后抽取，但本次不作为必须重构。
5. 重复 ValidatePreferences 属实；请求级校验保证无候选时也拒绝非法偏好，逐候选 EnforceRestrictions 是设计指定的共享安全边界。没有设计规模下性能问题证据，不删除校验、不修改安全接口。

已通过 `herdr agent prompt w2:p4` 将上述理由和代码/设计位置提交给 Claude，明确要求独立核对，不改业务或测试，并可坚持原发现。Claude 已实际完成复审（completion_seq=161），并完成证据更正（completion_seq=163）；原始终端记录见 [首次复审](logs/claude-reassessment.txt) 与 [证据更正](logs/claude-reassessment-corrections.txt)。

最终结论：第3项接受驳回，撤回直接复用 readPreferences 的建议，降为可选整理；第5项接受驳回，撤销效率缺陷定性，不删安全校验。没有开展偏好基础读取或安全接口重构。

证据纠正：Claude 曾以当前工作区已有用例ID推断原第2项误报；主Agent指出ID恰为本轮刚补入，Claude已撤回该推断，按“第2项已修复”记录。原五查询夹具未设非空忌口，不足证明此分支防N+1；已由独立验证补强并复跑通过。Claude未进行性能基准，已撤回其“毫秒级以下”和开销比例估算，报告不将其作为实测结论。

## 验证和修复

本轮由未参与业务修改的 validation Agent 独立验证，记录见 [followup-validation.md](followup-validation.md)。

独立验证发现：本地拦截超长 q 后，旧 changePage 仍会先改页码，再调用被校验拒绝的 load，导致旧页条目与新页码不一致。已改为 changePage 传入待跳页码，由 load 在校验通过后提交；新增 RECO-E2E-006 专项复测。

本轮不修改 Go 业务逻辑或数据库。Go 仅增加测试注释，不重复完整 race/CLI；前端单测、构建、推荐及 Swagger 浏览器检查和 Go 推荐专项按实际结果记录，不沿用旧结果冒报本轮通过。

独立 Review 另发现 JS trim 与 Go TrimSpace 的空白集合不一致：NEL 会被 Go 裁剪却被 JS 保留，FEFF 则相反。已新增 normalizeRecommendationQuery，按 Unicode White_Space 裁剪，校验、实际请求及空态判断共用该值；RECO-WEB-013 与扩展 RECO-E2E-005 覆盖两类边界。最终复测与关闭状态以 [独立验证](followup-validation.md) 和 [独立 Review](followup-review.md) 为准。

## 最终验证结果

独立验证全部通过：Vitest 78/78、前端重建、推荐及 Swagger 浏览器专项 7/7、34个唯一用例ID的文件/函数/标题/区间映射。Go推荐专项14个顶层函数通过，非空忌口五查询夹具额外单独复跑通过。新增NEL/FEFF规范化与非法查询翻页回归已验证关闭，无未解决验证失败。Go生产代码未改，不重复完整race/CLI；本轮浏览器结果为专项7例，不声称重跑全部套件。
