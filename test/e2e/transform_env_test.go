//go:build unix

package e2e

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
)

func envTransformFixture(s *sandbox, id string, env map[string]any) config.Transform {
	s.t.Helper()
	tr := transformFixture(s, id, nil)
	data, err := json.Marshal(map[string]any{"schemaVersion": 1, "env": env})
	if err != nil {
		s.t.Fatal(err)
	}
	tr.Env["FAKEGIT_HOOK_RESPONSE"] = string(data)
	tr.Env["FAKEGIT_HOOK_ENV_RECORD"] = filepath.Join(s.root, id+"-env.json")
	return tr
}

func recordedHookEnv(t *testing.T, s *sandbox, id string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.root, id+"-env.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []string
	decodeJSON(t, data, &entries)
	return envMap(entries)
}

func TestTransformEnvExecution(t *testing.T) {
	for _, supervised := range []bool{false, true} {
		name := "exec-fast-path"
		if supervised {
			name = "supervised"
		}
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			token := "ghs" + "_environment_fixture"
			s.env["REMOVE_ME"] = "original"
			s.configure(func(c *config.Config) {
				c.Transforms = []config.Transform{envTransformFixture(s, "credentials", map[string]any{"GH_TOKEN": token, "REMOVE_ME": nil, "Z_CONTEXT": "selected"})}
				if supervised {
					pre, post := s.hook("pre", "pre"), s.hook("post", "post")
					pre.Match = config.Match{Env: map[string]string{"Z_CONTEXT": "selected", "REMOVE_ME": ""}}
					pre.Env["FAKEGIT_HOOK_ENV_RECORD"] = filepath.Join(s.root, "pre-env.json")
					post.Env["FAKEGIT_HOOK_ENV_RECORD"] = filepath.Join(s.root, "post-env.json")
					c.Hooks = []config.Hook{pre, post}
				}
			})
			args := []string{"version"}
			s.management(nil, "exec", "--", "version").code(t, 0)
			env := envMap(s.onlyRecord(args, nil).Env)
			if env["GH_TOKEN"] != token || env["GTHOK_DEPTH"] != "1" || env["Z_CONTEXT"] != "selected" {
				t.Fatalf("child environment missing edits or depth: %v", env)
			}
			if _, exists := env["REMOVE_ME"]; exists {
				t.Fatal("unset variable survived")
			}
			if supervised {
				for _, id := range []string{"pre", "post"} {
					in := s.hookInput(id)
					if !reflect.DeepEqual(in.Invocation.EnvChanged, []string{"GH_TOKEN", "REMOVE_ME", "Z_CONTEXT"}) {
						t.Fatalf("%s input metadata: %+v", id, in.Invocation)
					}
				}
				pre, post := recordedHookEnv(t, s, "pre"), recordedHookEnv(t, s, "post")
				if pre["GH_TOKEN"] != "" || pre["REMOVE_ME"] != "original" || post["GH_TOKEN"] != token {
					t.Fatalf("PRE/POST process environments incorrect: pre=%v post=%v", pre, post)
				}
			}
		})
	}
}

func TestTransformEnvChain(t *testing.T) {
	s := newSandbox(t)
	s.env["CHAIN"] = "original"
	s.configure(func(c *config.Config) {
		never := envTransformFixture(s, "no-retrigger", map[string]any{"CHAIN": "wrong"})
		never.Match.Env = map[string]string{"CHAIN": "first"}
		first := envTransformFixture(s, "first", map[string]any{"CHAIN": "first", "GH_TOKEN": "ghs" + "_chain_fixture"})
		second := envTransformFixture(s, "second", map[string]any{"CHAIN": "last"})
		second.Match = config.Match{Env: map[string]string{"CHAIN": "first"}}
		c.Transforms = []config.Transform{never, first, second}
	})
	s.git(nil, "version").code(t, 0)
	if envMap(s.onlyRecord([]string{"version"}, nil).Env)["CHAIN"] != "last" {
		t.Fatal("later matching transform did not win")
	}
	if got := recordedHookEnv(t, s, "second"); got["CHAIN"] != "first" || got["GH_TOKEN"] != "ghs"+"_chain_fixture" {
		t.Fatalf("transform process did not receive current environment: %v", got)
	}
	if _, err := os.Stat(filepath.Join(s.root, "no-retrigger.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("earlier transform retriggered: %v", err)
	}
}

func TestTransformEnvRollback(t *testing.T) {
	s := newSandbox(t)
	s.env["CHAIN"] = "original"
	s.configure(func(c *config.Config) {
		first := envTransformFixture(s, "environment", map[string]any{"CHAIN": "changed", "GH_TOKEN": "ghs" + "_rollback_fixture"})
		bad := envTransformFixture(s, "bad", map[string]any{"GTHOK_DEPTH": "99"})
		bad.OnError = "allow"
		c.Transforms = []config.Transform{first, bad, envTransformFixture(s, "never", map[string]any{"CHAIN": "wrong"})}
		c.Hooks = []config.Hook{s.hook("inspect", "pre")}
	})
	args := []string{"notes", "add", "12", "-R", "acme/project", "--body", "original"}
	s.git(nil, args...).code(t, 0)
	env := envMap(s.onlyRecord(args, nil).Env)
	in := s.hookInput("inspect")
	if env["CHAIN"] != "original" || env["GH_TOKEN"] != "" || env["GTHOK_DEPTH"] != "1" || len(in.Invocation.EnvChanged) != 0 {
		t.Fatalf("partial chain survived rollback: env=%v input=%+v", env, in)
	}
	if _, err := os.Stat(filepath.Join(s.root, "never.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("chain continued after rollback: %v", err)
	}
}

func TestTransformEnvForbiddenName(t *testing.T) {
	for _, mode := range []string{"observe", "enforce"} {
		t.Run(mode, func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = mode
				c.Transforms = []config.Transform{envTransformFixture(s, "forbidden", map[string]any{"GTHOK_DEPTH": "99"})}
			})
			got := s.git(nil, "version")
			got.code(t, 78)
			s.absentRecord()
			if !strings.Contains(string(got.stderr), "denied by transform forbidden") || !strings.Contains(string(got.stderr), "(error: invalid-response)") {
				t.Fatalf("missing invalid-response deny contract: %s", got.stderr)
			}
		})
	}
}
