package git

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestNormalizePush(t *testing.T) {
	argv := []string{"-C", "/tmp", "-c", "a.b=c", "push", "--force", "origin", "HEAD:refs/heads/main"}
	r := Normalize(model.Invocation{Argv: argv})
	if r.SchemaVersion != 1 || r.NormalizerVersion != model.NormalizerVersion || r.Registry.Git != "v2.56.0" || r.Registry.Commit != "a018953688f1b10bddf91bff8747068f5f4746a4" || r.Classification.Level != model.LevelL1 || r.Classification.RetrySafe || r.Classification.MutationFree || len(r.Operations) != 1 {
		t.Fatalf("result: %+v", r)
	}
	op := r.Operations[0]
	if op.Action != "remote.push.update" || op.Target.Remote != "origin" || !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/heads/main")) || op.Parameters["forceMode"] != "unconditional" || !reflect.DeepEqual(r.Source.Global, argv[:4]) {
		t.Fatalf("push: %+v, source %+v", op, r.Source)
	}
	for _, args := range [][]string{{"push", "origin", "+HEAD:refs/heads/main"}, {"push", "--force", "--rep=origin", "HEAD:refs/heads/main"}, {"push", "-qf", "origin", "HEAD:refs/heads/main"}} {
		r := Normalize(model.Invocation{Argv: args})
		if r.Operations[0].Parameters["forceMode"] != "unconditional" {
			t.Fatalf("%q: %+v", args, r)
		}
	}
	for _, args := range [][]string{{"push"}, {"push", "--mirror", "origin"}, {"push", "origin", ":"}} {
		r := Normalize(model.Invocation{Argv: args})
		if r.Operations[0].Target.Refs.Kind != model.SetUnknown || r.Classification.Resolution == model.ResolutionComplete {
			t.Fatalf("unknown refs: %+v", r)
		}
	}
}

func TestStatusAndInfoSafety(t *testing.T) {
	for _, tt := range []struct {
		args []string
		env  map[string]string
		safe bool
	}{
		{[]string{"status"}, nil, false},
		{[]string{"--no-optional-locks", "status", "-sb"}, nil, true},
		{[]string{"status"}, map[string]string{"GIT_OPTIONAL_LOCKS": "0"}, true},
		{[]string{"status"}, map[string]string{"GIT_OPTIONAL_LOCKS": "1"}, false},
		{[]string{"-C", "--no-optional-locks", "status"}, nil, false},
		{[]string{"--version"}, nil, true}, {[]string{"-v"}, nil, true},
		{[]string{"--help", "status"}, nil, true}, {[]string{"help"}, nil, true},
		{[]string{"push", "--help"}, nil, true}, {[]string{"commit", "-h"}, nil, true},
		{[]string{"--exec-path"}, nil, true},
	} {
		r := Normalize(model.Invocation{Argv: tt.args, Env: tt.env})
		if r.Classification.MutationFree != tt.safe || r.Classification.RetrySafe != tt.safe || r.Operations[0].Effect != model.EffectRead || r.Classification.Coverage != model.CoverageSemantic {
			t.Errorf("%q env %v safe=%v: %+v", tt.args, tt.env, tt.safe, r)
		}
	}
}

func TestUnknownCoverage(t *testing.T) {
	for _, tt := range []struct {
		args     []string
		reason   model.UnknownReason
		coverage model.Coverage
	}{
		{[]string{"push", "--nonsense"}, model.UnknownFlag, model.CoverageConservative},
		{[]string{"push", "--force=1"}, model.UnknownParseError, model.CoverageConservative},
		{[]string{"--nonsense", "push"}, model.UnknownParseError, model.CoverageConservative},
		{[]string{"no-such-command"}, model.UnknownCommand, model.CoverageConservative},
		{[]string{"git-custom"}, model.UnknownExternal, model.CoverageOpaque},
		{[]string{"update-ref", "--stdin"}, model.UnknownInputUnavailable, model.CoverageOpaque},
		{[]string{"cat-file", "--batch"}, model.UnknownInputUnavailable, model.CoverageOpaque},
		{[]string{"fast-import"}, model.UnknownInputUnavailable, model.CoverageOpaque},
	} {
		r := Normalize(model.Invocation{Argv: tt.args})
		if len(r.Operations) != 1 || r.Operations[0].UnknownReason != tt.reason || r.Classification.Coverage != tt.coverage || r.Classification.MutationFree || r.Classification.RetrySafe {
			t.Errorf("%q: %+v", tt.args, r)
		}
	}
	r := Normalize(model.Invocation{Argv: []string{"ls-files"}})
	if r.Operations[0].Family != "git.ls-files" || r.Operations[0].Effect != model.EffectRead || r.Classification.Coverage != model.CoverageConservative || r.Classification.RetrySafe {
		t.Fatal(r)
	}
	if r := Normalize(model.Invocation{}); r.Classification.Level != model.LevelNone {
		t.Fatal(r)
	}
}

func TestCapturedProtocolIsNotMissingInput(t *testing.T) {
	r := Normalize(model.Invocation{Argv: []string{"update-ref", "--stdin"}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("delete refs/heads/topic\n")}})
	if r.Operations[0].UnknownReason == model.UnknownInputUnavailable || r.Classification.Coverage == model.CoverageOpaque {
		t.Fatal(r)
	}
}

func TestAggregation(t *testing.T) {
	ops := []model.Operation{
		{Effect: model.EffectRead, Coverage: model.CoverageSemantic},
		{Effect: model.EffectRead, Coverage: model.CoverageConservative, Unresolved: []model.Unresolved{{Field: "target.refs", Source: "git"}}},
	}
	c := aggregate(model.LevelL1, model.ConfidenceExact, ops)
	if c.Coverage != model.CoverageConservative || c.MutationFree || c.RetrySafe || c.Resolution != model.ResolutionDeferred {
		t.Fatal(c)
	}
	ops = append(ops, model.Operation{Effect: model.EffectUnknown, Coverage: model.CoverageOpaque, UnknownReason: model.UnknownShellAlias})
	c = aggregate(model.LevelL1, model.ConfidenceExact, ops)
	if c.Coverage != model.CoverageOpaque || c.Resolution != model.ResolutionUnresolved {
		t.Fatal(c)
	}
}

func TestStdin(t *testing.T) {
	for _, argv := range [][]string{{"update-ref", "--stdin"}, {"cat-file", "--batch"}, {"fast-import"}, {"credential", "fill"}, {"status", "--", "-F", "-"}, {"commit", "-F", "message.txt"}} {
		if yes, _ := NeedsStdin(argv); yes {
			t.Fatalf("unnecessary or bidirectional protocol read ahead: %q", argv)
		}
	}
	for _, tt := range []struct {
		argv  []string
		which string
	}{
		{[]string{"commit", "-F", "-"}, "commit-message"},
		{[]string{"tag", "-F-", "v1"}, "tag-message"},
		{[]string{"merge", "--file=-", "topic"}, "merge-message"},
		{[]string{"notes", "add", "--file=-"}, "notes-message"},
		{[]string{"hash-object", "--stdin"}, "hash-object"},
		{[]string{"hash-object", "--stdin-paths"}, "hash-object"},
		{[]string{"commit", "--pathspec-from-file=-"}, "pathspecs"},
		{[]string{"update-index", "--index-info"}, "index-paths"},
		{[]string{"log", "--stdin"}, "revisions"},
	} {
		if yes, which := NeedsStdin(tt.argv); !yes || which != tt.which {
			t.Errorf("%q: stdin %v %s", tt.argv, yes, which)
		}
	}
}

func FuzzNormalize(f *testing.F) {
	for _, seed := range []string{"", "status", "push\x00-f\x00origin\x00+HEAD:refs/heads/main", "-C\x00/tmp\x00commit\x00-amhello", "update-ref\x00--stdin", "-c\x00alias.a=b\x00-c\x00alias.b=a\x00a"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		r := Normalize(model.Invocation{Argv: strings.Split(raw, "\x00"), Aliases: map[string]string{"shell-alias": "!echo hi", "loop": "loop"}})
		if r.SchemaVersion != model.SchemaVersion || r.NormalizerVersion == "" || len(r.Operations) == 0 {
			t.Fatalf("invalid result: %+v", r)
		}
		for _, op := range r.Operations {
			for _, set := range []*model.TargetSet{op.Target.Refs, op.Target.Paths, op.Target.Revisions, op.Target.ConfigKeys} {
				if set != nil && (set.Kind == model.SetExact || set.Kind == model.SetSet || set.Kind == model.SetPattern) && len(set.Values) == 0 {
					t.Fatalf("empty known target: %+v", set)
				}
			}
		}
	})
}
