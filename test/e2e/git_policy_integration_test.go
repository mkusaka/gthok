//go:build unix

package e2e

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
)

func gitPolicyRule(id, action, reason string) config.Rule {
	return config.Rule{ID: id, Decision: "deny", Reason: reason, Match: config.Match{Operations: config.StringList{action}}}
}

func checkGitPolicyDeny(t *testing.T, out outcome, reason string) {
	t.Helper()
	out.code(t, 77)
	if len(out.stdout) != 0 || !bytes.HasPrefix(out.stderr, []byte("gthok:")) || !bytes.Contains(out.stderr, []byte(reason)) {
		t.Fatalf("deny contract: stdout=%q stderr=%q", out.stdout, out.stderr)
	}
}

func TestGitPolicyUnconditionalForceVersusLease(t *testing.T) {
	s, real := gitFixture(t)
	remote := filepath.Join(s.root, "origin.git")
	s.run(real, nil, "init", "-q", "--bare", remote).code(t, 0)
	s.run(real, nil, "remote", "add", "origin", "file://"+remote).code(t, 0)
	s.run(real, nil, "push", "-q", "origin", "main").code(t, 0)
	before := s.run(real, nil, "--git-dir="+remote, "show-ref")
	before.code(t, 0)
	s.run(real, nil, "commit", "-q", "--allow-empty", "-m", "next").code(t, 0)
	s.configure(func(c *config.Config) {
		c.Policy.Mode = config.ModeEnforce
		rule := gitPolicyRule("no-force", "remote.push.*", "unconditional pushes prohibited")
		rule.Match.Parameters = map[string]string{"forceMode": "unconditional"}
		c.Policy.Rules = []config.Rule{rule}
	})
	for _, args := range [][]string{
		{"push", "--force", "origin", "main"},
		{"push", "origin", "+main:refs/heads/main"},
		{"push", "-qf", "origin", "main"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			checkGitPolicyDeny(t, s.git(nil, args...), "unconditional pushes prohibited")
			after := s.run(real, nil, "--git-dir="+remote, "show-ref")
			after.code(t, 0)
			if !bytes.Equal(before.stdout, after.stdout) {
				t.Fatal("denied push changed remote refs")
			}
		})
	}
	s.git(nil, "push", "--force-with-lease", "origin", "main").code(t, 0)
	local := s.run(real, nil, "rev-parse", "main")
	pushed := s.run(real, nil, "--git-dir="+remote, "rev-parse", "refs/heads/main")
	local.code(t, 0)
	pushed.code(t, 0)
	if !bytes.Equal(local.stdout, pushed.stdout) || bytes.Contains(before.stdout, bytes.TrimSpace(pushed.stdout)) {
		t.Fatal("lease push was not executed")
	}
}

func TestGitPolicySemanticRefRename(t *testing.T) {
	for _, flag := range []string{"-m", "-M"} {
		t.Run(flag, func(t *testing.T) {
			s, real := gitFixture(t)
			s.run(real, nil, "branch", "a").code(t, 0)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = config.ModeEnforce
				c.Policy.Rules = []config.Rule{gitPolicyRule("no-ref-rename", "ref.rename", "ref renaming prohibited")}
			})
			before := gitState(t, s, real)
			checkGitPolicyDeny(t, s.git(nil, "branch", flag, "a", "b"), "ref renaming prohibited")
			if after := gitState(t, s, real); !reflect.DeepEqual(before, after) {
				t.Fatal("denied ref rename changed repository state")
			}
		})
	}
}

func TestGitPolicySemanticRefDelete(t *testing.T) {
	for _, args := range [][]string{{"update-ref", "-d", "refs/heads/x"}, {"update-ref", "--no-deref", "-d", "refs/heads/x"}, {"branch", "-D", "x"}, {"tag", "-d", "v1"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s, real := gitFixture(t)
			s.run(real, nil, "branch", "x").code(t, 0)
			s.run(real, nil, "tag", "--no-sign", "v1").code(t, 0)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = config.ModeEnforce
				c.Policy.Rules = []config.Rule{gitPolicyRule("no-ref-delete", "ref.delete", "ref deletion prohibited")}
			})
			before := gitState(t, s, real)
			checkGitPolicyDeny(t, s.git(nil, args...), "ref deletion prohibited")
			if after := gitState(t, s, real); !reflect.DeepEqual(before, after) {
				t.Fatal("denied ref deletion changed repository state")
			}
		})
	}
}
