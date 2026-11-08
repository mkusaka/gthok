//go:build unix

package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestDenyContractContent(t *testing.T) {
	for _, hook := range []bool{false, true} {
		name := "single-rule"
		if hook {
			name = "multiline-hook"
		}
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			secret := "contract-secret-" + "token"
			s.env["GH_TOKEN"] = secret
			s.configure(func(c *config.Config) {
				c.Policy.Mode = config.ModeEnforce
				if hook {
					h := s.hook("review", config.PhasePre)
					h.Env["FAKEGIT_HOOK_RESPONSE"] = `{"decision":"deny","reason":"unused reason","message":"review required\n` + secret + `\ncontact owners"}`
					c.Hooks = []config.Hook{h}
				} else {
					c.Policy.Rules = []config.Rule{{ID: "no-merge", Decision: "deny", Reason: "review required"}}
				}
			})
			got := s.git(nil, "push", "origin", "1", "-R", "acme/project")
			got.code(t, 77)
			s.absentRecord()
			want := "gthok: denied by rule no-merge\n  - rule no-merge: review required (unknown)\n"
			if hook {
				want = "gthok: denied by hook review\n  - hook review: review required\n    [REDACTED]\n    contact owners\n"
			}
			if string(got.stderr) != want {
				t.Fatalf("stderr=%q, want=%q", got.stderr, want)
			}
		})
	}
}

func TestDenyContractHookErrors(t *testing.T) {
	for _, kind := range []string{"timeout", "exit 23", "exit -1", "invalid response", "spawn failed"} {
		t.Run(kind, func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = config.ModeEnforce
				h := s.hook("gate", config.PhasePre)
				h.OnError = config.OnErrorDeny
				switch kind {
				case "timeout":
					h.Timeout = config.Duration(2 * time.Second)
					h.Env["FAKEGIT_HOOK_SLEEP"] = "1m"
				case "exit 23":
					h.Env["FAKEGIT_HOOK_EXIT"] = "23"
				case "exit -1":
					h.Command = []string{"/bin/sh", "-c", "kill -TERM $$"}
				case "invalid response":
					h.Env["FAKEGIT_HOOK_RESPONSE"] = "not JSON"
				case "spawn failed":
					h.Command = []string{filepath.Join(s.root, "missing-hook")}
				}
				c.Hooks = []config.Hook{h}
			})
			got := s.git(nil, "version")
			got.code(t, 78)
			s.absentRecord()
			want := "gthok: denied by hook gate (error: " + kind + ")\n  - hook gate: "
			if !strings.HasPrefix(string(got.stderr), want) {
				t.Fatalf("stderr=%q, want prefix=%q", got.stderr, want)
			}
		})
	}
}

func TestDenyContractMixedOrdering(t *testing.T) {
	s := newSandbox(t)
	s.configure(func(c *config.Config) {
		c.Policy.Mode = config.ModeEnforce
		c.Policy.UnknownMutation = config.UnknownDeny
		c.Policy.Rules = []config.Rule{{ID: "z-first", Decision: "deny", Reason: "first configured"}, {ID: "a-second", Decision: "deny", Reason: "second configured"}}
		c.Runtime.HookConcurrency = 1
		yes := true
		c.Logging.Enabled = &yes
		failed := s.hook("failed", config.PhasePre)
		failed.OnError = config.OnErrorDeny
		failed.Env["FAKEGIT_HOOK_EXIT"] = "23"
		pending := s.hook("pending", config.PhasePre)
		pending.OnError = config.OnErrorDeny
		pending.DependsOn = []string{"failed"}
		c.Hooks = []config.Hook{failed, pending}
	})
	got := s.git(nil, "push", "--future-option")
	got.code(t, 77)
	s.absentRecord()
	want := "gthok: denied by rule z-first (+3 more)\n  - rule z-first: first configured (unknown)\n  - rule a-second: second configured (unknown)\n  - policy unknownMutation: unclassified-write: unknownMutation policy (unknown)\n  - hook failed: exit status 23\n"
	if string(got.stderr) != want {
		t.Fatalf("stderr=%q, want=%q", got.stderr, want)
	}
	logs := s.logs()
	if len(logs) != 1 || len(logs[0].Hooks) != 2 || logs[0].Hooks[1].Outcome != protocol.OutcomeSkipped ||
		logs[0].Hooks[1].Reason != "after pre-hook denial" || logs[0].Hooks[1].Effective != protocol.DecisionAbstain {
		t.Fatalf("full hook results not retained: %+v", logs)
	}
}

func TestDenyContractInvalidConfig(t *testing.T) {
	s := newSandbox(t)
	s.env["GTHOK_FAIL_CLOSED"] = "1"
	if err := os.WriteFile(s.cfg, []byte("version: 1\npolicy: {mode: broken}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := s.git(nil, "version")
	got.code(t, 78)
	s.absentRecord()
	if !bytes.HasPrefix(got.stderr, []byte("gthok: denied by config\n  - config: ")) || !bytes.Contains(got.stderr, []byte("\n    fail-closed: not running git")) || !bytes.Contains(got.stderr, []byte("gthok config validate")) {
		t.Fatalf("stderr=%q", got.stderr)
	}
}

func TestDenyContractObservePreservesStatus(t *testing.T) {
	s := newSandbox(t)
	s.env["FAKEGIT_EXIT"] = "42"
	s.configure(func(c *config.Config) {
		c.Policy.Mode = config.ModeObserve
		c.Policy.Rules = []config.Rule{{ID: "observe", Decision: "deny"}}
	})
	got := s.git(nil, "version")
	got.code(t, 42)
	if len(got.stderr) != 0 {
		t.Fatalf("observe printed denial: %s", got.stderr)
	}
	s.onlyRecord([]string{"version"}, nil)
}

func TestDenyHeaderPrecedesIgnoredProjectHooks(t *testing.T) {
	s := newSandbox(t)
	warningProject(t, s, nil)
	s.configure(func(c *config.Config) {
		c.Policy.Mode = config.ModeEnforce
		c.Policy.Rules = []config.Rule{{ID: "user-deny", Decision: "deny"}}
	})
	got := s.git(nil, "version")
	got.code(t, 77)
	s.absentRecord()
	if !bytes.HasPrefix(got.stderr, []byte("gthok: denied by rule user-deny\n")) {
		t.Fatalf("denial header was not stderr line 1: %q", got.stderr)
	}
	warnings, err := filepath.Glob(filepath.Join(filepath.Dir(s.cfg), "state", "ignored-hooks-*.warning"))
	if err != nil || len(warnings) != 0 {
		t.Fatalf("denial consumed hook warning throttle: %v, %v", warnings, err)
	}
}

func TestDenyHookMessageFallback(t *testing.T) {
	for _, message := range []string{"", "review required"} {
		t.Run("message="+message, func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = config.ModeEnforce
				h := s.hook("review", config.PhasePre)
				h.Env["FAKEGIT_HOOK_RESPONSE"] = `{"decision":"deny","reason":"review required","message":"` + message + `"}`
				c.Hooks = []config.Hook{h}
			})
			got := s.git(nil, "version")
			got.code(t, 77)
			s.absentRecord()
			if want := "gthok: denied by hook review\n  - hook review: review required\n"; string(got.stderr) != want {
				t.Fatalf("denial=%q, want=%q", got.stderr, want)
			}
		})
	}
}
