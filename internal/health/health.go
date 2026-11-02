// Package health checks repository availability over HTTP, optionally via proxy.
package health

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vasyakrg/helm-tui/internal/helm"
)

// State of an availability check.
type State int

// Check states.
const (
	Checking State = iota
	OK
	Fail
)

// Result is the outcome of checking one repository.
type Result struct {
	Repo    string
	State   State
	Latency time.Duration
	Reason  string
}

// Check performs a single GET <url>/index.yaml, aborting after timeout.
// The body is not read: latency covers the response headers only.
func Check(ctx context.Context, repo, repoURL, proxyURL string, timeout time.Duration) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 0,
		}).DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		TLSClientConfig:       &tls.Config{},
	}
	if proxyURL != "" {
		u, perr := url.Parse(proxyURL)
		if perr != nil {
			return Result{Repo: repo, State: Fail, Reason: "proxy error: bad proxy URL"}
		}
		transport.Proxy = http.ProxyURL(u)
	}
	client := &http.Client{Transport: transport}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(repoURL, "/")+"/index.yaml", nil)
	if err != nil {
		return Result{Repo: repo, State: Fail, Reason: classify(err)}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{Repo: repo, State: Fail, Latency: time.Since(start), Reason: classify(err)}
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Result{Repo: repo, State: Fail, Latency: time.Since(start),
			Reason: "HTTP " + strconv.Itoa(resp.StatusCode)}
	}
	return Result{Repo: repo, State: OK, Latency: time.Since(start)}
}

// CheckAll checks every repository in parallel and returns a channel of
// results (one per repo) that closes once all repos have answered.
func CheckAll(ctx context.Context, repos []helm.Repo, proxyFor func(repo string) string, timeout time.Duration) <-chan Result {
	ch := make(chan Result, len(repos))
	var wg sync.WaitGroup
	for _, r := range repos {
		wg.Add(1)
		go func(r helm.Repo) {
			defer wg.Done()
			ch <- Check(ctx, r.Name, r.URL, proxyFor(r.Name), timeout)
		}(r)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	return ch
}

func classify(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "proxyconnect"), strings.Contains(msg, "proxy"):
		if isProxyAuthFailure(msg) {
			return "proxy error: HTTP 407"
		}
		return "proxy error: " + maskCreds(msg)
	case strings.Contains(msg, "certificate"), strings.Contains(msg, "x509"), strings.Contains(msg, "tls"):
		return "TLS error: " + maskCreds(msg)
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "lookup"):
		return "DNS error: " + shortHost(err)
	default:
		return maskCreds(msg)
	}
}

func isProxyAuthFailure(msg string) bool { return strings.Contains(msg, "407") }

// maskCreds replaces user:password in any URL inside the message.
func maskCreds(msg string) string {
	out := msg
	if i := strings.Index(out, "://"); i >= 0 {
		rest := out[i+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 && at < len(rest) {
			head := rest[:at]
			if c := strings.IndexByte(head, ':'); c >= 0 && !strings.Contains(head[:c], "/") {
				out = out[:i+3] + head[:c] + ":***@" + rest[at+1:]
			}
		}
	}
	return out
}

func shortHost(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "lookup "); i >= 0 {
		rest := msg[i+7:]
		if j := strings.Index(rest, ":"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return msg
}
