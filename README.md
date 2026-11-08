# gthok

`gthok` is a Unix Git wrapper: it classifies operations, applies declarative
policy and trusted pre/post hooks, then runs the real Git. It understands modes
and effects, not just command names: `branch -D` and `update-ref -d` are both
ref deletions; a push with a lease differs from an unconditional force push.

**A guardrail, not an OS security boundary.** Calling the real Git directly or
using another client bypasses it. Keep server-side branch protection for
requirements that must hold for every client. Hooks run as your user.
Git's `--no-verify` does not bypass gthok.

## Install

Install Git first. The grammar is pinned to **Git v2.56.0**, commit
`a018953688f1b10bddf91bff8747068f5f4746a4`. Binaries support Linux and macOS,
amd64 and arm64, without CGO. Windows is not supported.

```sh
brew install mkusaka/tap/gthok
# Or use mise; no plugin needed:
mise use -g github:mkusaka/gthok
# Or build with the Go version in go.mod:
go install github.com/mkusaka/gthok/cmd/gthok@latest
```

Release archives are named `gthok_<version>_<os>_<arch>.tar.gz`, for example
`gthok_0.0.1_darwin_arm64.tar.gz`. Download the matching archive and checksums
from [Releases](https://github.com/mkusaka/gthok/releases), verify its SHA-256,
and install the `gthok` binary into a stable directory on PATH.

```sh
gthok shim install                    # ~/.local/bin/git -> gthok
export PATH="$HOME/.local/bin:$PATH"
gthok doctor
```

The real Git must remain on PATH, or be set with `GTHOK_REAL_GIT` /
user-only `runtime.realGit`. Discovery excludes gthok itself and rejects
recursive shims. Installation never edits shell rc files. Refresh your shell's
command cache after changing PATH (`hash -r` in bash, `rehash` in zsh).
Uninstall the owned shim with `gthok shim uninstall` before deleting the binary.
Use the same `--bin-dir DIR` for a custom install/uninstall location.

Without a shim, `gthok exec -- <git args>` uses the identical pipeline.
For an interactive shell, `eval "$(gthok shell-init zsh)"` (or `bash`; for fish,
`gthok shell-init fish | source`) defines a `git` function. Functions do not
cover direct process launches or shells that do not load that startup file.

### GitHub Action

```yaml
- uses: mkusaka/gthok@v0.0.1 # Pin a reviewed commit SHA in production.
  with:
    version: v0.0.1
- run: gthok exec -- status --short
```

The `version` input selects a release (`vX.Y.Z` or `latest`); `shim: false`
installs only the management binary. Linux/macOS runners are supported.
See [installation notes](docs/notes/install.md) for release verification.

## Quick start

Repos without applicable configuration are pure passthrough: gthok starts no
normalizer, logger, spool or hook runner. A user config activates gthok globally.
New configurations default to **observe**, which reports would-deny without
blocking Git. Inspect results before switching to enforce.

```sh
gthok config init --scope user
gthok normalize --no-resolve -- push --force origin HEAD:refs/heads/main
gthok explain --no-transforms -- reset --hard HEAD~1
gthok actions 'remote.push.*' --json
gthok docs search "force push lease"
```

Put Git arguments after `--`, without a leading `git`. For example,
`gthok exec -- git status` does **not** mean `gthok exec -- status`.
`normalize --no-resolve` launches no subprocess; it does not load aliases or
repository context through Git. Omitted resolution is uncertainty, not safety.

## Policy and configuration

Files load in order: user `config.yaml` → repository-root `.gthok.yaml` →
personal `.gthok.local.yaml`. The effective repository follows `-C`,
`--git-dir`, `--work-tree`, `--bare` and Git's environment. Project/local policy
can tighten, not weaken, user policy. Only user config sets runtime/logging.
The personal file is auto-trusted **only while untracked**; initialization adds
it to `.git/info/exclude` rather than `.gitignore`.

User config is `$GTHOK_CONFIG_DIR/config.yaml`, otherwise
`$XDG_CONFIG_HOME/gthok/config.yaml`, otherwise `~/.config/gthok/config.yaml`.

```yaml
version: 1
policy:
  mode: enforce
  unknownMutation: audit
  unknownInput: deny
  rules:
    - id: protect-main
      match:
        operation: "remote.push.*"
        remotes: origin
        refs: [refs/heads/main, "refs/heads/release/**"]
        parameters: {forceMode: unconditional}
      decision: deny
      reason: Unconditional force pushes to protected branches are disabled.
    - id: protect-local-refs
      match:
        operation: [ref.delete, ref.branch.delete]
        refs: refs/heads/main
      decision: deny
```

Conditions are AND on **one operation**, list entries OR. `refs` uses full
ref-name globs, **not refspecs**; `paths` uses repository-relative globs,
**not Git pathspecs**. Unknown/incomplete targets maybe-match; deny rules
apply conservatively unless `onUnresolved: ignore` is explicit. Allow responses
never lift a deny. See [policy and matchers](docs/policy-and-matchers.md).

```sh
gthok config init                   # shared .gthok.yaml
gthok config init --scope local     # personal, per checkout
gthok config validate
gthok config trust --dry-run        # inspect executable definitions and content
gthok config trust                  # activate reviewed project executables
```

Project policy applies without executable trust. Hooks/transforms/delegates
require content trust or user path trust. Trusting a repository or parent
directory permits future checked-out code there to run as you; inspect
[security](docs/security.md) before using `config trust repo` / `config trust dir`.
Invalid user configuration that declares enforce mode fails closed (78);
observe/undetermined mode normally fails open with a warning.
`GTHOK_FAIL_CLOSED=1` rejects invalid configuration in either mode.

## Hooks and transforms

Build the Git examples into your private user config directory:

```sh
config_dir="${GTHOK_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/gthok}"
mkdir -p "$config_dir/hooks" "$config_dir/transforms" "$config_dir/logs"
go build -o "$config_dir/hooks/deny-force-push" ./examples/hooks/deny-force-push
go build -o "$config_dir/hooks/audit-history" ./examples/hooks/audit-history
go build -o "$config_dir/transforms/commit-trailer" ./examples/transforms/commit-trailer
```

Review and adapt [examples/config.yaml](examples/config.yaml). Its pre-hook
rejects force/lease pushes to protected branches, its post-hook records requested
history rewrites and exit status, and its transform appends a fixed trailer to
explicit commit messages. The trailer transform intentionally rejects editor
input: use `commit -m` or `-F`. It fails closed even in observe mode.

A hook reads one versioned JSON document on stdin and writes a response:

```json
{"schemaVersion":1,"decision":"deny","reason":"Protected branch"}
```

Hooks judge; transforms edit only fields with a verified write-back adapter.
Hook commands are argv arrays, not shell snippets. Raw input and inherited
credentials are available by default; use `redactInput: true` before forwarding
input outside your machine. Post hooks never change Git's status.
See [hook protocol](docs/hook-protocol.md), [transforms](docs/transforms.md), and
[retry-safe delegates](docs/delegates.md).

```sh
gthok hooks test --expect deny -- push --force origin HEAD:refs/heads/main
gthok hooks test --print-input -- commit -m message
```

`hooks test` runs trusted transforms and selected hooks, never the request or
delegate. `explain` runs trusted transforms but never hooks or the request;
`explain --no-transforms` avoids transform side effects too. Resolution is
local-only: neither diagnostic makes a remote query to learn the remote's refs.

## CLI reference

| Command | Purpose |
| --- | --- |
| `gthok exec -- <args>` | Execute through the same pipeline as the `git` shim. |
| `gthok normalize [--no-resolve] -- <args>` | Redacted normalization JSON. |
| `gthok explain [--json] [--no-transforms] -- <args>` | Policy, transforms and hook/delegate selection; no requested Git execution. |
| `gthok hooks list [--json]` | Active/inactive hook inventory. |
| `gthok hooks test [--json] [--expect deny] -- <args>` | Run transforms and hooks against a synthetic request. |
| `gthok config init/validate/path` | Create, validate or locate configuration. |
| `gthok config trust/untrust` | Review or revoke executable trust. |
| `gthok shim install/uninstall [--bin-dir DIR]` | Manage only the owned PATH shim. |
| `gthok shell-init bash/zsh/fish` | Print an interactive-shell function. |
| `gthok doctor [--json]` | Installation/config/executable checks. |
| `gthok actions [glob] [--json]` | Actions, effects, example scopes and Git routes. A literal prefix also works. |
| `gthok docs [id]`, `docs --all`, `docs search "query"` | Embedded offline Markdown and top-five JSON search. |
| `gthok schema <name>` | Print a Draft 2020-12 schema. |
| `gthok version` | Text release, commit, Go and platform metadata. |

Schemas: `config`, `project`, `local`, `hook-input`, `hook-response`,
`transform-response`, `normalize-result`, `explain`, `hooks-test`, `doctor`,
`actions`, `docs`, `version`, `log`. The aliases `project-config` and
`local-config` remain supported. `version` is a metadata schema; the current
version CLI has no JSON mode. Regenerate with `go generate ./internal/schema`.

## Exit codes

| Code | Meaning |
| --- | --- |
| Git's status | Passed through after execution, including failure; signals are re-raised. |
| 77 | Content denial from policy or a pre-hook. Wins over mixed hook-error denials. |
| 78 | Fail-closed config, inactive gate, hook/transform/delegate error or unsafe retry. |
| 127 / 126 | Real Git missing / other start failure. |
| 2 | Management usage error. |
| 1 | Management failure, hooks-test denial/error or expectation mismatch, internal wrapper failure. |

`explain` exits 0 even for a predicted policy deny. With expectations,
`hooks test` exits 0 only when all requested final/hook/policy decisions match.
Observe-mode policy/pre-hook denials and all post-hook outcomes preserve Git's
status. Details: [exit status](docs/exit-status.md).

## Agent skill and development

```sh
npx skills add mkusaka/gthok
gthok docs skill
make build
make test
make lint
```

The [agent skill](skills/gthok/SKILL.md) ships in the binary. Docs links/search
URLs use the matching release tag; development builds use `main`.
Architecture, exhaustive action/parameter reference, local resolution and
regeneration instructions are in [docs](docs/architecture.md),
[normalization](docs/normalization.md), [compatibility](docs/compatibility.md),
and [registry generation](docs/registry-generation.md).
