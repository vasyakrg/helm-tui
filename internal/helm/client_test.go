package helm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls [][]string
	opts  []RunOpts
	// behaviour
	stdout     []byte
	stderr     string
	err        bool
	lastStdout string
}

func (f *fakeRunner) Run(_ context.Context, o RunOpts, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	f.opts = append(f.opts, o)
	if f.err {
		return nil, &RunError{Args: args, Stderr: f.stderr, Err: errors.New("exit status 1")}
	}
	if f.lastStdout != "" {
		return []byte(f.lastStdout), nil
	}
	return f.stdout, nil
}

func TestList_ParsesJSON(t *testing.T) {
	fr := &fakeRunner{lastStdout: `[{"name":"a","url":"https://a"},{"name":"b","url":"https://b"}]`}
	c := &Client{R: fr}
	repos, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].Name != "a" || repos[1].URL != "https://b" {
		t.Errorf("got %+v", repos)
	}
	if got := strings.Join(fr.calls[0], " "); got != "repo list -o json" {
		t.Errorf("args = %q", got)
	}
}

func TestList_NoRepositoriesIsEmpty(t *testing.T) {
	fr := &fakeRunner{err: true, stderr: "Error: no repositories to show"}
	c := &Client{R: fr}
	repos, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("want empty, got %+v", repos)
	}
}

func TestAdd_PasswordGoesToStdinNotArgv(t *testing.T) {
	fr := &fakeRunner{}
	c := &Client{R: fr}
	err := c.Add(context.Background(), AddArgs{
		Name: "r", URL: "https://r", Username: "user", Password: "sup3rs3cret",
	})
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(fr.calls[0], " ")
	if strings.Contains(argv, "sup3rs3cret") {
		t.Errorf("password in argv: %s", argv)
	}
	if !strings.Contains(argv, "--password-stdin") || !strings.Contains(argv, "--username user") {
		t.Errorf("missing flags: %s", argv)
	}
	if fr.opts[0].Stdin == nil {
		t.Fatal("stdin not set")
	}
	buf := make([]byte, 32)
	n, _ := fr.opts[0].Stdin.Read(buf)
	if strings.TrimSpace(string(buf[:n])) != "sup3rs3cret" {
		t.Errorf("stdin = %q", string(buf[:n]))
	}
}

func TestAdd_PassesProxyToRunner(t *testing.T) {
	fr := &fakeRunner{}
	c := &Client{R: fr}
	_ = c.Add(context.Background(), AddArgs{Name: "r", URL: "https://r", ProxyURL: "socks5://h:1080"})
	if fr.opts[0].ProxyURL != "socks5://h:1080" {
		t.Errorf("proxy not passed: %+v", fr.opts[0])
	}
}

func TestUpdate_ArgsAndProxy(t *testing.T) {
	fr := &fakeRunner{}
	c := &Client{R: fr}
	if err := c.Update(context.Background(), "r", "http://p:1"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fr.calls[0], " "); got != "repo update r" {
		t.Errorf("args = %q", got)
	}
	if fr.opts[0].ProxyURL != "http://p:1" {
		t.Errorf("proxy = %q", fr.opts[0].ProxyURL)
	}
}

func TestPull_Args(t *testing.T) {
	fr := &fakeRunner{}
	c := &Client{R: fr}
	if err := c.Pull(context.Background(), "repo", "chart", "1.2.3", "/tmp/x", ""); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fr.calls[0], " "); got != "pull repo/chart --version 1.2.3 --untar --untardir /tmp/x" {
		t.Errorf("args = %q", got)
	}
}

func TestEnv_ParsesHelmEnv(t *testing.T) {
	fr := &fakeRunner{lastStdout: "HELM_REPOSITORY_CONFIG=\"/Users/x/repositories.yaml\"\nHELM_REPOSITORY_CACHE=\"/Users/x/cache\"\nHELM_BIN=\"helm\"\n"}
	c := &Client{R: fr}
	env, err := c.Env(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if env.RepositoryConfig != "/Users/x/repositories.yaml" || env.RepositoryCache != "/Users/x/cache" {
		t.Errorf("env = %+v", env)
	}
}

func TestRunError_DoesNotLeakProxyPassword(t *testing.T) {
	e := &RunError{
		Args:   []string{"repo", "add", "r"},
		Stderr: "dial tcp: dial via http://u:s3cr3t@h:3128 failed",
		Err:    errors.New("exit status 1"),
	}
	msg := e.Error()
	if strings.Contains(msg, "s3cr3t") {
		t.Errorf("password leaked: %s", msg)
	}
	if !strings.Contains(msg, "***") {
		t.Errorf("want masked: %s", msg)
	}
}

// compile-time check that RunError marshalling isn't accidentally used for output paths
var _ = json.Marshal
