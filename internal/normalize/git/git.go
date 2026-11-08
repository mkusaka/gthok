// Package git classifies Git invocations without executing Git or reading input.
package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
	"github.com/mkusaka/gthok/internal/registry"
)

var (
	commands        = registry.Commands()
	manifest        = registry.Manifest()
	revisionOptions = registry.RevisionOptions()
	diffOptions     = registry.DiffOptions()
	semantics       = map[string]func(*Context) []model.Operation{}
)

// Context is the syntax/analysis contract for semantics_<family>.go adapters.
// Invocation.Argv remains the original execution argv; Argv is expanded analysis
// argv. Parsed spans always refer to Argv, never to Invocation.Argv.
type Context struct {
	Invocation model.Invocation
	Argv       []string
	Global     gitargs.Global
	Parsed     ParsedCommand
}

// register is called at init time by individual semantic families. The most
// specific registered command path wins ("stash drop" before "stash").
// fallbackSemantics holds explicit conservative/opaque entries for commands
// without a dedicated classifier; register() always takes precedence.
var fallbackSemantics = map[string]func(*Context) []model.Operation{}

func register(command string, fn func(*Context) []model.Operation) {
	if semantics[command] != nil {
		panic("git: duplicate semantics registration for " + command)
	}
	semantics[command] = fn
}

func registryInfo() model.Registry {
	return model.Registry{Git: manifest.Tag, Commit: manifest.Commit}
}

func Normalize(inv model.Invocation) model.Result {
	expanded, alias, reason := expandAlias(inv.Argv, inv.Aliases, inv.Env)
	g, err := gitargs.Parse(expanded)
	command := g.Command
	if command == "" {
		command = g.Early
	}
	level := model.LevelL1
	if command == "" {
		level = model.LevelNone
	}
	r := model.Result{
		SchemaVersion: model.SchemaVersion, NormalizerVersion: model.NormalizerVersion,
		Registry: registryInfo(), Source: model.Source{Kind: "command", Argv: expanded, Alias: alias, Global: g.Tokens},
	}
	unknown := func(why model.UnknownReason, detail string, opaque bool) model.Result {
		coverage := model.CoverageConservative
		if opaque {
			coverage = model.CoverageOpaque
		}
		r.Operations = []model.Operation{{Family: model.FamilyUnknown, Action: model.FamilyUnknown,
			Effect: model.EffectUnknown, Coverage: coverage, UnknownReason: why, Detail: detail,
			Execution: model.ExecutionWillRun, Origin: model.Origin{Command: []string{command}}}}
		if err != nil {
			r.Errors = []string{err.Error()}
		}
		r.Classification = aggregate(level, model.ConfidenceNone, r.Operations)
		return r
	}
	if reason != model.UnknownNone {
		return unknown(reason, "alias could not be expanded safely", reason == model.UnknownShellAlias)
	}
	if err != nil {
		return unknown(model.UnknownParseError, err.Error(), false)
	}
	if command == "" {
		return unknown(model.UnknownCommand, "no command supplied", false)
	}
	if g.Early != "" && g.Early != "help" && g.Early != "version" {
		r.Operations = []model.Operation{{Family: "git.info", Action: "git.info.read", Effect: model.EffectRead, Coverage: model.CoverageSemantic,
			Execution: model.ExecutionWillRun, Origin: model.Origin{Command: []string{g.Early}}}}
		r.Classification = aggregate(level, model.ConfidenceExact, r.Operations)
		return r
	}
	entry, known := commands[command]
	if !known {
		if strings.HasPrefix(command, "git-") {
			return unknown(model.UnknownExternal, "external Git executable", true)
		}
		return unknown(model.UnknownCommand, "command is not in the pinned Git registry", false)
	}
	if entry.Implementation == "document" {
		return unknown(model.UnknownCommand, "registry entry is documentation, not a command", false)
	}
	parsed, parseErr := parseCommand(g)
	if parseErr != nil {
		err = parseErr
		why := model.UnknownParseError
		if e, ok := parseErr.(*syntaxError); ok {
			why = e.reason
		}
		return unknown(why, parseErr.Error(), false)
	}
	if len(parsed.Unknown) > 0 {
		return unknown(model.UnknownFlag, "option is not covered by the command grammar: "+parsed.Unknown[0].Value, false)
	}
	ctx := &Context{Invocation: inv, Argv: expanded, Global: g, Parsed: parsed}
	if parsed.Help {
		r.Operations = readInfo(ctx)
	} else {
		for n := len(parsed.Path); n > 0; n-- {
			key := strings.Join(parsed.Path[:n], " ")
			fn := semantics[key]
			if fn == nil {
				fn = fallbackSemantics[key]
			}
			if fn != nil {
				r.Operations = fn(ctx)
				break
			}
		}
	}
	if len(r.Operations) == 0 {
		r.Operations = []model.Operation{defaultOperation(ctx)}
	}
	for i := range r.Operations {
		op := &r.Operations[i]
		if op.Execution == "" {
			op.Execution = model.ExecutionWillRun
		}
		if op.Coverage == "" {
			op.Coverage = model.CoverageConservative
		}
		if len(op.Origin.Command) == 0 {
			op.Origin.Command = append([]string(nil), parsed.Path...)
		}
		if op.Origin.Source == "" && entry.Source.File != "" {
			op.Origin.Source = entry.Source.File + ":" + entry.Entrypoint
		}
	}
	r.Classification = aggregate(level, model.ConfidenceExact, r.Operations)
	return r
}

func IsBuiltinCommand(name string) bool { return commands[name].Builtin }

func defaultOperation(c *Context) model.Operation {
	command := c.Parsed.Path[0]
	op := model.Operation{Family: "git." + command, Action: "git." + command,
		Effect: model.EffectUnknown, Coverage: model.CoverageConservative, UnknownReason: model.UnknownCommand,
		Detail: "command semantics are not installed", Target: model.Target{Refs: model.UnknownSet(), Paths: model.UnknownSet()}}
	// These commands only inspect local data, but conservative coverage is not
	// proof of retry safety (configuration may add external execution).
	switch command {
	case "log", "show", "shortlog", "describe", "ls-files", "ls-tree", "show-ref", "rev-list", "merge-base", "name-rev", "count-objects", "check-ref-format", "check-ignore", "check-attr", "check-mailmap", "for-each-ref", "cat-file", "rev-parse":
		op.Effect, op.UnknownReason = model.EffectRead, model.UnknownNone
		op.Unresolved = append(op.Unresolved, model.Unresolved{Field: "target", Reason: "command-specific target resolution is not installed", Source: "git"})
	}
	if inputProtocol(c) {
		op.Effect, op.UnknownReason, op.Coverage = model.EffectUnknown, model.UnknownInputUnavailable, model.CoverageOpaque
		op.Unresolved = append(op.Unresolved, model.Unresolved{Field: "input", Reason: "stdin protocol was not captured", Source: "stdin"})
	}
	return op
}

func aggregate(level model.Level, confidence model.Confidence, ops []model.Operation) model.Classification {
	c := model.Classification{Level: level, Confidence: confidence, Resolution: model.ResolutionComplete,
		Coverage: model.CoverageSemantic, MutationFree: len(ops) > 0, RetrySafe: len(ops) > 0}
	for _, op := range ops {
		if op.Coverage == model.CoverageOpaque || (op.Coverage == model.CoverageConservative && c.Coverage == model.CoverageSemantic) {
			c.Coverage = op.Coverage
		}
		safe := op.Effect == model.EffectRead && op.Coverage == model.CoverageSemantic
		retry := safe
		for _, capability := range op.Capabilities {
			switch capability {
			case model.CapabilityIndexRefresh, model.CapabilityFileWrite, model.CapabilityExternalExec, model.CapabilityEditor:
				safe, retry = false, false
			case model.CapabilityNetwork, model.CapabilityInteractive:
				// No local Git data changes, but running twice is observable.
				retry = false
			}
		}
		c.MutationFree = c.MutationFree && safe
		c.RetrySafe = c.RetrySafe && retry
		if op.Effect == model.EffectUnknown || op.UnknownReason != model.UnknownNone {
			c.Resolution = model.ResolutionUnresolved
		} else if c.Resolution != model.ResolutionUnresolved && len(op.Unresolved) > 0 {
			resolution := model.ResolutionDeferred
			for _, u := range op.Unresolved {
				if u.Source != "git" {
					resolution = model.ResolutionPartial
				}
			}
			if c.Resolution != model.ResolutionPartial {
				c.Resolution = resolution
			}
		}
	}
	return c
}

// ActionRoutes lists illustrative argv (without the leading git). Input-protocol
// actions share a route; the input document, not argv, selects their action.
func ActionRoutes() map[string][]string {
	routes := map[string][]string{
		"status.show": {"status"}, "git.help.read": {"help", "--help"}, "git.version.read": {"version", "--version"},
		"git.info.read":  {"--exec-path", "--html-path", "--man-path", "--info-path", "--list-cmds=main"},
		"unknown":        {"custom-command"},
		"worktree.clean": {"clean -f"}, "worktree.clean.preview": {"clean -n"},
		"ref.create": {"branch topic", "branch -c old new", "tag v1", "checkout -b topic", "switch -c topic", "worktree add -b topic ../topic"},
		"ref.update": {"update-ref refs/heads/topic HEAD", "branch -f topic", "tag -f v1", "symbolic-ref HEAD refs/heads/main", "checkout -B topic", "switch -C topic", "worktree add -B topic ../topic"},
		"ref.delete": {"update-ref -d refs/heads/topic", "branch -D topic", "tag -d v1", "symbolic-ref -d refs/heads/alias"},
		"ref.rename": {"branch -m old new"},
		"ref.verify": {"update-ref --stdin"}, "ref.pack": {"pack-refs --all"}, "ref.list": {"show-ref", "for-each-ref"},
		"remote.push.update": {"push origin HEAD:refs/heads/main", "send-pack origin HEAD:refs/heads/main"},
		"remote.push.delete": {"push --delete origin refs/heads/topic"}, "remote.push.mirror": {"push --mirror origin"},
		"remote.push.matching": {"push origin :"}, "remote.push.tags": {"push --tags origin"},
		"remote.fetch": {"fetch origin", "remote update"}, "remote.pull": {"pull origin main"},
		"remote.prune": {"remote prune origin"}, "remote.clone": {"clone ../source destination"},
		"remote.list": {"remote"}, "remote.show": {"remote show -n origin"},
		"remote.get-url": {"remote get-url origin", "ls-remote --get-url origin"}, "remote.ls-remote": {"ls-remote origin"},
		"remote.pack.fetch": {"fetch-pack ../source refs/heads/main"}, "remote.pack.receive": {"receive-pack repo.git"},
		"remote.pack.upload": {"upload-pack repo.git"}, "remote.pack.advertise": {"receive-pack --advertise-refs repo.git"},
		"index.read-tree": {"read-tree HEAD"}, "index.read-tree.preview": {"read-tree -n HEAD"}, "index.update": {"update-index --refresh"},
		"repository.init": {"init repository"}, "hook.list": {"hook list"}, "hook.run": {"hook run pre-commit"},
		"archive.create": {"archive HEAD"}, "archive.list": {"archive --list"}, "archive.output": {"archive --output=out.tar HEAD"},
		"bundle.stdout":     {"bundle create - HEAD"},
		"objects.hash.read": {"hash-object --no-filters file"}, "objects.hash.write": {"hash-object -w file"},
		"objects.fsck.lost-found": {"fsck --lost-found"}, "objects.pack-objects.read": {"pack-objects --stdout"},
		"objects.fast-export.output": {"fast-export --export-marks=marks HEAD"}, "objects.index-pack.verify": {"index-pack --verify data.pack"},
		"submodule.clone": {"submodule--helper clone ../source lib"}, "submodule.gitdir": {"submodule--helper gitdir lib"},
		"submodule.get-default-remote": {"submodule--helper get-default-remote"},
	}
	for _, spec := range actions.All() {
		if len(routes[spec.Action]) > 0 {
			continue
		}
		mode := strings.TrimPrefix(spec.Action, spec.Family+".")
		var route string
		switch spec.Family {
		case "git.cat-file":
			if mode == "protocol" {
				route = "cat-file --batch"
			}
			fallthrough
		case "git.log", "git.show", "git.diff", "git.diff-tree", "git.diff-index", "git.diff-files", "git.diff-pairs", "git.rev-list", "git.rev-parse", "git.ls-files", "git.ls-tree", "git.grep", "git.blame", "git.annotate", "git.pickaxe", "git.shortlog", "git.describe", "git.name-rev", "git.format-rev", "git.merge-base", "git.show-branch", "git.whatchanged", "git.range-diff", "git.format-patch", "git.last-modified", "git.cherry", "git.check-attr", "git.check-ignore", "git.check-mailmap", "git.check-ref-format", "git.var", "git.repo", "git.column", "git.stripspace", "git.patch-id", "git.show-index", "git.get-tar-commit-id", "git.fmt-merge-msg", "git.pack-redundant", "git.url-parse", "git.verify-commit", "git.verify-tag":
			if route == "" {
				route = strings.TrimPrefix(spec.Family, "git.")
				if mode == "output" {
					route += " --output=report"
				} else if spec.Family == "git.format-patch" {
					route += " --stdout"
				}
			}
		case "ref.transaction":
			route = "update-ref --stdin"
		case "ref.symbolic":
			route = "symbolic-ref HEAD"
			switch mode {
			case "verify":
				route = "update-ref --stdin --no-deref"
			}
		case "ref.branch":
			route = map[string]string{"list": "branch --list", "upstream": "branch --set-upstream-to=origin/main topic", "description": "branch --edit-description topic"}[mode]
		case "ref.tag":
			route = map[string]string{"list": "tag --list", "verify": "tag --verify v1"}[mode]
		case "ref.reflog":
			route = "reflog " + mode
		case "ref.replace":
			route = "replace --" + mode
			if mode == "create" {
				route = "replace HEAD HEAD~1"
			} else if mode == "convert" {
				route = "replace --convert-graft-file"
			} else if mode != "list" {
				route += " HEAD"
			}
		case "remote.config":
			route = "remote " + mode + " origin"
			switch mode {
			case "add", "set-url":
				route += " ../source"
			case "rename":
				route += " backup"
			case "set-branches":
				route += " main"
			}
		case "worktree.reset":
			route = "reset --" + mode
			if mode == "paths" {
				route = "reset -- file"
			}
		case "worktree.restore":
			route = "restore -- file"
		case "worktree.checkout", "worktree.switch":
			command := strings.TrimPrefix(spec.Family, "worktree.")
			route = command + " " + map[string]string{"switch": "main", "create": "-b topic", "reset": "-B topic", "orphan": "--orphan topic", "detach": "--detach HEAD", "paths": "-- file"}[mode]
			if command == "switch" {
				route = strings.ReplaceAll(strings.ReplaceAll(route, "-b ", "-c "), "-B ", "-C ")
			}
		case "index.add":
			route = map[string]string{"update": "add file", "refresh": "add --refresh file", "preview": "add -n file"}[mode]
		case "index.rm":
			route = "rm file"
			if mode == "preview" {
				route = "rm -n file"
			}
		case "worktree.mv":
			route = "mv old new"
			if mode == "preview" {
				route = "mv -n old new"
			}
		case "worktree.apply":
			route = "apply patch"
			if mode != "patch" {
				route = "apply --" + mode + " patch"
			}
		case "worktree.sparse-checkout":
			route = "sparse-checkout " + mode
			if mode == "preview" {
				route = "sparse-checkout clean -n"
			}
		case "worktree.admin":
			route = "worktree " + mode
			if mode == "prune.preview" {
				route = "worktree prune -n"
			} else if mode != "list" && mode != "prune" {
				route += " ../topic"
				if mode == "move" {
					route += " ../renamed"
				}
			}
		case "history.commit":
			route = map[string]string{"create": "commit -m message", "amend": "commit --amend -m message", "preview": "commit --dry-run"}[mode]
		case "history.merge", "history.rebase", "history.cherry-pick", "history.revert", "history.am":
			route = strings.TrimPrefix(spec.Family, "history.")
			switch mode {
			case "start":
				route += " HEAD"
			case "apply":
				route += " --no-commit HEAD"
			default:
				route += " --" + mode
			}
		case "history.bisect":
			route = "bisect " + mode
			switch mode {
			case "run":
				route += " sh -c true"
			case "mark":
				route = "bisect custom-term HEAD"
			}
		case "stash", "notes", "config", "maintenance", "submodule", "bundle":
			route = spec.Family + " " + mode
			if spec.Action == "notes.preview" {
				route = "notes prune -n"
			}
			if spec.Family == "bundle" {
				route += " data.bundle"
			}
			if spec.Family == "config" {
				switch mode {
				case "get", "unset":
					route += " core.editor"
				case "set":
					route += " core.editor vi"
				case "add":
					route = "config --add core.editor vi"
				case "rename-section":
					route += " test.old test.new"
				case "remove-section":
					route += " test.old"
				}
			}
		case "objects":
			route = strings.ReplaceAll(mode, ".", " ")
			if strings.HasSuffix(mode, ".preview") {
				route = strings.TrimSuffix(mode, ".preview") + " --dry-run"
				if mode == "unpack-objects.preview" {
					route = "unpack-objects -n"
				}
			}
		}
		if route != "" {
			routes[spec.Action] = []string{route}
		}
	}
	return routes
}
