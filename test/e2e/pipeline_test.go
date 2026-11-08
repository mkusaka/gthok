//go:build unix

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/logging"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestPreHookDecisions(t *testing.T) {
	for _, mode := range []string{"enforce", "observe"} {
		for _, decision := range []string{"allow", "deny", "abstain"} {
			t.Run(mode+"/"+decision, func(t *testing.T) {
				s := newSandbox(t)
				s.configure(func(c *config.Config) {
					c.Policy.Mode = mode
					h := s.hook("pre", "pre")
					h.Env["FAKEGIT_HOOK_DECISION"] = decision
					h.Env["FAKEGIT_HOOK_MESSAGE"] = "consumer supplied reason"
					c.Hooks = []config.Hook{h}
				})
				args := []string{"push", "origin", "1", "-R", "acme/project"}
				result := s.git(nil, args...)
				if mode == "enforce" && decision == "deny" {
					result.code(t, 77)
					s.absentRecord()
					if !bytes.Contains(result.stderr, []byte("gthok: denied by hook pre")) || !bytes.Contains(result.stderr, []byte("consumer supplied reason")) {
						t.Fatal("missing explicit denial/message")
					}
				} else {
					result.code(t, 0)
					s.onlyRecord(args, nil)
					if len(result.stderr) != 0 {
						t.Fatalf("allowed invocation emitted diagnostic: %s", result.stderr)
					}
				}
				input := s.hookInput("pre")
				if input.Event != "pre" || input.Result != nil || input.Policy == nil || input.Policy.Mode != mode {
					t.Fatalf("invalid pre protocol: %+v", input)
				}
				if input.Invocation.IsTTY || input.Invocation.StdinTTY {
					t.Fatal("pipe invocation marked as TTY")
				}
			})
		}
	}
}

func TestHookErrorsAndTimeout(t *testing.T) {
	for _, behavior := range []string{"timeout", "exit", "invalid-response"} {
		for _, onError := range []string{"deny", "allow", "ignore"} {
			t.Run(behavior+"/"+onError, func(t *testing.T) {
				s := newSandbox(t)
				s.configure(func(c *config.Config) {
					yes := true
					c.Logging.Enabled = &yes
					c.Policy.Mode = "enforce"
					h := s.hook("pre", "pre")
					h.OnError = onError
					switch behavior {
					case "timeout":
						h.Timeout = config.Duration(2 * time.Second)
						h.Env["FAKEGIT_HOOK_SLEEP"] = "1m"
					case "exit":
						h.Env["FAKEGIT_HOOK_EXIT"] = "23"
					case "invalid-response":
						h.Env["FAKEGIT_HOOK_RESPONSE"] = "not JSON"
					}
					c.Hooks = []config.Hook{h}
				})
				result := s.git(nil, "version")
				if onError == "deny" {
					result.code(t, 78)
					s.absentRecord()
				} else {
					result.code(t, 0)
					s.onlyRecord([]string{"version"}, nil)
				}
				s.hookInput("pre")
				logs := s.logs()
				wantOutcome := behavior
				if behavior == "exit" {
					wantOutcome = "error"
				}
				wantEffective := onError
				if onError == "ignore" {
					wantEffective = "abstain"
				}
				if len(logs) != 1 || len(logs[0].Hooks) != 1 || string(logs[0].Hooks[0].Outcome) != wantOutcome || string(logs[0].Hooks[0].Effective) != wantEffective {
					t.Fatalf("hook outcome log=%+v", logs)
				}
			})
		}
	}
}

func TestPostHooksCannotChangeStatus(t *testing.T) {
	for _, behavior := range []string{"deny", "exit", "timeout", "invalid-response"} {
		t.Run(behavior, func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				yes := true
				c.Logging.Enabled = &yes
				c.Policy.Mode = "enforce"
				h := s.hook("post", "post")
				switch behavior {
				case "deny":
					h.Env["FAKEGIT_HOOK_DECISION"] = "deny"
				case "exit":
					h.Env["FAKEGIT_HOOK_EXIT"] = "33"
				case "timeout":
					h.Timeout = config.Duration(2 * time.Second)
					h.Env["FAKEGIT_HOOK_SLEEP"] = "1m"
				case "invalid-response":
					h.Env["FAKEGIT_HOOK_RESPONSE"] = "not JSON"
				}
				c.Hooks = []config.Hook{h}
			})
			s.env["FAKEGIT_EXIT"] = "42"
			result := s.git(nil, "version")
			result.code(t, 42)
			s.onlyRecord([]string{"version"}, nil)
			input := s.hookInput("post")
			if input.Event != "post" || input.Result == nil || input.Result.ExitCode != 42 {
				t.Fatalf("post result=%+v", input.Result)
			}
			logs := s.logs()
			wantOutcome := behavior
			if behavior == "exit" {
				wantOutcome = "error"
			}
			if behavior == "deny" {
				wantOutcome = "ok"
			}
			if len(logs) != 1 || len(logs[0].Hooks) != 1 || string(logs[0].Hooks[0].Outcome) != wantOutcome || logs[0].Hooks[0].Effective != protocol.DecisionAbstain {
				t.Fatalf("post outcome log=%+v", logs)
			}
		})
	}
}

func TestObservePolicyNeverBlocks(t *testing.T) {
	s := newSandbox(t)
	s.configure(func(c *config.Config) {
		yes := true
		c.Logging.Enabled = &yes
		c.Policy.Mode = "observe"
		c.Policy.UnknownMutation = "deny"
		c.Policy.Rules = []config.Rule{{ID: "merge", Match: config.Match{Commands: config.StringList{"push"}}, Decision: "deny"}}
		h := s.hook("pre", "pre")
		h.Env["FAKEGIT_HOOK_DECISION"] = "deny"
		c.Hooks = []config.Hook{h}
	})
	args := []string{"push", "origin", "1", "-R", "acme/project"}
	result := s.git(nil, args...)
	result.code(t, 0)
	s.onlyRecord(args, nil)
	if len(result.stderr) != 0 {
		t.Fatal("observe printed enforcing diagnostic")
	}
	input := s.hookInput("pre")
	if input.Policy.Decision == protocol.DecisionDeny {
		t.Fatal("observe policy reported enforce deny")
	}
	if logs := s.logs(); len(logs) != 1 || logs[0].Decision != "would-deny" {
		t.Fatalf("observe decision log=%+v", logs)
	}
}

func TestNestedRecursionProbeAndInvalidConfig(t *testing.T) {
	t.Run("shim-only-PATH", func(t *testing.T) {
		s := newSandbox(t)
		s.env["PATH"] = s.bin
		result := s.git(nil, "version")
		result.code(t, 127)
		s.absentRecord()
	})
	t.Run("depth", func(t *testing.T) {
		s := newSandbox(t)
		s.env["GTHOK_DEPTH"] = "999"
		result := s.git(nil, "version")
		result.code(t, 1)
		s.absentRecord()
	})
	t.Run("probe-before-config-and-discovery", func(t *testing.T) {
		s := newSandbox(t)
		s.env["GTHOK_PROBE"] = "1"
		s.env["PATH"] = s.bin
		if err := os.WriteFile(s.cfg, []byte("invalid: ["), 0600); err != nil {
			t.Fatal(err)
		}
		result := s.git(nil, "anything")
		result.code(t, 0)
		if string(result.stdout) != "gthok-probe\n" || len(result.stderr) != 0 {
			t.Fatal("probe output changed")
		}
		s.absentRecord()
	})
	t.Run("invalid-config", func(t *testing.T) {
		s := newSandbox(t)
		if err := os.WriteFile(s.cfg, []byte("version: 1\nunknownField: true\n"), 0600); err != nil {
			t.Fatal(err)
		}
		result := s.git([]byte{0, 255}, "version")
		result.code(t, 0)
		s.onlyRecord([]string{"version"}, []byte{0, 255})
		if bytes.Count(result.stderr, []byte("\n")) != 1 || !bytes.HasPrefix(result.stderr, []byte("gthok: invalid config")) {
			t.Fatalf("invalid config warning=%q", result.stderr)
		}
		if logs := s.logs(); len(logs) != 1 || logs[0].Decision != "passthrough-invalid-config" || len(logs[0].Diagnostics) == 0 {
			t.Fatalf("invalid config fallback log=%+v", logs)
		}
	})
}

func (s *sandbox) logs() []logging.Record {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(s.cfg), "logs", "gthok.jsonl"))
	if err != nil {
		s.t.Fatal(err)
	}
	var records []logging.Record
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var record logging.Record
		if err := json.Unmarshal(line, &record); err != nil {
			s.t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func TestRoutineConfigNotesAbsentFromLogs(t *testing.T) {
	s := newSandbox(t)
	s.configure(func(c *config.Config) {
		yes := true
		c.Logging.Enabled = &yes
	})
	if err := os.Mkdir(filepath.Join(s.cwd, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.cwd, ".gthok.yaml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result := s.git(nil, "version")
	result.code(t, 0)
	s.onlyRecord([]string{"version"}, nil)
	if len(result.stderr) != 0 {
		t.Fatalf("routine configuration emitted diagnostic: %s", result.stderr)
	}
	records := s.logs()
	if len(records) != 1 {
		t.Fatalf("logs=%+v", records)
	}
	for _, diagnostic := range records[0].Diagnostics {
		if strings.Contains(diagnostic, "Global config:") || strings.Contains(diagnostic, "Project config:") {
			t.Fatalf("routine configuration leaked into diagnostics: %q", diagnostic)
		}
	}
}

func TestInvalidConfigWarningThrottleAndLogging(t *testing.T) {
	s := newSandbox(t)
	input := []byte{0, 'a', '\n', 255}
	args := []string{"api", "graphql", "--input", "-", "-f", "body=unchanged content"}
	writeInvalid := func(body string) {
		t.Helper()
		if err := os.WriteFile(s.cfg, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	invoke := func(warn bool) {
		t.Helper()
		if err := os.Remove(s.record); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		result := s.git(input, args...)
		result.code(t, 0)
		s.onlyRecord(args, input)
		if warn {
			if bytes.Count(result.stderr, []byte("\n")) != 1 || !bytes.HasPrefix(result.stderr, []byte("gthok: invalid config")) {
				t.Fatalf("invalid config warning=%q", result.stderr)
			}
		} else if len(result.stderr) != 0 {
			t.Fatalf("warning was not throttled: %q", result.stderr)
		}
		count++
		records := s.logs()
		if len(records) != count {
			t.Fatalf("invalid config events=%d, want=%d", len(records), count)
		}
		record := records[count-1]
		if record.Decision != "passthrough-invalid-config" || !strings.Contains(strings.Join(record.Diagnostics, "\n"), "unknownField") {
			t.Fatalf("invalid config event lacks error diagnostics: %+v", record)
		}
	}
	writeInvalid("version: 1\nunknownField: true\n")
	invoke(true)
	invoke(false)
	stateDir := filepath.Join(filepath.Dir(s.cfg), "state")
	warnings, err := filepath.Glob(filepath.Join(stateDir, "invalid-config-*.warning"))
	if err != nil || len(warnings) != 1 {
		t.Fatalf("warning state=%v, err=%v", warnings, err)
	}
	for path, want := range map[string]os.FileMode{stateDir: 0700, warnings[0]: 0600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode=%#o, want=%#o", path, info.Mode().Perm(), want)
		}
	}
	timestamp, err := os.ReadFile(warnings[0])
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(timestamp)))
	if err != nil || time.Since(stamp) > time.Minute {
		t.Fatalf("warning timestamp=%q, err=%v", timestamp, err)
	}
	old := time.Now().Add(-11*time.Minute).UTC().Format(time.RFC3339Nano) + "\n"
	if err := os.WriteFile(warnings[0], []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	invoke(true)
	invoke(false)
	writeInvalid("version: 1\nunknownFieldChanged: true\n")
	invoke(true)
	invoke(false)
	changed, err := filepath.Glob(filepath.Join(stateDir, "invalid-config-*.warning"))
	if err != nil || len(changed) != 2 {
		t.Fatalf("changed-error warning state=%v, err=%v", changed, err)
	}
	// Returning to the original error reuses that pair's independent throttle.
	writeInvalid("version: 1\nunknownField: true\n")
	invoke(false)
	// A state I/O failure must never hide the warning or prevent execution.
	if err := os.Remove(warnings[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(warnings[0], 0700); err != nil {
		t.Fatal(err)
	}
	invoke(true)
	invoke(true)
}

func TestNoConfigDefaultsAndLoggingFailures(t *testing.T) {
	for _, debug := range []bool{false, true} {
		t.Run(fmt.Sprintf("log-failure/debug=%v", debug), func(t *testing.T) {
			s := newSandbox(t)
			bad := filepath.Join(s.root, "not-directory")
			if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			s.configure(func(c *config.Config) {
				yes := true
				c.Logging.Enabled = &yes
				c.Logging.Path = filepath.Join(bad, "log")
			})
			if debug {
				s.env["GTHOK_DEBUG"] = "1"
			}
			result := s.git(nil, "version")
			result.code(t, 0)
			s.onlyRecord([]string{"version"}, nil)
			if (len(result.stderr) > 0) != debug {
				t.Fatalf("debug diagnostic=%q", result.stderr)
			}
		})
	}
}

func waitFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if body, err := os.ReadFile(path); err == nil && len(body) > 0 {
			return body
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for fixture readiness: %s", filepath.Base(path))
	return nil
}

func TestSignalDuringPreHookCancelsAndNeverStartsGh(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(strconv.Itoa(int(sig)), func(t *testing.T) {
			s := newSandbox(t)
			ready := filepath.Join(s.root, "hook-ready")
			s.configure(func(c *config.Config) {
				c.Policy.Mode = "enforce"
				h := s.hook("pre", "pre")
				h.Env["FAKEGIT_HOOK_SLEEP"] = "1m"
				h.Env["FAKEGIT_HOOK_READY"] = ready
				c.Hooks = []config.Hook{h}
			})
			cmd, cancel := s.command(filepath.Join(s.bin, "git"), nil, "version")
			defer cancel()
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
			pid, err := strconv.Atoi(string(waitFile(t, ready)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			result := outcome{err: cmd.Wait(), stdout: stdout.Bytes(), stderr: stderr.Bytes()}
			result.signaled(t, sig)
			s.absentRecord()
			s.hookInput("pre")
			waitProcessGone(t, pid)
		})
	}
}

func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		} else if err != nil {
			t.Fatalf("checking helper pid=%d: %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("helper remains alive after cleanup: pid=%d", pid)
}

func TestSignalDuringPostHookKeepsChildStatusAndReapsHook(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		for _, exit := range []int{0, 42} {
			t.Run(fmt.Sprintf("signal=%d/exit=%d", sig, exit), func(t *testing.T) {
				s := newSandbox(t)
				ready := filepath.Join(s.root, "hook-ready")
				s.configure(func(c *config.Config) {
					yes := true
					c.Logging.Enabled = &yes
					c.Hooks = []config.Hook{{
						ID: "post", Phase: config.PhasePost, Timeout: config.Duration(time.Minute),
						Command: []string{"/bin/sh", "-c", `printf '%s' "$$" > "$1"; exec /bin/sleep 30`, "hook", ready},
					}}
				})
				s.env["FAKEGIT_EXIT"] = strconv.Itoa(exit)
				args := []string{"show", "HEAD", "1"}
				cmd, cancel := s.command(filepath.Join(s.bin, "git"), nil, args...)
				defer cancel()
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
				pid, err := strconv.Atoi(string(waitFile(t, ready)))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
				if err := cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				result := outcome{err: cmd.Wait(), stdout: stdout.Bytes(), stderr: stderr.Bytes()}
				result.code(t, exit)
				waitProcessGone(t, pid)
				s.onlyRecord(args, nil)
				logs := s.logs()
				if len(logs) != 1 || logs[0].Exec == nil || logs[0].Exec.ExitCode != exit ||
					len(logs[0].Hooks) != 1 || logs[0].Hooks[0].Outcome != protocol.OutcomeCanceled ||
					!strings.Contains(strings.Join(logs[0].Diagnostics, "; "), "post-hooks interrupted by SIG") {
					t.Fatalf("post interruption log=%+v", logs)
				}
			})
		}
	}
}

func TestHelperPhasesPreserveIgnoredStartupSignals(t *testing.T) {
	for _, phase := range []string{config.PhasePre, config.PhasePost} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP} {
			t.Run(fmt.Sprintf("%s/signal=%d", phase, sig), func(t *testing.T) {
				s := newSandbox(t)
				ready, fifo := filepath.Join(s.root, "hook-ready"), filepath.Join(s.root, "release")
				if err := syscall.Mkfifo(fifo, 0600); err != nil {
					t.Fatal(err)
				}
				release, err := os.OpenFile(fifo, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = release.Close() }()
				s.configure(func(c *config.Config) {
					yes := true
					c.Logging.Enabled = &yes
					c.Hooks = []config.Hook{{
						ID: phase, Phase: phase,
						Command: []string{"/bin/sh", "-c", `printf '%s' "$$" > "$1"; read -r line < "$2"; printf '{"decision":"abstain"}'`, "hook", ready, fifo},
					}}
				})
				cmd, cancel := s.command("/bin/sh", nil, "-c", `trap '' INT HUP; exec "$@"`, "ignored", filepath.Join(s.bin, "git"), "version")
				defer cancel()
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
				pid, err := strconv.Atoi(string(waitFile(t, ready)))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
				if err := cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				if _, err := release.Write([]byte("continue\n")); err != nil {
					t.Fatal(err)
				}
				result := outcome{err: cmd.Wait(), stdout: stdout.Bytes(), stderr: stderr.Bytes()}
				result.code(t, 0)
				waitProcessGone(t, pid)
				s.onlyRecord([]string{"version"}, nil)
				logs := s.logs()
				if len(logs) != 1 || len(logs[0].Hooks) != 1 || logs[0].Hooks[0].Outcome != protocol.OutcomeOK {
					t.Fatalf("ignored signal interrupted helper: %+v", logs)
				}
			})
		}
	}
}

// Post hooks can be limited to git's outcome with match.result / match.exitCode;
// non-matching ones are not run at all.
func TestPostHooksMatchOutcome(t *testing.T) {
	for _, tc := range []struct {
		exit    string
		code    int
		ran     []string
		skipped []string
	}{
		{"0", 0, []string{"on-success", "on-zero"}, []string{"on-failure", "on-42"}},
		{"42", 42, []string{"on-failure", "on-42"}, []string{"on-success", "on-zero"}},
		{"1", 1, []string{"on-failure"}, []string{"on-success", "on-zero", "on-42"}},
	} {
		t.Run("exit"+tc.exit, func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				success, failure, zero, fortyTwo := s.hook("on-success", "post"), s.hook("on-failure", "post"), s.hook("on-zero", "post"), s.hook("on-42", "post")
				success.Match.Results = config.StringList{config.ResultSuccess}
				failure.Match.Results = config.StringList{config.ResultFailure}
				zero.Match.ExitCodes = []int{0}
				fortyTwo.Match.ExitCodes = []int{42}
				c.Hooks = []config.Hook{success, failure, zero, fortyTwo}
			})
			s.env["FAKEGIT_EXIT"] = tc.exit
			s.git(nil, "version").code(t, tc.code)
			for _, id := range tc.ran {
				if in := s.hookInput(id); in.Result == nil || in.Result.ExitCode != tc.code {
					t.Fatalf("%s: result=%+v", id, in.Result)
				}
			}
			for _, id := range tc.skipped {
				if _, err := os.Stat(filepath.Join(s.root, id+".json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("%s ran for exit %s", id, tc.exit)
				}
			}
		})
	}
}

// match.token distinguishes GitHub App installation tokens (gain, Actions)
// from user credentials, e.g. to forbid PR writes authored by an App.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
