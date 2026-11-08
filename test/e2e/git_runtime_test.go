//go:build unix

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
)

func TestOfflineNormalizeDoesNotStartGitOrTransforms(t *testing.T) {
	s := newSandbox(t)
	s.configure(func(c *config.Config) {
		c.Transforms = []config.Transform{envTransformFixture(s, "transform", map[string]any{"CONTEXT": "changed"})}
	})
	if err := os.Mkdir(filepath.Join(s.cwd, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.cwd, ".git", "index"), []byte("index-present"), 0600); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(s.cwd, config.LocalName)
	if err := os.WriteFile(local, []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out := s.management(nil, "normalize", "--no-resolve", "--", "-C", s.cwd, "unrecognized")
	out.code(t, 0)
	var result model.Result
	if err := json.Unmarshal(out.stdout, &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.NormalizerVersion == "" || result.Registry.Git != "v2.56.0" || len(result.Operations) != 1 {
		t.Fatalf("result: %+v", result)
	}
	s.absentRecord()
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "transform.json")); !os.IsNotExist(err) {
		t.Fatal("normalize ran a transform")
	}
	s.management(nil, "explain", "--no-resolve", "--no-transforms", "--", "status").code(t, 0)
	if _, err := os.Stat(filepath.Join(s.root, "transform.json")); !os.IsNotExist(err) {
		t.Fatal("--no-transforms ran a transform")
	}
	s.management(nil, "explain", "--no-resolve", "--", "status").code(t, 0)
	if _, err := os.Stat(filepath.Join(s.root, "transform.json")); err != nil {
		t.Fatal("explain omitted trusted transform")
	}
	s.absentRecord()
}

func TestExecDoesNotStripLeadingGit(t *testing.T) {
	s := newSandbox(t)
	if err := os.Remove(s.cfg); err != nil {
		t.Fatal(err)
	}
	s.management(nil, "exec", "--", "git", "status").code(t, 0)
	s.onlyRecord([]string{"git", "status"}, nil)
}

func TestEffectiveProjectPolicyAndFastPath(t *testing.T) {
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"chdir", "git-dir", "work-tree", "environment", "bare"} {
		t.Run(kind, func(t *testing.T) {
			s := newSandbox(t)
			if err := os.Remove(s.cfg); err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(s.root, "effective")
			init := []string{"init", "-q"}
			if kind == "bare" {
				init = append(init, "--bare")
			}
			cmd := exec.Command(real, append(init, repo)...)
			cmd.Env = s.environ()
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("init: %v\n%s", err, out)
			}
			deny := []byte("version: 1\npolicy: {mode: enforce, rules: [{id: effective, decision: deny}]}\n")
			if err := os.WriteFile(filepath.Join(repo, ".gthok.yaml"), deny, 0600); err != nil {
				t.Fatal(err)
			}
			var args []string
			switch kind {
			case "chdir":
				args = []string{"-C", repo, "status"}
			case "git-dir":
				args = []string{"--git-dir=" + filepath.Join(repo, ".git"), "status"}
			case "work-tree":
				args = []string{"--git-dir=" + filepath.Join(repo, ".git"), "--work-tree=" + repo, "status"}
			case "environment":
				s.env["GIT_DIR"], s.env["GIT_WORK_TREE"] = filepath.Join(repo, ".git"), repo
				args = []string{"status"}
			case "bare":
				args = []string{"-C", repo, "--bare", "status"}
			}
			s.git(nil, args...).code(t, 77)
			s.absentRecord()
			logPath := filepath.Join(filepath.Dir(s.cfg), "logs", "gthok.jsonl")
			beforeLog, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(repo, ".gthok.yaml")); err != nil {
				t.Fatal(err)
			}
			// A config in the caller's repository must not apply to another repository.
			if err := os.Mkdir(filepath.Join(s.cwd, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(s.cwd, ".gthok.yaml"), deny, 0600); err != nil {
				t.Fatal(err)
			}
			s.git(nil, args...).code(t, 0)
			s.onlyRecord(args, nil)
			afterLog, err := os.ReadFile(logPath)
			if err != nil || !bytes.Equal(beforeLog, afterLog) {
				t.Fatal("no-config fast path wrote a log")
			}
		})
	}
}
