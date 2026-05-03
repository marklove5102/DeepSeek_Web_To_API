# DeepSeek_Web_To_API

语言 / Language: [中文](README.MD) | [English](README.en.md)

DeepSeek_Web_To_API 是一个自托管的 Go 网关，把 DeepSeek Web 侧的会话能力封装为 OpenAI、Claude 和 Gemini 风格的 API，并提供 React 管理台用于账号、密钥、代理、缓存、历史记录和运行状态管理。

当前仓库以 **Go 后端 + React/Vite 管理台 + SQLite 本地运行数据** 为主，文档完全按当前源码和部署方式编写。

## 核心能力

- OpenAI 兼容：`/v1/models`、`/v1/chat/completions`、`/v1/responses`、`/v1/files`、`/v1/embeddings`。
- Claude 兼容：`/anthropic/v1/messages`、`/v1/messages`、`/messages`、`count_tokens`。
- Gemini 兼容：`/v1beta/models/{model}:generateContent`、`streamGenerateContent` 以及 `/v1/models/{model}:*`。
- 多账号托管：API Key 命中 `config.json` 后进入账号池，自动选择账号、刷新 token、处理并发槽位。
- 直通 token：调用方 token 不在 `config.json` 的 keys 中时，按 DeepSeek token 直通。
- 响应缓存：内存缓存默认 5 分钟、上限 3.8GB；磁盘缓存默认 4 小时、上限 16GB，使用 gzip。
- 历史记录：SQLite 保存服务端对话记录，默认保留 2 万条，详情内容 gzip 压缩。
- 管理台：`/admin` 提供账号、代理、密钥、运行设置、对话历史、测试器和总览指标。
- 运维能力：`/healthz`、`/readyz`、安全响应头、CORS、JSON UTF-8 入站校验、优雅退出。

## 快速开始

### 本地源码运行

```bash
cp config.example.json config.json
npm ci --prefix webui
npm run build --prefix webui
go run ./cmd/DeepSeek_Web_To_API
```

默认监听 `0.0.0.0:5001`，打开 `http://127.0.0.1:5001/admin` 进入管理台。

### Docker Compose

```bash
cp config.example.json config.json
cp .env.example .env
docker compose up -d
```

Compose 默认把宿主机 `${DEEPSEEK_WEB_TO_API_HOST_PORT:-6011}` 映射到容器内 `5001`，并把 `./config.json` 挂载到 `/data/config.json`。

### 二进制构建

```bash
npm ci --prefix webui
npm run build --prefix webui
go build -trimpath -ldflags="-s -w" -o deepseek-web-to-api ./cmd/DeepSeek_Web_To_API
```

运行前确保同目录或工作目录存在 `config.json`，或者通过 `DEEPSEEK_WEB_TO_API_CONFIG_PATH` 指定配置路径。

## 最小配置

复制 `config.example.json` 后至少修改：

- `keys` / `api_keys`：给外部客户端使用的 API Key。
- `accounts`：DeepSeek Web 账号，支持邮箱或手机号。
- `admin.key` 或 `admin.password_hash`：管理台登录凭据。
- `admin.jwt_secret`：管理台 JWT 签名密钥。
- `server.bind_addr`：如果前面有 Caddy/Nginx 反代，建议改为 `127.0.0.1`。

`.env` 只用于部署层覆盖，例如 Docker 端口、配置路径或平台注入。业务配置应优先写入 `config.json`。

## API 使用示例

OpenAI Chat Completions：

```bash
curl http://127.0.0.1:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key-1" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.5",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": true
  }'
```

Claude Messages：

```bash
curl http://127.0.0.1:5001/anthropic/v1/messages \
  -H "x-api-key: your-api-key-1" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "你好"}],
    "stream": true
  }'
```

更多接口见 [API.md](API.md)。

## 文档

- [文档导航](docs/README.md)
- [项目总览](docs/Project%20Overview/Project%20Overview.md)
- [架构设计](docs/Architecture%20Design/Architecture%20Design.md)
- [配置说明](docs/configuration.md)
- [部署运维](docs/deployment.md)
- [API 兼容系统](docs/API%20Compatibility%20System/API%20Compatibility%20System.md)
- [Prompt 兼容流程](docs/prompt-compatibility.md)
- [存储与缓存](docs/storage-cache.md)
- [安全说明](docs/security.md)
- [测试与交付](docs/Testing%20and%20Delivery/Testing%20and%20Delivery.md)

## 开发验证

仓库 CI 与本地 PR Gate 使用同一批命令：

```bash
./scripts/lint.sh
./tests/scripts/check-refactor-line-gate.sh
./tests/scripts/run-unit-all.sh
npm run build --prefix webui
```

仅修改文档时，至少运行 `git diff --check` 和旧项目残留扫描。

## 免责声明

本项目仅用于学习、研究、个人实验和内部验证。使用者需要自行确认部署、调用和账号使用行为是否符合相关服务条款、平台规则和法律法规。维护者不对账号风险、数据丢失、第三方限制或任何直接/间接损失负责。
