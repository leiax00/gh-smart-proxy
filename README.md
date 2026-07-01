# gh-smart-proxy

一个轻量 GitHub HTTPS 代理：URL Secret 鉴权、GitHub 域名白名单、清理 Authorization、缓存头策略、简单每 IP 限流，并内置一个类似 gh-proxy 的简单 Web 页面。

## 启动

```bash
docker compose up -d --build
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

注意：Secret 不会写进 HTML 页面源码，只在浏览器本地用于拼接 URL。

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
