// Package proxy implements the authenticated GitHub reverse proxy.
//
// Requests to /<secret>/<https-url> are authenticated against the secret,
// validated against the GitHub host allow-list, and forwarded to GitHub with
// hop-by-hop and credential headers stripped.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	revproxy "net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gh-smart-proxy/internal/httputil"
)

type ctxKey string

const proxyBaseKey ctxKey = "proxyBase"

var proxyableRedirects = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^https?://github\.com/.+?/.+?/(?:releases|archive)/.*$`),
	regexp.MustCompile(`(?i)^https?://github\.com/.+?/.+?/(?:blob|raw)/.*$`),
	regexp.MustCompile(`(?i)^https?://github\.com/.+?/.+?/(?:info|git-).*$`),
	regexp.MustCompile(`(?i)^https?://raw\.(?:githubusercontent|github)\.com/.+?/.+?/.+?/.+$`),
	regexp.MustCompile(`(?i)^https?://gist\.(?:githubusercontent|github)\.com/.+?/.+?/.+$`),
	regexp.MustCompile(`(?i)^https?://github\.com/.+?/.+?/tags.*$`),
}

// New returns an http.Handler that authenticates the request against secret,
// parses the upstream target from the URL, and reverse-proxies it to a host in
// allowedHosts.
func New(secret string, allowedHosts []string) http.Handler {
	allowed := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		allowed[strings.ToLower(strings.TrimSpace(h))] = true
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	rp := &revproxy.ReverseProxy{
		Director: func(r *http.Request) {},
		ModifyResponse: func(resp *http.Response) error {
			if err := handleRedirectResponse(resp, secret, allowed, transport); err != nil {
				return err
			}
			applyCacheHeaders(resp)
			applyProxyHeaders(resp)
			httputil.StripHopHeaders(resp.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error: %s %s: %v", r.Method, r.URL.String(), err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
		Transport: transport,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, err := parseTarget(r, secret, allowed)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		log.Printf("%s %s -> %s", httputil.ClientIP(r), r.Method, target.String())

		originalHost := r.Host
		proxyBase := httputil.PublicBaseURL(r)

		r = r.WithContext(context.WithValue(r.Context(), proxyBaseKey, proxyBase))
		r.URL = target
		r.Host = target.Host
		r.RequestURI = ""

		// Never forward client credentials to GitHub.
		r.Header.Del("Authorization")
		r.Header.Del("Proxy-Authorization")
		r.Header.Del("Cookie")
		httputil.StripHopHeaders(r.Header)

		r.Header.Set("Host", target.Host)
		r.Header.Set("X-Forwarded-Host", originalHost)
		r.Header.Set("X-Forwarded-Proto", httputil.Scheme(r))

		rp.ServeHTTP(w, r)
	})
}

func handleRedirectResponse(resp *http.Response, secret string, allowed map[string]bool, transport http.RoundTripper) error {
	location := resp.Header.Get("Location")
	if location == "" || resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return nil
	}

	loc, err := url.Parse(location)
	if err != nil {
		return nil
	}
	if !loc.IsAbs() {
		loc = resp.Request.URL.ResolveReference(loc)
	}
	if loc.Scheme != "https" || loc.Host == "" {
		return nil
	}

	proxyBase, _ := resp.Request.Context().Value(proxyBaseKey).(string)
	if proxyBase != "" && isProxyableRedirect(loc.String()) {
		prefix := "/"
		if secret != "" {
			prefix += secret + "/"
		}
		resp.Header.Set("Location", proxyBase+prefix+loc.String())
		return nil
	}

	if !allowed[strings.ToLower(loc.Hostname())] {
		return nil
	}
	return followRedirectInPlace(resp, loc, allowed, transport, 5)
}

func followRedirectInPlace(resp *http.Response, loc *url.URL, allowed map[string]bool, transport http.RoundTripper, redirectsLeft int) error {
	if redirectsLeft <= 0 {
		return errors.New("too many redirects")
	}
	req, err := http.NewRequestWithContext(resp.Request.Context(), resp.Request.Method, loc.String(), nil)
	if err != nil {
		return err
	}
	req.Header = resp.Request.Header.Clone()
	req.Host = loc.Host
	req.RequestURI = ""

	next, err := transport.RoundTrip(req)
	if err != nil {
		return err
	}
	if next.Header.Get("Location") != "" && next.StatusCode >= 300 && next.StatusCode < 400 {
		nextLoc, err := url.Parse(next.Header.Get("Location"))
		if err == nil {
			if !nextLoc.IsAbs() {
				nextLoc = next.Request.URL.ResolveReference(nextLoc)
			}
			if nextLoc.Scheme == "https" && nextLoc.Host != "" && allowed[strings.ToLower(nextLoc.Hostname())] && !isProxyableRedirect(nextLoc.String()) {
				next.Body.Close()
				return followRedirectInPlace(resp, nextLoc, allowed, transport, redirectsLeft-1)
			}
		}
	}

	resp.Body.Close()
	resp.Status = next.Status
	resp.StatusCode = next.StatusCode
	resp.Header = next.Header
	resp.Body = next.Body
	resp.ContentLength = next.ContentLength
	resp.TransferEncoding = next.TransferEncoding
	resp.Uncompressed = next.Uncompressed
	resp.Trailer = next.Trailer
	resp.Request = next.Request
	return nil
}

func isProxyableRedirect(raw string) bool {
	for _, re := range proxyableRedirects {
		if re.MatchString(raw) {
			return true
		}
	}
	return false
}

func applyProxyHeaders(resp *http.Response) {
	resp.Header.Set("Access-Control-Allow-Origin", "*")
	resp.Header.Set("Access-Control-Expose-Headers", "*")
	resp.Header.Del("Content-Security-Policy")
	resp.Header.Del("Content-Security-Policy-Report-Only")
	resp.Header.Del("Clear-Site-Data")
}

// parseTarget validates the request against the secret prefix and parses the
// upstream https:// URL, enforcing the host allow-list.
func parseTarget(r *http.Request, secret string, allowed map[string]bool) (*url.URL, error) {
	// An empty secret means open-proxy mode: the target is the whole path
	// after the leading slash (/<target>) instead of /<secret>/<target>.
	prefix := "/"
	if secret != "" {
		prefix = "/" + secret + "/"
	}
	if !strings.HasPrefix(r.URL.EscapedPath(), prefix) && !strings.HasPrefix(r.URL.Path, prefix) {
		return nil, errors.New("forbidden")
	}

	raw := strings.TrimPrefix(r.URL.Path, prefix)
	if raw == "" {
		return nil, errors.New("empty target")
	}

	if strings.HasPrefix(raw, "https:/") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + strings.TrimPrefix(raw, "https:/")
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
	if !allowed[host] {
		return nil, fmt.Errorf("host not allowed: %s", host)
	}
	return target, nil
}

// applyCacheHeaders sets a CDN-friendly cache policy: cacheable GitHub assets
// get long-lived public caching; everything else (including git smart HTTP) is
// no-store.
func applyCacheHeaders(resp *http.Response) {
	path := resp.Request.URL.Path
	host := strings.ToLower(resp.Request.URL.Hostname())

	// Git smart HTTP must not be cached.
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
