# OpenCode 兼容性工程简报

<!-- cite: sst/opencode — https://github.com/sst/opencode -->
<!-- cite: OpenCode 官方文档 — https://opencode.ai/docs -->

## 简介

OpenCode（`sst/opencode`）是一款开源 AI 编程 Agent，以终端 TUI 形式运行，采用 **客户端/服务端分离架构**（`opencode serve` + TUI/Desktop/Web 客户端），通过 Vercel AI SDK（`@ai-sdk/*`）与各大 LLM 提供商通信。本文档分析其调用行为，为将 **ds2api** 配置为 OpenCode 兼容的 Drop-in 替换提供工程依据。

## 项目结构（参考）

```
sst/opencode/
├── packages/opencode/      # TypeScript 主体
│   ├── src/provider/       # 提供商抽象层（@ai-sdk 包装）
│   ├── src/session/        # 会话/历史/编排（SessionPrompt.loop）
│   ├── src/tool/           # 内置 Tool 注册表（ToolRegistry）
│   └── src/app/            # HTTP/SSE 服务端
└── opencode.json           # 项目级配置（可选）
```

## 核心组件

### 1. 提供商发现机制（Provider Model）

OpenCode 通过 `opencode.json` 中的 `provider` 字段声明提供商，配置层级从低到高依次覆盖：

```
远端默认配置 → ~/.config/opencode/opencode.json（全局） → $OPENCODE_CONFIG（自定义路径） → 项目 opencode.json
```

**标准提供商**直接使用 `@ai-sdk/anthropic`、`@ai-sdk/openai`、`@ai-sdk/google` 等专用包，通过同名环境变量鉴权：

| 提供商 | 环境变量 |
|--------|---------|
| Anthropic | `ANTHROPIC_API_KEY` |
| OpenAI | `OPENAI_API_KEY` |
| Google | `GOOGLE_GENERATIVE_AI_API_KEY` |
| DeepSeek | `DEEPSEEK_API_KEY` |
| Groq | `GROQ_API_KEY` |

**自定义兼容端点**使用 `@ai-sdk/openai-compatible`：

```json
{
  "$schema": "https://opencode.ai/config.json",
  "model": "ds2api/deepseek-r1",
  "provider": {
    "ds2api": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "ds2api (DeepSeek Web Proxy)",
      "options": {
        "baseURL": "http://localhost:8080/v1",
        "apiKey": "{env:DS2API_KEY}"
      },
      "models": {
        "deepseek-r1": { "name": "DeepSeek R1 (via ds2api)" },
        "deepseek-v3": { "name": "DeepSeek V3 (via ds2api)" }
      }
    }
  }
}
```

**对 Anthropic 端点的 baseURL 覆盖**方式为：

```json
{
  "provider": {
    "anthropic": {
      "options": {
        "baseURL": "http://localhost:8080/v1"
      }
    }
  }
}
```

> **已知问题**：GitHub Issue #5674 指出，使用 `@ai-sdk/openai-compatible` 时，`options`（包括 `baseURL` 和 `apiKey`）存在**未被转发到实际 API 调用**的 Bug，现象为 log 中 `"params={"options":{}}"` 为空对象。这是 OpenCode 当前版本（截至 2025 年底）的重要缺陷，若用户反馈连接失败，应首先排查此问题。作为 workaround，可将 `baseURL` 写为环境变量或通过 `OPENAI_BASE_URL` 环境变量注入。

### 2. 模型 ID 格式

OpenCode 使用 `provider_id/model_id` 复合格式：

```
anthropic/claude-opus-4-5      # 原生 Anthropic 提供商
ds2api/deepseek-r1             # 自定义提供商
opencode/gpt-5.1-codex         # OpenCode Zen 服务
```

模型 ID 必须与提供商 API 实际接受的 ID **精确匹配**，大小写敏感。

### 3. 鉴权方式

- API 密钥存储于 `~/.local/share/opencode/auth.json`（通过 `/connect` 命令写入）
- 配置中可使用 `{env:VAR_NAME}` 语法引用环境变量
- Anthropic SDK 使用 `x-api-key` header；OpenAI SDK 使用 `Authorization: Bearer` header
- 支持的额外 header 写法：

```json
{
  "options": {
    "headers": {
      "x-custom-header": "value"
    }
  }
}
```

## 架构总览

```
用户 TTY / Desktop App
        │
        ▼
  OpenCode TUI Client
        │ HTTP/SSE
        ▼
  opencode serve (本地 HTTP 服务器)
        │
        ▼
  SessionPrompt.loop()
  ├── ToolRegistry（权限过滤）
  ├── SystemPrompt（环境上下文注入）
  ├── ProviderTransform（消息格式归一化）
  └── Vercel AI SDK streamText()
              │
              ▼
       LLM Provider API（Anthropic / OpenAI / 自定义）
```

会话状态通过 **Drizzle ORM 持久化到本地 SQLite** 数据库；每轮调用时，OpenCode **将完整历史消息重播给 LLM**，不依赖服务端会话状态。这意味着 ds2api 不需要实现任何跨轮会话存储，只需无状态地处理每次独立请求。

## 详细组件分析

### 4. 工具调用格式（Tool Calling）

OpenCode 通过 Vercel AI SDK 的抽象层发送工具调用，底层格式因提供商而异：

- **Anthropic 端（`@ai-sdk/anthropic`）**：使用 Anthropic 原生 `tool_use` / `tool_result` blocks，发往 `/v1/messages`
- **OpenAI 端（`@ai-sdk/openai`、`@ai-sdk/openai-compatible`）**：使用 OpenAI `tools` + `function_call` / `tool_calls` 格式，发往 `/v1/chat/completions`
- **工具描述注入**：OpenCode **不**将工具定义写入 system prompt——而是完全依赖提供商原生的 tool schema 机制（Anthropic `tools` 数组 / OpenAI `tools` 数组）

工具流式传输（tool streaming）在 Anthropic 提供商下**默认开启**；对非 Anthropic 提供商则**默认关闭**。

**特殊参数**：对具有推理能力的模型，OpenCode 会在请求中附加 `reasoningSummary` 字段，该参数不属于标准 Chat Completions API。如果 ds2api 收到此字段，应直接忽略，避免 400 错误。

**工具名称容错**：OpenCode 实现了 `experimental_repairToolCall`，会自动修正大小写不匹配的工具名（如 `Read_file` → `read`）；无法修复的调用会路由至 `invalid` 工具让模型自我纠正。

**MCP 集成**：OpenCode 实现了 Model Context Protocol，可通过 `mcp` 配置字段挂载外部 MCP 服务器。MCP 工具与内置工具同等对待，均通过提供商原生 tool schema 传递，不需要 ds2api 特殊处理。

### 5. 内置工具列表

OpenCode 内置 **13 个工具**（以下为发送给 LLM 时的 `name` 字段）：

| 工具名 | 功能 | 权限分类 |
|--------|------|---------|
| `bash` | 执行 Shell 命令 | 受控（`ask`/`allow`/`deny`） |
| `edit` | 字符串替换式文件修改 | `edit` 权限 |
| `write` | 创建或覆写文件 | `edit` 权限 |
| `read` | 读取文件内容（分页） | 默认允许 |
| `grep` | ripgrep 正则搜索 | 默认允许 |
| `glob` | 文件名模式匹配 | 默认允许 |
| `lsp` | LSP 代码智能（实验性） | 需开启 experimental |
| `apply_patch` | 应用 patch 文件 | `edit` 权限 |
| `skill` | 加载技能文档 | 默认允许 |
| `todowrite` | 管理 todo 列表 | 默认允许 |
| `webfetch` | 抓取网页内容 | 默认允许 |
| `websearch` | Web 搜索（需 `OPENCODE_ENABLE_EXA=true`） | 需环境变量 |
| `question` | 向用户提问 | 默认允许 |
| `task` | 创建子 Agent 会话 | 继承父权限 |

这些工具名会出现在 `tools` 数组的 `name` 字段中，ds2api 转发给 DeepSeek 时通过 system prompt 注入工具描述（现有 `toolcall/tool_prompt.go` 机制已覆盖此场景）。

### 6. 流式传输（Streaming）

OpenCode 通过 Vercel AI SDK 的 `streamText()` 驱动流式输出，底层 SSE 格式因提供商不同而异：

**Anthropic 原生流**（发往 `/v1/messages`，`stream: true`）：

```
event: message_start
data: {"type":"message_start","message":{...}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}

event: message_stop
data: {"type":"message_stop"}
```

工具调用流：`content_block_start` 的 `content_block.type` 为 `"tool_use"`，随后通过 `input_json_delta` 增量传输 JSON 参数。

**OpenAI 兼容流**（发往 `/v1/chat/completions`，`stream: true`）：

```
data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: [DONE]
```

**AI SDK 6.x 内部流格式**（OpenCode 内部使用，与 ds2api 无直接关系）：

```
text-start { id: "0" }
text-delta { id: "0", delta: "..." }
text-end { id: "0" }
```

> **关键兼容性风险**：LiteLLM 等 OpenAI 兼容代理在多轮工具调用流式传输时，会将 `text-delta` 的 `id` 误设为响应级别的 `chatcmpl-<id>` 而非 AI SDK 期望的本地 part ID（`"0"`），导致 AI SDK 抛出 `"text part {id} not found"` 错误，且只在**多轮工具调用的第二轮及之后**出现（GitHub BerriAI/litellm #26529）。ds2api 若走 OpenAI 兼容路径，需确保流式 `text-delta` 事件中的 part ID 与 `text-start` 保持一致。

### 7. 请求头 / User-Agent

OpenCode 发出的请求头因提供商类型而异：

- **Anthropic 原生提供商**：header 管理完全委托给 `@ai-sdk/anthropic` 包，包含 `x-api-key`、`anthropic-version: 2023-06-01`、`anthropic-beta`（视功能而定）
- **非 Anthropic 第三方提供商**：发送 `User-Agent: opencode/<VERSION>`
- **OpenCode 自有服务**（Zen/Go）：发送 `x-opencode-project`、`x-opencode-session`、`x-opencode-request`、`x-opencode-client`

ds2api 的 Anthropic 入口（`handler_messages.go` 第 22–24 行）已处理 `anthropic-version` 缺失情况：若请求未携带此 header 则自动补充 `2023-06-01`，与 OpenCode 行为兼容。

### 8. 会话状态管理

OpenCode 将完整对话历史存储在本地 SQLite（通过 Drizzle ORM），**每次请求均携带全部历史消息**，不依赖服务端 session ID。`context management / compaction` 机制在上下文窗口接近上限时自动进行历史摘要（通过 `SessionCompaction.isOverflow` 触发）。

这意味着 **ds2api 无需实现任何跨请求状态**——每次收到的请求都是完整的独立消息列表，现有架构完全适配。

## ds2api 适配 Checklist

以下各项对应 ds2api 仓库中的具体文件，在支持 OpenCode 时需逐一确认或修复：

### Anthropic 接口适配

**文件**：`internal/httpapi/claude/standard_request.go`、`internal/httpapi/claude/handler_messages.go`

- [x] `anthropic-version` 缺失时自动补充（已在 `handler_messages.go:22-24` 实现）
- [x] `mcp_servers` 字段展开为虚拟工具条目（已在 `standard_request.go:42-45` 实现）
- [ ] **验证 `anthropic-beta` header 透传**：OpenCode 调用扩展功能（如 extended thinking、computer use）时会附加 `anthropic-beta` header，确认 ds2api 不过滤此 header
- [ ] **`betas` 数组字段支持**：Anthropic Messages API 支持在请求体中携带 `betas` 数组，需确认 `normalizeClaudeRequest` 不会丢弃此字段
- [ ] **工具流式 `input_json_delta`**：Anthropic 流式工具调用通过 `input_json_delta` 增量传输 JSON，确认 `stream_runtime_emit.go` / `tool_call_state.go` 正确拼接
- [ ] **`reasoningSummary` 字段**：OpenCode 对推理模型附加此字段，需确认 Claude 入口不因未知字段返回 400

**文件**：`internal/httpapi/claude/handler_routes.go`

- [x] `/v1/messages`、`/anthropic/v1/messages`、`/messages` 路由均已注册

### OpenAI 兼容接口适配

**文件**：`internal/httpapi/openai/chat/handler_chat.go`、`internal/httpapi/openai/chat/chat_stream_runtime.go`

- [ ] **流式 text part ID 一致性**：确认 OpenAI SSE 流输出中每个文本块的 part ID（如 `index`）在 `text-start` / `text-delta` / `text-end` 生命周期内保持稳定，避免 AI SDK 6.x 的 `"text part not found"` 错误（参见 BerriAI/litellm #26529）
- [ ] **多轮工具调用**：确认第二轮（工具结果回传后）的流式文本正确携带 part ID，而非使用响应级 `id` 字段
- [ ] **`reasoningSummary` 字段忽略**：在 `handler_chat.go` 的请求解析阶段显式丢弃 `reasoningSummary`，避免转发至 DeepSeek 时产生无效参数

**文件**：`internal/httpapi/openai/shared/models.go`、`internal/httpapi/openai/shared/handler_toolcall_format.go`

- [ ] **`@ai-sdk/openai-compatible` 的工具格式**：确认 OpenCode 使用 `@ai-sdk/openai-compatible` 时发送的 `tools` 数组格式（`type: "function"` + `function.parameters`）被正确解析

### 翻译层

**文件**：`internal/translatorcliproxy/bridge.go`

- [ ] **Anthropic → OpenAI 翻译中的工具块处理**：`ToOpenAI(sdktranslator.FormatClaude, ...)` 路径需确认 `tool_use` / `tool_result` content blocks 被正确转换为 OpenAI `tool_calls` / `tool` role 消息
- [ ] **流式翻译 part ID 注入**：`FromOpenAIStream` 在将 OpenAI SSE 转换回 Anthropic 格式时，确认 `content_block_delta.index` 字段正确对应 `content_block_start.index`

### 工具调用层

**文件**：`internal/toolcall/tool_prompt.go`、`internal/toolcall/toolcalls_format.go`

- [ ] **OpenCode 工具名大小写**：确认 `buildClaudeToolPrompt` / 工具名提取逻辑对 OpenCode 的 `read`、`write`、`edit`、`bash`、`grep`、`glob` 等全小写工具名的 system prompt 注入格式与 Anthropic `tool_use` 块中的 `name` 字段一致
- [ ] **`task` 子 Agent 工具**：OpenCode 的 `task` 工具由模型发出后，由 OpenCode 本地处理（创建子会话），不会回传给 ds2api。无需特殊处理，但需确认 ds2api 不会因 `task` 工具结果格式异常而报错

### 配置与认证

**文件**：`internal/config/`、`internal/auth/`

- [ ] **自定义 provider baseURL 参数**：若用户使用 `{env:DS2API_KEY}` 语法，OpenCode 展开后会以标准 `Authorization: Bearer <key>` 或 `x-api-key: <key>` header 发送，确认 ds2api 的 auth 层（`internal/auth/admin.go`）接受两种形式
- [ ] **模型 ID 前缀剥离**：OpenCode 发送 `model: "ds2api/deepseek-r1"` 时，ds2api 收到的 model 字段为复合格式，需在 `config` 层或 handler 层剥离 `ds2api/` 前缀后再做模型解析

## 推荐接入方案

### 方案 A：OpenAI 兼容路径（推荐）

将 ds2api 配置为 OpenAI 兼容端点，用户使用 `@ai-sdk/openai-compatible`：

```json
{
  "provider": {
    "ds2api": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "ds2api",
      "options": {
        "baseURL": "http://your-ds2api-host/v1"
      },
      "models": {
        "deepseek-r1": { "name": "DeepSeek R1" }
      }
    }
  },
  "model": "ds2api/deepseek-r1"
}
```

优点：路径最短，ds2api 的 `/v1/chat/completions` 已稳定工作。
风险：需规避 Issue #5674 的 `options` 未转发 Bug；建议通过环境变量传递 apiKey。

### 方案 B：Anthropic 原生路径

将 ds2api 配置为 Anthropic 兼容端点（覆盖 `baseURL`）：

```json
{
  "provider": {
    "anthropic": {
      "options": {
        "baseURL": "http://your-ds2api-host"
      }
    }
  },
  "model": "anthropic/claude-sonnet-4-5"
}
```

优点：工具调用格式最完整，Anthropic SDK 完整 feature（thinking、extended context）均可用。
风险：Issue #5163 显示自定义 Anthropic baseURL 可能访问 `/api/messages` 路由（非标准），需确认 ds2api 的 Anthropic 路由是否覆盖此路径（目前 `handler_routes.go` 注册了 `/v1/messages`，未见 `/api/messages`）。

**建议补充**：在 `handler_routes.go` 增加 `/api/messages` 别名路由以覆盖 Claude Code SDK 的默认路径：

```go
r.Post("/api/messages", h.Messages)
```

## ds2api 版本影响说明

| ds2api 版本 | 对 OpenCode 的影响 |
|------------|-----------------|
| **v1.0.8** | `/v1/chat/completions`（OpenAI 兼容路径）和 `/v1/messages`（Anthropic 原生路径）的 handler 均已接入 `AutoDeleteRemoteSession`。WebUI"结束后全部删除"开关现在对 OpenCode 请求生效。 |
| **v1.0.10** | **可能的破坏性变更**：若 OpenCode 的 `opencode.json` 中 `model` 字段使用了非 DefaultModelAliases 中的 id（如 `ds2api/deepseek-r1` 会被剥离前缀后得到 `deepseek-r1`，该 id 需在 allowlist 中），请求可能返回 4xx。**操作**：检查 `DefaultModelAliases` 是否已覆盖你使用的模型 id；若未覆盖，在 ds2api WebUI Settings → Model Aliases 添加映射。另外，需确认 OpenCode 的 `provider_id/model_id` 前缀剥离逻辑是否在 ds2api 层已处理。 |
| **v1.0.12** | 上游 429 弹性故障转移，OpenCode 的多轮工具调用密集请求场景下 429 暴露率降低。 |

## 结论

OpenCode 通过 Vercel AI SDK 抽象层与各提供商交互，支持自定义 baseURL 和 OpenAI 兼容路径，与 ds2api 的接入点高度契合。核心适配工作集中在三个方面：

1. **流式 text part ID 一致性**（多轮工具调用关键路径）
2. **`reasoningSummary` 等扩展字段的无损透传或显式忽略**
3. **`/api/messages` 路由别名补充**（应对 Anthropic SDK 默认路径差异）

另外，v1.0.10 后需特别关注模型 id 的 allowlist 检查——OpenCode 使用复合格式 `provider/model`，ds2api 收到请求时需正确剥离 provider 前缀再做 allowlist 查询。

现有 `translatorcliproxy` + `toolcall` 层的设计已为工具调用翻译提供了充分基础，重点需验证 Anthropic ↔ OpenAI 双向翻译在 OpenCode 实际工具名集合下的正确性。

---

章节来源：
- [OpenCode 官方文档 - Providers](https://opencode.ai/docs/providers/)
- [OpenCode 官方文档 - Config](https://opencode.ai/docs/config/)
- [OpenCode 官方文档 - Tools](https://opencode.ai/docs/tools/)
- [OpenCode 官方文档 - Models](https://opencode.ai/docs/models/)
- [DeepWiki - sst/opencode 架构概览](https://deepwiki.com/sst/opencode)
- [DeepWiki - Prompt Orchestration](https://deepwiki.com/sst/opencode/2.3-prompt-orchestration)
- [GitHub Issue #5674 - Custom provider options not passed](https://github.com/anomalyco/opencode/issues/5674)
- [GitHub Issue #5163 - Custom baseURL Route not found](https://github.com/sst/opencode/issues/5163)
- [LiteLLM Issue #26529 - OpenAI-proxy streaming breaks AI SDK 6.x tool calls](https://github.com/BerriAI/litellm/issues/26529)
- [LiteLLM - OpenCode Integration Tutorial](https://docs.litellm.ai/docs/tutorials/opencode_integration)
- [Vercel AI SDK - Anthropic Provider](https://ai-sdk.dev/providers/ai-sdk-providers/anthropic)
- [haimaker.ai - Custom Provider Setup Guide](https://haimaker.ai/blog/opencode-custom-provider-setup/)
