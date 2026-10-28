# AI 接入层

`server/internal/ai` 提供文本生成接口 `Generator`，使用兼容 Chat Completions 的 HTTP 协议。支持 `openai`、`deepseek`、`glm` 和 `minimax` 的默认地址，亦可传入其他兼容服务的 HTTPS 基础地址。由调用方通过 `ai.New(provider, baseURL, model, key)` 显式提供模型和 API 密钥；空 `baseURL` 使用默认地址。调用方应从环境或密钥管理系统取得密钥，不要提交到仓库。实际模型名称、可用性和服务地址以供应商文档为准。

该接入层目前没有接入 HTTP 路由、页面或服务启动配置，不会主动调用模型；请求尚未设置生成 token 上限。接入生成食谱时需按供应商和模型增加预算与用量限制、处理模型可能输出的推理内容，并在展示前重新校验全部食材与过敏原。AI 不负责安全判定、库存扣减或营养计算。不同服务的模型参数、限额和可用性可能不同；当前仅支持非流式文本输入输出。

参考：[OpenAI Chat Completions](https://platform.openai.com/docs/api-reference/chat/create)、[DeepSeek API](https://api-docs.deepseek.com/quick_start/agent_integrations/)、[智谱 API](https://docs.bigmodel.cn/)、[MiniMax Chat Completions](https://platform.minimax.io/docs/api-reference/text-chat-openai)。
