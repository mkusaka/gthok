package git

import (
	"strconv"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func init() {
	register("reset", whReset)
	register("restore", whRestore)
	register("checkout", whCheckout)
	register("switch", whCheckout)
	register("clean", whClean)
	register("add", whAdd)
	register("rm", whRemove)
	register("mv", whMove)
	register("stash", whStash)
	register("apply", whApply)
	register("sparse-checkout", whSparse)
}

func whOperation(c *Context, action, mode string, scopes ...model.Scope) model.Operation {
	op := model.Operation{Coverage: model.CoverageSemantic, Scopes: scopes,
		Parameters: map[string]any{}, Origin: model.Origin{Mode: mode}}
	actions.Apply(&op, action)
	return op
}

func whUnresolved(op *model.Operation, field, reason, source string) {
	op.Unresolved = append(op.Unresolved, model.Unresolved{Field: field, Reason: reason, Source: source})
}

func whCurrentRef(op *model.Operation) {
	op.Target.Refs = model.UnknownSet()
	whUnresolved(op, "target.refs", "HEAD may be symbolic or detached", "git")
}

func whSet(values []string) *model.TargetSet {
	if len(values) == 0 {
		return model.UnknownSet()
	}
	kind := model.SetExact
	if len(values) > 1 {
		kind = model.SetSet
	}
	return &model.TargetSet{Kind: kind, Values: values, Complete: true}
}

// whPaths returns -1 when the selector input is unavailable, or its count.
func whPaths(c *Context, op *model.Operation, values []string) int {
	op.Target.Paths = model.UnknownSet()
	if o, ok := c.Parsed.Option("pathspec-from-file"); ok && !o.Negated {
		op.Parameters["pathspecFromFile"] = o.Value
		data, source, available := whInput(c, o.Value)
		if !available {
			whUnavailable(op, "target.paths", source)
			return -1
		}
		if c.Parsed.Has("pathspec-file-nul") {
			values = strings.Split(string(data), "\x00")
			if values[len(values)-1] == "" {
				values = values[:len(values)-1]
			}
		} else {
			values = nil
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSuffix(line, "\r")
				if strings.HasPrefix(line, "\"") {
					unquoted, err := strconv.Unquote(line)
					if err != nil {
						op.Coverage = model.CoverageConservative
						whUnresolved(op, "target.paths", "pathspec quoting requires Git interpretation", source)
						return -1
					}
					line = unquoted
				}
				if line != "" {
					values = append(values, line)
				}
			}
		}
	}
	if len(values) > 0 {
		// Pathspecs are selectors, not policy globs or resolved file names.
		op.Parameters["pathspecs"] = values
		whUnresolved(op, "target.paths", "expand pathspecs relative to the effective repository and prefix", "git")
	} else {
		whUnresolved(op, "target.paths", "affected paths depend on repository state", "git")
	}
	return len(values)
}

func whInput(c *Context, path string) ([]byte, string, bool) {
	if path == "-" {
		return c.Invocation.Stdin.Data, "stdin", c.Invocation.Stdin.Status == model.StdinCaptured
	}
	if c.Invocation.ReadFile != nil {
		data, err := c.Invocation.ReadFile(path)
		return data, "file", err == nil
	}
	return nil, "file", false
}

func whUnavailable(op *model.Operation, field, source string) {
	op.Coverage, op.UnknownReason = model.CoverageOpaque, model.UnknownInputUnavailable
	whUnresolved(op, field, "input was not provided for analysis", source)
}

func whFilters(op *model.Operation) {
	op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
	whUnresolved(op, "capabilities", "attributes may select external clean/smudge filters", "git")
}

func whInteractive(op *model.Operation) {
	op.Capabilities = append(op.Capabilities, model.CapabilityInteractive)
	op.Coverage, op.UnknownReason = model.CoverageOpaque, model.UnknownInteractive
	whUnresolved(op, "parameters.selection", "final selection is made interactively", "interactive")
}

// whLastMode follows parse-options' shared destination rather than flag order
// in a map (e.g. reset --hard --soft).
func whLastMode(p ParsedCommand, fallback string, names ...string) string {
	mode := fallback
	for _, o := range p.Options {
		for _, name := range names {
			if o.Name == name && !o.Negated {
				mode = name
			}
		}
	}
	return mode
}

func whRecurse(c *Context, op *model.Operation) {
	if o, ok := c.Parsed.Option("recurse-submodules"); ok {
		enabled := !o.Negated && (!o.HasValue || o.Value != "no" && o.Value != "false" && o.Value != "off" && o.Value != "0")
		op.Parameters["recurseSubmodules"] = enabled
		if enabled {
			op.Scopes = append(op.Scopes, model.ScopeSubmodule)
		}
	} else {
		whUnresolved(op, "parameters.recurseSubmodules", "submodule.recurse may enable recursive updates", "git")
	}
}

func whReset(c *Context) []model.Operation {
	p := c.Parsed
	mode := whLastMode(p, "mixed", "soft", "mixed", "hard", "merge", "keep")
	op := whOperation(c, "worktree.reset."+mode, mode, model.ScopeLocalRef)
	args := p.Values()
	revision := "HEAD"
	filePaths := p.Has("pathspec-from-file")
	paths := len(p.AfterDashDash) > 0
	if filePaths {
		count := whPaths(c, &op, nil)
		paths = count > 0
		if count < 0 {
			whUnresolved(&op, "parameters.mode", "an empty pathspec file performs a whole-tree reset", "file")
		}
	}
	if len(p.Positionals) > 0 && !p.Positionals[0].AfterDashDash {
		revision = args[0]
		args = args[1:]
	}
	if len(args) > 0 {
		paths = true
	}
	if p.Has("patch") {
		mode = "patch"
	} else if paths {
		mode = "paths"
	}
	actions.Apply(&op, "worktree.reset."+mode)
	op.Origin.Mode = mode
	op.Parameters["mode"], op.Parameters["intentToAdd"] = mode, p.Has("intent-to-add")
	op.Target.Revisions = model.Exact(revision)
	if mode == "paths" || mode == "patch" {
		op.Scopes = []model.Scope{model.ScopeIndex}
		if len(p.AfterDashDash) > 0 {
			args = nil
			for _, a := range p.AfterDashDash {
				args = append(args, a.Value)
			}
		} else if !p.Has("pathspec-from-file") && len(p.Positionals) > 0 {
			// Without --, Git must first test the leading token as a tree.
			op.Coverage = model.CoverageConservative
			whUnresolved(&op, "target.revisions", "leading operand may be a tree or a pathspec", "git")
			args = p.Values()
		}
		if !filePaths {
			whPaths(c, &op, args)
		}
	} else {
		whCurrentRef(&op)
		if mode != "soft" {
			op.Scopes = append(op.Scopes, model.ScopeIndex)
		}
		if mode == "hard" || mode == "merge" || mode == "keep" {
			op.Scopes = append(op.Scopes, model.ScopeWorktree)
			if !filePaths {
				whPaths(c, &op, nil)
			}
			whRecurse(c, &op)
			whFilters(&op)
		}
		if !filePaths && len(p.Positionals) > 0 && whLastMode(p, "", "soft", "mixed", "hard", "merge", "keep") == "" && !whHasSeparator(c, "--") {
			op.Coverage = model.CoverageConservative
			op.Target.Paths = model.UnknownSet()
			whUnresolved(&op, "parameters.mode", "operand may select an index-only path reset instead of a revision reset", "git")
		}
	}
	if mode == "mixed" || mode == "paths" {
		op.Parameters["refresh"] = !p.Has("no-refresh")
		if !p.Has("no-refresh") {
			op.Capabilities = append(op.Capabilities, model.CapabilityIndexRefresh)
		}
	}
	if mode == "patch" {
		whInteractive(&op)
	}
	return []model.Operation{op}
}

func whHasSeparator(c *Context, value string) bool {
	// Only inspect option syntax, not option values equal to "--".
	for i := len(c.Global.Tokens) + 1; i < len(c.Argv); i++ {
		isValue := false
		for _, o := range c.Parsed.Options {
			if o.ValueIndex == i && o.ValueIndex != o.Span.Start {
				isValue = true
			}
		}
		if !isValue && c.Argv[i] == value {
			return true
		}
	}
	return false
}

func whRestore(c *Context) []model.Operation {
	p := c.Parsed
	staged, worktree := p.Has("staged"), p.Has("worktree")
	if _, s := p.Option("staged"); !s {
		if _, w := p.Option("worktree"); !w {
			worktree = true
		}
	}
	op := whOperation(c, "worktree.restore.paths", "paths")
	op.Parameters["staged"], op.Parameters["worktree"] = staged, worktree
	if staged {
		op.Scopes = append(op.Scopes, model.ScopeIndex)
	}
	if worktree {
		op.Scopes = append(op.Scopes, model.ScopeWorktree)
		whRecurse(c, &op)
		whFilters(&op)
	}
	source := "index"
	if staged {
		source = "HEAD"
	}
	if o, ok := p.Option("source"); ok && !o.Negated {
		source = o.Value
	}
	op.Parameters["source"] = source
	if source != "index" {
		op.Target.Revisions = model.Exact(source)
	}
	whCheckoutFlags(c, &op)
	whPaths(c, &op, p.Values())
	if p.Has("patch") {
		whInteractive(&op)
	}
	return []model.Operation{op}
}

func whCheckoutFlags(c *Context, op *model.Operation) {
	p := c.Parsed
	op.Parameters["force"] = p.Has("force") || p.Has("discard-changes")
	op.Parameters["stage"] = whLastMode(p, "normal", "ours", "theirs")
	op.Parameters["merge"] = p.Has("merge") || p.Has("conflict")

	if o, ok := p.Option("overlay"); ok {
		op.Parameters["overlay"] = !o.Negated
	}
}

func whCheckout(c *Context) []model.Operation {
	p := c.Parsed
	command := p.Path[0]
	mode := "switch"
	newBranch := ""
	for _, o := range p.Options {
		if o.Negated {
			continue
		}
		switch o.Name {
		case "b", "create":
			mode, newBranch = "create", o.Value
		case "B", "force-create":
			mode, newBranch = "reset", o.Value
		case "orphan":
			mode, newBranch = "orphan", o.Value
		}
	}
	if p.Has("detach") {
		mode = "detach"
	}
	branchMode := mode
	filePaths := p.Has("pathspec-from-file")
	pathMode := command == "checkout" && (len(p.AfterDashDash) > 0 || filePaths || p.Has("patch") || p.Has("ours") || p.Has("theirs"))
	if command == "checkout" && newBranch == "" && !p.Has("detach") && len(p.Positionals) > 1 {
		pathMode = true
	}
	if pathMode {
		mode = "paths"
	}
	op := whOperation(c, "worktree."+command+"."+mode, mode, model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree)
	if filePaths {
		count := whPaths(c, &op, nil)
		if count <= 0 {
			pathMode, mode = false, branchMode
			actions.Apply(&op, "worktree."+command+"."+mode)
			op.Origin.Mode = mode
			if count < 0 {
				whUnresolved(&op, "parameters.mode", "an empty pathspec file permits branch switching", "file")
			}
		}
	}
	whCheckoutFlags(c, &op)
	args := p.Values()
	if pathMode {
		op.Scopes = []model.Scope{model.ScopeWorktree}
		source := "index"
		if len(p.Positionals) > 0 && !p.Positionals[0].AfterDashDash && (!p.Has("ours") && !p.Has("theirs")) {
			source, args = args[0], args[1:]
			op.Scopes = []model.Scope{model.ScopeIndex, model.ScopeWorktree}
			op.Target.Revisions = model.Exact(source)
			if !whHasSeparator(c, "--") && !p.Has("pathspec-from-file") {
				op.Coverage = model.CoverageConservative
				whUnresolved(&op, "target.revisions", "leading operand may be a tree or a pathspec", "git")
				args = p.Values()
			}
		}
		op.Parameters["source"] = source
		if !filePaths {
			whPaths(c, &op, args)
		}
	} else {
		op.Target.Paths = model.UnknownSet()
		whUnresolved(&op, "target.paths", "checked-out trees determine affected paths", "git")
		if newBranch != "" {
			op.Target.Refs = model.Exact("refs/heads/" + strings.TrimPrefix(newBranch, "refs/heads/"))
			op.Parameters["newBranch"] = newBranch
			op.Scopes = append(op.Scopes, model.ScopeConfig)
		} else {
			whCurrentRef(&op)
		}
		if len(args) == 0 {
			args = []string{"HEAD"}
		}
		op.Target.Revisions = whSet(args)
		if !filePaths && len(p.Positionals) == 1 && command == "checkout" && newBranch == "" && !p.Has("detach") && !whHasSeparator(c, "--") {
			op.Coverage = model.CoverageConservative
			whUnresolved(&op, "parameters.mode", "operand may name a branch, commit, or path", "git")
		}
		if mode == "switch" && !p.Has("detach") {
			whUnresolved(&op, "parameters.createBranch", "DWIM remote tracking and checkout.guess require local ref/config resolution", "git")
			op.Scopes = append(op.Scopes, model.ScopeConfig)
		}
		if p.Has("merge") || p.Has("conflict") {
			op.Scopes = append(op.Scopes, model.ScopeStash, model.ScopeObjects)
		}
		whSingleRevisionEdit(c, &op)
	}
	whRecurse(c, &op)
	whFilters(&op)
	if p.Has("patch") {
		whInteractive(&op)
	}
	if !pathMode && newBranch != "" && (mode == "create" || mode == "reset") {
		action := "ref.create"
		if mode == "reset" {
			action = "ref.update"
		}
		ref := rrOperation(action, model.ScopeLocalRef, model.ScopeReflog, model.ScopeConfig)
		ref.Origin.Mode = mode
		ref.Target.Refs, ref.Target.Revisions = op.Target.Refs, op.Target.Revisions
		ref.Parameters["refKind"], ref.Parameters["force"] = "branch", mode == "reset"
		if mode == "reset" {
			rrMaybeCreate(&ref)
		}
		return []model.Operation{op, ref}
	}
	return []model.Operation{op}
}

func whForceCount(p ParsedCommand) int {
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
	return force
}

func whClean(c *Context) []model.Operation {
	p := c.Parsed
	action := "worktree.clean"
	if p.Has("dry-run") {
		action = "worktree.clean.preview"
	}
	op := whOperation(c, action, "clean", model.ScopeWorktree)
	force := whForceCount(p)
	ignored := "exclude"
	if p.Has("x") {
		ignored = "include"
	}
	if p.Has("X") {
		ignored = "only"
	}
	op.Parameters["dryRun"], op.Parameters["forceCount"] = p.Has("dry-run"), force
	op.Parameters["nestedRepositories"] = force > 1
	op.Parameters["ignored"], op.Parameters["directories"] = ignored, p.Has("d") || len(p.Positionals) > 0
	op.Parameters["exclude"] = whOptionValues(p, "exclude")
	whPaths(c, &op, p.Values())
	if force == 0 && !p.Has("interactive") && !p.Has("dry-run") {
		whUnresolved(&op, "parameters.requireForce", "clean.requireForce determines whether deletion is permitted", "git")
	}
	if p.Has("interactive") {
		if p.Has("dry-run") {
			op.Capabilities = append(op.Capabilities, model.CapabilityInteractive)
		} else {
			whInteractive(&op)
		}
	}
	return []model.Operation{op}
}

func whAdd(c *Context) []model.Operation {
	p := c.Parsed
	action := "index.add.update"
	if p.Has("refresh") {
		action = "index.add.refresh"
	}
	if p.Has("dry-run") {
		action = "index.add.preview"
	}
	op := whOperation(c, action, "add", model.ScopeIndex)
	op.Capabilities = []model.Capability{model.CapabilityIndexRefresh}
	if !p.Has("refresh") && !p.Has("dry-run") {
		op.Scopes = append(op.Scopes, model.ScopeObjects)
		whFilters(&op)
	}
	all := len(p.Positionals) > 0 && !p.Has("update") && !p.Has("renormalize")
	for _, o := range p.Options {
		switch o.Name {
		case "all":
			all = !o.Negated
		case "ignore-removal":
			all = o.Negated
		}
	}
	op.Parameters["all"], op.Parameters["update"] = all, p.Has("update") || p.Has("renormalize")
	op.Parameters["dryRun"], op.Parameters["intentToAdd"] = p.Has("dry-run"), p.Has("intent-to-add")
	if o, ok := p.Option("chmod"); ok && !o.Negated {
		op.Parameters["chmod"] = o.Value
	}
	whPaths(c, &op, p.Values())
	if p.Has("patch") || p.Has("interactive") || p.Has("edit") {
		whInteractive(&op)
		if p.Has("edit") {
			op.Capabilities = append(op.Capabilities, model.CapabilityEditor)
		}
	}
	return []model.Operation{op}
}

func whRemove(c *Context) []model.Operation {
	p := c.Parsed
	action := "index.rm.remove"
	if p.Has("dry-run") {
		action = "index.rm.preview"
	}
	op := whOperation(c, action, "remove", model.ScopeIndex)
	if !p.Has("cached") {
		op.Scopes = append(op.Scopes, model.ScopeWorktree, model.ScopeSubmodule)
		whUnresolved(&op, "target.paths", "submodule removal may also update .gitmodules and absorb a gitdir", "git")
	}
	op.Parameters["cached"], op.Parameters["recursive"] = p.Has("cached"), p.Has("r")
	op.Parameters["force"], op.Parameters["dryRun"] = p.Has("force"), p.Has("dry-run")
	whPaths(c, &op, p.Values())
	if p.Has("dry-run") && !p.Has("cached") {
		// rm absorbs old-form submodule gitdirs before its show_only return.
		op.Coverage = model.CoverageConservative
	}
	return []model.Operation{op}
}

func whMove(c *Context) []model.Operation {
	action := "worktree.mv.move"
	if c.Parsed.Has("dry-run") {
		action = "worktree.mv.preview"
	}
	op := whOperation(c, action, "move", model.ScopeIndex, model.ScopeWorktree)
	op.Parameters["force"], op.Parameters["dryRun"] = c.Parsed.Has("force"), c.Parsed.Has("dry-run")
	op.Parameters["operands"] = c.Parsed.Values()
	op.Target.Paths = model.UnknownSet()
	whUnresolved(&op, "target.paths", "directories and submodule destinations require filesystem resolution", "git")
	if !c.Parsed.Has("dry-run") {
		op.Scopes = append(op.Scopes, model.ScopeSubmodule, model.ScopeConfig)
	}
	return []model.Operation{op}
}

func whOptionValues(p ParsedCommand, name string) []string {
	var values []string
	for _, o := range p.Options {
		if o.Name == name {
			if o.Negated {
				values = nil
			} else {
				values = append(values, o.Value)
			}
		}
	}
	return values
}

func whSubcommand(p ParsedCommand, fallback string) string {
	if len(p.Path) > 1 {
		return p.Path[1]
	}
	return fallback
}

func whStash(c *Context) []model.Operation {
	p := c.Parsed
	mode := whSubcommand(p, "push")
	action := mode
	if mode == "save" {
		action = "push"
	}
	op := whOperation(c, "stash."+action, mode, model.ScopeStash)
	op.Target.Refs = model.Exact("refs/stash")
	op.Parameters["stash"] = "stash@{0}"
	args := p.Values()
	switch mode {
	case "list", "show":
		whDiffSideEffects(c, &op)
	case "drop", "clear", "store", "import":
	case "create", "export":
		op.Scopes = []model.Scope{model.ScopeObjects}
		op.Target.Refs = &model.TargetSet{Kind: model.SetNone, Complete: true}
		if mode == "create" {
			op.Scopes = append(op.Scopes, model.ScopeIndex)
			op.Capabilities = append(op.Capabilities, model.CapabilityIndexRefresh)
			if len(args) > 0 {
				value := strings.Join(args, " ")
				op.Texts = &model.Texts{Body: &model.Text{Value: value, Source: "inline", Bytes: int64(len(value))}}
			}
		}
		if mode == "export" {
			if o, ok := p.Option("to-ref"); ok && !o.Negated {
				op.Scopes = append(op.Scopes, model.ScopeLocalRef)
				op.Target.Refs = model.UnknownSet()
				op.Parameters["toRef"] = o.Value
				whUnresolved(&op, "target.refs", "export ref requires full refname resolution", "git")
			}
		}
	case "push", "save":
		op.Scopes = append(op.Scopes, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects)
		op.Parameters["keepIndex"] = p.Has("keep-index") || p.Has("patch")
		if o, ok := p.Option("keep-index"); ok {
			op.Parameters["keepIndex"] = !o.Negated
		}
		op.Parameters["staged"] = p.Has("staged") && !p.Has("patch")
		untracked := "exclude"
		for _, o := range p.Options {
			if o.Name == "include-untracked" || o.Name == "all" {
				untracked = "exclude"
				if !o.Negated {
					untracked = "include"
					if o.Name == "all" {
						untracked = "all"
					}
				}
			}
		}
		op.Parameters["untracked"] = untracked
		if mode == "push" {
			whPaths(c, &op, args)
		}
		if p.Has("patch") {
			whInteractive(&op)
		}
	case "apply":
		op.Scopes = []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}
		fallthrough
	case "pop", "branch":
		if mode != "apply" {
			op.Scopes = append(op.Scopes, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects)
		}
		op.Parameters["reinstateIndex"] = p.Has("index")
		op.Target.Paths = model.UnknownSet()
		whUnresolved(&op, "target.paths", "stash tree determines affected paths", "git")
		if mode == "branch" {
			op.Scopes = append(op.Scopes, model.ScopeLocalRef, model.ScopeConfig)
			if len(args) > 0 {
				op.Parameters["newBranch"] = args[0]
				op.Target.Refs = whSet([]string{"refs/stash", "refs/heads/" + strings.TrimPrefix(args[0], "refs/heads/")})
				args = args[1:]
			}
		}
	}
	if mode == "push" || mode == "save" || mode == "pop" || mode == "apply" || mode == "branch" || mode == "create" {
		whFilters(&op)
	}
	if len(args) > 0 && mode != "push" && mode != "save" && mode != "create" {
		op.Target.Revisions = whSet(args)
		op.Parameters["stash"] = args[0]
	}
	if mode == "push" || mode == "save" || mode == "store" {
		whMessage(c, &op, false, "body")
		if mode == "save" && len(args) > 0 {
			value := strings.Join(args, " ")
			op.Texts = &model.Texts{Body: &model.Text{Value: value, Source: "inline", Bytes: int64(len(value))}}
			op.Editable = nil
		}
	}
	return []model.Operation{op}
}

func whApply(c *Context) []model.Operation {
	p := c.Parsed
	read := p.Has("check") || p.Has("stat") || p.Has("numstat") || p.Has("summary") || p.Has("build-fake-ancestor")
	if p.Has("apply") {
		read = false
	}
	action := "worktree.apply.patch"
	if read {
		action = "worktree.apply.stat"
		if p.Has("check") {
			action = "worktree.apply.check"
		}
	}
	op := whOperation(c, action, "apply")
	if p.Has("cached") || p.Has("index") || p.Has("3way") || p.Has("intent-to-add") {
		op.Scopes = append(op.Scopes, model.ScopeIndex)
	}
	if !p.Has("cached") {
		op.Scopes = append(op.Scopes, model.ScopeWorktree)
	}
	op.Parameters["cached"], op.Parameters["index"] = p.Has("cached"), p.Has("index") || p.Has("3way")
	op.Parameters["reject"], op.Parameters["reverse"] = p.Has("reject"), p.Has("reverse")
	op.Parameters["unsafePaths"] = p.Has("unsafe-paths")
	if o, ok := p.Option("build-fake-ancestor"); ok && !o.Negated {
		op.Capabilities = append(op.Capabilities, model.CapabilityFileWrite)
		op.Scopes = append(op.Scopes, model.ScopeFileOutput)
		op.Parameters["fakeAncestor"] = o.Value
	}
	if p.Has("reject") && !read {
		op.Capabilities = append(op.Capabilities, model.CapabilityFileWrite)
	}
	whPatchInput(c, &op)
	return []model.Operation{op}
}

func whPatchInput(c *Context, op *model.Operation) {
	inputs := c.Parsed.Values()
	if len(inputs) == 0 {
		inputs = []string{"-"}
	}
	op.Parameters["inputs"] = inputs
	op.Target.Paths = model.UnknownSet()
	for _, path := range inputs {
		_, source, available := whInput(c, path)
		if !available {
			whUnavailable(op, "target.paths", source)
		}
	}
	whUnresolved(op, "target.paths", "patch paths require patch/mail format interpretation", "input")
}

func whSparse(c *Context) []model.Operation {
	p := c.Parsed
	mode := whSubcommand(p, "list")
	action := mode
	if mode == "clean" && p.Has("dry-run") {
		action = "preview"
	}
	op := whOperation(c, "worktree.sparse-checkout."+action, mode)
	switch mode {
	case "list", "check-rules":
		op.Scopes = []model.Scope{model.ScopeConfig, model.ScopeWorktree}
	case "clean":
		op.Scopes = []model.Scope{model.ScopeWorktree}
		op.Parameters["dryRun"] = p.Has("dry-run")
		if !p.Has("force") && !p.Has("dry-run") {
			whUnresolved(&op, "parameters.requireForce", "clean.requireForce controls deletion", "git")
		}
	default:
		op.Scopes = []model.Scope{model.ScopeConfig, model.ScopeIndex, model.ScopeWorktree}
		whFilters(&op)
	}
	whPaths(c, &op, nil)
	op.Parameters["patterns"] = p.Values()
	for _, name := range []string{"cone", "sparse-index"} {
		if o, ok := p.Option(name); ok {
			key := "cone"
			if name == "sparse-index" {
				key = "sparseIndex"
			}
			op.Parameters[key] = !o.Negated
		}
	}
	if p.Has("stdin") || mode == "check-rules" {
		op.Parameters["stdin"] = true
		if c.Invocation.Stdin.Status != model.StdinCaptured {
			whUnavailable(&op, "parameters.patterns", "stdin")
		} else {
			op.Parameters["input"] = string(c.Invocation.Stdin.Data)
		}
	}
	return []model.Operation{op}
}
