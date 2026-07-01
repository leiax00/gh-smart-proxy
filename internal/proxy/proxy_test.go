package proxy

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestParseTargetReleaseDownloadURL(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://192.0.2.10:8080/secret-token/https://github.com/owner/repo/releases/download/v1.0.0/file.tar.gz", nil)
	if err != nil {
		t.Fatal(err)
	}

	target, err := parseTarget(req, "secret-token", map[string]bool{"github.com": true})
	if err != nil {
		t.Fatalf("parseTarget returned error: %v", err)
	}

	want := "https://github.com/owner/repo/releases/download/v1.0.0/file.tar.gz"
	if got := target.String(); got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestParseTargetRestoresMergedHTTPSSlashes(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://192.0.2.10:8080/secret-token/https:/github.com/owner/repo/releases/download/v1.0.0/file.tar.gz", nil)
	if err != nil {
		t.Fatal(err)
	}

	target, err := parseTarget(req, "secret-token", map[string]bool{"github.com": true})
	if err != nil {
		t.Fatalf("parseTarget returned error: %v", err)
	}

	want := "https://github.com/owner/repo/releases/download/v1.0.0/file.tar.gz"
	if got := target.String(); got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestHandleRedirectResponseRewritesProxyableGitHubLocation(t *testing.T) {
	resp := redirectResponse(t, "https://github.com/owner/repo", "https://github.com/owner/repo/archive/refs/heads/main.zip")

	if err := handleRedirectResponse(resp, "secret-token", map[string]bool{"github.com": true}, nil); err != nil {
		t.Fatalf("handleRedirectResponse returned error: %v", err)
	}

	want := "http://192.0.2.10:8080/secret-token/https://github.com/owner/repo/archive/refs/heads/main.zip"
	if got := resp.Header.Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestHandleRedirectResponseFollowsReleaseAssetInPlace(t *testing.T) {
	resp := redirectResponse(t, "https://github.com/owner/repo/releases/download/v1/file", "https://release-assets.githubusercontent.com/asset/file?sig=abc")
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.String(); got != "https://release-assets.githubusercontent.com/asset/file?sig=abc" {
			t.Fatalf("followed URL = %q", got)
		}
		return &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
			Body:          io.NopCloser(strings.NewReader("asset-body")),
			Request:       r,
			ContentLength: int64(len("asset-body")),
		}, nil
	})

	if err := handleRedirectResponse(resp, "secret-token", map[string]bool{"release-assets.githubusercontent.com": true}, transport); err != nil {
		t.Fatalf("handleRedirectResponse returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "asset-body" {
		t.Fatalf("body = %q, want asset-body", got)
	}
}

func TestHandleRedirectResponseIgnoresDisallowedHost(t *testing.T) {
	resp := redirectResponse(t, "https://github.com/owner/repo/releases/download/v1/file", "https://example.com/file")

	if err := handleRedirectResponse(resp, "secret-token", map[string]bool{"github.com": true}, nil); err != nil {
		t.Fatalf("handleRedirectResponse returned error: %v", err)
	}

	if got := resp.Header.Get("Location"); got != "https://example.com/file" {
		t.Fatalf("Location = %q, want original disallowed URL", got)
	}
}

func redirectResponse(t *testing.T, requestURL, location string) *http.Response {
	t.Helper()
	reqURL, err := url.Parse(requestURL)
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: reqURL, Header: make(http.Header)}
	req = req.WithContext(context.WithValue(req.Context(), proxyBaseKey, "http://192.0.2.10:8080"))

	resp := &http.Response{
		Status:     "302 Found",
		StatusCode: http.StatusFound,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}
	resp.Header.Set("Location", location)
	return resp
}
