//go:build unix

package delegates

import (
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize"
	"github.com/mkusaka/gthok/internal/policy"
)

func joinArgs(args []string) string {
	name := ""
	for _, arg := range args {
		name += arg + " "
	}
	return name
}

func TestSelectionRequiresEntireReadOnlyRequest(t *testing.T) {
	c := &config.Config{Delegates: []config.Delegate{{ID: "read", Match: config.Match{Effects: config.StringList{"read"}}, Command: []string{"relay"}}}}
	r := normalize.Normalize(model.Invocation{Argv: []string{"pr", "view", "1"}})
	r.Operations = append(r.Operations, model.Operation{Effect: model.EffectWrite, Execution: model.ExecutionWillRun})
	if Select(c, policy.Subject{Result: r}) != nil {
		t.Fatal("read match hid a second writing operation")
	}
}
