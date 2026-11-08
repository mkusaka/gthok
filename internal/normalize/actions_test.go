package normalize

import (
	"github.com/mkusaka/gthok/internal/normalize/actions"
	"testing"
)

func TestCuratedGitActions(t *testing.T) {
	all := actions.All()
	if len(all) < 16 {
		t.Fatalf("actions: %d", len(all))
	}
	for i, spec := range all {
		if got, ok := actions.Lookup(spec.Action); !ok || got != spec || actions.Must(spec.Action) != spec {
			t.Fatal(spec)
		}
		if i > 0 && all[i-1].Action >= spec.Action {
			t.Fatal("actions not sorted")
		}
	}
	if !KnownActionPattern("remote.push.*") || KnownActionPattern("pr.merge.*") || KnownActionPattern("rest.*") || KnownActionPattern("[") {
		t.Fatal("catalogue includes obsolete or invalid actions")
	}
	all[0].Action = "changed"
	if actions.All()[0].Action == "changed" {
		t.Fatal("All aliases catalogue")
	}
}
