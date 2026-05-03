# 存储与缓存

<cite>
**本文档引用的文件**
- [internal/chathistory/store.go](file://internal/chathistory/store.go)
- [internal/chathistory/sqlite_store.go](file://internal/chathistory/sqlite_store.go)
- [internal/chathistory/sqlite_detail.go](file://internal/chathistory/sqlite_detail.go)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [config.example.json](file://config.example.json)
</cite>

## 目录

1. [简介](#简介)
2. [项目结构](#项目结构)
3. [核心组件](#核心组件)
4. [架构总览](#架构总览)
5. [详细组件分析](#详细组件分析)
6. [性能考虑](#性能考虑)
7. [故障排查指南](#故障排查指南)
8. [结论](#结论)

## 简介

当前项目有两类本地状态：SQLite 对话历史和协议响应缓存。SQLite 用于管理台历史记录与运行分析，默认保留 2 万条；响应缓存用于减少相同协议请求重复打到上游，默认内存 5 分钟、磁盘 4 小时，并对磁盘内容启用 gzip。

**章节来源**
- [internal/chathistory/store.go](file://internal/chathistory/store.go)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

## 项目结构

```mermaid
graph TB
subgraph "Chat History"
STORE["internal/chathistory<br/>Store facade"]
SQLITE["data/chat_history.sqlite<br/>SQLite WAL"]
DETAIL["detail_blob<br/>gzip detail"]
LEGACY["data/chat_history.json<br/>legacy import source"]
end
subgraph "Response Cache"
CACHE["internal/responsecache<br/>middleware"]
MEM["memory map<br/>5 min default"]
DISK["data/response_cache<br/>json.gz files"]
end
STORE --> SQLITE
SQLITE --> DETAIL
LEGACY --> SQLITE
CACHE --> MEM
CACHE --> DISK
```

**图表来源**
- [internal/chathistory/sqlite_store.go](file://internal/chathistory/sqlite_store.go)
- [internal/chathistory/sqlite_detail.go](file://internal/chathistory/sqlite_detail.go)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

**章节来源**
- [config.example.json](file://config.example.json)

## 核心组件

- `chat_history` 表：存储摘要字段、状态、模型、账号、耗时、状态码、usage、详情版本。
- `detail_blob`：保存 gzip 压缩后的完整详情，`detail_json` 只用于旧数据迁移。
- `chat_history_meta`：保存保留上限、版本和修订号。
- `responsecache.Cache`：在路由中间件层读取请求体、计算缓存键、命中回放、未命中捕获响应并写入缓存。
- 磁盘缓存文件：以 `.json.gz` 保存，包含状态码、响应头、响应体、创建时间和过期时间。

**章节来源**
- [internal/chathistory/sqlite_store.go](file://internal/chathistory/sqlite_store.go)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

## 架构总览

```mermaid
sequenceDiagram
participant Client as Client
participant Router as Router Middleware
participant Cache as Response Cache
participant Handler as Protocol Handler
participant History as SQLite History
participant DS as DeepSeek
Client->>Router: POST protocol request
Router->>Cache: cacheable path?
Cache-->>Client: cache hit replay
Cache->>Handler: cache miss
Handler->>History: start history item
Handler->>DS: upstream request
DS-->>Handler: stream or response
Handler->>History: update detail and status
Handler-->>Cache: captured 2xx response
Cache->>Cache: memory + gzip disk store
Cache-->>Client: response
```

**图表来源**
- [internal/server/router.go](file://internal/server/router.go)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)
- [internal/chathistory/sqlite_write.go](file://internal/chathistory/sqlite_write.go)

**章节来源**
- [internal/server/router.go](file://internal/server/router.go)
- [internal/httpapi/historycapture/capture.go](file://internal/httpapi/historycapture/capture.go)

## 详细组件分析

### SQLite 历史记录

默认路径为 `data/chat_history.sqlite`。启动时会：

- 创建目录和 SQLite 连接。
- 设置 WAL、`synchronous=NORMAL`、`busy_timeout=5000`。
- 建表和索引。
- 从旧 `data/chat_history.json` 首次导入。
- 将上次未完成请求标记为停止。
- 压缩旧的未压缩详情，并执行 checkpoint/VACUUM。

历史保留上限由数据库 meta 保存，默认和最大值都是 `20000`。

### 响应缓存

默认缓存路径为 `data/response_cache`。缓存覆盖：

- OpenAI Chat Completions、Responses、Embeddings。
- Claude Messages、CountTokens。
- Gemini GenerateContent、StreamGenerateContent。

缓存键包含调用方、规范化路径、查询参数、影响输出的请求头和规范化 JSON 请求体。部分缓存控制字段会从 JSON key 中忽略，以提高相同内容请求的命中率。

**章节来源**
- [internal/chathistory/sqlite_store.go](file://internal/chathistory/sqlite_store.go)
- [internal/chathistory/sqlite_import.go](file://internal/chathistory/sqlite_import.go)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

## 性能考虑

- SQLite 单连接配合 WAL，适合本地嵌入式记录和管理台读取。
- 历史详情使用 `gzip.BestCompression`，节省磁盘空间，读取详情时按需解压。
- 响应缓存的内存层有总字节数上限，磁盘层会按过期和容量删除旧文件。
- 大响应超过 `cache.response.max_body_bytes` 时不会进入缓存。

**章节来源**
- [internal/chathistory/sqlite_detail.go](file://internal/chathistory/sqlite_detail.go)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

## 故障排查指南

- 历史记录数量不是 2 万：检查管理台保留策略或 `chat_history_meta.limit` 是否被改成 10、20、50、100 或关闭。
- SQLite 文件过大：确认当前版本已启动过，旧未压缩详情会在启动时分批压缩并 VACUUM。
- 缓存命中率低：检查请求体中是否存在每次变化的字段、是否显式 `no-cache`、是否跨 API Key/调用方、是否路径或模型 alias 不一致。
- 磁盘缓存未生效：确认 `cache.response.dir` 可写，且响应为 2xx、响应体未超过上限。

**章节来源**
- [internal/chathistory/store.go](file://internal/chathistory/store.go)
- [internal/responsecache/cache.go](file://internal/responsecache/cache.go)

## 结论

SQLite 和 gzip 响应缓存是当前版本的核心运行态能力：前者服务管理台与问题回溯，后者降低重复请求成本。两者都使用本地文件系统，需要在部署时持久化 `data/` 或至少持久化配置指定的历史与缓存路径。

**章节来源**
- [config.example.json](file://config.example.json)
