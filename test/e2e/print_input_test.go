//go:build unix

package e2e

import (
	"encoding/json"
	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/protocol"
	"testing"
)

func TestHooksTestPrintInput(t *testing.T) {
	s := newSandbox(t)
	s.configure(func(c *config.Config) { c.Hooks = []config.Hook{s.hook("pre", "pre")} })
	out := s.management(nil, "hooks", "test", "--no-resolve", "--print-input", "--hook", "pre", "--", "push", "origin", "main")
	out.code(t, 0)
	var input protocol.Input
	if err := json.Unmarshal(out.stdout, &input); err != nil {
		t.Fatal(err)
	}
	if input.Invocation.RealGit != s.real || input.Event != "pre" || len(input.Operations) != 1 {
		t.Fatalf("input: %+v", input)
	}
	s.absentRecord()
}
func TestHooksTestPrintInputInvalidHook(t *testing.T) {
	s := newSandbox(t)
	s.management(nil, "hooks", "test", "--print-input", "--hook", "missing", "--", "status").code(t, 2)
	s.absentRecord()
}
