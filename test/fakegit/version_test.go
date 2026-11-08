//go:build unix

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestFakeGitVersionHelper(t *testing.T) {
	if os.Getenv("FAKEGIT_TEST_HELPER") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("FAKEGIT_TEST_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	os.Args = append([]string{"fakegit"}, args...)
	main()
	os.Exit(0)
}

func fakeGitVersionCommand(t *testing.T, args []string, env ...string) (*exec.Cmd, string) {
	t.Helper()
	root := t.TempDir()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFakeGitVersionHelper$")
	cmd.Dir = root
	path := filepath.Join(root, "record.jsonl")
	cmd.Env = append([]string{"HOME=" + root, "PATH=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "FAKEGIT_TEST_HELPER=1", "FAKEGIT_TEST_ARGS=" + string(data), "FAKEGIT_RECORD=" + path}, env...)
	return cmd, path
}

func fakeGitVersionRecord(t *testing.T, path string) record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got record
	// Unmarshal also rejects more than one JSONL record.
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTaggedVersionProbeIgnoresRequestBehaviorAndStdin(t *testing.T) {
	for _, mode := range []string{"", "hook", "pty"} {
		t.Run("mode="+mode, func(t *testing.T) {
			cmd, path := fakeGitVersionCommand(t, []string{"--version"},
				"GTHOK_VERSION_QUERY=1", "FAKEGIT_MODE="+mode,
				"FAKEGIT_EXIT=23", "FAKEGIT_SIGNAL=15", "FAKEGIT_SLEEP=1h", "FAKEGIT_CATCH_TERM_EXIT=42",
				"FAKEGIT_STDOUT_B64="+base64.StdEncoding.EncodeToString([]byte("request output")),
				"FAKEGIT_STDERR_B64="+base64.StdEncoding.EncodeToString([]byte("request error")),
				"FAKEGIT_HOOK_EXIT=24", "FAKEGIT_HOOK_SLEEP=1h", "FAKEGIT_HOOK_RESPONSE=invalid",
			)
			ready := filepath.Join(filepath.Dir(path), "ready")
			cmd.Env = append(cmd.Env, "FAKEGIT_STARTED="+ready, "FAKEGIT_READY="+ready, "FAKEGIT_HOOK_READY="+ready)
			stdin, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stdin.Close(); _ = writer.Close() }()
			// Keeping the writer open proves the helper does not read to EOF.
			cmd.Stdin = stdin
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil || stdout.String() != "git version 2.56.0\n" || stderr.Len() != 0 {
				t.Fatalf("probe: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
			got := fakeGitVersionRecord(t, path)
			if got.Kind != "version" || !slices.Equal(got.Argv, []string{"--version"}) || got.StdinLength != 0 || got.StdinBase64 != "" {
				t.Fatalf("probe record=%+v", got)
			}
			if _, err := os.Stat(ready); !os.IsNotExist(err) {
				t.Fatalf("probe triggered request readiness: %v", err)
			}
		})
	}
}

func TestVersionProbeRequiresExactTagAndArguments(t *testing.T) {
	for _, tc := range []struct {
		name, tag string
		args      []string
	}{
		{"untagged original or doctor", "", []string{"--version"}},
		{"disabled tag", "0", []string{"--version"}},
		{"command form", "1", []string{"version"}},
		{"extra operand", "1", []string{"--version", "extra"}},
		{"Git globals", "1", []string{"-C", "repo", "--version"}},
		{"no arguments", "1", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte("original stdin")
			cmd, path := fakeGitVersionCommand(t, tc.args,
				"GTHOK_VERSION_QUERY="+tc.tag, "FAKEGIT_EXIT=23",
				"FAKEGIT_STDOUT_B64="+base64.StdEncoding.EncodeToString([]byte("original output")),
			)
			cmd.Stdin = bytes.NewReader(input)
			out, err := cmd.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 23 || string(out) != "original output" {
				t.Fatalf("original request: err=%v output=%q", err, out)
			}
			got := fakeGitVersionRecord(t, path)
			if got.Kind != "execution" || !slices.Equal(got.Argv, tc.args) || got.StdinLength != len(input) || got.StdinBase64 != base64.StdEncoding.EncodeToString(input) {
				t.Fatalf("original request hidden or changed: %+v", got)
			}
		})
	}
}
