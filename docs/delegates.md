# Delegates

A delegate serves a proven retry-safe read through another trusted executable.
It is off unless configured, runs **after** transforms, local resolution, policy
and pre hooks, and cannot lift a denial. The first definite match wins.

```yaml
delegates:
  - id: cached-log
    match:
      level: L1
      effect: read
      operation: git.log.read
    command: ["/opt/local/bin/git-log-cache"]
    timeout: 2s
    onError: fallback
```

This is a contract example, not a bundled cache implementation. See
[examples/delegates.yaml](../examples/delegates.yaml). Install and review the
executable before enabling it. Commands are argv arrays, no implicit shell.

## Eligibility and argv

The **whole invocation** must have classification.retrySafe and mutationFree,
exact native L1 classification, no normalization errors, and no considered
write/unknown operation. A maybe-match never routes. `effect: read` by itself
is insufficient: status index-refresh, file output, external diff/textconv,
editors, interactive input and network execution can invalidate retry safety.

The delegate receives the original Git argv (or successful trusted transform's
execution argv) appended unchanged to its configured command, cwd and execution
environment. Relative executable paths resolve from the config directory;
other argument paths retain ordinary process-cwd semantics. Never configure a
delegate that silently changes the request or starts a write before declining.

Metadata environment includes `GTHOK_DELEGATE_ID`, `GTHOK_ACTION`, `GTHOK_REPO`,
`GTHOK_REAL_GIT` and `GTHOK_NO_DELEGATE=1`. That last value prevents recursive
routing. Setting `GTHOK_NO_DELEGATE=1` for a call disables delegates, not policy
or hooks. Project delegates use executable content/path trust; inactive ones are
ignored, never gates. User/personal trust follows the usual rules.

## Exit and fallback contract

| Delegate outcome | Wrapper behavior |
| --- | --- |
| 125, no stdout | Not handled: run real Git once, never the next delegate. |
| Start failure or timeout before any output | `onError: fallback` (default) runs real Git once; deny returns 78. |
| Timeout after stdout or stderr output | 78, no retry. |
| 125 after stdout | 78, no retry. |
| Other exit status or signal | Pass through unchanged; post hooks see this actual result. |

Exit 125 is permitted to have stderr, but not stdout. A timeout after **either**
output stream is unsafe to retry. Fallback/error emits a `gthok:` warning.
Stdout/stderr stream through monitored pipes, so a delegate does not see TTY
outputs. Non-TTY stdin is privately replayable for fallback; an incomplete live
analysis spool cannot safely route. A declining delegate must not consume TTY
input. Be careful with format/color/interactive behavior when implementing one.

`explain` shows which delegate would run without executing it. `doctor` checks
its executable/permissions. Hooks test never invokes a delegate. The normalizer's
retrySafe proof is deliberately narrow; do not broaden it just to make a cache
eligible. See [normalization](normalization.md) and [exit status](exit-status.md).
