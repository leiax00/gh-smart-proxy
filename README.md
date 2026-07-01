# gh-smart-proxy

一个轻量 GitHub HTTPS 代理：URL Secret 鉴权、GitHub 域名白名单、清理 Authorization、缓存头策略、简单每 IP 限流，并内置一个类似 gh-proxy 的简单 Web 页面。

## 启动

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

部署文件（`Dockerfile`、`docker-compose.yml`）都在 `deploy/` 下，与源码分开。本地直接跑：

```bash
PROXY_SECRET=你的长随机secret go run ./cmd/gh-smart-proxy
```

## 配置

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `PROXY_SECRET` | 可选 | URL 鉴权 secret，同时作为前缀出现在代理 URL 里；留空=开放代理模式 |
| `ADDR` | `:8080` | 监听地址 |
| `RATE_LIMIT` | `120` | 单个 IP 在窗口内最大请求数 |
| `RATE_WINDOW_SECONDS` | `60` | 限流窗口（秒） |
| `ALLOWED_HOSTS` | 内置默认 | 逗号分隔的上游域名白名单；留空=用内置 GitHub 列表 |

也可以在编译时把默认 secret 烧进二进制（运行时 `PROXY_SECRET` 仍优先）：

```bash
go build -ldflags="-X gh-smart-proxy/internal/config.Secret=your-secret" ./cmd/gh-smart-proxy
```

### 配置文件（可选）

也可以用 YAML 文件管理配置，通过 `CONFIG_PATH` 指向它（模板见 `configs/config.sample.yaml`）：

```bash
cp configs/config.sample.yaml configs/config.yaml
CONFIG_PATH=configs/config.yaml PROXY_SECRET=覆盖值 go run ./cmd/gh-smart-proxy
```

优先级：**环境变量 > 配置文件 > 编译时默认 > 内置默认**。`configs/config.yaml` 已在 `.gitignore` 里。

`allowed_hosts`（或环境变量 `ALLOWED_HOSTS`，逗号分隔）会**整体替换**默认 GitHub 白名单（不是追加），常用于加上自托管 GitHub Enterprise 域名。留空则保持内置默认列表。

### 开放代理模式

`PROXY_SECRET` 留空时不鉴权，代理 URL 变成 `https://你的域名/https://github.com/...`，启动会打印警告。仅适合本地 / 内网，或前端已有 Cloudflare / NPM 保护时使用——否则任何人都能借你的服务器烧带宽。

## 项目结构

```
├── cmd/gh-smart-proxy/     # 入口：只做配置加载 + 启动
├── internal/
│   ├── config/             # 配置：Config + Load()
│   ├── httputil/           # 共享 HTTP 工具（ClientIP / Scheme / StripHopHeaders）
│   ├── proxy/              # 反向代理：鉴权、目标解析、缓存头
│   ├── ratelimit/          # 每 IP 限流
│   ├── server/             # 组装路由 + *http.Server
│   └── web/                # 内置 Web 页面
├── deploy/                 # Dockerfile + docker-compose.yml
└── go.mod
```

## Web 页面

浏览器打开：

```text
https://gh.example.com/
```

页面功能：

- 输入原始 GitHub URL
- 输入你的 `PROXY_SECRET`
- 自动生成代理 URL
- 一键复制代理地址
- 一键复制 `git clone` 命令
- 直接通过页面打开 / 下载文件

注意：Secret 不会写进 HTML 页面源码，只在浏览器本地用于拼接 URL。输入后会保存在本浏览器的 localStorage 里，下次自动填入（可用页面上的「清除记住的 Secret」按钮清除）。

## 命令行使用

```bash
git clone https://gh.example.com/<PROXY_SECRET>/https://github.com/hunshcn/gh-proxy.git
curl -I https://gh.example.com/<PROXY_SECRET>/https://github.com/hunshcn/gh-proxy/archive/refs/heads/main.zip
```

## Nginx Proxy Manager

Proxy Host:

- Forward Hostname/IP: `gh-smart-proxy`
- Forward Port: `8080`
- Scheme: `http`
- SSL: 开启 Let's Encrypt / Force SSL

不要再开 Basic Auth。

## Cloudflare 缓存建议

- `/info/refs`、`git-upload-pack`：Bypass Cache
- `/releases/download/`、`raw.githubusercontent.com`、`codeload.github.com`、`objects.githubusercontent.com`：Cache Everything
