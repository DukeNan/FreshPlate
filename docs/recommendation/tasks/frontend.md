# 推荐前端实现记录

## 范围与接口

- 独占 `web/src`，不修改后端、依赖、浏览器测试。
- `GET /api/recommendations?q=&availability=all|can_cook&page=&page_size=20`，复用 api 包装与身份代次。
- 数据结构严格使用设计 §4 的 `items/total/stats`，推荐快照的配方版本使用 `recipe.version`。

## 要求对应

| 设计 / 验收 | 实现 |
| --- | --- |
| §6 推荐入口与筛选分页 | router/App 增加 `/recommendations`；RecommendationsView 使用服务端 availability、总数与分页，条件修改回第 1 页 |
| §6 三种互斥空态、交叉筛选 | recommendations.js 基于 candidates、安全过滤计数和 total 判定；不依据 items 是否为空 |
| §6 无库存独立横幅 | 使用同次响应 has_usable_inventory，横幅与空态/正常列表叠加 |
| §6 缺料摘要与提示 | ok/short/missing/unknown 状态明确文字，short 使用 lack、missing 使用 need、unknown 不将 null 当零 |
| §6 详情复用与版本校验 | 点击记录单份内存快照，详情一次性取用；版本不一致整体返回 null，显示中性刷新提示 |
| §6 身份隔离、无持久化 | auth.generation 同步 watch 清空快照；列表请求序列、身份代次、离开页面失效处理；未写浏览器存储 |
| §6 错误/加载/防连点 | 共用 api 的 401/403/503/网络错误文案；加载期禁用输入与操作；失败清空旧推荐并提供重试 |

## 测试

新增 `web/src/recommendations.test.js`，覆盖全部空态组合、总数非零但空页时不误报、无库存不替代列表、三种安全过滤计数、快照单次取用、身份切换立即清空、配方版本整体丢弃以及四态数量摘要。

- `npm test --prefix web`：通过，5 文件 / 76 用例。
- `npm run build --prefix web`：通过。
- 浏览器交互、跨账号与详情修改场景由独立验证 Agent 执行，此处不将未执行的浏览器验证计为通过。

## 决策与限制

- 无新增依赖，无改动 API、库存、偏好或食谱管理行为。
- 快照仅用于当前标签页普通点击进入详情，刷新或新标签直接打开详情不展示匹配提示。
- 所有摘要只是推荐加载时点快照，版本校验保证配方一致性，不保证当前库存或偏好未变。
