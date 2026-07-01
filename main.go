package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"gh-smart-proxy/config"
)

// cfg holds the resolved runtime configuration. It is populated from the
// environment in main(); see package config for resolution order.
var cfg config.Config

var allowedHosts = map[string]bool{
	"github.com":                            true,
	"www.github.com":                        true,
	"raw.githubusercontent.com":             true,
	"gist.githubusercontent.com":            true,
	"codeload.github.com":                   true,
	"objects.githubusercontent.com":         true,
	"release-assets.githubusercontent.com":  true,
	"github-releases.githubusercontent.com": true,
}

type rateState struct {
	count int
	reset time.Time
}

type limiter struct {
	mu     sync.Mutex
	states map[string]*rateState
	limit  int
	window time.Duration
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{states: make(map[string]*rateState), limit: limit, window: window}
}

func (l *limiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.states[ip]
	if !ok || now.After(st.reset) {
		l.states[ip] = &rateState{count: 1, reset: now.Add(l.window)}
		return true
	}
	if st.count >= l.limit {
		return false
	}
	st.count++
	return true
}

type pageData struct {
	BaseURL string
}

func main() {
	cfg.Secret = os.Getenv("PROXY_SECRET")
	if cfg.Secret == "" {
		cfg.Secret = config.Secret
	}

	if cfg.Secret == "" {
		log.Fatal("secret is required")
	}

	addr := env("ADDR", ":8080")
	limit := atoi(env("RATE_LIMIT", "120"), 120)
	window := time.Duration(atoi(env("RATE_WINDOW_SECONDS", "60"), 60)) * time.Second
	lim := newLimiter(limit, window)

	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {},
		ModifyResponse: func(resp *http.Response) error {
			applyCacheHeaders(resp)
			stripHopHeaders(resp.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error: %s %s: %v", r.Method, r.URL.String(), err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   20 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = io.WriteString(w, "ok\n")
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			renderHome(w, r)
			return
		}

		ip := clientIP(r)
		if !lim.allow(ip) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}

		target, err := parseTarget(r, cfg.Secret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		log.Printf("%s %s -> %s", ip, r.Method, target.String())

		r.URL = target
		r.Host = target.Host
		r.RequestURI = ""

		// Very important: never forward proxy auth/basic auth to GitHub.
		r.Header.Del("Authorization")
		r.Header.Del("Proxy-Authorization")
		r.Header.Del("Cookie")
		stripHopHeaders(r.Header)

		r.Header.Set("Host", target.Host)
		r.Header.Set("X-Forwarded-Host", r.Host)
		r.Header.Set("X-Forwarded-Proto", schemeFromRequest(r))

		proxy.ServeHTTP(w, r)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           http.HandlerFunc(h),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("gh-smart-proxy listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

func renderHome(w http.ResponseWriter, r *http.Request) {
	base := publicBaseURL(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := homeTemplate.Execute(w, pageData{BaseURL: base}); err != nil {
		log.Printf("render home: %v", err)
	}
}

func parseTarget(r *http.Request, secret string) (*url.URL, error) {
	prefix := "/" + secret + "/"
	if !strings.HasPrefix(r.URL.EscapedPath(), prefix) && !strings.HasPrefix(r.URL.Path, prefix) {
		return nil, errors.New("forbidden")
	}

	raw := strings.TrimPrefix(r.URL.Path, prefix)
	if raw == "" {
		return nil, errors.New("empty target")
	}

	if !strings.HasPrefix(raw, "https://") {
		return nil, errors.New("target must start with https://")
	}

	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return nil, errors.New("invalid target")
	}
	target.RawQuery = r.URL.RawQuery

	host := strings.ToLower(target.Hostname())
	if !allowedHosts[host] {
		return nil, fmt.Errorf("host not allowed: %s", host)
	}
	return target, nil
}

func applyCacheHeaders(resp *http.Response) {
	path := resp.Request.URL.Path
	host := strings.ToLower(resp.Request.URL.Hostname())

	// Git smart HTTP should not be cached.
	if strings.Contains(path, "/info/refs") || strings.Contains(path, "git-upload-pack") || strings.Contains(path, "git-receive-pack") || resp.Request.Method != http.MethodGet {
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Set("CDN-Cache-Control", "no-store")
		return
	}

	cacheable := host == "raw.githubusercontent.com" ||
		host == "codeload.github.com" ||
		host == "objects.githubusercontent.com" ||
		host == "release-assets.githubusercontent.com" ||
		host == "github-releases.githubusercontent.com" ||
		(host == "github.com" && strings.Contains(path, "/releases/download/")) ||
		(host == "github.com" && strings.Contains(path, "/archive/refs/"))

	if cacheable && resp.StatusCode >= 200 && resp.StatusCode < 400 {
		resp.Header.Set("Cache-Control", "public, max-age=86400, s-maxage=604800")
		resp.Header.Set("CDN-Cache-Control", "public, max-age=604800")
	} else {
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Set("CDN-Cache-Control", "no-store")
	}
}

func stripHopHeaders(h http.Header) {
	for _, k := range []string{"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(k)
	}
}

func clientIP(r *http.Request) string {
	if cf := r.Header.Get("CF-Connecting-IP"); cf != "" {
		return cf
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func publicBaseURL(r *http.Request) string {
	scheme := schemeFromRequest(r)
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	return scheme + "://" + host
}

func schemeFromRequest(r *http.Request) string {
	if s := r.Header.Get("X-Forwarded-Proto"); s != "" {
		return s
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func env(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func atoi(s string, def int) int {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

var _ = secureEqual

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GitHub Proxy</title>
  <style>
    :root { color-scheme: light dark; }
    body { margin: 0; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #0f172a; color: #e5e7eb; }
    .wrap { max-width: 920px; margin: 0 auto; padding: 48px 20px; }
    .card { background: rgba(15, 23, 42, .85); border: 1px solid rgba(148, 163, 184, .25); border-radius: 22px; padding: 28px; box-shadow: 0 24px 80px rgba(0,0,0,.35); }
    h1 { margin: 0 0 10px; font-size: 36px; letter-spacing: -0.04em; }
    p { color: #94a3b8; line-height: 1.7; }
    label { display: block; margin: 18px 0 8px; font-weight: 700; }
    input { width: 100%; box-sizing: border-box; padding: 14px 16px; border-radius: 14px; border: 1px solid rgba(148,163,184,.35); background: #020617; color: #e5e7eb; font-size: 15px; outline: none; }
    input:focus { border-color: #38bdf8; box-shadow: 0 0 0 3px rgba(56,189,248,.16); }
    .row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
    .buttons { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 16px; }
    button, a.button { appearance: none; border: 0; cursor: pointer; text-decoration: none; border-radius: 999px; padding: 11px 16px; font-weight: 800; color: #0f172a; background: #38bdf8; }
    button.secondary, a.secondary { background: #334155; color: #e5e7eb; }
    pre { white-space: pre-wrap; word-break: break-all; background: #020617; border: 1px solid rgba(148,163,184,.25); border-radius: 14px; padding: 14px; color: #bfdbfe; }
    .hint { font-size: 13px; color: #64748b; }
    .ok { color: #86efac; }
    .bad { color: #fca5a5; }
    .footer { margin-top: 18px; font-size: 13px; color: #64748b; }
    @media (max-width: 700px) { .row { grid-template-columns: 1fr; } h1 { font-size: 30px; } }
  </style>
</head>
<body>
  <main class="wrap">
    <section class="card">
      <h1>GitHub Proxy</h1>
      <p>输入 GitHub 原始地址，生成带代理的地址。Secret 只在你的浏览器里使用，不会出现在页面源码里。</p>

      <div class="row">
        <div>
          <label for="secret">Proxy Secret</label>
          <input id="secret" type="password" autocomplete="off" placeholder="你的 PROXY_SECRET">
        </div>
        <div>
          <label for="base">代理域名</label>
          <input id="base" value="{{.BaseURL}}" spellcheck="false">
        </div>
      </div>

      <label for="raw">原始 GitHub 地址</label>
      <input id="raw" spellcheck="false" placeholder="https://github.com/owner/repo/releases/download/v1.0/file.zip">
      <p class="hint">支持 github.com、raw.githubusercontent.com、codeload.github.com、objects.githubusercontent.com 等白名单域名。</p>

      <label>代理地址</label>
      <pre id="out">等待输入...</pre>

      <label>Git clone 命令</label>
      <pre id="clone">等待输入仓库地址...</pre>

      <div class="buttons">
        <button onclick="copyProxy()">复制代理地址</button>
        <button class="secondary" onclick="copyClone()">复制 git clone</button>
        <button class="secondary" onclick="openProxy()">打开 / 下载</button>
      </div>

      <p id="msg" class="hint"></p>
      <div class="footer">建议：页面可以公开，但 Secret 不要公开；Cloudflare/NPM 前面继续加 WAF 和限流。</div>
    </section>
  </main>
<script>
const allowed = new Set([
  'github.com', 'www.github.com', 'raw.githubusercontent.com', 'gist.githubusercontent.com',
  'codeload.github.com', 'objects.githubusercontent.com', 'release-assets.githubusercontent.com',
  'github-releases.githubusercontent.com'
]);
const $ = id => document.getElementById(id);
function cleanBase(v) { return v.replace(/\/+$/, ''); }
function build() {
  const secret = $('secret').value.trim();
  const base = cleanBase($('base').value.trim());
  const raw = $('raw').value.trim();
  const msg = $('msg');
  msg.textContent = '';
  msg.className = 'hint';
  if (!secret || !base || !raw) {
    $('out').textContent = '等待输入...';
    $('clone').textContent = '等待输入仓库地址...';
    return '';
  }
  let u;
  try { u = new URL(raw); } catch(e) {
    $('out').textContent = '原始地址不是合法 URL';
    $('clone').textContent = '等待输入仓库地址...';
    msg.textContent = '请粘贴完整的 https://github.com/... 地址';
    msg.className = 'bad';
    return '';
  }
  if (u.protocol !== 'https:' || !allowed.has(u.hostname)) {
    $('out').textContent = '该域名不在代理白名单内';
    $('clone').textContent = '等待输入仓库地址...';
    msg.textContent = '为了防止开放代理，只允许 GitHub 相关域名。';
    msg.className = 'bad';
    return '';
  }
  const proxied = base + '/' + encodeURIComponent(secret) + '/' + raw;
  $('out').textContent = proxied;
  if (u.hostname === 'github.com' || u.hostname === 'www.github.com') {
    let cloneURL = proxied;
    if (!cloneURL.endsWith('.git') && /^\/[^\/]+\/[^\/]+\/?$/.test(u.pathname)) cloneURL += '.git';
    $('clone').textContent = 'git clone ' + cloneURL;
  } else {
    $('clone').textContent = '这个地址不是仓库地址，通常用于 curl/wget/浏览器下载。';
  }
  msg.textContent = '已生成。下载 Release / raw / archive 时，Cloudflare 命中缓存后能显著省 VPS 流量。';
  msg.className = 'ok';
  return proxied;
}
async function copyText(t) {
  if (!t || t.startsWith('等待') || t.includes('不是合法') || t.includes('不在代理')) return;
  try { await navigator.clipboard.writeText(t); $('msg').textContent = '已复制'; $('msg').className = 'ok'; }
  catch(e) { $('msg').textContent = '复制失败，可以手动复制上面的内容'; $('msg').className = 'bad'; }
}
function copyProxy() { copyText($('out').textContent); }
function copyClone() { copyText($('clone').textContent); }
function openProxy() { const u = build(); if (u) window.open(u, '_blank', 'noopener'); }
['secret','base','raw'].forEach(id => $(id).addEventListener('input', build));
build();
</script>
</body>
</html>`))
