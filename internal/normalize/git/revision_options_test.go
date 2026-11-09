package git

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

var mergeDiffCases = []struct {
	args  string
	patch bool
}{
	{"log -c", true},
	{"log --cc", true},
	{"log -m -p", true},
	{"log --dd", true},
	{"log --remerge-diff", true},
	{"log --diff-merges=first-parent", true},
	{"log --no-diff-merges", false},
	{"log --combined-all-paths", false},
	{"show -c HEAD", true},
	{"show -m HEAD", true},
	{"show --diff-merges=off HEAD", true},
	{"log -1 -c core.pager=cat", true},
	{"diff --cc HEAD", true},
	{"diff -c", true},
	{"log -m", false},
	{"log --diff-merges=off", false},
	{"log --diff-merges first-parent", true},
	{"log -c --combined-all-paths", true},
}

func TestMergeDiffOptions(t *testing.T) {
	for _, tt := range mergeDiffCases {
		t.Run(tt.args, func(t *testing.T) {
			args := strings.Fields(tt.args)
			r, op := adminTestOperation(t, args...)
			if op.Action != "git."+args[0]+".read" || op.Effect != model.EffectRead || op.Coverage != model.CoverageSemantic || op.UnknownReason != model.UnknownNone {
				t.Fatal(r)
			}
			if slices.Contains(op.Capabilities, model.CapabilityExternalExec) != tt.patch || r.Classification.RetrySafe != !tt.patch {
				t.Fatal("patch/textconv safety differs from log -p:", r)
			}
		})
	}
	p, err := Parse([]string{"log", "-1", "-c", "core.pager=cat"})
	o, ok := p.Option("c")
	if err != nil || !ok || o.HasValue || o.Span.Start != 2 || o.Span.End != 3 || o.Definition.Source.File != "diff-merges.c" || !reflect.DeepEqual(p.Values(), []string{"core.pager=cat"}) {
		t.Fatalf("combined-diff flag consumed a revision: %+v, %v", p, err)
	}
	for _, args := range [][]string{{"log", "--diff-merges"}, {"log", "--cc=value"}, {"log", "--no-cc"}} {
		if p, err := Parse(args); err == nil && len(p.Unknown) == 0 {
			t.Errorf("accepted invalid arity/negation: %q", args)
		}
	}
	for _, command := range []string{"log", "show", "whatchanged", "diff", "diff-tree", "diff-index", "diff-files", "diff-pairs"} {
		r, op := adminTestOperation(t, command, "--cc", "--no-textconv")
		if op.UnknownReason != model.UnknownNone || op.Coverage != model.CoverageSemantic || command != "diff" && slices.Contains(op.Capabilities, model.CapabilityExternalExec) {
			t.Fatal(r)
		}
	}
}

func TestRevisionPseudoOptionValues(t *testing.T) {
	for _, command := range []string{
		"annotate", "blame", "format-patch", "format-rev", "last-modified", "log", "pickaxe",
		"reflog", "reflog show", "rev-list", "shortlog", "show", "whatchanged",
		"rev-parse", "stash show", "stash list",
	} {
		for _, tt := range []struct{ name, value string }{
			{"glob", "refs/heads/*"}, {"exclude", "x"}, {"exclude-hidden", "receive"},
		} {
			prefix := strings.Fields(command)
			for _, attached := range []bool{false, true} {
				args := append([]string(nil), prefix...)
				if attached {
					args = append(args, "--"+tt.name+"="+tt.value)
				} else {
					args = append(args, "--"+tt.name, tt.value)
				}
				args = append(args, "--all", "HEAD")
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					p, err := Parse(args)
					o, ok := p.Option(tt.name)
					if err != nil || len(p.Unknown) != 0 || !ok || !o.HasValue || o.Value != tt.value || !p.Has("all") || !reflect.DeepEqual(p.Values(), []string{"HEAD"}) {
						t.Fatalf("parsed: %+v, %v", p, err)
					}
					r := Normalize(model.Invocation{Argv: args})
					if len(r.Errors) != 0 {
						t.Fatal(r)
					}
					for _, op := range r.Operations {
						if op.UnknownReason != model.UnknownNone || op.Coverage != model.CoverageSemantic {
							t.Fatal(r)
						}
					}
				})
			}
			if p, err := Parse(append(append([]string(nil), prefix...), "--"+tt.name)); err == nil {
				t.Errorf("accepted missing %s value for %s: %+v", tt.name, command, p)
			}
		}
		for _, name := range []string{"branches", "tags", "remotes"} {
			for _, attached := range []bool{false, true} {
				flag := "--" + name
				if attached {
					flag += "=topic*"
				}
				args := append(strings.Fields(command), flag, "HEAD")
				p, err := Parse(args)
				o, ok := p.Option(name)
				if err != nil || !ok || o.HasValue != attached || o.Definition.Arity != "optional" || !hasFlag(o.Definition.Flags, "ATTACHED_VALUE") || !reflect.DeepEqual(p.Values(), []string{"HEAD"}) {
					t.Errorf("optional attached-only value changed for %q: %+v, %v", args, p, err)
				}
			}
		}
	}
}

func TestReflogLogOptions(t *testing.T) {
	for _, prefix := range [][]string{{"reflog"}, {"reflog", "show"}} {
		for _, options := range []string{
			"-p", "--stat", "--format=%H", "--pretty=oneline", "-1", "-n 2",
			"--output=report", "--output=first --output=second --output=second",
			"--ext-diff", "--textconv", "-p --no-textconv", "--ext-diff --no-ext-diff",
			"--textconv --no-textconv", "--show-signature", "--show-signature --no-show-signature",
			"--format=%G?", "--pretty=format:%G? --no-show-signature", "--format=%%G?",
			"--format=%%%G?", "--cc", "--diff-merges=first-parent",
		} {
			args := append(append([]string(nil), prefix...), strings.Fields(options)...)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				r, op := adminTestOperation(t, args...)
				logResult, logOp := adminTestOperation(t, append([]string{"log"}, strings.Fields(options)...)...)
				action := "ref.reflog.show"
				if logOp.Effect == model.EffectWrite {
					action = "git.log.output"
				}
				if op.Action != action || op.Effect != logOp.Effect || op.Coverage != model.CoverageSemantic || op.UnknownReason != model.UnknownNone || !reflect.DeepEqual(op.Capabilities, logOp.Capabilities) || r.Classification.RetrySafe != logResult.Classification.RetrySafe || r.Classification.MutationFree != logResult.Classification.MutationFree {
					t.Fatalf("reflog differs from log: %+v; log: %+v", r, logResult)
				}
				if !reflect.DeepEqual(op.Target.Refs, model.Exact("HEAD")) || !reflect.DeepEqual(op.Target.Paths, logOp.Target.Paths) {
					t.Fatal(op.Target)
				}
			})
		}
	}
	p, err := Parse([]string{"reflog", "HEAD", "-p", "--format=%H"})
	if err != nil || !p.Has("patch") || !p.Has("format") || !reflect.DeepEqual(p.Values(), []string{"HEAD"}) {
		t.Fatalf("implicit show stopped at the ref: %+v %v", p, err)
	}
	for _, mode := range []string{"expire", "delete", "drop", "list", "exists", "write"} {
		r := Normalize(model.Invocation{Argv: []string{"reflog", mode, "--output=report"}})
		if r.Operations[0].UnknownReason != model.UnknownFlag {
			t.Fatalf("log flags leaked to reflog %s: %+v", mode, r)
		}
	}
}

func TestRevisionOptionsRealGit(t *testing.T) {
	run, dir := rrRealGit(t), t.TempDir()
	run(dir, "", "init", "-bmain")
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run(dir, "", "add", "file")
	run(dir, "", "commit", "-mfirst")
	run(dir, "", "branch", "core.pager=cat")
	for _, prefix := range [][]string{
		{"log"}, {"rev-list"}, {"show"}, {"whatchanged", "--i-still-use-this"}, {"reflog"}, {"reflog", "show"},
	} {
		for _, options := range []string{"--glob refs/heads/*", "--exclude x --all", "--exclude-hidden receive --all"} {
			separate := strings.Fields(options)
			attached := append([]string{separate[0] + "=" + separate[1]}, separate[2:]...)
			got := run(dir, "", append(append([]string(nil), prefix...), separate...)...)
			want := run(dir, "", append(append([]string(nil), prefix...), attached...)...)
			if got != want {
				t.Fatalf("git %q %s differs from attached value: got %q, want %q", prefix, options, got, want)
			}
		}
	}
	for _, tt := range mergeDiffCases {
		if tt.args != "log --combined-all-paths" { // Git requires -c or --cc with this flag.
			run(dir, "", strings.Fields(tt.args)...)
		}
	}
	for _, args := range []string{
		"reflog show -p", "reflog -p", "reflog show --stat", "reflog show --format=%H", "reflog HEAD -p",
		"reflog show --ext-diff", "reflog show --textconv", "reflog show --output=first --output=second",
		"stage file", "stage -A", "stage -p",
	} {
		run(dir, "n\n", strings.Fields(args)...)
	}
	for _, file := range []string{"first", "second"} {
		if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
			t.Fatal("reflog must account for every --output file:", err)
		}
	}
}
