package policy

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

func operation(action, repo string, effect model.Effect) model.Operation {
	return model.Operation{Family: "remote.push", Action: action, Effect: effect, Execution: model.ExecutionWillRun, Target: model.Target{RepoRoot: repo}}
}
func subject(ops ...model.Operation) Subject {
	return Subject{Result: model.Result{Classification: model.Classification{Level: model.LevelL1}, Operations: ops}, Argv: []string{"push"}, Phase: "pre"}
}

func TestOutcomeMatches(t *testing.T) {
	ok := &protocol.ExecResult{ExitCode: 0}
	failed := &protocol.ExecResult{ExitCode: 2}
	signaled := &protocol.ExecResult{ExitCode: -1, Signal: "SIGINT"}
	notStarted := &protocol.ExecResult{ExitCode: 127, StartError: "not found"}
	for _, tc := range []struct {
		m    config.Match
		o    *protocol.ExecResult
		want bool
	}{
		{config.Match{}, failed, true},
		{config.Match{Results: config.StringList{"failure"}}, nil, true}, // unknown yet: candidate
		{config.Match{Results: config.StringList{"failure"}}, ok, false},
		{config.Match{Results: config.StringList{"failure"}}, failed, true},
		{config.Match{Results: config.StringList{"failure"}}, signaled, true},
		{config.Match{Results: config.StringList{"failure"}}, notStarted, true},
		{config.Match{Results: config.StringList{"success"}}, ok, true},
		{config.Match{Results: config.StringList{"success", "failure"}}, failed, true},
		{config.Match{ExitCodes: []int{2, 3}}, failed, true},
		{config.Match{ExitCodes: []int{0}}, failed, false},
		{config.Match{ExitCodes: []int{127}}, notStarted, false}, // start failure is not an exit code
		{config.Match{Results: config.StringList{"failure"}, ExitCodes: []int{3}}, failed, false},
	} {
		if got := outcomeMatches(tc.m, tc.o); got != tc.want {
			t.Errorf("match=%+v outcome=%+v: got %v", tc.m, tc.o, got)
		}
	}
}

func TestDenialProvenance(t *testing.T) {
	op := operation("unknown", "/repo", model.EffectUnknown)
	p := config.Policy{
		Mode: config.ModeEnforce, UnknownMutation: config.UnknownDeny,
		Rules: []config.Rule{
			{ID: "z-first", Decision: "deny", Reason: "first"},
			{ID: "a-second", Decision: "deny", Reason: "second"},
		},
	}
	for _, mode := range []string{config.ModeEnforce, config.ModeObserve} {
		p.Mode = mode
		got := Evaluate(p, subject(op))
		want := []Denial{
			{Kind: DenialRule, ID: "z-first", Reason: "first (unknown)"},
			{Kind: DenialRule, ID: "a-second", Reason: "second (unknown)"},
			{Kind: DenialPolicy, ID: "unknownMutation", Reason: "unclassified-write: unknownMutation policy (unknown)"},
		}
		if !reflect.DeepEqual(got.Denials, want) {
			t.Fatalf("%s denials=%+v, want=%+v", mode, got.Denials, want)
		}
		body, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != 7 || fields["denials"] != nil || fields["Denials"] != nil {
			t.Fatalf("provenance changed serialized policy contract: %s", body)
		}
	}
}
