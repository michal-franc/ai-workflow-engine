package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
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

// interfaceAddrs lists this machine's addresses; tests swap it.
var interfaceAddrs = net.InterfaceAddrs

// loopbackHosts are always valid Host names; the *.localhost names resolve
// to loopback by RFC 6761, so a rebinding DNS answer cannot claim them.
var loopbackHosts = []string{"localhost", "127.0.0.1", "[::1]", "*.localhost"}

// allowedHostsFor is the Host allowlist past loopback: projects.yaml's
// allowed_hosts, plus the bind address when -host leaves loopback (every
// interface address for a wildcard bind).
func allowedHostsFor(bind string, configured []string) []string {
	var hosts []string
	add := func(h string) {
		if h = normalizeHost(h); h != "" && !isLoopbackName(h) && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	for _, h := range configured {
		add(h)
	}
	switch {
	case isLoopbackHost(bind):
	case bind == "" || bind == "0.0.0.0" || bind == "::":
		addrs, _ := interfaceAddrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				add(ipnet.IP.String())
			}
		}
	default:
		add(bind)
	}
	return hosts
}

// hostAllowed refuses, with 421, any request whose Host is not loopback or
// in allowed. It closes DNS rebinding: a page that rebinds its own name to
// 127.0.0.1 is same-origin to itself, so sameOriginOnly lets it through,
// but it still sends its own name as Host.
func hostAllowed(allowed []string, next http.Handler) http.Handler {
	msg := fmt.Sprintf("misdirected request: this viewer answers only to %s (add a name under allowed_hosts: in projects.yaml)", hostList(allowed))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := normalizeHost(r.Host)
		if !isLoopbackName(h) && !slices.Contains(allowed, h) {
			http.Error(w, msg, http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// normalizeHost drops the port, IPv6 brackets, a trailing dot and case.
func normalizeHost(h string) string {
	h = strings.TrimSpace(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimPrefix(strings.TrimSuffix(h, "]"), "[")
	return strings.ToLower(strings.TrimSuffix(h, "."))
}

// isLoopbackName takes a normalized host: localhost, *.localhost or a
// loopback IP.
func isLoopbackName(h string) bool {
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// hostList names every Host the viewer answers to, IPv6 bracketed as in a
// Host header.
func hostList(allowed []string) string {
	out := slices.Clone(loopbackHosts)
	for _, h := range allowed {
		if strings.Contains(h, ":") {
			h = "[" + h + "]"
		}
		out = append(out, h)
	}
	return strings.Join(out, ", ")
}
