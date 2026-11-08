package policy

import (
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestUnknownInputIndependentOfUnknownMutation(t *testing.T) {
	for _, reason := range []model.UnknownReason{model.UnknownInputUnavailable, model.UnknownInputTooLarge, model.UnknownCommand, model.UnknownParseError} {
		for _, tc := range []struct {
			input, mutation                 string
			inputDecision, mutationDecision protocol.Decision
		}{
			{"allow", "deny", protocol.DecisionAllow, protocol.DecisionDeny},
			{"deny", "allow", protocol.DecisionDeny, protocol.DecisionAllow},
			{"", "deny", protocol.DecisionAudit, protocol.DecisionDeny},
		} {
			t.Run(string(reason)+"/"+tc.input+"/"+tc.mutation, func(t *testing.T) {
				op := model.NewUnknown(model.LevelL1, reason, "stdin not captured (timeout)").Operations[0]
				got := Evaluate(config.Policy{Mode: config.ModeEnforce, UnknownInput: tc.input, UnknownMutation: tc.mutation}, subject(op))
				want := tc.mutationDecision
				if reason == model.UnknownInputUnavailable || reason == model.UnknownInputTooLarge {
					want = tc.inputDecision
				}
				if got.Decision != want {
					t.Fatalf("decision=%s, want=%s: %+v", got.Decision, want, got)
				}
				if want == protocol.DecisionDeny && reason == model.UnknownInputUnavailable && (len(got.DenyReasons) != 1 || !strings.Contains(got.DenyReasons[0], "timeout")) {
					t.Fatalf("missing denial cause: %+v", got)
				}
			})
		}
	}
}

func TestWarnRuleOrderAndPrecedence(t *testing.T) {
	for _, decision := range []string{"", "audit", "deny"} {
		p := config.Policy{Mode: config.ModeEnforce, Rules: []config.Rule{
			{ID: "first", Decision: "warn", Reason: "first warning"},
			{ID: "second", Decision: "warn", Reason: "second warning"},
		}}
		if decision != "" {
			p.Rules = append(p.Rules, config.Rule{ID: "blocking", Decision: decision, Reason: "existing decision"})
		}
		s := subject(operation("pr.merge.direct", "org/repo", model.EffectWrite))
		s.Result.Operations = append(s.Result.Operations, s.Result.Operations[0])
		got := Evaluate(p, s)
		want := protocol.DecisionAllow
		if decision != "" {
			want = protocol.Decision(decision)
		}
		if got.Decision != want || len(got.Warnings) != 2 || got.Warnings[0] != (protocol.Warning{Source: "rule", ID: "first", Text: "first warning"}) || got.Warnings[1].ID != "second" {
			t.Fatalf("%s: %+v", decision, got)
		}
	}
}
