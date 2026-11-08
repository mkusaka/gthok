# Normalization

Normalize Git argv into operations before deciding whether to run it. Inspect
`classification`, each operation's scopes/capabilities, and target completeness;
`effect: read` alone does not prove a command harmless or safe to retry.

```sh
gthok normalize --no-resolve -- push --force-with-lease origin HEAD:refs/heads/main
gthok normalize --no-resolve -- reset --hard HEAD~1
gthok actions 'ref.*' --json
```

## Result contract

`schemaVersion: 1` and `normalizerVersion` identify the public contract and
classification rules. `registry` records Git `v2.56.0`, source commit
`a018953688f1b10bddf91bff8747068f5f4746a4`, and, when known, runtime
`compatibility: verified|unverified`. Offline inspection can omit compatibility.
`source` includes redacted analysis argv, alias name and global tokens;
`errors` contains non-fatal analysis problems.

Classification dimensions are independent:

- `level: L1|none`: native Git syntax or no recognized command.
- `confidence: exact|heuristic|none`: certainty of operation identity, not success.
- `resolution: complete|partial|deferred|unresolved`: whether target/parameter
  fields are known. Inspect individual `unresolved` entries, including their source.
- `coverage: semantic|conservative|opaque`: depth of understanding.
- `mutationFree`: proved no modeled write, stat-cache refresh, external execution,
  editor or file output. Unknown is false, not a guess.
- `retrySafe`: additionally excludes observable network/interactive execution.
  Delegates require this proof for the **whole invocation**.

Each operation contains `family`, specific `action`, `effect: read|write|unknown`,
`scopes`, `capabilities`, `coverage`, `target`, optional `parameters`/`texts`,
`execution`, optional `evidence`/`unresolved`/`unknownReason`, and `origin`
(command path, mode, source entrypoint). Execution is `will-run`, `maybe`,
`not-selected` or `skipped`. For example, aborted finite update-ref transactions
record their pending ref changes as skipped with evidence.

## Targets and TargetSet semantics

A target may include `repoRoot`, `gitDir`, `commonDir`, `worktree`, `remote`,
credential-free `remoteUrl`/`pushUrl`, and `refs`, `paths`, `revisions`,
`configKeys`. A multi-remote request expands into separate targets, never one
object assembled from unrelated remotes. Remote-ref targets are destination
refs on the remote; fetch/prune affect **local** refs.

| TargetSet kind | Meaning |
| --- | --- |
| `exact` | One known value. |
| `set` | Finite values; `complete` says whether they enumerate every target. |
| `pattern` | Git selectors, ref patterns/refspec expansion or pathspecs, not resolved names. |
| `unknown` | Unknown target, never an empty list masquerading as no target. |
| `none` | Proven absence of that kind of target. |

`complete: false` means known values are not an exhaustive set. A complete
pattern still requires expansion and conservatively maybe-matches policy globs.
An omitted field is not evidence of an empty set. `refs/heads/main`, `HEAD`,
revision expressions, refspecs and pathspecs are different domains. Pathspecs
remain in `parameters.pathspecs` until safely expanded; policy `paths` must not
interpret pathspec magic as its own glob language.

## Scopes and capabilities

Scopes name Git data areas, not severity. On a read operation they describe
what is read; on a write operation what may change.

| Scope | Data area |
| --- | --- |
| `local-ref` / `remote-ref` | Local refs (including remote-tracking refs) / refs on a remote. |
| `index` / `worktree` | Index entries or cache / working files. |
| `config` / `remote-config` | Git configuration / configured remote definitions. |
| `objects` | Object database, packs, graphs and indexes. |
| `stash` / `reflog` | Stash state / reference logs. |
| `submodule` / `worktree-admin` | Nested repositories / worktree administrative state. |
| `hooks` / `file-output` | Native Git hook configuration/execution / explicit output files. |

Capabilities are side channels: `external-exec` (filters, textconv, exec commands,
signature tools), `editor`, `network`, `file-write`, `index-refresh`,
`interactive`, `pager`. They may be conservative possibilities, not proof a
child was actually started. Git-native hooks are distinct from gthok hooks;
`noVerify` records user intent and never disables gthok's pipeline.

`status` normally has `index-refresh`; disabling optional locks can remove it.
Porcelain `diff` may refresh the index and use external diff/textconv.
`--output` and default `format-patch` produce file output. Those reads are not
retry-safe. `push -n` still has `network` and is not a resolver operation.

## Coverage and explicit uncertainty

Semantic means the mode/effect is modeled, not that every target is resolved.
Conservative means syntax is known but effects/selection are over-approximated.
An unknown option on a known command is conservative, **not opaque**.

Opaque coverage requires a named cause: shell alias, arbitrary executable,
unprovided/oversized input, future interactive choice, remote state or
unverified Git version. `unknownReason` values are `unknown-command`,
`unknown-flag`, `external-command`, `shell-alias`, `alias-loop`, `parse-error`,
`input-unavailable`, `input-too-large`, `malformed-request`, `interactive`,
`remote-state`, `unverified-version`. Unknown known syntax must not be labeled
opaque merely because its semantic adapter is incomplete. Some adapters retain
a known write effect with opaque input: do not ignore `unknownReason`.

The model defines version uncertainty, but runtime version enforcement is not
implemented yet; see [compatibility](compatibility.md). Arbitrary commands,
interactive tools and low-level server protocols are intentionally not inferred
from their names. Never convert uncertainty into an allow assumption.

## Parameters by family

Parameters are JSON scalars or arrays, not a second Git parser. Booleans record
effective options, strings retain values (including numeric option text), and
arrays retain repetitions. Absent means unrecorded/unknown, not false. The
matcher supports scalar parameter globs; array/object predicates require a hook.
The following inventory follows the semantic adapters and focused tests.

### Worktree and index

| Family/actions | Parameters and mode distinctions |
| --- | --- |
| `worktree.reset.*` | `mode`: soft/mixed/hard/merge/keep/paths/patch; `intentToAdd`, `refresh`, `recurseSubmodules`. Soft changes refs; mixed adds index; hard/merge/keep add worktree; paths/patch are index-only. An ambiguous bare operand stays conservative. |
| `worktree.restore.paths` | `staged`, `worktree`, `source` (index, HEAD or explicit tree); shared checkout flags below. Index and worktree selection are separate. |
| `worktree.checkout.*`, `worktree.switch.*` | create/reset/orphan/detach/switch and checkout-only paths; `newBranch`, `source`, `revision` when a single editable operand exists; shared `force`, `stage` (normal/ours/theirs), `merge`, optional `overlay`, `recurseSubmodules`. DWIM and checkout.guess may leave branch creation unresolved. |
| `worktree.clean`, `worktree.clean.preview` | `dryRun`, `forceCount`, `nestedRepositories` (more than one force), `ignored` (exclude/include/only for default/-x/-X), `directories`, repeated `exclude`. Interactive selection is opaque; `requireForce` may be unresolved. |
| `index.add.*` | `all`, `update` (also renormalize), `dryRun`, `intentToAdd`, optional `chmod`; update/refresh/preview modes. Refresh is not a mutation-free read. |
| `index.rm.*` | `cached`, `recursive`, `force`, `dryRun`; noncached removal includes worktree/submodule effects. Some dry runs remain conservative due to submodule gitdir absorption. |
| `worktree.mv.*` | `force`, `dryRun`, `operands`; directory/submodule destination resolution remains uncertain. |
| `worktree.apply.*` | `cached`, `index` (also 3-way), `reject`, `reverse`, `unsafePaths`, optional `fakeAncestor`, `inputs`; patch/check/stat modes. Reject/fake-ancestor can write files. Captured patches are not fully decoded into paths. |
| `worktree.sparse-checkout.*` | `patterns`, optional `cone`, `sparseIndex`, `stdin`, `input`, `dryRun`; list/check-rules vs init/set/add/reapply/disable/clean/preview. |
| `index.read-tree*` | `merge`, `reset`, `updateWorktree`, `dryRun`, optional `output`; recursion adds submodule/ref/reflog effects, filters can execute externally. |
| `index.update` | `add`, `remove`, `forceRemove`, `refresh`, `reallyRefresh`, `infoOnly`, optional `chmod`, `cacheInfo`; finite stdin/index-info yields paths. Content updates can store objects/run filters; metadata-only switches do not. |

Common path-reading adapters add `pathspecs` and `pathspecFromFile`.
`--pathspec-file-nul` preserves NUL-separated bytes; line mode handles CRLF and
quoted names. Empty pathspec files can select whole-tree reset/checkout modes.
Missing input is not equivalent to an empty file. External filters and recursive
submodules remain capabilities/scopes even when paths are not enumerable.

### History, stash and notes

| Family/actions | Parameters and text |
| --- | --- |
| `history.commit.*` | create/amend/preview; `amend`, `dryRun`, `all`, `allowEmpty`, `noVerify`, `only`, `include`, optional `fixup`, `squash`, `reuseMessage`, `gpgSign`. Message in `texts.body`; editor/reused/generated final text remains unresolved. |
| `history.merge.*` | start/squash/continue/abort/quit; `squash`, `noVerify`, `commit`, `fastForward` (allow/never/only), optional `revision`, `gpgSign`. Squash leaves HEAD alone but can write ORIG_HEAD; quit changes administrative/stash state. |
| `history.rebase.*` | start/continue/abort/skip/quit/edit-todo/show-current-patch; `noVerify`, `root`, `autosquash`, optional `onto`, `updateRefs`, repeated `exec`, `gpgSign`. Saved todos can execute programs on continue/skip. Interactive/edit-todo is opaque; autostash adds stash scope. |
| `history.cherry-pick.*`, `history.revert.*` | start/apply/continue/abort/skip/quit; `noCommit`, optional `mainline`, `revision`, `gpgSign`. Apply does not update a branch ref. |
| `history.am.*` | start/continue (also --resolved)/abort/skip/quit/retry/allow-empty/show-current-patch; `noVerify`, `inputs`, optional `gpgSign`; supplied mail paths are not fully interpreted. |
| `history.bisect.*` | start/good/bad/skip/next/reset/run/replay/log/terms/visualize/mark. `command` retains every child argv for run. Old/new/view canonicalize to good/bad/visualize; custom terms use conservative mark. Start accepts no-checkout/first-parent/reset-when-found/term options; these currently remain in source argv, not dedicated parameters. |
| `stash.*` | push (also save)/pop/apply/drop/clear/branch/store/create/export/import/list/show. `stash` defaults to stash@{0}; `keepIndex`, `staged`, `untracked` (exclude/include/all), `reinstateIndex`, `newBranch`, `toRef`, optional `output`; messages in `texts.body`. Create stores objects but does not update refs/stash; export may explicitly write another ref. |
| `notes.*` | add/append/copy/edit/remove/prune/merge/list/show/get-ref/preview; `force`, `allowEmpty`, optional `revision`, `stdin`, `mode` (merge start/commit/abort), `dryRun`. Message is `texts.notes`; custom separators/reused blobs can prevent edits. Notes ref follows --ref, GIT_NOTES_REF or local configuration. |

### Refs and remote operations

| Family/actions | Parameters and targets |
| --- | --- |
| `ref.branch.*` | list/create/delete/rename/copy/upstream/description; `force`, `source`, `destination`, `upstream`, `unset`, `deleteMerged`, `dryRun`. -D is a delete, not a separate safety class. Remote-tracking branches are local refs. |
| `ref.tag.*` | list/create/delete/verify; `force`, `annotated`, `signed`; annotation body in `texts.body`. Signing/verification may execute a program. |
| `ref.update`, `ref.delete`, `ref.verify` | `newOid`, `oldOid`, `noDeref`; finite protocol records also include `command`, `arguments`. Dereferencing symbolic refs can leave the target incomplete. A zero new OID is a deletion. |
| `ref.symbolic.*` | read/set/delete/verify; `newTarget`, protocol `command`, `arguments`, `noDeref`; also used for remote set-head. |
| `ref.transaction.*` | start/prepare/commit/abort/option; `command`. Record mutations separately; abort/unfinished explicit transactions mark pending mutations skipped. |
| `ref.pack` | `all`, `auto`, `prune`, repeated `include`, `exclude`. |
| `ref.reflog.*` | show/list/exists/write/delete/drop/expire; `dryRun`, `rewrite`, `updateref`, `all`, `singleWorktree`, `expire`, `expireUnreachable`, `staleFix`. --updateref adds local-ref scope. Safe display log options are recognized; patch/output/custom-format/signature options stay unknown pending side-effect modeling. |
| `ref.replace.*` | list/create/delete/edit/graft/convert; `force`; object-ID lookup determines ref names; edit uses an editor and convert can write files. |
| `ref.list` | show-ref / for-each-ref; no dedicated parameters. Stdin reference filtering can leave target.refs unresolved. |
| `remote.push.*` | update/delete/mirror/matching/tags; `forceMode`: none/unconditional/lease; `leaseMode`: none/tracking/tracking-includes/explicit; repeated `leases`, `refspecs`, `pushOptions`; `dryRun`, `all`, `tags`, `mirror`, `prune`, `followTags`, `atomic`, `setUpstream`, `forceIfIncludes`, `delete`, `recurseSubmodules`, optional `matching`, `exec`, `implicit`. A leading + forces that refspec; empty source deletes; ':' matches remote refs. Prune/follow-tags add possible separate deletes/tags. |
| `remote.fetch` | `dryRun`, `writeFetchHead`, optional `fetchHead`, `all`, `multiple`, `prune`, `pruneTags`, `append`, `force`, `atomic`, `depth`, `deepen`, `unshallow`, `shallowSince`, `updateShallow`, `setUpstream`, `prefetch`, `recurseSubmodules`, `negotiateOnly`, `tags`, arrays `refmap`, `serverOptions`, `shallowExclude`, `refspecs`, optional `exec`, `remoteUpdate`. Targets are local refs, not remote-ref writes. |
| `remote.pull` | Separate integration operation after remote.fetch; `mode`: configured/merge/rebase, optional `squash`, `ffOnly`, `ff`, `autostash`, `commit`, `edit`, `recurseSubmodules`. --dry-run skips integration with evidence; interactive rebase is opaque. |
| `remote.config.*` | add/remove/rename/set-url/set-branches; optional `url`, `oldURL`, `newName`, `branches`, `track`, `head`, `push`, `add`, `delete`, `all`, `tags`, `mirror`, `auto`, `dryRun`. Add -f emits a fetch too. |
| `remote.list`, `remote.show`, `remote.get-url`, `remote.prune`, `remote.ls-remote` | Optional `push`, `all`, `dryRun`, `exec`; remote show -n and URL inspection are local; prune writes local refs and requires unresolved advertised state. |
| `remote.clone` | `bare`, `checkout`, `mirror`, optional `depth`, `unshallow`, `singleBranch`, `branch`, `revision`, `origin`, `separateGitDir`, `recurseSubmodules`, `local`, `shared`, `exec`. Destination path is explicit or derived. |
| `remote.pack.*` | fetch/receive/upload/advertise. No dedicated parameters currently; repository and revision operands become targets. Live receive-pack is opaque input-unavailable. Fetch-pack grammar includes its hand-parsed options; it does not decode the transport protocol. |

Push targets distinguish remote groups and multiple push URLs during local
resolution. Remote advertisements are never fetched to make a target complete.
A lease is a conditional force request, not proof the destination is safe.

### Objects, administration and read commands

| Family/actions | Parameters and distinctions |
| --- | --- |
| `objects.hash.*` | read/write; `write`, `stdin`, `stdinPaths`, `objectType`; filters may execute externally unless disabled. |
| `objects.write-tree`, `objects.commit-tree`, `objects.mktag`, `objects.mktree` | Object writes, not implicit branch updates. commit-tree message is texts.body but has no write-back adapter. No dedicated parameters for these modes. |
| `objects.fast-import`, `objects.fast-export*` | Import remains opaque even with a finite stream; export is read except mark-file output. Optional `output` for export-marks/export-pack-edges. |
| `objects.unpack-objects*`, `objects.index-pack*`, `objects.pack-objects*` | Unpack preview vs write; index-pack `verify`, `stdin`, optional `output`; pack-objects `outputPrefix`, optional `output`. --stdout usually reads, but unpack-unreachable can still write. Companion output names can remain incomplete. |
| `objects.prune*`, `objects.prune-packed*`, `objects.repack*`, `objects.gc` | `dryRun`, `deletePacks` (repack), `prune`: configured/none/explicit string (gc), optional `output` for expire-to/filter-to; GC may pack refs and expire reflogs. |
| `objects.fsck*`, `objects.count-objects`, `objects.verify-pack`, `objects.commit-graph.*`, `objects.multi-pack-index.*` | Read/verify vs write/repack/compact/expire; fsck lost-found has optional output. Other modes have no dedicated parameters. |
| `maintenance.*` | run/is-needed/start/stop/register/unregister; repeated `tasks`, optional `scheduler` (configured or explicit). Default tasks may prefetch; scheduler changes execute programs/write files. |
| `archive.*`, `bundle.*` | Archive create/list/output; bundle create/stdout/unbundle/verify/list-heads. Optional `output`; remote archive has network, custom formats may execute archivers. Bundle '-' means stdout, unbundle stores objects. |
| `worktree.admin.*` | add/list/lock/unlock/move/prune/remove/repair/prune.preview; `forceLevel`, `dryRun`, `checkout`, `detach`, `orphan`, `resetBranch`. Targets identify administrative worktrees/paths; add can create/reset a branch. |
| `repository.init` | init/init-db; `bare`, optional `template`, `separateGitDir`, `initialBranch`, `objectFormat`, `refFormat`. |
| `submodule.*` | status/summary/gitdir/get-default-remote vs add/clone/init/deinit/update/sync/set-url/set-branch/absorbgitdirs/foreach; `recursive`, `remote`, `force`, `init`, `branch`, `defaultBranch`, `strategy` (configured/checkout/merge/rebase), `noFetch`, or foreach `command`. --no-fetch does not rule out cloning missing submodules. Foreach executes arbitrary shell code. |
| `hook.list`, `hook.run` | Native hook inventory vs arbitrary explicit execution; run retains `arguments` and is opaque external-command. |
| `status.show`, `git.help.read`, `git.version.read`, `git.info.read` | No dedicated parameters; status may refresh the index, informational early modes read paths/version/help. |
| `git.<command>.read/output/protocol` | Local inspection commands listed below. Optional `cached` (diff), `pattern` (grep), `batch` (cat-file), `output`, `outputDirectory` (format-patch). External diff/textconv/filter/signature/pager possibilities are capabilities, not fabricated parameter values. Captured cat-file input can name revisions; live batch input remains opaque. |
| `unknown` | No stable parameters. Examine origin, detail, unknownReason, errors and unresolved fields; never assume mutation-free. |

Other known, unmodeled commands explicitly use conservative coverage and a
reviewed reason. Tools that execute arbitrary code or consume a future protocol
have named opaque causes instead. The registry is syntax coverage, not a promise
of semantic coverage for every Git tool.

## Text provenance and stdin

Texts (`body`, `notes`, reserved `title`) carry `value`, `source`, byte count,
redaction count, optional file path, status and truncation. Repeated message
paragraphs join as Git expects. Inline/file/captured stdin are not the same as
editor/fixup/reused final messages. Hooks requiring complete inspection must
reject unavailable/truncated input themselves or set unknown-input policy.

Finite one-way input may be captured and replayed byte-for-byte within bounds.
Bidirectional `update-ref --stdin`, `cat-file --batch`, fast-import and credential
pipes are not read ahead to EOF; a finite regular file can be inspected where
an adapter exists. Finite update-ref supports line/NUL transaction syntax;
finite fast-import is deliberately not decoded. There is no implicit network
resolution. See [architecture](architecture.md) and [transforms](transforms.md).

## Action catalogue

The [complete action catalogue](action-catalogue.md) lists every registered
action with its family, default effect, example scopes and command routes
(omit the leading Git executable in gthok commands). Effects and scopes can
change with flags/input: use normalize for a particular request.
`gthok actions --json` includes example scopes, not an exhaustive scope union for
all possible options. Input-selected actions share `update-ref --stdin` routes;
the transaction document selects the actual operation.
