//go:build unix

package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/hooks"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize"
	"github.com/mkusaka/gthok/internal/policy"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestIgnoredGateDecisions(t *testing.T) {
	for _, mode := range []string{config.ModeEnforce, config.ModeObserve} {
		for _, blocked := range []bool{false, true} {
			c := &config.Config{Policy: config.Policy{Mode: mode}, IgnoredHooks: []config.IgnoredHook{{Hook: config.Hook{ID: "gate", Phase: config.PhasePre, Match: config.Match{Commands: []string{"pr view"}}}, Reason: "changed since trust", GateBlocked: blocked}}}
			c.ApplyDefaults()
			result := normalize.Normalize(model.Invocation{Argv: []string{"pr", "view", "1", "-R", "acme/repo"}})
			evaluation := evaluateConfig(c, policy.Subject{Result: result, Argv: result.Source.Argv})
			got := decide(mode, evaluation, hooks.Aggregate{}, true)
			if evaluation.WouldDeny != blocked {
				t.Fatalf("mode=%s blocked=%t evaluation=%+v", mode, blocked, evaluation)
			}
			if !blocked {
				continue
			}
			if len(evaluation.Denials) != 1 || evaluation.Denials[0].ID != "gate" || string(evaluation.Denials[0].Kind) != "config" || !strings.Contains(evaluation.DenyReasons[0], "run 'gthok config trust'") {
				t.Fatalf("gate provenance=%+v", evaluation)
			}
			if mode == config.ModeObserve {
				if got.Decision != "would-deny" || got.ExitCode != 0 || evaluation.Decision != protocol.DecisionAudit {
					t.Fatalf("observe gate=%+v evaluation=%+v", got, evaluation)
				}
			} else {
				if got.Decision != "deny" || got.ExitCode != 78 {
					t.Fatalf("enforce gate=%+v", got)
				}
				content := hooks.Aggregate{Decision: protocol.DecisionDeny, Results: []protocol.HookResult{{HookID: "content", Effective: protocol.DecisionDeny, Outcome: protocol.OutcomeOK}}}
				if got := decide(mode, evaluation, content, false); got.ExitCode != 77 {
					t.Fatalf("content denial must win: %+v", got)
				}
			}
			c.IgnoredHooks[0].Match.Commands = []string{"pr merge"}
			if got := evaluateConfig(c, policy.Subject{Result: result, Argv: result.Source.Argv}); got.WouldDeny {
				t.Fatalf("nonmatching gate blocked: %+v", got)
			}
		}
	}
}

func TestIgnoredHookWarningReasonsAndDigestThrottle(t *testing.T) {
	c := &config.Config{IgnoredProject: &config.IgnoredProject{Path: "/project/.gthok.yaml", Digest: "one"}, IgnoredHooks: []config.IgnoredHook{
		{Hook: config.Hook{ID: "gate"}, Reason: "changed since trust", GateBlocked: true},
		{Hook: config.Hook{ID: "sibling"}, Reason: "untrusted"},
	}}
	dir, now := t.TempDir(), time.Now()
	warning := ignoredHooksWarning(c, dir, now)
	for _, want := range []string{"gate hook gate inactive: changed since trust: definition changed", "hook sibling inactive: untrusted"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("missing %q: %s", want, warning)
		}
	}
	if got := ignoredHooksWarning(c, dir, now); got != "" {
		t.Fatalf("warning not throttled: %s", got)
	}
	c.IgnoredProject.Digest = "two"
	if got := ignoredHooksWarning(c, dir, now); got == "" {
		t.Fatal("changed ignored digest must invalidate throttle")
	}
}
