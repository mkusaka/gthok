package transforms

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestEnvironmentApplyAndAtomicFailure(t *testing.T) {
	inv := model.Invocation{Argv: []string{"commit", "-m", "original"}, Env: map[string]string{"KEEP": "original", "REMOVE": "old"}}
	before := normalize.Normalize(inv)
	changed := "changed"
	next, _, cleanup, err := Apply(inv, before, protocol.TransformResponse{SchemaVersion: 1, Env: map[string]*string{"KEEP": &changed, "REMOVE": nil}})
	defer cleanup()
	if err != nil || next.Env["KEEP"] != changed || next.Env["REMOVE"] != "" || inv.Env["KEEP"] != "original" {
		t.Fatalf("env apply: %+v %v", next, err)
	}
	for _, response := range []protocol.TransformResponse{
		{SchemaVersion: 1, Env: map[string]*string{"GIT_DIR": &changed}},
		{SchemaVersion: 1, Env: map[string]*string{"KEEP": &changed}, Set: map[string]any{"/operations/0/parameters/unsupported": "value"}},
	} {
		next, after, cleanup, err := Apply(inv, before, response)
		cleanup()
		if err == nil || !reflect.DeepEqual(next, inv) || !reflect.DeepEqual(after, before) {
			t.Fatalf("failure was not atomic: %+v %v", next, err)
		}
	}
}

func TestUnsupportedAndIncompleteTextSources(t *testing.T) {
	inv := model.Invocation{Argv: []string{"commit", "-m", "original"}}
	for _, truncated := range []bool{false, true} {
		before := normalize.Normalize(inv)
		before.Operations[0].Texts = &model.Texts{Body: &model.Text{Value: "original", Truncated: truncated}}
		before.Operations[0].Editable = map[string]model.EditLocator{"texts/body/value": {Kind: "api-field", Text: "body"}}
		_, _, cleanup, err := Apply(inv, before, protocol.TransformResponse{SchemaVersion: 1, Set: map[string]any{"/operations/0/texts/body/value": "new"}})
		cleanup()
		if !errors.Is(err, ErrUnsupportedEdit) {
			t.Fatalf("unsupported/truncated edit accepted: %v", err)
		}
		if truncated && !strings.Contains(err.Error(), "incomplete") {
			t.Fatal("truncated text was not rejected before write-back")
		}
	}
}

func TestRoundTripRejectsUneditedGitSemantics(t *testing.T) {
	before := normalize.Normalize(model.Invocation{Argv: []string{"push", "origin", "main"}})
	for _, mutate := range []func(*model.Result){
		func(r *model.Result) { r.Operations[0].Action = "ref.delete" },
		func(r *model.Result) { r.Operations[0].Effect = model.EffectRead },
		func(r *model.Result) { r.Operations[0].Scopes = []model.Scope{model.ScopeLocalRef} },
		func(r *model.Result) { r.Operations[0].Capabilities = []model.Capability{model.CapabilityExternalExec} },
		func(r *model.Result) { r.Operations[0].Target.Remote = "other" },
		func(r *model.Result) { r.Classification.RetrySafe = true },
		func(r *model.Result) { r.Operations = append(r.Operations, r.Operations[0]) },
	} {
		after := before
		after.Operations = append([]model.Operation(nil), before.Operations...)
		mutate(&after)
		if err := roundTrip(before, after, nil); !errors.Is(err, ErrUnsupportedEdit) {
			t.Fatal("changed unedited semantics accepted")
		}
	}
}

func TestRewriteKeepsArgumentBoundaries(t *testing.T) {
	argv := []string{"commit", "-m", "old", "--", "file"}
	got, err := rewrite(argv, []replacement{{span: model.ArgSpan{Start: 1, End: 3}}}, []string{"-m", "new"})
	if err != nil || !reflect.DeepEqual(got, []string{"commit", "-m", "new", "--", "file"}) {
		t.Fatalf("rewrite: %q %v", got, err)
	}
	if _, err := rewrite(argv, []replacement{{span: model.ArgSpan{Start: 1, End: 4}}, {span: model.ArgSpan{Start: 2, End: 3}}}, nil); !errors.Is(err, ErrUnsupportedEdit) {
		t.Fatal("overlapping locators accepted")
	}
}
