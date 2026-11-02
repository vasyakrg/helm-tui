# helm-tui

Terminal UI for browsing and managing helm repositories: view charts and
versions from the local helm cache, check repository availability, search
across repositories and diff two chart versions.

## Requirements

- Helm v4 CLI (`helm`) installed and configured
- macOS or Linux

## Install

```sh
make build      # produces bin/helm-tui
./bin/helm-tui
```

## Screens and keys

### Repositories (start screen)

Lists `helm repo list` entries with chart counts from the cached
`<repo>-index.yaml` and a live availability check (`…` checking, `●` ok with
latency, `✕` failed with reason).

| Key | Action |
|-----|--------|
| `j`/`k`, arrows | navigate |
| `a` | add repository (name, URL, username, password, proxy) |
| `d` | delete repository (with confirmation; also clears its proxy) |
| `u` | update selected repository (`helm repo update <name>`) |
| `U` | update all repositories (one failure does not stop the rest) |
| `p` | set/clear proxy for the selected repository |
| `r` | re-run availability checks |
| `/` | global chart search across all cached indexes |
| `Enter` | open charts of the repository |
| `q`, `Esc` | quit / back |

Repository passwords are passed to helm via `--password-stdin`, never on the
command line.

### Charts / Versions

- `/` filter charts by substring, `Enter` open versions, `Esc` back.
- Versions: `space` marks up to two versions, `D` diffs them (older → newer),
  `Esc` back.

### Search

Type a substring — matches chart names and descriptions case-insensitively
across all repositories with a cached index (unavailable repos are searched
from cache too). `Enter` opens the versions of the hit.

### Diff

Pulls both versions with `helm pull --untar` into a temp workspace
(`/tmp/helm-tui-*`), compares the trees and shows changed files (`~` modified,
`+` added, `-` removed) with a line diff per file; binary files and `*.tgz`
are marked as changed without contents. `j`/`k` select file, `n`/`N` jump
between hunks. The workspace is removed when you leave the screen or quit.
Stale workspaces from crashed runs are cleaned up at startup.

## Configuration

Per-repository proxies live in `~/.config/helm-tui/config.yaml` (override with
`XDG_CONFIG_HOME`):

```yaml
proxies:
  bitnami: socks5://127.0.0.1:1080
  private-repo: http://user:password@proxy.corp:3128
```

The file is written atomically with mode `0600` (directory `0700`). Repos with
a configured proxy get `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY` set and
`NO_PROXY` cleared in the helm subprocess environment; other repos inherit the
environment as is.

## Limitations and security notes

- OCI registries, TLS/CA flags for repositories and fuzzy search are not
  supported.
- Nested `charts/*.tgz` inside a pulled chart are not unpacked.
- **Proxy passwords are stored in plain text** in the config file (protected
  only by file permissions). Passwords never appear in the UI, logs or error
  messages.
- A configured proxy is passed to the helm subprocess via environment
  variables.
