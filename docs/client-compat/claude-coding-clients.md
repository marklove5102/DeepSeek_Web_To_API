# Claude 系编码类客户端兼容性报告

用户提到的 "OpenClaw" 最可能指 **OpenClaw**（github.com/openclaw/openclaw，奥地利开发者 Peter Steinberger 创作的开源个人 AI 助手框架）—— 主要研究 OpenClaw，附带列出 Claude Code CLI / Cline / Aider 的差异点。

---

## 背景：候选工具辨析

搜索结果可以确认 **OpenClaw 是一个真实存在的独立项目**，并非"open-claude"的误拼。以下列出四个主要候选，以及被采纳或排除的理由：

| 工具 | 仓库 / 主页 | 与 "OpenClaw" 的相似度 |
|------|-------------|----------------------|
| **OpenClaw** ⭐ 首选 | github.com/openclaw/openclaw | 名称直接匹配，搜索量最高 |
| Claude Code | github.com/anthropics/claude-code | 是 Anthropic 官方 CLI，非第三方 |
| Cline (claude-dev) | github.com/cline/cline | VS Code 扩展，不含 "claw" |
| Aider | github.com/Aider-AI/aider | 终端工具，亦无 "claw" 关联 |

OpenClaw 于 2025 年 11 月发布后迅速成为 GitHub 增长最快的仓库之一，定位为"本地优先的个人 AI 自动化平台"，通过 WhatsApp/Telegram/Discord 等 IM 接收指令并自主执行任务。它的 Anthropic 相关 API 调用模式与 Claude Code 高度重叠，因此在 ds2api 代理层面值得专门研究。

---

## 1. API 接口面（Anthropic Messages API）

OpenClaw 使用 **Anthropic Messages API**（`POST /v1/messages`），通过官方 Anthropic SDK（TypeScript）发起请求，版本锁定在：

```
anthropic-version: 2023-06-01
```

**默认 beta 头**（由 `providers/anthropic.js` 的 `createClient()` 函数注入）：

```
anthropic-beta: fine-grained-tool-streaming-2025-05-14
```

条件性追加（启用 extended thinking 或超长上下文时）：

```
anthropic-beta: fine-grained-tool-streaming-2025-05-14,interleaved-thinking-2025-05-14
anthropic-beta: fine-grained-tool-streaming-2025-05-14,context-1m-2025-08-07
```

> **已知 bug（issue #10647 / #18771）**：当用户在配置中选择 `contextWindow: 1000000` 的模型时，`context-1m-2025-08-07` 并不总是被自动加入，导致 API 实际以 200K 限制运行而无任何警告。对于 ds2api 等代理：若上游（DeepSeek）不识别该 beta 头，需在接收侧直接剥离，而非透传。

常用模型名称（客户端侧直接传入）：

```
anthropic/claude-opus-4-6
anthropic/claude-opus-4.7
anthropic/claude-sonnet-4-6
```

---

## 2. Base URL / Auth 覆盖

**环境变量方式**（PR #7821 合入后）：

```bash
export ANTHROPIC_BASE_URL="http://your-proxy:8045"
export ANTHROPIC_API_KEY="your-api-key"
openclaw agent --message "Hello" --local
```

**配置文件方式**（`~/.openclaw/openclaw.json`）：

```json
{
  "models": {
    "providers": {
      "anthropic": {
        "baseUrl": "http://your-proxy:8045",
        "apiKey": "your-api-key"
      }
    }
  }
}
```

**注意事项**：

- 若 `ANTHROPIC_BASE_URL` 为空字符串，当前实现视为"未设置"，仍使用 `api.anthropic.com` —— 因此无法通过传入空字符串来"禁用"代理。
- issue #56679 指出：内置 `anthropic` provider 的 `baseUrl` 覆盖在 `mode: "merge"` 配置下无效；当前推荐的完整绕过方案是通过 `iptables` 重定向（需要代理层 TLS 终止），或等待官方 `providerOverrides` 支持。
- 与 LiteLLM 对接时，OpenClaw 将配置写入 `baseUrl` 字段，并在 `model` 字段加 `openrouter/<author>/<slug>` 前缀路由到对应提供商。

---

## 3. Tool 格式

OpenClaw 使用 **Anthropic 原生 `tool_use` 块**，不使用 `computer_use` 类型。内置工具集如下：

| 工具名 | 类别 | 说明 |
|--------|------|------|
| `exec` / `process` | 执行 | Shell 命令与后台进程管理 |
| `read` / `write` / `edit` / `apply_patch` | 文件 | 文件读写与多段落补丁 |
| `code_execution` | 沙盒 | 远程沙箱 Python 分析 |
| `browser` | 浏览器 | Chromium 自动化 |
| `web_search` / `x_search` / `web_fetch` | 检索 | Web 查询与内容抓取 |
| `message` | 通讯 | 跨 IM 频道消息发送 |
| `canvas` | 呈现 | Node Canvas 展示 |
| `nodes` / `cron` / `gateway` | 系统 | 设备发现、定时任务 |
| `image` / `image_generate` / `tts` | 多模态 | 图像与语音生成 |
| `sessions_*` / `subagents` | 编排 | 子代理与会话管理 |

工具以标准 Anthropic `tool_use` block 格式提交给模型：

```json
{
  "type": "tool_use",
  "id": "toolu_01XY...",
  "name": "read",
  "input": { "file_path": "/path/to/file" }
}
```

**MCP 工具**（见第 6 节）由 OpenClaw 在本地处理后转换为普通 `tool_use` block，不使用 Anthropic 的 `mcp_servers` API 字段直接透传至模型。

---

## 4. Streaming 事件序列

OpenClaw 期望的完整 SSE 事件顺序与 Anthropic 官方规范一致：

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
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":42}}

event: message_stop
data: {"type":"message_stop"}
```

启用 `fine-grained-tool-streaming-2025-05-14` 时，工具调用参数通过 `input_json_delta` 逐步流出：

```json
{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}
```

启用 `interleaved-thinking-2025-05-14` 时，会出现 `thinking_delta` 类型事件，内含 `<thinking>` 块。

**心跳（ping）**：OpenAI 协议的 SSE 注释行（`: keep-alive`）会被 ds2api 的 `stream_writer.go` 转换为：

```
event: ping
data: {"type":"ping"}
```

这与 Anthropic 官方 ping 格式完全一致，OpenClaw 可正常接收。

---

## 5. Prompt Caching（提示缓存）

OpenClaw 使用 **显式 `cache_control` 标记**注入到系统提示和 tools 定义区：

```json
{
  "system": [
    {
      "type": "text",
      "text": "You are a helpful assistant...",
      "cache_control": { "type": "ephemeral" }
    }
  ]
}
```

缓存策略通过配置的 `cacheRetention` 键控制：

- `"short"`（默认）→ `{"type": "ephemeral"}` — 5 分钟缓存（api.anthropic.com 默认 TTL）
- `"long"` → `{"type": "ephemeral", "ttl": "1h"}` — 1 小时缓存（仅对直连 api.anthropic.com 有效）
- `"none"` → 不注入 `cache_control`

**代理兼容注意**：`anthropic-beta: prompt-caching-2024-07-31` 头已不再需要（Anthropic 在 2025 年中期将 prompt caching 设为默认启用），但 OpenClaw 在针对 OpenRouter 路由 Anthropic 模型时会主动注入 `cache_control` 块。ds2api 当前对这些块保持透传，不做特殊处理，是正确的策略。

---

## 6. MCP 集成

OpenClaw 在**本地**管理 MCP 连接，不将 `mcp_servers` 字段透传至 Anthropic API。其配置格式（`~/.openclaw/openclaw.json`）：

```json
{
  "mcp": {
    "servers": {
      "context7": {
        "command": "uvx",
        "args": ["context7-mcp"],
        "env": { "API_KEY": "xxx" }
      },
      "remote-docs": {
        "url": "https://mcp.example.com",
        "transport": "streamable-http",
        "headers": { "Authorization": "Bearer token" }
      }
    }
  }
}
```

支持三种传输方式：stdio（本地子进程）、SSE/HTTP（远程服务）、Streamable HTTP（双向流）。

MCP 工具最终以普通 `tool_use` block 送入模型，**不使用** Anthropic 的顶层 `mcp_servers` 请求字段。因此 ds2api 的 `expandMCPServersAsTools()` 函数（`standard_request.go` L42–L43）与此路径相互独立：若客户端恰好使用了 Anthropic SDK 的 `mcp_servers` 字段（Claude Code CLI 2.x 的某些版本会这样做），ds2api 会将其展开为虚拟 tool 描述符注入系统提示，保持向下兼容。

---

## 7. 与代理（LiteLLM / OpenRouter / ds2api）的已知兼容性问题

### 7.1 不支持的 beta 头被代理拒绝

**现象**：`interleaved-thinking-2025-05-14`、`context-1m-2025-08-07`、`fine-grained-tool-streaming-2025-05-14` 等 beta 头发送至不认识它们的代理（AWS Bedrock、Vertex AI）时触发 `invalid beta flag` 错误。

**LiteLLM 解决方案**：
```python
litellm.drop_params = True
litellm.modify_params = True
```

**ds2api 建议**：在 `handler_messages.go` 的请求入口处，对 `anthropic-beta` 头进行白名单过滤，剥除 ds2api 上游无法识别的 beta 值，仅保留无害条目（如 `prompt-caching-2024-07-31`）。

### 7.2 FQDN URL 下 LiteLLM 静默失败

issue #9453 显示，当 `baseUrl` 设为含完整域名（FQDN）的地址时，OpenClaw 的请求静默失败，无任何错误日志。排查方向：检查 OpenClaw SDK 对 `baseUrl` 末尾斜杠的处理（是否导致路径拼接为 `//v1/messages`）。ds2api 作为代理无需特别处理此问题，但可在响应头或错误信息中注明接受路径。

### 7.3 `service_tier` 参数被非 Anthropic 代理拒绝

OpenClaw 的 Fast Mode 会在请求中附加 `service_tier: "auto"`，该参数 Anthropic 接受但大多数代理不认识。LiteLLM 的 `drop_params: true` 可处理此情况；ds2api 目前会通过 `translatorcliproxy.ToOpenAI()` 进行格式转换，该过程会自然过滤掉 Anthropic 专有字段。

### 7.4 pricing fetch 超时阻塞启动

issue #74128 显示 OpenClaw 在 macOS 上启动时会阻塞约 75 秒做定价信息拉取，与代理无关，但可能导致首次请求延迟超过客户端超时设置。

---

## 8. 竞品 Runner-up 的 API 差异摘要

### Claude Code CLI（github.com/anthropics/claude-code）

Anthropic 官方 CLI，直接调用 `/v1/messages`。同样发送 `anthropic-version: 2023-06-01`；在 2.x 版本中对需要 MCP 的请求会发送顶层 `mcp_servers` 字段（而非将其展开为工具），这正是 ds2api `expandMCPServersAsTools()` 函数的目标客户端。工具集以 Bash、Read、Write、Edit、Grep、Glob、WebFetch、NotebookEdit 为主，均为标准 `tool_use` 块。不使用 `computer_use` 类型。streaming 格式与规范完全一致。

### Cline / claude-dev（github.com/cline/cline）

VS Code 扩展，同样走 `/v1/messages`，支持通过 UI 切换 base URL（对应字段 `customBaseUrl`）。关键差异：当使用 OpenAI Compatible 模式时 tool_use 支持不完整，需切换到 Anthropic native 模式才能获得完整工具调用。Cline 会在请求中显式设置 `cache_control` 标记以利用 prompt caching。extended thinking 与工具调用并用时需要将 thinking block 回传，Cline 已适配此行为。

### Aider（github.com/Aider-AI/aider）

终端编码助手，通过 `litellm` 库对接多家提供商，支持 `ANTHROPIC_API_BASE` 环境变量覆盖 base URL。Aider 使用 **系统提示内嵌 diff 格式**，不走 tool_use 块，模型以纯文本返回 `SEARCH/REPLACE` 块，Aider 本地解析。对代理而言，Aider 的请求是"无工具调用的纯文本对话"，兼容性最简单。Aider 会根据配置和模型类型自动决定是否注入 `anthropic-beta: prompt-caching-2024-07-31`。

---

## ds2api 适配清单

以下是与本报告对应的 ds2api 实现要点，按文件索引：

### `internal/httpapi/claude/standard_request.go`

- **L42–L43**：`expandMCPServersAsTools()` — 将 `mcp_servers` 字段（Claude Code CLI 2.x 方式）展开为虚拟 tool 描述符，现已实现。OpenClaw 不使用此字段（它在本地管理 MCP），但 Claude Code CLI 会。
- **L35**：`tools` 透传逻辑 — 标准 `tool_use` 块格式，OpenClaw / Cline / Claude Code 均适配。
- 建议新增：对 `cache_control` 字段的透传保障——确认 `normalizeClaudeMessages()` 不会剥除消息体中的 `cache_control` 子字段。

### `internal/httpapi/claude/handler_messages.go`

- **L22–L24**：`anthropic-version` 缺失时自动补 `2023-06-01` —— 与 OpenClaw / Claude Code 的实际发送值完全对齐，正确。
- 建议新增：在此位置对 `anthropic-beta` 头进行过滤，剥除 ds2api 上游（DeepSeek web channel）无法识别的 beta 标记（`fine-grained-tool-streaming-2025-05-14`、`interleaved-thinking-2025-05-14`、`context-1m-2025-08-07`），以防止上游报 `invalid beta flag`。

### `internal/httpapi/claude/handler_messages_direct.go`

- **L52–L56**：`normalizeClaudeRequest()` 调用 —— 保证所有 beta 头在此之前已被处理（入参仅含 body，不含头信息），beta 头过滤应在调用方（`Messages()` 函数）完成。
- **L206**：`sse.CollectStream()` —— 负责消费 OpenClaw 发来的 SSE 流；注意 OpenClaw 在 extended thinking 模式下会产生 `thinking_delta` 事件，`CollectStream` 的 `thinkingEnabled` 参数需与请求中 `thinking` 字段保持同步。

### `internal/translatorcliproxy/`

- **`stream_writer.go` L77–L85**：SSE 注释行（`: keep-alive`）转换为 `event: ping / data: {"type":"ping"}` —— 与 Anthropic ping 格式一致，OpenClaw 不会因此报错。
- **`stream_writer.go` L165–L175**：OpenAI 流式错误转换为 Claude SSE 错误格式 —— OpenClaw 可正确解析 `event: error` 块。
- **`bridge.go` L108–L112**：非流式响应 usage 字段映射为 `input_tokens` / `output_tokens` —— 与 Anthropic 规范对齐，OpenClaw 的计费统计依赖此字段。

---

*本文档由 ds2api 研究流程自动生成，基于 OpenClaw GitHub issue、官方文档及 ds2api 源码交叉比对。若 OpenClaw 上游版本更新导致 beta 头列表变化，请重新检查 `handler_messages.go` 的过滤逻辑。*
