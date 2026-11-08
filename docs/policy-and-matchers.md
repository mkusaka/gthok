# Policy and matchers

Use action/effect/target matchers, not a command-name denylist. Rules can deny,
audit or warn. Hooks can allow, deny or abstain; **allow never overrides a deny**
from another rule, gate or pre-hook.

## Configuration layers

User `config.yaml` loads before repository `.gthok.yaml` and personal
`.gthok.local.yaml`. Discovery follows the effective Git repository, including
root options and environment. Projects and personal overlays can tighten user
policy and append rules/hooks, not relax it. Runtime/logging are user-only.
Project policy does not need executable trust. Personal local executables are
auto-trusted only while that file is untracked.

`mode: observe` is the default: run analysis/hooks and record would-deny while
preserving Git's status. `mode: enforce` blocks on policy/effective pre-hook
denials. A transform with `onError: deny` is fail-closed in **both** modes.
`unknownMutation: allow|audit|deny` handles unclassified potentially writing
requests; `unknownInput` independently handles unavailable/too-large input.
Both unknown defaults are audit. Unknown is never a proof of safety.

## Same-operation AND

All fields of one match block are AND on **the same operation and target**.
Within list-valued criteria, entries are OR. A multi-operation pull/push cannot
satisfy an effect on one operation and a ref/remote condition on another.
`unless` entries are OR exclusions, applied only on a definite match to that
operation. A maybe exclusion never grants permission.

```yaml
version: 1
policy:
  mode: enforce
  rules:
    - id: no-forced-protected-push
      match:
        operation: "remote.push.*"
        scopes: remote-ref
        remotes: origin
        refs: [refs/heads/main, "refs/heads/release/**"]
        parameters: {forceMode: unconditional}
      decision: deny
      reason: Use a reviewed update instead of an unconditional force push.
```

## Matcher fields

| Field | Meaning |
| --- | --- |
| `operation` | Case-sensitive glob on action, not family. `remote.push.*` covers descendants; `remote.push` does not. |
| `command` | Git command path prefix, for example push or config. Prefer semantic actions for policy. |
| `effect`, `level` | read/write/unknown and L1/none. |
| `scopes`, `capabilities` | At least one listed item must occur on this operation. Unknown operations with missing data maybe-match. |
| `repoRoots`, `worktrees`, `commonDirs` | Filesystem globs, cleaned/absolute, symlinks resolved where possible. A whole ** component spans zero or more path components. |
| `remotes` | Exact, case-sensitive remote names. |
| `remoteURLs` | URL globs; scheme/host case-insensitive, paths case-sensitive/cleaned, credentials ignored. scp-like URLs normalize as SSH. Prefer pushUrl if present. |
| `refs` | Full ref-name globs. Short branch names and refspec syntax are rejected. |
| `paths` | Repository-relative globs; not Git pathspecs. |
| `parameters` | Map of normalized scalar parameter names to text globs, AND within this operation. Arrays/objects cannot be evaluated as scalar values. |
| `env` | Invocation environment before hook-specific overrides, after earlier transforms. Names are validated; all entries must match. |
| `result`, `exitCode` | Post-only success/failure or numeric Git status selection. |

Use `refs/heads/release/*` for one component, `refs/heads/release/**` for arbitrary
nested branch names. **Do not put `+src:dst`, `:dst`, `main`, or pathspec magic in
`refs`.** A refspec is a Git instruction, not a ref-name pattern.
Similarly, `paths: ["src/**"]` is a policy glob; `:(glob)src/**` is a Git
pathspec and not accepted as policy syntax. Unknown, pattern, incomplete or
pathspec-magic targets remain maybe-matches rather than false negatives.

Scalar parameter/env globs use case-sensitive `path.Match`; `"*"` means present
and nonempty (environment values can include `/`), `""` means absent or empty.
Environment values are caller-controlled guardrails, not authenticated identity.
Do not use a parameter predicate to infer data an adapter did not record.

## Tri-state matching

A match is yes, no or maybe. Unresolved remote names/URLs, refs, paths or repo
metadata are **not** a no.

| Consumer | A maybe-match |
| --- | --- |
| Deny/audit/warn rule | Applies conservatively by default. `onUnresolved: ignore` skips that rule only. |
| Pre/post hook | Selected conservatively. The hook must handle unavailable target/input itself. |
| Transform/delegate | Not selected; request edits/routing require definite identity. |
| `unless` | Does not exclude. |

`decision: warn` annotates execution without denying. It does not change the
policy decision or expectation vocabulary. Rule, transform and successful-hook
warnings remain structured in explain/hooks-test/log output and are printed
with the `gthok:` prefix only when execution is permitted. A hook's warning
alongside an effective deny is not another allow decision.

## Inactive gates and trust

A pre-hook with effective `onError: deny` and without `continueOnError` is a gate.
In enforce mode, a matching changed-since-trust gate, or an untrusted gate while
another project hook is trusted/enabled, can fail closed with 78. A completely
never-trusted project's executables are ignored with a warning, not silently
executed. Inactive transforms/delegates are ignored, not gates.
See [security](security.md#project-configuration-trust).

Check rules without running the requested Git command:

```sh
gthok config validate
gthok explain --no-transforms --json -- push --force origin HEAD:refs/heads/main
gthok hooks test --expect-policy deny --expect deny -- push --force origin HEAD:refs/heads/main
```

Explain predicts selection, not hook outcomes. Hooks test runs trusted code.
See [normalization](normalization.md) for parameters and uncertainty.
