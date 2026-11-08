package git

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/registry"
)

func init() {
	for _, name := range []string{"hash-object", "read-tree", "update-index", "write-tree", "commit-tree", "mktag", "mktree", "fast-import", "fast-export", "unpack-objects", "index-pack", "pack-objects", "prune", "prune-packed", "repack", "gc", "count-objects", "fsck", "fsck-objects", "verify-pack", "archive", "bundle", "commit-graph", "multi-pack-index", "maintenance"} {
		register(name, objectSemantics)
	}
	// These entrypoints hand-parse argv; their extracted parse-options tables are empty.
	installObjectManualGrammar("unpack-objects", []registry.Option{
		objectOption("", "n", "none"), objectOption("", "q", "none"), objectOption("", "r", "none"),
		objectOption("strict", "", "optional"), objectOption("pack_header", "", "required"), objectOption("max-input-size", "", "required"),
	})
	index := []registry.Option{objectOption("", "o", "required"), objectOption("", "v", "none")}
	for _, name := range []string{"stdin", "fix-thin", "check-self-contained-and-connected", "verify", "verify-stat", "verify-stat-only", "show-resolving-progress", "report-end-of-input", "rev-index", "no-rev-index"} {
		index = append(index, objectOption(name, "", "none"))
	}
	for _, name := range []string{"strict", "fsck-objects", "keep", "promisor"} {
		index = append(index, objectOption(name, "", "optional"))
	}
	for _, name := range []string{"threads", "pack_header", "index-version", "max-input-size", "object-format", "progress-title"} {
		index = append(index, objectOption(name, "", "required"))
	}
	installObjectManualGrammar("index-pack", index)
	entry := commands["update-index"]
	// cmd_update_index restarts option parsing after each pathname.
	entry.ParseFlags = nil
	commands["update-index"] = entry
	entry = commands["fast-export"]
	entry.Options = append(entry.Options, revisionOptions...)
	commands["fast-export"] = entry
	entry = commands["archive"]
	for _, name := range []string{"format", "prefix", "add-file", "add-virtual-file", "mtime"} {
		entry.Options = append(entry.Options, registry.Option{Long: name, Arity: "required", Negatable: true})
	}
	entry.Options = append(entry.Options, registry.Option{Long: "list", Short: "l", Arity: "none"}, registry.Option{Long: "verbose", Short: "v", Arity: "none"}, registry.Option{Long: "worktree-attributes", Arity: "none"})
	for _, short := range "0123456789" {
		entry.Options = append(entry.Options, registry.Option{Short: string(short), Arity: "none"})
	}
	commands["archive"] = entry
}

func objectOption(long, short, arity string) registry.Option {
	o := registry.Option{Long: long, Short: short, Arity: arity, Type: "manual"}
	if arity == "required" && short == "" && long != "progress-title" {
		o.Flags = []string{"ATTACHED_VALUE"}
	}
	return o
}

func installObjectManualGrammar(name string, options []registry.Option) {
	entry := commands[name]
	entry.Options = append(entry.Options, options...)
	commands[name] = entry
}

func objectSemantics(c *Context) []model.Operation {
	p, args, command := c.Parsed, c.Parsed.Values(), c.Parsed.Path[0]
	op := adminOperation(c, "objects", "objects."+command, model.EffectWrite, model.ScopeObjects)
	op.Target.Refs = &model.TargetSet{Kind: model.SetNone, Complete: true}
	if len(args) > 0 {
		op.Target.Revisions = adminSet(args, false)
	}
	op.Parameters = map[string]any{}
	switch command {
	case "hash-object":
		op.Action = "objects.hash.read"
		op.Effect = model.EffectRead
		if p.Has("w") {
			op.Action, op.Effect = "objects.hash.write", model.EffectWrite
		}
		op.Target.Paths = adminSet(args, false)
		op.Target.Revisions = nil
		op.Parameters["write"], op.Parameters["stdin"], op.Parameters["stdinPaths"] = p.Has("w"), p.Has("stdin"), p.Has("stdin-paths")
		if o, ok := p.Option("t"); ok {
			op.Parameters["objectType"] = o.Value
		} else {
			op.Parameters["objectType"] = "blob"
		}
		if p.Has("stdin") || p.Has("stdin-paths") {
			objectInput(&op, c, false)
		}
		if p.Has("stdin-paths") && c.Invocation.Stdin.Status == model.StdinCaptured {
			paths, err := objectInputPaths(c.Invocation.Stdin.Data, false, false)
			if err != nil {
				return []model.Operation{adminInvalid(c, err.Error())}
			}
			op.Target.Paths = adminSet(paths, false)
		}
		if !p.Has("no-filters") && (!p.Has("stdin") || len(args) > 0 || p.Has("path")) {
			adminExternal(&op, "parameters.filters", "attributes and configured clean filters require local resolution")
		}
	case "read-tree":
		op.Family, op.Action, op.Scopes = "index", "index.read-tree", []model.Scope{model.ScopeIndex}
		op.Parameters["merge"], op.Parameters["reset"], op.Parameters["updateWorktree"] = p.Has("m") || p.Has("reset") || p.Has("prefix"), p.Has("reset"), p.Has("u")
		op.Parameters["dryRun"] = p.Has("dry-run") || p.Has("debug-unpack")
		if p.Has("u") {
			op.Scopes = append(op.Scopes, model.ScopeWorktree)
			if !p.Has("dry-run") && !p.Has("debug-unpack") {
				adminExternal(&op, "parameters.filters", "worktree updates may run configured content filters")
			}
		}
		if p.Has("recurse-submodules") {
			op.Scopes = append(op.Scopes, model.ScopeSubmodule)
			if p.Has("u") && !p.Has("dry-run") && !p.Has("debug-unpack") {
				op.Scopes = append(op.Scopes, model.ScopeLocalRef, model.ScopeReflog)
				op.Target.Refs = model.UnknownSet()
				adminUnresolved(&op, "target.refs", "recursive worktree updates can change selected submodule HEADs", "git")
			}
		}
		if p.Has("dry-run") || p.Has("debug-unpack") {
			op.Effect, op.Action = model.EffectRead, "index.read-tree.preview"
		}
		if o, ok := p.Option("index-output"); ok && !o.Negated && op.Effect == model.EffectWrite {
			adminFileOutput(&op, o.Value)
		}
	case "update-index":
		op.Family, op.Action, op.Scopes = "index", "index.update", []model.Scope{model.ScopeIndex}
		op.Target.Paths, op.Target.Revisions = adminSet(args, false), nil
		for _, name := range []string{"add", "remove", "force-remove", "refresh", "really-refresh", "info-only"} {
			key := map[string]string{"force-remove": "forceRemove", "really-refresh": "reallyRefresh", "info-only": "infoOnly"}[name]
			if key == "" {
				key = name
			}
			op.Parameters[key] = p.Has(name)
		}
		if o, ok := p.Option("chmod"); ok {
			op.Parameters["chmod"] = o.Value
		}
		if p.Has("refresh") || p.Has("really-refresh") {
			op.Capabilities = append(op.Capabilities, model.CapabilityIndexRefresh)
		}
		// Updating tracked content (even without --add) can store new blobs.
		if objectIndexStoresContent(p) {
			op.Scopes = append(op.Scopes, model.ScopeObjects)
			adminExternal(&op, "parameters.filters", "index content updates may run configured clean filters")
		}
		if p.Has("stdin") || p.Has("index-info") {
			objectInput(&op, c, false)
			if c.Invocation.Stdin.Status == model.StdinCaptured {
				paths, err := objectInputPaths(c.Invocation.Stdin.Data, p.Has("z"), p.Has("index-info"))
				if err != nil {
					return []model.Operation{adminInvalid(c, err.Error())}
				}
				op.Target.Paths = adminSet(paths, false)
			}
		}
		if o, ok := p.Option("cacheinfo"); ok {
			parts := strings.SplitN(o.Value, ",", 3)
			if len(parts) == 3 {
				op.Target.Paths = model.Exact(parts[2])
				op.Parameters["cacheInfo"] = true
			}
		}
	case "write-tree":
		op.Scopes = append(op.Scopes, model.ScopeIndex)
	case "commit-tree":
		op.Target.Revisions = adminSet(args, false)
		objectCommitMessage(&op, c)
		if p.Has("gpg-sign") {
			adminExternal(&op, "", "")
		}
	case "mktag", "mktree", "unpack-objects":
		objectInput(&op, c, false)
		if command == "unpack-objects" && p.Has("n") {
			op.Effect, op.Action = model.EffectRead, "objects.unpack-objects.preview"
		}
	case "fast-import":
		op.Target.Refs = model.UnknownSet()
		op.Scopes = append(op.Scopes, model.ScopeLocalRef, model.ScopeReflog)
		objectInput(&op, c, true)
		op.Detail = "fast-import command/data stream is not decoded, including when finite input is provided"
		if p.Has("allow-unsafe-features") {
			adminFileOutput(&op, "")
			op.Effect = model.EffectUnknown
		}
		objectMarkOutputs(&op, p)
		// Signature modes can invoke verification/signing programs.
		for _, name := range []string{"signed-commits", "signed-tags"} {
			if o, ok := p.Option(name); ok && strings.Contains(o.Value, "invalid") {
				adminExternal(&op, "", "")
			}
		}
	case "fast-export":
		op.Effect = model.EffectRead
		objectMarkOutputs(&op, p)
		if op.Effect == model.EffectWrite {
			op.Action = "objects.fast-export.output"
		}
	case "index-pack":
		verify := p.Has("verify") || p.Has("verify-stat") || p.Has("verify-stat-only")
		op.Parameters["verify"], op.Parameters["stdin"] = verify, p.Has("stdin")
		if verify {
			op.Effect, op.Action = model.EffectRead, "objects.index-pack.verify"
		} else if o, ok := p.Option("o"); ok {
			adminFileOutput(&op, o.Value)
		} else if len(args) > 0 {
			adminFileOutput(&op, strings.TrimSuffix(args[0], ".pack")+".idx")
		}
		if !verify && op.Target.Paths != nil {
			op.Target.Paths.Complete = false
			adminUnresolved(&op, "target.paths", "reverse-index and companion outputs depend on index-pack options and configuration", "git")
		}
		if p.Has("stdin") {
			objectInput(&op, c, false)
		}
	case "pack-objects":
		if p.Has("stdout") {
			op.Effect, op.Action = model.EffectRead, "objects.pack-objects.read"
		} else {
			adminFileOutput(&op, "")
			if len(args) > 0 {
				op.Parameters["outputPrefix"] = args[0]
			}
		}
		if p.Has("unpack-unreachable") {
			op.Effect, op.Action = model.EffectWrite, "objects.pack-objects"
		}
		objectInput(&op, c, false)
	case "prune", "prune-packed":
		op.Parameters["dryRun"] = p.Has("dry-run")
		if p.Has("dry-run") {
			op.Effect, op.Action = model.EffectRead, op.Action+".preview"
		}
	case "repack":
		op.Parameters["deletePacks"] = p.Has("d")
		op.Parameters["dryRun"] = p.Has("dry-run")
		if p.Has("dry-run") {
			op.Effect, op.Action = model.EffectRead, "objects.repack.preview"
		}
	case "gc":
		op.Scopes = append(op.Scopes, model.ScopeLocalRef, model.ScopeReflog, model.ScopeWorktreeAdmin)
		op.Target.Refs = model.UnknownSet()
		adminUnresolved(&op, "target.refs", "ref packing and reflog expiration depend on repository data and GC configuration", "git")
		prune := "configured"
		if o, ok := p.Option("prune"); ok {
			prune = o.Value
			if o.Negated {
				prune = "none"
			} else if !o.HasValue {
				prune = "configured"
			}
		}
		op.Parameters["prune"] = prune
	case "count-objects", "verify-pack":
		op.Effect = model.EffectRead
	case "fsck", "fsck-objects":
		op.Action, op.Effect = "objects.fsck", model.EffectRead
		if p.Has("lost-found") {
			op.Action = "objects.fsck.lost-found"
			adminFileOutput(&op, "")
		}
	case "commit-graph", "multi-pack-index":
		if len(p.Path) < 2 {
			return []model.Operation{adminInvalid(c, "object database maintenance requires a subcommand")}
		}
		mode := p.Path[1]
		op.Action += "." + mode
		op.Origin.Mode = mode
		if mode == "verify" {
			op.Effect = model.EffectRead
		}
	case "maintenance":
		return maintenanceSemantics(c)
	case "archive":
		return archiveSemantics(c)
	case "bundle":
		return bundleSemantics(c)
	}
	if command == "gc" || command == "repack" {
		for _, name := range []string{"expire-to", "filter-to"} {
			if o, ok := p.Option(name); ok && !o.Negated && op.Effect == model.EffectWrite {
				adminFileOutput(&op, o.Value)
				op.Target.Paths = model.UnknownSet()
				adminUnresolved(&op, "target.paths", "pack file names are derived from the selected objects and output prefix", "git")
			}
		}
	}
	return []model.Operation{op}
}

func objectIndexStoresContent(p ParsedCommand) bool {
	metadata, infoOnly, forceRemove, next := false, false, false, 0
	applyBefore := func(index int) {
		for next < len(p.Options) && (index < 0 || p.Options[next].Span.Start < index) {
			o := p.Options[next]
			next++
			switch o.Name {
			case "assume-unchanged", "no-assume-unchanged", "skip-worktree", "no-skip-worktree", "fsmonitor-valid", "no-fsmonitor-valid":
				metadata = !o.Negated
			case "info-only":
				infoOnly = !o.Negated
			case "force-remove":
				forceRemove = !o.Negated
			}
		}
	}
	for _, path := range p.Positionals {
		applyBefore(path.Span.Start)
		if !metadata && !infoOnly && !forceRemove {
			return true
		}
	}
	applyBefore(-1)
	return !metadata && !infoOnly && !forceRemove && (p.Has("stdin") || p.Has("again"))
}

func objectInput(op *model.Operation, c *Context, opaque bool) {
	if c.Invocation.Stdin.Status != model.StdinCaptured || opaque {
		adminUnresolved(op, "input", "input bytes or protocol semantics are unavailable for analysis", "stdin")
	}
	if opaque {
		op.Effect, op.Coverage, op.UnknownReason = model.EffectUnknown, model.CoverageOpaque, model.UnknownInputUnavailable
	}
}

func objectInputPaths(data []byte, nul, indexInfo bool) ([]string, error) {
	sep := "\n"
	if nul {
		sep = "\x00"
	}
	lines := strings.Split(strings.TrimSuffix(string(data), sep), sep)
	var paths []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		if indexInfo {
			_, path, ok := strings.Cut(line, "\t")
			if !ok {
				return nil, fmt.Errorf("index-info record has no tab-separated path")
			}
			line = path
		}
		if !nul && strings.HasPrefix(line, "\"") {
			var err error
			line, err = strconv.Unquote(line)
			if err != nil {
				return nil, fmt.Errorf("invalid quoted input path: %w", err)
			}
		}
		paths = append(paths, line)
	}
	return paths, nil
}

func objectCommitMessage(op *model.Operation, c *Context) {
	var chunks []string
	text := model.Text{Source: "stdin"}
	for _, o := range c.Parsed.Options {
		if o.Name == "m" {
			chunks = append(chunks, strings.TrimSuffix(o.Value, "\n")+"\n")
			text.Source = "inline"
		}
		if o.Name != "F" {
			continue
		}
		text.Source, text.Path = "file", o.Value
		if o.Value == "-" {
			text.Source = "stdin"
			if c.Invocation.Stdin.Status == model.StdinCaptured {
				chunks = append(chunks, string(c.Invocation.Stdin.Data))
			} else {
				text.Status = c.Invocation.Stdin.Status
				adminUnresolved(op, "texts.body", "commit-tree message requires captured stdin", "stdin")
			}
		} else if c.Invocation.ReadFile != nil {
			data, err := c.Invocation.ReadFile(o.Value)
			if err == nil {
				chunks = append(chunks, string(data))
			} else {
				adminUnresolved(op, "texts.body", "commit-tree message file could not be read", "file")
			}
		} else {
			adminUnresolved(op, "texts.body", "commit-tree message file was not provided", "file")
		}
	}
	if len(chunks) == 0 && len(op.Unresolved) == 0 {
		if c.Invocation.Stdin.Status == model.StdinCaptured {
			chunks = append(chunks, string(c.Invocation.Stdin.Data))
		} else {
			text.Status = c.Invocation.Stdin.Status
			adminUnresolved(op, "texts.body", "commit-tree reads its message from stdin", "stdin")
		}
	}
	var message strings.Builder
	for _, chunk := range chunks {
		if message.Len() > 0 {
			message.WriteByte('\n')
		}
		message.WriteString(chunk)
	}
	text.Value = message.String()
	if len(chunks) > 1 {
		text.Source, text.Path = "unknown", ""
	}
	text.Bytes = int64(len(text.Value))
	op.Texts = &model.Texts{Body: &text}
}

func objectMarkOutputs(op *model.Operation, p ParsedCommand) {
	for _, name := range []string{"export-marks", "export-pack-edges"} {
		if o, ok := p.Option(name); ok && !o.Negated {
			effect := op.Effect
			adminFileOutput(op, o.Value)
			if effect == model.EffectUnknown {
				op.Effect = effect
			}
		}
	}
}

func maintenanceSemantics(c *Context) []model.Operation {
	if len(c.Parsed.Path) < 2 {
		return []model.Operation{adminInvalid(c, "maintenance requires a subcommand")}
	}
	p, mode := c.Parsed, c.Parsed.Path[1]
	op := adminOperation(c, "maintenance", "maintenance."+mode, model.EffectWrite)
	op.Origin.Mode = mode
	op.Parameters = map[string]any{}
	switch mode {
	case "run", "is-needed":
		op.Scopes = []model.Scope{model.ScopeObjects, model.ScopeLocalRef, model.ScopeReflog, model.ScopeWorktreeAdmin}
		op.Target.Refs = model.UnknownSet()
		adminUnresolved(&op, "target.refs", "maintenance task ref targets depend on repository state", "git")
		var tasks []string
		for _, o := range p.Options {
			if o.Name == "task" && !o.Negated {
				tasks = append(tasks, o.Value)
			}
		}
		op.Parameters["tasks"] = tasks
		if mode == "is-needed" {
			op.Effect = model.EffectRead
		} else if len(tasks) == 0 {
			op.Capabilities = []model.Capability{model.CapabilityNetwork}
			adminUnresolved(&op, "parameters.tasks", "configured maintenance tasks may include prefetch", "git")
		} else {
			for _, task := range tasks {
				if task == "prefetch" {
					op.Capabilities = append(op.Capabilities, model.CapabilityNetwork)
				}
			}
		}
	case "start", "stop":
		op.Scopes = []model.Scope{model.ScopeConfig, model.ScopeFileOutput}
		op.Capabilities = []model.Capability{model.CapabilityExternalExec, model.CapabilityFileWrite}
		op.Parameters["scheduler"] = "configured"
		if o, ok := p.Option("scheduler"); ok {
			op.Parameters["scheduler"] = o.Value
		}
	case "register", "unregister":
		op.Scopes = []model.Scope{model.ScopeConfig}
		op.Target.ConfigKeys = &model.TargetSet{Kind: model.SetPattern, Values: []string{"maintenance.*"}, Complete: true}
		if o, ok := p.Option("config-file"); ok {
			op.Target.Paths = model.Exact(o.Value)
		}
	}
	return []model.Operation{op}
}

func archiveSemantics(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	op := adminOperation(c, "archive", "archive.create", model.EffectRead, model.ScopeObjects)
	if p.Has("list") {
		op.Action = "archive.list"
	} else if len(args) > 0 {
		op.Target.Revisions = model.Exact(args[0])
		if len(args) > 1 {
			op.Target.Paths = adminSet(args[1:], true)
		}
	}
	if o, ok := p.Option("output"); ok && !o.Negated {
		op.Action = "archive.output"
		adminFileOutput(&op, o.Value)
	}
	if o, ok := p.Option("remote"); ok && !o.Negated {
		op.Target.Remote = o.Value
		if slices.Equal(c.Argv, c.Invocation.Argv) {
			op.Editable = map[string]model.EditLocator{"/target/remote": {Kind: "argv", Flag: "remote", ValueType: "string", Spans: []model.ArgSpan{o.Span}}}
		}
		op.Capabilities = append(op.Capabilities, model.CapabilityNetwork)
		adminUnresolved(&op, "target.revisions", "archive revision contents are determined by the remote", "remote")
	}
	if p.Has("exec") {
		adminExternal(&op, "", "")
	}
	if o, ok := p.Option("format"); ok && o.Value != "tar" && o.Value != "zip" && !p.Has("list") {
		adminExternal(&op, "parameters.archiver", "tar.<format>.command may select an external archiver")
	}
	return []model.Operation{op}
}

func bundleSemantics(c *Context) []model.Operation {
	if len(c.Parsed.Path) < 2 {
		return []model.Operation{adminInvalid(c, "bundle requires a subcommand")}
	}
	args, mode := c.Parsed.Values(), c.Parsed.Path[1]
	op := adminOperation(c, "bundle", "bundle."+mode, model.EffectRead, model.ScopeObjects)
	if len(args) == 0 {
		return []model.Operation{adminInvalid(c, "bundle requires a file operand")}
	}
	op.Target.Paths = model.Exact(args[0])
	if mode == "create" && args[0] != "-" {
		adminFileOutput(&op, args[0])
	} else if mode == "create" {
		op.Action = "bundle.stdout"
	} else if mode == "unbundle" {
		op.Effect = model.EffectWrite
	}
	if len(args) > 1 {
		op.Target.Revisions = adminSet(args[1:], false)
	}
	return []model.Operation{op}
}
