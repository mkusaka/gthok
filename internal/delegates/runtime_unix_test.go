//go:build unix

package delegates

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/policy"
	"github.com/mkusaka/gthok/internal/process"
)

func TestGitDelegateSelectionRequiresRetrySafety(t *testing.T) {
	c := &config.Config{Dir: "/config", Delegates: []config.Delegate{
		{ID: "first", Match: config.Match{Effects: config.StringList{"read"}}, Command: []string{"./first"}},
		{ID: "second", Command: []string{"./second"}},
	}}
	r := model.Result{Classification: model.Classification{Level: model.LevelL1, Confidence: model.ConfidenceExact, MutationFree: true, RetrySafe: true}, Operations: []model.Operation{{Action: "config.get", Effect: model.EffectRead, Execution: model.ExecutionWillRun}}}
	if got := Select(c, policy.Subject{Result: r}); got == nil || got.ID != "first" || got.Command[0] != "/config/first" {
		t.Fatalf("selection: %+v", got)
	}
	r.Classification.RetrySafe = false
	if Select(c, policy.Subject{Result: r}) != nil {
		t.Fatal("unsafe retry selected")
	}
	r.Classification.RetrySafe = true
	if Select(c, policy.Subject{Result: r, Env: map[string]string{"GTHOK_NO_DELEGATE": "1"}}) != nil {
		t.Fatal("nested delegation allowed")
	}
}

func TestDelegateFallbackAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name, body, onError string
		want                int
		fallback            bool
		signal              syscall.Signal
	}{
		{name: "not-handled", body: "exit 125", want: 42, fallback: true},
		{name: "partial-input", body: "/bin/dd bs=1 count=2 >/dev/null 2>/dev/null; exit 125", want: 42, fallback: true},
		{name: "stdout-125", body: "printf output; exit 125", want: 78},
		{name: "start-fallback", want: 42, fallback: true},
		{name: "start-deny", onError: config.OnErrorDeny, want: 78},
		{name: "silent-timeout", body: "exec /bin/sleep 10", want: 42, fallback: true},
		{name: "timeout-deny", body: "exec /bin/sleep 10", onError: config.OnErrorDeny, want: 78},
		{name: "output-timeout", body: "printf output; exec /bin/sleep 30", want: 78},
		{name: "ordinary-exit", body: "exit 23", want: 23},
		{name: "signal", body: "kill -TERM $$", want: -1, signal: syscall.SIGTERM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			record, real, relay := filepath.Join(root, "input"), filepath.Join(root, "git"), filepath.Join(root, "relay")
			if err := os.WriteFile(real, []byte("#!/bin/sh\n/bin/cat > '"+record+"'\nexit 42\n"), 0700); err != nil {
				t.Fatal(err)
			}
			if tc.body != "" {
				if err := os.WriteFile(relay, []byte("#!/bin/sh\n"+tc.body+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			input := []byte{0, 'a', 255, 'b', '\n'}
			spool, err := process.SpoolBytes(input)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = spool.Close() }()
			timeout := 5 * time.Second
			if strings.Contains(tc.body, "/bin/sleep 10") {
				timeout = 2 * time.Second
			}
			// The relay must reach printf before the deadline; a loaded runner
			// can take seconds to start sh, which is a genuinely silent timeout.
			selected := &Selection{ID: "relay", Command: []string{relay}, Timeout: config.Duration(timeout), OnError: tc.onError}
			warnings := 0
			result := Run(selected, nil, process.Options{Path: real, Args: []string{"git", "config", "--get", "user.name"}, Env: []string{"HOME=" + root, "PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}, Stdin: spool, Capture: true, CaptureLimit: 1024}, func(string) { warnings++ })
			if result.StartErr != nil || result.ExitCode != tc.want || result.Signal != tc.signal {
				t.Fatalf("execution: %+v", result)
			}
			data, err := os.ReadFile(record)
			if tc.fallback {
				if err != nil || !bytes.Equal(data, input) || warnings != 1 {
					t.Fatalf("fallback input=%q err=%v warnings=%d", data, err, warnings)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("real Git executed after unsafe fallback")
			}
		})
	}
}
