package git

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/registry"
)

func init() {
	for command := range adminConservativeCommands {
		fallbackSemantics[command] = adminConservative
	}
	for command := range adminOpaqueCommands {
		fallbackSemantics[command] = adminOpaque
	}
	register("worktree", worktreeAdmin)
	register("submodule", submoduleAdmin)
	register("submodule--helper", submoduleAdmin)
	register("init", repositoryInit)
	register("init-db", repositoryInit)
	register("hook", hookAdmin)
	installSubmoduleGrammar()
}

func adminOperation(c *Context, family, action string, effect model.Effect, scopes ...model.Scope) model.Operation {
	op := model.Operation{Family: family, Action: action, Effect: effect, Coverage: model.CoverageSemantic, Scopes: scopes,
		Origin: model.Origin{Command: append([]string(nil), c.Parsed.Path...)}}
	entry := commands[c.Parsed.Path[0]]
	source, fn := entry.Source.File, entry.Entrypoint
	grammar := entry.Grammar
	for _, name := range c.Parsed.Path[1:] {
		for _, sub := range grammar.Subcommands {
			if sub.Name == name {
				source, fn, grammar = sub.Source.File, sub.Entrypoint, sub.Grammar
				break
			}
		}
	}
	if source != "" {
		op.Origin.Source = source
		if fn != "" {
			op.Origin.Source += ":" + fn
		}
	}
	return op
}

func adminUnresolved(op *model.Operation, field, reason, source string) {
	op.Unresolved = append(op.Unresolved, model.Unresolved{Field: field, Reason: reason, Source: source})
}

func adminSet(values []string, pattern bool) *model.TargetSet {
	if len(values) == 0 {
		return model.UnknownSet()
	}
	kind := model.SetExact
	if len(values) > 1 {
		kind = model.SetSet
	}
	if pattern {
		kind = model.SetPattern
	}
	return &model.TargetSet{Kind: kind, Values: append([]string(nil), values...), Complete: true}
}

func adminInvalid(c *Context, detail string) model.Operation {
	op := adminOperation(c, model.FamilyUnknown, "unknown", model.EffectUnknown)
	op.Coverage, op.UnknownReason, op.Detail = model.CoverageConservative, model.UnknownParseError, detail
	return op
}

func adminFileOutput(op *model.Operation, path string) {
	previous := slices.Contains(op.Scopes, model.ScopeFileOutput)
	op.Effect = model.EffectWrite
	if !previous {
		op.Scopes = append(op.Scopes, model.ScopeFileOutput)
		op.Capabilities = append(op.Capabilities, model.CapabilityFileWrite)
	}
	if op.Parameters == nil {
		op.Parameters = map[string]any{}
	}
	op.Parameters["output"] = path
	if path == "" {
		op.Target.Paths = model.UnknownSet()
		adminUnresolved(op, "target.paths", "output file names depend on repository data or configuration", "git")
	} else if !previous {
		op.Target.Paths = model.Exact(path)
	} else if op.Target.Paths != nil && op.Target.Paths.Kind != model.SetUnknown {
		complete := op.Target.Paths.Complete
		values := append([]string(nil), op.Target.Paths.Values...)
		if !slices.Contains(values, path) {
			values = append(values, path)
		}
		op.Target.Paths = adminSet(values, false)
		op.Target.Paths.Complete = complete
	}
}

func adminExternal(op *model.Operation, field, reason string) {
	for _, cap := range op.Capabilities {
		if cap == model.CapabilityExternalExec {
			return
		}
	}
	op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
	if reason != "" {
		adminUnresolved(op, field, reason, "git")
	}
}

func adminOpaqueOperation(c *Context, reason model.UnknownReason, detail string) model.Operation {
	op := adminOperation(c, model.FamilyUnknown, "unknown", model.EffectUnknown,
		model.ScopeLocalRef, model.ScopeRemoteRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig,
		model.ScopeObjects, model.ScopeReflog, model.ScopeStash, model.ScopeRemoteConfig, model.ScopeSubmodule, model.ScopeWorktreeAdmin, model.ScopeHooks, model.ScopeFileOutput)
	op.Target = model.Target{Refs: model.UnknownSet(), Paths: model.UnknownSet(), ConfigKeys: model.UnknownSet()}
	op.Coverage, op.UnknownReason, op.Detail = model.CoverageOpaque, reason, detail
	return op
}

func adminOpaque(c *Context) []model.Operation {
	name := c.Parsed.Path[0]
	reason := model.UnknownExternal
	if strings.HasPrefix(name, "credential") {
		reason = model.UnknownInputUnavailable
	}
	op := adminOpaqueOperation(c, reason, adminOpaqueCommands[name])
	if reason == model.UnknownInputUnavailable {
		op.Capabilities = []model.Capability{model.CapabilityExternalExec, model.CapabilityInteractive}
		adminUnresolved(&op, "input", "credential helper protocol and configured helpers are not analyzed", "stdin")
	} else {
		op.Capabilities = []model.Capability{model.CapabilityExternalExec}
	}
	return []model.Operation{op}
}

func adminConservative(c *Context) []model.Operation {
	op := adminOpaqueOperation(c, model.UnknownCommand, adminConservativeCommands[c.Parsed.Path[0]])
	op.Coverage = model.CoverageConservative
	if inputProtocol(c) {
		op.Coverage, op.UnknownReason = model.CoverageOpaque, model.UnknownInputUnavailable
		adminUnresolved(&op, "input", "stdin protocol has not been captured", "stdin")
	}
	return []model.Operation{op}
}

func worktreeAdmin(c *Context) []model.Operation {
	if len(c.Parsed.Path) < 2 {
		return []model.Operation{adminInvalid(c, "worktree requires a subcommand")}
	}
	p, args, mode := c.Parsed, c.Parsed.Values(), c.Parsed.Path[1]
	effect := model.EffectWrite
	if mode == "list" || mode == "prune" && p.Has("dry-run") {
		effect = model.EffectRead
	}
	op := adminOperation(c, "worktree.admin", "worktree.admin."+mode, effect, model.ScopeWorktreeAdmin)
	op.Origin.Mode = mode
	force := 0
	for _, o := range p.Options {
		if o.Name == "force" {
			if o.Negated {
				force = 0
			} else {
				force++
			}
		}
	}
	op.Parameters = map[string]any{"forceLevel": force, "dryRun": p.Has("dry-run")}
	if mode == "prune" && p.Has("dry-run") {
		op.Action += ".preview"
	}
	if len(args) > 0 {
		op.Target.Worktree = args[0]
		op.Target.Paths = adminSet(args, false)
	} else {
		op.Target.Paths = model.UnknownSet()
		adminUnresolved(&op, "target.paths", "worktrees are selected from administrative metadata", "git")
	}
	switch mode {
	case "add":
		if len(args) == 0 {
			return []model.Operation{adminInvalid(c, "worktree add requires a path")}
		}
		op.Scopes = append(op.Scopes, model.ScopeWorktree, model.ScopeIndex, model.ScopeLocalRef, model.ScopeConfig)
		op.Target.Paths = model.Exact(args[0])
		op.Parameters["checkout"] = !adminDisabled(p, "checkout")
		op.Parameters["detach"], op.Parameters["orphan"] = p.Has("detach"), p.Has("orphan")
		branch := ""
		for _, o := range p.Options {
			if (o.Name == "b" || o.Name == "B") && !o.Negated {
				branch = o.Value
				op.Parameters["resetBranch"] = o.Name == "B"
			}
		}
		if branch != "" {
			op.Target.Refs = model.Exact("refs/heads/" + strings.TrimPrefix(branch, "refs/heads/"))
		} else if !p.Has("detach") && len(args) == 1 {
			op.Target.Refs = model.Exact("refs/heads/" + filepath.Base(filepath.Clean(args[0])))
			adminUnresolved(&op, "parameters.createBranch", "branch existence and worktree.guessRemote require repository data", "git")
		} else {
			op.Target.Refs = model.UnknownSet()
			adminUnresolved(&op, "target.refs", "worktree HEAD and possible branch creation need local ref resolution", "git")
		}
		if len(args) > 1 {
			op.Target.Revisions = model.Exact(args[1])
		} else {
			op.Target.Revisions = model.Exact("HEAD")
		}
		if !adminDisabled(p, "checkout") {
			adminExternal(&op, "parameters.filters", "worktree checkout may run configured content filters")
		}
	case "move", "remove":
		op.Scopes = append(op.Scopes, model.ScopeWorktree)
	}
	return []model.Operation{op}
}

func adminDisabled(p ParsedCommand, name string) bool {
	o, ok := p.Option(name)
	return ok && o.Negated
}

func repositoryInit(c *Context) []model.Operation {
	op := adminOperation(c, "repository", "repository.init", model.EffectWrite,
		model.ScopeWorktreeAdmin, model.ScopeConfig, model.ScopeObjects, model.ScopeLocalRef)
	args := c.Parsed.Values()
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	op.Target.Paths, op.Target.Worktree = model.Exact(path), path
	bare := c.Global.Bare || c.Parsed.Has("bare")
	op.Parameters = map[string]any{"bare": bare}
	if bare {
		op.Target.Worktree, op.Target.GitDir = "", path
	}
	for _, name := range []string{"template", "separate-git-dir", "initial-branch", "object-format", "ref-format"} {
		if o, ok := c.Parsed.Option(name); ok && !o.Negated {
			key := map[string]string{"separate-git-dir": "separateGitDir", "initial-branch": "initialBranch", "object-format": "objectFormat", "ref-format": "refFormat", "template": "template"}[name]
			op.Parameters[key] = o.Value
		}
	}
	if o, ok := c.Parsed.Option("initial-branch"); ok && !o.Negated {
		op.Target.Refs = model.Exact("refs/heads/" + o.Value)
	} else {
		op.Target.Refs = model.UnknownSet()
		adminUnresolved(&op, "target.refs", "initial branch name depends on init.defaultBranch", "git")
	}
	return []model.Operation{op}
}

func hookAdmin(c *Context) []model.Operation {
	if len(c.Parsed.Path) < 2 {
		return []model.Operation{adminInvalid(c, "hook requires a subcommand")}
	}
	mode := c.Parsed.Path[1]
	if mode == "list" {
		return []model.Operation{adminOperation(c, "hook", "hook.list", model.EffectRead, model.ScopeHooks)}
	}
	op := adminOpaqueOperation(c, model.UnknownExternal, "explicit hook execution may run arbitrary code")
	op.Family, op.Action = "hook", "hook.run"
	op.Capabilities = []model.Capability{model.CapabilityExternalExec}
	op.Parameters = map[string]any{"arguments": c.Parsed.Values()}
	return []model.Operation{op}
}

func installSubmoduleGrammar() {
	entry := commands["submodule"]
	entry.Grammar = registry.Grammar{Parser: "parse-options", ParseFlags: []string{"PARSE_OPT_SUBCOMMAND_OPTIONAL"},
		Options: []registry.Option{{Long: "quiet", Short: "q", Arity: "none"}, {Long: "cached", Arity: "none"}}}
	for _, sub := range commands["submodule--helper"].Subcommands {
		switch sub.Name {
		case "add", "init", "deinit", "update", "foreach", "sync", "set-url", "set-branch", "absorbgitdirs", "status", "summary":
			sub.ParseFlags = []string{"PARSE_OPT_STOP_AT_NON_OPTION"}
			entry.Subcommands = append(entry.Subcommands, sub)
		}
	}
	commands["submodule"] = entry
}

func submoduleAdmin(c *Context) []model.Operation {
	p, args, mode := c.Parsed, c.Parsed.Values(), "status"
	if len(p.Path) > 1 {
		mode = p.Path[1]
	}
	effect := model.EffectWrite
	if mode == "status" || mode == "summary" || mode == "gitdir" || mode == "get-default-remote" {
		effect = model.EffectRead
	}
	op := adminOperation(c, "submodule", "submodule."+mode, effect, model.ScopeSubmodule)
	if p.Path[0] == "submodule" {
		op.Origin.Source = "git-submodule.sh:cmd_" + strings.ReplaceAll(mode, "-", "_")
	}
	op.Origin.Mode = mode
	op.Target.Paths = adminSet(args, true)
	if len(args) == 0 {
		adminUnresolved(&op, "target.paths", "active submodule paths require repository configuration", "git")
	}
	op.Parameters = map[string]any{"recursive": p.Has("recursive") || mode == "absorbgitdirs", "remote": p.Has("remote"), "force": p.Has("force"), "init": p.Has("init")}
	switch mode {
	case "status", "gitdir", "get-default-remote":
	case "summary":
		if len(p.AfterDashDash) > 0 {
			var paths []string
			for _, arg := range p.AfterDashDash {
				paths = append(paths, arg.Value)
			}
			op.Target.Paths = adminSet(paths, true)
			if len(args) > len(paths) {
				op.Target.Revisions = model.Exact(args[0])
			}
		} else if len(args) > 0 {
			op.Target.Paths = model.UnknownSet()
			adminUnresolved(&op, "target.paths", "summary operands may name a commit or submodule paths", "git")
		}
	case "add", "clone":
		op.Scopes = append(op.Scopes, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig, model.ScopeObjects, model.ScopeLocalRef)
		op.Capabilities = []model.Capability{model.CapabilityNetwork}
		if len(args) > 0 {
			op.Target.Remote = args[0]
		}
		if len(args) > 1 {
			op.Target.Paths = model.Exact(args[1])
		} else {
			op.Target.Paths = model.UnknownSet()
			adminUnresolved(&op, "target.paths", "submodule path is inferred from the repository URL", "git")
		}
		adminExternal(&op, "parameters.filters", "submodule checkout may run configured content filters")
	case "init", "sync":
		op.Scopes = append(op.Scopes, model.ScopeConfig)
		if mode == "sync" {
			op.Scopes = append(op.Scopes, model.ScopeRemoteConfig)
		}
	case "deinit", "absorbgitdirs":
		op.Scopes = append(op.Scopes, model.ScopeConfig, model.ScopeWorktree, model.ScopeWorktreeAdmin)
	case "set-url", "set-branch":
		op.Scopes = append(op.Scopes, model.ScopeConfig, model.ScopeWorktree)
		if mode == "set-url" {
			op.Scopes = append(op.Scopes, model.ScopeRemoteConfig)
		}
		if len(args) > 0 {
			op.Target.Paths = model.Exact(args[0])
		}
		if mode == "set-url" && len(args) > 1 {
			op.Target.RemoteURL = args[1]
		}
		if o, ok := p.Option("branch"); ok && !o.Negated {
			op.Parameters["branch"] = o.Value
		}
		op.Parameters["defaultBranch"] = p.Has("default")
	case "update":
		op.Scopes = append(op.Scopes, model.ScopeWorktree, model.ScopeIndex, model.ScopeLocalRef, model.ScopeObjects)
		if p.Has("init") {
			op.Scopes = append(op.Scopes, model.ScopeConfig)
		}
		// --no-fetch suppresses fetching, but missing submodules can still be cloned.
		op.Capabilities = []model.Capability{model.CapabilityNetwork}
		strategy := "configured"
		for _, o := range p.Options {
			if !o.Negated && (o.Name == "checkout" || o.Name == "merge" || o.Name == "rebase") {
				strategy = o.Name
			}
		}
		op.Parameters["strategy"], op.Parameters["noFetch"] = strategy, p.Has("no-fetch")
		if strategy == "configured" {
			adminExternal(&op, "parameters.updateCommand", "submodule.<name>.update may select !command; repository config is not provided")
		}
		adminExternal(&op, "parameters.filters", "submodule checkout may run configured content filters")
	case "foreach":
		op = adminOpaqueOperation(c, model.UnknownExternal, "submodule foreach executes an arbitrary shell command")
		op.Family, op.Action = "submodule", "submodule.foreach"
		op.Capabilities = []model.Capability{model.CapabilityExternalExec}
		op.Parameters = map[string]any{"recursive": p.Has("recursive"), "command": args}
		op.Target.Paths = model.UnknownSet()
	default:
		op = adminOpaqueOperation(c, model.UnknownCommand, "internal submodule helper mode is not modeled")
		op.Coverage = model.CoverageConservative
	}
	return []model.Operation{op}
}

// Explicit inventory: adding a pinned command must add semantics or a reviewed reason.
// Other workers' adapters override these conservative registrations at init time.
var adminConservativeCommands = map[string]string{
	"stage":                  "index staging alias modes are provided by the worktree adapter",
	"checkout--worker":       "internal checkout worker consumes an unmodeled worker protocol",
	"checkout-index":         "index checkout modes are provided by the worktree adapter",
	"history":                "history rewrite subcommands are not modeled",
	"http-fetch":             "low-level HTTP fetch transport is not modeled",
	"http-push":              "low-level HTTP push transport is not modeled",
	"imap-send":              "mail submission transport and configuration are not modeled",
	"interpret-trailers":     "trailer commands and in-place file edits are not modeled",
	"mailinfo":               "mail message and patch output file operands are not modeled",
	"mailsplit":              "mailbox splitting writes output files whose names depend on input",
	"merge-file":             "in-place merge-file output and conflict modes are not modeled",
	"merge-index":            "merge-index invokes an explicit merge program",
	"merge-ours":             "internal merge strategy index checks are not modeled",
	"merge-recursive":        "internal merge strategy index/worktree/objects writes are not modeled",
	"merge-recursive-ours":   "internal merge strategy index/worktree/objects writes are not modeled",
	"merge-recursive-theirs": "internal merge strategy index/worktree/objects writes are not modeled",
	"merge-subtree":          "internal merge strategy index/worktree/objects writes are not modeled",
	"merge-tree":             "merge-tree can write objects; legacy read mode is not modeled",
	"refs":                   "ref database management subcommands are not modeled",
	"replay":                 "revision replay may rewrite refs and objects; modes are not modeled",
	"rerere":                 "recorded conflict resolutions and index updates are not modeled",
	"scalar":                 "repository orchestration can configure maintenance and fetch; modes are not modeled",
	"unpack-file":            "temporary file output path depends on object contents and filesystem",
	"update-server-info":     "server metadata and pack-list file generation are not modeled",
	"backfill":               "partial-clone object fetching and remote selection are not modeled",
	"bugreport":              "diagnostic report and optional archive output paths are not modeled",
	"diagnose":               "diagnostic archive output and collected files are not modeled",
	"fsmonitor--daemon":      "filesystem monitor daemon lifecycle and socket writes are not modeled",
}

var adminOpaqueCommands = map[string]string{
	"credential":               "interactive credential protocol can invoke configured credential helpers",
	"credential-cache":         "credential cache protocol and daemon state are not analyzed",
	"credential-cache--daemon": "credential daemon accepts an interactive socket protocol",
	"credential-store":         "credential storage protocol and persisted secrets are not analyzed",
	"daemon":                   "server requests select repository effects after startup",
	"instaweb":                 "web server lifecycle invokes configured server commands",
	"http-backend":             "HTTP server requests select read or write operations at runtime",
	"filter-branch":            "history rewrite filters execute arbitrary shell programs",
	"for-each-repo":            "executes a user-supplied Git command for configured repositories",
	"difftool":                 "executes an external diff tool selected by arguments or configuration",
	"difftool--helper":         "internal helper executes a configured external diff tool",
	"mergetool":                "executes a configured external merge tool",
	"mergetool--lib":           "shell merge tool library is not a standalone semantic operation",
	"merge-octopus":            "shell strategy may invoke custom merge programs",
	"merge-one-file":           "shell strategy invokes configured merge programs",
	"merge-resolve":            "shell strategy invokes configured merge programs",
	"archimport":               "external archive import tool and remote protocol are not analyzed",
	"cvsexportcommit":          "external CVS export commands and credentials are not analyzed",
	"cvsimport":                "external CVS import commands and credentials are not analyzed",
	"cvsserver":                "CVS server requests determine effects after startup",
	"citool":                   "interactive graphical commit tool chooses future effects",
	"gitk":                     "interactive graphical history tool can invoke user commands",
	"gui":                      "interactive graphical tool chooses future repository effects",
	"gitweb":                   "web application requests choose operations at runtime",
	"p4":                       "external Perforce bridge executes configured commands",
	"quiltimport":              "external patch import script can open an editor and rewrite history",
	"request-pull":             "shell request generator queries remote state",
	"send-email":               "mail submission invokes configured SMTP or sendmail transport",
	"sh-i18n":                  "shell support library is not a standalone semantic operation",
	"sh-i18n--envsubst":        "shell substitution helper has no repository semantic adapter",
	"sh-setup":                 "shell support library is not a standalone semantic operation",
	"shell":                    "restricted shell chooses commands from subsequent input",
	"svn":                      "external Subversion bridge and configured commands are not analyzed",
	"web--browse":              "executes a configured browser command",
	"remote-ext":               "remote helper invokes an arbitrary transport command",
	"remote-fd":                "remote helper consumes an interactive transport protocol",
	"remote-ftp":               "remote helper consumes an interactive transport protocol",
	"remote-ftps":              "remote helper consumes an interactive transport protocol",
	"remote-http":              "remote helper consumes an interactive transport protocol",
	"remote-https":             "remote helper consumes an interactive transport protocol",
	"upload-archive":           "archive server consumes requests after startup",
	"upload-archive--writer":   "internal archive server consumes requests after startup",
}
