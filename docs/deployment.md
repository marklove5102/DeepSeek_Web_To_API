# 部署运维

<cite>
**本文档引用的文件**
- [Dockerfile](file://Dockerfile)
- [docker-compose.yml](file://docker-compose.yml)
- [.env.example](file://.env.example)
- [scripts/build-release-archives.sh](file://scripts/build-release-archives.sh)
- [.github/workflows/release-artifacts.yml](file://.github/workflows/release-artifacts.yml)
- [cmd/DeepSeek_Web_To_API/main.go](file://cmd/DeepSeek_Web_To_API/main.go)
- [.cnb.yml](file://.cnb.yml)
</cite>

## 目录

1. [简介](#简介)
2. [项目结构](#项目结构)
3. [核心组件](#核心组件)
4. [架构总览](#架构总览)
5. [详细组件分析](#详细组件分析)
6. [故障排查指南](#故障排查指南)
7. [结论](#结论)

## 简介

DeepSeek_Web_To_API 支持源码运行、二进制运行、Docker Compose 和 GitHub Release/GHCR 镜像发布。生产部署推荐使用二进制或 Docker，并放在 Caddy/Nginx 等反代后面。

> **v1.0.3 ~ v1.0.12 部署侧变更**
>
> - **v1.0.3 CNB CI 增加 PR Docker 构建检查**：`.cnb.yml` 的 `pull_request:` 阶段新增"Docker build (PR check)"步骤，合并请求提交时自动执行 `docker build --no-cache` 验证镜像可构建。Cherry-picked 自 CNB PR #12（已以"已采纳"语义关闭，无需合并冲突解析）。
> - **v1.0.3 重复违规自动拉黑 + 安全 WebUI 修复**：新增 `autoBanTracker`，滑动窗口内超阈值的 IP 自动写入 `safety_ips.blocked_ips`；WebUI 安全策略控制台同步修复（之前列表全空）。详见 [安全说明](file://docs/security.md)。
> - **v1.0.6 Pro 模型 120s 超时已修**：`cmd/DeepSeek_Web_To_API/main.go` 的 `http.Server.{ReadTimeout,WriteTimeout}` 改用 `Store.HTTPTotalTimeout()`（默认 7200s）。CF 套餐自身 100s 限制由 CF 控制，本服务无法绕过；流式调用首字节秒级返回不会触发 CF 524，非流式 + 长推理建议改流式或升级 CF Business+。
> - **v1.0.8 WebUI 刷新 401 已修**：管理台 JWT 从 `sessionStorage` 改存 `localStorage`（避免 Firefox 硬刷新或部分浏览器恢复标签页清空 token），同时保留 sessionStorage → localStorage 自动迁移以平滑升级旧用户。
> - **v1.0.9 文件上传速率限制已根除**：`server.remote_file_upload_enabled = false`（默认），inline file 与 history transcript 都内联到对话上下文，不再调上游 `upload_file`。生产抽样显示之前 2 小时 4116 次 `upload_file failed` → 升级后 0 次。如有账号配额冗余想恢复上传，设置 `DEEPSEEK_WEB_TO_API_REMOTE_FILE_UPLOAD_ENABLED=true`。
> - **v1.0.11 5 套独立 SQLite 布局**：`data/{accounts,chat_history,token_usage,safety_words,safety_ips}.sqlite` 各自独立，可单独备份/轮转。
> - **v1.0.12 缓存 TTL 调长**：内存 5 min → 30 min，磁盘 4 h → 24 h。客户端兼容增强：`/api/messages` 路由别名、`/v1/responses/compact` 501 stub、Codex `compaction` input 容忍。

**章节来源**
- [Dockerfile](file://Dockerfile)
- [docker-compose.yml](file://docker-compose.yml)

## 项目结构

```mermaid
graph TB
subgraph "Build"
WEBBUILD["webui<br/>npm run build"]
GOBUILD["cmd/DeepSeek_Web_To_API<br/>go build"]
ARCHIVE["scripts/build-release-archives.sh<br/>多平台压缩包"]
end
subgraph "Runtime"
BINARY["deepseek-web-to-api<br/>二进制"]
DOCKER["ghcr.io/meow-calculations/deepseek-web-to-api<br/>容器镜像"]
CONFIG["data/config.json<br/>可选回写文件"]
ENV[".env<br/>主配置入口"]
DATA["data<br/>配置回写、SQLite 与缓存"]
end
WEBBUILD --> GOBUILD
GOBUILD --> BINARY
GOBUILD --> ARCHIVE
ARCHIVE --> DOCKER
ENV --> BINARY
ENV --> DOCKER
CONFIG --> DATA
BINARY --> DATA
DOCKER --> DATA
```

**图表来源**
- [scripts/build-release-archives.sh](file://scripts/build-release-archives.sh)
- [Dockerfile](file://Dockerfile)
- [docker-compose.yml](file://docker-compose.yml)

**章节来源**
- [scripts/release-targets.sh](file://scripts/release-targets.sh)
- [.github/workflows/release-artifacts.yml](file://.github/workflows/release-artifacts.yml)

## 核心组件

- Docker 镜像：多阶段构建，前端用 Node 构建，后端用 Go 1.26 构建，运行层使用 Debian slim 非 root 用户。
- Compose 模板：镜像来源为 `ghcr.io/meow-calculations/deepseek-web-to-api:latest`，`.env` 注入初始结构化配置，`./data` 保存回写配置、账号 SQLite、历史记录与缓存。
- Release 脚本：构建 Linux、macOS、Windows 多架构压缩包，并复制 `config.example.json`、`.env.example`、README 与静态管理台。
- HTTP Server：默认端口 `5001`，包含读写超时、请求头超时、空闲超时和优雅退出。
- CNB CI（v1.0.3 新增 PR 阶段）：`.cnb.yml` 配置主分支 push 时构建并推送镜像；PR 阶段运行 `docker build --no-cache` 验证可构建性，不推送。

**章节来源**
- [Dockerfile](file://Dockerfile)
- [docker-compose.yml](file://docker-compose.yml)
- [cmd/DeepSeek_Web_To_API/main.go](file://cmd/DeepSeek_Web_To_API/main.go)

## 架构总览

```mermaid
graph TB
subgraph "Internet"
CLIENT["Client SDK / Browser"]
end
subgraph "Reverse Proxy"
PROXY["Caddy or Nginx<br/>TLS and public port"]
end
subgraph "Host"
APP["DeepSeek_Web_To_API<br/>127.0.0.1:5001 recommended"]
CONFIG["data/config.json<br/>可选回写"]
ENV[".env"]
DATA["data/config.json<br/>data/accounts.sqlite<br/>data/chat_history.sqlite<br/>data/response_cache"]
end
CLIENT --> PROXY
PROXY --> APP
ENV --> APP
APP --> CONFIG
APP --> DATA
```

**图表来源**
- [cmd/DeepSeek_Web_To_API/main.go](file://cmd/DeepSeek_Web_To_API/main.go)
- [config.example.json](file://config.example.json)

**章节来源**
- [config.example.json](file://config.example.json)
- [.env.example](file://.env.example)

## 详细组件分析

### Docker Compose

```bash
cp .env.example .env
docker compose up -d
```

关键点：

- 容器内端口保持 `5001`。
- 宿主机端口通过 `.env` 的 `DEEPSEEK_WEB_TO_API_HOST_PORT` 控制。
- 初始结构化配置写在 `.env` 的 `DEEPSEEK_WEB_TO_API_CONFIG_JSON`。
- 持久化目录挂载为 `./data:/app/data`，保存 `config.json` 回写文件、`accounts.sqlite`、历史记录和缓存。

### 二进制部署

```bash
npm ci --prefix webui
npm run build --prefix webui
go build -trimpath -ldflags="-s -w" -o deepseek-web-to-api ./cmd/DeepSeek_Web_To_API
```

生产目录建议包含：

- `deepseek-web-to-api`
- `.env`
- `static/admin`
- `data/`

### CNB CI 流水线（v1.0.3 新增 PR 检查）

`.cnb.yml` 定义两条流水线：

| 触发事件 | 阶段 | 操作 |
|---|---|---|
| `main` 分支 push | Docker build | `docker build -t …:latest .` |
| `main` 分支 push | Docker push | 推送到 CNB 内部 registry |
| Pull Request | Docker build (PR check) | `docker build --no-cache -t …:pr-check .`（不推送） |

PR 阶段使用 `--no-cache` 确保每次都是全量构建，避免层缓存掩盖依赖变更。PR #12 的 `pull_request:` 段落已经 cherry-pick 进主分支（`7620954`），PR 以"已采纳"语义在 CNB 上关闭。

**章节来源**
- [.cnb.yml:1-16](file://.cnb.yml#L1-L16)

### 反代建议

如果外部已经由 Caddy/Nginx 提供 HTTPS 与公网监听，`.env` 内的结构化配置建议：

```json
{
  "server": {
    "bind_addr": "127.0.0.1",
    "port": "5001"
  }
}
```

这样应用只监听本机，公网入口由反代负责。

**章节来源**
- [docker-compose.yml](file://docker-compose.yml)
- [.env.example](file://.env.example)
- [cmd/DeepSeek_Web_To_API/main.go](file://cmd/DeepSeek_Web_To_API/main.go)

## 故障排查指南

- `/admin` 空白或 404：确认 `static/admin/index.html` 存在，或启用 `server.auto_build_webui` 并安装 Node/npm。
- 容器启动后读取不到配置：确认 `.env` 存在、`DEEPSEEK_WEB_TO_API_CONFIG_JSON` 非空，并挂载 `./data:/app/data`。
- 管理台改动重启后丢失：确认 `DEEPSEEK_WEB_TO_API_ENV_WRITEBACK=true` 且 `DEEPSEEK_WEB_TO_API_CONFIG_PATH=/app/data/config.json` 可写。
- 反代后访问失败：确认应用绑定地址、反代 upstream、CORS 头与超时配置。
- 长流式请求中断：检查反代读写超时是否低于业务请求时间。

**章节来源**
- [internal/webui/build.go](file://internal/webui/build.go)
- [cmd/DeepSeek_Web_To_API/main.go](file://cmd/DeepSeek_Web_To_API/main.go)

## 结论

当前部署模型是标准自托管服务：一个 Go 进程、一个 React 静态管理台、一份 `.env` 入口配置和本地 `data/` 目录。生产环境应通过反代暴露公网，并避免让应用直接监听公网端口。

**章节来源**
- [Dockerfile](file://Dockerfile)
- [docker-compose.yml](file://docker-compose.yml)
