// Package model holds the canonical, versioned data model shared by every
// gthok layer: normalizer output (Result/Operation) and the invocation handed to
// normalizers.
//
// This package is the hook JSON contract. Optional additive fields preserve
// schemaVersion; incompatible changes require a new protocol version.
package model

// SchemaVersion of Result and of the hook protocol.
const SchemaVersion = 1

// Level is the normalization layer that produced a classification: L1 for a
// native Git command, none when nothing was classified.
type Level string

const (
	LevelNone Level = "none" // nothing could be classified
	LevelL1   Level = "L1"   // native git command
)

// NormalizerVersion identifies the operation classification rules.
const NormalizerVersion = "0.0.1"

// Coverage says how deeply the normalizer understood the request.
type Coverage string

const (
	CoverageSemantic     Coverage = "semantic"     // command mode and effects classified from Git semantics
	CoverageConservative Coverage = "conservative" // syntax known; effects over-approximated
	CoverageOpaque       Coverage = "opaque"       // an explicit, named cause hides the effect (see unknownReason)
)

// Compatibility says whether the real Git version matches the pinned registry.
type Compatibility string

const (
	CompatibilityVerified   Compatibility = "verified"
	CompatibilityUnverified Compatibility = "unverified"
)

// Registry identifies the Git source snapshot the grammar was generated from.
type Registry struct {
	Git           string        `json:"git" doc:"Pinned Git release, e.g. v2.56.0."`
	Commit        string        `json:"commit" doc:"Pinned Git source commit."`
	Compatibility Compatibility `json:"compatibility,omitempty" enum:"verified,unverified" doc:"Whether the running Git matches the pinned release; omitted when unknown (offline)."`
}

// Confidence says how sure the normalizer is about the *identity* of the
// operations it reports.
type Confidence string

const (
	ConfidenceExact     Confidence = "exact"     // syntactically certain (AST field / typed route / known command)
	ConfidenceHeuristic Confidence = "heuristic" // inferred, may be wrong
	ConfidenceNone      Confidence = "none"      // nothing recognised
)

// Resolution says how complete targets/parameters are.
type Resolution string

const (
	ResolutionComplete   Resolution = "complete"   // every target field needed is known
	ResolutionPartial    Resolution = "partial"    // some fields unresolved (see Operation.Unresolved)
	ResolutionDeferred   Resolution = "deferred"   // resolvable later by the resolver (git/API), not yet done
	ResolutionUnresolved Resolution = "unresolved" // cannot be resolved
)

// Effect is the side-effect class of an operation.
type Effect string

const (
	EffectRead    Effect = "read"
	EffectWrite   Effect = "write"
	EffectUnknown Effect = "unknown" // never treat as safe
)

// Execution says whether an operation will actually run as part of the request.
type Execution string

const (
	ExecutionWillRun     Execution = "will-run"     // default
	ExecutionMaybe       Execution = "maybe"        // e.g. @include(if: $unresolvedVar)
	ExecutionNotSelected Execution = "not-selected" // operation not chosen by operationName; Evidence explains
	ExecutionSkipped     Execution = "skipped"      // @skip(if:true)/@include(if:false) proven; Evidence explains
)

// Result is the output of normalization for one git invocation.
type Result struct {
	SchemaVersion     int            `json:"schemaVersion" const:"1" doc:"Normalization and hook protocol version."`
	NormalizerVersion string         `json:"normalizerVersion" doc:"Version of the operation classification rules."`
	Registry          Registry       `json:"registry"`
	Classification    Classification `json:"classification"`
	Operations        []Operation    `json:"operations"`
	// Source is the original, redaction-safe description of what was invoked.
	Source Source `json:"source"`
	// Errors are non-fatal problems (parse errors, unreadable input, ...).
	// A non-empty Errors with an unknown operation means "could not tell".
	Errors []string `json:"errors,omitempty"`
}

type Classification struct {
	Level      Level      `json:"level" enum:"none,L1" doc:"Normalization layer: native git command or none."`
	Confidence Confidence `json:"confidence" enum:"exact,heuristic,none" doc:"Certainty of operation identity, not execution success."`
	Resolution Resolution `json:"resolution" enum:"complete,partial,deferred,unresolved" doc:"Completeness of targets and parameters; inspect unresolved entries."`
	// MutationFree is true only when the normalizer *proved* that no write
	// happens (e.g. a known read-only command). It is false when unknown.
	// Never set it on guesses.
	MutationFree bool `json:"mutationFree"`
	// Coverage is the weakest coverage of any operation.
	Coverage Coverage `json:"coverage" enum:"semantic,conservative,opaque" doc:"Depth of understanding; opaque always carries a named cause."`
	// RetrySafe is true only when every operation may run twice without a
	// Git data change, external execution or file output (delegate fallback).
	RetrySafe bool `json:"retrySafe"`
}

// Operation is one semantic operation. A single invocation may yield many
// (e.g. a push to a remote group yields one per remote).
type Operation struct {
	// Family groups operations with related meaning, e.g. "remote.push".
	// Unknown operations use family "unknown".
	Family string `json:"family"`
	// Action is the specific action, e.g. "ref.update", "ref.delete",
	// "remote.push.update".
	// Hook/policy operation globs match against Action.
	Action string `json:"action"`
	Effect Effect `json:"effect" enum:"read,write,unknown" doc:"Side-effect classification; unknown never proves safety."`
	// Scopes are the Git data areas the operation may change (write) or reads.
	Scopes []Scope `json:"scopes,omitempty" enum:"local-ref,remote-ref,index,worktree,config,objects,stash,reflog,remote-config,submodule,worktree-admin,hooks,file-output" doc:"Git data areas touched."`
	// Capabilities are side channels beyond Git data: external programs,
	// editors, network, explicit file output, index stat refresh.
	Capabilities []Capability   `json:"capabilities,omitempty" enum:"external-exec,editor,network,file-write,index-refresh,interactive,pager" doc:"Side channels the request may use."`
	Coverage     Coverage       `json:"coverage" enum:"semantic,conservative,opaque" doc:"Depth of understanding of this operation."`
	Target       Target         `json:"target"`
	Parameters   map[string]any `json:"parameters,omitempty" doc:"Action- and layer-specific JSON values; keys are documented in docs/normalization.md."`
	Texts        *Texts         `json:"texts,omitempty"`
	Execution    Execution      `json:"execution" enum:"will-run,maybe,not-selected,skipped" doc:"Requested execution state, not proof of server-side effects."`
	// Evidence explains Execution != will-run, or other proofs.
	Evidence   string       `json:"evidence,omitempty"`
	Unresolved []Unresolved `json:"unresolved,omitempty"`
	// UnknownReason is set (non-empty) iff Family == "unknown" or Effect == unknown.
	UnknownReason UnknownReason `json:"unknownReason,omitempty" enum:"unknown-command,unknown-flag,external-command,shell-alias,alias-loop,parse-error,input-unavailable,input-too-large,malformed-request,interactive,remote-state,unverified-version,invalid-option-combination" doc:"Machine-readable reason for an unknown or opaque classification."`
	// Detail is free-form human-readable explanation.
	Detail string `json:"detail,omitempty"`
	// Origin ties the operation back to its source (command path).
	Origin Origin `json:"origin"`
	// Editable contains execution-only locators, excluded from hook/log JSON.
	Editable map[string]EditLocator `json:"-"`
}

// Texts contains normalized prose supplied by a command or API request.
// Nil fields mean that no text was supplied or identified for that purpose.
type Texts struct {
	Title *Text `json:"title,omitempty"`
	Body  *Text `json:"body,omitempty"`
	Notes *Text `json:"notes,omitempty"`
}

// Text describes prose available before gh runs. Bytes counts original bytes
// before redaction, including the full size of truncated input when known.
// Unavailable input has an empty Value and an Operation.Unresolved entry.
// Status explains unavailable stdin; zero Bytes then means an unknown size.
type Text struct {
	Value      string      `json:"value"`
	Source     string      `json:"source" enum:"inline,file,stdin,editor,fill,template,unknown" doc:"Text input provenance; generated/editor text is not the final submitted value."`
	Path       string      `json:"path,omitempty"`
	Status     StdinStatus `json:"status,omitempty" enum:"not-captured,tty,too-large,timeout,error" doc:"Unavailable stdin status; omitted on successful capture."`
	Truncated  bool        `json:"truncated,omitempty"`
	Bytes      int64       `json:"bytes"`
	Redactions int         `json:"redactions"` // credential substitutions in this text
}

// UnknownReason is a machine-readable reason for an unknown classification.
type UnknownReason string

const (
	UnknownNone               UnknownReason = ""
	UnknownCommand            UnknownReason = "unknown-command"            // git subcommand not in registry
	UnknownFlag               UnknownReason = "unknown-flag"               // unrecognised option made parsing unsafe
	UnknownExternal           UnknownReason = "external-command"           // git-<name> on PATH / arbitrary executable
	UnknownShellAlias         UnknownReason = "shell-alias"                // alias starting with "!"
	UnknownAliasLoop          UnknownReason = "alias-loop"                 // alias expansion cycle / depth limit
	UnknownParseError         UnknownReason = "parse-error"                // arguments could not be parsed
	UnknownInputUnavailable   UnknownReason = "input-unavailable"          // stdin / file input not read
	UnknownInputTooLarge      UnknownReason = "input-too-large"            // spool/size limit exceeded
	UnknownMalformed          UnknownReason = "malformed-request"          // structurally invalid but not a parse error
	UnknownInteractive        UnknownReason = "interactive"                // effect chosen later interactively (rebase -i todo, add -p)
	UnknownRemoteState        UnknownReason = "remote-state"               // depends on remote refs not resolved locally
	UnknownUnverified         UnknownReason = "unverified-version"         // real git differs from the pinned registry
	UnknownInvalidCombination UnknownReason = "invalid-option-combination" // options Git rejects together
)

// FamilyUnknown is the family of operations that could not be classified.
const FamilyUnknown = "unknown"

// Scope is a Git data area.
type Scope string

const (
	ScopeLocalRef      Scope = "local-ref"
	ScopeRemoteRef     Scope = "remote-ref"
	ScopeIndex         Scope = "index"
	ScopeWorktree      Scope = "worktree"
	ScopeConfig        Scope = "config"
	ScopeObjects       Scope = "objects"
	ScopeStash         Scope = "stash"
	ScopeReflog        Scope = "reflog"
	ScopeRemoteConfig  Scope = "remote-config"
	ScopeSubmodule     Scope = "submodule"
	ScopeWorktreeAdmin Scope = "worktree-admin"
	ScopeHooks         Scope = "hooks"
	ScopeFileOutput    Scope = "file-output"
)

// Capability is a side channel beyond Git data.
type Capability string

const (
	CapabilityExternalExec Capability = "external-exec" // external diff, textconv, hooks, exec, editor-less scripts
	CapabilityEditor       Capability = "editor"
	CapabilityNetwork      Capability = "network"
	CapabilityFileWrite    Capability = "file-write" // explicit output file (--output, format-patch -o)
	CapabilityIndexRefresh Capability = "index-refresh"
	CapabilityInteractive  Capability = "interactive"
	CapabilityPager        Capability = "pager"
)

// SetKind says how complete a TargetSet is. Unknown targets are never an
// empty Values list: they are Kind "unknown".
type SetKind string

const (
	SetExact   SetKind = "exact"   // exactly one value
	SetSet     SetKind = "set"     // a finite list of values
	SetPattern SetKind = "pattern" // Values are Git patterns (refspec glob, pathspec magic)
	SetUnknown SetKind = "unknown" // could not be determined (see Operation.Unresolved)
	SetNone    SetKind = "none"    // the operation provably has no such target
)

// TargetSet is a possibly-incomplete set of refs or paths.
type TargetSet struct {
	Kind     SetKind  `json:"kind" enum:"exact,set,pattern,unknown,none" doc:"Shape of the set; unknown never means empty."`
	Values   []string `json:"values,omitempty"`
	Complete bool     `json:"complete" doc:"True when Values lists every affected item."`
}

// Exact returns an exact single-value set.
func Exact(v string) *TargetSet {
	return &TargetSet{Kind: SetExact, Values: []string{v}, Complete: true}
}

// UnknownSet returns an unknown set.
func UnknownSet() *TargetSet { return &TargetSet{Kind: SetUnknown} }

// Target is what one operation acts on. Multi-remote requests (remote groups,
// push to several URLs) are split into one Operation per remote; conditions
// in a matcher always apply to a single Operation's Target.
type Target struct {
	RepoRoot  string `json:"repoRoot,omitempty" doc:"Top-level worktree directory; empty for bare or unknown."`
	GitDir    string `json:"gitDir,omitempty"`
	CommonDir string `json:"commonDir,omitempty"`
	Worktree  string `json:"worktree,omitempty"`
	Remote    string `json:"remote,omitempty" doc:"Remote name, or the URL given in place of a name."`
	RemoteURL string `json:"remoteUrl,omitempty" doc:"Effective (rewritten) URL, credentials removed."`
	PushURL   string `json:"pushUrl,omitempty"`
	// Refs are full ref names (refs/heads/main) after local resolution where
	// possible; for remote-ref scopes they are destination refs on the remote.
	Refs *TargetSet `json:"refs,omitempty"`
	// Paths are repository-relative paths or pathspecs.
	Paths *TargetSet `json:"paths,omitempty"`
	// Revisions are revision expressions as given (HEAD~1, A..B).
	Revisions *TargetSet `json:"revisions,omitempty"`
	// Config keys for config.* operations.
	ConfigKeys *TargetSet `json:"configKeys,omitempty"`
}

// Unresolved records a target/parameter field whose value is not known.
type Unresolved struct {
	Field  string `json:"field"`            // e.g. "target.repository", "parameters.method", "variables.id"
	Reason string `json:"reason"`           // human readable
	Source string `json:"source,omitempty"` // where it would come from: "git", "api", "variable", "stdin", ...
}

// Origin links an operation to its syntactic origin.
type Origin struct {
	Command []string `json:"command,omitempty"` // canonical command path, e.g. ["push"], ["stash","drop"]
	Mode    string   `json:"mode,omitempty"`    // parsed command mode, e.g. "hard", "delete", "--continue"
	Source  string   `json:"source,omitempty"`  // upstream entrypoint from source-map.json, e.g. "builtin/push.c:cmd_push"
}

// Source is a redaction-safe description of the original invocation.
type Source struct {
	Kind  string   `json:"kind" enum:"command,unknown" doc:"Syntax source kind."`
	Argv  []string `json:"argv,omitempty"`  // redacted analysis argv (after alias expansion if any)
	Alias string   `json:"alias,omitempty"` // alias name if expanded
	// Global are the parsed git root options (-C, -c, --git-dir, ...), redacted.
	Global []string `json:"global,omitempty"`
}

// NewUnknown is a helper that builds a single-unknown-operation Result.
func NewUnknown(level Level, reason UnknownReason, detail string) Result {
	return Result{
		SchemaVersion:     SchemaVersion,
		NormalizerVersion: NormalizerVersion,
		Classification:    Classification{Level: level, Confidence: ConfidenceNone, Resolution: ResolutionUnresolved, Coverage: CoverageOpaque},
		Operations: []Operation{{
			Family:        FamilyUnknown,
			Action:        FamilyUnknown,
			Effect:        EffectUnknown,
			Coverage:      CoverageOpaque,
			Execution:     ExecutionWillRun,
			UnknownReason: reason,
			Detail:        detail,
		}},
	}
}

// IntPtr returns a pointer to n.
func IntPtr(n int) *int { return &n }
