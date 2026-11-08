# Hook protocol

A gthok hook is a trusted program that reads one JSON document on stdin and
writes at most one JSON response on stdout. Commands are argv arrays, not shell
snippets. Git-native hooks are unrelated: `--no-verify` never bypasses gthok.

## Input envelope

`schemaVersion` is 1. `event` is pre/post (or transform for a request transform),
with `eventId`, `hookId`, `redactions`, `invocation`, `classification`, optional
`operations`, `errors`, `context`, `policy`, `result`, and dependency `previous`.
Empty optional arrays are omitted; treat missing arrays as empty.

`invocation` includes `via: shim|exec`, original `argv`, alias/expanded analysis
argv, cwd, stdout/stdin TTY state, realGit and optional sorted `envChanged`
**names**, never edited environment values. Operations use the public
[normalization contract](normalization.md). Unknown/incomplete targets are not
empty sets. A known action with unavailable text can still require denial.

Pre input includes the policy result. Post input includes the actual execution
`result`: exitCode (-1 for signal), optional signal/coreDump/startError,
durationMs and opt-in stdout/stderr captures. Captures contain base64, total
size and truncated, are bounded, and change Git's output descriptors to pipes.
A successful status does not establish which remote refs changed.

Inspect the actual envelope without running hooks or Git:

```sh
gthok hooks test --print-input -- push origin HEAD:refs/heads/main
gthok hooks test --print-input --hook deny-force-push -- push --force origin main
```

Trusted transforms still run. Display is redacted unless --show-secrets, and a
hook's `redactInput` remains honored even with that flag.

## Responses and outcomes

```json
{"schemaVersion":1,"decision":"deny","reason":"Protected branch","message":"Use a reviewed non-forced update."}
```

`decision` is allow/deny/abstain, never audit. The schemaVersion response field
may be omitted or 1. Exit 0 and empty stdout means abstain. Nonzero exit,
start failure, timeout, cancellation, invalid JSON/version/decision are execution
outcomes, not successful denials; configured `onError` decides their effect.
A successful allow/abstain may include `warning`; warnings do not override deny.

Pre `onError` defaults to deny; post defaults to ignore. Post hooks cannot
change Git's final status. `optional: true` skips a missing/nonexecutable hook,
not a running hook's failure. `enabled: false` disables selection.
`continueOnError: true` is pre-only: an effective denial becomes abstain,
retaining its real outcome/decision and `continuedOnError` diagnostic. Dependencies
count a continued result as satisfied. It does not waive other denials.

Hooks are raw-input by default and inherit credentials. Add `redactInput: true`
when forwarding input to notifiers or other services; `redactions` counts
substitutions, not unique secrets, and zero does not prove secret-free input.
Reject truncated/unavailable input when complete inspection is required.

## Parallel hooks

Each phase runs list items in order, user before project before personal.
Use dependencies for success ordering, not just list position.

```yaml
hooks:
  pre:
    - id: check-ref
      command: ["./hooks/check-ref"]
    - parallel:
        - id: check-message
          command: ["./hooks/check-message"]
        - id: check-files
          command: ["./hooks/check-files"]
    - id: audit
      background: true
      command: ["./hooks/audit"]
    - id: final-check
      dependsOn: [check-ref]
      command: ["./hooks/final-check"]
    - wait: audit
```

`parallel` launches members concurrently and joins at block end. Background
hooks run until an explicit `wait: id` / `wait: [ids]`, `wait-all:`, or phase end.
A phase always joins all started hooks. `runtime.hookConcurrency` defaults to 4.
Dependencies reference earlier hooks in the same phase; missing/failed required
dependencies skip a dependent and appear in `previous`. Empty/nested parallel
blocks, invalid waits and unsupported `cancel` items are rejected.

An effective pre deny skips hooks not yet started; running siblings finish
within their timeouts. Wrapper signals cancel hooks. Pre outcomes combine with
policy and inactive gates; **allow cannot lift deny**. Enforce denials prevent
the requested process and post phase.

## Synthetic tests and diagnostics

`gthok hooks test --json --expect deny -- <args>` runs transforms and hooks for
real, but never the requested command/delegate. Post tests use a synthetic
result (`--phase post --exit-code 1`). `--expect` compares FINAL;
`--expect-hooks` compares allow/deny/abstain; `--expect-policy` compares
allow/audit/deny. Observe-mode would-deny is FINAL audit. No hook denial plus
policy allow is FINAL allow even when the hook aggregate is abstain.

Successful expectations exit 0, mismatch exits 1. Without expectations, FINAL
deny or hook execution error exits 1; usage errors exit 2. Explain never runs
hooks, so it cannot predict their responses.

Terminal hook text is credential-redacted and control-sanitized, limited to
40 lines / 4 KiB per entry and 500 Unicode characters per line. JSON/log values
retain structured data rather than terminal escaping. Wrapper denials use the
stable `gthok:` prefix; see [exit status](exit-status.md).

Examples: [deny force push](../examples/hooks/deny-force-push/main.go),
[audit history](../examples/hooks/audit-history/main.go), and
[configuration](../examples/config.yaml). Build/install instructions are in the
[README](../README.md#hooks-and-transforms). The examples use only Go's standard
library plus this repository's protocol/model types.

## Request transforms

Transforms use the same input envelope with `event: transform`, `phase: pre`,
and no policy decision. They return a separate response, not hook allow/deny.
See [transforms](transforms.md) for editable paths and round-trip checks.

## Transform environment edits

A transform may return a flat env map: strings set variables and null unsets.
Names/values are validated; private GTHOK variables cannot be changed. Edited
values are never displayed in transform diagnostics, even with --show-secrets;
only changed names and transform IDs are reported. Selection uses the request
and environment before that entry, with earlier successful edits visible.
Credentials and subprocess side effects make transforms trusted code, not a
sandbox. See [security](security.md).
