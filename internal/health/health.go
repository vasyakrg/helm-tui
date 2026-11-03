// Package health checks repository availability over HTTP, optionally via proxy.
package health

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	// Unchecked means no check has been run for the repository yet
	// (e.g. it was added after the last check run).
	Unchecked
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
		// One-shot transport: never keep connections (and their goroutines) alive.
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
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
	if resp.StatusCode == http.StatusUnauthorized {
		// Server answered: it is reachable, credentials are simply required.
		return Result{Repo: repo, State: OK, Latency: time.Since(start), Reason: "auth"}
	}
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

// classify maps a transport error to a short reason. The reason never
// contains the URL, host or credentials.
func classify(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && (opErr.Op == "proxyconnect" || strings.HasPrefix(opErr.Op, "socks")) {
		if strings.Contains(err.Error(), "407") {
			return "proxy error: HTTP 407"
		}
		return "proxy error"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "DNS error"
	}
	var (
		unknownAuth x509.UnknownAuthorityError
		invalid     x509.CertificateInvalidError
		hostErr     x509.HostnameError
		verifyErr   *tls.CertificateVerificationError
		recordErr   tls.RecordHeaderError
		alertErr    tls.AlertError
	)
	if errors.As(err, &unknownAuth) || errors.As(err, &invalid) || errors.As(err, &hostErr) ||
		errors.As(err, &verifyErr) || errors.As(err, &recordErr) || errors.As(err, &alertErr) {
		return "TLS error"
	}
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset"
	}
	return "error"
}
