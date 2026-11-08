package git

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func init() {
	register("branch", refsBranch)
	register("tag", refsTag)
	register("update-ref", refsUpdate)
	register("symbolic-ref", refsSymbolic)
	register("pack-refs", refsPack)
	register("reflog", refsReflog)
	register("replace", refsReplace)
	register("show-ref", refsList)
	register("for-each-ref", refsList)
}

func rrOperation(action string, scopes ...model.Scope) model.Operation {
	op := model.Operation{Coverage: model.CoverageSemantic, Scopes: scopes, Parameters: map[string]any{}}
	actions.Apply(&op, action)
	return op
}

func rrUnresolved(op *model.Operation, field, reason, source string) {
	op.Unresolved = append(op.Unresolved, model.Unresolved{Field: field, Reason: reason, Source: source})
}

func rrInvalid(op model.Operation, reason string) []model.Operation {
	op.Effect, op.Coverage, op.UnknownReason, op.Detail = model.EffectUnknown, model.CoverageConservative, model.UnknownParseError, reason
	return []model.Operation{op}
}

func rrSet(values []string, pattern bool) *model.TargetSet {
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
	return &model.TargetSet{Kind: kind, Values: values, Complete: true}
}

func rrNames(values []string, prefix string) []string {
	refs := make([]string, len(values))
	for i, value := range values {
		refs[i] = prefix + value
	}
	return refs
}

func rrEditable(c *Context, op *model.Operation, pointer string, arg ParsedArgument) {
	// Expanded alias spans do not address the original execution argv.
	if strings.Join(c.Argv, "\x00") != strings.Join(c.Invocation.Argv, "\x00") {
		return
	}
	if op.Editable == nil {
		op.Editable = map[string]model.EditLocator{}
	}
	op.Editable[strings.TrimPrefix(pointer, "/")] = model.EditLocator{Kind: "positional", ValueType: "string", Spans: []model.ArgSpan{arg.Span}}
}

func rrCurrent(op *model.Operation) {
	op.Target.Refs = model.UnknownSet()
	rrUnresolved(op, "target.refs", "current branch requires symbolic HEAD resolution", "git")
}

func rrFilters(p ParsedCommand) bool {
	for _, o := range p.Options {
		switch o.Name {
		case "contains", "no-contains", "with", "without", "merged", "no-merged", "points-at", "forked":
			if !o.Negated {
				return true
			}
		}
	}
	return false
}

func refsBranch(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	action := "ref.branch.create"
	switch {
	case p.Has("delete") || p.Has("D") || p.Has("delete-merged"):
		action = "ref.branch.delete"
	case p.Has("move") || p.Has("M"):
		action = "ref.branch.rename"
	case p.Has("copy") || p.Has("C"):
		action = "ref.branch.copy"
	case p.Has("set-upstream-to") || p.Has("unset-upstream"):
		action = "ref.branch.upstream"
	case p.Has("edit-description"):
		action = "ref.branch.description"
	case p.Has("list") || p.Has("show-current") || rrFilters(p) || len(args) == 0:
		action = "ref.branch.list"
	}
	op := rrOperation(action, model.ScopeLocalRef)
	op.Parameters["force"] = p.Has("force") || p.Has("D") || p.Has("M") || p.Has("C")
	prefix, allBranches := "refs/heads/", false
	for _, o := range p.Options {
		switch o.Name {
		case "remotes":
			prefix, allBranches = "refs/remotes/", false
		case "all":
			prefix, allBranches = "refs/heads/", true
		}
	}
	switch action {
	case "ref.branch.list":
		if p.Has("show-current") {
			rrCurrent(&op)
		} else if len(args) > 0 {
			op.Target.Refs = rrSet(rrNames(args, prefix), true)
			if allBranches {
				op.Target.Refs = rrSet(append(rrNames(args, "refs/heads/"), rrNames(args, "refs/remotes/")...), true)
			}
		} else if allBranches {
			op.Target.Refs = rrSet([]string{"refs/heads/*", "refs/remotes/*"}, true)
		} else {
			op.Target.Refs = rrSet([]string{prefix + "*"}, true)
		}
	case "ref.branch.upstream", "ref.branch.description":
		op.Scopes = []model.Scope{model.ScopeConfig}
		if len(args) > 1 {
			return rrInvalid(op, "one branch name expected")
		}
		if len(args) == 0 || args[0] == "HEAD" {
			rrCurrent(&op)
		} else {
			op.Target.Refs = model.Exact("refs/heads/" + args[0])
			op.Target.ConfigKeys = rrSet([]string{"branch." + args[0] + ".*"}, true)
		}
		if u, ok := p.Option("set-upstream-to"); ok && !u.Negated {
			op.Parameters["upstream"] = u.Value
		}
		op.Parameters["unset"] = p.Has("unset-upstream")
		if action == "ref.branch.description" {
			op.Capabilities = []model.Capability{model.CapabilityEditor}
			rrUnresolved(&op, "texts.body", "branch description will be edited", "editor")
		}
	case "ref.branch.rename", "ref.branch.copy":
		op.Scopes = append(op.Scopes, model.ScopeConfig, model.ScopeReflog)
		if len(args) < 1 || len(args) > 2 {
			return rrInvalid(op, "one or two branch names expected")
		}
		op.Parameters["destination"] = args[len(args)-1]
		if len(args) == 1 {
			rrCurrent(&op)
			op.Target.Refs.Values = []string{"refs/heads/" + args[0]}
		} else {
			op.Parameters["source"] = args[0]
			op.Target.Refs = rrSet(rrNames(args, "refs/heads/"), false)
		}
	case "ref.branch.delete":
		op.Scopes = append(op.Scopes, model.ScopeConfig, model.ScopeReflog)
		if p.Has("delete-merged") {
			op.Target.Refs = model.UnknownSet()
			op.Parameters["deleteMerged"] = rrOptionValues(p, "delete-merged")
			op.Parameters["dryRun"] = p.Has("dry-run")
			if p.Has("dry-run") {
				op.Effect = model.EffectRead
			}
			rrUnresolved(&op, "target.refs", "merged branches and upstream patterns require repository metadata", "git")
		} else {
			if len(args) == 0 {
				return rrInvalid(op, "branch name required")
			}
			op.Target.Refs = rrSet(rrNames(args, prefix), false)
		}
	default:
		if len(args) == 0 || len(args) > 2 {
			return rrInvalid(op, "branch and optional start point expected")
		}
		op.Target.Refs = model.Exact("refs/heads/" + args[0])
		op.Target.Revisions = model.Exact("HEAD")
		if len(args) == 2 {
			op.Target.Revisions = model.Exact(args[1])
		}
		op.Scopes = append(op.Scopes, model.ScopeReflog, model.ScopeConfig)
		if p.Has("recurse-submodules") {
			op.Scopes = append(op.Scopes, model.ScopeSubmodule)
		}
	}
	return []model.Operation{op}
}

func refsTag(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	action := "ref.tag.create"
	switch {
	case p.Has("delete"):
		action = "ref.tag.delete"
	case p.Has("verify"):
		action = "ref.tag.verify"
	case p.Has("list") || len(args) == 0 || rrFilters(p) || p.Has("n"):
		action = "ref.tag.list"
	}
	op := rrOperation(action, model.ScopeLocalRef)
	op.Parameters["force"] = p.Has("force")
	if action == "ref.tag.list" {
		if len(args) == 0 {
			args = []string{"*"}
		}
		op.Target.Refs = rrSet(rrNames(args, "refs/tags/"), true)
		return []model.Operation{op}
	}
	if len(args) == 0 {
		return rrInvalid(op, "tag name required")
	}
	if action != "ref.tag.create" {
		op.Target.Refs = rrSet(rrNames(args, "refs/tags/"), false)
		if action == "ref.tag.verify" {
			op.Capabilities = []model.Capability{model.CapabilityExternalExec}
		} else {
			op.Scopes = append(op.Scopes, model.ScopeReflog)
		}
		return []model.Operation{op}
	}
	op.Scopes = append(op.Scopes, model.ScopeReflog)
	if len(args) > 2 {
		return rrInvalid(op, "tag and optional object expected")
	}
	op.Target.Refs = model.Exact("refs/tags/" + args[0])
	op.Target.Revisions = model.Exact("HEAD")
	if len(args) > 1 {
		op.Target.Revisions = model.Exact(args[1])
	}
	annotated := p.Has("annotate") || p.Has("sign") || p.Has("local-user") || p.Has("message") || p.Has("file") || p.Has("edit") || p.Has("trailer")
	op.Parameters["annotated"], op.Parameters["signed"] = annotated, p.Has("sign") || p.Has("local-user")
	if annotated {
		op.Scopes = append(op.Scopes, model.ScopeObjects)
		rrTagMessage(c, &op)
	}
	if p.Has("sign") || p.Has("local-user") {
		op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
	}
	if _, explicit := p.Option("sign"); !explicit && !p.Has("local-user") {
		rrUnresolved(&op, "parameters.signed", "tag.gpgSign and tag.forceSignAnnotated require configuration", "git")
	}
	return []model.Operation{op}
}

func rrTagMessage(c *Context, op *model.Operation) {
	p := c.Parsed
	text := &model.Text{}
	var spans []model.ArgSpan
	kind, flag := "argv", "message"
	if p.Has("edit") || (!p.Has("message") && !p.Has("file")) {
		text.Source = "editor"
		op.Capabilities = append(op.Capabilities, model.CapabilityEditor)
		rrUnresolved(op, "texts.body", "tag message will be edited", "editor")
	} else if p.Has("message") {
		text.Source, text.Value = "inline", strings.Join(rrOptionValues(p, "message"), "\n\n")
		for _, o := range p.Options {
			if o.Name == "message" {
				spans = append(spans, o.Span)
			}
		}
	} else if o, ok := p.Option("file"); ok {
		flag, spans = "file", []model.ArgSpan{o.Span}
		text.Source, text.Path, kind = "file", o.Value, "file"
		var data []byte
		var err error
		if o.Value == "-" {
			text.Source, kind = "stdin", "stdin"
			if c.Invocation.Stdin.Status == model.StdinCaptured {
				data = c.Invocation.Stdin.Data
			} else {
				text.Status = c.Invocation.Stdin.Status
				if text.Status == "" {
					text.Status = model.StdinNotCaptured
				}
				text.Truncated = text.Status == model.StdinTooLarge
				err = fmt.Errorf("stdin message not captured")
			}
		} else if c.Invocation.ReadFile != nil {
			data, err = c.Invocation.ReadFile(o.Value)
		} else {
			err = fmt.Errorf("file reader unavailable")
		}
		if err != nil {
			rrUnresolved(op, "texts.body", err.Error(), text.Source)
		} else {
			text.Value = string(data)
		}
	}
	text.Bytes = int64(len(text.Value))
	op.Texts = &model.Texts{Body: text}
	if len(spans) > 0 && len(op.Unresolved) == 0 && strings.Join(c.Argv, "\x00") == strings.Join(c.Invocation.Argv, "\x00") {
		if op.Editable == nil {
			op.Editable = map[string]model.EditLocator{}
		}
		op.Editable["texts/body/value"] = model.EditLocator{Kind: kind, Flag: flag, ValueType: "string", Text: "body", Spans: spans}
	}
}

func refsSymbolic(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	action := "ref.symbolic.read"
	if p.Has("delete") {
		action = "ref.symbolic.delete"
	} else if len(args) == 2 {
		action = "ref.symbolic.set"
	}
	op := rrOperation(action, model.ScopeLocalRef)
	if len(args) < 1 || len(args) > 2 || (action != "ref.symbolic.set" && len(args) != 1) {
		return rrInvalid(op, "symbolic-ref expects one name, or name and target")
	}
	op.Target.Refs = model.Exact(args[0])
	if len(args) == 2 {
		op.Parameters["newTarget"] = args[1]
	}
	if op.Effect == model.EffectWrite {
		op.Scopes = append(op.Scopes, model.ScopeReflog)
	}
	return []model.Operation{op}
}

func refsPack(c *Context) []model.Operation {
	op := rrOperation("ref.pack", model.ScopeLocalRef)
	op.Target.Refs = model.UnknownSet()
	op.Parameters["all"], op.Parameters["auto"] = c.Parsed.Has("all"), c.Parsed.Has("auto")
	op.Parameters["prune"] = true
	if o, ok := c.Parsed.Option("prune"); ok {
		op.Parameters["prune"] = !o.Negated
	}
	op.Parameters["include"], op.Parameters["exclude"] = rrOptionValues(c.Parsed, "include"), rrOptionValues(c.Parsed, "exclude")
	rrUnresolved(&op, "target.refs", "packed and loose reference storage requires local metadata", "git")
	return []model.Operation{op}
}

func refsReflog(c *Context) []model.Operation {
	mode := "show"
	if len(c.Parsed.Path) > 1 {
		mode = c.Parsed.Path[1]
	}
	op := rrOperation("ref.reflog."+mode, model.ScopeReflog)
	op.Origin.Mode = mode
	args := c.Parsed.Values()
	if mode == "show" && len(args) == 0 {
		args = []string{"HEAD"}
	}
	if mode == "write" && len(args) > 0 {
		args = args[:1]
	}
	refs := append([]string(nil), args...)
	for i, ref := range refs {
		refs[i], _, _ = strings.Cut(ref, "@{")
	}
	op.Target.Refs = rrSet(refs, false)
	if c.Parsed.Has("all") || mode == "list" {
		op.Target.Refs = rrSet([]string{"refs/*", "HEAD"}, true)
	}
	for _, ref := range refs {
		if ref != "HEAD" && !strings.HasPrefix(ref, "refs/") {
			op.Target.Refs.Complete = false
			rrUnresolved(&op, "target.refs", "reflog names require local ref resolution", "git")
			break
		}
	}
	op.Parameters["dryRun"] = c.Parsed.Has("dry-run")
	if c.Parsed.Has("dry-run") {
		op.Effect = model.EffectRead
	}
	if c.Parsed.Has("updateref") {
		op.Scopes = append(op.Scopes, model.ScopeLocalRef)
	}
	for _, name := range []string{"rewrite", "updateref", "all", "single-worktree", "expire", "expire-unreachable", "stale-fix"} {
		rrParameter(&op, c.Parsed, name)
	}
	return []model.Operation{op}
}

func refsReplace(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	mode := "create"
	switch {
	case p.Has("delete"):
		mode = "delete"
	case p.Has("edit"):
		mode = "edit"
	case p.Has("graft"):
		mode = "graft"
	case p.Has("convert-graft-file"):
		mode = "convert"
	case p.Has("list") || len(args) == 0:
		mode = "list"
	}
	op := rrOperation("ref.replace."+mode, model.ScopeLocalRef)
	op.Parameters["force"] = p.Has("force")
	op.Target.Refs = model.UnknownSet()
	switch mode {
	case "list":
		if len(args) == 0 {
			args = []string{"*"}
		}
		op.Target.Refs = rrSet(rrNames(args, "refs/replace/"), true)
	case "convert":
		op.Scopes = append(op.Scopes, model.ScopeObjects, model.ScopeFileOutput)
		op.Capabilities = append(op.Capabilities, model.CapabilityFileWrite)
		rrUnresolved(&op, "target.refs", "grafts file requires local input", "git")
	default:
		if len(args) == 0 {
			return rrInvalid(op, "object required")
		}
		op.Target.Revisions = rrSet(args, false)
		rrUnresolved(&op, "target.refs", "replacement reference name requires object ID resolution", "git")
		if mode == "edit" || mode == "graft" {
			op.Scopes = append(op.Scopes, model.ScopeObjects)
		}
		if mode == "edit" {
			op.Capabilities = append(op.Capabilities, model.CapabilityEditor)
		}
	}
	return []model.Operation{op}
}

func refsList(c *Context) []model.Operation {
	op := rrOperation("ref.list", model.ScopeLocalRef)
	args := c.Parsed.Values()
	if c.Parsed.Has("verify") || c.Parsed.Has("exists") {
		op.Target.Refs = rrSet(args, false)
	} else {
		if len(args) == 0 {
			args = []string{"refs/*"}
		}
		op.Target.Refs = rrSet(args, true)
	}
	if c.Parsed.Has("stdin") || c.Parsed.Has("exclude-existing") {
		op.Target.Refs = model.UnknownSet()
		rrUnresolved(&op, "target.refs", "reference filter is supplied on stdin", "stdin")
	}
	return []model.Operation{op}
}

func refsUpdate(c *Context) []model.Operation {
	if c.Parsed.Has("stdin") {
		return refsUpdateStdin(c)
	}
	args := c.Parsed.Values()
	op := rrOperation("ref.update", model.ScopeLocalRef, model.ScopeReflog)
	deleting := c.Parsed.Has("d")
	if len(args) < 1 || deleting && len(args) > 2 || !deleting && (len(args) < 2 || len(args) > 3) {
		return rrInvalid(op, "ref and new object (or -d ref) expected")
	}
	if deleting || rrZeroOID(args[1]) {
		actions.Apply(&op, "ref.delete")
	} else {
		op.Parameters["newOid"] = args[1]
	}
	op.Target.Refs = model.Exact(args[0])
	oldIndex := 2
	if deleting {
		oldIndex = 1
	}
	if len(args) > oldIndex {
		op.Parameters["oldOid"] = args[oldIndex]
	}
	rrUpdateDeref(&op, c.Parsed.Has("no-deref"))
	return []model.Operation{op}
}

func rrZeroOID(value string) bool {
	return (len(value) == 40 || len(value) == 64) && strings.Trim(value, "0") == ""
}

func rrUpdateDeref(op *model.Operation, noDeref bool) {
	op.Parameters["noDeref"] = noDeref
	if !noDeref {
		op.Target.Refs.Complete = false
		rrUnresolved(op, "target.refs", "update-ref follows symbolic references unless --no-deref", "git")
	}
}

func refsUpdateStdin(c *Context) []model.Operation {
	base := rrOperation("ref.update", model.ScopeLocalRef, model.ScopeReflog)
	base.Target.Refs = model.UnknownSet()
	if c.Invocation.Stdin.Status != model.StdinCaptured {
		base.Effect, base.Coverage, base.UnknownReason = model.EffectUnknown, model.CoverageOpaque, model.UnknownInputUnavailable
		if c.Invocation.Stdin.Status == model.StdinTooLarge {
			base.UnknownReason = model.UnknownInputTooLarge
		}
		rrUnresolved(&base, "input", "finite update-ref transaction input was not captured", "stdin")
		return []model.Operation{base}
	}
	data := string(c.Invocation.Stdin.Data)
	var ops []model.Operation
	pending := []int{}
	state := "open"
	noDeref, nextNoDeref := c.Parsed.Has("no-deref"), false
	fail := func(err error) []model.Operation {
		return append(ops, rrInvalid(base, err.Error())...)
	}
	skip := func() {
		for _, i := range pending {
			ops[i].Execution, ops[i].Evidence = model.ExecutionSkipped, "transaction aborted without committing its reference changes"
		}
		pending = nil
	}
	for len(data) > 0 {
		fields, rest, err := rrUpdateFields(data, c.Parsed.Has("z"))
		if err != nil {
			return fail(err)
		}
		data = rest
		cmd := fields[0]
		if state == "closed" && cmd != "start" || state == "prepared" && cmd != "commit" && cmd != "abort" {
			return fail(fmt.Errorf("invalid command %s in %s transaction", cmd, state))
		}
		if cmd == "start" || cmd == "prepare" || cmd == "commit" || cmd == "abort" || cmd == "option" {
			op := rrOperation("ref.transaction."+cmd, model.ScopeLocalRef)
			op.Parameters["command"] = cmd
			switch cmd {
			case "start":
				if state == "started" {
					return fail(fmt.Errorf("transaction already started"))
				}
				state = "started"
			case "prepare":
				state = "prepared"
			case "commit":
				state, pending = "closed", nil
			case "abort":
				skip()
				state = "closed"
			case "option":
				if fields[1] != "no-deref" {
					return fail(fmt.Errorf("unsupported update-ref option %q", fields[1]))
				}
				nextNoDeref = true
			}
			ops = append(ops, op)
			continue
		}
		action := "ref.update"
		switch cmd {
		case "delete":
			action = "ref.delete"
		case "verify":
			action = "ref.verify"
		case "symref-update", "symref-create":
			action = "ref.symbolic.set"
		case "symref-delete":
			action = "ref.symbolic.delete"
		case "symref-verify":
			action = "ref.symbolic.verify"
		case "update":
			if fields[2] == "" || rrZeroOID(fields[2]) {
				action = "ref.delete"
			}
		}
		op := rrOperation(action, model.ScopeLocalRef)
		op.Target.Refs = model.Exact(fields[1])
		op.Parameters["command"] = cmd
		op.Parameters["arguments"] = fields[2:]
		if (cmd == "symref-delete" || cmd == "symref-verify") && !noDeref && !nextNoDeref {
			return fail(fmt.Errorf("%s requires no-deref mode", cmd))
		}
		if cmd == "symref-update" || cmd == "symref-create" {
			op.Parameters["newTarget"] = fields[2]
		}
		rrUpdateDeref(&op, noDeref || nextNoDeref)
		nextNoDeref = false
		if op.Effect == model.EffectWrite {
			op.Scopes = append(op.Scopes, model.ScopeReflog)
		}
		pending = append(pending, len(ops))
		ops = append(ops, op)
	}
	if state == "started" || state == "prepared" {
		skip()
	}
	if len(ops) == 0 {
		base.Effect, base.Action = model.EffectRead, "ref.verify"
		return []model.Operation{base}
	}
	return ops
}

func rrUpdateFields(data string, nul bool) ([]string, string, error) {
	counts := map[string][2]int{
		"update": {2, 3}, "create": {2, 2}, "delete": {1, 2}, "verify": {1, 2},
		"symref-update": {2, 4}, "symref-create": {2, 2}, "symref-delete": {1, 2}, "symref-verify": {1, 2},
		"option": {1, 1}, "start": {0, 0}, "prepare": {0, 0}, "commit": {0, 0}, "abort": {0, 0},
	}
	term := "\n"
	if nul {
		term = "\x00"
	}
	line, rest, found := strings.Cut(data, term)
	if !found && nul {
		return nil, "", fmt.Errorf("unterminated NUL command")
	}
	var fields []string
	var err error
	if nul {
		cmd, ref, hasRef := strings.Cut(line, " ")
		count, ok := counts[cmd]
		if !ok || count[1] > 0 && !hasRef || count[1] == 0 && hasRef {
			return nil, "", fmt.Errorf("invalid update-ref command %q", line)
		}
		fields = []string{cmd}
		if count[1] > 0 {
			fields = append(fields, ref)
			for i := 1; i < count[1]; i++ {
				if rest == "" && len(fields)-1 >= count[0] {
					break
				}
				value, next, ok := strings.Cut(rest, term)
				if !ok {
					return nil, "", fmt.Errorf("missing NUL argument for %s", cmd)
				}
				fields, rest = append(fields, value), next
			}
		}
	} else {
		fields, err = rrQuotedFields(line)
		if err != nil {
			return nil, "", err
		}
	}
	if len(fields) == 0 {
		return nil, "", fmt.Errorf("empty update-ref command")
	}
	count, ok := counts[fields[0]]
	if !ok || len(fields)-1 < count[0] || len(fields)-1 > count[1] || count[0] > 0 && fields[1] == "" {
		return nil, "", fmt.Errorf("invalid update-ref arguments: %q", fields)
	}
	if fields[0] == "symref-update" && len(fields) > 3 && fields[3] != "" && (len(fields) != 5 || fields[3] != "ref" && fields[3] != "oid") {
		return nil, "", fmt.Errorf("invalid symref-update old target")
	}
	return fields, rest, nil
}

func rrQuotedFields(line string) ([]string, error) {
	var out []string
	for len(line) > 0 {
		if line[0] == ' ' || line[0] == '\t' {
			return nil, fmt.Errorf("unexpected whitespace in update-ref input")
		}
		var word string
		if line[0] == '"' {
			end := 1
			for ; end < len(line); end++ {
				if line[end] == '\\' {
					end++
				} else if line[end] == '"' {
					break
				}
			}
			if end >= len(line) {
				return nil, fmt.Errorf("unterminated quoted update-ref argument")
			}
			var err error
			word, err = strconv.Unquote(line[:end+1])
			if err != nil {
				return nil, err
			}
			line = line[end+1:]
		} else {
			end := strings.IndexByte(line, ' ')
			if end < 0 {
				end = len(line)
			}
			word, line = line[:end], line[end:]
		}
		out = append(out, word)
		if len(line) > 0 {
			if line[0] != ' ' {
				return nil, fmt.Errorf("missing update-ref argument separator")
			}
			line = line[1:]
			if line == "" {
				return nil, fmt.Errorf("trailing update-ref whitespace")
			}
		}
	}
	return out, nil
}
