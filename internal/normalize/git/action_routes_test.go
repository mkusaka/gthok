package git

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func TestEveryActionHasRoutes(t *testing.T) {
	routes := ActionRoutes()
	for _, spec := range actions.All() {
		if len(routes[spec.Action]) == 0 {
			t.Errorf("missing routes for %s", spec.Action)
		}
	}
	for action, examples := range routes {
		if _, ok := actions.Lookup(action); !ok {
			t.Errorf("stale route for %s", action)
		}
		for _, example := range examples {
			if action == "unknown" || action == "git.info.read" {
				continue
			}
			if _, err := Parse(strings.Fields(example)); err != nil {
				t.Errorf("%s example %q: %v", action, example, err)
			}
		}
	}
}

func TestManualCommandAdapters(t *testing.T) {
	for _, tc := range []struct {
		args, action string
	}{
		{"fetch-pack --all -q -k --thin --depth=1 --filter=blob:none ../source refs/heads/main", "remote.pack.fetch"},
		{"fetch-pack --upload-pack=custom --no-progress --shallow-since=yesterday ../source", "remote.pack.fetch"},
		{"reflog --oneline -n 5 --date=iso", "ref.reflog.show"},
		{"reflog show --oneline -5 --date=iso HEAD", "ref.reflog.show"},
		{"bisect start --no-checkout --first-parent --term-good old --term-bad=new HEAD HEAD~2", "history.bisect.start"},
		{"bisect start --reset-when-found=original HEAD HEAD~2", "history.bisect.start"},
		{"bisect terms --term-good", "history.bisect.terms"},
		{"bisect terms --term-new", "history.bisect.terms"},
		{"bisect run sh -c true --arbitrary-child-option --help", "history.bisect.run"},
	} {
		t.Run(tc.args, func(t *testing.T) {
			r := Normalize(model.Invocation{Argv: strings.Fields(tc.args)})
			if len(r.Errors) > 0 || len(r.Operations) != 1 || r.Operations[0].Action != tc.action {
				t.Fatal(r)
			}
		})
	}
	child := []string{"sh", "-c", "echo test", "--help", "--unknown", "-x"}
	r := Normalize(model.Invocation{Argv: append([]string{"bisect", "run"}, child...)})
	if !reflect.DeepEqual(r.Operations[0].Parameters["command"], child) {
		t.Fatal(r)
	}
	for _, argv := range [][]string{{"fetch-pack", "--depth", "1", "repo"}, {"bisect", "start", "--unknown"}, {"reflog", "show", "--unknown"}, {"reflog", "expire", "--output=report"}, {"reflog", "show", "-L", "1,2:file"}} {
		r := Normalize(model.Invocation{Argv: argv})
		if r.Operations[0].UnknownReason == model.UnknownNone {
			t.Fatalf("accepted invalid options: %q: %+v", argv, r)
		}
	}
}
