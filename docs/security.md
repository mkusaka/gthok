# Security

**gthok is not an OS security boundary.** It catches mistakes and enforces
policy on requests that pass through its shim/exec entrypoint. A caller can use
the real Git path, another client, a transport helper or a custom program.
Use server-side protected refs and authorization for enforcement across clients.
Git's `--no-verify` only affects Git-native hooks, not gthok.

## Trusted code and local-only resolution

Hooks, transforms and delegates are trusted programs running as your user, with
your environment and credentials. They may read files, start programs or use
the network. Explain runs trusted transforms; hooks test runs transforms/hooks.
Neither is a sandbox. No wrapper check makes a malicious executable safe.

The resolver itself is local-only, using a fixed set of bounded metadata
queries against the discovered real Git. It sets no pager,
`GIT_TERMINAL_PROMPT=0` and `GIT_OPTIONAL_LOCKS=0`; it does not run `ls-remote`,
`push --dry-run`, SSH, credential helpers or network probes. Remote state stays
unresolved and policy maybe-matches conservatively. `normalize --no-resolve`
starts no subprocess at all.

## Project configuration trust

Declarative project policy applies automatically but cannot weaken user policy.
Cloning a repository does **not** activate its executable hooks/transforms/
delegates. Review `gthok config trust --dry-run`, then explicitly trust.

Trust is per executable definition and content digest. Repository-local
executables, hook-specific `files`, and project `trust.include` dependencies
participate. Shared content across clones/worktrees can share trust; unrelated
policy/comment/formatting changes do not untrust an unchanged hook. Definitions
or covered bytes/executable bits/missing-file changes revoke affected digests.
Linked worktrees share their common-directory trust record.

Project commands must be ./relative or absolute. Manifest paths are regular,
repository-relative files, without parent traversal or globs; symlinks must
remain inside the repository. Interpreters, PATH utilities, imports and unlisted
dependencies are not automatically hashed. Absolute external executables are
not content-pinned. List dependencies explicitly when content review matters.
Concurrent malicious replacement is outside this guardrail's guarantees.

User-only `trust.repositories` trusts an exact repository and linked worktrees;
`trust.directories` trusts a directory and descendants on path boundaries.
**Any future code checked out there can run without a new content review.**
Projects cannot trust themselves. User hooks do not require content trust.
Personal `.gthok.local.yaml` is auto-trusted only while Git does not track it;
a tracked copy is ignored to prevent a clone activating arbitrary local code.

Changed/inactive pre gates can fail closed (78) in enforce mode when prior or
partial project trust exists. Entirely never-trusted project executables are
ignored with warnings. Inactive transforms/delegates are ignored, not gates.
Use `config trust --list`, `hooks list`, validate, doctor and explain to inspect
activation, rather than interpreting silence as trust.

### Transform trust and failures

Project transforms need the same reviewed content/path trust as hooks. Inactive
transforms/delegates are ignored, never gates. A trusted transform failure with
onError deny stops Git with 78 even in observe mode; onError allow rolls back
the entire edit chain. External side effects of trusted programs cannot be
rolled back. Review source and dependencies before enabling one, not only its
configuration entry.

## Stored trust and logs

Trust state lives under the private config directory. Exact previously trusted
file bytes may be retained in `trust/blobs` for review diffs (0700 directories,
0600 files); **these copies are unredacted and may contain secrets**. Size
bounds can make content hash-only. Missing/corrupt blobs do not grant trust.
Untrust removes unused retained blobs. Displayed diffs mask credentials unless
--show-secrets; --no-diff omits content output.

Hooks receive raw input by default. Add `redactInput: true` before forwarding
input elsewhere; inspect redactions/truncated/status and deny if a masked or
incomplete value is not safely reviewable. Normal display/log diagnostics are
redacted and content-minimized, but redaction is not a proof of no secrets.
Keep config/log directories private. Do not send full argv, prose or credentials
to an audit service unless explicitly intended.

## Failures and recursion

Invalid user config with raw enforce mode fails closed; observe/undetermined
mode normally runs without policy/hooks with a throttled warning.
`GTHOK_FAIL_CLOSED=1` rejects invalid configuration, including project failures.
Valid project/local overlays cannot disable user policy. Transforms with
onError deny fail closed in either mode.

Real Git discovery rejects gthok/owned shims, checks overrides, and uses depth
and verified nested-call metadata to prevent accidental recursion. Nested
trusted-helper behavior is not an authentication boundary against a user who
controls the machine. Inspect `gthok doctor` after PATH/install changes.

Supported platforms and version limitations are in [compatibility](compatibility.md).
Stable wrapper-origin statuses/prefixes are in [exit status](exit-status.md).
