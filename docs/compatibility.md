# Compatibility

The shipped grammar is pinned to **Git v2.56.0**, commit
`a018953688f1b10bddf91bff8747068f5f4746a4`. Runtime needs an independently
installed Git, but neither Git source nor a C compiler. Supported targets are
Linux/macOS × amd64/arm64 with CGO_ENABLED=0; Windows is out of scope.

## Version verification gap

The model supports registry.compatibility verified/unverified and the named
`unverified-version` uncertainty cause. **The current runtime does not implement
`runtime.gitCompatibility: conservative|verified-only`, or populate/enforce the
real Git version compatibility check.** Do not add that setting to config: it is
not a recognized runtime field. Offline normalization can omit compatibility.
Until version enforcement lands, operators must pin/verify Git v2.56.0
independently. Other versions may change accepted options or effects; a parsed
request is not evidence the installed Git matches the registry.

## Intent, not execution success

Normalization reports modes, possible effects and local targets before Git
runs. It does not prove a lease will succeed, a ref will be updated, a merge
will commit, or an editor/interactive selection will be safe. Remote state is
not queried. Unknown options remain conservative; named unavailable input,
arbitrary execution and future interactive choice can be opaque.

Git aliases expand for analysis only. Shell aliases/external commands stay
unknown/opaque. Registry syntax coverage is broader than semantic coverage;
unmodeled known commands have explicit conservative reasons. See
[normalization](normalization.md) and [registry generation](registry-generation.md).

Fetch-pack has a hand-parser grammar; its transport stream is not decoded.
Reflog default/show supports safe log display options (oneline/count/date/decorations)
but patch, output-file, custom format and signature flags intentionally stay unknown until its
semantic adapter models those side effects. Bisect start/terms hand options
and arbitrary run child argv are recognized; custom terms remain conservative.

## Descriptors, capture and stdin

Normal execution inherits descriptors; gthok does not emulate a PTY.
Opt-in post-hook capture and delegated execution use pipes, changing color/TTY
behavior. Captures are bounded and binary-safe base64. Git's original status
and signals pass through after execution; post-hook errors cannot replace them.

Bidirectional protocols are not read ahead to EOF from live pipes. Finite files
may be captured where an adapter supports them; update-ref transactions support
line/NUL input, while fast-import remains deliberately opaque even when finite.
Unavailable/too-large input must never be mistaken for no input.

## Configuration and schema compatibility

Config version and normalization/hook schemaVersion are 1. Additive fields may
arrive without a version bump; consumers should use the generated schemas and
tolerate additive fields according to their own policy. Project unknown fields
can generate notes rather than silently changing user policy; user config is
strict. Legacy names `runtime.resolveAPI` and management `--allow-api` remain
for compatibility, but resolution is **local Git metadata only**, never network.
`runtime.resolveAPI: never` disables implicit local resolution.

Schemas cover config/project/local, hook input/response, transform response,
normalize result, explain, hooks-test, doctor, actions, docs, version metadata
and JSONL logs. Doctor currently serializes Go field names (`Name`, `Status`,
`Detail`, `Fix`); the schema preserves that casing. Version is text-only in the
CLI; its schema describes a metadata object, not a nonexistent --json flag.
The docs schema accepts either a section-tree array or search-result array.

## Documentation versions

The binary embeds README, public Markdown and the agent skill; search is offline.
Release source URLs point to `https://github.com/mkusaka/gthok/blob/vX.Y.Z/...`.
Unreleased/development version strings use main rather than inventing a tag.
Do not assume current online main docs match an older binary; use `gthok docs`.
