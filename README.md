# gh-smart-proxy

English | [简体中文](README.zh-CN.md)

A lightweight GitHub HTTPS proxy with URL-secret authentication, a GitHub host allow-list, credential header stripping, cache-friendly response headers, simple per-IP rate limiting, and a built-in web page similar to `gh-proxy`.

The public URL format is:

```text
https://gh.example.com/<PROXY_SECRET>/https://github.com/owner/repo/...
```

Leaving `PROXY_SECRET` empty enables open-proxy mode:

```text
https://gh.example.com/https://github.com/owner/repo/...
```

## Features

- Proxies GitHub releases, archives, raw files, gists, and Git smart HTTP traffic.
- Keeps a secret path segment in front of proxied URLs to avoid exposing a public open proxy by default.
- Follows GitHub release asset redirects server-side, like `hunshcn/gh-proxy`, so clients receive the final file response directly.
- Rewrites proxyable GitHub redirects back through the proxy prefix.
- Strips `Authorization`, `Proxy-Authorization`, `Cookie`, and hop-by-hop headers before forwarding to GitHub.
- Adds cache headers suitable for Cloudflare or another CDN in front of the service.
- Includes a small web UI for generating proxy, clone, and jsDelivr links.

## Quick Start

With Docker Compose:

```bash
PROXY_SECRET=your-long-random-secret docker compose -f deploy/docker-compose.yml up -d --build
```

Without Docker:

```bash
PROXY_SECRET=your-long-random-secret go run ./cmd/gh-smart-proxy
```

The deployment files are under `deploy/`. The image and runtime environment are injected externally, and the production network is attached by the CI deployment flow.

## Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `PROXY_SECRET` | optional | URL authentication secret. It also becomes the first path segment in proxy URLs. Empty means open-proxy mode. |
| `ADDR` | `:8080` | Listen address. |
| `RATE_LIMIT` | `120` | Maximum requests per IP within the rate window. |
| `RATE_WINDOW_SECONDS` | `60` | Rate-limit window in seconds. |
| `ALLOWED_HOSTS` | built-in GitHub list | Comma-separated upstream host allow-list. Empty keeps the built-in GitHub hosts. |

You can also bake a default secret into the binary at build time. Runtime `PROXY_SECRET` still takes precedence:

```bash
go build -ldflags="-X gh-smart-proxy/internal/config.Secret=your-secret" ./cmd/gh-smart-proxy
```

### Optional Config File

Use `CONFIG_PATH` to load a YAML config file. A sample is available at `configs/config.sample.yaml`:

```bash
cp configs/config.sample.yaml configs/config.yaml
CONFIG_PATH=configs/config.yaml PROXY_SECRET=override-value go run ./cmd/gh-smart-proxy
```

Precedence:

```text
environment variables > config file > link-time default > built-in defaults
```

`configs/config.yaml` is ignored by git.

`allowed_hosts` in the config file, or `ALLOWED_HOSTS` in the environment, replaces the default allow-list as a whole. It is not appended. This is useful when adding GitHub Enterprise hosts. Leave it empty to keep the built-in GitHub list.

### Open-Proxy Mode

When `PROXY_SECRET` is empty, authentication is disabled and proxy URLs become:

```text
https://gh.example.com/https://github.com/owner/repo/...
```

Only use this locally, on a private network, or behind trusted protection such as Cloudflare or Nginx Proxy Manager. Otherwise anyone can use your server as a bandwidth relay.

## Usage

Clone a repository:

```bash
git clone https://gh.example.com/<PROXY_SECRET>/https://github.com/hunshcn/gh-proxy.git
```

Download an archive:

```bash
curl -I https://gh.example.com/<PROXY_SECRET>/https://github.com/hunshcn/gh-proxy/archive/refs/heads/main.zip
```

Download a release asset:

```bash
curl -L -o ax-linux-x86_64 \
  https://gh.example.com/<PROXY_SECRET>/https://github.com/owner/repo/releases/download/v1.0.0/file.tar.gz
```

## Web UI

Open the service root in a browser:

```text
https://gh.example.com/
```

The page can:

- Accept an original GitHub URL.
- Accept your `PROXY_SECRET` locally in the browser.
- Generate the proxied URL.
- Generate a `git clone` command for repository URLs.
- Generate jsDelivr CDN URLs for supported raw/blob/tree file URLs.
- Open or download through the proxy.

The secret is never rendered into the HTML source by the server. If entered, it is stored only in the browser's `localStorage` so the page can remember it next time.

## Project Layout

```text
├── cmd/gh-smart-proxy/     # Entrypoint: load config and start the server
├── internal/
│   ├── config/             # Config resolution: Config + Load()
│   ├── httputil/           # Shared HTTP helpers
│   ├── proxy/              # Reverse proxy, auth, target parsing, redirects, cache headers
│   ├── ratelimit/          # Per-IP rate limiting
│   ├── server/             # Routes and *http.Server assembly
│   └── web/                # Built-in web page
├── deploy/                 # Dockerfile and docker-compose.yml
└── go.mod
```

## CI/CD Deployment

The repository includes a Gitea-compatible GitHub Actions workflow at `.github/workflows/ci.yml`.

The workflow can:

- Build the image with buildx and registry cache.
- Push `:latest` to the Gitea package registry.
- SSH into the deployment host.
- Download `docker-compose.yml` from the Gitea API.
- Write a `docker-compose.override.yml` that attaches the external `self` network.
- Inject `IMAGE` and `PROXY_SECRET` from Gitea Actions variables/secrets.
- Run `docker compose pull && docker compose up -d`.

### Repository Variables

| Variable | Default | Description |
|---|---|---|
| `REGISTRY_HOST` | required | Container registry host, configured in repository Variables. |
| `GITEA_HOST` | same as `REGISTRY_HOST` | Gitea API host for downloading compose files. |
| `DEPLOY_HOST` | required | Deployment server hostname or IP. |
| `DEPLOY_SSH_USER` | required | SSH user. |
| `DEPLOY_SSH_PORT` | `22` | SSH port. |
| `CONTAINER_NAME` | `gh-smart-proxy` | Container name and default deployment directory suffix. |
| `DEPLOY_BASE_DIR` | required | Base directory on the deployment server. The workflow deploys to `<DEPLOY_BASE_DIR>/<CONTAINER_NAME>`. |
| `DOCKERHUB_MIRROR` | required by the included workflow | Docker Hub mirror used during image builds. Configure it in repository Variables. |

### Repository Secrets

| Secret | Description |
|---|---|
| `CI_TOKEN` | Gitea token with `package:write`, `package:read`, and `repo` permissions. |
| `SSH_PRIVATE_KEY` | Private key for the deployment server. |
| `PROXY_SECRET` | URL authentication secret injected into the deployed container. Empty means open-proxy mode. |

### One-Time Server Setup

```bash
echo "<CI_TOKEN>" | docker login <REGISTRY_HOST> -u <REGISTRY_USER> --password-stdin
mkdir -p <DEPLOY_BASE_DIR>/<CONTAINER_NAME>
```

You do not need to place a `.env` file on the server unless you want to override defaults manually.

## Nginx Proxy Manager

Proxy Host settings:

| Setting | Value |
|---|---|
| Forward Hostname/IP | `gh-smart-proxy` |
| Forward Port | `8080` |
| Scheme | `http` |
| SSL | Enable Let's Encrypt and Force SSL |

Do not enable Basic Auth in front of this app if you want Git clients and download tools to work normally.

## Cloudflare Cache Suggestions

- Bypass cache for `/info/refs`, `git-upload-pack`, and `git-receive-pack`.
- Cache GitHub downloads such as `/releases/download/`, `/archive/`, `raw.githubusercontent.com`, `codeload.github.com`, `objects.githubusercontent.com`, `release-assets.githubusercontent.com`, and `github-releases.githubusercontent.com`.

The proxy itself sets conservative `Cache-Control` and `CDN-Cache-Control` headers, but the final behavior depends on your CDN rules.

Create these Cloudflare Cache Rules in order. Replace `gh.example.com` with your proxy hostname.

### 1. Bypass Git Smart HTTP

Expression:

```text
(http.host eq "gh.example.com" and (
  http.request.uri.path contains "/info/refs" or
  http.request.uri.path contains "git-upload-pack" or
  http.request.uri.path contains "git-receive-pack"
))
```

Action:

```text
Cache eligibility: Bypass cache
```

### 2. Cache GitHub Download Assets

Expression:

```text
(http.host eq "gh.example.com" and (
  http.request.uri.path contains "/releases/download/" or
  http.request.uri.path contains "/archive/" or
  http.request.uri.path contains "raw.githubusercontent.com" or
  http.request.uri.path contains "codeload.github.com" or
  http.request.uri.path contains "objects.githubusercontent.com" or
  http.request.uri.path contains "release-assets.githubusercontent.com" or
  http.request.uri.path contains "github-releases.githubusercontent.com"
))
```

Action:

```text
Cache eligibility: Eligible for cache
Edge TTL: 7 days
Browser TTL: Respect origin header
```

### 3. Bypass Other Proxy Traffic

This rule is optional, but useful if the proxy host is dedicated to this service.

Expression:

```text
(http.host eq "gh.example.com")
```

Action:

```text
Cache eligibility: Bypass cache
```

If you use a secret path such as `https://gh.example.com/secret-token/https://github.com/...`, you do not need to match the secret. Match the GitHub path fragments after it.
