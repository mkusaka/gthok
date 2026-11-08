package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func init() {
	register("commit", whCommit)
	register("merge", whMerge)
	register("rebase", whRebase)
	register("cherry-pick", whSequencer)
	register("revert", whSequencer)
	register("am", whAM)
	register("notes", whNotes)
	register("bisect", whBisect)
}

func whSingleRevisionEdit(c *Context, op *model.Operation) {
	if len(c.Parsed.Positionals) != 1 || op.Coverage != model.CoverageSemantic || op.Target.Revisions == nil {
		return
	}
	if op.Editable == nil {
		op.Editable = map[string]model.EditLocator{}
	}
	op.Parameters["revision"] = c.Parsed.Positionals[0].Value
	op.Editable["parameters/revision"] = model.EditLocator{Kind: "positional", Parameter: "revision", ValueType: "string", Key: []string{"target.revisions"}, Spans: []model.ArgSpan{c.Parsed.Positionals[0].Span}}
}

// Messages retain their source, not a fabricated final editor/reused message.
// Only one transport (or repeated -m paragraphs) has an unambiguous write-back.
func whMessage(c *Context, op *model.Operation, editor bool, field string) {
	var parts []string
	var spans []model.ArgSpan
	var transport, input, source string
	var unavailable bool
	var status model.StdinStatus
	mixed := false
	for _, o := range c.Parsed.Options {
		if o.Negated {
			if o.Name == "message" {
				parts, spans = nil, nil
				transport, source = "", ""
			}
			continue
		}
		if o.Name != "message" && o.Name != "file" {
			continue
		}
		if c.Parsed.Path[0] == "commit" && o.Name == "file" {
			last, _ := c.Parsed.Option("file")
			if last.Span.Start != o.Span.Start {
				continue
			}
		}
		kind := "argv"
		value := o.Value
		nextSource := "inline"
		if o.Name == "file" {
			kind, input = "file", o.Value
			data, provenance, ok := whInput(c, o.Value)
			// merge's option_read_message uses strbuf_read_file even for "-".
			if c.Parsed.Path[0] == "merge" && input == "-" {
				data, provenance, ok = nil, "file", false
				if c.Invocation.ReadFile != nil {
					var err error
					data, err = c.Invocation.ReadFile(input)
					ok = err == nil
				}
			}
			nextSource = provenance
			value = string(data)
			if provenance == "stdin" {
				kind = "stdin"
				status = c.Invocation.Stdin.Status
				if status == "" {
					status = model.StdinNotCaptured
				}
			}
			if !ok {
				unavailable = true
				whUnavailable(op, "texts."+field, provenance)
			}
		}
		if transport != "" && (transport != "argv" || kind != "argv") {
			mixed = true
		}
		transport, source = kind, nextSource
		if c.Parsed.Path[0] == "merge" && o.Name == "file" && len(parts) > 0 {
			parts = []string{strings.Join(parts, "\n\n") + "\n" + value}
		} else {
			parts = append(parts, value)
		}
		spans = append(spans, o.Span)
	}
	var text *model.Text
	if len(parts) > 0 {
		value := strings.Join(parts, "\n\n")
		text = &model.Text{Value: value, Source: source, Bytes: int64(len(value))}
		if source == "file" {
			text.Path = input
		}
		if unavailable {
			text.Status = status
			text.Truncated = status == model.StdinTooLarge
			if text.Truncated {
				op.UnknownReason = model.UnknownInputTooLarge
			}
		}
	}
	if editor {
		op.Capabilities = append(op.Capabilities, model.CapabilityEditor)
		whUnresolved(op, "texts."+field, "final message is chosen in an editor", "editor")
		if text == nil {
			text = &model.Text{Source: "editor"}
		}
	}
	if text == nil {
		return
	}
	if op.Texts == nil {
		op.Texts = &model.Texts{}
	}
	if field == "notes" {
		op.Texts.Notes = text
	} else {
		op.Texts.Body = text
	}
	if !editor && !unavailable && !mixed && len(spans) > 0 {
		flag := "message"
		if transport != "argv" {
			flag = "file"
		}
		if op.Editable == nil {
			op.Editable = map[string]model.EditLocator{}
		}
		op.Editable["texts/"+field+"/value"] = model.EditLocator{Kind: transport, Flag: flag, ValueType: "string", Input: input, Text: field, Spans: spans}
	}
}

func whSigning(c *Context, op *model.Operation) {
	if c.Parsed.Has("gpg-sign") {
		op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
		op.Parameters["gpgSign"] = true
	}
}

func whCommit(c *Context) []model.Operation {
	p := c.Parsed
	mode := "create"
	if p.Has("amend") {
		mode = "amend"
	}
	format := false
	for _, o := range p.Options {
		if o.Name == "short" || o.Name == "porcelain" || o.Name == "long" {
			format = !o.Negated
		}
	}
	dry := p.Has("dry-run") || format || p.Has("null")
	if dry {
		mode = "preview"
	}
	op := whOperation(c, "history.commit."+mode, mode, model.ScopeLocalRef, model.ScopeIndex, model.ScopeObjects)
	op.Parameters["amend"], op.Parameters["dryRun"] = p.Has("amend"), dry
	op.Parameters["all"], op.Parameters["allowEmpty"] = p.Has("all"), p.Has("allow-empty")
	op.Parameters["noVerify"] = p.Has("no-verify")
	op.Parameters["only"], op.Parameters["include"] = p.Has("only"), p.Has("include")
	whPaths(c, &op, p.Values())
	if p.Has("all") || p.Has("include") || len(p.Positionals) > 0 || p.Has("pathspec-from-file") {
		whFilters(&op)
	}
	if dry {
		op.Scopes = []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}
		op.Capabilities = append(op.Capabilities, model.CapabilityIndexRefresh)
	} else {
		whCurrentRef(&op)
		editor := !p.Has("message") && !p.Has("file") && !p.Has("reuse-message")
		if o, ok := p.Option("fixup"); ok && !o.Negated {
			op.Parameters["fixup"] = o.Value
			editor = strings.HasPrefix(o.Value, "amend:") || strings.HasPrefix(o.Value, "reword:")
			whUnresolved(&op, "texts.body", "fixup message is derived from the named commit", "git")
		}
		if o, ok := p.Option("squash"); ok && !o.Negated {
			op.Parameters["squash"] = o.Value
			whUnresolved(&op, "texts.body", "squash prefix is derived from the named commit", "git")
		}
		for _, name := range []string{"reuse-message", "reedit-message"} {
			if o, ok := p.Option(name); ok && !o.Negated {
				op.Parameters["reuseMessage"] = o.Value
				whUnresolved(&op, "texts.body", "message must be read from an existing commit", "git")
			}
		}
		if o, ok := p.Option("edit"); ok {
			editor = !o.Negated
		}
		whMessage(c, &op, editor, "body")
		if op.Texts == nil {
			whUnresolved(&op, "texts.body", "message depends on commit/sequencer state", "git")
		}
		if p.Has("fixup") || p.Has("squash") || p.Has("reuse-message") || p.Has("reedit-message") {
			delete(op.Editable, "texts/body/value")
		}
		whSigning(c, &op)
	}
	if p.Has("interactive") || p.Has("patch") {
		whInteractive(&op)
	}
	if op.Coverage == model.CoverageSemantic {
		messageSwitchEdits(c, &op)
	}
	return []model.Operation{op}
}

func whMerge(c *Context) []model.Operation {
	p := c.Parsed
	mode := whLastMode(p, "start", "abort", "continue", "quit")
	if mode == "start" && p.Has("squash") {
		mode = "squash"
	}
	op := whOperation(c, "history.merge."+mode, mode, model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects)
	op.Parameters["squash"], op.Parameters["noVerify"] = p.Has("squash"), p.Has("no-verify")
	commit := true
	if o, ok := p.Option("commit"); ok {
		commit = !o.Negated
	}
	if p.Has("squash") {
		commit = false
	}
	op.Parameters["commit"] = commit
	ff := "allow"
	for _, o := range p.Options {
		if o.Name == "ff" {
			ff = "allow"
			if o.Negated {
				ff = "never"
			}
		} else if o.Name == "ff-only" && !o.Negated {
			ff = "only"
		}
	}
	op.Parameters["fastForward"] = ff
	whCurrentRef(&op)
	op.Target.Paths = model.UnknownSet()
	switch mode {
	case "quit":
		op.Scopes = []model.Scope{model.ScopeWorktreeAdmin, model.ScopeStash}
		op.Target.Refs, op.Unresolved = nil, nil
	case "squash":
		// --squash leaves HEAD alone, but cmd_merge still writes ORIG_HEAD.
		op.Target.Refs, op.Unresolved = model.Exact("ORIG_HEAD"), nil
	case "start":
		op.Target.Revisions = whSet(p.Values())
		if len(p.Positionals) == 0 {
			whUnresolved(&op, "target.revisions", "default merge heads come from branch upstream configuration", "git")
		}
		whSingleRevisionEdit(c, &op)
	}
	if mode == "start" || mode == "continue" || mode == "squash" {
		editor := commit && ff != "only" && !p.Has("message") && !p.Has("file")
		if o, ok := p.Option("edit"); ok {
			editor = commit && !o.Negated
		}
		whMessage(c, &op, editor, "body")
		if commit {
			whSigning(c, &op)
		}
	}
	if p.Has("autostash") {
		op.Scopes = append(op.Scopes, model.ScopeStash)
	}
	if mode != "quit" {
		whFilters(&op)
	}
	if op.Coverage == model.CoverageSemantic {
		messageSwitchEdits(c, &op)
	}
	return []model.Operation{op}
}

func whRebase(c *Context) []model.Operation {
	p := c.Parsed
	mode := whLastMode(p, "start", "continue", "abort", "skip", "quit", "edit-todo", "show-current-patch")
	op := whOperation(c, "history.rebase."+mode, mode, model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects)
	whCurrentRef(&op)
	op.Target.Paths = model.UnknownSet()
	if mode == "show-current-patch" {
		op.Scopes = []model.Scope{model.ScopeObjects, model.ScopeWorktreeAdmin}
		op.Target.Refs, op.Unresolved = nil, nil
		// The merge backend uses show, which may use configured textconv.
		op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
		return []model.Operation{op}
	}
	if mode == "quit" || mode == "edit-todo" {
		op.Scopes = []model.Scope{model.ScopeWorktreeAdmin}
		op.Target.Refs, op.Unresolved = nil, nil
		if mode == "quit" {
			op.Scopes = append(op.Scopes, model.ScopeStash)
		}
	}
	op.Parameters["noVerify"] = p.Has("no-verify")
	op.Parameters["root"], op.Parameters["autosquash"] = p.Has("root"), p.Has("autosquash")
	if o, ok := p.Option("onto"); ok && !o.Negated {
		op.Parameters["onto"] = o.Value
	}
	if o, ok := p.Option("update-refs"); ok {
		op.Parameters["updateRefs"] = !o.Negated
		if !o.Negated {
			op.Target.Refs = model.UnknownSet()
			whUnresolved(&op, "target.refs", "other branches pointing at rebased commits are updated, excluding checked-out branches", "git")
		}
	} else if mode == "start" {
		whUnresolved(&op, "parameters.updateRefs", "rebase.updateRefs may update other local refs", "git")
	}
	if mode == "start" {
		op.Target.Revisions = whSet(p.Values())
		if len(p.Positionals) < 2 {
			whUnresolved(&op, "target.revisions", "upstream or working branch defaults require repository state", "git")
		}
	}
	exec := whOptionValues(p, "exec")
	if len(exec) > 0 {
		op.Parameters["exec"] = exec
		op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
	}
	if mode == "continue" || mode == "skip" {
		// A saved todo may include arbitrary exec lines, even without -x now.
		op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec, model.CapabilityEditor)
		whUnresolved(&op, "parameters.todo", "remaining todo actions require sequencer state", "git")
	}
	if p.Has("interactive") || mode == "edit-todo" {
		op.Capabilities = append(op.Capabilities, model.CapabilityEditor)
		whInteractive(&op)
	} else if p.Has("autosquash") {
		op.Capabilities = append(op.Capabilities, model.CapabilityEditor)
	}
	if p.Has("autostash") {
		op.Scopes = append(op.Scopes, model.ScopeStash)
	}
	if mode != "quit" && mode != "edit-todo" {
		whFilters(&op)
	}
	whSigning(c, &op)
	return []model.Operation{op}
}

func whSequencer(c *Context) []model.Operation {
	p := c.Parsed
	command := p.Path[0]
	mode := whLastMode(p, "start", "continue", "abort", "skip", "quit")
	if mode == "start" && p.Has("no-commit") {
		mode = "apply"
	}
	op := whOperation(c, "history."+command+"."+mode, mode, model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects)
	op.Parameters["noCommit"] = p.Has("no-commit")
	whCurrentRef(&op)
	op.Target.Paths = model.UnknownSet()
	if mode == "apply" {
		op.Scopes = []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}
		op.Target.Refs, op.Unresolved = nil, nil
	}
	if mode == "quit" {
		op.Scopes = []model.Scope{model.ScopeWorktreeAdmin}
		op.Target.Refs, op.Unresolved = nil, nil
	}
	if mode == "start" || mode == "apply" {
		op.Target.Revisions = whSet(p.Values())
		whSingleRevisionEdit(c, &op)
	}
	if mode == "start" || mode == "continue" || mode == "skip" {
		editor := command == "revert" || mode == "continue"
		if o, ok := p.Option("edit"); ok {
			editor = !o.Negated
		}
		if editor {
			op.Capabilities = append(op.Capabilities, model.CapabilityEditor)
			whUnresolved(&op, "texts.body", "commit message may be edited", "editor")
		}
		whSigning(c, &op)
	}
	if mode != "quit" {
		whFilters(&op)
	}
	if o, ok := p.Option("mainline"); ok && !o.Negated {
		op.Parameters["mainline"] = o.Value
	}
	return []model.Operation{op}
}

func whAM(c *Context) []model.Operation {
	p := c.Parsed
	mode := whLastMode(p, "start", "continue", "resolved", "abort", "skip", "quit", "retry", "show-current-patch", "allow-empty")
	if mode == "resolved" {
		mode = "continue"
	}
	op := whOperation(c, "history.am."+mode, mode, model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects)
	op.Parameters["noVerify"] = p.Has("no-verify")
	if mode == "show-current-patch" {
		op.Scopes = []model.Scope{model.ScopeWorktreeAdmin}
		return []model.Operation{op}
	}
	if mode == "quit" {
		op.Scopes = []model.Scope{model.ScopeWorktreeAdmin}
	} else {
		whCurrentRef(&op)
		op.Target.Paths = model.UnknownSet()
		if mode == "start" {
			whPatchInput(c, &op)
		}
	}
	if p.Has("interactive") {
		op.Capabilities = append(op.Capabilities, model.CapabilityEditor)
		whInteractive(&op)
	}
	if mode != "quit" {
		whFilters(&op)
	}
	whSigning(c, &op)
	return []model.Operation{op}
}

func whNotes(c *Context) []model.Operation {
	p := c.Parsed
	mode := whSubcommand(p, "list")
	action := mode
	if mode == "prune" && p.Has("dry-run") {
		action = "preview"
	}
	op := whOperation(c, "notes."+action, mode, model.ScopeLocalRef, model.ScopeObjects)
	ref := c.Invocation.Env["GIT_NOTES_REF"]
	if o, ok := p.Option("ref"); ok && !o.Negated {
		ref = o.Value
	}
	if ref == "" {
		op.Target.Refs = model.UnknownSet()
		whUnresolved(&op, "target.refs", "core.notesRef defaults to refs/notes/commits", "git")
	} else {
		if !strings.HasPrefix(ref, "refs/notes/") {
			if strings.HasPrefix(ref, "notes/") {
				ref = "refs/" + ref
			} else {
				ref = "refs/notes/" + ref
			}
		}
		op.Target.Refs = model.Exact(ref)
	}
	args := p.Values()
	if len(args) == 0 && mode != "list" && mode != "prune" && mode != "get-ref" && mode != "merge" {
		args = []string{"HEAD"}
	}
	if len(args) > 0 {
		op.Target.Revisions = whSet(args)
		whSingleRevisionEdit(c, &op)
	}
	op.Parameters["force"], op.Parameters["allowEmpty"] = p.Has("force"), p.Has("allow-empty")
	switch mode {
	case "add", "append", "edit":
		editor := !p.Has("message") && !p.Has("file") && !p.Has("reuse-message")
		if o, ok := p.Option("edit"); ok {
			editor = !o.Negated || editor
		}
		if p.Has("reedit-message") {
			editor = true
		}
		whMessage(c, &op, editor, "notes")
		if p.Has("reuse-message") || p.Has("reedit-message") {
			whUnresolved(&op, "texts.notes", "note contents must be read from a blob", "git")
			delete(op.Editable, "texts/notes/value")
		}
		if p.Has("separator") {
			// Git's custom separator changes the normalized paragraph join.
			op.Coverage = model.CoverageConservative
			delete(op.Editable, "texts/notes/value")
			whUnresolved(&op, "texts.notes", "custom separator requires Git's paragraph assembly", "git")
		}
	case "copy", "remove":
		if p.Has("stdin") || p.Has("for-rewrite") {
			op.Parameters["stdin"] = true
			op.Target.Revisions = model.UnknownSet()
			if c.Invocation.Stdin.Status != model.StdinCaptured {
				whUnavailable(&op, "target.revisions", "stdin")
			} else {
				whUnresolved(&op, "target.revisions", "stdin object list requires notes protocol interpretation", "stdin")
			}
		}
	case "merge":
		op.Scopes = append(op.Scopes, model.ScopeWorktreeAdmin)
		op.Parameters["mode"] = whLastMode(p, "start", "commit", "abort")
	case "prune":
		op.Parameters["dryRun"] = p.Has("dry-run")
	}
	return []model.Operation{op}
}

func whBisect(c *Context) []model.Operation {
	p := c.Parsed
	mode := whSubcommand(p, "")
	args := p.Values()
	if mode == "" && len(args) > 0 {
		mode, args = args[0], args[1:]
	}
	switch mode {
	case "old":
		mode = "good"
	case "new":
		mode = "bad"
	case "view":
		mode = "visualize"
	}
	action := "history.bisect." + mode
	if _, ok := actions.Lookup(action); !ok {
		action = "history.bisect.mark"
	}
	op := whOperation(c, action, mode, model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeWorktreeAdmin)
	if op.Effect == model.EffectRead {
		op.Scopes = []model.Scope{model.ScopeLocalRef, model.ScopeWorktreeAdmin}
	} else {
		op.Target.Refs = model.UnknownSet()
		op.Target.Paths = model.UnknownSet()
		whUnresolved(&op, "target.refs", "bisect state determines bisect refs and the next checked-out HEAD", "git")
		whFilters(&op)
	}
	if mode == "run" {
		op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
		op.Parameters["command"] = args
	} else if mode == "visualize" {
		op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
	} else if len(args) > 0 {
		op.Target.Revisions = whSet(args)
	}
	if mode == "mark" || action == "history.bisect.mark" {
		op.Coverage = model.CoverageConservative
		whUnresolved(&op, "parameters.term", "custom good/bad terms require bisect state", "git")
	}
	return []model.Operation{op}
}

func whDiffSideEffects(c *Context, op *model.Operation) {
	p := c.Parsed
	if p.Has("output") {
		op.Capabilities = append(op.Capabilities, model.CapabilityFileWrite)
		op.Scopes = append(op.Scopes, model.ScopeFileOutput)
		if o, ok := p.Option("output"); ok {
			op.Parameters["output"] = o.Value
		}
	}
	if whSubcommand(p, "") == "show" || p.Has("patch") {
		textconv := true
		if o, ok := p.Option("textconv"); ok {
			textconv = !o.Negated
		}
		if p.Has("ext-diff") || textconv {
			op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
		}
	}
}
