package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
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

// A DNS-rebound page reaches 127.0.0.1 under its own name: Host and Origin
// are both attacker.example, so only the Host check can tell.
func TestHostGuard_RefusesForeignHost(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	evil := "attacker.example:" + strings.TrimPrefix(ts.URL[strings.LastIndex(ts.URL, ":"):], ":")

	for _, tc := range []struct {
		name, method, path string
		headers            map[string]string
	}{
		{"list", "GET", "/", nil},
		{"issue page", "GET", "/p/test-project/issue/bug-in-login", nil},
		{"page-raw", "GET", "/p/test-project/page-raw/status.html", nil},
		{"static", "GET", "/static/style.css", nil},
		{"rebound write", "POST", "/p/test-project/issue/bug-in-login/comments",
			map[string]string{"Origin": "http://" + evil, "Sec-Fetch-Site": "same-origin"}},
		{"rebound delete", "DELETE", "/p/test-project/issue/bug-in-login",
			map[string]string{"Origin": "http://" + evil}},
		{"no port", "GET", "/", nil},
		{"localhost lookalike", "GET", "/", nil},
	} {
		req := mustReq(t, tc.method, ts.URL+tc.path)
		req.Host = evil
		switch tc.name {
		case "no port":
			req.Host = "attacker.example"
		case "localhost lookalike":
			req.Host = "localhost.attacker.example"
		}
		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}
		resp, body := pageResp(t, req)
		if resp.StatusCode != http.StatusMisdirectedRequest {
			t.Errorf("%s: %s %s with Host %s = %d, want 421", tc.name, tc.method, tc.path, req.Host, resp.StatusCode)
			continue
		}
		if !strings.Contains(body, "allowed_hosts") || !strings.Contains(body, "localhost") || strings.Count(strings.TrimSpace(body), "\n") != 0 {
			t.Errorf("%s: body %q should be one line naming the allowed hosts and allowed_hosts", tc.name, body)
		}
	}
}

func TestHostGuard_LetsLoopbackNamesThrough(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	port := ts.URL[strings.LastIndex(ts.URL, ":"):]

	for _, host := range []string{
		"localhost" + port, "LocalHost" + port, "localhost", "localhost." + port,
		"127.0.0.1" + port, "127.0.0.1", "[::1]" + port, "[::1]",
		"anything.localhost" + port, "a.b.localhost",
	} {
		req := mustReq(t, "GET", ts.URL+"/p/test-project/issue/bug-in-login")
		req.Host = host
		if resp, _ := pageResp(t, req); resp.StatusCode != http.StatusOK {
			t.Errorf("Host %s: GET = %d, want 200", host, resp.StatusCode)
		}
	}
	// A same-origin write under a loopback name still goes through.
	req := mustReq(t, "POST", ts.URL+"/p/test-project/issue/bug-in-login/comments")
	req.Host = "localhost" + port
	req.Header.Set("Origin", "http://localhost"+port)
	if resp, _ := pageResp(t, req); resp.StatusCode == http.StatusMisdirectedRequest || resp.StatusCode == http.StatusForbidden {
		t.Errorf("same-origin write on localhost refused: %d", resp.StatusCode)
	}
}

func TestHostGuard_AllowedHostsFromConfig(t *testing.T) {
	proj, _ := setupTestProject(t)
	srv, err := NewServer([]tracker.Project{proj})
	if err != nil {
		t.Fatal(err)
	}
	srv.allowedHosts = allowedHostsFor(defaultHost, []string{"Viewer.LAN", "box.tail1234.ts.net:8080", "[fd00::7]"})
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	for host, want := range map[string]int{
		"viewer.lan:8080":       http.StatusOK,
		"VIEWER.lan":            http.StatusOK,
		"box.tail1234.ts.net":   http.StatusOK,
		"[fd00::7]:8080":        http.StatusOK,
		"sub.viewer.lan":        http.StatusMisdirectedRequest,
		"attacker.example:8080": http.StatusMisdirectedRequest,
	} {
		req := mustReq(t, "GET", ts.URL+"/p/test-project/issue/bug-in-login")
		req.Host = host
		resp, body := pageResp(t, req)
		if resp.StatusCode != want {
			t.Errorf("Host %s = %d, want %d", host, resp.StatusCode, want)
		}
		if want == http.StatusMisdirectedRequest && !strings.Contains(body, "viewer.lan") {
			t.Errorf("Host %s: body %q should list the configured hosts", host, body)
		}
	}
}

// -host 0.0.0.0 serves every interface, so its addresses are valid Hosts;
// a name is still refused.
func TestHostGuard_WildcardBindAcceptsInterfaceAddresses(t *testing.T) {
	orig := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
			&net.IPNet{IP: net.ParseIP("192.168.1.5"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
		}, nil
	}
	t.Cleanup(func() { interfaceAddrs = orig })

	for _, tc := range []struct {
		bind  string
		allow []string
		deny  []string
	}{
		{"0.0.0.0", []string{"192.168.1.5:8080", "[fe80::1]:8080", "localhost:8080"}, []string{"attacker.example:8080", "10.0.0.9:8080"}},
		{"", []string{"192.168.1.5:8080"}, []string{"attacker.example"}},
		{"::", []string{"192.168.1.5"}, []string{"attacker.example"}},
		{"10.0.0.9", []string{"10.0.0.9:8080"}, []string{"192.168.1.5:8080", "attacker.example"}},
		{"viewer.lan", []string{"viewer.lan:8080"}, []string{"attacker.example"}},
		{defaultHost, []string{"localhost"}, []string{"192.168.1.5:8080"}},
	} {
		allowed := allowedHostsFor(tc.bind, nil)
		h := hostAllowed(allowed, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		check := func(host string, want int) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Host = host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("-host %q, Host %s = %d, want %d", tc.bind, host, rec.Code, want)
			}
		}
		for _, host := range tc.allow {
			check(host, http.StatusOK)
		}
		for _, host := range tc.deny {
			check(host, http.StatusMisdirectedRequest)
		}
	}
}

// viewer_url's host is allowed with allowed_hosts: the links point there.
func TestConfigHosts_IncludesViewerURLHost(t *testing.T) {
	got := configHosts(&tracker.ProjectsConfig{AllowedHosts: []string{"box.lan"}, ViewerURL: "https://issues.example.com:9000/viewer"})
	if want := []string{"box.lan", "issues.example.com:9000"}; !slices.Equal(got, want) {
		t.Fatalf("configHosts = %q, want %q", got, want)
	}
	if hosts := allowedHostsFor("127.0.0.1", got); !slices.Contains(hosts, "issues.example.com") {
		t.Fatalf("allowedHostsFor = %q, want issues.example.com", hosts)
	}
	if got := configHosts(&tracker.ProjectsConfig{}); len(got) != 0 {
		t.Fatalf("no keys: %q", got)
	}
}
