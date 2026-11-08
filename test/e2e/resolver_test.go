//go:build unix

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
)

func TestResolverRecordsDoNotHideOriginalRequests(t *testing.T) {
	args := []string{"rev-parse", "--git-dir", "--git-common-dir", "--is-bare-repository", "--is-inside-work-tree", "--show-prefix"}
	for _, deny := range []bool{false, true} {
		t.Run(map[bool]string{false: "allow", true: "deny"}[deny], func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = config.ModeEnforce
				if deny {
					c.Policy.Rules = []config.Rule{{ID: "deny-request", Decision: "deny"}}
				}
			})
			started := filepath.Join(s.root, "request-started")
			s.env["FAKEGIT_STARTED"] = started
			payload := []byte("original request output\n")
			setBytes(s.env, "FAKEGIT_STDOUT_B64", payload)
			input := []byte{0, 'i', 255}
			result := s.git(input, args...)
			if deny {
				result.code(t, 77)
				s.absentRecord()
				if _, err := os.Stat(started); !os.IsNotExist(err) {
					t.Fatalf("denied original request started: %v", err)
				}
			} else {
				result.code(t, 0)
				record := s.onlyRecord(args, input)
				if record.Kind != "execution" || envMap(record.Env)["GTHOK_RESOLVER_QUERY"] != "" || !bytes.Equal(result.stdout, payload) {
					t.Fatalf("original read command was mistaken for a probe: %+v", record)
				}
			}
			queries := s.resolverRecords()
			if len(queries) != 1 || !reflect.DeepEqual(queries[0].Argv, args) || queries[0].StdinLength != 0 || envMap(queries[0].Env)["GTHOK_RESOLVER_QUERY"] != "1" {
				t.Fatalf("resolver query records: %+v", queries)
			}
		})
	}
}

func TestResolverRecordsDoNotHideDelegates(t *testing.T) {
	s := newSandbox(t)
	s.env["GTHOK_RESOLVER_QUERY"], s.env["GTHOK_DELEGATE_ID"] = "1", "test-delegate"
	args := []string{"rev-parse", "--show-toplevel"}
	input := []byte("delegate input")
	s.run(fakeBinary, input, args...).code(t, 0)
	if got := s.onlyRecord(args, input); got.Kind != "execution" || len(s.resolverRecords()) != 0 {
		t.Fatal("delegate was hidden among resolver probes")
	}
}

func TestWrapperResolverUsesOriginalContextAndGlobals(t *testing.T) {
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	s := newSandbox(t)
	repo := filepath.Join(s.cwd, "repo")
	cmd := exec.Command(real, "init", "-q", "-b", "main", repo)
	cmd.Env = s.environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, output)
	}
	if err := os.Mkdir(filepath.Join(repo, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	s.env["GTHOK_REAL_GIT"] = real
	s.env["RESOLVE_PUSH_URL"] = "ssh://example.test/push"
	s.configure(func(c *config.Config) {
		c.Policy.Mode = config.ModeEnforce
		h := s.hook("pre", "pre")
		h.Env["FAKEGIT_HOOK_DECISION"] = "deny"
		c.Hooks = []config.Hook{h}
	})
	args := []string{"-C", "repo", "-C", "sub", "-C", "..", "--git-dir=.git", "--work-tree=.", "--namespace=fixture", "-c", "remote.origin.url=https://example.test/request", "--config-env=remote.origin.pushurl=RESOLVE_PUSH_URL", "status"}
	s.git(nil, args...).code(t, 77)
	input := s.hookInput("pre")
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if input.Context == nil || input.Context.RepoRoot != repo || input.Context.GitDir != filepath.Join(repo, ".git") || input.Context.Branch != "refs/heads/main" {
		t.Fatalf("effective resolver context: %+v", input.Context)
	}
	remote := input.Context.Remotes["origin"]
	if remote.URL != "https://example.test/request" || !reflect.DeepEqual(remote.PushURLs, []string{"ssh://example.test/push"}) {
		t.Fatalf("root config options lost: %+v", remote)
	}
	for _, op := range input.Operations {
		if op.Target.RepoRoot != repo || op.Target.GitDir != input.Context.GitDir {
			t.Fatalf("resolved target: %+v", op.Target)
		}
	}
}
