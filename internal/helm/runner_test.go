package helm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeHelm(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "helm")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExecRunner_PassesProxyEnv(t *testing.T) {
	bin := fakeHelm(t, `echo "PROXY=$HTTPS_PROXY"; echo "STDIN=$(cat)"; echo "ARGS=$@"`)
	r := &ExecRunner{Bin: bin}
	stdin := strings.NewReader("secretpass")
	out, err := r.Run(context.Background(), RunOpts{
		ProxyURL: "http://proxyuser:proxypass@127.0.0.1:3128",
		Stdin:    stdin,
	}, "repo", "add", "x", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "PROXY=http://proxyuser:proxypass@127.0.0.1:3128") {
		t.Errorf("proxy env not passed: %s", s)
	}
	if !strings.Contains(s, "STDIN=secretpass") {
		t.Errorf("stdin not passed: %s", s)
	}
	if !strings.Contains(s, "repo add x https://example.com") {
		t.Errorf("args not passed: %s", s)
	}
}

func TestExecRunner_KilledOnContextCancel(t *testing.T) {
	bin := fakeHelm(t, `sleep 30`)
	r := &ExecRunner{Bin: bin}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := r.Run(ctx, RunOpts{}, "sleep")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("cancel took %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRunError_MasksPasswordWithAtSign(t *testing.T) {
	e := &RunError{Args: []string{"repo", "update"}, Err: errors.New("exit status 1"),
		Stderr: "Get \"HTTPS://u:p@ss@h/index.yaml\": denied"}
	if msg := e.Error(); strings.Contains(msg, "p@ss") || !strings.Contains(msg, "u:***@h") {
		t.Errorf("Error() = %q", msg)
	}
}
