//go:build unix

package e2e

import (
	"github.com/mkusaka/gthok/internal/config"
	"testing"
)

func transformFixture(s *sandbox, id string, set map[string]any) config.Transform {
	s.t.Helper()
	h := s.hook(id, "pre")
	h.Env["FAKEGIT_HOOK_RESPONSE"] = string(mustJSON(s.t, map[string]any{"schemaVersion": 1, "set": set}))
	return config.Transform{ID: id, Command: h.Command, Env: h.Env}
}
func TestUnsupportedGitEditFailsClosed(t *testing.T) {
	for _, mode := range []string{config.ModeObserve, config.ModeEnforce} {
		s := newSandbox(t)
		s.configure(func(c *config.Config) {
			c.Policy.Mode = mode
			c.Transforms = []config.Transform{transformFixture(s, "unsupported", map[string]any{"/operations/0/parameters/force": true})}
		})
		s.git(nil, "push", "origin", "main").code(t, 78)
		s.absentRecord()
	}
}
