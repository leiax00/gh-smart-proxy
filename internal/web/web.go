// Package web serves the built-in landing page that turns a GitHub URL plus
// the user's secret into a ready-to-use proxy URL.
package web

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"

	"gh-smart-proxy/internal/httputil"
)

type pageData struct {
	BaseURL      string
	AllowedHosts template.JS
}

// Handler returns an http.Handler that renders the landing page. The page's
// base URL is derived from the incoming request, and allowedHosts is injected
// so client-side validation matches the server's allow-list.
func Handler(allowedHosts []string) http.Handler {
	allowedJS, err := json.Marshal(allowedHosts)
	if err != nil || len(allowedHosts) == 0 {
		allowedJS = []byte("[]")
	}
	allowed := template.JS(allowedJS)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := homeTemplate.Execute(w, pageData{
			BaseURL:      httputil.PublicBaseURL(r),
			AllowedHosts: allowed,
		}); err != nil {
			log.Printf("render home: %v", err)
		}
	})
}

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
    .section-label { margin-top: 22px; }
    .footer { margin-top: 18px; font-size: 13px; color: #64748b; }
    @media (max-width: 700px) { .row { grid-template-columns: 1fr; } h1 { font-size: 30px; } }
  </style>
</head>
<body>
  <main class="wrap">
    <section class="card">
      <h1>GitHub Proxy</h1>
      <p>输入 GitHub 原始地址，生成带代理的地址。Secret 只在你的浏览器里使用，不会出现在页面源码里；留空表示服务端未启用鉴权(开放代理模式)。Secret 会记住在本浏览器(localStorage)，下次自动填入。</p>

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

      <label class="section-label">jsDelivr CDN 地址</label>
      <pre id="jsdelivr">等待输入 raw/blob/tree 文件地址...</pre>

      <label>Git clone 命令</label>
      <pre id="clone">等待输入仓库地址...</pre>

      <div class="buttons">
        <button onclick="copyProxy()">复制代理地址</button>
        <button class="secondary" onclick="copyJsdelivr()">复制 jsDelivr</button>
        <button class="secondary" onclick="copyClone()">复制 git clone</button>
        <button class="secondary" onclick="openProxy()">代理打开 / 下载</button>
        <button class="secondary" onclick="openJsdelivr()">jsDelivr 打开</button>
        <button class="secondary" onclick="forgetSecret()">清除记住的 Secret</button>
      </div>

      <p id="msg" class="hint"></p>
      <div class="footer">建议：页面可以公开，但 Secret 不要公开；Cloudflare/NPM 前面继续加 WAF 和限流。</div>
    </section>
  </main>
<script>
const allowed = new Set({{.AllowedHosts}});
const $ = id => document.getElementById(id);
function cleanBase(v) { return v.replace(/\/+$/, ''); }
function splitRefAndPath(parts, start) {
  if (parts.length <= start) return null;
  if (parts[start] === 'refs' && parts.length > start + 2) {
    return { ref: parts.slice(start, start + 3).join('/'), path: parts.slice(start + 3).join('/') };
  }
  return { ref: parts[start], path: parts.slice(start + 1).join('/') };
}
function jsdelivrURL(u) {
  const parts = u.pathname.split('/').filter(Boolean);
  let owner, repo, refAndPath;
  if (u.hostname === 'raw.githubusercontent.com' && parts.length >= 4) {
    owner = parts[0];
    repo = parts[1];
    refAndPath = splitRefAndPath(parts, 2);
  } else if ((u.hostname === 'github.com' || u.hostname === 'www.github.com') && parts.length >= 5 && (parts[2] === 'blob' || parts[2] === 'tree')) {
    owner = parts[0];
    repo = parts[1];
    refAndPath = splitRefAndPath(parts, 3);
  } else {
    return '';
  }
  if (!owner || !repo || !refAndPath || !refAndPath.ref) return '';
  return 'https://cdn.jsdelivr.net/gh/' + owner + '/' + repo + '@' + refAndPath.ref + (refAndPath.path ? '/' + refAndPath.path : '/');
}
function build() {
  const secret = $('secret').value.trim();
  const base = cleanBase($('base').value.trim());
  const raw = $('raw').value.trim();
  const msg = $('msg');
  msg.textContent = '';
  msg.className = 'hint';
  if (!base || !raw) {
    $('out').textContent = '等待输入...';
    $('jsdelivr').textContent = '等待输入 raw/blob/tree 文件地址...';
    $('clone').textContent = '等待输入仓库地址...';
    return '';
  }
  let u;
  try { u = new URL(raw); } catch(e) {
    $('out').textContent = '原始地址不是合法 URL';
    $('jsdelivr').textContent = '原始地址不是合法 URL';
    $('clone').textContent = '等待输入仓库地址...';
    msg.textContent = '请粘贴完整的 https://github.com/... 地址';
    msg.className = 'bad';
    return '';
  }
  if (u.protocol !== 'https:' || !allowed.has(u.hostname)) {
    $('out').textContent = '该域名不在代理白名单内';
    $('jsdelivr').textContent = '该域名不在代理白名单内';
    $('clone').textContent = '等待输入仓库地址...';
    msg.textContent = '为了防止开放代理，只允许 GitHub 相关域名。';
    msg.className = 'bad';
    return '';
  }
  const secretPart = secret ? '/' + encodeURIComponent(secret) : '';
  const proxied = base + secretPart + '/' + raw;
  const cdn = jsdelivrURL(u);
  $('out').textContent = proxied;
  $('jsdelivr').textContent = cdn || '此地址不能转换为 jsDelivr。Release 附件和 archive zip 请使用代理地址，并让 Cloudflare 缓存代理响应。';
  if (u.hostname === 'github.com' || u.hostname === 'www.github.com') {
    let cloneURL = proxied;
    if (!cloneURL.endsWith('.git') && /^\/[^\/]+\/[^\/]+\/?$/.test(u.pathname)) cloneURL += '.git';
    $('clone').textContent = 'git clone ' + cloneURL;
  } else {
    $('clone').textContent = '这个地址不是仓库地址，通常用于 curl/wget/浏览器下载。';
  }
  msg.textContent = cdn ? '已生成代理地址和 jsDelivr 地址。' : '已生成代理地址。Release / archive 不适合转换到 jsDelivr，建议走代理并配合 Cloudflare 缓存。';
  msg.className = 'ok';
  return proxied;
}
async function copyText(t) {
  if (!t || t.startsWith('等待') || t.includes('不是合法') || t.includes('不在代理') || t.includes('不能转换')) return;
  try { await navigator.clipboard.writeText(t); $('msg').textContent = '已复制'; $('msg').className = 'ok'; }
  catch(e) { $('msg').textContent = '复制失败，可以手动复制上面的内容'; $('msg').className = 'bad'; }
}
function copyProxy() { copyText($('out').textContent); }
function copyJsdelivr() { copyText($('jsdelivr').textContent); }
function copyClone() { copyText($('clone').textContent); }
function openProxy() { const u = build(); if (u) window.open(u, '_blank', 'noopener'); }
function openJsdelivr() {
  let raw;
  try { raw = new URL($('raw').value.trim()); } catch(e) { return; }
  const u = jsdelivrURL(raw);
  if (u) window.open(u, '_blank', 'noopener');
}
const SECRET_KEY = 'gh-proxy-secret';
try { if (localStorage[SECRET_KEY]) $('secret').value = localStorage[SECRET_KEY]; } catch(e) {}
$('secret').addEventListener('input', () => { try { localStorage[SECRET_KEY] = $('secret').value; } catch(e) {} });
function forgetSecret() {
  try { localStorage.removeItem(SECRET_KEY); } catch(e) {}
  $('secret').value = '';
  build();
  $('msg').textContent = '已清除记住的 Secret';
  $('msg').className = 'ok';
}
['secret','base','raw'].forEach(id => $(id).addEventListener('input', build));
build();
</script>
</body>
</html>`))
