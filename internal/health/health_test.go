package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasyakrg/helm-tui/internal/helm"
)

func TestCheck_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.yaml" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	res := Check(context.Background(), "r", srv.URL+"/", "", 5*time.Second)
	if res.State != OK {
		t.Errorf("state = %v reason %q", res.State, res.Reason)
	}
	if res.Latency <= 0 {
		t.Errorf("latency = %v", res.Latency)
	}
}

func TestCheck_ForbiddenReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()
	res := Check(context.Background(), "r", srv.URL, "", 5*time.Second)
	if res.Reason != "HTTP 403" {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestCheck_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()
	start := time.Now()
	res := Check(context.Background(), "r", srv.URL, "", 100*time.Millisecond)
	if res.Reason != "timeout" {
		t.Errorf("reason = %q, want timeout", res.Reason)
	}
	if time.Since(start) > time.Second {
		t.Errorf("Check blocked for %v", time.Since(start))
	}
}

func TestCheck_TrailingSlashURL(t *testing.T) {
	var gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	Check(context.Background(), "r", srv.URL+"///", "", 5*time.Second)
	if p := gotPath.Load(); p != "/index.yaml" {
		t.Errorf("path = %v", p)
	}
}

func TestCheck_ProxyUsed(t *testing.T) {
	var proxied atomic.Bool
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Store(true)
		w.WriteHeader(200)
	}))
	defer proxySrv.Close()
	res := Check(context.Background(), "r", "http://target.invalid", proxySrv.URL, 5*time.Second)
	if res.State != OK || !proxied.Load() {
		t.Errorf("proxy not used: %+v", res)
	}
}

func TestCheck_BadProxyReasonHasNoPassword(t *testing.T) {
	res := Check(context.Background(), "r", "http://target.invalid", "http://u:sup3rs3cr3t@127.0.0.1:1", 5*time.Second)
	if res.State != Fail {
		t.Fatalf("state = %v", res.State)
	}
	if !strings.Contains(res.Reason, "proxy error") {
		t.Errorf("reason = %q, want proxy error", res.Reason)
	}
	if strings.Contains(res.Reason, "sup3rs3cr3t") {
		t.Errorf("password leaked: %q", res.Reason)
	}
}

func TestCheckAll_ParallelAndClosesChannel(t *testing.T) {
	mk := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(200)
		}))
	}
	var servers []*httptest.Server
	var repos []helm.Repo
	for _, n := range []string{"a", "b", "c"} {
		s := mk()
		servers = append(servers, s)
		repos = append(repos, helm.Repo{Name: n, URL: s.URL})
	}
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()
	start := time.Now()
	ch := CheckAll(context.Background(), repos, func(string) string { return "" }, 5*time.Second)
	n := 0
	for res := range ch {
		if res.State != OK {
			t.Errorf("repo %s: %+v", res.Repo, res)
		}
		n++
	}
	if n != 3 {
		t.Errorf("got %d results, want 3", n)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("checks not parallel: %v", d)
	}
}

func TestCheck_UnauthorizedIsReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	res := Check(context.Background(), "r", srv.URL, "", 5*time.Second)
	if res.State != OK {
		t.Fatalf("401 must count as reachable, got %+v", res)
	}
	if res.Reason != "auth" {
		t.Errorf("reason = %q, want auth", res.Reason)
	}
}

func TestCheck_ConnectionRefusedClassified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	res := Check(context.Background(), "r", url, "", 2*time.Second)
	if res.State != Fail || res.Reason != "connection refused" {
		t.Errorf("got %+v", res)
	}
}

func TestCheck_UnclassifiedReasonHasNoURLOrCreds(t *testing.T) {
	// invalid scheme -> transport error containing the URL
	res := Check(context.Background(), "r", "foo://u:sup3r@host/x", "", 2*time.Second)
	if res.State != Fail {
		t.Fatalf("state = %v", res.State)
	}
	if strings.Contains(res.Reason, "sup3r") || strings.Contains(res.Reason, "://") || strings.Contains(res.Reason, "host") {
		t.Errorf("reason leaks details: %q", res.Reason)
	}
}

func TestCheck_DNSErrorHasNoURL(t *testing.T) {
	res := Check(context.Background(), "r", "http://nonexistent.invalid", "", 3*time.Second)
	if res.State != Fail {
		t.Fatalf("state = %v", res.State)
	}
	if strings.Contains(res.Reason, "://") || strings.Contains(res.Reason, "Get ") {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestCheck_DoesNotKeepConnectionsOpen(t *testing.T) {
	var closed atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateClosed {
			closed.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()
	Check(context.Background(), "r", srv.URL, "", 2*time.Second)
	deadline := time.Now().Add(time.Second)
	for closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if closed.Load() == 0 {
		t.Error("connection still open after Check (keep-alive leak)")
	}
}
