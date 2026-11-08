//go:build unix

package e2e

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
)

func realGitSandbox(t *testing.T) (*sandbox, string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	s := newSandbox(t)
	s.env["GTHOK_REAL_GIT"] = real
	s.env["PATH"] = s.bin + ":" + filepath.Dir(real) + ":/usr/bin:/bin"
	for _, role := range []string{"AUTHOR", "COMMITTER"} {
		s.env["GIT_"+role+"_NAME"], s.env["GIT_"+role+"_EMAIL"] = "Fixture", "fixture@example.test"
	}
	s.run(real, nil, "init", "-q", "-b", "main").code(t, 0)
	s.run(real, nil, "commit", "-q", "--allow-empty", "-m", "initial").code(t, 0)
	return s, real
}

func TestRealGitMessageTransformRoundTrip(t *testing.T) {
	for _, transport := range []string{"paragraphs", "file", "stdin"} {
		t.Run(transport, func(t *testing.T) {
			s, real := realGitSandbox(t)
			message := "transformed\n\nparagraph"
			s.configure(func(c *config.Config) {
				c.Transforms = []config.Transform{transformFixture(s, "message", map[string]any{"/operations/0/texts/body/value": message})}
			})
			args := []string{"commit", "-q", "--allow-empty"}
			var input []byte
			switch transport {
			case "paragraphs":
				args = append(args, "-moriginal", "-m", "paragraph")
			case "stdin":
				args, input = append(args, "-F-"), []byte("original")
			case "file":
				path := filepath.Join(s.cwd, "message")
				if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "-F", path)
			}
			s.git(input, args...).code(t, 0)
			got := s.run(real, nil, "log", "-1", "--format=%B")
			got.code(t, 0)
			if strings.TrimSpace(string(got.stdout)) != message {
				t.Fatalf("message: %q", got.stdout)
			}
			files, err := filepath.Glob(filepath.Join(s.env["TMPDIR"], "gthok-transform-*"))
			if err != nil || len(files) != 0 {
				t.Fatal("temporary files remain", files, err)
			}
		})
	}
}

func TestRealGitRemoteTransform(t *testing.T) {
	s, real := realGitSandbox(t)
	for _, name := range []string{"origin", "other"} {
		path := filepath.Join(s.root, name+".git")
		s.run(real, nil, "init", "-q", "--bare", path).code(t, 0)
		s.run(real, nil, "remote", "add", name, path).code(t, 0)
	}
	s.configure(func(c *config.Config) {
		c.Transforms = []config.Transform{transformFixture(s, "remote", map[string]any{"/operations/0/target/remote": "other"})}
	})
	s.git(nil, "push", "origin", "HEAD:refs/heads/main").code(t, 0)
	s.run(real, nil, "--git-dir="+filepath.Join(s.root, "origin.git"), "show-ref", "--verify", "--quiet", "refs/heads/main").code(t, 1)
	s.run(real, nil, "--git-dir="+filepath.Join(s.root, "other.git"), "show-ref", "--verify", "--quiet", "refs/heads/main").code(t, 0)
	// Changing a remote may change its URLs, but not unedited default ref mappings.
	s.run(real, nil, "config", "remote.origin.push", "HEAD:refs/heads/main").code(t, 0)
	s.run(real, nil, "config", "remote.other.push", "HEAD:refs/heads/other").code(t, 0)
	s.git(nil, "push", "origin").code(t, 78)
	s.run(real, nil, "--git-dir="+filepath.Join(s.root, "other.git"), "show-ref", "--verify", "--quiet", "refs/heads/other").code(t, 1)
}

func TestRealGitEditorTransformRejectedInObserve(t *testing.T) {
	s, real := realGitSandbox(t)
	s.env["GIT_EDITOR"] = "/usr/bin/false"
	s.configure(func(c *config.Config) {
		c.Transforms = []config.Transform{transformFixture(s, "editor", map[string]any{"/operations/0/texts/body/value": "new"})}
	})
	before := s.run(real, nil, "rev-parse", "HEAD")
	s.git(nil, "commit", "--allow-empty").code(t, 78)
	after := s.run(real, nil, "rev-parse", "HEAD")
	if !bytes.Equal(before.stdout, after.stdout) {
		t.Fatal("rejected editor edit executed")
	}
}

func TestRealGitTransformRollbackRemovesTemporaryFiles(t *testing.T) {
	s, real := realGitSandbox(t)
	path := filepath.Join(s.cwd, "message")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	s.configure(func(c *config.Config) {
		first := transformFixture(s, "message", map[string]any{"/operations/0/texts/body/value": "changed"})
		last := transformFixture(s, "unsupported", map[string]any{"/operations/0/parameters/unsupported": "value"})
		last.OnError = config.OnErrorAllow
		c.Transforms = []config.Transform{first, last}
	})
	s.git(nil, "commit", "-q", "--allow-empty", "-F", path).code(t, 0)
	got := s.run(real, nil, "log", "-1", "--format=%B")
	got.code(t, 0)
	if strings.TrimSpace(string(got.stdout)) != "original" {
		t.Fatalf("partial chain ran: %s", got.stdout)
	}
	files, err := filepath.Glob(filepath.Join(s.env["TMPDIR"], "gthok-transform-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("rollback leaked temporary files", files, err)
	}
}

func TestRealGitDelegateFallbackOnce(t *testing.T) {
	s, real := realGitSandbox(t)
	marker, delegate := filepath.Join(s.root, "attempts"), filepath.Join(s.root, "delegate")
	if err := os.WriteFile(delegate, []byte("#!/bin/sh\nprintf 'attempt\\n' >> '"+marker+"'\nexit 125\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s.configure(func(c *config.Config) {
		c.Delegates = []config.Delegate{{ID: "read", Match: config.Match{Effects: config.StringList{"read"}, Levels: config.StringList{"L1"}}, Command: []string{delegate}}}
	})
	want := s.run(real, nil, "rev-parse", "HEAD")
	got := s.git(nil, "rev-parse", "HEAD")
	got.code(t, 0)
	if !bytes.Equal(want.stdout, got.stdout) {
		t.Fatalf("fallback output: %s", got.stdout)
	}
	data, err := os.ReadFile(marker)
	version, _ := exec.Command(real, "--version").Output()
	if !strings.HasPrefix(string(version), "git version 2.56.0") {
		// Unverified Git versions are conservative: nothing is retrySafe, so
		// no delegate may run (runtime.gitCompatibility).
		if !os.IsNotExist(err) {
			t.Fatalf("delegate ran on unverified %s: %q", version, data)
		}
		return
	}
	if err != nil || string(data) != "attempt\n" {
		t.Fatal("delegate retried", string(data), err)
	}
}

func TestRealGitUpdateRefPipeStaysLive(t *testing.T) {
	s, _ := realGitSandbox(t)
	s.configure(func(c *config.Config) {
		c.Hooks = []config.Hook{s.hook("input", "pre")}
		c.Runtime.StdinTimeout = config.Duration(20 * time.Second)
	})
	cmd, cancel := s.command(filepath.Join(s.bin, "git"), nil, "update-ref", "--stdin")
	defer cancel()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	cmd.Stdin = reader
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	// A producer waits for Git's response before sending EOF. A read-ahead
	// wrapper deadlocks here until its 20s spool timeout instead of replying.
	timer := time.AfterFunc(8*time.Second, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()
	if _, err := writer.WriteString("start\n"); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "start: ok" {
		t.Fatalf("no live start response: %v", scanner.Err())
	}
	if _, err := writer.WriteString("commit\n"); err != nil {
		t.Fatal(err)
	}
	if !scanner.Scan() || scanner.Text() != "commit: ok" {
		t.Fatalf("no live commit response: %v", scanner.Err())
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("update-ref: %v %s", err, stderr.String())
	}
	input := s.hookInput("input")
	if input.Operations[0].UnknownReason != model.UnknownInputUnavailable {
		t.Fatal(input.Operations)
	}
}

func TestRealGitUpdateRefRegularFileAnalyzedAndReplayed(t *testing.T) {
	s, real := realGitSandbox(t)
	s.run(real, nil, "branch", "topic").code(t, 0)
	s.configure(func(c *config.Config) { c.Hooks = []config.Hook{s.hook("input", "pre")} })
	file, err := os.CreateTemp(s.root, "transaction")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString("start\ndelete refs/heads/topic\ncommit\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	cmd, cancel := s.command(filepath.Join(s.bin, "git"), nil, "update-ref", "--stdin")
	defer cancel()
	cmd.Stdin = file
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("regular transaction: %v %s", err, out)
	}
	input := s.hookInput("input")
	found := false
	for _, op := range input.Operations {
		found = found || op.Action == "ref.delete" && op.Target.Refs.Values[0] == "refs/heads/topic"
	}
	if !found {
		t.Fatal("regular transaction not analyzed", input.Operations)
	}
	s.run(real, nil, "show-ref", "--verify", "--quiet", "refs/heads/topic").code(t, 1)
}
