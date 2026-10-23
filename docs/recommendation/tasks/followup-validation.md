# Claude 复审修复后的独立验证

日期：2026-10-23。负责 Agent：validation；未参与本轮业务修改，仅新增 `RECO-E2E-006` 回归与该用例文档映射。

## 范围与独立检查

- 搜索校验：Trim 后按 Unicode 码点计数，100 码点可提交、101 码点本地拒绝；同时覆盖基本平面中文与补充平面字符，不使用 UTF-16 `maxlength` 截断。真实浏览器断言无效查询不会发推荐请求、修正到100码点后真实接口返回200且清除错误。
- 输入校验应在页码与请求状态变更前完成。独立源码检查发现 `changePage` 先加页码，再让 `load` 拒绝无效查询，会导致无请求但页标变化、旧条目留在新页标下。已通知主 Agent；其将目标页参数交给 `load`，校验通过后才提交页码。
- 独立新增 `RECO-E2E-006`，使用21条响应的页面夹具，验证超长查询时点击下一页不会发请求、页标仍为1且20条第一页记录保留；修正搜索后可正常到第2页，仅显示第21条。该用例验证修复后行为；没有在旧版重跑并声称观察到测试失败。
- 详情原料展示统一调用 `unitLabel`：静态检查与原内联映射 g→克、ml→毫升、piece→个及未知单位原文回退等价。
- 用例追溯：程序逐个检查 Go 测试函数前的编号与文档函数/文件一致、编号无重复；Vitest和E2E实际测试标题与编号范围一致。共33个唯一ID：14个Go函数、12个Vitest定义（参数化展开18例）、6个推荐E2E、1个Swagger兼容测试。两份测试文档已追加006映射/区间。

## 实际执行结果

| 命令 | 结果 | 完整输出 |
| --- | --- | --- |
| `npm test --prefix web` | 通过；5个文件77例（推荐18例，其他59例） | [Vitest](logs/followup-vitest.log) |
| `npm run build --prefix web` | 通过；发现翻页问题前的初次构建 | [初次构建](logs/followup-build.log) |
| `(cd server && go test ./internal/api ./internal/logic ./internal/store -run Recommendation -count=1 -v)` | 通过；14个顶层函数及其参数化子项 | [Go专项](logs/followup-go.log) |
| `(cd web && npx playwright test recommendation-module.spec.js swagger.spec.js)` | 首轮6/6通过；当时未有新增006 | [首轮Playwright](logs/followup-playwright-initial.log) |
| `npm run build --prefix web` | 翻页修复后重新构建通过；7例最终浏览器检查使用此产物 | [最终构建](logs/followup-build-final.log) |
| `(cd web && npx playwright test recommendation-module.spec.js swagger.spec.js)` | 最终7/7通过，13.4秒；推荐001–006及兼容001 | [最终Playwright](logs/followup-playwright-final.log) |
| 独立逐文件编号检查（Python断言） | 通过；33个唯一编号无缺项/重复且Go函数与文档精确一致 | [追溯检查](logs/followup-id-mapping.log) |

日志范围：上表测试/构建日志均为此次命令直接重定向的完整 stdout/stderr，无手工裁剪；编号检查为独立断言输出。一次Go命令因日志重定向路径写错返回 `no such file or directory: ../../../docs/recommendation/tasks/logs/followup-go.log`，该次未运行测试；纠正为 `../docs/...` 后上述Go专项正常执行通过。

## 结论与未执行项

本轮发现的无效查询翻页页标错误已修复，独立新增回归验证关闭；码点校验、单位共用helper和ID追溯均符合本轮范围。无尚未解决的验证失败或环境阻塞。

未重跑完整Go race、CLI和全部浏览器测试：Go业务仅增加注释，CLI未改，本轮已执行受影响的推荐/Swagger七例；不得将此次专项描述为本轮全套浏览器回归通过。此前完整验证结果保留在 [validation.md](validation.md)，不能替代本轮未执行项。Claude对第3/5项的独立复审由主 Agent 协调，本记录不代其作出结论。

## RECO-STORE-001 查询次数证据补强

按主 Agent 与 Claude 复审协调，在既有 `TestRecommendationBatchQueriesAndVisibility` 夹具中，于 `counted.reset()` 前写入非空 `excluded_food_ids_json`（既有食材ID），并新增断言快照偏好完整带回同一个ID；原40候选、5 SELECT断言保持。该前置可以发现直接复用带忌口食材扩展读取的 `readPreferences` 所引入的额外查询；原空忌口夹具不足以排除此回归。

实际命令：`(cd server && go test ./internal/store -run '^TestRecommendationBatchQueriesAndVisibility$' -count=1 -v)`，通过（测试0.03秒、包0.846秒），[完整输出](logs/followup-store-query-count.log)。复用 `RECO-STORE-001`，不新增编号、不修改业务实现，也未扩大重跑范围。

## Unicode 空白契约：独立 Review 发现1的修复复测

Review 指出真实跨语言差异：JavaScript `trim()` 保留 NEL（U+0085），但 Go `strings.TrimSpace` 会裁剪；JavaScript会裁剪 FEFF（U+FEFF），但Go会保留。旧实现因此可能误拒合法100码点查询、误裁剪应参与字面检索的FEFF，并造成空态归因不一致。

主 Agent 新增 `normalizeRecommendationQuery`，按 Unicode `White_Space` 统一处理前置校验、真实请求参数与空态判断。独立验证 Agent 仅扩展既有 `RECO-E2E-005`，真实服务下验证三条边界：

1. 首尾NEL包围100个「菜」：发出请求的 `q` 恰为100个「菜」，响应200，错误清除。
2. FEFF加100个「菜」：总101码点，本地报错且推荐请求数不增长。
3. FEFF加99个「菜」：总100码点，响应200，实际 `q` 原样保留前导FEFF。

`RECO-WEB-013` 同时覆盖上述校验及仅NEL/仅FEFF在can_cook空态中的区别。两份映射文档已追加013/区间；本轮不新增E2E编号。

| 实际命令/检查 | 最终结果 | 完整直接重定向日志 |
| --- | --- | --- |
| `npm test --prefix web` | 5文件78/78通过；推荐13个定义展开19例 | [Vitest](logs/followup-unicode-vitest.log) |
| `npm run build --prefix web` | 新normalizer版本构建通过，后续E2E使用该产物 | [build](logs/followup-unicode-build.log) |
| `(cd web && npx playwright test recommendation-module.spec.js swagger.spec.js)` | 推荐001–006及兼容001，7/7通过（13.7秒） | [Playwright](logs/followup-unicode-playwright.log) |
| 独立编号/映射断言 | 34个唯一ID；14个Go函数、13个Vitest定义、6推荐E2E、1Swagger；真实Go函数及Vitest标题逐项匹配文档，两份区间一致 | [映射](logs/followup-unicode-id-mapping.log) |

独立验证结论：Review发现1已修复并经单元和真实浏览器请求复测通过，验证侧关闭；已通知Review Agent复核关闭状态。无新增失败或环境阻塞。未重复Go/race/CLI，因为本次仅修改前端Unicode处理；本轮仍不声称重跑了全套浏览器测试。前文77例/33个ID为前一轮的历史记录，最新最终结果以本节78例/34个ID为准。
