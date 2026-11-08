//go:build unix

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/install"
)

func TestWrapperExecFailureLog(t *testing.T) {
	s := newSandbox(t)
	s.configure(func(c *config.Config) {
		yes := true
		c.Logging.Enabled = &yes
	})
	if err := os.WriteFile(s.real, []byte("not an executable\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s.git(nil, "version").code(t, 126)
	s.absentRecord()
	records := s.logs()
	last := records[len(records)-1]
	if last.Decision != "exec-error" || last.Exec == nil || last.Exec.StartError == "" || last.Exec.ExitCode != 126 {
		t.Fatalf("failed exec log=%+v", last)
	}
	if len(records) != 2 || records[0].InvocationID != last.InvocationID {
		t.Fatalf("expected pre-exec and failure records for one invocation: %+v", records)
	}
}

func TestWrapperNestedHookLifecycle(t *testing.T) {
	for _, phase := range []string{"pre", "post"} {
		t.Run(phase, func(t *testing.T) {
			s := newSandbox(t)
			s.configure(func(c *config.Config) {
				c.Policy.Mode = config.ModeEnforce
				h := s.hook("nested", phase)
				h.Command = []string{"/bin/sh", "-c", `/bin/cat >/dev/null; git version || exit; printf '{"decision":"allow"}'`}
				h.Env = map[string]string{"GTHOK_NESTED_TOKEN": "cannot override invocation token"}
				c.Hooks = []config.Hook{h}
			})
			s.git(nil, "version").code(t, 0)
			records := s.records()
			if len(records) != 2 {
				t.Fatalf("nested and user git calls=%+v", records)
			}
			nestedIndex := 0
			if phase == "post" {
				nestedIndex = 1
			}
			helper := envMap(records[nestedIndex].Env)
			if helper["GTHOK_NESTED"] != "1" || len(helper["GTHOK_NESTED_TOKEN"]) != 64 || helper["GTHOK_DEPTH"] != "2" {
				t.Fatalf("nested helper env=%v", helper)
			}
			if install.ValidNestedToken(filepath.Dir(s.cfg), helper) {
				t.Fatal("hook token survived wrapper exit/exec")
			}
			if child := envMap(records[1-nestedIndex].Env); child["GTHOK_NESTED_TOKEN"] != "" {
				t.Fatal("helper token leaked to original git invocation")
			}
		})
	}
}
