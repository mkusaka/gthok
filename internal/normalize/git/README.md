# Git syntax / semantics boundary

`Normalize(model.Invocation)` is pure: it uses only invocation data and the
embedded registry. Alias expansion is analysis-only. `Invocation.Argv` is never
rewritten, and no filesystem, Git process, environment, or stdin is read here.

Semantic families add a `semantics_<family>.go` file with:

```go
func init() { register("command", classifyCommand) }
func classifyCommand(c *Context) []model.Operation { /* ... */ }
```

The longest registered command path wins (`"stash drop"` before `"stash"`).
Registration is init-time only. The seed `semantics_push.go` is deliberately
conservative and should be replaced/deepened by the remote semantics worker.

`Context` contains:

- `Invocation`: original execution argv, environment, bounded input and aliases.
- `Argv`: expanded analysis argv (including root options).
- `Global`: `gitargs.Global`; root options remain verbatim in `Tokens`.
- `Parsed`: `ParsedCommand`, with canonical `Path`, ordered `Options`,
  `Positionals`, `AfterDashDash`, `Unknown` and `Help`.

`Parsed.Option(name)` returns the last spelling/alias/short-name match;
`Parsed.Has(name)` additionally requires it not to be negated.
`Parsed.Values()` returns positional values, including those after `--`.
An option's `Negated` is relative to its declared name: `--verify` negates the
registry's `no-verify`, while `--no-verify` does not.

All `Span` indices refer to **Context.Argv**, not execution argv; end indices
are exclusive. `ValueIndex == -1` means no explicit value. `ValueOffset` is a
byte offset for attached values. Bundles retain sibling flags in `Span.Keep`
and `Span.KeepGroups` for write-back. Alias-expanded locations cannot be used
against execution argv without a separate alias write-back adapter.

Conflicting legacy/subsection alias definitions in an unordered
`Invocation.Aliases` map fail closed (`parse-error`); inline root config has a
known order and is applied last. The runtime must preserve config precedence
when loading aliases if it wants to resolve those collisions exactly.

Unknown command flags never reach semantic adapters. `KEEP_UNKNOWN_OPT`
retains them in the parsed structure, but normalization still reports
`unknown-flag` with conservative coverage instead of treating them as safe.
Unimplemented manual parser flags are likewise conservative, not opaque.
`NeedsStdin` must not request update-ref/cat-file/fast-import/credential protocol
input: a runtime may attach a live bidirectional pipe. Captured finite input
already supplied in `Invocation.Stdin` remains available to family adapters.
