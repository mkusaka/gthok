//go:build unix

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/install"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

func decodeJSON(t *testing.T, data []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("invalid command JSON: %v; output=%q", err, data)
	}
}

func TestManagementVersionAndHelp(t *testing.T) {
	s := newSandbox(t)
	result := s.management(nil, "version")
	result.code(t, 0)
	for _, field := range []string{"gthok", "e2e-version", "e2e-commit", runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH} {
		if !bytes.Contains(result.stdout, []byte(field)) {
			t.Fatalf("version missing %q", field)
		}
	}
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"help", "normalize"}, {"hooks", "--help"}, {"config", "--help"}} {
		result := s.management(nil, args...)
		result.code(t, 0)
		if len(result.stdout) == 0 || len(result.stderr) != 0 {
			t.Fatalf("help output for %q", args)
		}
	}
	s.absentRecord()
}

func TestManagementUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"unknown-command"}, {"version", "unexpected"}, {"version", "--invalid"},
		{"normalize"}, {"normalize", "push", "origin", "1"}, {"normalize", "--allow-api", "--no-resolve", "--", "push", "origin", "1"},
		{"explain", "--allow-api", "--no-resolve", "--", "push", "origin", "1"}, {"hooks", "unknown"},
		{"hooks", "test", "--phase", "bad", "--", "version"}, {"hooks", "test", "--"},
		{"hooks", "test", "--allow-api", "--no-resolve", "--", "version"},
		{"config", "unknown"}, {"doctor", "--unknown"}, {"shim", "install", "--bin-dir", ""},
		{"config", "schema"}, {"schema", "unknown"}, {"schema", "hook-input", "--project"}, {"schema", "hook-input", "extra"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			s := newSandbox(t)
			result := s.management(nil, args...)
			result.code(t, 2)
			if !bytes.HasPrefix(result.stderr, []byte("gthok:")) {
				t.Fatal("missing usage diagnostic")
			}
			s.absentRecord()
		})
	}
}

func TestManagementExplainDecisions(t *testing.T) {
	for _, tc := range []struct {
		mode, rule, decision string
	}{{"observe", "", "allow"}, {"observe", "deny", "audit"}, {"enforce", "audit", "audit"}, {"enforce", "deny", "deny"}} {
		t.Run(tc.mode+"-"+tc.rule, func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = tc.mode
				if tc.rule != "" {
					c.Policy.Rules = []config.Rule{{ID: "rule", Decision: tc.rule, Reason: "consumer reason"}}
				}
			})
			result := s.management(nil, "explain", "--", "push", "origin", "12", "-R", "acme/project")
			result.code(t, 0)
			if !bytes.HasPrefix(result.stdout, []byte("Decision: "+tc.decision+" (mode="+tc.mode+")\n")) {
				t.Fatalf("explain decision=%s", result.stdout)
			}
			if tc.rule != "" && bytes.Count(result.stdout, []byte("consumer reason")) != 1 {
				t.Fatalf("explain reason duplication=%s", result.stdout)
			}
			s.absentRecord()
		})
	}
}

func TestManagementHooksListAndTest(t *testing.T) {
	for _, mode := range []string{"enforce", "observe"} {
		t.Run(mode, func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = mode
				pre, post := s.hook("pre", "pre"), s.hook("post", "post")
				pre.Env["FAKEGIT_HOOK_DECISION"] = "deny"
				pre.Env["FAKEGIT_HOOK_MESSAGE"] = "test denial"
				c.Hooks = []config.Hook{pre, post}
			})
			result := s.management(nil, "hooks", "list", "--json")
			result.code(t, 0)
			var list []struct {
				ID    string `json:"id"`
				Phase string `json:"phase"`
			}
			decodeJSON(t, result.stdout, &list)
			if len(list) != 2 || list[0].ID != "pre" || list[1].ID != "post" || list[0].Phase != "pre" {
				t.Fatalf("hooks list=%+v", list)
			}
			result = s.management(nil, "hooks", "list")
			result.code(t, 0)
			if !bytes.Contains(result.stdout, []byte("pre")) || !bytes.Contains(result.stdout, []byte("post")) {
				t.Fatal("human hook list omitted IDs")
			}
			for _, phase := range []string{"pre", "post"} {
				result = s.management(nil, "hooks", "test", "--json", "--phase", phase, "--", "push", "origin", "7", "-R", "acme/project")
				want := 0
				if phase == "pre" && mode == "enforce" {
					want = 1
				}
				result.code(t, want)
				var report struct {
					Phase     string                `json:"phase"`
					Synthetic bool                  `json:"synthetic"`
					Results   []protocol.HookResult `json:"results"`
					Final     struct {
						Decision  string `json:"decision"`
						WouldDeny bool   `json:"wouldDeny"`
					} `json:"final"`
				}
				decodeJSON(t, result.stdout, &report)
				if report.Phase != phase || !report.Synthetic || len(report.Results) != 1 || report.Results[0].HookID != phase || report.Results[0].Outcome != "ok" {
					t.Fatalf("hook test report=%+v", report)
				}
				if phase == "pre" {
					if !report.Final.WouldDeny || (report.Final.Decision == "deny") != (mode == "enforce") {
						t.Fatalf("final=%+v", report.Final)
					}
				}
				input := s.hookInput(phase)
				if phase == "post" && (input.Result == nil || input.Result.ExitCode != 0) {
					t.Fatal("synthetic post hook missing execution result")
				}
			}
			result = s.management(nil, "hooks", "test", "--phase", "post", "--", "version")
			result.code(t, 0)
			if !bytes.Contains(result.stdout, []byte("post")) {
				t.Fatal("human hook test omitted result")
			}
			s.absentRecord()
		})
	}
	t.Run("hook-error-status", func(t *testing.T) {
		s := newSandbox(t)
		s.configure(func(c *config.Config) {
			h := s.hook("pre", "pre")
			h.Env["FAKEGIT_HOOK_EXIT"] = "19"
			c.Hooks = []config.Hook{h}
		})
		result := s.management(nil, "hooks", "test", "--json", "--", "version")
		result.code(t, 1)
		s.absentRecord()
	})
}

func TestManagementHookSources(t *testing.T) {
	s := newSandbox(t)
	if err := os.Mkdir(filepath.Join(s.cwd, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(s.cwd, ".gthok.yaml")
	command, err := json.Marshal([]string{fakeBinary})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("version: 1\nhooks: {post: [{id: project, command: "+string(command)+"}]}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	s.configure(func(c *config.Config) {
		c.Hooks = []config.Hook{s.hook("user", "pre")}
	})
	s.management(nil, "config", "trust").code(t, 0)
	result := s.management(nil, "hooks", "list", "--json")
	result.code(t, 0)
	var list []struct {
		ID     string `json:"id"`
		Source string `json:"source"`
	}
	decodeJSON(t, result.stdout, &list)
	if len(list) != 2 || list[0].ID != "user" || list[0].Source != s.cfg || list[1].ID != "project" || list[1].Source != project {
		t.Fatalf("hook provenance=%+v", list)
	}
	result = s.management(nil, "hooks", "list")
	result.code(t, 0)
	for _, source := range []string{s.cfg, project} {
		if !bytes.Contains(result.stdout, []byte("source: "+source)) {
			t.Fatalf("human hook provenance=%s", result.stdout)
		}
	}
	s.absentRecord()
}

func TestManagementConfigAndDoctor(t *testing.T) {
	s := newSandbox(t)
	result := s.management(nil, "config", "path")
	result.code(t, 0)
	if strings.TrimSpace(string(result.stdout)) != s.cfg {
		t.Fatalf("config path=%q", result.stdout)
	}
	result = s.management(nil, "config", "validate")
	result.code(t, 0)
	if !bytes.Contains(result.stdout, []byte(s.cfg)) {
		t.Fatal("validation omitted config path")
	}
	setBytes(s.env, "FAKEGIT_STDOUT_B64", []byte("git version 2.102.0 (fake)\n"))
	result = s.management(nil, "doctor", "--json")
	result.code(t, 0)
	var checks []install.Check
	decodeJSON(t, result.stdout, &checks)
	byName := make(map[string]install.Check)
	for _, check := range checks {
		byName[check.Name] = check
	}
	for _, name := range []string{"gthok", "real-git", "path-precedence", "shim-ownership", "recursion-risk", "git-version", "platform-process", "config", "log-writable"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("doctor missing %s", name)
		}
	}
	if byName["config"].Status != install.StatusOK || byName["real-git"].Status != install.StatusOK {
		t.Fatal("healthy doctor check failed")
	}
	records := s.records()
	if len(records) != 1 || len(records[0].Argv) != 1 || records[0].Argv[0] != "--version" {
		t.Fatalf("doctor child=%+v", records)
	}
	result = s.management(nil, "doctor")
	result.code(t, 0)
	if !bytes.Contains(result.stdout, []byte("real-git")) || !bytes.Contains(result.stdout, []byte("config")) {
		t.Fatal("human doctor omitted checks")
	}
	if err := os.WriteFile(s.cfg, []byte("version: 1\ninvalid: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result = s.management(nil, "config", "validate")
	result.code(t, 1)
	result = s.management(nil, "doctor", "--json")
	result.code(t, 1)
	checks = nil
	decodeJSON(t, result.stdout, &checks)
	found := false
	for _, check := range checks {
		if check.Name == "config" && check.Status == install.StatusFail {
			found = true
		}
	}
	if !found {
		t.Fatal("doctor did not report invalid configuration")
	}
}

func TestManagementDoctorRecordedCustomInstall(t *testing.T) {
	s := newSandbox(t)
	s.management(nil, "shim", "install", "--bin-dir", s.bin).code(t, 0)
	bin, err := filepath.EvalSymlinks(s.bin)
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(bin, "git")
	defaultShim := filepath.Join(s.env["HOME"], ".local", "bin", "git")
	if _, err := os.Lstat(defaultShim); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected default shim: %v", err)
	}
	setBytes(s.env, "FAKEGIT_STDOUT_B64", []byte("git version 2.102.0 (fake)\n"))
	result := s.management(nil, "doctor")
	result.code(t, 0)
	for _, diagnostic := range []string{"ok path-precedence: " + shim + " is the first executable git", "ok shim-ownership: " + shim + " is an owned symlink"} {
		if !bytes.Contains(result.stdout, []byte(diagnostic)) {
			t.Fatalf("doctor omitted custom shim diagnostic %q: %s", diagnostic, result.stdout)
		}
	}
	if bytes.Contains(result.stdout, []byte(defaultShim)) {
		t.Fatalf("doctor inspected absent default shim: %s", result.stdout)
	}
	result = s.management(nil, "doctor", "--json")
	result.code(t, 0)
	var checks []install.Check
	decodeJSON(t, result.stdout, &checks)
	found := 0
	for _, check := range checks {
		if check.Name == "path-precedence" || check.Name == "shim-ownership" {
			found++
			if check.Status != install.StatusOK || !strings.HasPrefix(check.Detail, shim+" ") {
				t.Fatalf("custom shim check=%+v", check)
			}
		}
	}
	if found != 2 {
		t.Fatalf("expected one pair of custom shim checks: %+v", checks)
	}
}

func TestManagementDoctorHookFailures(t *testing.T) {
	for _, kind := range []string{"missing", "not-executable", "writable"} {
		t.Run(kind, func(t *testing.T) {
			s := newSandbox(t)
			path := filepath.Join(s.root, "hook")
			if kind != "missing" {
				mode := os.FileMode(0600)
				if kind == "writable" {
					mode = 0777
				}
				if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			s.configure(func(c *config.Config) { c.Hooks = []config.Hook{{ID: "bad", Phase: "pre", Command: []string{path}}} })
			result := s.management(nil, "doctor", "--json")
			result.code(t, 1)
			var checks []install.Check
			decodeJSON(t, result.stdout, &checks)
			found := false
			for _, check := range checks {
				if strings.Contains(check.Name, "bad") && check.Status == install.StatusFail {
					found = true
				}
			}
			if !found {
				t.Fatalf("doctor omitted hook failure: %+v", checks)
			}
		})
	}
}

func TestManagementEnableDisableOwnership(t *testing.T) {
	s := newSandbox(t)
	result := s.management(nil, "shim", "install", "--bin-dir", s.bin)
	result.code(t, 0)
	for _, value := range []string{filepath.Join(s.bin, "git"), s.real, "PATH"} {
		if !bytes.Contains(result.stdout, []byte(value)) {
			t.Fatalf("enable output omitted %q", value)
		}
	}
	s.absentRecord()
	for _, name := range []string{".bashrc", ".zshrc", ".profile"} {
		if _, err := os.Stat(filepath.Join(s.env["HOME"], name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("enable modified shell configuration")
		}
	}
	result = s.management(nil, "shim", "uninstall", "--bin-dir", s.bin)
	result.code(t, 0)
	if !bytes.Contains(result.stdout, []byte(filepath.Join(s.bin, "git"))) {
		t.Fatal("disable omitted shim path")
	}
	if _, err := os.Lstat(filepath.Join(s.bin, "git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("disable left shim")
	}
	if _, err := os.Stat(s.real); err != nil {
		t.Fatal("disable removed real git")
	}
	result = s.management(nil, "shim", "uninstall", "--bin-dir", s.bin)
	result.code(t, 0)
	if err := os.WriteFile(filepath.Join(s.bin, "git"), []byte("foreign file"), 0700); err != nil {
		t.Fatal(err)
	}
	result = s.management(nil, "shim", "install", "--bin-dir", s.bin)
	result.code(t, 1)
	result = s.management(nil, "shim", "uninstall", "--bin-dir", s.bin)
	result.code(t, 0)
	body, err := os.ReadFile(filepath.Join(s.bin, "git"))
	if err != nil || string(body) != "foreign file" {
		t.Fatal("management replaced foreign file")
	}
}

func TestManagementConfigValidateSelectedFileAndNotes(t *testing.T) {
	s := newSandbox(t)
	selected := filepath.Join(s.cwd, "selected.yaml")
	body := []byte("version: 1\npolicy:\n  mode: invalid\n  unknownMutation: invalid\nhooks:\n  pre:\n    - id: bad\n      onError: invalid\n      command: []\nruntime:\n  stdinMaxBytes: -1\n")
	if err := os.WriteFile(selected, body, 0600); err != nil {
		t.Fatal(err)
	}
	result := s.management(nil, "config", "validate", "--file", "selected.yaml")
	result.code(t, 1)
	for _, field := range []string{"policy.mode", "policy.unknownMutation", "hooks[0].onError", "hooks[0].command", "runtime.stdinMaxBytes"} {
		if !bytes.Contains(result.stderr, []byte(field)) {
			t.Fatalf("validation omitted independent error %q: %s", field, result.stderr)
		}
	}
	// --file does not replace or invalidate the global configuration.
	s.management(nil, "config", "validate").code(t, 0)
	if err := os.WriteFile(selected, []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(selected, 0666); err != nil {
		t.Fatal(err)
	}
	result = s.management(nil, "config", "validate", "--file", selected)
	result.code(t, 0)
	if !bytes.Contains(result.stdout, []byte(selected)) || !bytes.Contains(result.stdout, []byte("group/world-writable")) {
		t.Fatalf("validation omitted selected file/permissions note: %s", result.stdout)
	}
	if err := os.Mkdir(filepath.Join(s.cwd, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(s.cwd, ".gthok.yaml")
	if err := os.WriteFile(project, []byte("version: 1\nhooks: {pre: [{id: project, command: [./missing-hook]}]}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result = s.management(nil, "config", "validate")
	result.code(t, 0)
	if !bytes.Contains(result.stdout, []byte(".gthok.yaml")) {
		t.Fatal("validation omitted untrusted project note")
	}
	s.absentRecord()
}

func TestManagementCobraUsageBoundaries(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		group bool
	}{
		{[]string{"shim"}, true},
		{[]string{"hooks"}, true},
		{[]string{"config"}, true},
		{[]string{"--help", "extra"}, false},
		{[]string{"doctor", "--json=bad"}, false},
		{[]string{"config", "validate", "--file"}, false},
		{[]string{"config", "trust", "one", "two"}, false},
		{[]string{"config", "untrust", "one", "two"}, false},
		{[]string{"help", "unknown-command"}, false},
		{[]string{"completion"}, false},
		{[]string{"normalize", "version", "--", "version"}, false},
		{[]string{"explain", "version", "--", "version"}, false},
		{[]string{"hooks", "test", "version", "--", "version"}, false},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			s := newSandbox(t)
			result := s.management(nil, tc.args...)
			result.code(t, 2)
			if tc.group {
				if !bytes.Contains(result.stdout, []byte("Usage:")) || len(result.stderr) != 0 {
					t.Fatalf("bare group must show usage on stdout: %+v", result)
				}
			} else if len(result.stdout) != 0 || !bytes.HasPrefix(result.stderr, []byte("gthok:")) {
				t.Fatalf("usage error must report only on stderr: %+v", result)
			}
			s.absentRecord()
		})
	}
}

func TestManagementCobraPassthrough(t *testing.T) {
	s := newSandbox(t)
	s.configure(func(c *config.Config) {
		c.Hooks = []config.Hook{s.hook("passthrough", "pre")}
	})
	// These management-looking options, the second separator, and the empty
	// argument belong exclusively to git; none may affect management parsing.
	args := []string{"status", "--json=false", "--resolve", "--allow-api", "--show-secrets", "--phase=post", "--help", "", "--", "-h", "help", "version"}
	result := s.management(nil, append([]string{"normalize", "--"}, args...)...)
	result.code(t, 0)
	var normalized model.Result
	decodeJSON(t, result.stdout, &normalized)
	if !reflect.DeepEqual(normalized.Source.Argv, args) {
		t.Fatalf("normalize changed passthrough argv: %q", normalized.Source.Argv)
	}
	result = s.management(nil, append([]string{"explain", "--"}, args...)...)
	result.code(t, 0)
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.stdout, encoded) || bytes.Contains(result.stdout, []byte("WARNING: --show-secrets")) {
		t.Fatalf("explain consumed passthrough options: %s", result.stdout)
	}
	result = s.management(nil, append([]string{"hooks", "test", "--json", "--"}, args...)...)
	result.code(t, 0)
	var report struct {
		Phase string `json:"phase"`
	}
	decodeJSON(t, result.stdout, &report)
	input := s.hookInput("passthrough")
	if report.Phase != "pre" || !reflect.DeepEqual(input.Invocation.Argv, args) {
		t.Fatalf("hooks test consumed passthrough options: phase=%s argv=%q", report.Phase, input.Invocation.Argv)
	}
	s.absentRecord()
}

func TestManagementConfigInit(t *testing.T) {
	s := newSandbox(t)
	s.management(nil, "config", "init").code(t, 1) // not inside a git repository
	if err := os.Mkdir(filepath.Join(s.cwd, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	result := s.management(nil, "config", "init")
	result.code(t, 0)
	project := filepath.Join(s.cwd, ".gthok.yaml")
	data, err := os.ReadFile(project)
	if err != nil || !bytes.HasPrefix(data, []byte("# yaml-language-server: $schema=")) || !bytes.Contains(data, []byte("project.schema.json")) {
		t.Fatalf("project config: err=%v\n%s", err, data)
	}
	s.management(nil, "config", "init").code(t, 1) // never overwrites
	if err := os.Remove(s.cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	s.management(nil, "config", "init", "--scope", "user").code(t, 0)
	if data, err := os.ReadFile(s.cfg); err != nil || !bytes.Contains(data, []byte("config.schema.json")) {
		t.Fatalf("user config: err=%v", err)
	}
	s.management(nil, "config", "validate").code(t, 0)
	s.management(nil, "config", "init", "--scope", "team").code(t, 1)
}
