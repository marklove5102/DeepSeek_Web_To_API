# DeepSeek_Web_To_API 部署指南

语言 / Language: [中文](DEPLOY.md) | [English](DEPLOY.en.md)

本指南基于当前 Go 代码库，详细说明各种部署方式。

本页导航：[文档总索引](./README.md)｜[架构说明](./ARCHITECTURE.md)｜[接口文档](../API.md)｜[测试指南](./TESTING.md)

---

## 目录

- [部署方式优先级建议](#部署方式优先级建议)
- [前置要求](#0-前置要求)
- [一、下载 Release 构建包](#一下载-release-构建包)
- [二、Docker / GHCR 部署](#二docker--ghcr-部署)
- [三、本地源码运行](#三本地源码运行)
- [四、反向代理（Nginx）](#四反向代理nginx)
- [五、Linux systemd 服务化](#五linux-systemd-服务化)
- [六、部署后检查](#六部署后检查)
- [七、发布前进行本地回归](#七发布前进行本地回归)

---

## 部署方式优先级建议

推荐按以下顺序选择部署方式：

1. **下载 Release 构建包运行**：最省事，产物已编译完成，最适合大多数用户。
2. **Docker / GHCR 镜像部署**：适合需要容器化、编排或云环境部署。
3. **本地源码运行 / 自行编译**：适合开发、调试或需要自行修改代码的场景。

---

## 0. 前置要求

| 依赖 | 最低版本 | 说明 |
| --- | --- | --- |
| Go | 1.26+ | 编译后端 |
| Node.js | `20.19+` 或 `22.12+` | 仅在需要本地构建 WebUI 时 |
| npm | 随 Node.js 提供 | 安装 WebUI 依赖 |

配置来源（任选其一）：

- **文件方式**：`config.json`（推荐本地/Docker 使用）
- **环境变量方式**：`DEEPSEEK_WEB_TO_API_CONFIG_JSON`（适合只读或云平台注入场景，支持 JSON 字符串或 Base64 编码，也可以直接写原始 JSON）

统一建议（最优实践）：

```bash
cp config.example.json config.json
# 编辑 config.json
```

配置模板配套提供 `config.example.json.Annotation`。它是字段注释说明，不参与运行；用于核对 `config.example.json` 中每个部署项的含义、类型、默认值、范围和可选环境变量覆盖。

建议把 `config.json` 作为唯一配置源：
- 本地运行：直接读 `config.json`
- Docker / 云平台：从 `config.json` 生成 `DEEPSEEK_WEB_TO_API_CONFIG_JSON`（Base64）注入环境变量

---

## 一、下载 Release 构建包

仓库内置 GitHub Actions 工作流：`.github/workflows/release-artifacts.yml`

- **触发条件**：默认仅在 Release `published` 时自动触发；也支持在 Actions 页面手动 `workflow_dispatch`，并填写 `release_tag` 复跑/补发
- **构建产物**：多平台二进制压缩包、Linux Docker 镜像导出包 + `sha256sums.txt`
- **容器镜像发布**：仅发布到 GHCR（`ghcr.io/meow-calculations/deepseek-web-to-api`）

| 平台 | 架构 | 文件格式 |
| --- | --- | --- |
| Linux | amd64, arm64, armv7 | `.tar.gz` |
| macOS | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

每个压缩包包含：

- `deepseek-web-to-api` 可执行文件（Windows 为 `deepseek-web-to-api.exe`）
- `static/admin/`（WebUI 构建产物）
- `config.example.json`、`.env.example`
- `README.MD`、`README.en.md`、`LICENSE`

### 使用步骤

```bash
# 1. 下载对应平台的压缩包
# 2. 解压
tar -xzf deepseek-web-to-api_<tag>_linux_amd64.tar.gz
cd deepseek-web-to-api_<tag>_linux_amd64

# 3. 配置
cp config.example.json config.json
# 编辑 config.json

# 4. 启动
./deepseek-web-to-api
```

### 维护者发布步骤

1. 在 GitHub 创建并发布 Release（带 tag，如 `vX.Y.Z`）
2. 等待 Actions 工作流 `Release Artifacts` 完成
3. 在 Release 的 Assets 下载对应平台压缩包

---

## 二、Docker / GHCR 部署

### 2.1 基本步骤

```bash
# 拉取预编译镜像
docker pull ghcr.io/meow-calculations/deepseek-web-to-api:latest

# 复制部署层覆盖模板和单一运行配置
cp .env.example .env
cp config.example.json config.json

# 编辑 config.json（请改成你的强密码），至少设置：
#   admin.key
#   admin.jwt_secret
#   keys / accounts
# .env 只用于部署层覆盖；如需修改宿主机端口，可设置：
#   DEEPSEEK_WEB_TO_API_HOST_PORT=6011

# 启动
docker-compose up -d

# 查看日志
docker-compose logs -f
```

默认 `docker-compose.yml` 直接使用 `ghcr.io/meow-calculations/deepseek-web-to-api:latest`，并把宿主机 `6011` 映射到容器内的 `5001`。如果你希望直接对外暴露 `5001`，请设置 `DEEPSEEK_WEB_TO_API_HOST_PORT=5001`（或者手动调整 `ports` 配置）。
Compose 模板会默认设置 `DEEPSEEK_WEB_TO_API_CONFIG_PATH=/data/config.json` 并挂载 `./config.json:/data/config.json`，优先避免 `/app` 只读带来的配置持久化问题。若未设置 `DEEPSEEK_WEB_TO_API_CONFIG_PATH` 且运行目录是 `/app`，程序会在 `/data` 存在时优先使用 `/data/config.json`，否则回退到 `/app/config.json`，兼容旧容器升级。

如需固定版本，也可以直接拉取指定 tag：

```bash
docker pull ghcr.io/meow-calculations/deepseek-web-to-api:v3.0.0
```

### 2.2 更新

```bash
docker-compose up -d --build
```

### 2.3 Docker 架构说明

`Dockerfile` 提供两条构建路径：

1. **本地/开发默认路径（`runtime-from-source`）**：三阶段构建（WebUI 构建 + Go 构建 + 运行阶段）。
2. **Release 路径（`runtime-from-dist`）**：发布工作流先生成 tag 命名的 Release 压缩包，再把 Linux 产物复制成 `dist/docker-input/linux_amd64.tar.gz` / `linux_arm64.tar.gz`；Docker 构建阶段直接消费这些输入，不再重复执行 `npm build`/`go build`。

Release 路径可确保 Docker 镜像与 release 压缩包使用同一套产物，减少重复构建带来的差异。

容器内启动命令：`/usr/local/bin/deepseek-web-to-api`，默认暴露端口 `5001`。

### 2.4 开发环境

```bash
docker-compose -f docker-compose.dev.yml up
```

开发模式特性：
- 源代码挂载（修改即生效）
- `LOG_LEVEL=DEBUG`
- 不自动重启

### 2.5 健康检查

Docker Compose 已配置内置健康检查：

```yaml
healthcheck:
  test: ["CMD", "/usr/local/bin/busybox", "wget", "-qO-", "http://localhost:${PORT:-5001}/healthz"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 10s
```

### 2.6 Docker 常见排查

如果容器日志正常但面板打不开，优先检查：

1. **端口是否一致**：`PORT` 改成非 `5001` 时，访问地址也要改成对应端口（如 `http://localhost:8080/admin`）。
2. **开发 compose 的 WebUI 静态文件**：`docker-compose.dev.yml` 使用 `go run` 开发镜像，不会在容器内自动安装 Node.js；若仓库里没有 `static/admin`，`/admin` 会返回 404。可先在宿主机构建一次：`./scripts/build-webui.sh`。

### 2.7 Zeabur 一键部署（Dockerfile）

仓库提供 `zeabur.yaml` 模板，可在 Zeabur 上一键部署：

Zeabur 模板地址：`https://zeabur.com/templates/L4CFHP`

部署要点：

- **端口**：服务默认监听 `5001`，模板会固定设置 `PORT=5001`。
- **配置持久化**：模板挂载卷 `/data`，并设置 `DEEPSEEK_WEB_TO_API_CONFIG_PATH=/data/config.json`；在管理台导入配置后，会写入并持久化到该路径。
- **`open /app/config.json: permission denied`**：说明当前实例在尝试把运行时 token 持久化到只读路径。建议显式设置 `DEEPSEEK_WEB_TO_API_CONFIG_PATH=/data/config.json` 并挂载持久卷；如果使用 `DEEPSEEK_WEB_TO_API_CONFIG_JSON` 且不需要运行时落盘，可保持环境变量模式。
- **构建版本号**：Zeabur / 普通 `docker build` 默认不需要传 `BUILD_VERSION`；镜像会优先使用该构建参数，未提供时自动回退到仓库根目录的 `VERSION` 文件。
- **首次登录**：部署完成后访问 `/admin`，使用 `/data/config.json` 中的 `admin.key` 登录（建议首次登录后自行更换为强密码）。

---

## 三、本地源码运行

### 3.1 常用环境变量

| 变量 | 说明 | 默认值 |
| --- | --- | --- |
| `DEEPSEEK_WEB_TO_API_ACCOUNT_MAX_INFLIGHT` | 每账号并发上限 | `2` |
| `DEEPSEEK_WEB_TO_API_ACCOUNT_MAX_QUEUE` | 等待队列上限 | `recommended_concurrency` |
| `DEEPSEEK_WEB_TO_API_GLOBAL_MAX_INFLIGHT` | 全局并发上限 | `recommended_concurrency` |
| `DEEPSEEK_WEB_TO_API_ENV_WRITEBACK` | 检测到 `DEEPSEEK_WEB_TO_API_CONFIG_JSON` 时自动写入 `DEEPSEEK_WEB_TO_API_CONFIG_PATH`，并在成功后转为文件模式（`1/true/yes/on`） | 关闭 |
| `DEEPSEEK_WEB_TO_API_HTTP_TOTAL_TIMEOUT_SECONDS` | DeepSeek 非流式 HTTP 请求总超时；推荐改 `config.json` 的 `server.http_total_timeout_seconds`，此变量仅作部署覆盖 | `7200` |
| `DEEPSEEK_WEB_TO_API_STREAM_IDLE_TIMEOUT_SECONDS` | 流式输出已有内容后的空闲 / 思考超时 | `1800` |
| `DEEPSEEK_WEB_TO_API_STREAM_KEEPALIVE_INTERVAL_SECONDS` | 流式保活间隔 | `5` |
| `DEEPSEEK_WEB_TO_API_STREAM_MAX_KEEPALIVE_NO_CONTENT` | 首个有效内容前最多保活次数（默认 `5s × 360 = 1800s`） | `360` |
| `DEEPSEEK_WEB_TO_API_CHAT_HISTORY_SQLITE_PATH` | 服务器端对话历史 SQLite 文件路径；推荐改 `config.json` 的 `storage.chat_history_sqlite_path`，旧 `storage.chat_history_path` 仅作为首次导入来源 | `data/chat_history.sqlite` |
| `DEEPSEEK_WEB_TO_API_RAW_STREAM_SAMPLE_ROOT` | raw stream 样本保存/读取根目录；推荐改 `config.json` 的 `storage.raw_stream_sample_root` | `tests/raw_stream_samples` |
| `DEEPSEEK_WEB_TO_API_RESPONSE_CACHE_DIR` | 协议响应 gzip 磁盘缓存目录；推荐改 `config.json` 的 `cache.response.dir`。所有 OpenAI / Claude / Gemini 兼容 POST 协议共用，内存层固定 5 分钟且最多 3.8GB，磁盘层固定 4 小时且最多 16GB | `data/response_cache` |

### 3.2 运行时行为配置（通过 Admin API 设置）

部分运行时行为无法通过环境变量直接配置，需要在部署后通过 Admin API 设置，例如：

- **自动删除会话模式** (`auto_delete.mode`)：支持 `none` / `single` / `all`，默认为 `none`。可通过 `PUT /admin/settings` 更新。
- **每账号并发上限** (`account_max_inflight`)：环境变量已支持，但也可通过 Admin API 热更新。
- **全局并发上限** (`global_max_inflight`)：同上。

Claude Code 的主 Agent / 子 Agent 请求会先生成会话亲和 key：稳定会话信息作为 root，显式 `agent_id` / `subagent_id` / `task_id` 或首个 user task 作为 lane。相同 lane 的多轮请求会回到同一账号以保留上游上下文，不同 lane 会进入账号池轮转；如果子 Agent 数量超过 `账号数量 × account_max_inflight`，超出的流式请求会等待可用槽位，而不是继续压到同一账号。

详细说明参见 [API.md](../API.md#admin-接口) 中 `/admin/settings` 部分。

### 3.3 基本步骤

```bash
# 克隆仓库
git clone https://github.com/Meow-Calculations/DeepSeek_Web_To_API.git
cd deepseek-web-to-api

# 复制并编辑配置
cp config.example.json config.json
# 使用你喜欢的编辑器打开 config.json，填入：
#   - keys: 你的 API 访问密钥
#   - accounts: DeepSeek 账号（email 或 mobile + password）

# 启动服务
go run ./cmd/DeepSeek_Web_To_API
```

默认本地访问地址是 `http://127.0.0.1:5001`；服务实际绑定 `0.0.0.0:5001`，可通过 `PORT` 环境变量覆盖。
协议响应缓存默认写入 `config.json` 的 `cache.response.dir`（示例值为 `data/response_cache`），磁盘文件使用 gzip 压缩并保留 4 小时，磁盘总量最多 16GB；内存层保留 5 分钟且最多 3.8GB。如需把缓存落到单独磁盘或容器持久卷，推荐修改 `cache.response.dir`；`DEEPSEEK_WEB_TO_API_RESPONSE_CACHE_DIR` 只保留为部署层覆盖。
聊天历史默认写入 `storage.chat_history_sqlite_path` 指向的 SQLite 文件。完整请求详情、上下文和响应正文会在写入前 gzip 压缩到 `detail_blob`；旧版本残留的未压缩 `detail_json` 会在服务启动时分批压缩并尝试 `VACUUM`，因此升级后首次启动可能比平时慢一些。如果磁盘空间不足导致 `VACUUM` 失败，新写入仍会压缩，但库文件实际缩小要等下次 compact 成功。
运行时写回的配置、聊天历史 SQLite、响应缓存、raw sample 与 testsuite artifact 都可能包含账号、token、请求体或响应体，程序会按敏感数据默认使用 `0700` 目录和 `0600` 文件权限。部署到共享主机或容器卷时，请不要把这些目录映射到公开可读路径。

### 3.4 WebUI 构建

本地首次启动时，若 `static/admin/` 不存在，服务会自动尝试构建 WebUI（需要 Node.js/npm；缺依赖时会先执行 `npm ci`，再执行 `npm run build -- --outDir static/admin --emptyOutDir`）。

你也可以手动构建：

```bash
./scripts/build-webui.sh
```

或手动执行：

```bash
cd webui
npm ci
npm run build
# 产物输出到 static/admin/
```

通过环境变量控制自动构建行为：

```bash
# 强制关闭自动构建
DEEPSEEK_WEB_TO_API_AUTO_BUILD_WEBUI=false go run ./cmd/DeepSeek_Web_To_API

# 强制开启自动构建
DEEPSEEK_WEB_TO_API_AUTO_BUILD_WEBUI=true go run ./cmd/DeepSeek_Web_To_API
```

### 3.5 编译为二进制文件

```bash
go build -o deepseek-web-to-api ./cmd/DeepSeek_Web_To_API
./deepseek-web-to-api
```

---

## 四、反向代理（Nginx）

如果在 Nginx 后部署，**必须关闭缓冲**以保证 SSE 流式响应正常工作：

```nginx
location / {
    proxy_pass http://127.0.0.1:5001;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_cache off;
    chunked_transfer_encoding on;
    tcp_nodelay on;
}
```

如果需要 HTTPS，可以在 Nginx 层配置 SSL 证书：

```nginx
server {
    listen 443 ssl;
    server_name api.example.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:5001;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding on;
        tcp_nodelay on;
    }
}
```

---

## 五、Linux systemd 服务化

### 6.1 安装

```bash
# 将编译好的二进制文件和相关文件复制到目标目录
sudo mkdir -p /opt/deepseek-web-to-api
sudo cp deepseek-web-to-api config.json /opt/deepseek-web-to-api/
sudo mkdir -p /opt/deepseek-web-to-api/static
sudo cp -r static/admin /opt/deepseek-web-to-api/static/admin
```

### 6.2 创建 systemd 服务文件

```ini
# /etc/systemd/system/deepseek-web-to-api.service

[Unit]
Description=DeepSeek_Web_To_API (Go)
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/deepseek-web-to-api
Environment=PORT=5001
Environment=DEEPSEEK_WEB_TO_API_CONFIG_PATH=/opt/deepseek-web-to-api/config.json
ExecStart=/opt/deepseek-web-to-api/deepseek-web-to-api
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### 6.3 常用命令

```bash
# 加载服务配置
sudo systemctl daemon-reload

# 设置开机自启
sudo systemctl enable deepseek-web-to-api

# 启动服务
sudo systemctl start deepseek-web-to-api

# 查看状态
sudo systemctl status deepseek-web-to-api

# 查看日志
sudo journalctl -u deepseek-web-to-api -f

# 重启服务
sudo systemctl restart deepseek-web-to-api

# 停止服务
sudo systemctl stop deepseek-web-to-api
```

---

## 六、部署后检查

无论使用哪种部署方式，启动后建议依次检查：

```bash
# 1. 存活探针
curl -s http://127.0.0.1:5001/healthz
# 预期: {"status":"ok"}

# 2. 就绪探针
curl -s http://127.0.0.1:5001/readyz
# 预期: {"status":"ready"}

# 3. 模型列表
curl -s http://127.0.0.1:5001/v1/models
# 预期: {"object":"list","data":[...]}（包含 `*-nothinking` 变体）

# 4. 管理台页面（如果已构建 WebUI）
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:5001/admin
# 预期: 200

# 5. 测试 API 调用
curl http://127.0.0.1:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}]}'
```

安全回归可参考 [security-audit-2026-05-02.md](security-audit-2026-05-02.md)，至少复跑 `gosec ./...`、`govulncheck ./...`、前端 `npm audit` 和敏感信息扫描。

---

## 七、发布前进行本地回归

建议在发布前执行完整的端到端测试集（使用真实账号）：

```bash
./tests/scripts/run-live.sh
```

可自定义参数：

```bash
go run ./cmd/DeepSeek_Web_To_API-tests \
  --config config.json \
  --admin-key admin \
  --out artifacts/testsuite \
  --timeout 120 \
  --retries 2
```

测试集自动执行内容：

- ✅ 语法/构建/单测 preflight
- ✅ 隔离副本配置启动服务（不污染原始 `config.json`）
- ✅ 真实调用场景验证（OpenAI/Claude/Admin/并发/toolcall/流式）
- ✅ 全量请求与响应日志落盘（用于故障复盘）

详细测试集说明参阅 [TESTING.md](TESTING.md)。PR 前的固定本地门禁以 [TESTING.md](TESTING.md#pr-门禁--pr-gates) 为准。
