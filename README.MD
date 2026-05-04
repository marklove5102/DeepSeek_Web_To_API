# DeepSeek_Web_To_API

语言 / Language: [中文](README.MD) | [English](README.en.md)

DeepSeek_Web_To_API 是一个自托管的 Go 网关，把 DeepSeek Web 侧的会话能力封装为 OpenAI、Claude 和 Gemini 风格的 API，并提供 React 管理台用于账号、密钥、代理、缓存、历史记录和运行状态管理。

当前仓库以 **Go 后端 + React/Vite 管理台 + SQLite 本地运行数据** 为主，文档完全按当前源码和部署方式编写。

## 核心能力

- OpenAI 兼容：`/v1/models`、`/v1/chat/completions`、`/v1/responses`、`/v1/files`、`/v1/embeddings`。
- Claude 兼容：`/anthropic/v1/messages`、`/v1/messages`、`/messages`、`count_tokens`。
- Gemini 兼容：`/v1beta/models/{model}:generateContent`、`streamGenerateContent` 以及 `/v1/models/{model}:*`。
- 多账号托管：API Key 命中 `.env` 中的托管密钥后进入账号池，自动选择账号、刷新 token、处理并发槽位。
- 直通 token：调用方 token 不在托管密钥中时，按 DeepSeek token 直通。
- 响应缓存：内存缓存默认 5 分钟、上限 3.8GB；磁盘缓存默认 4 小时、上限 16GB，使用 gzip。
- 账号与历史记录：账号池单独写入 `data/accounts.sqlite`；对话历史写入 SQLite，默认保留 2 万条，详情内容 gzip 压缩。
- 管理台：`/admin` 提供账号、代理、密钥、运行设置、对话历史、测试器、总览指标和新版本提醒。
- 运维能力：`/healthz`、`/readyz`、安全响应头、CORS、JSON UTF-8 入站校验、优雅退出。

## 快速开始

### 本地源码运行

```bash
cp .env.example .env
npm ci --prefix webui
npm run build --prefix webui
go run ./cmd/DeepSeek_Web_To_API
```

示例 `.env` 默认监听 `127.0.0.1:5001`，打开 `http://127.0.0.1:5001/admin` 进入管理台。

### Docker Compose

```bash
cp .env.example .env
docker compose up -d
```

Compose 默认把宿主机 `${DEEPSEEK_WEB_TO_API_HOST_PORT:-6011}` 映射到容器内 `5001`，并把 `./data` 挂载为配置回写、账号 SQLite、历史记录和缓存目录。

### 二进制构建

```bash
npm ci --prefix webui
npm run build --prefix webui
go build -trimpath -ldflags="-s -w" -o deepseek-web-to-api ./cmd/DeepSeek_Web_To_API
```

运行前确保 `.env` 已配置 `DEEPSEEK_WEB_TO_API_CONFIG_JSON`，或者通过 `DEEPSEEK_WEB_TO_API_CONFIG_PATH` 指定旧版 JSON 配置路径。

## 最小配置

复制 `.env.example` 后至少修改：

- `DEEPSEEK_WEB_TO_API_CONFIG_JSON` 里的 `api_keys`：给外部客户端使用的 API Key。
- `admin.key` 或 `admin.password_hash`：管理台登录凭据。
- `admin.jwt_secret`：管理台 JWT 签名密钥。
- `server.bind_addr`：如果前面有 Caddy/Nginx 反代，建议改为 `127.0.0.1`。
- `DEEPSEEK_WEB_TO_API_ACCOUNTS_SQLITE_PATH`：账号 SQLite 路径，默认 `data/accounts.sqlite`。

账号不再需要写 JSON。进入管理台后，在“批量导入”页面直接粘贴一行一个 `账号:密码`，邮箱和手机号都使用同一种格式，后端会写入独立 SQLite。

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
