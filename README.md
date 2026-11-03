# helm-tui

A terminal UI for browsing and managing your Helm repositories: see which
sources are alive, add/remove/update them, search charts across all of them
and diff any two versions of a chart.

![Repositories](docs/screenshots/repos.png)

## Features

- **Repository list** with chart counts and an availability check that runs
  on start and on demand (`r`): `●` ok with latency, `✕` failed with a short
  reason, `…` checking, `·` not checked yet (e.g. a repository added later).
- **Add / delete / update** repositories (single or all) in modal dialogs —
  everything goes through the `helm` CLI, `repositories.yaml` is never edited
  directly. Delete asks for confirmation with "No" as the default.
- **Per-repository proxy** (`http://`, `https://`, `socks5://`) for sources
  that get blocked from time to time. Used for the availability check, `add`,
  `update` and `pull`.
- **Global search** across the cached indexes of all repositories (name and
  description, case-insensitive). Works for unreachable repositories too, as
  long as their index is already cached.
- **Charts and versions** per repository with release dates and app versions.
- **Diff two versions** of a chart: both are pulled into a temporary
  workspace and compared file by file (`Chart.yaml`, `values.yaml`,
  `templates/`, `crds/`, …). Files on the left, scrollable diff on the right
  (vertical and horizontal).

| Global search | Versions |
|---|---|
| ![Search](docs/screenshots/search.png) | ![Versions](docs/screenshots/versions.png) |

![Diff](docs/screenshots/diff.png)

![Delete dialog](docs/screenshots/dialog.png)

## Requirements

- [Helm](https://helm.sh/) v4 CLI (`helm`) in your `PATH`
- macOS or Linux

## Install

### Homebrew (macOS arm64, Linux)

```sh
brew tap vasyakrg/tap
brew install helm-tui
brew upgrade helm-tui   # update
```

### From a release

Download the archive for your platform from the
[Releases](https://github.com/vasyakrg/helm-tui/releases) page
(`darwin_arm64`, `linux_arm64`, `linux_amd64`), verify it against
`checksums.txt` and unpack:

```sh
tar -xzf helm-tui_1.1.0_linux_amd64.tar.gz
sudo mv helm-tui_1.1.0_linux_amd64/helm-tui /usr/local/bin/
helm-tui --version
```

The macOS binary is signed with a Developer ID certificate and notarized by
Apple, so Gatekeeper accepts it without extra steps (the first launch after
download needs an internet connection for the notarization check).

### From source

```sh
go install github.com/vasyakrg/helm-tui/cmd/helm-tui@latest
```

or

```sh
git clone git@github.com:vasyakrg/helm-tui.git && cd helm-tui
make build        # produces bin/helm-tui
```

## Usage

```sh
helm-tui            # start the UI
helm-tui --version
```

The UI reads the same repository configuration and cache as `helm` itself
(`helm env`), so it shows exactly what `helm repo list` shows.

## Keys

### Repositories (start screen)

| Key | Action |
|-----|--------|
| `j`/`k`, arrows | move |
| `Enter` | open the charts of the repository |
| `a` | add a repository (name, URL, username, password, proxy) |
| `d` | delete the repository (asks for confirmation, also drops its proxy) |
| `u` | update the selected repository (`helm repo update <name>`) |
| `U` | update all repositories (one failure does not stop the rest) |
| `p` | set the proxy of the selected repository (`ctrl+d` clears it) |
| `r` | check availability of all repositories (also runs once on start) |
| `/` | global search |
| `q`, `Esc` | quit / back |

Repository passwords are passed to `helm` via `--password-stdin`, never on the
command line. A repository that answers `401` is reported as reachable with an
`auth` mark, because the check is anonymous.

### Charts, versions and search

- Charts: `/` filters by substring, `u` updates the repository, `Enter` opens
  the versions.
- Versions: `space` marks up to two versions, `D` diffs them (older → newer).
- Search: just type; `Enter` opens the versions of the selected chart.
- `Esc` always goes one screen back.

### Diff

Changed files are listed with `~` modified, `+` added, `-` removed. Binary
files and `*.tgz` are marked as changed without contents.

| Key | Action |
|-----|--------|
| `j`/`k`, `g`/`G` | file list focused: previous / next file, first / last |
| `Tab`, `Enter`, `l` | focus the diff pane (`Tab` toggles back) |
| `j`/`k`, `g`/`G` | diff pane focused: scroll by line, top / end |
| `h`/`l` (`←`/`→`) | diff pane focused: pan horizontally (`0` resets); `h` at column 0 returns to the files |
| `n`/`N` | next / previous hunk |
| `pgup`/`pgdn`, `ctrl+u`/`ctrl+d` | scroll the diff by a page |
| `Esc` | diff pane focused: back to the file list; otherwise leave (the workspace is removed) |

Versions are pulled with `helm pull --untar` into `/tmp/helm-tui-<id>`. The
directory is removed when you leave the screen or quit, including on
`SIGINT`/`SIGTERM`; leftovers of crashed runs are cleaned up at the next
start.

## Configuration

Proxies are stored per repository in `~/.config/helm-tui/config.yaml`
(`$XDG_CONFIG_HOME` is respected):

```yaml
proxies:
  bitnami: socks5://127.0.0.1:1080
  private-repo: http://user:password@proxy.corp:3128
```

For a repository with a proxy, the `helm` subprocess gets `HTTP_PROXY`,
`HTTPS_PROXY` and `ALL_PROXY` set and `NO_PROXY` cleared. Repositories without
a proxy inherit your environment unchanged.

## Limitations and security notes

- OCI registries, custom CA / client certificates / `--insecure-skip-tls-verify`
  for repositories and fuzzy search are not supported.
- Nested `charts/*.tgz` inside a pulled chart are shown as changed, not unpacked.
- **Proxy passwords are stored in plain text** in the config file, protected
  only by its permissions (`0600`, directory `0700`) — the same trade-off as
  Helm's own `repositories.yaml`. They never appear in the UI, logs or error
  messages.
- A configured proxy reaches the `helm` subprocess through environment
  variables, so it is visible to the same OS user (e.g. via `ps eww`).
- Version and chart names from a repository index are treated as untrusted:
  they are validated as a single path segment before anything is written to
  disk.

## Development

```sh
make test       # go test ./...
make vet        # go vet ./...
make build      # bin/helm-tui
make release    # dist/*.tar.gz for darwin/arm64, linux/arm64, linux/amd64 + checksums.txt
                # SIGN_IDENTITY / NOTARY_PROFILE enable signing + notarization of the darwin binary
```

After publishing a release, update the Homebrew tap
([vasyakrg/homebrew-tap](https://github.com/vasyakrg/homebrew-tap)):
`scripts/formula.sh > Formula/helm-tui.rb`, then commit and push it there.

The version is stored in the `VERSION` file and shared by all release
artifacts.

## License

[MIT](LICENSE)
