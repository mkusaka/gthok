---
name: gthok
description: Inspect and use gthok, the semantic Git policy wrapper. Use when a Git command is blocked, when checking force-push/reset/ref-delete effects, or when configuring or testing gthok policy, hooks, transforms, delegates, and executable trust. Prefer its installed offline docs and actual normalization over guessing from command names.
---

# Use gthok safely

Keep policy enforcement intact. Do not bypass a denial by using the real Git
path, changing PATH/config/trust, or switching clients without the user's
explicit authorization. gthok is a convenience guardrail, not an OS boundary;
server-side branch protection remains authoritative across clients.

## Inspect before executing

1. Read the installed version's docs rather than assuming main matches it:
   ```sh
   gthok version
   gthok docs search "force push lease"
   gthok docs normalization
   ```
   Search accepts one English query and returns at most five JSON hits with
   section IDs and matching-release source URLs. Fetch a hit with `gthok docs <id>`.
2. Find the action, default effect, example scopes and command routes:
   ```sh
   gthok actions 'remote.push.*' --json
   gthok actions 'ref.*' --json
   ```
   Catalogue scopes describe examples, not every possible option combination.
3. Normalize the **actual** Git argv, without a leading git executable:
   ```sh
   gthok normalize --no-resolve -- push --force-with-lease origin HEAD:refs/heads/main
   gthok normalize -- reset --hard HEAD~1
   ```
   --no-resolve launches no subprocess. Ordinary normalization resolves only
   local metadata, never advertised remote refs or network state.
4. Explain policy/selection without running the requested Git command:
   ```sh
   gthok explain --no-transforms --json -- push --force origin HEAD:refs/heads/main
   ```
   Without --no-transforms, explain runs trusted transform programs for real.
   It never runs hooks, delegates or the requested command; exit 0 can still
   contain a predicted deny.

## Read the result conservatively

Check every operation, classification.coverage/resolution/retrySafe, scopes,
capabilities, unresolved fields and TargetSet.complete. Read is not necessarily
safe: status refreshes the index, diff can execute textconv or write output.
Unknown/pattern/incomplete targets are not empty targets. AND conditions must
hold on one operation/target, not across separate operations.

Policy refs are full ref-name globs (`refs/heads/main`), not refspecs (`+src:dst`).
Paths are repository-relative globs, not Git pathspec magic. Maybe-deny rules
and maybe-hook selection are conservative; transforms/delegates require a
**definite** match. Allow never lifts another deny.

## Test policy and trusted hooks

```sh
gthok config validate
gthok hooks list --json
gthok hooks test --expect deny -- push --force origin HEAD:refs/heads/main
gthok hooks test --expect-hooks abstain --expect-policy deny -- push --delete origin refs/heads/main
gthok hooks test --print-input -- commit -m message
```

Hooks test executes trusted transforms/hooks, but never the request/delegate.
Print-input executes transforms but not hooks. Post tests use synthetic status:
`--phase post --exit-code 1`. Expectations compare final/hook/policy separately;
observe would-deny is final audit. A matching expectation exits 0 even for deny.
Do not claim test success from the command name alone; inspect exit/output.

Raw hook inputs inherit credentials by default. Avoid --show-secrets or copying
raw input into reports. Use redactInput when forwarding data. Editor/truncated/
unavailable messages cannot be transformed safely; inspect the installed
transform docs rather than inventing a writable JSON pointer.

## Execute and diagnose

```sh
gthok exec -- status --short
# With the installed PATH shim, ordinary git uses the same pipeline.
gthok doctor --json
gthok schema normalize-result
gthok docs security/project-configuration-trust
```

Git's --no-verify does not bypass gthok. Exit 77 is content denial; 78 is a
fail-closed config/gate/hook/transform/delegate error; 127 means Git missing,
126 another start failure. Executed Git statuses/signals pass through unchanged;
management usage errors are 2 and failures are 1.

Configuration layers are user → project → personal local. Project executables
need reviewed content/path trust, and project/local policy cannot weaken user
policy. A personal local file is auto-trusted only while untracked. Never trust
repository code simply to silence an inactive-gate error. Review its definitions,
executable and dependencies first and ask the user to authorize trust changes.

Report what was inspected, the actual classification/decision, any unknown
fields and which requested commands were **not** executed. If a deny is expected,
stop and explain it; do not retry through a bypass.
