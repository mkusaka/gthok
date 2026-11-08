# Exit status

Once Git or a handled delegate executes, gthok preserves its status and re-raises
its terminating signal. Post-hook failures never change that outcome. Wrapper
origin decisions use the stable stderr prefix **`gthok:`** and JSONL diagnostics.

| Status | Wrapper meaning |
| --- | --- |
| 77 (EX_NOPERM) | Content denial: declarative policy or a pre-hook successfully returned deny. |
| 78 (EX_CONFIG) | Fail-closed configuration/inactive gate; effective pre-hook malfunction; transform fail-closed; delegate error under deny or unsafe fallback. |
| 127 | Real Git missing. |
| 126 | Other process-start failure. |
| 1 | Internal wrapper failure, or management failure. |
| 2 | Management usage error (bad arguments/options). |

A mixed content-deny plus hook-error denial exits **77**, regardless of which
hook finished first. Transform onError deny returns **78 even in observe**.
Observe-mode policy/pre-hook would-deny preserves the executed Git result.
A successful post hook returning deny is diagnostic only.

## Denial stderr

The first denial line is stable:

```text
gthok: denied by hook protected-ref (+1 more)
  - hook protected-ref: Protected branch update rejected.
  - hook scanner: hook timed out
```

A malfunction-only denial includes its error kind, for example:
`gthok: denied by hook scanner (error: timeout)`. Sources include rule, policy,
config (including an inactive gate ID), hook, transform or delegate. Messages
remain redacted/control-sanitized; structured JSON keeps full diagnostic fields.
Content denials take precedence over error-only denials.

## Management diagnostics

- `normalize`: 0 when a result is printed, including explicit unknown results;
  management errors use 1, usage 2.
- `explain`: 0 even when the predicted policy denies; it does not execute hooks
  or the request. Trusted transform/config/analysis failures can still fail.
- `hooks test`: without expectations, 1 on final denial/hook execution error;
  with expectations, 0 only when all specified final/hook/policy decisions match,
  otherwise 1. Usage is 2, never wrapper 77/78.
- `doctor`: 1 if checks fail, 0 otherwise; JSON does not waive failures.
- Other management commands: 0 success, 1 failure, 2 usage.

Delegates reserve 125 with no stdout for not-handled. That triggers real Git
once, never another delegate; unsafe partial-output retries return 78.
See [delegates](delegates.md) and [hook protocol](hook-protocol.md).
