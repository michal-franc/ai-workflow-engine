package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestSameOriginGuard_RefusesCrossOriginWrites(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	comments := ts.URL + "/p/test-project/issue/bug-in-login/comments"

	for _, tc := range []struct {
		name, method, url string
		headers           map[string]string
	}{
		{"evil origin", "POST", comments, map[string]string{"Origin": "http://evil.example"}},
		{"other localhost port", "POST", comments, map[string]string{"Origin": "http://127.0.0.1:1"}},
		{"sandboxed page", "POST", comments, map[string]string{"Origin": "null"}},
		{"cross-site fetch", "POST", comments, map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{"cross-site with own origin", "POST", comments, map[string]string{"Origin": ts.URL, "Sec-Fetch-Site": "cross-site"}},
		{"delete", "DELETE", ts.URL + "/p/test-project/issue/bug-in-login", map[string]string{"Origin": "http://evil.example"}},
		{"put", "PUT", ts.URL + "/p/test-project/issue/bug-in-login", map[string]string{"Origin": "http://evil.example"}},
		{"outside /p/", "POST", ts.URL + "/project-for", map[string]string{"Origin": "http://evil.example"}},
	} {
		req := mustReq(t, tc.method, tc.url)
		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}
		if resp, _ := pageResp(t, req); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: %s = %d, want 403", tc.name, tc.method, resp.StatusCode)
		}
	}
}

func TestSameOriginGuard_LetsSameOriginAndReadsThrough(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	comments := ts.URL + "/p/test-project/issue/bug-in-login/comments"

	for _, tc := range []struct {
		name, method string
		headers      map[string]string
	}{
		{"UI fetch", "POST", map[string]string{"Origin": ts.URL, "Sec-Fetch-Site": "same-origin"}},
		{"origin host case", "POST", map[string]string{"Origin": strings.ToUpper(ts.URL)}},
		{"no browser headers (curl, issue-cli)", "POST", nil},
		{"user typed the URL", "POST", map[string]string{"Sec-Fetch-Site": "none"}},
		{"cross-site read", "GET", map[string]string{"Origin": "http://evil.example", "Sec-Fetch-Site": "cross-site"}},
	} {
		req := mustReq(t, tc.method, comments)
		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}
		if resp, _ := pageResp(t, req); resp.StatusCode == http.StatusForbidden {
			t.Errorf("%s: %s refused", tc.name, tc.method)
		}
	}
}

func TestListenAddr(t *testing.T) {
	for _, tc := range []struct {
		host     string
		want     string
		loopback bool
	}{
		{defaultHost, "127.0.0.1:8080", true},
		{"localhost", "localhost:8080", true},
		{"::1", "[::1]:8080", true},
		{"0.0.0.0", "0.0.0.0:8080", false},
		{"", ":8080", false},
		{"192.168.1.10", "192.168.1.10:8080", false},
	} {
		if got := listenAddr(tc.host, 8080); got != tc.want {
			t.Errorf("listenAddr(%q) = %q, want %q", tc.host, got, tc.want)
		}
		if got := isLoopbackHost(tc.host); got != tc.loopback {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", tc.host, got, tc.loopback)
		}
	}
	if defaultHost != "127.0.0.1" {
		t.Errorf("defaultHost = %q, want 127.0.0.1", defaultHost)
	}
}
