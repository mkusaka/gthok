// Package transforms applies restricted semantic edits to their original input
// transports and refuses any rewrite that does not round-trip through analysis.
package transforms

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize"
	"github.com/mkusaka/gthok/internal/protocol"
	"github.com/mkusaka/gthok/internal/registry"
)

var ErrUnsupportedEdit = errors.New("unsupported edit")
var ErrInvalidResponse = errors.New("invalid transform response")

type edit struct {
	path  string
	op    int
	parts []string
	value any
	unset bool
	loc   model.EditLocator
}

type replacement struct {
	span model.ArgSpan
	args []string
}

// Apply is atomic: failure returns the original invocation and result. Cleanup
// is always callable, including on failure and when no transport was changed.
func Apply(inv model.Invocation, before model.Result, response protocol.TransformResponse) (model.Invocation, model.Result, func(), error) {
	cleanup := func() {}
	fail := func(err error) (model.Invocation, model.Result, func(), error) {
		cleanup()
		return inv, before, func() {}, err
	}
	if err := protocol.ValidateTransformEnv(response.Env); err != nil {
		return fail(fmt.Errorf("%w: %v", ErrInvalidResponse, err))
	}
	edits, err := parseEdits(before, response)
	if err != nil {
		return fail(err)
	}
	envChanged := false
	for name, value := range response.Env {
		old, present := inv.Env[name]
		if (value == nil && present) || (value != nil && (!present || old != *value)) {
			envChanged = true
			break
		}
	}
	if len(edits) == 0 && !envChanged {
		return inv, before, cleanup, nil
	}
	next := inv
	if envChanged {
		next.Env = make(map[string]string, len(inv.Env)+len(response.Env))
		for name, value := range inv.Env {
			next.Env[name] = value
		}
		for name, value := range response.Env {
			if value == nil {
				delete(next.Env, name)
			} else {
				next.Env[name] = *value
			}
		}
	}
	if len(edits) == 0 {
		return next, normalize.Normalize(next), cleanup, nil
	}
	next.Argv = append([]string(nil), before.Source.Argv...)
	if next.Argv == nil {
		next.Argv = append([]string(nil), inv.Argv...)
	}
	next.TemporaryFiles = append([]string(nil), inv.TemporaryFiles...)
	var temporary []string
	files := make(map[string][]byte)
	cleanup = func() {
		for _, path := range temporary {
			_ = os.Remove(path)
		}
	}
	makeFile := func(data []byte) (string, error) {
		f, err := os.CreateTemp("", "gthok-transform-*")
		if err != nil {
			return "", err
		}
		path := f.Name()
		temporary = append(temporary, path)
		if _, err = f.Write(data); err != nil {
			_ = f.Close()
			return "", err
		}
		if err = f.Close(); err != nil {
			return "", err
		}
		files[path] = append([]byte(nil), data...)
		next.TemporaryFiles = append(next.TemporaryFiles, path)
		return path, nil
	}
	var replacements []replacement
	var appended []string
	resources := make(map[string]edit)
	for _, e := range edits {
		resource := locatorResource(e.loc)
		if previous, exists := resources[resource]; exists {
			if previous.unset != e.unset || !equal(previous.value, e.value) {
				return fail(fmt.Errorf("%w: conflicting edits to the same input", ErrUnsupportedEdit))
			}
			continue
		}
		resources[resource] = e
		loc := e.loc
		var args []string
		switch loc.Kind {
		case "argv", "file", "stdin":
			if !e.unset {
				switch loc.Kind {
				case "file":
					path, err := makeFile([]byte(e.value.(string)))
					if err != nil {
						return fail(fmt.Errorf("%w: %v", ErrUnsupportedEdit, err))
					}
					args = []string{"--" + loc.Flag, path}
				case "stdin":
					next.Stdin = model.StdinData{Status: model.StdinCaptured, Data: []byte(e.value.(string))}
					args = []string{"--" + loc.Flag, "-"}
				default:
					args, err = nativeArgs(loc, e.value, before.Operations[e.op].Origin.Command)
					if err != nil {
						return fail(err)
					}
				}
			}
			for i, span := range loc.Spans {
				r := replacement{span: span}
				if i == 0 {
					r.args = args
				}
				replacements = append(replacements, r)
			}
			if len(loc.Spans) == 0 {
				appended = append(appended, args...)
			}
		case "positional":
			if len(loc.Spans) > 1 {
				return fail(fmt.Errorf("%w: positional selector has no unique source", ErrUnsupportedEdit))
			}
			if !e.unset {
				args = []string{scalarString(e.value)}
			}
			if len(loc.Spans) == 0 {
				appended = append(appended, args...)
			} else {
				replacements = append(replacements, replacement{span: loc.Spans[0], args: args})
			}
		default:
			return fail(fmt.Errorf("%w: missing write-back transport", ErrUnsupportedEdit))
		}
	}
	next.Argv, err = rewrite(next.Argv, replacements, appended)
	if err != nil {
		return fail(err)
	}
	if len(files) > 0 {
		originalReader := inv.ReadFile
		next.ReadFile = func(path string) ([]byte, error) {
			if data, ok := files[path]; ok {
				return append([]byte(nil), data...), nil
			}
			if originalReader == nil {
				return nil, errors.New("file reader unavailable")
			}
			return originalReader(path)
		}
	}
	after := normalize.Normalize(next)
	if err := roundTrip(before, after, edits); err != nil {
		return fail(err)
	}
	return next, after, cleanup, nil
}

func parseEdits(before model.Result, response protocol.TransformResponse) ([]edit, error) {
	if response.SchemaVersion != 1 {
		return nil, fmt.Errorf("%w: schemaVersion must be 1", ErrInvalidResponse)
	}
	paths := make([]string, 0, len(response.Set)+len(response.Unset))
	unset := make(map[string]bool)
	for path := range response.Set {
		paths = append(paths, path)
	}
	for _, path := range response.Unset {
		if unset[path] {
			return nil, fmt.Errorf("%w: duplicate unset path", ErrInvalidResponse)
		}
		if _, exists := response.Set[path]; exists {
			return nil, fmt.Errorf("%w: set and unset overlap", ErrInvalidResponse)
		}
		unset[path] = true
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var edits []edit
	requested := make(map[string]edit)
	for _, path := range paths {
		parts, index, err := parsePath(path)
		if err != nil {
			return nil, err
		}
		if index >= len(before.Operations) {
			return nil, fmt.Errorf("%w: operation index out of range", ErrInvalidResponse)
		}
		op := before.Operations[index]
		key := strings.Join(parts, "/")
		if parts[0] == "parameters" {
			key = "parameters/" + strings.ReplaceAll(strings.ReplaceAll(parts[1], "~", "~0"), "/", "~1")
		}
		loc, exists := op.Editable[key]
		if !exists && parts[0] == "parameters" {
			loc, exists = op.Editable["parameters/*"]
			if exists {
				loc.Key = append([]string(nil), loc.Key...)
				if len(loc.Key) > 0 && loc.Key[len(loc.Key)-1] == "*" {
					loc.Key[len(loc.Key)-1] = parts[1]
				}
				loc.Parameter = parts[1]
			}
		}
		if parts[0] == "texts" {
			if source := text(op.Texts, parts[1]); source != nil && (source.Truncated || source.Status != "") {
				return nil, fmt.Errorf("%w: text input is incomplete", ErrUnsupportedEdit)
			}
		}
		if !exists || !supportedLocator(op, loc, parts) || before.Source.Alias != "" {
			return nil, fmt.Errorf("%w: %s has no editable source", ErrUnsupportedEdit, path)
		}
		for i, other := range before.Operations {
			if i == index {
				continue
			}
			for _, otherLoc := range other.Editable {
				if locatorResource(loc) == locatorResource(otherLoc) {
					return nil, fmt.Errorf("%w: shared input source", ErrUnsupportedEdit)
				}
			}
		}
		e := edit{path: path, op: index, parts: parts, value: response.Set[path], unset: unset[path], loc: loc}
		if parts[0] == "texts" && !e.unset {
			if value, ok := e.value.(string); !ok || strings.ContainsRune(value, '\x00') {
				return nil, fmt.Errorf("%w: text value must be a string without NUL", ErrUnsupportedEdit)
			}
		}
		if parts[0] == "target" && !e.unset {
			value, ok := e.value.(string)
			if !ok || value == "" || strings.ContainsAny(value, "\x00\r\n") {
				return nil, fmt.Errorf("%w: target must be a nonempty string", ErrUnsupportedEdit)
			}
		}
		if loc.Kind == "positional" && !e.unset {
			value, ok := e.value.(string)
			if !ok || value == "" || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "\x00\r\n") {
				return nil, fmt.Errorf("%w: invalid positional value", ErrUnsupportedEdit)
			}
		}
		resource := locatorResource(loc)
		if previous, exists := requested[resource]; exists && (previous.unset != e.unset || !equal(previous.value, e.value)) {
			return nil, fmt.Errorf("%w: conflicting edits to the same input", ErrUnsupportedEdit)
		}
		requested[resource] = e
		old, present := semanticValue(op, parts)
		if (e.unset && !present) || (!e.unset && present && equal(old, e.value)) {
			continue
		}
		edits = append(edits, e)
	}
	return edits, nil
}

func parsePath(path string) ([]string, int, error) {
	if err := protocol.ValidateTransformPath(path); err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	p := strings.Split(path, "/")
	index, err := strconv.Atoi(p[2])
	if err != nil {
		return nil, 0, fmt.Errorf("%w: operation index out of range", ErrInvalidResponse)
	}
	parts := p[3:]
	if parts[0] == "parameters" {
		parts[1] = strings.ReplaceAll(strings.ReplaceAll(parts[1], "~1", "/"), "~0", "~")
	}
	return parts, index, nil
}

func semanticValue(op model.Operation, parts []string) (any, bool) {
	switch parts[0] {
	case "parameters":
		v, ok := op.Parameters[parts[1]]
		return v, ok
	case "target":
		if parts[1] == "remote" {
			return op.Target.Remote, op.Target.Remote != ""
		}
		return nil, false
	case "texts":
		t := text(op.Texts, parts[1])
		if t != nil {
			return t.Value, true
		}
	}
	return nil, false
}
func text(texts *model.Texts, key string) *model.Text {
	if texts == nil {
		return nil
	}
	switch key {
	case "title":
		return texts.Title
	case "body":
		return texts.Body
	case "notes":
		return texts.Notes
	}
	return nil
}
func equal(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	if value, ok := a.(string); ok {
		other, ok := b.(string)
		return ok && value == other
	}
	if left, ok := a.(map[string]any); ok {
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, exists := right[key]
			if !exists || !equal(value, other) {
				return false
			}
		}
		return true
	}
	if a != nil && b != nil {
		left, right := reflect.ValueOf(a), reflect.ValueOf(b)
		if left.Kind() == reflect.Slice && right.Kind() == reflect.Slice {
			if left.IsNil() != right.IsNil() || left.Len() != right.Len() {
				return false
			}
			for i := range left.Len() {
				if !equal(left.Index(i).Interface(), right.Index(i).Interface()) {
					return false
				}
			}
			return true
		}
	}
	x, e1 := json.Marshal(a)
	y, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && string(x) == string(y)
}
func scalarString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func locatorResource(loc model.EditLocator) string {
	return loc.Kind + ":" + loc.Flag + ":" + fmt.Sprint(loc.Spans)
}

func supportedLocator(op model.Operation, loc model.EditLocator, parts []string) bool {
	if len(op.Origin.Command) == 0 {
		return false
	}
	command := op.Origin.Command[0]
	switch parts[0] {
	case "texts":
		return (command == "commit" || command == "tag" || command == "merge" || command == "notes") &&
			(loc.Kind == "argv" || loc.Kind == "file" || loc.Kind == "stdin") && (loc.Flag == "message" || loc.Flag == "file")
	case "target":
		return parts[1] == "remote" && (command == "push" || command == "fetch" || command == "pull")
	case "parameters":
		return loc.Kind == "positional" && len(loc.Spans) == 1 || loc.Kind == "argv" && loc.ValueType == "bool"
	}
	return false
}

func nativeArgs(loc model.EditLocator, value any, command []string) ([]string, error) {
	bad := func() ([]string, error) {
		return nil, fmt.Errorf("%w: invalid --%s value", ErrUnsupportedEdit, loc.Flag)
	}
	switch loc.ValueType {
	case "bool":
		v, ok := value.(bool)
		if !ok {
			return bad()
		}
		if len(command) == 0 {
			return bad()
		}
		grammar := registry.Commands()[command[0]].Grammar
		for _, sub := range command[1:] {
			for _, candidate := range grammar.Subcommands {
				if candidate.Name == sub {
					grammar = candidate.Grammar
					break
				}
			}
		}
		for _, option := range grammar.Options {
			if option.Long != loc.Flag || option.Arity != "none" {
				continue
			}
			flag := loc.Flag
			if !v {
				if !option.Negatable {
					return bad()
				}
				if !strings.HasPrefix(flag, "no-") {
					flag = "no-" + flag
				} else {
					flag = strings.TrimPrefix(flag, "no-")
				}
			}
			return []string{"--" + flag}, nil
		}
		return bad()
	case "stringSlice", "stringArray":
		var values []string
		switch v := value.(type) {
		case []string:
			values = v
		case []any:
			for _, entry := range v {
				s, ok := entry.(string)
				if !ok {
					return bad()
				}
				values = append(values, s)
			}
		default:
			return bad()
		}
		var args []string
		for _, v := range values {
			if loc.ValueType == "stringSlice" {
				v = csvValue(v)
			}
			args = append(args, "--"+loc.Flag, v)
		}
		if len(values) == 0 {
			return []string{"--" + loc.Flag + "="}, nil
		}
		return args, nil
	default:
		v, ok := value.(string)
		if !ok {
			return bad()
		}
		return []string{"--" + loc.Flag, v}, nil
	}
}
func csvValue(v string) string {
	if v == "" || strings.ContainsAny(v, ",\"\r\n") {
		return "\"" + strings.ReplaceAll(v, "\"", "\"\"") + "\""
	}
	return v
}
func rewrite(argv []string, replacements []replacement, appended []string) ([]string, error) {
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].span.Start < replacements[j].span.Start })
	var merged []replacement
	for _, r := range replacements {
		if len(merged) > 0 && merged[len(merged)-1].span.Start == r.span.Start && merged[len(merged)-1].span.End == r.span.End {
			previous := &merged[len(merged)-1]
			var keep []string
			var groups [][]string
			for _, a := range previous.span.KeepGroups {
				for _, b := range r.span.KeepGroups {
					if reflect.DeepEqual(a, b) {
						groups = append(groups, a)
						keep = append(keep, a...)
						break
					}
				}
			}
			previous.span.Keep = keep
			previous.span.KeepGroups = groups
			previous.args = append(previous.args, r.args...)
			continue
		}
		merged = append(merged, r)
	}
	var out []string
	at := 0
	for _, r := range merged {
		if r.span.Start < at || r.span.Start < 0 || r.span.End > len(argv) || r.span.End <= r.span.Start {
			return nil, fmt.Errorf("%w: overlapping input locators", ErrUnsupportedEdit)
		}
		out = append(out, argv[at:r.span.Start]...)
		out = append(out, r.span.Keep...)
		out = append(out, r.args...)
		at = r.span.End
	}
	out = append(out, argv[at:]...)
	for i, arg := range out {
		if arg == "--" {
			result := append([]string(nil), out[:i]...)
			result = append(result, appended...)
			return append(result, out[i:]...), nil
		}
	}
	return append(out, appended...), nil
}

type semanticProjection struct {
	Family        string
	Action        string
	Effect        model.Effect
	Execution     model.Execution
	Origin        model.Origin
	UnknownReason model.UnknownReason
	Scopes        []model.Scope
	Capabilities  []model.Capability
	Coverage      model.Coverage
	Evidence      string
	Detail        string
	Unresolved    []model.Unresolved
	Target        model.Target
	Parameters    map[string]any
	Texts         [3]projectedText
}

type projectedText struct {
	Present bool
	Value   string
}

// projection excludes edited values and their recorded alternate semantic views.
// Transport provenance is not semantic: rewriting one JSON file relocates every text.
func projection(op model.Operation, editedPaths []edit) semanticProjection {
	parameters := make(map[string]bool)
	texts := make(map[string]bool)
	targets := make(map[string]bool)
	for _, e := range editedPaths {
		switch e.parts[0] {
		case "parameters":
			parameters[e.parts[1]] = true
		case "texts":
			texts[e.parts[1]] = true
		case "target":
			targets[e.parts[1]] = true
		}
		if e.loc.Parameter != "" {
			parameters[e.loc.Parameter] = true
		}
		if e.loc.Text != "" {
			texts[e.loc.Text] = true
		}
		for _, field := range e.loc.Key {
			switch field {
			case "target.refs":
				targets["refs"] = true
			case "target.revisions":
				targets["revisions"] = true
			}
		}
	}
	p := semanticProjection{Family: op.Family, Action: op.Action, Effect: op.Effect, Execution: op.Execution, Origin: op.Origin, UnknownReason: op.UnknownReason, Scopes: op.Scopes, Capabilities: op.Capabilities, Coverage: op.Coverage, Evidence: op.Evidence, Detail: op.Detail, Unresolved: op.Unresolved, Target: op.Target, Parameters: make(map[string]any)}
	for key, value := range op.Parameters {
		if !parameters[key] {
			p.Parameters[key] = value
		}
	}
	for i, key := range []string{"title", "body", "notes"} {
		if !texts[key] {
			if value := text(op.Texts, key); value != nil {
				p.Texts[i] = projectedText{Present: true, Value: value.Value}
			}
		}
	}
	if targets["remote"] {
		p.Target.Remote, p.Target.RemoteURL, p.Target.PushURL = "", "", ""
	}
	if targets["refs"] {
		p.Target.Refs = nil
	}
	if targets["revisions"] {
		p.Target.Revisions = nil
	}
	p.Unresolved = nil
	for _, unresolved := range op.Unresolved {
		field, target := strings.CutPrefix(unresolved.Field, "target.")
		if !target || !targets[field] {
			p.Unresolved = append(p.Unresolved, unresolved)
		}
	}
	return p
}

// Validate checks the same invariants after local resolution. Only locator-
// declared alternate views and derived targets may differ.
func Validate(before, after model.Result, response protocol.TransformResponse) error {
	edits, err := parseEdits(before, response)
	if err != nil {
		return err
	}
	return roundTrip(before, after, edits)
}

func roundTrip(before, after model.Result, edits []edit) error {
	bad := func() error {
		return fmt.Errorf("%w: edited request changed an unedited semantic value", ErrUnsupportedEdit)
	}
	if !reflect.DeepEqual(before.Errors, after.Errors) || before.Registry != after.Registry {
		return bad()
	}
	if before.Classification.Level != after.Classification.Level || before.Classification.Confidence != after.Classification.Confidence || before.Classification.MutationFree != after.Classification.MutationFree || before.Classification.RetrySafe != after.Classification.RetrySafe || before.Classification.Coverage != after.Classification.Coverage || len(before.Operations) != len(after.Operations) {
		return bad()
	}
	for i, a := range before.Operations {
		b := after.Operations[i]
		var editedPaths []edit
		for _, e := range edits {
			if e.op != i {
				continue
			}
			editedPaths = append(editedPaths, e)
			value, present := semanticValue(b, e.parts)
			if (e.unset && present) || (!e.unset && (!present || !equal(value, e.value))) {
				return bad()
			}
		}
		left, right := projection(a, editedPaths), projection(b, editedPaths)
		if !equal(left.Parameters, right.Parameters) {
			return bad()
		}
		left.Parameters = nil
		right.Parameters = nil
		if !reflect.DeepEqual(left, right) {
			return bad()
		}
	}
	return nil
}
