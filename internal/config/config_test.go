package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() *Config {
	c := &Config{Hooks: []Hook{{ID: "a", Phase: PhasePre, Command: []string{"hook"}}, {ID: "b", Phase: PhasePost, Command: []string{"hook"}}}}
	c.ApplyDefaults()
	return c
}
func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsAndVersions(t *testing.T) {
	for _, body := range []string{"", "# empty\n", "{}", "version: 1\n"} {
		c, err := Parse([]byte(body), "/config")
		if err != nil {
			t.Fatal(err)
		}
		if c.Version != 1 || c.Policy.Mode != ModeObserve || c.Policy.UnknownMutation != UnknownAudit || c.Runtime.HookConcurrency != 4 || c.Runtime.StdinMaxBytes != 8<<20 || c.Runtime.StdinTimeout.D() != DefaultStdinTimeout || c.Runtime.FileMaxBytes != 8<<20 || c.Runtime.CaptureLimit != 1<<20 || !*c.Logging.Enabled || c.Logging.Path != "/config/logs/gthok.jsonl" {
			t.Fatalf("defaults: %+v", c)
		}
	}
	for _, body := range []string{"policy: {mode: enforce}", "version: 0", "version: 2", "version: 1\n---\nversion: 1", "[1, 2]"} {
		if _, err := Parse([]byte(body), ""); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
	c, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil || !filepath.IsAbs(c.Dir) {
		t.Fatalf("missing config: %v, %+v", err, c)
	}
}

func TestGitConfigAndRemovedFeatures(t *testing.T) {
	c, err := Parse([]byte("version: 1\nruntime: {realGit: /usr/bin/git}\nhooks: {pre: [{id: check, match: {operation: remote.push.*, level: L1}, command: [./hook]}]}"), "/config")
	if err != nil {
		t.Fatal(err)
	}
	if c.Runtime.RealGit != "/usr/bin/git" || c.HookCommandPath(c.Hooks[0]) != "/config/hook" {
		t.Fatalf("config: %+v", c)
	}
	for _, body := range []string{"extensions: [{name: dash}]", "runtime: {realGh: /bin/gh}", "policy: {protectedRepositories: [owner/repo]}", "hooks: {pre: [{id: check, command: [hook], match: {repositories: [owner/repo]}}]}", "hooks: {pre: [{id: check, command: [hook], match: {token: app}}]}", "hooks: {pre: [{id: check, command: [hook], match: {method: GET}}]}"} {
		if _, err := Parse([]byte("version: 1\n"+body), ""); err == nil {
			t.Fatalf("accepted removed GitHub feature %q", body)
		}
	}
}

func TestExplicitZeroAndUnknownKeys(t *testing.T) {
	for _, body := range []string{"runtime: {hookConcurrency: 0}", "runtime: {stdinMaxBytes: 0}", "runtime: {stdinTimeout: 0s}", "runtime: {fileMaxBytes: 0}", "runtime: {captureLimit: 0}", "runtime: {resolveTimeout: 0s}", "hooks: {pre: [{id: h, command: [h], timeout: 0s}]}", "policy: {mode: bad}", "policy: {unknown: true}", "hooks: {pre: [{id: h, command: [h], match: {level: L3}}]}"} {
		if _, err := Parse([]byte("version: 1\n"+body), ""); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}

func TestContinueOnErrorConfig(t *testing.T) {
	for _, phase := range []string{PhasePre, PhasePost} {
		c, err := Parse([]byte("version: 1\nhooks: {"+phase+": [{id: h, command: [h], continueOnError: true}]}"), "")
		if phase == PhasePre && (err != nil || !c.Hooks[0].ContinueOnError) {
			t.Fatalf("pre: %v", err)
		}
		if phase == PhasePost && (err == nil || !strings.Contains(err.Error(), "continueOnError")) {
			t.Fatalf("post: %v", err)
		}
	}
}
func TestConfigDirectoryPrecedence(t *testing.T) {
	env := map[string]string{"HOME": "/home/test"}
	if Dir(env) != "/home/test/.config/gthok" {
		t.Fatal(Dir(env))
	}
	env["XDG_CONFIG_HOME"] = "/xdg"
	if Dir(env) != "/xdg/gthok" {
		t.Fatal(Dir(env))
	}
	env["GTHOK_CONFIG_DIR"] = "/custom"
	if DefaultPath(env) != "/custom/config.yaml" {
		t.Fatal(DefaultPath(env))
	}
}
