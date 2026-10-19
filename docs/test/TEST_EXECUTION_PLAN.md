# 三模块测试实施方案

本文档是「步骤 4」交付物：把 `FOOD_TEST_CASES.md`、`INVENTORY_TEST_CASES.md`、`RECIPE_TEST_CASES.md` 三份用例文档中的用例 ID 映射到**已实际落盘**的测试代码与运行方式。映射关系以 grep 实际测试文件（文件头注释、函数名与函数内注释中的用例 ID）为准，不照抄用例文档中的「建议落点」；无测试对应的区段在第五节如实列为缺口。

约定：Go 命令一律用 `env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go`（shell 的 GOROOT 指向旧版本，直接 `go` 会编译失败），且在 `server/` 目录下执行；npm 命令在 `web/` 目录下（或仓库根用 `--prefix web`）。

---

## 一、测试分层与映射总表

> 「测试函数前缀」列给出的是可传给 `go test -run` 的正则锚点；一个函数可覆盖多个相邻用例（函数名以 `TestXxx001_002` 形式标注首尾，函数内注释补充中段 ID）。

### 食材模块（FOOD）

| 用例 ID 区段 | 实际测试文件 | 测试函数 / 前缀 | 运行命令（原样） |
| --- | --- | --- | --- |
| FOOD-LOGIC-001~019 | server/internal/logic/foods_test.go | `TestFoodLOGIC`（17 个函数，002 并入 TestFoodLOGIC001_002、006 并入 TestFoodLOGIC005_006） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/logic/ -run 'TestFoodLOGIC')` |
| FOOD-STORE-001~013 | server/internal/store/foods_test.go | `TestFoodSTORE`（12 个函数，010 并入 TestFoodSTORE007_010） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/store/ -run 'TestFoodSTORE')` |
| FOOD-MIG-001~010、012 | server/internal/store/migrate_test.go | `TestFoodMIG`（11 个函数；012 为 v5 结构校验，含全部新表/新列/新索引） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/store/ -run 'TestFoodMIG')` |
| FOOD-MIG-011（CLI 打开新库与已升级库） | scripts/verify_cli.py（无 Go 测试；现库版本为 v5） | — | `env -u GOROOT python3 scripts/verify_cli.py --go /Users/dukenan/.goenv/versions/1.26.4/bin/go` |
| FOOD-API-001~024 | server/internal/api/foods_test.go | `TestFoodAPI`（021~024 为缺字段/显式 null/allergens null/Swagger required 一致性回归） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/api/ -run 'TestFoodAPI')` |
| FOOD-SEC-001~007、009 | server/internal/api/foods_test.go | `TestFoodSEC`（8 个函数） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/api/ -run 'TestFoodSEC')` |
| FOOD-SEC-008（access 过期刷新重试边界） | web/src/api.test.js（既有用例，无 ID 标注） | `refreshes an explicitly expired JWT and retries a write exactly once` 等 4 条刷新用例 | `(cd web && npm test)` |
| FOOD-COMPAT-001 | server/internal/api/foods_test.go | `TestFoodCOMPAT001_LegacyFoodsEndpointByteContract` | 同 FOOD-API 命令，`-run 'TestFoodCOMPAT'` |
| FOOD-COMPAT-002~005 | 无新增专属测试：002/003 由既有套件回归（api_test.go、user-module.spec.js）、004 与 FOOD-STORE-008 同源（TestFoodSTORE008）、005 由 swagger.spec.js opblock=33 断言承载（均已更新，无 ID 标注） | — | 全量回归（见第二节） |
| FOOD-WEB-001~007 | 无组件单测（项目约束见第三节取舍），交互行为转 E2E 覆盖 | — | 见第三节 |
| FOOD-E2E-001~007 | web/e2e/food-module.spec.js（已落盘；007 为编辑对象切换失败态与保存目标固定回归） | `test('FOOD-E2E-xxx …')` | `(cd web && npx playwright test food-module.spec.js)` |

### 库存模块（INV）

| 用例 ID 区段 | 实际测试文件 | 测试函数 / 前缀 | 运行命令（原样） |
| --- | --- | --- | --- |
| INV-LOGIC-001~018（数量契约） | server/internal/logic/quantity_test.go | `TestParseQuantityMilli`（001~016）、`TestFormatQuantity`（017）、`TestQuantityRoundTrip`（018）、`TestParseExpiryDate`（019~024 亦在此） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/logic/ -run 'TestParseQuantityMilli|TestFormatQuantity|TestQuantityRoundTrip')` |
| INV-LOGIC-019~036（日期/expiry_state/业务规则） | server/internal/logic/inventory_test.go | `TestInvLOGIC019_024`、`TestInvLOGIC025_027_031`（注释标注 025/027/028/029/030/031）、`TestInvLOGIC032`、`TestInvLOGIC033`、`TestInvLOGIC034`、`TestInvLOGIC035`、`TestInvLOGIC036`、`TestInvLOGIC026` | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/logic/ -run 'TestInvLOGIC')` |
| INV-STORE-001~023 | server/internal/store/inventory_test.go | `TestINVSTORE`（14 个函数，函数名标注合并区间，如 TestINVSTORE003_004_005_006） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/store/ -run 'TestINVSTORE')` |
| INV-MIG-001~009 | 无 INV-MIG 标注的专属函数；由共享迁移链测试承载：TestFoodMIG001（空库直建到 v5，含 inventory_items）、TestFoodMIG003（幂等）、TestFoodMIG008（结构损坏拒绝）、TestFoodMIG012（v5 结构校验含 inventory_items 列与两个索引）。用例文档建议的 server/migrations/migrations_test.go 路径不存在，实际为 store/migrate_test.go。006（CLI）→ verify_cli.py | — | 同 FOOD-MIG 命令 + verify_cli.py |
| INV-API-001~037 | server/internal/api/inventory_test.go | `TestInvAPI`（12 个函数；037（no-store）归入 TestInvAPI010_023_032_033，另有共享 fixture 对每个请求全局断言 no-store，见 api_test.go） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/api/ -run 'TestInvAPI')` |
| INV-SEC-001~008、010~014 | server/internal/api/inventory_test.go | `TestInvSEC`（8 个函数：001_002_003、004、005、006_007_008、010_011_012、013、014） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/api/ -run 'TestInvSEC')` |
| INV-SEC-009（事务内撤销身份 API 级） | **无 API 级专属用例**；store 级由 TestINVSTORE014_SessionRevokedMidTransaction 覆盖同一复核路径（见第五节缺口） | — | 同 INV-STORE 命令 |
| INV-COMPAT-001 | web/e2e/inventory-module.spec.js（已落盘） | `test('INV-COMPAT-001 …')` | Playwright |
| INV-COMPAT-002 | server/internal/api/inventory_test.go（与 INV-SEC-010 同源，TestInvSEC010_011_012） | — | 同 INV-SEC 命令 |
| INV-COMPAT-003/005 | 全量回归矩阵 + swagger.spec.js（opblock 计数已更新为 33，无 ID 标注） | — | 见第二节 |
| INV-WEB-001~018 | 无组件单测（取舍见第三节）；018 已实现 | INV-WEB-018 → web/src/quantity.test.js（28 断言，随 `npm test` 执行）；017 中路由/导航注册已实现于 router.js、App.vue，无专属测试 | 见第三节 |
| INV-E2E-001~009 | web/e2e/inventory-module.spec.js（已落盘） | `test('INV-E2E-xxx …')` | `(cd web && npx playwright test inventory-module.spec.js)` |

### 食谱模块（REC）

| 用例 ID 区段 | 实际测试文件 | 测试函数 / 前缀 | 运行命令（原样） |
| --- | --- | --- | --- |
| REC-STORE-001~018、020 | server/internal/store/recipes_test.go | `TestRecipeSTORE`（14 个函数；016 与 SEC-014 合并于 TestRecipeSTORE016SEC014） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/store/ -run 'TestRecipeSTORE')` |
| REC-REG-001~003 | server/internal/store/recipes_test.go | `TestRecipeREG001_StepsOnlyPut`、`TestRecipeREG002_UpdateReturnsFreshDetail`、`TestRecipeSTORE014_NoChangePut` | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/store/ -run 'TestRecipeREG|TestRecipeSTORE014')` |
| REC-STORE-019（v5 索引与结构校验） | 无 REC-STORE-019 标注；实际由 TestFoodMIG012StartupStructureChecks（wantIndexes 含 recipes_owner、recipes_visibility、recipe_ingredients_food）与 TestFoodMIG008 承载 | — | 同 FOOD-MIG 命令 |
| REC-LOGIC-001~019 | server/internal/logic/recipes_test.go | `TestRecipeLOGIC`（14 个函数；002/005/006/008/019 以合并函数名标注，016 store 侧另见 store/recipes_test.go 注释与 TestRecipeSTORE016SEC014） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/logic/ -run 'TestRecipeLOGIC')` |
| REC-MIG-001~003、005~006 | 无 REC-MIG 标注；由共享迁移链测试承载（同 INV-MIG：TestFoodMIG001/003/004/008/012，迁移链已到 v5，含 recipes 四表） | — | 同 FOOD-MIG 命令 |
| REC-MIG-004（CLI 打开 v5） | scripts/verify_cli.py | — | `env -u GOROOT python3 scripts/verify_cli.py --go /Users/dukenan/.goenv/versions/1.26.4/bin/go` |
| REC-COMPAT-001~003 | 无专属标注；001/002 由全量 Go 测试与既有 e2e 回归承载，003 由 swagger.spec.js（opblock=33）承载 | — | 见第二节 |
| REC-API-001~013、016~024 | server/internal/api/recipes_test.go | `TestRecipeAPI`（10 个函数：001_002、003、004_005、006_008_009、010_013、016_008、017_018、019、020、021_024）+ TestRecipeAPIFoodUnavailable | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/api/ -run 'TestRecipeAPI')` |
| REC-API-014（非食谱写路由仍限 64 KiB） | 无独立函数；在 TestRecipeAPI010_013 内以 PUT /api/me/preferences 约 90KiB → 400 断言承载（无 014 标注） | — | 同上 |
| REC-API-015（响应契约） | 无独立函数；items/total 形状断言散布于 TestRecipeAPI001_002、TestRecipeAPI017_018；no-store 由共享 fixture 全局断言；503 DATABASE_BUSY 由 TestFoodAPI018 同模式覆盖 | — | 同上 |
| REC-SEC-001~006、008、012、014、016 | server/internal/api/recipes_test.go + server/internal/store/recipes_test.go | `TestRecipeSEC001_005`、006（TestRecipeAPI006_008_009 内 403 断言，行 229）、`TestRecipeSEC008`、012（TestRecipeSTORE016_012_DerivedOnRead）、`TestRecipeSEC014`、016（TestRecipeAPI021_024 内注释标注） | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/api/ ./internal/store/ -run 'TestRecipeSEC')` |
| REC-SEC-007、011、013、015 | **未发现对应断言**（见第五节缺口；015 的浏览器侧 CSP 由既有 user-module.spec.js SEC-004 模式覆盖，Go 侧无断言） | — | — |
| REC-WEB-001~010 | 无组件单测（取舍见第三节）；011 已实现 | REC-WEB-011 → web/src/quantity.test.js（28 断言，随 `npm test` 执行） | 见第三节 |
| REC-E2E-001~006 | web/e2e/recipe-module.spec.js（已落盘；006 为步骤持久化与删除按钮回归） | `test('REC-E2E-xxx …')` | `(cd web && npx playwright test recipe-module.spec.js)` |

---

## 二、运行矩阵

按序执行；「验证的用例区段」列出该命令覆盖的 ID 组。

| # | 命令（原样，含执行目录） | 验证的用例区段 | 预期通过标准 |
| --- | --- | --- | --- |
| 1 | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go build ./...)` | 全部后端区段（编译前置） | 退出码 0，无输出 |
| 2 | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go vet ./...)` | 全部后端区段（静态检查） | 退出码 0，无告警 |
| 3 | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./...)` | FOOD-LOGIC/STORE/MIG/API/SEC/COMPAT-001、INV-LOGIC/STORE/API/SEC、REC-STORE/LOGIC/API/SEC 全部 + 既有用户模块回归（REC-COMPAT、INV-COMPAT-003、FOOD-COMPAT-002） | 全部 PASS；用例数不低于本表映射的函数数之和 |
| 4 | `(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test -race ./internal/...)` | 并发类：INV-STORE-013/021、INV-STORE-014、FOOD-STORE-005/007/010、REC-STORE-010/013、FOOD-SEC-004/005 | 全部 PASS 且数据竞争报告为 0 |
| 5 | 分区段定位（失败时使用，示例）：`(cd server && env -u GOROOT /Users/dukenan/.goenv/versions/1.26.4/bin/go test ./internal/logic/ -run 'TestFoodLOGIC')` | 单一区段 | 该区段函数全 PASS |
| 6 | `npm test --prefix web`（等价 `(cd web && npm test)`） | FOOD-SEC-008（api.test.js 既有刷新重试用例）、validation/auth 既有单测；FOOD-WEB/INV-WEB/REC-WEB 无组件测试（见第三节） | Vitest 全绿（node 环境） |
| 7 | `npm run build --prefix web` | 前端构建级验证（视图/路由可编译，vue-tsc 级错误即失败） | 退出码 0，产出 web/dist |
| 8 | `(cd web && npx playwright install chromium && npm run test:e2e)` | 既有回归：user-module.spec.js、swagger.spec.js（opblock=33，承载 FOOD-COMPAT-005、INV-COMPAT-004/005、REC-COMPAT-003）；food/inventory/recipe-module.spec.js（已落盘）覆盖 FOOD-E2E-001~006、INV-E2E-001~009、INV-COMPAT-001、REC-E2E-001~006；已知既有失败 user-module FE-003/004 ID-001（见总结文档 §6） | 隔离服务端口 18080 自动起停；全部 spec 通过；CSP 违规计数为 0 |
| 9 | `env -u GOROOT python3 scripts/verify_cli.py --go /Users/dukenan/.goenv/versions/1.26.4/bin/go` | FOOD-MIG-011、INV-MIG-006、REC-MIG-004、INV-COMPAT-003（CLI 侧） | 脚本自身断言全过（admin init/recover-password、进程锁、恢复流程）；脚本内建库经迁移到 v5 |
| 10 | admin CLI 打开 v5 库（verify_cli.py 已内置；如需手工核验）：`sqlite3 <夹具库路径> 'select max(version) from schema_migrations'` | FOOD-MIG-011、INV-MIG-006、REC-MIG-004 | 输出 `5`；`cmd/admin init|recover-password` 对 v5 库操作无迁移报错 |

执行说明：

- 命令 3、4 必须使用 `env -u GOROOT` 前缀，否则复现「GOROOT 指向旧版本导致编译失败」。
- 命令 8 的 `npm run test:e2e` 自带 `npm run build`（见 web/package.json scripts），可替代命令 7 的构建验证，但命令 7 仍建议单独跑一次以隔离构建错误。
- 命令 9 若直接写 `--go go`，在本机 shell 环境会因 GOROOT 问题失败，故传 goenv 绝对路径并保留 `env -u GOROOT` 外层前缀。

---

## 三、E2E 覆盖方案

三个新模块的 E2E spec 已落盘并通过（2026-10-20 实测，三模块 21/21；全量回归仅既有 user-module ID-001 失败）。测试名约定：`test('<用例 ID> <标题摘要>', …)`，使 Playwright 报告能直接按用例 ID 定位。

### food-module.spec.js（计划）

| 用例 | 计划测试名 | 要点 |
| --- | --- | --- |
| FOOD-E2E-001 | `FOOD-E2E-001 主链路：新增→检索→停用→忌口保留` | 管理员新增食材、列表检索、停用，用户忌口保留 |
| FOOD-E2E-002 | `FOOD-E2E-002 未知信息明确显示` | 营养/过敏原未知显示「未知」而非 0 |
| FOOD-E2E-003 | `FOOD-E2E-003 并发修改提示重新加载` | 409 VERSION_CONFLICT 后表单保留并提示刷新 |
| FOOD-E2E-004 | `FOOD-E2E-004 编辑与启停确认可键盘操作` | 纯键盘完成编辑与启停确认 |
| FOOD-E2E-005 | `FOOD-E2E-005 资源审计页面联动` | ResourceAuditView 出现对应审计事件 |
| FOOD-E2E-006 | `FOOD-E2E-006 移动端视口可用` | 390×844 无横向溢出 |
| FOOD-E2E-007 | `FOOD-E2E-007 编辑对象切换的失败态与保存目标固定` | route 注入 500 后无残留、保存禁用；恢复后按 B 保存 |

### inventory-module.spec.js（计划）

| 用例 | 计划测试名 | 要点 |
| --- | --- | --- |
| INV-COMPAT-001 | `INV-COMPAT-001 DELETE body 经同源 fetch 透传` | 页面内 fetch 带 body DELETE → 204 |
| INV-E2E-001 | `INV-E2E-001 新增两批同食材不合并` | 两行独立批次 |
| INV-E2E-002 | `INV-E2E-002 筛选临期` | expiring 筛选与文字标签 |
| INV-E2E-003 | `INV-E2E-003 编辑批次` | 数量/备注/到期日保存成功，食材与单位锁定 |
| INV-E2E-004 | `INV-E2E-004 用完设零` | quantity=0 → depleted |
| INV-E2E-005 | `INV-E2E-005 删除批次` | 确认框含食材与数量；末项回退有效页 |
| INV-E2E-006 | `INV-E2E-006 用户 B 看不到 A 的批次` | 列表隔离 + 详情 URL 404 |
| INV-E2E-007 | `INV-E2E-007 管理员停用后按规则处理` | 减量可、增量 409 FOOD_DISABLED |
| INV-E2E-008 | `INV-E2E-008 两标签页编辑冲突` | 第二标签页 409、草稿保留 |
| INV-E2E-009 | `INV-E2E-009 移动端视口可用` | 390×844 无横向溢出 |
| INV-E2E-010 | `INV-E2E-010 删除版本冲突：列表刷新后保留失败提示` | 409 后提示可见、批次为最新值 |

### recipe-module.spec.js（计划）

| 用例 | 计划测试名 | 要点 |
| --- | --- | --- |
| REC-E2E-001 | `REC-E2E-001 公共食谱发布可见性闭环` | 草稿→发布→他人可见→归档不可见 |
| REC-E2E-002 | `REC-E2E-002 双用户私有食谱隔离` | B 不可见/不可达 A 的私有食谱 |
| REC-E2E-003 | `REC-E2E-003 私有食谱全生命周期` | 创建→编辑→发布→撤回→归档 |
| REC-E2E-004 | `REC-E2E-004 发布失败原因与冲突比较` | 不完整食材引用与 409 冲突提示 |
| REC-E2E-005 | `REC-E2E-005 列表检索/分页与移动端可用性` | 检索、翻页与 390×844 视口 |

### WEB-\* 类用例的覆盖取舍（重要）

FOOD-WEB-001~007、INV-WEB-001~017、REC-WEB-001~010 在用例文档中建议落点为 `web/src/views/*.test.js` 组件单测。**实际未实现任何组件单测**，取舍如下：

1. **项目约束：不加新依赖。** devDependencies 中无 `@vue/test-utils`，组件挂载式单测需要它；按约束不引入。
2. **组件交互行为（表单校验提示、按钮防连点、409/400/503 错误展示、删除确认、loading/空状态、状态标签等）统一由 E2E 覆盖**——即上面三份 module spec 的职责范围；E2E 用例设计时已把 WEB 组中的交互要点并入对应场景（如 INV-WEB-004 并入 INV-E2E-008、INV-WEB-009 并入 INV-E2E-005、INV-WEB-012 并入 INV-E2E-002）。
3. **纯函数逻辑已在后端 logic 测试覆盖，前端不重复测**：数量字符串正则（INV-LOGIC-001~018，`ParseQuantityMilli` 拒绝浮点/指数/空白路径，前端仅原样提交字符串）、码点计数（FOOD-LOGIC-010/019、INV-LOGIC-035/036、REC-LOGIC-003 等，按 Unicode 码点而非字节的后端校验已固化）。前端对这些规则只做「透传」，重复断言无增量价值。
4. 已有且保留的前端单测仅三类：web/src/api.test.js（刷新重试/身份代次，承载 FOOD-SEC-008）、auth.test.js、validation.test.js（密码规则）。WEB 组中与这些文件重叠的条目（FOOD-WEB-005）由既有用例承载，但无用例 ID 标注。

该取舍的代价：WEB 组用例没有一一对应的自动化断言，其覆盖依赖 E2E 场景的合并程度，属于已知折衷，不在本文档中掩饰。

---

## 四、执行顺序与失败处置

### 执行顺序

1. 后端静态：运行矩阵 #1、#2（build → vet）。
2. 后端单测/集成：#3（go test 全量）→ #4（-race）。
3. 前端单测与构建：#6 → #7。
4. E2E：#8（先确认三份 module spec 已落盘；首次执行需 `npx playwright install chromium`）。
5. CLI：#9、#10（admin CLI 打开 v5 库）。

先单元/集成后 E2E 的理由：E2E 依赖 18080 隔离服务与构建产物，底层失败时先修后端/前端，避免在 E2E 报告里排查底层问题。

### 失败定位

失败信息按用例 ID 前缀定位文件（与第一节总表一致）：

| 失败 ID 前缀 | 打开这个文件 |
| --- | --- |
| FOOD-LOGIC-\* | server/internal/logic/foods_test.go |
| INV-LOGIC-001~018 | server/internal/logic/quantity_test.go |
| INV-LOGIC-019~036 | server/internal/logic/inventory_test.go |
| REC-LOGIC-\* | server/internal/logic/recipes_test.go |
| FOOD-STORE-\* / REC-STORE-\* / INV-STORE-\* | server/internal/store/{foods,recipes,inventory}_test.go |
| FOOD-MIG-\* / 共享迁移 | server/internal/store/migrate_test.go |
| FOOD-API-\* / FOOD-SEC-\* | server/internal/api/foods_test.go |
| INV-API-\* / INV-SEC-\* | server/internal/api/inventory_test.go |
| REC-API-\* / REC-SEC-\* | server/internal/api/recipes_test.go |
| FOOD-SEC-008 / 前端刷新 | web/src/api.test.js |
| FOOD-E2E-\* / INV-E2E-\* / REC-E2E-\* / INV-COMPAT-001 | web/e2e/{food,inventory,recipe}-module.spec.js |
| CLI / 迁移版本 | scripts/verify_cli.py |

Go 测试失败信息本身带 `=== RUN TestXxx` 与文件行号，直接回溯即可；Playwright 失败先看 trace 与 `test('…')` 名称中的用例 ID，再按上表回到对应模块的 Go 用例确认契约层是否同样失败（E2E 失败而 Go 用例通过 → 前端问题；两者同失败 → 后端契约问题）。

### 结果回填约定

本文档只登记映射与计划，**不回填执行结果**。步骤 5 实际执行后，各命令的通过/失败、修复与复跑结论由步骤 6 的总结文档统一记录；总结文档按本表区段顺序罗列实际结果，并注明与第五节缺口清单的对照（哪些缺口在执行中被补齐、哪些保留）。

---

## 五、覆盖缺口如实记录

以下区段经 grep 实际测试文件确认**没有对应的测试函数或断言**，属缺口清单，供步骤 5/6 决定是否补齐或接受：

1. **INV-SEC-009（事务内撤销身份的 API 级断言）**：api/inventory_test.go 无对应函数；同一复核路径仅有 store 级 TestINVSTORE014_SessionRevokedMidTransaction 覆盖，foods/recipes 模块也无 API 级同类用例（FOOD-SEC-004/005 除外，那是 foods 自己的）。风险：中间件复核与库存写路径的组合行为未被端到端断言。
2. **REC-SEC-007（事务内降权/禁用复核，食谱写路径）**：recipes 相关测试中仅有将用户提升为管理员的 PATCH（用于夹具），未发现「写事务进行中降权/禁用 → 复核失败回滚」的用例。
3. **REC-SEC-011（food 过敏原不可被食谱移除）**：用例文档标注落点为 logic + api，grep 未发现显式断言（effective_allergens 在 TestRecipeAPI017_018 中有出现，但断言的是字段存在与附加过敏原，未覆盖「清空附加过敏原后 food 标注仍保留」的场景）。
4. **REC-SEC-013（未知营养不当作零）**：未发现「食谱引用缺营养食材时响应明确标注不可完整计算、不显示 0」的断言。
5. **REC-SEC-015（纯文本脚本不执行）Go 侧**：无 Go 断言；浏览器侧 CSP 由既有 user-module.spec.js SEC-004 的 `securitypolicyviolation` 模式覆盖。
6. **WEB 组整体无组件单测**：FOOD-WEB-001~007（7 条）、INV-WEB-001~017（17 条）、REC-WEB-001~010（10 条）共 34 条用例没有一一对应的自动化断言，按第三节取舍并入 E2E 场景；其中未被任何 E2E 场景显式合并的条目（如 INV-WEB-002 inputmode、INV-WEB-014 日期不转时区、REC-WEB-002/003 等）目前无自动化覆盖，依赖人工验证。
7. **MIG 类无模块专属标注**：INV-MIG-001~009、REC-MIG-001~003/005~006、REC-STORE-019 共 16 条用例没有以自身 ID 标注的测试，由共享迁移链测试（TestFoodMIG001/003/004/008/012，链路已到 v5 并校验 inventory/recipes 结构与索引）间接覆盖。行为有覆盖、ID 不可检索：`go test -run 'INV-MIG'` 不会命中任何测试。
8. **REC-API-015、REC-API-014 无独立测试函数**：分别由 TestRecipeAPI017_018/TestRecipeAPI001_002（契约形状）、共享 fixture 全局 no-store、TestFoodAPI018（503 模式）与 TestRecipeAPI010_013（64 KiB 双通道）间接覆盖，无用例 ID 标注。
9. ~~E2E spec 尚未落盘~~（已解决）：food/inventory/recipe-module.spec.js 均已落盘并全部通过。
10. **FOOD-MIG-011 / INV-MIG-006 / REC-MIG-004（CLI 类）**：无 Go 测试，仅由 scripts/verify_cli.py 承载；该脚本是黑盒验证，失败时不给出用例 ID 级定位。

除上述各项外，其余区段（FOOD-LOGIC/STORE/API/SEC 的 Go 部分、INV-LOGIC 全部、INV-STORE 全部、INV-API-001~037、INV-SEC-001~008/010~014、REC-LOGIC 全部、REC-STORE-001~018/020、REC-API 其余、REC-SEC-001~006/008/012/014/016）均有 ID 可检索的实际测试函数对应。
