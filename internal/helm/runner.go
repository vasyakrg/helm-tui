package helm

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/vasyakrg/helm-tui/internal/proxy"
)

// RunOpts carries per-invocation settings: proxy env override and stdin.
type RunOpts struct {
	ProxyURL string
	Stdin    io.Reader
}

// Runner executes the helm binary.
type Runner interface {
	Run(ctx context.Context, o RunOpts, args ...string) (stdout []byte, err error)
}

// ExecRunner runs the helm binary as a subprocess. Bin=="" means "helm".
type ExecRunner struct {
	Bin string
}

// RunError describes a failed helm invocation with scrubbed stderr.
type RunError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *RunError) Error() string {
	msg := "helm " + strings.Join(e.Args, " ") + ": " + e.Err.Error()
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + proxy.MaskText(s)
	}
	return proxy.MaskText(msg)
}

func (e *RunError) Unwrap() error { return e.Err }

func (r *ExecRunner) Run(ctx context.Context, o RunOpts, args ...string) ([]byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "helm"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	// After context cancel the process is killed, but its children (e.g. sleep)
	// may keep the stdout/stderr pipes open; WaitDelay unblocks Wait.
	cmd.WaitDelay = 500 * time.Millisecond
	cmd.Env = proxy.Env(os.Environ(), o.ProxyURL)
	if o.Stdin != nil {
		cmd.Stdin = o.Stdin
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return nil, &RunError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return []byte(stdout.String()), nil
}
