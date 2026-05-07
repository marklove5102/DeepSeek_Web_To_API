# 响应缓存深度研究：提供商机制、命中率下降根因及向量语义缓存评估

<cite>
**本文档引用的来源**

官方文档：
- [Anthropic 提示缓存文档](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [OpenAI 提示缓存指南](https://developers.openai.com/api/docs/guides/prompt-caching)
- [DeepSeek 上下文缓存文档](https://api-docs.deepseek.com/guides/kv_cache)
- [DeepSeek 上下文缓存磁盘方案发布公告](https://api-docs.deepseek.com/news/news0802)
- [DeepSeek 模型定价](https://api-docs.deepseek.com/quick_start/pricing)

学术研究：
- [GPT 语义缓存论文 arXiv:2411.05276](https://arxiv.org/html/2411.05276v3)
- [vCache：已验证的语义提示缓存 arXiv:2502.03771](https://arxiv.org/html/2502.03771v5)
- [Introl：提示缓存基础设施 2025 指南](https://introl.com/blog/prompt-caching-infrastructure-llm-cost-latency-reduction-guide-2025)

工程案例：
- [DeepSeek agent 框架实现 85% 前缀缓存命中率](https://dev.to/esengine/how-a-deepseek-only-agent-framework-hit-85-prefix-cache-rate-and-saved-93-vs-claude-5c9g)
- [Anthropic：构建 Claude Code 的经验——提示缓存是一切的基础](https://claude.com/blog/lessons-from-building-claude-code-prompt-caching-is-everything)
- [vLLM 自动前缀缓存文档](https://docs.vllm.ai/en/stable/design/prefix_caching/)
- [Sankalp：提示缓存工作原理（分页注意力 + 自动前缀缓存）](https://sankalp.bearblog.dev/how-prompt-caching-works/)

向量数据库与工具：
- [sqlite-vec Go 绑定](https://github.com/asg017/sqlite-vec)
- [go-sqlite-vss](https://github.com/k1LoW/go-sqlite-vss)
- [Redis Vector 语义缓存](https://redis.io/docs/latest/develop/ai/)
- [Encore：pgvector vs Qdrant 对比](https://encore.dev/articles/pgvector-vs-qdrant)
- [Artificial Analysis：提供商缓存定价对比](https://artificialanalysis.ai/models/caching)

项目代码：
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [internal/responsecache/path_policy.go](file://internal/responsecache/path_policy.go)
- [docs/storage-cache.md](file://docs/storage-cache.md)
</cite>

## 目录

1. [简介](#1-简介)
2. [ds2api 当前缓存机制解析](#2-ds2api-当前缓存机制解析)
3. [提供商缓存机制详解](#3-提供商缓存机制详解)
4. [为什么 ds2api 的方案结构性地低于提供商命中率](#4-为什么-ds2api-的方案结构性地低于提供商命中率)
5. [向量语义缓存方案评估](#5-向量语义缓存方案评估)
6. [分层缓存设计建议](#6-分层缓存设计建议)
7. [结论与下一步](#7-结论与下一步)

---

## 1. 简介

ds2api 是一个 Go 网关，将 DeepSeek Web Chat 桥接到 OpenAI / Claude / Gemini 兼容的 HTTP API。当前生产环境有响应缓存条目，内存默认 TTL 30 分钟、磁盘默认 TTL 48 小时（v1.0.7 热重载修复后的最终默认值）。用户反映**命中率随时间下降**，并观察到 Anthropic、OpenAI、DeepSeek 等提供商官方宣称的命中率（70%+）与 ds2api 实际达到的命中率（约 9.6% 全请求体哈希天花板、LLM agent 工作流下）之间存在巨大落差，因而提出：是否应该切换到向量数据库支撑的语义缓存？

本报告通过阅读代码、查阅提供商官方文档及学术论文，系统分析三个问题：（1）提供商怎样实现高命中率；（2）ds2api 当前方案为何结构性地无法达到同等命中率；（3）向量语义缓存是否能弥补差距，以及具体应该怎么做。

**章节来源**
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [docs/storage-cache.md](file://docs/storage-cache.md)

---

## 2. ds2api 当前缓存机制解析

### 2.1 缓存键生成

缓存键由函数 `requestKey` 生成，逻辑为：

```
SHA-256(
  version       // "v1" 或 "v2-semantic"
  owner         // CallerID（调用方令牌标识，per-caller 分区）
                // 注：/v1/embeddings 和 /v1/messages/count_tokens 路径
                //     SharedAcrossCallers=true，owner 字段置空，跨 caller 共享
  method        // HTTP 方法（POST）
  canonicalPath // 规范化路径
  canonicalQuery
  varyRequestHeaders  // Accept / Anthropic-Beta / Anthropic-Version 等7个头
  SHA-256(canonicalRequestBodyForKey)  // 正文规范化后的哈希
)
```

**关键点**：最终键是**整个请求体规范化后的哈希**，而不是请求的某个子集或语义摘要。

### 2.2 规范化逻辑（`canonicalRequestBodyForKey` + `normalizeCacheKeyJSONValue`）

语义键模式（`semanticKey=true`）会做以下处理：
- 删除以下顶层字段：`metadata`, `user`, `service_tier`, `parallel_tool_calls`, `seed`, `store`, `betas`
- 删除会话级 ID 字段：`conversation_id`, `session_id`, `thread_id`, `message_id`, `request_id` 等
- 删除 `cache_control`, `cache_reference`, `context_management`
- 对 `content/text/input/prompt/query/instructions/system` 等文本字段执行 `strings.Fields` 折叠（多空白→单空白）
- 对 `role/type` 字段转小写

这意味着：字段顺序不变、tools 数组不变、所有 messages 内容完整保留。**只有极微小的格式差异被消除，语义内容本身不变。**

### 2.3 缓存结构

| 层 | 实现 | 默认 TTL | 最大容量 |
|---|---|---|---|
| 内存层 | `map[string]memoryEntry`，全局互斥锁 | **30 分钟**（v1.0.7 起） | 3.8 GB |
| 磁盘层 | `{key[:2]}/{key}.json.gz` 文件，gzip 压缩 | **48 小时**（v1.0.7 起） | 16 GB |

命中逻辑：先查内存，内存未命中则查磁盘，磁盘命中后回填内存。

### 2.4 v1.0.7 热重载修复：TTL 不再硬编码于路径策略

**这是 v1.0.7 最重要的缓存架构修复。**

修复前（v1.0.6 及更早）：`internal/responsecache/path_policy.go` 的 `pathPolicy` 结构持有 `MemoryTTL` / `DiskTTL` 字段，LLM 业务路径（chat/completions、responses、messages、embeddings、count_tokens）被硬编码为固定值（原约 30 min / 48 h）。操作员通过 WebUI 修改 `cache.response.memory_ttl_seconds` 后，数值在 `/admin/metrics/overview` 显示已变更，但这些路径实际上仍沿用硬编码值——TTL 调整**静默无效**。

修复后（v1.0.7 起）：`pathPolicy` 结构体**只剩两个字段**：

```go
type pathPolicy struct {
    Path                string
    SharedAcrossCallers bool
}
```

TTL 完全由 `Cache.memoryTTL` / `Cache.diskTTL`（即 Store 配置）决定，不再有任何 per-path 硬编码覆盖。`pathPolicyFor()` 函数中仅保留调用方共享策略（见 §2.5）。同时，v1.0.7 命中率优化的收益被固化进新的系统默认值：`defaultMemoryTTL = 30 * time.Minute`、`defaultDiskTTL = 48 * time.Hour`（见 `cache.go` 常量定义）。操作员不修改配置时也能享受优化后的默认值；通过 WebUI 修改时，改动将**真正生效**。

源代码说明（`cache.go` 注释）：
> "These are pure DEFAULTS — operators override via cache.response.{memory,disk}_ttl_seconds in the admin WebUI and Store TTL is always authoritative (no per-path hardcoded override; see path_policy.go for the rationale)."

### 2.5 可缓存路径与调用方共享策略

`CacheableRequest` 覆盖：`/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/anthropic/v1/messages`, `/v1/messages/count_tokens`, Gemini 的 `generateContent` 和 `streamGenerateContent`。

路径分桶策略（`pathPolicyFor()`）：

| 路径 | SharedAcrossCallers | 说明 |
|---|---|---|
| `/v1/embeddings` | **true** | 嵌入向量是确定性函数，owner 从 key 中移除，全部调用方共享 |
| `/v1/messages/count_tokens` | **true** | token 计数是纯确定性函数，同上 |
| LLM completions（其余所有路径）| false | 采样结果不跨调用方共享，防止一方回答泄漏给另一方 |

### 2.6 agent 流量的全请求体哈希命中率上限：约 9.6%

经对生产流量的实证分析，**全请求体哈希（whole-body-hash）在 LLM agent 工作流中的理论命中率上限约为 9.6%**。这一上限来自以下两类可命中场景：

1. **客户端重试**：网络超时、工具 crash 后的自动重发——此时请求体与上一次请求完全相同。
2. **工具幂等性重发**：agent 在确认工具结果前重发相同调用。

多轮对话本身（每轮 messages 数组末尾追加新消息）永远不会命中全请求体哈希缓存，因为 SHA-256 对整个正文计算。

**v1.0.7 默认 TTL 调整（30 min 内存 / 48 h 磁盘）的意义**：将有效命中率从约 5–8% 推向 9.6% 上限附近——更长的 TTL 让重试窗口覆盖更多实际的 agent 重发间隔（上游慢思考最长 2 小时，TTL 以前的 5 min 在此期间已失效）。

**章节来源**
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [internal/responsecache/path_policy.go](file://internal/responsecache/path_policy.go)

---

## 3. 提供商缓存机制详解

### 3.1 Anthropic 提示缓存

**缓存单元**：不是整个请求，而是请求前缀的 **KV Cache 张量**。缓存以分层前缀块（block）为单位，每块 hash = hash(前一块 hash + 当前块 token IDs)，形成链式校验。最多可设置 4 个 `cache_control` 断点，系统支持向回查找最多 20 块以找到最近一次缓存写入位置。

**最小 token 阈值**：

| 模型 | 最小可缓存长度 |
|---|---|
| Claude Opus 4.7 / Sonnet 4.6 | 2,048 tokens |
| Claude Sonnet 4.5 / Opus 4 / Sonnet 4 / 3.7 | 1,024 tokens |
| Haiku 4.5 | 4,096 tokens |

**定价模型**：

| 操作 | 倍率 | 实际含义 |
|---|---|---|
| 缓存写入（5 分钟 TTL）| 1.25× 标准输入价 | 首次写入多付 25% |
| 缓存写入（1 小时 TTL）| 2.0× 标准输入价 | 首次写入多付 100% |
| 缓存读取 | **0.1× 标准输入价** | 90% 折扣 |

**TTL**：默认 5 分钟（ephemeral）；可升级至 1 小时（费用翻倍）；每次缓存读取刷新 TTL。

**缓存范围**：组织级隔离（2026 年 2 月 5 日起改为工作区级）。不同组织之间的 cache 完全隔离，同一组织内共享相同系统提示的所有用户可以共享 cache。

**章节来源**
- [Anthropic 提示缓存官方文档](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [构建 Claude Code 的经验](https://claude.com/blog/lessons-from-building-claude-code-prompt-caching-is-everything)

### 3.2 OpenAI 自动提示缓存

**缓存单元**：以 1,024 token 为块单位的前缀 KV Cache。路由机制：请求按前缀（通常前 256 token）的哈希路由到同一台机器，该机器持有此前缓存的 KV 张量，并以 128 token 为增量匹配最长共同前缀。

**完全自动**：无需任何 `cache_control` 标记，无需代码修改，无额外费用。

**定价**：缓存命中 token **50% 折扣**（无写入费用）。延迟可降低最多 80%，成本最多降低 90%（高占比前缀情况下）。

**TTL**：默认模型中，前缀缓存在 **5–10 分钟不活动后清除**，最长保留 1 小时。新一代模型（gpt-5.5 等）支持最长 24 小时的"扩展提示缓存"。

**章节来源**
- [OpenAI 提示缓存文档](https://developers.openai.com/api/docs/guides/prompt-caching)

### 3.3 DeepSeek 上下文缓存（Context Caching on Disk）

**缓存单元**：前缀 token 精确匹配的 KV Cache，存储于磁盘（利用 DeepSeek V2 的 MLA 架构大幅压缩了 KV Cache 体积）。官方强调"**只有前缀从第 0 个 token 开始完全匹配才算命中，中间位置的部分匹配不触发缓存**"。

**完全自动**：无需代码修改，默认对所有用户启用。

**Cache hit 约为 cache miss 价格的 1/10**（约 90% 折扣）。DeepSeek 历史数据表明，未经专门优化时平均可节省 50% 以上；某专注 DeepSeek 的 agent 框架通过三区分区策略（不可变前缀 + 仅追加日志 + 易变暂存区）在多轮对话中实现了 85–95% 的前缀缓存命中率。

**章节来源**
- [DeepSeek 上下文缓存文档](https://api-docs.deepseek.com/guides/kv_cache)
- [DeepSeek agent 85% 命中率案例](https://dev.to/esengine/how-a-deepseek-only-agent-framework-hit-85-prefix-cache-rate-and-saved-93-vs-claude-5c9g)

### 3.4 提供商对比表

| 维度 | Anthropic | OpenAI | DeepSeek |
|---|---|---|---|
| 缓存层级 | 推理服务器 KV Cache 前缀 | 推理服务器 KV Cache 前缀 | 推理服务器 KV Cache 前缀（磁盘持久化）|
| 是否自动 | 需 `cache_control` 标记 | 完全自动 | 完全自动 |
| 最小 token 阈值 | 1,024–4,096 | 1,024 | ~64（未明确）|
| Cache 读折扣 | **90%** | 50% | **90%** |
| 默认 TTL | 5 分钟（可升级 1 h）| 5–10 分钟（可升级 24 h）| 数小时到数天（磁盘持久化）|
| 典型命中率 | 70–90%（正确设计的 agent）| 70–80%（稳定前缀）| 50–95%（视前缀设计）|

**章节来源**
- [Anthropic 提示缓存](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [OpenAI 提示缓存](https://developers.openai.com/api/docs/guides/prompt-caching)
- [DeepSeek 上下文缓存](https://api-docs.deepseek.com/guides/kv_cache)

---

## 4. 为什么 ds2api 的方案结构性地低于提供商命中率

### 4.1 关键心智模型：提供商缓存的是计算状态，ds2api 缓存的是响应结果

提供商 KV Cache 层（推理服务器内部）：每个 token 在通过 Transformer 的注意力层时，会生成 Key 和 Value 张量（K/V 矩阵）。后续请求只要**前缀 tokens 完全相同**，就可以跳过这部分全部注意力计算，直接从缓存取出 K/V 张量，只需对新增的尾部 token 做计算。**每轮对话追加一条新消息，前面所有内容都作为前缀命中缓存**。

ds2api 应用层缓存：将整个规范化后的请求体计算 SHA-256 哈希作为 key，命中条件是**整个请求体的哈希与历史记录完全一致**。多轮对话中，每一轮都是 miss。**唯一能命中的场景是：客户端重复发送与历史请求字节完全相同的请求**。

### 4.2 Per-Caller 分区对 LLM completions 路径

```go
owner := strings.TrimSpace(a.CallerID)  // 每个 API Key 对应独立分区
key := c.requestKey(r, owner, rawBody)  // owner 被哈入 key
```

N 个 API Key 发送语义等价的 LLM completion 请求时，缓存被 N 路隔离，需各自独立预热。对于本来就极少出现完全重复请求的 agent 工作流，per-caller 分区进一步将稀少的命中机会切碎。

**注**：`/v1/embeddings` 和 `/v1/messages/count_tokens` 路径（v1.0.3 起）已移除 owner，SharedAcrossCallers=true，不受此限制。

### 4.3 ds2api 缓存命中率的物理上限

在 LLM agent 工作流中，能触发 ds2api 缓存命中的场景：

| 场景 | 能否命中 |
|---|---|
| 同一 caller 重发完全相同的历史请求 | **能**（内存层 30 分钟内，磁盘层 48 小时内）|
| 工具崩溃/网络超时后的客户端重试 | **能**（最常见的合法命中场景）|
| 上游慢思考长达 2 小时的重发 | **能**（v1.0.7 inflight wait timeout = 2h，单飞 dedup 覆盖）|
| 多轮对话中每一轮的新消息 | **不能**（整体哈希变化）|
| 不同 caller 发送相同 LLM completion 请求 | **不能**（per-caller 分区）|
| 不同 caller 发送相同 embeddings 请求 | **能**（v1.0.3+ SharedAcrossCallers）|

实证上限：约 **9.6%**。v1.0.7 默认 TTL 拉长（30 min / 48 h）将有效命中率推向此上限。

### 4.4 结论：两种缓存的定位根本不同

ds2api 的全请求哈希缓存是**幂等重放缓存**（replay cache）——它解决的是"完全相同的请求不需要再次打到上游"。这对于抵御重放攻击、客户端重试有意义，但不是提供商 KV 缓存所解决的问题。

**用户看到的"提供商命中率 70–90%"指的是 API 账单上 `prompt_cache_hit_tokens / total_input_tokens` 的比例**，反映的是推理服务器对 KV 张量的复用，与 ds2api 的"命中"是完全不同的概念。

**章节来源**
- [sankalp：提示缓存工作原理](https://sankalp.bearblog.dev/how-prompt-caching-works/)
- [vLLM 自动前缀缓存](https://docs.vllm.ai/en/stable/design/prefix_caching/)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [internal/responsecache/path_policy.go](file://internal/responsecache/path_policy.go)

---

## 5. 向量语义缓存方案评估

### 5.1 什么是向量语义缓存

向量语义缓存在全请求哈希缓存的基础上增加一层：将请求的核心文本内容转换为向量嵌入，然后在嵌入空间中进行近邻搜索，如果找到余弦相似度超过阈值 τ 的历史请求，则返回对应的历史响应，无需调用 LLM。

### 5.2 ds2api 实际工作负载对语义缓存的适配性分析

ds2api 的主要调用方是 **Claude Code / Cline / Cherry Studio / Codex** 等 LLM coding agent，其请求特征如下：

| 特征 | 值 |
|---|---|
| 对话长度 | 通常 10–100+ 轮 |
| 每轮输入 token 数 | 5,000–200,000+（含完整代码库上下文）|
| 请求间重复性 | 低——每轮都包含新的代码修改、错误信息、工具调用结果 |
| 语义等价重复请求出现率 | 极低——coding 问题高度情境依赖，几乎每个请求都是唯一的 |

研究文献（arXiv:2502.03771 / vCache）明确指出语义缓存适合"单轮、短到中等上下文"的交互，对长上下文多轮对话**效果有限**。具体原因：

1. **嵌入向量只反映 last user message**：100K token 的对话历史无法整体嵌入，只能对最后一条消息嵌入。相同的"帮我修复这个 bug"在不同代码上下文下正确答案完全不同——嵌入相似但答案迥异，是典型假阳性。
2. **coding 任务假阳性代价高**：相似度 0.8 的语义缓存可能将历史答案返回给语义近似但不同的新请求，在 coding agent 工作流中返回错误代码会直接导致任务失败。
3. **嵌入延迟不可忽视**：本地 ONNX 嵌入约 10–50ms，远程 API 约 100–800ms。对于低命中率工作负载（<15%），每次 miss 都付出了额外的嵌入延迟。

**章节来源**
- [GPT 语义缓存论文 arXiv:2411.05276](https://arxiv.org/html/2411.05276v3)
- [vCache arXiv:2502.03771](https://arxiv.org/html/2502.03771v5)
- [sqlite-vec GitHub](https://github.com/asg017/sqlite-vec)

---

## 6. 分层缓存设计建议

### 6.1 核心结论

**不应该切换到向量数据库语义缓存作为主要策略**。ds2api 的工作负载（长对话 coding agent）是语义缓存收益最低的场景之一。当前的低命中率是结构性的（全请求哈希 vs. 不断变化的对话历史），向量语义相似度无法弥补。

**应该做的事情**：保留现有全哈希缓存（v1.0.7 热重载修复后语义已正确），维持 30 min / 48 h 默认 TTL，针对 `/v1/embeddings` 跨 caller 共享（已实施），提升可观测性。

### 6.2 TTL 和容量现状（v1.0.7 已固化）

| 参数 | 当前默认值 | 说明 |
|---|---|---|
| `defaultMemoryTTL` | **30 min** | 固化进代码常量，不需要 config 即可生效 |
| `defaultDiskTTL` | **48 h** | 覆盖更长的上游慢思考窗口 |
| `memory_max_bytes` | 3.8 GB | 保持 |
| `disk_max_bytes` | 16 GB | 保持 |
| `max_body_bytes` | 64 MB | 保持 |

### 6.3 按路径分解命中率指标（优先建议）

**当前问题**：无法区分"因每轮 prompt 变化导致的结构性 miss"和"因 TTL 过期、容量驱逐导致的可优化 miss"。

建议在 Stats() 中暴露：
- 按路径分解的命中率（`/v1/embeddings` 预期远高于 `/v1/chat/completions`）
- 内存 vs 磁盘命中分布
- streaming bypass 计数（streaming 请求主动跳过缓存，理解 uncacheable 请求构成）

**章节来源**
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [internal/responsecache/path_policy.go](file://internal/responsecache/path_policy.go)

---

## 7. 结论与下一步

### 核心结论

| 问题 | 结论 |
|---|---|
| 为什么提供商命中率高？ | 推理服务器内 KV Cache 前缀复用，不是应用层哈希缓存，根本不可比 |
| ds2api 命中率为何随时间下降？ | 全请求哈希缓存对每轮变化的对话历史永远 miss；随对话数量增加，完全重复请求的概率下降 |
| v1.0.7 热重载 bug 修了什么？ | pathPolicy 移除 TTL 字段，Store 配置成为绝对权威；操作员 WebUI 调整 TTL 现在真正生效；新默认值 30 min / 48 h 固化到代码常量 |
| 应该切换向量语义缓存吗？ | **不应该**作为主要策略；coding agent 工作负载是语义缓存收益最低的场景 |
| 全请求体哈希命中率上限？ | 约 **9.6%**（agent 流量实证）；v1.0.7 TTL 调整将有效命中率推向此上限 |

### 下一步行动（按优先级）

| 优先级 | 行动 | 预期收益 | 工作量 |
|---|---|---|---|
| P0 | 按路径分解命中率指标，写入 Stats() 和 admin dashboard | 建立数据基线，支撑后续所有决策 | 1–2 天 |
| P1 | 暴露 streaming / uncacheable_reason 细分统计 | 理解 miss 构成，识别可优化空间 | 0.5 天 |
| P2（可选）| 如 P0 数据显示 `/v1/embeddings` 跨 caller 重复率高且 24 h 内 TTL 不足，引入本地 ONNX + SQLite 向量扫描 | 语义去重，进一步提升嵌入缓存命中率 | 1 周 |

**明确不做的事情**：
- 不引入 Redis、PostgreSQL、Qdrant、Milvus 等独立服务依赖
- 不对 `/v1/chat/completions` 或 `/v1/messages` 实施向量语义缓存
- 不试图在应用层复制提供商的 KV Cache 前缀复用机制（这在模型外部不可能实现）

**章节来源**
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [internal/responsecache/path_policy.go](file://internal/responsecache/path_policy.go)
- [docs/storage-cache.md](file://docs/storage-cache.md)
