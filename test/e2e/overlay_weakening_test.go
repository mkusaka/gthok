//go:build unix

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
)

// A repository's .gthok.yaml or .gthok.local.yaml cannot loosen the user's
// enforcement: the real request is still denied before git starts, and a
// project hook runs only when the user config trusts the repository.
func TestOverlaysCannotWeakenUserEnforcement(t *testing.T) {
	for _, tc := range []struct {
		name, project, local string
		note                 string // config validate output proving the overlay was read
		userTrust, hookRuns  bool
	}{
		{name: "no overlay", note: "Configuration valid"},
		{name: "project observe", project: "version: 1\npolicy: {mode: observe}\n", note: "project policy.mode would weaken global policy; ignored"},
		{name: "project allows unknowns", project: "version: 1\npolicy: {unknownMutation: allow, unknownInput: allow}\n", note: "project policy.unknownInput would weaken global policy; ignored"},
		{name: "project runtime and logging", project: "version: 1\nruntime: {hookConcurrency: 1}\nlogging: {enabled: false}\n", note: "cannot set runtime"},
		{name: "project trusts itself", project: "version: 1\npolicy: {mode: observe}\ntrust: {repositories: [{cwd}]}\n{hook}", note: "cannot set trust.repositories (user config only)"},
		{name: "untrusted project hook", project: "version: 1\n{hook}", note: "hook project-hook ignored (untrusted)"},
		{name: "local observe", local: "version: 1\npolicy: {mode: observe}\n", note: "project policy.mode would weaken global policy; ignored"},
		{name: "local trusts repository", project: "version: 1\n{hook}", local: "version: 1\ntrust: {repositories: [{cwd}]}\n", note: "local config ignored"},
		{name: "user trusts repository", project: "version: 1\npolicy: {mode: observe}\n{hook}", note: "Project hooks and transforms run without per-hook trust", userTrust: true, hookRuns: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSandbox(t)
			if err := os.Mkdir(filepath.Join(s.cwd, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(s.root, "project-hook.json")
			hook := fmt.Sprintf("hooks:\n  pre:\n  - id: project-hook\n    match: {command: branch}\n    command: [%q]\n    env: {FAKEGIT_MODE: hook, FAKEGIT_HOOK_RECORD: %q}\n", fakeBinary, marker)
			s.configure(func(c *config.Config) {
				c.Policy.Mode, c.Policy.UnknownMutation = config.ModeEnforce, config.UnknownDeny
				c.Policy.Rules = []config.Rule{{ID: "no-delete", Match: config.Match{Commands: config.StringList{"branch"}}, Decision: "deny", Reason: "user policy"}}
			})
			if tc.userTrust { // as YAML: a marshalled Trust carries include, which user configs reject
				f, err := os.OpenFile(s.cfg, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := fmt.Fprintf(f, "trust: {repositories: [%q]}\n", s.cwd); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for name, body := range map[string]string{".gthok.yaml": tc.project, ".gthok.local.yaml": tc.local} {
				if body == "" {
					continue
				}
				body = strings.NewReplacer("{cwd}", fmt.Sprintf("%q", s.cwd), "{hook}", hook).Replace(body)
				if err := os.WriteFile(filepath.Join(s.cwd, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}

			for _, req := range []struct {
				deniedBy string
				args     []string
			}{
				{"rule no-delete", []string{"branch", "-D", "acme/widgets", "--yes"}},
				{"policy unknownMutation", []string{"push", "--future-option"}},
			} {
				got := s.git(nil, req.args...)
				got.code(t, 77)
				s.absentRecord()
				if !bytes.HasPrefix(got.stderr, []byte("gthok: denied by "+req.deniedBy)) {
					t.Fatalf("%v: stderr=%q", req.args, got.stderr)
				}
			}
			validate := s.management(nil, "config", "validate")
			if out := string(validate.stdout) + string(validate.stderr); !strings.Contains(out, tc.note) {
				t.Fatalf("config validate lacks %q:\n%s", tc.note, out)
			}
			if _, err := os.Stat(marker); tc.hookRuns != (err == nil) {
				t.Fatalf("project hook ran=%t, want %t", err == nil, tc.hookRuns)
			}
		})
	}
}
