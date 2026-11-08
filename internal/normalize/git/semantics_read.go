package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/registry"
)

var adminReadCommands = []string{
	"log", "show", "diff", "diff-tree", "diff-index", "diff-files", "diff-pairs", "rev-list", "rev-parse", "cat-file",
	"ls-files", "ls-tree", "grep", "blame", "annotate", "pickaxe", "shortlog", "describe", "name-rev", "format-rev",
	"merge-base", "show-branch", "whatchanged", "range-diff", "format-patch", "last-modified", "cherry",
	"check-attr", "check-ignore", "check-mailmap", "check-ref-format", "var", "repo",
	"column", "stripspace", "patch-id", "show-index", "get-tar-commit-id", "fmt-merge-msg", "pack-redundant", "url-parse",
	"verify-commit", "verify-tag",
}

func init() {
	for _, name := range adminReadCommands {
		register(name, readSemantics)
	}
	// diff.c and its plumbing entrypoints handle these after setup_revisions.
	for _, name := range []string{"diff", "diff-files", "diff-index", "diff-tree", "diff-pairs"} {
		entry := commands[name]
		for _, flag := range []string{"cached", "staged", "merge-base", "base", "ours", "theirs", "stdin"} {
			entry.Options = append(entry.Options, objectOption(flag, "", "none"))
		}
		for _, short := range []string{"1", "2", "3", "0", "c"} {
			entry.Options = append(entry.Options, objectOption("", short, "none"))
		}
		commands[name] = entry
	}
	entry := commands["rev-parse"]
	for _, flag := range []string{"parseopt", "sq-quote", "local-env-vars", "revs-only", "no-revs", "flags", "no-flags", "verify", "quiet", "sq", "symbolic", "symbolic-full-name", "show-toplevel", "show-superproject-working-tree", "show-prefix", "show-cdup", "git-dir", "absolute-git-dir", "git-common-dir", "is-inside-git-dir", "is-inside-work-tree", "is-bare-repository", "is-shallow-repository", "shared-index-path", "show-ref-format"} {
		entry.Options = append(entry.Options, objectOption(flag, "", "none"))
	}
	for _, flag := range []string{"resolve-git-dir", "git-path", "path-format", "prefix", "output-object-format"} {
		o := objectOption(flag, "", "required")
		o.Flags = nil // these use opt_with_value or consume the following argv
		entry.Options = append(entry.Options, o)
	}
	for _, flag := range []string{"short", "abbrev-ref", "show-object-format"} {
		entry.Options = append(entry.Options, objectOption(flag, "", "optional"))
	}
	commands["rev-parse"] = entry
	entry = commands["var"]
	entry.Options = append(entry.Options, objectOption("", "l", "none"))
	commands["var"] = entry
	entry = commands["check-ref-format"]
	for _, flag := range []string{"allow-onelevel", "normalize", "branch"} {
		entry.Options = append(entry.Options, objectOption(flag, "", "none"))
	}
	commands["check-ref-format"] = entry
	entry = commands["patch-id"]
	for _, flag := range []string{"stable", "unstable", "verbatim"} {
		entry.Options = append(entry.Options, objectOption(flag, "", "none"))
	}
	commands["patch-id"] = entry
	entry = commands["show-index"]
	entry.Options = append(entry.Options, registry.Option{Long: "object-format", Arity: "required", Type: "manual", Flags: []string{"ATTACHED_VALUE"}})
	commands["show-index"] = entry
}

func readSemantics(c *Context) []model.Operation {
	p, args, command := c.Parsed, c.Parsed.Values(), c.Parsed.Path[0]
	op := adminOperation(c, "git."+command, "git."+command+".read", model.EffectRead, model.ScopeObjects)
	op.Parameters = map[string]any{}
	var paths, revisions []string
	for _, arg := range p.Positionals {
		if arg.AfterDashDash {
			paths = append(paths, arg.Value)
		} else {
			revisions = append(revisions, arg.Value)
		}
	}
	switch command {
	case "ls-files", "check-attr", "check-ignore":
		op.Scopes = []model.Scope{model.ScopeIndex, model.ScopeWorktree}
		op.Target.Paths = adminSet(args, true)
		if command == "check-attr" {
			// Attribute names precede --; only the following operands are paths.
			op.Target.Paths = adminSet(paths, true)
		}
	case "blame", "annotate", "pickaxe":
		op.Scopes = append(op.Scopes, model.ScopeWorktree)
		if len(paths) == 0 && len(revisions) > 0 {
			paths, revisions = revisions[len(revisions)-1:], revisions[:len(revisions)-1]
		}
		op.Target.Paths = adminSet(paths, true)
		op.Target.Revisions = adminSet(revisions, false)
	case "grep":
		op.Scopes = append(op.Scopes, model.ScopeIndex, model.ScopeWorktree)
		if len(revisions) > 0 && !p.Has("e") && !p.Has("f") {
			op.Parameters["pattern"] = revisions[0]
			revisions = revisions[1:]
		}
		op.Target.Paths = adminSet(paths, true)
		if len(revisions) > 0 {
			op.Target.Revisions = adminSet(revisions, false)
			adminUnresolved(&op, "target.paths", "grep operands before -- may name trees or pathspecs", "git")
		}
	case "for-each-ref", "show-ref", "show-branch":
		op.Scopes = []model.Scope{model.ScopeLocalRef}
		op.Target.Refs = model.UnknownSet()
		adminUnresolved(&op, "target.refs", "ref names and patterns require local namespace expansion", "git")
	case "check-mailmap", "check-ref-format", "var", "column", "stripspace", "patch-id", "show-index", "get-tar-commit-id", "fmt-merge-msg", "url-parse":
		op.Scopes = nil
	case "repo":
		op.Scopes = append(op.Scopes, model.ScopeLocalRef, model.ScopeConfig)
	default:
		if len(paths) > 0 {
			op.Target.Paths = adminSet(paths, true)
		}
		op.Target.Revisions = adminSet(revisions, false)
	}
	if strings.HasPrefix(command, "diff") {
		op.Parameters["cached"] = p.Has("cached") || p.Has("staged")
		switch command {
		case "diff", "diff-files":
			op.Scopes = append(op.Scopes, model.ScopeIndex, model.ScopeWorktree)
		case "diff-index", "diff-tree":
			op.Scopes = append(op.Scopes, model.ScopeIndex)
			if command == "diff-index" && !p.Has("cached") {
				op.Scopes = append(op.Scopes, model.ScopeWorktree)
			}
		}
		if command == "diff-files" {
			op.Target.Paths, op.Target.Revisions = adminSet(args, true), nil
		}
		// Only porcelain diff refreshes the stat cache on disk, not diff-* plumbing.
		if command == "diff" && len(status(c)[0].Capabilities) > 0 && !p.Has("no-index") {
			op.Capabilities = append(op.Capabilities, model.CapabilityIndexRefresh)
		}
	}
	for _, o := range p.Options {
		if o.Name == "output" && !o.Negated {
			// diff_opt_output opens each file while parsing, not only the last one.
			adminFileOutput(&op, o.Value)
			op.Action = "git." + command + ".output"
		}
	}
	if command == "format-patch" {
		// builtin/log.c:cmd_format_patch writes files unless --stdout is selected.
		if !p.Has("stdout") && !p.Has("output") || p.Has("output-directory") {
			path := ""
			if o, ok := p.Option("output-directory"); ok && !o.Negated {
				path = o.Value
			}
			adminFileOutput(&op, path)
			op.Parameters["outputDirectory"] = path
			op.Target.Paths = model.UnknownSet()
			if path != "" {
				adminUnresolved(&op, "target.paths", "patch file names depend on commit subjects and format configuration", "git")
			}
			op.Action = "git.format-patch.output"
		}
	}
	readExternalCapabilities(&op, c)
	if command == "cat-file" {
		batch := p.Has("batch") || p.Has("batch-check") || p.Has("batch-command")
		op.Parameters["batch"] = batch
		if batch && !p.Has("batch-all-objects") {
			if c.Invocation.Stdin.Status != model.StdinCaptured {
				op.Coverage, op.Effect, op.UnknownReason = model.CoverageOpaque, model.EffectUnknown, model.UnknownInputUnavailable
				op.Action = "git.cat-file.protocol"
				adminUnresolved(&op, "target.revisions", "live cat-file protocols must not be read ahead", "stdin")
			} else {
				sep := "\n"
				if p.Has("Z") || p.Has("z") {
					sep = "\x00"
				}
				var objects []string
				for _, line := range strings.Split(strings.TrimSuffix(string(c.Invocation.Stdin.Data), sep), sep) {
					if line == "" || p.Has("batch-command") && line == "flush" {
						continue
					}
					if p.Has("batch-command") {
						mode, object, ok := strings.Cut(line, " ")
						if !ok || mode != "info" && mode != "contents" {
							return []model.Operation{adminInvalid(c, "invalid captured cat-file batch command")}
						}
						line = object
					}
					if p.Has("textconv") || p.Has("filters") {
						line, _, _ = strings.Cut(line, " ")
					}
					objects = append(objects, line)
				}
				op.Target.Revisions = adminSet(objects, false)
			}
		}
		if len(args) == 2 && !batch {
			op.Target.Revisions = model.Exact(args[1])
		}
	} else if p.Has("stdin") {
		objectInput(&op, c, false)
		if c.Invocation.Stdin.Status == model.StdinCaptured {
			adminUnresolved(&op, "target.revisions", "captured revision or path input is not expanded", "stdin")
		}
	}
	return []model.Operation{op}
}

func readExternalCapabilities(op *model.Operation, c *Context) {
	p, command := c.Parsed, c.Parsed.Path[0]
	if p.Has("ext-diff") || command == "diff" && !adminDisabled(p, "ext-diff") {
		adminExternal(op, "parameters.externalDiff", "diff.external, GIT_EXTERNAL_DIFF and diff drivers require local configuration")
	}
	textconv := p.Has("textconv")
	if (command == "diff" || command == "blame" || command == "annotate" || command == "pickaxe") && !adminDisabled(p, "textconv") {
		textconv = true
	}
	// Log/show patches allow textconv by default; plain log metadata does not.
	patch := command == "show" || command == "whatchanged"
	for _, o := range p.Options {
		switch o.Name {
		case "patch", "u":
			patch = !o.Negated
		case "no-patch":
			patch = o.Negated
		}
	}
	if (command == "show" || command == "whatchanged" || command == "log") && patch && !adminDisabled(p, "textconv") {
		textconv = true
	}
	if command == "range-diff" {
		textconv = true // range-diff.c builds its input with git log -p.
	}
	if textconv || p.Has("filters") {
		adminExternal(op, "parameters.filters", "attributes and configured textconv/content filters require local resolution")
	}
	signature := false
	for _, o := range p.Options {
		switch o.Name {
		case "show-signature":
			signature = !o.Negated
		case "no-show-signature":
			signature = o.Negated
		}
	}
	if p.Has("open-files-in-pager") || signature || command == "verify-commit" || command == "verify-tag" {
		adminExternal(op, "", "")
	}
}
