//go:build unix

package e2e

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
)

type gitRepositoryState struct {
	Refs, Head, Index, Config []byte
	Files                     map[string]string
}

func gitState(t *testing.T, s *sandbox, real string) gitRepositoryState {
	t.Helper()
	read := func(args ...string) []byte {
		t.Helper()
		out := s.run(real, nil, args...)
		out.code(t, 0)
		return out.stdout
	}
	state := gitRepositoryState{
		Refs:   read("for-each-ref", "--format=%(refname) %(objectname) %(symref)"),
		Head:   read("rev-parse", "HEAD"),
		Index:  read("ls-files", "-s", "-z"),
		Config: read("config", "--local", "--null", "--list"),
		Files:  make(map[string]string),
	}
	head, err := os.ReadFile(filepath.Join(s.cwd, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	state.Head = append(state.Head, head...)
	if err := filepath.WalkDir(s.cwd, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.cwd, path)
		if err != nil {
			return err
		}
		state.Files[rel] = info.Mode().String() + ":" + string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestGitExecutionComparison(t *testing.T) {
	direct, real := gitFixture(t)
	wrapped, _ := gitFixture(t)
	binary, err := os.ReadFile("../fixtures/git/binary-blob")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*sandbox{direct, wrapped} {
		// Fixed commit metadata makes object IDs comparable between independent repos.
		s.env["GIT_AUTHOR_DATE"], s.env["GIT_COMMITTER_DATE"] = "2001-01-01T00:00:00Z", "2001-01-01T00:00:00Z"
		s.run(real, nil, "commit", "-q", "--amend", "--allow-empty", "--no-edit", "--date=2001-01-01T00:00:00Z").code(t, 0)
		for name, data := range map[string][]byte{"tracked": []byte("initial\n"), "binary": binary, "--literal": []byte("dash\n")} {
			if err := os.WriteFile(filepath.Join(s.cwd, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		s.run(real, nil, "add", "--", "tracked", "binary", "--literal").code(t, 0)
		s.run(real, nil, "commit", "-q", "-m", "files").code(t, 0)
		s.run(real, nil, "config", "alias.inspect", "status --porcelain=v1 -z").code(t, 0)
		if err := os.WriteFile(filepath.Join(s.cwd, "tracked"), []byte("modified\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(s.cwd, "untracked"), []byte("untracked\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	wrapped.configure(func(c *config.Config) {
		c.Policy.Mode = config.ModeObserve
		c.Hooks = []config.Hook{wrapped.hook("comparison", "pre")}
	})
	if want, got := gitState(t, direct, real), gitState(t, wrapped, real); !reflect.DeepEqual(got, want) {
		t.Fatalf("comparison repositories did not start in the same state\ngot: %+v\nwant: %+v", got, want)
	}
	for _, tc := range []struct {
		name  string
		args  []string
		input []byte
		code  int
	}{
		{"status", []string{"status", "--porcelain=v1", "-z"}, nil, 0},
		{"alias", []string{"inspect"}, nil, 0},
		{"inline alias", []string{"-c", "alias.inspect=diff --stat", "inspect"}, nil, 0},
		{"binary stdout", []string{"cat-file", "blob", "HEAD:binary"}, nil, 0},
		{"stdin hash", []string{"hash-object", "-w", "--stdin"}, binary, 0},
		{"diff", []string{"diff", "--binary", "--no-ext-diff", "--no-textconv"}, nil, 0},
		{"argv terminator", []string{"add", "--", "--literal", "tracked"}, nil, 0},
		{"path reset", []string{"reset", "-q", "HEAD", "--", "tracked"}, nil, 0},
		{"restore", []string{"restore", "--", "tracked"}, nil, 0},
		{"branch", []string{"branch", "topic"}, nil, 0},
		{"stdin ref update", []string{"update-ref", "--stdin"}, []byte("create refs/heads/from-stdin HEAD\n"), 0},
		{"switch", []string{"switch", "-q", "topic"}, nil, 0},
		{"commit stdin", []string{"commit", "-q", "--allow-empty", "-F-"}, []byte("stdin message\n\nsecond paragraph\n"), 0},
		{"config", []string{"config", "set", "fixture.value", "two words"}, nil, 0},
		{"clean preview", []string{"clean", "-n"}, nil, 0},
		{"clean", []string{"clean", "-f"}, nil, 0},
		{"usage 129", []string{"branch", "--gthok-invalid-option"}, nil, 129},
		{"missing revision", []string{"rev-parse", "--verify", "refs/heads/missing"}, nil, 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, got := direct.run(real, tc.input, tc.args...), wrapped.git(tc.input, tc.args...)
			want.code(t, tc.code)
			got.code(t, tc.code)
			if !bytes.Equal(got.stdout, want.stdout) || !bytes.Equal(got.stderr, want.stderr) {
				t.Fatalf("execution differs for %q\nstdout got=%q want=%q\nstderr got=%q want=%q", tc.args, got.stdout, want.stdout, got.stderr, want.stderr)
			}
			if tc.name == "binary stdout" && !bytes.Equal(got.stdout, binary) {
				t.Fatalf("binary fixture changed: %x", got.stdout)
			}
			input := wrapped.hookInput("comparison")
			if input.Invocation.Via != "shim" || !reflect.DeepEqual(input.Invocation.Argv, tc.args) || input.Policy == nil || input.Policy.Mode != config.ModeObserve {
				t.Fatalf("full shim pipeline/argv: %+v", input)
			}
			if want, got := gitState(t, direct, real), gitState(t, wrapped, real); !reflect.DeepEqual(got, want) {
				t.Fatalf("repository state differs after %q\ngot: %+v\nwant: %+v", tc.args, got, want)
			}
		})
	}
}

func TestGitExecutionSIGINTComparison(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		name := "direct"
		if wrapped {
			name = "shim"
		}
		t.Run(name, func(t *testing.T) {
			s, real := gitFixture(t)
			path := real
			if wrapped {
				path = filepath.Join(s.bin, "git")
			}
			cmd, cancel := s.command(path, nil, "cat-file", "--batch-check")
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
			// The live response proves real Git has started, without sleeps or GIT_TRACE.
			if _, err := writer.WriteString("HEAD\n"); err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || !strings.Contains(line, " commit ") {
				t.Fatalf("real Git not ready: %q %v", line, err)
			}
			if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
				t.Fatal(err)
			}
			err = cmd.Wait()
			out := outcome{stderr: stderr.Bytes(), err: err}
			out.signaled(t, syscall.SIGINT)
			if len(stderr.Bytes()) != 0 {
				t.Fatalf("signal diagnostic changed stderr: %q", stderr.Bytes())
			}
		})
	}
}
