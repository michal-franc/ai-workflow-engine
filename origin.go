package main

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// defaultHost keeps the viewer on loopback: its POST endpoints start agents
// and tmux sessions, so another interface is an explicit -host opt-in.
const defaultHost = "127.0.0.1"

func listenAddr(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// isLoopbackHost reports whether -host keeps the server off the network.
// An empty host listens on every interface.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sameOriginOnly refuses writes a browser sends on behalf of another site:
// Sec-Fetch-Site cross-site, or an Origin whose host is not this server's
// Host (a sandboxed project page sends Origin: null). Reads, and clients
// that send neither header (curl, issue-cli), pass.
func sameOriginOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && crossOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func crossOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host)
}

// displayHost is the host to print in the startup URL: a wildcard bind is
// still reachable on localhost.
func displayHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "localhost"
	}
	return host
}
