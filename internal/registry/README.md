# Pinned Git registry

`registry` embeds the inventory and grammar extracted from Git **v2.56.0**,
commit `a018953688f1b10bddf91bff8747068f5f4746a4`. Runtime loading uses only
the Go standard library; Git source and a C compiler are not required.

## Generate and validate

From the repository root:

```sh
go run ./internal/registry/gen \
  -git-src "$HOME/src/github.com/git/git" \
  -out internal/registry/data -verify
go build ./internal/registry/...
go vet ./internal/registry/...
go test ./internal/registry/...
```

`go generate ./internal/registry` runs the same generation command. The source
checkout must be at the pinned commit. The generator does not modify or build
that checkout. Without `-verify`, generation omits the optional completion
verification record; use `-verify` to reproduce the checked-in manifest.

Verification requires local Git 2.56.0. It invokes each parse-options builtin's
`--git-completion-helper` in an isolated temporary repository, with no user or
system Git configuration, no prompts, no optional locks, and a per-command
timeout. It makes no network requests. Its manifest records the checked count,
unsupported helpers, and missing/extra long names. Validation does **not**
supply grammar options. Identical source and verification-binary inputs produce
byte-identical artifacts; the regeneration test checks all four artifacts twice.

## Artifacts and API

- `commands.json`: `git.c` builtins and setup flags; every `command-list.txt`
  entry and category; Makefile scripts/standalone programs; remote-curl aliases;
  and Git's option-only modes. Documentation entries have implementation
  `document`, not `external`.
- `grammar.json`: command grammars plus `$global`, `$revision`, and `$diff`.
  Option macros are expanded from the source definitions, including helper
  macros and aliases. Tables from local parsing helpers and external option
  builders are followed; execution-time command calls do not contribute options.
- `source-map.json`: keys such as `push`, `push --force-with-lease`, `commit -m`,
  `stash push --message`, and `$global -C`, with source-relative files, lines,
  and entrypoints.
- `manifest.json`: source pin, generator version, extraction counts, explicit
  adapter/exclusion reasons, and optional completion verification results.
  Coverage counts include all inventoried entries and all extracted source
  tables/declarations, not just options unique to builtin commands.

`Commands()` and `Lookup()` expose `Command.Grammar`, whose fields are also
promoted (`command.Options`, `command.Parser`, `command.Subcommands`).
`RevisionOptions()`, `DiffOptions()`, and `GlobalOptions()` expose shared options.
`Manifest()` returns `ManifestInfo`: Go cannot use `Manifest` as both a type
and function name. All returned data is caller-owned.

`Option.Short` is the short character as a string without `-`; `Long` has no
`--`. `Kind` is the outer option macro, while `Type` is the expanded `OPTION_*`
kind (for example, `OPT__FORCE` / `OPTION_COUNTUP`). `Arity` is `none`,
`required`, or `optional`. `PARSE_OPT_LASTARG_DEFAULT` stays in `Flags`, together
with `required` arity: it requires a value except in the last argv position.
Aliases retain their declared alias target and inherit effective argument,
negation, and flag behavior. `Exclusive` records CMDMODE groups and literal
`die_for_incompatible_opt*` declarations.

## Adapter boundaries

A grammar is **not** a claim that a command is safe or mutation-free.
`Parser` names the source-specific adapter when a simple parse-options table
is insufficient: `global`, `revision`, `diff`, or `manual:<file>`. Scripts,
standalone executables, and hand-written argv loops retain explicit adapters;
the generator does not fabricate exhaustive option tables for them.

`Adapters` additionally marks conditional option tables, optional-subcommand
fallbacks, unresolved option helpers, and the explicitly recognized low-level
callbacks that can consume argv themselves. Unknown constructors, option types,
flags, and low-level callbacks fail generation rather than becoming no-argument
flags. Consumers must honor these adapter requirements, not blindly parse the
union of conditional tables. In particular, cherry-pick and revert share
conditional declarations; the two corresponding completion extras remain
visible in the checked-in manifest. The shared revision grammar also retains
its adapter boundary for numeric and pseudo-option handling.

This is static extraction, not a C preprocessor/interpreter: conditional source
branches are retained and conditional table declarations are marked. Adding
support for an adapter belongs in the normalizer, not in the completion checker.
