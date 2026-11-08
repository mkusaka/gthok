package git

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/registry"
)

func TestOptionSyntax(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		name   string
		value  string
		has    bool
		negate bool
	}{
		{[]string{"push", "--porc"}, "porcelain", "", false, false},
		{[]string{"push", "--no-force"}, "force", "", false, true},
		{[]string{"push", "--force-with-lease=refs/heads/main:abc"}, "force-with-lease", "refs/heads/main:abc", true, false},
		{[]string{"push", "--force-with-lease", "origin"}, "force-with-lease", "", false, false},
		{[]string{"push", "--branches"}, "all", "", false, false},
		{[]string{"commit", "-amhello"}, "message", "hello", true, false},
		{[]string{"commit", "--message="}, "message", "", true, false},
		{[]string{"commit", "-F", "-"}, "file", "-", true, false},
		{[]string{"status", "-uno"}, "untracked-files", "no", true, false},
		{[]string{"commit", "--verify"}, "no-verify", "", false, true},
		{[]string{"log", "--max-count=3"}, "max-count", "3", true, false},
		{[]string{"log", "-10"}, "max-count", "10", true, false},
		{[]string{"diff", "--output=changes.patch"}, "output", "changes.patch", true, false},
		{[]string{"revert", "--reference", "HEAD"}, "reference", "", false, false},
		{[]string{"cherry-pick", "--empty=keep", "HEAD"}, "empty", "keep", true, false},
	} {
		p, err := Parse(tt.args)
		o, ok := p.Option(tt.name)
		if err != nil || !ok || len(p.Unknown) > 0 || o.Value != tt.value || o.HasValue != tt.has || o.Negated != tt.negate {
			t.Errorf("%q: %+v %v; option %+v", tt.args, p, err, o)
		}
	}
	for _, args := range [][]string{
		{"push", "--forc"}, {"push", "--force-"}, {"push", "--force=yes"}, {"commit", "-F"}, {"push", "--no-ipv4"},
		{"status", "--not-an-option"}, {"push", "--no-force=1"},
	} {
		if p, err := Parse(args); err == nil && len(p.Unknown) == 0 {
			t.Errorf("accepted %q: %+v", args, p)
		}
	}
	for _, args := range [][]string{{"cherry-pick", "--reference"}, {"revert", "--empty=keep"}, {"revert", "-x"}} {
		if r := Normalize(model.Invocation{Argv: args}); r.Operations[0].UnknownReason != model.UnknownFlag {
			t.Errorf("conditional union accepted %q: %+v", args, r)
		}
	}
}

func TestSpansAndTerminators(t *testing.T) {
	args := []string{"-C", "/repo", "commit", "-amhello", "--message", "second", "--", "--literal-path"}
	p, err := Parse(args)
	if err != nil || len(p.Options) != 3 || len(p.Positionals) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	if !reflect.DeepEqual(p.Options[0].Span.Keep, []string{"-mhello"}) || !reflect.DeepEqual(p.Options[1].Span.Keep, []string{"-a"}) {
		t.Fatalf("bundle write-back: %+v", p.Options)
	}
	if p.Options[1].Span.Start != 3 || p.Options[1].Span.End != 4 || p.Options[1].ValueIndex != 3 || p.Options[1].ValueOffset != 3 || p.Options[2].Span.Start != 4 || p.Options[2].Span.End != 6 || p.Options[2].ValueIndex != 5 {
		t.Fatal(p.Options)
	}
	if p.Positionals[0].Span.Start != 7 || !p.Positionals[0].AfterDashDash || len(p.AfterDashDash) != 1 {
		t.Fatal(p.Positionals)
	}
	p, err = Parse([]string{"push", "--end-of-options", "--force"})
	if err != nil || p.Has("force") || len(p.AfterDashDash) != 0 || !reflect.DeepEqual(p.Values(), []string{"--force"}) {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestSubcommandsAndStop(t *testing.T) {
	p, err := Parse([]string{"config", "get", "--global", "user.name", "--literal-value"})
	if err != nil || !reflect.DeepEqual(p.Path, []string{"config", "get"}) || !p.Has("global") || !reflect.DeepEqual(p.Values(), []string{"user.name", "--literal-value"}) {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = Parse([]string{"worktree", "add", "--detach", "dir", "HEAD"})
	if err != nil || !reflect.DeepEqual(p.Path, []string{"worktree", "add"}) || !p.Has("detach") {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = Parse([]string{"stash", "-m", "message", "--", "file"})
	if err != nil || !p.Has("message") || len(p.Positionals) != 1 {
		t.Fatalf("default stash push: %+v %v", p, err)
	}
	if _, err := Parse([]string{"worktree", "nonexistent"}); err == nil {
		t.Fatal("unknown subcommand accepted")
	}
	p, err = Parse([]string{"cherry-pick", "--unrecognized", "HEAD", "--continue"})
	if err != nil || len(p.Unknown) != 1 || !p.Has("continue") || !reflect.DeepEqual(p.Values(), []string{"HEAD"}) {
		t.Fatalf("KEEP_UNKNOWN: %+v %v", p, err)
	}
}

func TestDefaultAndNumericAdapters(t *testing.T) {
	p, err := Parse([]string{"config", "--global", "--get", "user.name", "--not-a-flag"})
	if err != nil || !p.Has("global") || !p.Has("get") || !reflect.DeepEqual(p.Values(), []string{"user.name", "--not-a-flag"}) {
		t.Fatalf("legacy config: %+v %v", p, err)
	}
	p, err = Parse([]string{"grep", "-12", "(", "-e", "needle", ")"})
	if err != nil || len(p.Options) != 4 || p.Options[0].Value != "12" || len(p.Values()) != 0 {
		t.Fatalf("numeric/nodash: %+v %v", p, err)
	}
	p, err = Parse([]string{"branch", "--no-contains"})
	if err != nil || !p.Has("no-contains") || p.Options[0].HasValue {
		t.Fatalf("LASTARG_DEFAULT: %+v %v", p, err)
	}
	if _, err := Parse([]string{"log", "--filter", "blob:none"}); err == nil {
		t.Fatal("accepted detached revision pseudo-option")
	}
}

func TestLastArgDefaultAndLowLevelCallbacks(t *testing.T) {
	grammar := registry.Grammar{Parser: "parse-options", Options: []registry.Option{{Long: "value", Arity: "required", Flags: []string{"PARSE_OPT_LASTARG_DEFAULT"}}}}
	p := ParsedCommand{Path: []string{"test"}}
	if err := p.parseArgs(grammar, []string{"--value"}, 1); err != nil || p.Options[0].HasValue {
		t.Fatalf("%+v %v", p, err)
	}
	for _, args := range [][]string{
		{"update-index", "--cacheinfo", "100644", "object", "file"},
		{"update-index", "--cacheinfo", "100644," + strings.Repeat("0", 40) + ",file"},
		{"update-index", "--unresolve", "--flag-is-a-path"},
	} {
		p, err := Parse(args)
		if err != nil || len(p.Options) != 1 || p.Options[0].Span.End != len(args) {
			t.Errorf("%q: %+v %v", args, p, err)
		}
	}
	for _, args := range [][]string{{"update-index", "--cacheinfo"}, {"update-index", "--cacheinfo", "100644,invalid,file"}, {"update-index", "--index-info", "path"}} {
		if _, err := Parse(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}
