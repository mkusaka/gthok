// Package git classifies Git invocations without executing Git or reading input.
package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/model"
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
func register(command string, fn func(*Context) []model.Operation) {
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
			if fn := semantics[strings.Join(parsed.Path[:n], " ")]; fn != nil {
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

func ActionRoutes() map[string][]string {
	return map[string][]string{
		"status.show": {"status"}, "git.help.read": {"help", "--help"}, "git.version.read": {"version", "--version"},
		"git.info.read":      {"--exec-path", "--html-path", "--man-path", "--info-path", "--list-cmds=main"},
		"remote.push.update": {"push"},
	}
}
