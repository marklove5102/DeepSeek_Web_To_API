# 部署运维

<cite>
**本文档引用的文件**
- [Dockerfile](file://Dockerfile)
- [docker-compose.yml](file://docker-compose.yml)
- [.env.example](file://.env.example)
- [scripts/build-release-archives.sh](file://scripts/build-release-archives.sh)
- [.github/workflows/release-artifacts.yml](file://.github/workflows/release-artifacts.yml)
- [cmd/DeepSeek_Web_To_API/main.go](file://cmd/DeepSeek_Web_To_API/main.go)
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
