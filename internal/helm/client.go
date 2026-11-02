package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Repo is an entry of `helm repo list`.
type Repo struct {
	Name string
	URL  string
}

// AddArgs describes a `helm repo add` invocation.
type AddArgs struct {
	Name     string
	URL      string
	Username string
	Password string
	ProxyURL string
}

// Env holds the helm environment paths we care about.
type Env struct {
	RepositoryConfig string
	RepositoryCache  string
}

// Client wraps Runner with helm operations used by the TUI.
type Client struct {
	R Runner
}

func (c *Client) run(ctx context.Context, o RunOpts, args ...string) ([]byte, error) {
	if c.R == nil {
		c.R = &ExecRunner{}
	}
	return c.R.Run(ctx, o, args...)
}

type repoListJSON struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// List returns configured repositories. A helm "no repositories" error yields
// an empty list with a nil error.
func (c *Client) List(ctx context.Context) ([]Repo, error) {
	out, err := c.run(ctx, RunOpts{}, "repo", "list", "-o", "json")
	if err != nil {
		var re *RunError
		if asRunError(err, &re) && strings.Contains(re.Stderr, "no repositories") {
			return nil, nil
		}
		return nil, err
	}
	var raw []repoListJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing helm repo list output: %w", err)
	}
	repos := make([]Repo, 0, len(raw))
	for _, r := range raw {
		repos = append(repos, Repo{Name: r.Name, URL: r.URL})
	}
	return repos, nil
}

func asRunError(err error, target **RunError) bool {
	re, ok := err.(*RunError)
	if ok {
		*target = re
	}
	return ok
}

// Add registers a repository. The password is passed via stdin
// (--password-stdin), never in argv.
func (c *Client) Add(ctx context.Context, a AddArgs) error {
	args := []string{"repo", "add", a.Name, a.URL}
	if a.Username != "" {
		args = append(args, "--username", a.Username)
	}
	if a.Password != "" {
		args = append(args, "--password-stdin")
	}
	_, err := c.run(ctx, RunOpts{
		ProxyURL: a.ProxyURL,
		Stdin:    strings.NewReader(a.Password + "\n"),
	}, args...)
	return err
}

// Remove unregisters a repository.
func (c *Client) Remove(ctx context.Context, name string) error {
	_, err := c.run(ctx, RunOpts{}, "repo", "remove", name)
	return err
}

// Update refreshes a single repository's index.
func (c *Client) Update(ctx context.Context, name, proxyURL string) error {
	_, err := c.run(ctx, RunOpts{ProxyURL: proxyURL}, "repo", "update", name)
	return err
}

// Pull downloads and untars a chart version into destDir.
func (c *Client) Pull(ctx context.Context, repo, chart, version, destDir, proxyURL string) error {
	_, err := c.run(ctx, RunOpts{ProxyURL: proxyURL},
		"pull", repo+"/"+chart, "--version", version, "--untar", "--untardir", destDir)
	return err
}

// Env parses the output of `helm env`.
func (c *Client) Env(ctx context.Context) (Env, error) {
	out, err := c.run(ctx, RunOpts{}, "env")
	if err != nil {
		return Env{}, err
	}
	var env Env
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "HELM_REPOSITORY_CONFIG":
			env.RepositoryConfig = v
		case "HELM_REPOSITORY_CACHE":
			env.RepositoryCache = v
		}
	}
	return env, nil
}
