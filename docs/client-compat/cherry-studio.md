# Cherry Studio 接入兼容性简报

<cite>
文档类型：客户端兼容性集成简报  
目标仓库：`e:/ds2api`（ds2api — DeepSeek-web-to-API 代理）  
Cherry Studio 版本参考：`CherryHQ/cherry-studio` main 分支（2026-05 调研时点，最新稳定版 v1.9.x）  
调研方法：GitHub 源码精读（`src/renderer/src/aiCore/`、`packages/`）+ 已知 Issues 审计  
ds2api 版本变更：v1.0.8 会话自动删除统一修复 · v1.0.10 严格模型 allowlist（**Cherry Studio 用户可能受影响**） · v1.0.12 429 弹性故障转移  
</cite>

---

## 简介

Cherry Studio 是一款基于 Electron 的桌面多模型 AI 客户端（CherryHQ/cherry-studio，前身 kangfenmao/cherry-studio），支持 Windows / macOS / Linux，内置 300+ 助手预设，并可通过自定义 Provider 接入任意 OpenAI 兼容端点。其底层通信层使用 Vercel AI SDK（`ai` 包 + `@ai-sdk/*`），通过中间件/插件体系统一封装各家 API 差异，然后向上层 UI 暴露统一的 `Chunk` 流。

本文档面向 ds2api 开发者，梳理 Cherry Studio 的请求形态与已知兼容性缺陷，帮助确认代理端需要实现哪些细节才能让 Cherry Studio 用户"一填即用"。

---

## 项目结构

```
CherryHQ/cherry-studio
├── src/renderer/src/aiCore/          # 核心 AI 通信层（Renderer 进程）
│   ├── AiProvider.ts                 # 顶层入口，封装 createExecutor
│   ├── prepareParams/
│   │   ├── parameterBuilder.ts       # 组装 StreamTextParams（温度/maxTokens/topP/工具）
│   │   ├── messageConverter.ts       # 消息格式转换（Cherry→AI SDK）
│   │   ├── fileProcessor.ts          # 文件/图片处理（base64 / fileid:// / 文本提取）
│   │   ├── modelParameters.ts        # temperature / topP / maxTokens 策略
│   │   └── header.ts                 # Anthropic beta 请求头
│   ├── provider/
│   │   ├── providerConfig.ts         # URL 格式化 + SDK 配置构建（含 formatProviderApiHost）
│   │   └── factory.ts                # getAiSdkProviderId 映射
│   ├── plugins/
│   │   ├── deepseekDsmlParserPlugin.ts  # DeepSeek DSML 工具调用格式解析
│   │   ├── anthropicCachePlugin.ts      # Anthropic 提示词缓存中间件
│   │   └── ...                          # Gemini thinking / noThink / reasoning 等
│   └── utils/
│       ├── options.ts                # buildProviderOptions（按 provider 类型分支）
│       └── mcp.ts                    # MCP 工具→AI SDK tools 转换
├── src/main/apiServer/               # 内置 API Server（供本机其他应用调用）
└── packages/shared/config/providers.ts  # Silicon / Anthropic 兼容模型白名单
```

---

## 核心组件

### 1. Provider 类型体系

Cherry Studio 定义了以下 `ProviderType`（`src/renderer/src/types/provider.ts`）：

```typescript
z.enum([
  'openai',           // 标准 OpenAI + 所有自定义 OpenAI 兼容端点
  'openai-response',  // OpenAI Responses API（o 系列新接口）
  'anthropic',        // Anthropic 原生 Messages API
  'gemini',           // Google Gemini 原生 API（v1beta）
  'azure-openai',
  'vertexai',
  'mistral',
  'aws-bedrock',
  'vertex-anthropic',
  'new-api',
  // ... 其他
])
```

**DeepSeek 官方 Provider** 的 `id = 'deepseek'`，但其 `type = 'openai'`，意味着它走完整的 OpenAI 兼容路径（`buildOpenAICompatibleConfig`）。用户也可以自建"Custom Provider"，只要 `type = 'openai'`，即走同样路径。

**结论**：ds2api 暴露 `/v1/chat/completions` 端点、声称 OpenAI 兼容即可对接 Cherry Studio 的 DeepSeek profile 以及所有自定义 OpenAI provider。

---

### 2. URL 格式化规则（`formatProviderApiHost`）

这是兼容性最容易出问题的地方。Cherry Studio 在发送请求前会自动对 `apiHost` 做格式化：

| Provider 类型 | 格式化逻辑 | 举例 |
|---|---|---|
| `openai`（含 DeepSeek、自定义） | 末尾自动追加 `/v1`（除非 URL 以 `#` 结尾） | `http://localhost:8080` → `http://localhost:8080/v1` |
| `gemini` | 末尾追加 `/v1beta` | 同理 |
| `anthropic` | 使用 `anthropicApiHost`，格式化后同步到 `apiHost` | `https://api.anthropic.com` → `https://api.anthropic.com/v1` |
| `ollama` | 特殊格式化，追加 `/v1` | |
| Azure / Copilot / Perplexity | 不追加版本后缀（`appendApiVersion = false`） | |

**关键结论**：用户在 Cherry Studio 中填写 `http://your-ds2api-host:8080`，Cherry Studio 会自动变为 `http://your-ds2api-host:8080/v1`，然后向 `/v1/chat/completions` 发请求。**ds2api 的路由前缀必须是 `/v1`**，否则会收到 404。

**已知 Bug（Issue #13192）**：Cherry Studio 内置 API Server 模式（供本机调用）的代码路径不经过 `formatProviderApiHost`，导致 Ollama 等非 `https://api.openai.com` 端点被发到 `/chat/completions` 而非 `/v1/chat/completions`。该 Bug 在 Renderer 侧不存在，但若 ds2api 想支持 Cherry Studio API Server 模式的转发，需兼容 `/chat/completions`（无 `/v1` 前缀）。

---

### 3. 默认请求格式（OpenAI 兼容路径）

Cherry Studio 通过 Vercel AI SDK 的 `openai-compatible` provider 发送请求，结构如下：

```json
POST /v1/chat/completions
Authorization: Bearer <apiKey>
Content-Type: application/json
X-Cherry-Studio-Version: <app_version>

{
  "model": "deepseek-chat",
  "messages": [
    { "role": "system", "content": "你是一个助手..." },
    { "role": "user",   "content": "你好" },
    { "role": "assistant", "content": "你好！有什么可以帮你？" },
    { "role": "user",   "content": [
        { "type": "text", "text": "帮我分析这张图片" },
        { "type": "image_url", "image_url": { "url": "data:image/png;base64,iVBORw..." } }
    ]}
  ],
  "stream": true,
  "temperature": 0.7,
  "max_tokens": 2048,
  "top_p": 1,
  "stream_options": { "include_usage": true }
}
```

**逐字段说明**：

- `stream`：Cherry Studio 默认启用流式，`stream: true` 几乎总是存在。非流式仅在特殊场景（如标题生成）使用。
- `stream_options.include_usage`：**关键字段**，Cherry Studio 对 `openai-compatible` 类型 provider 默认发送此字段（通过 `isSupportStreamOptionsProvider` 控制，仅 `mistral` 及用户手动设置 `isNotSupportStreamOptions: true` 的 provider 会跳过）。ds2api 必须接受此字段，否则返回 400 会导致整个请求失败（Issue #11652）。
- `temperature` / `top_p` / `max_tokens`：由 `modelParameters.ts` 决定是否发送。若用户未开启对应滑块（`enableTemperature = false`），这些字段不出现在请求中。ds2api 应对缺失字段使用合理默认值。
- `messages`：完整历史消息数组，每轮对话追加，**不做截断**（截断逻辑在 LLM 侧）。多模态消息时 `content` 为数组，每个元素是 `{ type, text | image_url }` part。

---

### 4. Streaming SSE 消费行为

Cherry Studio 使用 AI SDK 的 `fullStream`（`ReadableStream<TextStreamPart>`）消费 SSE，通过 `AiSdkToChunkAdapter` 转为内部 `Chunk` 格式。实际发到网络层的期望如下：

```
data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1700000000,"model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"你好"},"finish_reason":null}]}\n\n

data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1700000000,"model":"deepseek-chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}\n\n

data: [DONE]\n\n
```

**Cherry Studio 对 SSE 的期望/容忍细节**：

1. **必须以 `data: [DONE]\n\n` 结束**：AI SDK 依赖此信号判断流结束，缺失会导致流挂起或超时。
2. **`usage` 字段位置**：当 `stream_options.include_usage: true` 时，Cherry Studio 期望 `usage` 出现在**最后一个 non-DONE chunk**（含 `finish_reason: "stop"` 的那个块）里。若 ds2api 在中间块里返回 `usage` 或根本不返回，不会报错，但界面上的 token 统计会为零。
3. **`id` 字段**：每个 chunk 应有 `id`，但 Cherry Studio 并不做强校验，缺失不会崩溃。
4. **`choices[0].delta.content`**：流式增量内容。Cherry Studio 也支持 `choices[0].delta.reasoning_content`（DeepSeek R1 思考链字段），会被渲染为折叠的"思考过程"块。
5. **空内容块**：Cherry Studio 会跳过 `delta.content === ""`，不需要过滤。

---

### 5. System Prompt / Temperature / Max Tokens

Cherry Studio 在助手（Assistant）维度存储以下设置，每次请求时写入对应字段：

| UI 设置 | 请求字段 | 备注 |
|---|---|---|
| 系统提示词（Prompt） | `messages[0].role = "system"` | 多轮对话每次均携带 |
| 温度开关 + 滑块 | `temperature`（0~2，部分模型钳制到 1） | 开关关闭时**不发送** |
| Top-P 开关 + 滑块 | `top_p` | 与 temperature 互斥时后者优先 |
| Max Output Tokens 开关 + 数值 | `max_tokens` | 开关关闭时**不发送**，由上游默认 |
| 思考强度（Reasoning Effort） | `provider.options.reasoning_effort` 或 `thinking.budget_tokens` | Claude / Gemini / DeepSeek R1 差异较大 |
| 工具调用上限 | 通过 AI SDK `stepCountIs(n)` 控制，不直接映射到请求字段 | |

---

### 6. 工具调用（Tool Calling / MCP）

Cherry Studio 通过 MCP（Model Context Protocol）集成外部工具，在底层通过 `convertMcpToolsToAiSdkTools` 将 MCP 工具定义转换为 AI SDK `ToolSet`，再由 AI SDK 序列化为 OpenAI 标准 `tools` 数组：

```json
{
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "weather__get_current_weather",
        "description": "Get current weather for a location",
        "parameters": {
          "type": "object",
          "properties": {
            "location": { "type": "string" }
          },
          "required": ["location"]
        }
      }
    }
  ],
  "tool_choice": "auto"
}
```

**Cherry Studio 的工具调用架构是"客户端执行"**：

1. Cherry Studio 向 API 发送包含 `tools` 的请求。
2. 上游 LLM 返回 `tool_calls` chunk（`delta.tool_calls[].function`）。
3. Cherry Studio 在**本地**调用对应 MCP 服务器执行工具。
4. 将工具结果作为 `role: "tool"` 消息追加，再发起下一轮请求。

**对 ds2api 的影响**：ds2api 代理层需要能**透传** `tools` 数组到 DeepSeek，并在流式响应里正确输出 `delta.tool_calls`，包括：
- `delta.tool_calls[0].index`
- `delta.tool_calls[0].id`（如 `call_abc123`）
- `delta.tool_calls[0].type: "function"`
- `delta.tool_calls[0].function.name`（**首块** 出现）
- `delta.tool_calls[0].function.arguments`（**增量** JSON 片段，跨多块）

工具名称会带有 MCP 服务器前缀（如 `weather__get_current_weather`），ds2api 无需解析，透传即可。

**已知 Bug（Issue #11404）**：当模型同时启用 Extended Thinking（`reasoning_effort: "high"`）且进行 MCP 工具调用时，Cherry Studio 发送的 assistant 消息中 `content` 数组顺序错误——文本块出现在思考块之前，违反 Anthropic "thinking block must come first" 约束，导致 Anthropic 端报错。**这是 Cherry Studio 自身的 Bug，ds2api 的 Anthropic 适配层（`internal/httpapi/claude/`）需留意此类畸形消息**，可在 `handler_messages.go` 或 `promptcompat/` 中做 reorder 修复。

---

### 7. 文件与图片附件

#### 图片（Vision）

视觉模型（`isVisionModel(model) === true`）：Cherry Studio 将图片读取为 base64，通过 AI SDK 的 `{ type: "image", image: "<base64>", mediaType: "image/png" }` Part 传递。AI SDK 最终序列化为：

```json
{ "type": "image_url", "image_url": { "url": "data:image/png;base64,iVBORw..." } }
```

远程图片 URL 则直接传 `{ "url": "https://..." }`。

**已知 Bug（Issue #12602）**：当 AI 的上一轮回复包含 base64 图片 Markdown（`![image](data:image/jpeg;base64,...)`），下一轮请求会把该 base64 字符串完整打包进 `messages` 数组，导致请求体超过服务端 payload 限制（HTTP 413）。ds2api 已有 `internal/httpapi/openai/chat/handler.go` 等入口，建议在消息预处理层（参考 `messageConverter.ts` 的 `stripMarkdownBase64Images`）对 assistant 消息内的内嵌 base64 图片做截断或替换。

#### 文件

- **文本/文档文件**：提取为纯文本，拼入对应消息 `content` 的 `{ type: "text", text: "filename\ncontent" }` Part。
- **大文件（OpenAI Files API）**：调用 `/v1/files` 上传后，以 `fileid://FILE_ID` 形式传入 `data` 字段，由 SDK 转换为 OpenAI 的 `file` Part。**ds2api 需实现 `/v1/files` 上传端点**（`internal/httpapi/openai/` 下已有 `files/` 目录）。
- **Gemini 大文件**：走 Gemini Files API 上传，返回 `fileUri`，对 OpenAI 兼容路径无影响。

---

### 8. 多模态与思考模式

| 功能 | 触发条件 | 请求字段 |
|---|---|---|
| Vision | `isVisionModel(model) && 用户上传图片` | `content` 数组含 `image_url` Part |
| DeepSeek R1 思考链 | `isFixedReasoningModel(model)`（R1 系列） | 无额外字段；回复流含 `reasoning_content` 增量 |
| DeepSeek V3 thinking toggle | 用户设置 `reasoning_effort` | `providerOptions.deepseek.reasoningEffort` 经 AI SDK 序列化 |
| Claude Extended Thinking | `isSupportedThinkingTokenClaudeModel(model)` + `reasoning_effort != none` | `anthropic-beta: interleaved-thinking-2025-05-14` header；`thinking.budget_tokens` |
| Gemini Thinking | `isGeminiModel` + reasoning 开关 | `providerOptions.google.thinkingConfig` |
| Qwen3 Thinking | `enable_thinking` provider option | 通过 `providerOptions.openai-compatible.enable_thinking` 透传 |

对于 ds2api（OpenAI 兼容 + DeepSeek 后端）：
- DeepSeek R1/V3 的思考链通过 `delta.reasoning_content` 增量返回，Cherry Studio 能正确渲染。
- Claude / Gemini 思考模式只在用户选择对应 provider（`type = anthropic / gemini`）时才激活，与 OpenAI 路径无关。

---

## 架构总览

```
Cherry Studio UI
       │
       ▼
AiProvider.completions()          ← src/renderer/src/aiCore/AiProvider.ts
       │
       ├── buildStreamTextParams()   ← prepareParams/parameterBuilder.ts
       │     ├── getMaxTokens / getTemperature / getTopP
       │     ├── setupToolsConfig (MCP → ToolSet)
       │     └── buildProviderOptions (按 provider 分支)
       │
       ├── providerToAiSdkConfig()   ← provider/providerConfig.ts
       │     ├── formatProviderApiHost()  （自动追加 /v1 或 /v1beta）
       │     └── buildOpenAICompatibleConfig()
       │           └── isSupportStreamOptionsProvider → include stream_options
       │
       └── createExecutor() [AI SDK]
             │
             ▼
   POST /v1/chat/completions        ← ds2api 端点
   Headers: Authorization, X-Cherry-Studio-Version, (anthropic-beta for Claude)
   Body: { model, messages, stream, temperature?, max_tokens?, top_p?,
           stream_options?, tools?, tool_choice? }
             │
             ▼
   SSE stream → AiSdkToChunkAdapter → Cherry Studio UI chunks
```

---

## 详细组件分析

### 9. Anthropic 原生路径

当用户配置 `type = anthropic` 的 Provider，Cherry Studio 走 `@ai-sdk/anthropic` SDK，发送到 `anthropicApiHost`（默认 `https://api.anthropic.com/v1`），使用 `/v1/messages` 接口，格式：

```json
POST /v1/messages
x-api-key: <key>
anthropic-version: 2023-06-01
anthropic-beta: interleaved-thinking-2025-05-14   (如有 extended thinking)
Content-Type: application/json

{
  "model": "claude-sonnet-4-5",
  "messages": [...],
  "system": "...",
  "stream": true,
  "max_tokens": 8192
}
```

ds2api 的 `internal/httpapi/claude/` 需处理此格式。Cherry Studio 还支持 Anthropic OAuth（`authType = oauth`），此时 Bearer token 来自 OAuth 流程，而非 API Key。

### 10. Gemini 原生路径

`type = gemini` 的 Provider 走 `@ai-sdk/google`，发往 `https://generativelanguage.googleapis.com/v1beta`，使用 `:streamGenerateContent` 接口。Cherry Studio 的 URL 格式化会对 `gemini` 类型 provider 追加 `/v1beta`（而非 `/v1`）。

**已知 Bug（Issue #11541）**：v1.7.0 版本起，使用 Cloudflare AI Gateway 等自定义 Gemini 端点时，`/v1beta` 路径段被意外剥离，导致请求路径错误（404）。该 Bug 已通过 PR #11543 修复，但部分旧版用户可能仍受影响。ds2api 的 `internal/httpapi/gemini/` 若要支持，需确保路由正确识别 `/v1beta/` 前缀。

---

## ds2api 版本影响说明

| ds2api 版本 | 对 Cherry Studio 的影响 |
|------------|----------------------|
| **v1.0.8** | `/v1/chat/completions` handler 接入 `AutoDeleteRemoteSession`。若 WebUI 开启"结束后全部删除"，Cherry Studio 发起的请求结束后正确清理远程会话。通常情况下用户无感知。 |
| **v1.0.10** | **可能的破坏性变更**：v1.0.10 移除了模型族前缀启发式兜底。若 Cherry Studio 用户在 Provider 设置里填写的 Model ID 不在 `DefaultModelAliases`（约 100 个 OpenAI/Claude/Gemini 常见 id）中，请求会返回 4xx 并提示"strict allowlist"。**操作**：在 ds2api WebUI Settings → Model Aliases 中添加从客户端 id 到实际 DeepSeek 模型的显式映射，配置热重载无需重启服务器。 |
| **v1.0.12** | 上游 429 触发账号切换（不消耗重试配额），Cherry Studio 在多任务并发时 429 暴露率显著降低。 |

## 已知兼容性问题（Issues 清单）

| # | Issue | 影响 | ds2api 对策 |
|---|---|---|---|
| **#11652** | `stream_options.include_usage` 被 Cherry Studio 默认发送，不支持此参数的代理返回 400 | **高** — 所有自定义 OpenAI 兼容 provider 的流式请求均受影响 | ds2api 在 `handler_chat.go` / `chat_stream_runtime.go` 中**忽略/接受** `stream_options` 字段，不因此报错 |
| **#13192** | 内置 API Server 路径不格式化 `apiHost`，请求发到 `/chat/completions` 而非 `/v1/chat/completions` | 中 — 仅影响通过 Cherry Studio API Server 中转的场景 | ds2api 路由层同时注册 `/chat/completions` 和 `/v1/chat/completions` 两条路径作为别名 |
| **#11541** | Gemini 自定义端点 `/v1beta` 路径段被剥离（v1.7.0，已修复） | 低 — 已修复，影响未升级用户 | `internal/httpapi/gemini/` 路由应同时兼容有无 `/v1beta/` 前缀 |
| **#11404** | Extended Thinking + MCP 工具调用时，assistant 消息 content 块顺序错误（文本块先于思考块） | 中 — 仅 Anthropic 侧受影响 | `internal/httpapi/claude/handler_messages.go` 或 `promptcompat/` 中对 assistant 消息做 thinking-block reorder |
| **#12602** | 多轮对话中 assistant 回复含 base64 图片 Markdown，下一轮请求体超限（HTTP 413） | 中 — 图片生成场景 | 在 OpenAI chat handler 的消息预处理中，对 assistant 消息内 `data:image/...;base64,...` 做截断/移除 |
| **v1.0.10 allowlist** | 用户填写了非 DefaultModelAliases 中的自定义模型 id | 中 — 受影响用户请求返回 4xx | 在 ds2api WebUI Settings → Model Aliases 添加从 Cherry Studio model id 到实际模型的映射 |

---

## ds2api 适配检查清单

以下每一项对应一个具体需要确认或实现的代码位置：

### A. `internal/httpapi/openai/chat/`

- [ ] **`handler_chat.go`**：接收 `stream_options` 字段时不报 400，可选择透传或静默忽略（对应 Issue #11652）
- [ ] **`handler_chat.go` / `chat_stream_runtime.go`**：SSE 输出在最终 chunk（`finish_reason: "stop"`）携带 `usage`（`prompt_tokens`、`completion_tokens`、`total_tokens`），并以 `data: [DONE]\n\n` 结束流
- [ ] **`handler_chat.go`**：支持 `messages[].content` 为数组形式（`[{ type: "text" }, { type: "image_url" }]`）的多模态请求
- [ ] **`handler_chat.go`**：`temperature`、`top_p`、`max_tokens` 字段缺失时使用合理默认值，不报 422
- [ ] **`chat_stream_runtime.go`**：工具调用场景下，`delta.tool_calls[0]` 的流式增量格式正确（`index`、`id`、`type`、`function.name`、`function.arguments` 分块累积）
- [ ] **路由别名**：在路由注册处同时挂载 `/chat/completions`（无 `/v1` 前缀）作为 `/v1/chat/completions` 的别名（对应 Issue #13192）

### B. `internal/httpapi/claude/`

- [ ] **`handler_messages.go`**：处理 assistant 消息中 content 块顺序错误（thinking block 不在首位）时做自动修复 reorder
- [ ] **`handler_messages.go`**：支持 `anthropic-beta: interleaved-thinking-2025-05-14` 请求头透传
- [ ] **`handler_routes.go`**：确认 `/v1/messages` 路由正确（Anthropic 的 `apiHost` 格式化后也加 `/v1`）

### C. `internal/httpapi/gemini/`

- [ ] **`handler_routes.go`** / `convert_request.go`：路由同时支持 `/v1beta/models/{model}:streamGenerateContent` 和 `/models/{model}:streamGenerateContent`（对应 Issue #11541，兼容旧版 Cherry Studio）

### D. `internal/promptcompat/`

- [ ] **`message_normalize.go`**：对多轮对话历史中 assistant 消息内嵌的 `data:image/...;base64,...` Markdown 图片做截断，防止 413（对应 Issue #12602）
- [ ] **`tool_message_repair.go`**（如已存在）：处理 MCP 工具调用后 tool 消息的格式合规性检查

### E. 通用（`internal/httpapi/openai/shared/` 或 `requestbody/`）

- [ ] **请求体大小限制**：建议 ≥ 50 MB（base64 图片场景），或在中间件层提前剥离 base64 图片
- [ ] **`model` 字段前缀透传**：Cherry Studio 发送的 `model` 值是用户在 UI 里填写的原始字符串（如 `deepseek-chat`、`deepseek-reasoner`），ds2api 不应强制改写

---

## 结论

Cherry Studio 对 OpenAI 兼容端点的接入相对标准，核心差异点集中在：

1. **`stream_options.include_usage` 默认开启** — ds2api 必须接受此字段（忽略或透传均可），并在最终 chunk 返回 `usage`。
2. **URL 自动追加 `/v1`** — 用户填写裸域名即可，ds2api 路由需以 `/v1/` 为前缀，同时提供 `/chat/completions` 别名兼容内置 API Server 模式。
3. **多模态 `content` 数组** — 图片以 `data:image/...;base64,...` 内嵌，历史消息中可能出现超大 payload。
4. **工具调用透传** — MCP 工具由 Cherry Studio 客户端执行，ds2api 仅需正确透传 `tools` 定义和 `delta.tool_calls` 增量流。
5. **SSE 终止信号** — 必须发送 `data: [DONE]\n\n`，否则流不会正常关闭。

完成上述检查清单中标注的改动后，Cherry Studio 用户只需在设置中添加一个 `type = openai`、`apiHost = http://<ds2api-host>` 的自定义 Provider，即可完整使用聊天、工具调用、多模态等功能。

---

*章节来源：CherryHQ/cherry-studio GitHub 源码精读（2026-05）；Issues #11652 / #13192 / #11541 / #11404 / #12602；Cherry Studio 官方文档 docs.cherry-ai.com*
