# Architecture

The wrapper evaluates Git's requested effects before execution. Analysis never
silently rewrites execution argv; only a trusted, round-trip-checked transform
may change the request.

## Pipeline

```text
entrypoint (git shim or gthok exec)
  -> minimal Git global-option parse
  -> configuration presence / effective repository discovery
  -> layered configuration and executable trust
  -> real Git discovery and recursion checks
  -> required input acquisition
  -> syntax normalization and semantic classification
  -> trusted transforms, serially; re-normalize after each edit
  -> local target resolution
  -> policy + inactive gates + pre hooks
  -> deny OR first retry-safe delegate / real Git
  -> post hooks -> original status or signal
```

With no applicable configuration, repository discovery may read the filesystem,
but Git is exec'd directly without starting the normalizer, logger, spool or
hook runner. A user configuration enables processing in every repository.

## Effective repository and argv

The root parser follows Git's `git.c:handle_options`, not a generic flag parser.
Ordered `-C`, `-c`, `--config-env`, `--git-dir`, `--work-tree`, `--namespace`,
`--bare`, pager/pathspec switches and early informational modes are distinct.
Git environment such as `GIT_DIR` and `GIT_WORK_TREE` participates in repository
discovery. A rule in the caller's checkout must not accidentally apply to a
request redirected to another repository.

Plain aliases expand only the command word for analysis. Shell aliases and
arbitrary executables stay explicitly opaque. Original execution argv remains
unchanged without a trusted request edit. File and stdin edits use private
transport, never overwrite the source file, and are cleaned up after execution.

## Packages and contracts

| Package | Responsibility |
| --- | --- |
| `internal/gitargs` | Git root options and effective starting directory. |
| `internal/registry` | Embedded, source-derived command/option grammar and source map. |
| `internal/normalize/git` | Alias analysis, command parsing, Git modes/effects and action routes. |
| `internal/model` | Frozen normalization/target/text contracts. |
| `internal/resolve` | Bounded local metadata queries through the real Git. |
| `internal/config`, `internal/policy` | Layering, trust and tri-state same-operation matching. |
| `internal/hooks`, `internal/protocol` | Trusted process execution and JSON protocol/scheduling. |
| `internal/transforms`, `internal/delegates` | Restricted request edits and retry-safe alternate execution. |
| `internal/process`, `internal/cli` | Supervision, descriptor inheritance, signals and entrypoint orchestration. |
| `internal/redact`, `internal/logging` | Display redaction and content-minimized JSONL diagnostics. |
| `internal/docsearch`, `internal/schema` | Embedded offline documentation and generated JSON Schemas. |

Normalization is syntactic/semantic intent, not proof of Git success or of a
remote ref update. One invocation can produce many operations: pull includes
fetch plus integration; mixed push updates/deletes remain separate; resolution
can expand remote groups or multiple push URLs.

## Local resolution

The resolver has a fixed query builder for `rev-parse`, read-only
`symbolic-ref`, config reads, `for-each-ref`/`show-ref`, `remote get-url`, limited
`ls-files`, and bounded `cat-file` type/OID queries. Queries use the discovered
real Git, no pager, `GIT_TERMINAL_PROMPT=0`, and `GIT_OPTIONAL_LOCKS=0`.
It does not run `push --dry-run`, `ls-remote`, SSH, credential helpers or any
network probe. Remote state stays unresolved.

`normalize --no-resolve` is the no-subprocess inspection path. `explain` may run
trusted transforms, and `hooks test` really runs hooks: neither is a sandbox.

## Execution and supervision

Without post-hook/capture/live-stdin requirements, exec replacement preserves
Git's inherited descriptors. Supervision is used when needed for post hooks,
signals or byte-exact replay. Capture is opt-in, bounded and base64 encoded;
pipes change the child's TTY behavior. Delegates always use output monitoring
to decide whether fallback can safely happen.

See [hook protocol](hook-protocol.md), [delegates](delegates.md),
[security](security.md), and [exit status](exit-status.md).
