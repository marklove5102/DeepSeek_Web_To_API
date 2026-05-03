# 更新日志

## 2026-05-02

### 安全

- 为 SQLite 聊天历史 gzip 详情增加解压后大小上限，避免异常压缩数据导致内存放大。
- 为 DeepSeek 非流式 JSON 响应读取增加解压后 16MB 上限，避免上游异常响应占用过量内存。
- 为 Claude/Gemini 经 OpenAI 兼容通道代理的请求体增加 `http.MaxBytesReader` 限制，超限返回 413。
- 修复 Admin metrics 中 `uint -> int64` 统计值转换的溢出风险。

### 稳定性

- 总览成功率统计排除用户侧 `401 / 403 / 502 / 504 / 524`，这些状态单独计数，不再拖低服务成功率。
- 总览缓存指标新增可缓存未命中率，并默认展示可缓存命中/未命中口径，避免不可缓存上游错误污染缓存判断。
- 为无效 direct token 增加短期负缓存，避免未配置或失效的客户端 token 反复穿透上游并持续拉低成功率。
- 规范化响应缓存请求体 key，消除 JSON 字段顺序、空白和无效顶层元数据导致的缓存碎片，提升重复请求命中概率。
- 响应缓存新增总命中率、可缓存命中率、不可缓存未命中原因和写入次数统计，避免客户端取消、上游错误等不可缓存请求污染缓存判断。
- 响应缓存 key 递归忽略 Claude / Claude Code 的 `cache_control`、`cache_reference`、`context_management`、`cache_edits` 和 `betas` 等传输字段，同时保留真实 JSON `null` 语义。

### 文档

- 刷新安全审计报告、源码全景分析、Admin WebUI 文档和 SQLite 迁移分析，统一当前 SQLite + gzip 详情存储事实。
- 记录本轮 `gosec`、`govulncheck`、`npm audit`、密钥扫描、SQL 注入扫描和内存放大扫描结果。
