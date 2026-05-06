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

ds2api 是一个 Go 网关，将 DeepSeek Web Chat 桥接到 OpenAI / Claude / Gemini 兼容的 HTTP API。当前生产环境有 4,230 个缓存条目，共 22 MB，内存 TTL 30 分钟、磁盘 TTL 24 小时。用户反映**命中率随时间下降**，并观察到 Anthropic、OpenAI、DeepSeek 等提供商官方宣称的命中率（70%+）与 ds2api 实际达到的命中率（约 10–15%，LLM agent 工作流下的物理上限）之间存在巨大落差，因而提出：是否应该切换到向量数据库支撑的语义缓存？

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
| 内存层 | `map[string]memoryEntry`，全局互斥锁 | 30 分钟 | 3.8 GB |
| 磁盘层 | `{key[:2]}/{key}.json.gz` 文件，gzip 压缩 | 24 小时 | 16 GB |

命中逻辑：先查内存，内存未命中则查磁盘，磁盘命中后回填内存。

### 2.4 可缓存路径

`CacheableRequest` 覆盖：`/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/anthropic/v1/messages`, `/v1/messages/count_tokens`, Gemini 的 `generateContent` 和 `streamGenerateContent`。

**章节来源**
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

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

以 Claude Opus 4.7（标准输入 $5/MTok）为例：缓存写入 $6.25/MTok，缓存读取 $0.50/MTok。**一次缓存读取就能收回 5 分钟 TTL 写入成本，读取 2 次可收回 1 小时 TTL 写入成本。**

**TTL**：默认 5 分钟（ephemeral）；可升级至 1 小时（费用翻倍）；每次缓存读取刷新 TTL。

**缓存范围**：组织级隔离（2026 年 2 月 5 日起改为工作区级）。不同组织之间的 cache 完全隔离，同一组织内共享相同系统提示的所有用户可以共享 cache。

**命中率来源**：Claude Code 官方团队的经验报告显示，正确设计的长对话 coding agent 可以将 90%+ 的输入 token 来自缓存读取，将缓存命中率视为关键可用性指标，一旦下降即触发告警。

**章节来源**
- [Anthropic 提示缓存官方文档](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [构建 Claude Code 的经验](https://claude.com/blog/lessons-from-building-claude-code-prompt-caching-is-everything)

### 3.2 OpenAI 自动提示缓存

**缓存单元**：以 1,024 token 为块单位的前缀 KV Cache。路由机制：请求按前缀（通常前 256 token）的哈希路由到同一台机器，该机器持有此前缓存的 KV 张量，并以 128 token 为增量匹配最长共同前缀。

**完全自动**：无需任何 `cache_control` 标记，无需代码修改，无额外费用。

**定价**：缓存命中 token **50% 折扣**（无写入费用）。延迟可降低最多 80%，成本最多降低 90%（高占比前缀情况下）。

**TTL**：默认模型中，前缀缓存在 **5–10 分钟不活动后清除**，最长保留 1 小时。新一代模型（gpt-5.5 等）支持最长 24 小时的"扩展提示缓存"。

**缓存范围**：组织级，不跨组织共享。每分钟请求量超过约 15 次时，部分请求可能溢出到其他机器降低命中率。

**章节来源**
- [OpenAI 提示缓存文档](https://developers.openai.com/api/docs/guides/prompt-caching)

### 3.3 DeepSeek 上下文缓存（Context Caching on Disk）

**缓存单元**：前缀 token 精确匹配的 KV Cache，存储于磁盘（利用 DeepSeek V2 的 MLA 架构大幅压缩了 KV Cache 体积）。官方强调"**只有前缀从第 0 个 token 开始完全匹配才算命中，中间位置的部分匹配不触发缓存**"。

**最小 token**：文档未明确说明最低阈值（另有第三方来源指出约 64 tokens）。

**完全自动**：无需代码修改，默认对所有用户启用。

**定价（DeepSeek-V4-Pro，当前 75% 折扣期间）**：

| 类型 | 原始价格 | 折后价格（至 2026/05/31）|
|---|---|---|
| 输入 cache miss | $1.74/MTok | $0.435/MTok |
| 输入 cache hit | $0.0145/MTok | $0.003625/MTok |
| 输出 | $3.48/MTok | $0.87/MTok |

**DeepSeek-V4-Flash**：cache miss $0.14/MTok，cache hit $0.0028/MTok，输出 $0.28/MTok。

Cache hit 约为 cache miss 价格的 **1/10**（约 90% 折扣）。DeepSeek 历史数据表明，未经专门优化时平均可节省 50% 以上；某专注 DeepSeek 的 agent 框架通过三区分区策略（不可变前缀 + 仅追加日志 + 易变暂存区）在多轮对话中实现了 85–95% 的前缀缓存命中率。

**TTL**：按需自动清理，"通常数小时到数天"，不提供精确保证。

**缓存范围**：per-user 磁盘缓存，"尽力而为"，不保证 100% 命中。

**章节来源**
- [DeepSeek 上下文缓存文档](https://api-docs.deepseek.com/guides/kv_cache)
- [DeepSeek 上下文缓存磁盘方案公告](https://api-docs.deepseek.com/news/news0802)
- [DeepSeek 定价](https://api-docs.deepseek.com/quick_start/pricing)
- [DeepSeek agent 85% 命中率案例](https://dev.to/esengine/how-a-deepseek-only-agent-framework-hit-85-prefix-cache-rate-and-saved-93-vs-claude-5c9g)

### 3.4 提供商对比表

| 维度 | Anthropic | OpenAI | DeepSeek |
|---|---|---|---|
| 缓存层级 | 推理服务器 KV Cache 前缀 | 推理服务器 KV Cache 前缀 | 推理服务器 KV Cache 前缀（磁盘持久化）|
| 是否自动 | 需 `cache_control` 标记 | 完全自动 | 完全自动 |
| 最小 token 阈值 | 1,024–4,096 | 1,024 | ~64（未明确）|
| Cache 读折扣 | **90%** | 50% | **90%** |
| Cache 写额外费 | +25%（5 min）/ +100%（1 h）| 无 | 无 |
| 默认 TTL | 5 分钟（可升级 1 h）| 5–10 分钟（可升级 24 h）| 数小时到数天（磁盘持久化）|
| Cache 范围 | 组织/工作区级 | 组织级 | 用户级 |
| 典型命中率 | 70–90%（正确设计的 agent）| 70–80%（稳定前缀）| 50–95%（视前缀设计）|

**章节来源**
- [Anthropic 提示缓存](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [OpenAI 提示缓存](https://developers.openai.com/api/docs/guides/prompt-caching)
- [DeepSeek 上下文缓存](https://api-docs.deepseek.com/guides/kv_cache)
- [Artificial Analysis 定价对比](https://artificialanalysis.ai/models/caching)

---

## 4. 为什么 ds2api 的方案结构性地低于提供商命中率

这是本文最核心的分析。提供商与 ds2api 的缓存不在同一个层级上运作，二者根本不可比。

### 4.1 关键心智模型：提供商缓存的是计算状态，ds2api 缓存的是响应结果

**提供商 KV Cache 层**（推理服务器内部）：

每个 token 在通过 Transformer 的注意力层时，会生成 Key 和 Value 张量（K/V 矩阵）。对于请求 R1：

```
系统提示（100K tokens）→ 计算 K/V 张量 → 存入 GPU KV Cache
```

后续请求 R2 只要**前缀 tokens 完全相同**，就可以跳过这 100K token 的全部注意力计算，直接从缓存取出 K/V 张量，只需对新增的尾部 token 做计算。这意味着：

- 一个 100K token 的系统提示只需要在 cache miss 时计算一次
- 后续任意新增一条用户消息，仍然命中 90% 的输入 token 缓存
- **每轮对话追加一条新消息，前面所有内容都作为前缀命中缓存**

这是推理引擎在模型权重层面的计算复用，任何应用层代码都无法在模型外部复制这种效果。

**ds2api 应用层缓存**：

ds2api 将整个规范化后的请求体计算 SHA-256 哈希作为 key，命中条件是**整个请求体的哈希与历史记录完全一致**。这意味着：

- 多轮对话中，Turn 1 → Turn 2 时，messages 数组末尾新增了一条 user/assistant 消息对
- 整个 messages 数组变化 → 正文哈希变化 → **缓存完全未命中**
- Turn 3、Turn 4 同理，每一轮都是 miss
- **唯一能命中的场景是：客户端重复发送与历史请求字节完全相同的请求**

### 4.2 Per-Caller 分区使命中率雪上加霜

```go
owner := strings.TrimSpace(a.CallerID)  // 每个 API Key 对应独立分区
key := c.requestKey(r, owner, rawBody)  // owner 被哈入 key
```

假设有 N 个不同的 API Key 访问同一个 LLM 应用（例如，团队中多人各自持有 key 使用 Claude Code），即使他们发出了语义上完全一致的请求，缓存也会被 N 路隔离。对于同一个语义请求，缓存必须被各个 caller 独立"预热"一次，才能对各自后续的完全重复请求命中。

在 LLM agent 工作流中，这种完全重复的情况本来就极为罕见（每轮 prompt 都不同），per-caller 分区进一步将稀少的命中机会切碎。

### 4.3 语义规范化只解决格式噪音，不解决内容变化

`normalizeSemanticString` 将多余空白折叠为单空白，`ignoredCacheKeyField` 删除约 20 个会话级 ID 字段。这些优化确实减少了"同一内容因格式差异未命中"的情况，但：

- 它不能把"含义相同、措辞不同"的问题归一（"帮我写一个排序函数"与"写个排序算法"哈希不同）
- 它不能处理对话历史的任何变化
- agent 工作流中没有"措辞稍有不同"这种重复，每轮消息都是全新内容

### 4.4 ds2api 缓存命中率的物理上限

在 LLM agent 工作流（Claude Code / Cline / Cherry Studio 等）中，能触发 ds2api 缓存命中的场景极为有限：

| 场景 | 能否命中 |
|---|---|
| 同一 caller 重发完全相同的历史请求 | **能**（30 分钟内内存层，24 小时内磁盘层）|
| 工具崩溃/网络超时后的客户端重试 | **能**（最常见的合法命中场景）|
| 多轮对话中每一轮的新消息 | **不能**（整体哈希变化）|
| 不同 caller 发送相同内容的请求 | **不能**（per-caller 分区）|
| 措辞稍有不同的语义等价请求 | **不能**（哈希不同）|

这解释了为什么生产环境命中率天然偏低，并随时间下降：随着对话变长、用户数量增加，重发完全相同请求的概率不断降低，而命中率分母（总请求数）却在增大。

### 4.5 结论：两种缓存的定位根本不同

ds2api 的全请求哈希缓存是**幂等重放缓存**（replay cache）——它解决的是"完全相同的请求不需要再次打到上游"。这对于抵御重放攻击、客户端重试有意义，但不是提供商 KV 缓存所解决的问题。

提供商 KV 缓存是**推理计算复用缓存**——它解决的是"共享前缀的注意力计算只需要做一次"。这需要在模型推理引擎内部实现，任何应用层代理都无法在模型外部复制。

**用户看到的"提供商命中率 70-90%"指的是 API 账单上 `prompt_cache_hit_tokens / total_input_tokens` 的比例**，反映的是推理服务器对 KV 张量的复用。它与 ds2api 的"命中"是完全不同的概念，根本不应该拿来比较。

**章节来源**
- [sankalp：提示缓存工作原理](https://sankalp.bearblog.dev/how-prompt-caching-works/)
- [vLLM 自动前缀缓存](https://docs.vllm.ai/en/stable/design/prefix_caching/)
- [Introl：提示缓存基础设施](https://introl.com/blog/prompt-caching-infrastructure-llm-cost-latency-reduction-guide-2025)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

---

## 5. 向量语义缓存方案评估

### 5.1 什么是向量语义缓存

向量语义缓存在全请求哈希缓存的基础上增加一层：将请求的核心文本内容（通常是最后一条 user message）转换为向量嵌入，然后在嵌入空间中进行近邻搜索，如果找到余弦相似度超过阈值 τ 的历史请求，则返回对应的历史响应，无需调用 LLM。

```
新请求 → 嵌入向量 q
→ ANN 搜索：找到最近邻 h（sim(q,h) ≥ τ）
→ 命中：返回 h 对应的历史响应
→ 未命中：调用 LLM，将 (q, 响应) 写入向量 DB
```

### 5.2 主要开源方案对比

#### 5.2.1 GPTCache（Zilliz / Milvus 来源，Python 优先）

GPTCache 是目前最知名的 LLM 语义缓存框架，支持多种嵌入模型（OpenAI text-embedding-ada-002、ONNX 本地模型等）和多种向量存储（Milvus、Faiss、Qdrant、Redis 等）。

**工作机制**：将每条请求（或对话的最后一条 user 消息）嵌入向量，存入向量数据库，查询时做 ANN 搜索，相似度超过阈值则返回缓存响应。

**发表数据**（arXiv:2411.05276，不同工作类型下的命中率）：

| 工作负载类型 | 命中率 | 正确率 |
|---|---|---|
| 订单与物流查询（高重复性） | 68.8% | 97.3% |
| 技术支持 / Python 编程 | 67.0% | 95.1% |
| 购物查询（高语义变异性）| 61.6% | 92.5% |

论文使用余弦相似度阈值 0.8（通过 0.6–0.9 范围实验得出"最优平衡点"）。

**主要缺陷**：
1. **Python 优先**：无官方 Go SDK，需通过 gRPC 或 HTTP 调用 Milvus/Qdrant；对 ds2api 这类 Go 单进程网关引入较大集成成本
2. **假阳性风险**：相似度阈值过低会返回语义相近但答案不同的历史响应（特别是 coding 场景，不同问题可能嵌入向量接近，但正确答案差异极大）
3. **嵌入漂移**：嵌入模型升级后，新向量与旧向量的相似度计算失效，需要重建索引
4. **对长对话效果差**：研究（vCache arXiv:2502.03771）明确指出语义缓存"对单轮、短到中等上下文的交互有效"，对长上下文多轮对话效果有限

#### 5.2.2 Redis Vector / Redis Search

Redis 的 `HNSW`（分层可导航小世界图）和 `FLAT` 索引支持向量相似度搜索，并通过 LangCache 集成了语义缓存功能。

**Go SDK**：`github.com/redis/go-redis/v9` 原生支持向量搜索命令（`FT.SEARCH` 与 vector 字段）。

**优势**：Redis 已被广泛部署，低延迟（sub-100ms ANN 查询），与现有缓存/限流逻辑可共存，适合已使用 Redis 的项目。

**劣势**：ds2api 当前架构**没有 Redis**，为语义缓存引入 Redis 增加了独立服务依赖（容器、持久化、监控、升级路径）。对于 4,230 条缓存条目的规模而言，这是严重的过度设计。

#### 5.2.3 pgvector（PostgreSQL 扩展）

在 PostgreSQL 中添加 `vector` 列类型，支持余弦相似度、L2 距离、内积三种度量，支持 HNSW 和 IVFFlat 索引，Go 通过 `pgx` 或 `database/sql` 集成。

**优势**：零额外服务依赖（若已有 Postgres），备份/监控复用现有方案。

**劣势**：ds2api 当前架构基于 SQLite，不使用 PostgreSQL。**引入 pgvector 意味着引入整个 PostgreSQL 依赖**，这是比引入 Redis 更大的栈变更。对于当前规模完全不合理。

#### 5.2.4 专用向量数据库（Qdrant / Milvus / Weaviate / Chroma）

这些是独立服务，需要单独容器化部署，有各自的持久化、监控和运维需求。

| 方案 | 适合场景 | ds2api 适配性 |
|---|---|---|
| Qdrant | 高性能 HNSW，Go gRPC 客户端可用 | 独立服务，过重 |
| Milvus | 分布式，亿级向量 | 架构不匹配 |
| Weaviate | 内置模型支持 | Python 生态为主 |
| Chroma | 开发者友好，规模偏小 | Python 优先，Go 无官方 SDK |

**统一结论：对 4,230 条条目、单主机 SQLite 架构的 ds2api 而言，这些方案全部过度设计。**

#### 5.2.5 sqlite-vec / sqlite-vss（最接近 ds2api 现有架构）

`sqlite-vec`（`github.com/asg017/sqlite-vec`）是 `sqlite-vss` 的继任者，以 SQLite 扩展形式提供 KNN 向量搜索。

- **Go 绑定**：`go get github.com/asg017/sqlite-vec/bindings/go`
- **CGO 要求**：是，C 语言实现，需要 CGO 编译
- **状态**：pre-v1，明确标注会有 breaking changes，不适合生产使用
- **能力**：向量存储 + KNN 搜索，不含嵌入生成（需配合 sqlite-rembed 或外部 API）
- **性能**：定位为"足够快"（"fast enough"），没有 HNSW 图，为暴力扫描或简单分区，对大规模（>10 万条）效率较差

**对 ds2api 的评估**：sqlite-vec 的架构方向是正确的（与现有 SQLite 栈对齐），但 pre-v1 状态意味着不能依赖其 API 稳定性。更根本的问题是：向量搜索本身需要嵌入生成，而嵌入生成的延迟和成本需要单独核算。

### 5.3 ds2api 实际工作负载对语义缓存的适配性分析

ds2api 的主要调用方是 **Claude Code / Cline / Cherry Studio / Codex** 等 LLM coding agent，其请求特征如下：

| 特征 | 值 |
|---|---|
| 对话长度 | 通常 10–100+ 轮 |
| 每轮输入 token 数 | 5,000–200,000+（含完整代码库上下文）|
| 请求间重复性 | 低——每轮都包含新的代码修改、新的错误信息、新的工具调用结果 |
| 语义等价重复请求出现率 | 极低——coding 问题高度情境依赖，几乎每个请求都是唯一的 |

**语义缓存对此工作负载的适用性**：

研究文献（arXiv:2502.03771 / vCache）明确指出语义缓存适合"单轮、短到中等上下文"的交互，对长上下文多轮对话**效果有限**。具体原因：

1. **嵌入向量只反映 last user message**：100K token 的对话历史无法整体嵌入，只能对最后一条消息嵌入。而相同的"帮我修复这个 bug"在不同代码上下文下正确答案完全不同——嵌入相似但答案迥异，是典型假阳性。
2. **coding 任务假阳性代价高**：相似度 0.8 的语义缓存可能将"写一个冒泡排序"的历史答案返回给"写一个快速排序"的新请求。在普通 Q&A 中这不严重，但在 coding agent 工作流中返回错误代码会直接导致任务失败。
3. **嵌入延迟不可忽视**：本地 ONNX 嵌入约 10–50ms，远程 API（OpenAI text-embedding-3-small）约 100–800ms。对于低命中率工作负载（<15%），每次 miss 都付出了额外的嵌入延迟，但没有获得收益。

**例外场景——语义缓存有意义的路径**：

| 路径 | 原因 |
|---|---|
| `/v1/embeddings` | 嵌入请求本身：相同文本返回相同向量，纯粹幂等，但当前全哈希缓存已经完全覆盖 |
| `/v1/messages/count_tokens` | 纯计算，无生成，幂等性强，全哈希缓存足够 |
| 极短单轮 Q&A（< 1K token，非 agent 上下文）| 语义相似请求重复率较高，但 ds2api 的调用方几乎全是 agent |

**章节来源**
- [GPT 语义缓存论文 arXiv:2411.05276](https://arxiv.org/html/2411.05276v3)
- [vCache arXiv:2502.03771](https://arxiv.org/html/2502.03771v5)
- [sqlite-vec GitHub](https://github.com/asg017/sqlite-vec)
- [pgvector vs Qdrant 对比](https://encore.dev/articles/pgvector-vs-qdrant)

---

## 6. 分层缓存设计建议

### 6.1 核心结论

**不应该切换到向量数据库语义缓存作为主要策略**。原因如下：

1. ds2api 的工作负载（长对话 coding agent）是语义缓存收益最低的场景之一
2. 语义缓存解决的是"措辞不同、意义相同"的重复请求，coding agent 几乎没有这类重复
3. 引入嵌入服务和向量 DB 会显著增加架构复杂度和运维负担
4. 当前的低命中率是结构性的（全请求哈希 vs. 不断变化的对话历史），不是可以通过语义相似度弥补的问题

**应该做的事情**：优化现有全哈希缓存的定位（正确理解其职责），并添加少量精准的辅助能力。

### 6.2 分层设计方案

```
┌─────────────────────────────────────────────────────────────────┐
│                     Layer 0: 请求过滤                           │
│  已有：CacheableRequest() 路径过滤                              │
│  建议新增：streaming 请求主动跳过（stream=true 不值得缓存）      │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                   Layer 1: 精确哈希缓存（保留现有）              │
│  职责：幂等重放——客户端重试、工具 crash 重发                    │
│  优化：调整 per-caller 策略（见 6.3）                           │
│  TTL：内存 30 min / 磁盘 24 h（v1.0.12 已调整，保持）          │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│              Layer 2（仅选定路径）: 嵌入相似度缓存              │
│  适用路径：/v1/embeddings（文本嵌入请求，极高幂等性）           │
│  不适用：/v1/chat/completions, /v1/messages（coding agent）     │
│  实现：本地 ONNX（轻量 MiniLM）+ SQLite 向量存储（未来）        │
└─────────────────────────────────────────────────────────────────┘
```

### 6.3 第一优先级：命中率可观测性

**当前问题**：无法区分"因每轮 prompt 变化导致的结构性 miss"和"因 TTL 过期、容量驱逐、per-caller 分区导致的可优化 miss"。

**建议立即添加的指标**：

```go
// 在 Stats() 中扩展：
"hit_rate_percent":          hits * 100 / (hits + misses),
"cacheable_hit_rate_percent": hits * 100 / (hits + stores),

// 新增：分请求路径的命中率
"hits_by_path": map[string]int64{ "/v1/embeddings": ..., "/v1/chat/completions": ... },
"misses_by_path": map[string]int64{...},

// 新增：内存 vs 磁盘命中分布
"memory_hit_rate": memoryHits * 100 / hits,
"disk_hit_rate":   diskHits * 100 / hits,
```

只有先把命中率按路径分解，才能知道优化哪里有价值。预期数据：`/v1/embeddings` 命中率应远高于 `/v1/chat/completions`，验证不同路径的差异分布。

### 6.4 第二优先级：per-caller 分区策略调整

**当前设计**：每个 `CallerID` 完全隔离，防止不同用户的响应互相污染。这有安全意义（不同用户不应获得他人的对话响应），但对非个人化内容是浪费。

**建议**：对 `/v1/embeddings` 路径，考虑引入"内容哈希 + 路径"的组合 key，**不含 owner**。嵌入请求的结果与调用者无关，相同文本的嵌入向量对所有调用者都相同。

```go
// 仅针对 /v1/embeddings 的特殊键策略：
if path == "/v1/embeddings" {
    // 不含 owner，允许跨 caller 共享
    key = requestKey(r, "", rawBody)
} else {
    key = requestKey(r, owner, rawBody)
}
```

**预期收益**：`/v1/embeddings` 命中率从 per-caller 隔离下的接近 0（N 个 caller 各自预热），升至接近全量去重后的命中率。

### 6.5 第三优先级：Streaming 请求明确跳过

Streaming 响应（`stream: true`）的缓存捕获非常复杂（需要重组 SSE 流），且 agent 工作流几乎全部使用 streaming。建议明确记录 streaming bypass 率：

```go
// 在 recordUncacheable 中增加 reason = "streaming"
// 在 Stats() 中暴露 uncacheable_streaming 计数
```

这有助于理解 uncacheable 请求的实际组成。

### 6.6 第四优先级（未来）：嵌入相似度缓存（仅 /v1/embeddings）

如果可观测性数据显示 `/v1/embeddings` 的命中率在跨 caller 去分区后仍不理想（意味着相同文本在 24 小时内被不同 caller 重复请求），可以考虑语义缓存：

**方案选择**：使用本地轻量 ONNX 嵌入模型（all-MiniLM-L6-v2，384 维，约 10–20ms CPU 延迟）+ SQLite 暴力近邻扫描（对 < 10K 条目，线性扫描 < 1ms）。

**具体实现思路**：

```go
// 新建 internal/embedcache/cache.go
type EmbedCache struct {
    db     *sql.DB     // 复用现有 SQLite 基础设施
    model  *onnx.Model // 本地 ONNX 嵌入模型
    thresh float32     // 余弦相似度阈值，建议 0.97（coding 场景需高精度）
}

// 表结构（SQLite）：
// CREATE TABLE embed_cache (
//   id       INTEGER PRIMARY KEY,
//   path     TEXT NOT NULL,
//   content_hash TEXT NOT NULL,  -- 精确哈希（快速路径）
//   embedding BLOB,              -- 384 维 float32
//   response BLOB,               -- gzip 压缩响应
//   expires_at INTEGER
// );
// CREATE INDEX idx_path ON embed_cache(path);
```

**why 阈值建议 0.97 而非 0.80**：arXiv:2411.05276 的 0.80 阈值针对客服/Q&A 场景，假阳性代价低（最多返回略不准确的答案）。对于嵌入 API，0.97 更保守，排除"接近但不完全相同"的文本嵌入被复用。

**成本估算**：
- 本地 ONNX 嵌入（all-MiniLM-L6-v2）：约 10–20ms / 请求，0 API 成本
- SQLite 线性扫描 10K 条目：< 1ms
- 嵌入存储：10K 条 × 384 × 4 bytes ≈ 15 MB，与现有 22 MB 磁盘缓存量级相当

**不建议**对 `/v1/chat/completions` 或 `/v1/messages` 做向量语义缓存，原因见 5.3。

### 6.7 TTL 和容量建议（保持现有设置）

当前 v1.0.12 已将 TTL 调整为内存 30 分钟、磁盘 24 小时，这对 client-retry 场景已经足够。进一步拉长 TTL 的边际收益递减（大多数 agent 对话在 24 小时内完成），不建议调整。

| 参数 | 当前值 | 建议 |
|---|---|---|
| `memory_ttl` | 30 min | 保持 |
| `disk_ttl` | 24 h | 保持 |
| `memory_max_bytes` | 3.8 GB | 保持 |
| `disk_max_bytes` | 16 GB | 保持 |
| `max_body_bytes` | 64 MB | 保持 |

### 6.8 第一次迭代的最小可行步骤

按 ROI 排序：

1. **（1–2 天）添加按路径分解的命中率指标** → 获得数据基线，验证假设
2. **（0.5 天）对 `/v1/embeddings` 移除 per-caller 分区** → 立即提升嵌入请求命中率，零架构风险
3. **（0.5 天）在 Stats() 中暴露 streaming bypass 计数** → 理解 uncacheable 请求实际组成
4. **（1 周，可选）** 如果步骤 1 数据显示 `/v1/embeddings` 请求量大且跨 caller 重复高，引入本地 ONNX + SQLite 向量扫描

**章节来源**
- [嵌入 API 延迟基准](https://nixiesearch.substack.com/p/benchmarking-api-latency-of-embedding)
- [vCache](https://arxiv.org/html/2502.03771v5)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

---

## 7. 结论与下一步

### 核心结论

| 问题 | 结论 |
|---|---|
| 为什么提供商命中率高？ | 推理服务器内 KV Cache 前缀复用，不是应用层哈希缓存，不可与 ds2api 直接比较 |
| ds2api 命中率为何随时间下降？ | 全请求哈希缓存对每轮变化的对话历史永远 miss；随对话数量增加，完全重复请求的概率下降 |
| 应该切换向量语义缓存吗？ | **不应该**作为主要策略；ds2api 的 coding agent 工作负载是语义缓存收益最低的场景 |
| 应该怎么做？ | 保留现有全哈希缓存（正确定位为幂等重放缓存），优先建立可观测性，针对 `/v1/embeddings` 单独优化 |

### 下一步行动（按优先级）

| 优先级 | 行动 | 预期收益 | 工作量 |
|---|---|---|---|
| P0 | 按路径分解命中率指标，写入 Stats() 和 admin dashboard | 建立数据基线，支撑后续所有决策 | 1–2 天 |
| P1 | `/v1/embeddings` 路径移除 per-caller 分区 | 直接提升嵌入缓存命中率 | 0.5 天 |
| P2 | 暴露 streaming / uncacheable_reason 细分统计 | 理解 miss 构成，识别可优化空间 | 0.5 天 |
| P3（可选）| 如 P0 数据证明值得：引入本地 ONNX + SQLite 向量扫描，仅作用于 `/v1/embeddings` | 语义去重，提升跨 caller 复用率 | 1 周 |

**明确不做的事情**：
- 不引入 Redis、PostgreSQL、Qdrant、Milvus 等独立服务依赖
- 不对 `/v1/chat/completions` 或 `/v1/messages` 实施向量语义缓存
- 不试图在应用层复制提供商的 KV Cache 前缀复用机制（这在模型外部不可能实现）

**章节来源**
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [docs/storage-cache.md](file://docs/storage-cache.md)
- [Anthropic 提示缓存](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [OpenAI 提示缓存](https://developers.openai.com/api/docs/guides/prompt-caching)
- [DeepSeek 上下文缓存](https://api-docs.deepseek.com/guides/kv_cache)
