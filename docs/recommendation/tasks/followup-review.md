What this change does: The follow-up adds a client-side 100-code-point search limit and keeps the current page unchanged when validation rejects a request. It reuses the existing unit label helper and adds test IDs, traceability tables and regression checks.

## 已关闭发现（保留首次审查证据）

1. **低严重度：前端拒绝 Go 接受的合法 Unicode 搜索** (`web/src/recommendations.js:22`，调用处 `web/src/views/RecommendationsView.vue:15`)
   - **What this is:** 前端先裁剪输入再按码点计数，意图与服务端 TrimSpace 后至多100码点一致。
   - **Problem:** JavaScript `trim()` 与 Go `strings.TrimSpace` 空白集合不同。输入 `\u0085` 加100个「菜」时，Go裁剪NEL后接受100码点，前端保留NEL并数到101而拒绝；合法搜索无法发出。已通过本机 `go doc unicode.IsSpace`（明确含U+0085）和Node表达式确认后者计101。反方向U+FEFF被JS裁剪但Go不裁剪，亦不满足同一规范化契约。
   - **Fix:** 在推荐路径共用一个按Unicode `White_Space`裁剪的规范化值（例如首尾 `\p{White_Space}` 正则）；校验、实际请求及空态使用它。补NEL与FEFF边界断言，保证不是只改helper而调用处仍先用JS trim。
   - **If we skip it:** 从外部文本粘贴这类空白时，前端与API的合法查询范围和搜索内容不同。常见中文/补充平面字符不受影响，无安全或数据写入风险。

## 其余核对结果

本Agent未参与本轮实现或测试修改。只读复核recommendations helper、RecommendationsView、RecipeDetailView、全部编号后的Go/前端测试、本轮新增E2E005/006、两份测试映射文档及followup-validation.md；仅写本报告。沿用原Review的本机数十至数百食谱规模假设。

- 非法查询在page、sequence、loading、列表清空和网络调用之前返回；changePage只传targetPage，不提前改变page。旧列表和旧页标一致，修正后load(true)回第一页，合法翻页才提交目标页。E2E006对无请求、页标、20条旧记录以及修正后第2页唯一记录有直接断言。
- JS展开字符串按Unicode码点计算，中文与补充平面的100/101边界正确；移除UTF-16 maxlength避免先截断补充平面字符。E2E005的101无请求及100真实API200断言有效，但未覆盖上述空白集合差异。
- 详情原料unitLabel替换与原g/ml/piece映射及未知单位回退等价，没有修改匹配数据或version语义。
- 编号映射含14个Go顶层函数、12个Vitest定义（展开18例）、6个推荐E2E、1个Swagger测试；两份文档与实际函数/标题范围一致，未把33个ID误作33个实际展开用例。
- 独立补强的RECO-STORE-001在计数重置前写入非空忌口，断言该ID完整读回且40候选五查询不变；能揭示直接复用含额外食材扩展查询的偏好读取路径。核对专项原始输出：PASS、包0.846秒。
- 核对独立验证记录：77/77 Vitest、最终build、14个Go推荐顶层测试、最终推荐+Swagger 7/7通过；没有把本轮未执行的完整race/CLI/全部浏览器回归计为本轮通过。Go生产逻辑未因本轮修改。

## 修复复核与最终结论

发现1已关闭。复核新增 `normalizeRecommendationQuery` 使用首尾Unicode `White_Space` 正则，准确包含NEL并排除FEFF；校验helper、View实际请求和空态归因均共用它，没有残留推荐查询的JS trim前置调用。`RECO-WEB-013` 覆盖NEL合法100、FEFF超长101及NEL/FEFF空态区别。

独立验证最终扩展的E2E005使用真实API，检查NEL包100菜的实际q已裁剪、FEFF加100菜不发请求、FEFF加99菜实际q保留FEFF且响应200；同批E2E006仍通过，确认此次规范化没有破坏非法翻页状态保护。直接核对 `followup-unicode-{vitest,build,playwright,id-mapping}.log` 及验证报告：Vitest78/78、重建成功、推荐+Swagger7/7（13.7秒）、34个唯一编号映射通过；两份文档013映射已落盘。前文77例/33编号保留为首次复核时历史状态，最终为13个Vitest定义展开19个推荐用例、合计78例及34编号。额外 `git diff --check` 通过。

无新增问题、无未关闭Review项。本轮非空忌口固定五查询补强及Claude对3/5的复审未引入Go业务变更，前端改动限于用户要求范围。

Verdict: Ship.

Not checked: 未重复运行完整Go/race、CLI、全量浏览器回归或超过设计规模的压测；本轮受影响检查由独立验证执行并核对其原始日志。先前完整race结果仍见validation.md，不能替代本轮未执行项。
