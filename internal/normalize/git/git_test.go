package git

import (
	"github.com/mkusaka/gthok/internal/model"
	"reflect"
	"testing"
)

func TestBootstrapNormalize(t *testing.T) {
	r := Normalize(model.Invocation{Argv: []string{"-C", "/repo", "push", "origin", "main"}})
	if r.SchemaVersion != 1 || r.NormalizerVersion != model.NormalizerVersion || r.Registry.Git != "v2.56.0" || r.Registry.Commit != "a018953688f1b10bddf91bff8747068f5f4746a4" || r.Classification.Level != model.LevelL1 || r.Classification.RetrySafe || r.Classification.MutationFree || len(r.Operations) != 1 || r.Operations[0].UnknownReason != model.UnknownCommand || r.Operations[0].Coverage != model.CoverageOpaque {
		t.Fatalf("result: %+v", r)
	}
	if r := Normalize(model.Invocation{}); r.Classification.Level != model.LevelNone {
		t.Fatal(r)
	}
}
func TestExpandAlias(t *testing.T) {
	aliases := map[string]string{"p": "push origin", "shell": "!echo hi", "status": "push"}
	argv := []string{"-C", "repo", "p", "main"}
	got, alias, reason := ExpandAlias(argv, aliases)
	if alias != "p" || reason != model.UnknownNone || !reflect.DeepEqual(got, []string{"-C", "repo", "push", "origin", "main"}) || argv[2] != "p" {
		t.Fatalf("expanded: %q %s %s", got, alias, reason)
	}
	if _, _, reason := ExpandAlias([]string{"shell"}, aliases); reason != model.UnknownShellAlias {
		t.Fatal(reason)
	}
	if got, _, _ := ExpandAlias([]string{"status"}, aliases); got[0] != "status" {
		t.Fatal(got)
	}
}
func TestStdin(t *testing.T) {
	for _, argv := range [][]string{{"update-ref", "--stdin"}, {"cat-file", "--batch"}, {"fast-import"}, {"credential", "fill"}} {
		if yes, _ := NeedsStdin(argv); yes {
			t.Fatalf("bidirectional protocol read ahead: %q", argv)
		}
	}
	if yes, which := NeedsStdin([]string{"commit", "-F", "-"}); !yes || which != "commit-message" {
		t.Fatalf("stdin: %v %s", yes, which)
	}
}
