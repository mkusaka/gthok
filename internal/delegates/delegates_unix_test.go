//go:build unix

package delegates

import (
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize"
	"github.com/mkusaka/gthok/internal/policy"
)

func TestGitSelectionRequiresRetrySafety(t *testing.T) {
	c := &config.Config{Delegates: []config.Delegate{{ID: "read", Command: []string{"relay"}}}}
	for _, tc := range []struct {
		argv     []string
		eligible bool
	}{
		{[]string{"rev-parse", "HEAD"}, true},
		{[]string{"cat-file", "-t", "HEAD"}, true},
		{[]string{"status"}, false},
		{[]string{"diff", "--output=patch"}, false},
		{[]string{"update-ref", "-d", "refs/heads/topic"}, false},
	} {
		r := normalize.Normalize(model.Invocation{Argv: tc.argv})
		if selected := Select(c, policy.Subject{Result: r}); (selected != nil) != tc.eligible {
			t.Fatalf("%q: selected=%+v classification=%+v", tc.argv, selected, r.Classification)
		}
	}
}

func TestSelectionRequiresEntireReadOnlyRequest(t *testing.T) {
	c := &config.Config{Delegates: []config.Delegate{{ID: "read", Match: config.Match{Effects: config.StringList{"read"}}, Command: []string{"relay"}}}}
	r := normalize.Normalize(model.Invocation{Argv: []string{"pr", "view", "1"}})
	r.Operations = append(r.Operations, model.Operation{Effect: model.EffectWrite, Execution: model.ExecutionWillRun})
	if Select(c, policy.Subject{Result: r}) != nil {
		t.Fatal("read match hid a second writing operation")
	}
}
