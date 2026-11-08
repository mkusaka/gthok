# Transforms

Use `transforms.pre` only for trusted, restricted request edits. Hooks judge a
request; transforms can rewrite only values with a Git write-back adapter.
An arbitrary normalized JSON pointer is not permission to alter Git semantics.

```yaml
transforms:
  pre:
    - id: commit-trailer
      match: {operation: [history.commit.create, history.commit.amend]}
      command: ["./transforms/commit-trailer"]
      timeout: 5s
      onError: deny
```

The [commit-trailer example](../examples/transforms/commit-trailer/main.go)
appends `Origin: gthok-example` to an explicit `-m`/`-F` commit message and is
idempotent. It intentionally fails for editor/unavailable messages; it does not
claim to edit the message an editor will later produce. See
[examples/config.yaml](../examples/config.yaml) and README build instructions.

## Response and write-back

Input has schemaVersion 1, event transform, phase pre. A successful response:

```json
{"schemaVersion":1,"set":{"/operations/0/texts/body/value":"message\n\nOrigin: gthok-example\n"}}
```

The response supports `set`, `unset`, flat `env`, and `warning`. Empty stdout is
a no-op. Supported pointer namespaces are:

- `/operations/<index>/texts/{title|body|notes}/value`
- `/operations/<index>/parameters/<escaped-key>`
- `/operations/<index>/target/remote`

A pointer must also have an adapter on that exact operation. Typical adapters
cover commit/tag/merge/notes message `-m`/`-F`, one-to-one flags, explicit remote
operands and single revision/ref operands. The namespace is intentionally wider
than the installed adapters; unsupported edits fail closed rather than being
silently dropped. commit-tree text, for example, currently has no write-back.

Editor/future/reused/generated input, truncated/unavailable text, shared
ambiguous sources and non-one-to-one edits are rejected. File/stdin edits use
private temporary transport and byte-exact replay; they never modify the input
file. Temporary transport is removed when finished. Inline argument boundaries
and unrelated short-option bundles are preserved.

## Round-trip invariants

For every edit, rewrite execution argv/input, re-normalize and compare semantic
projections. Operation count, action/family/effect, execution, coverage,
capabilities/scopes and all nonedited fields must stay unchanged. Only
adapter-declared alternate views may change. An edited revision that changes a
mode or affected ref is not allowed merely because the new argv is valid Git.
Resolution happens after the transform chain, so edited remotes resolve from
local metadata only.

Alias expansion is analysis-only without request edits. No-op/environment-only
transforms preserve original argv; actual request edits use verified expanded
argument locators where supported. Adapters can reject alias-derived ambiguous
sources rather than guessing execution spans.

## Selection, environment and failure

Entries run serially in user/project/personal order. Only definite matches
select a transform; a maybe-match does not. Each entry sees previous successful
edits, is selected once, and does not retrigger itself. Project transforms need
the same content/path trust as hooks; untrusted transforms are ignored, not gates.
Default timeout is 5s; configured timeouts are bounded.

An env response uses string values to set and null to unset. Names must be valid,
values cannot contain NUL, and reserved private wrapper variables are forbidden.
Configured hook/delegate env remains separate. Changed environment values never
appear in transform diagnostics; only names and originating IDs do.

`onError: deny` stops execution with **78 even in observe mode**.
`onError: allow` rolls back the **entire chain** of argv/input/environment edits,
skips later entries, and evaluates the original request/environment. Filesystem,
network or other external side effects of the transform program itself cannot
be rolled back: review trusted code carefully.

`explain` runs transforms for real; `explain --no-transforms` does not.
`hooks test` runs transforms before hooks; normalize does not run transforms.
Schema: `gthok schema transform-response`. See [hook protocol](hook-protocol.md)
and [security](security.md).
