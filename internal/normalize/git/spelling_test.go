package git_test

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
	"github.com/mkusaka/gthok/internal/registry"
)

var spellingCommands = registry.Commands()
var spellingNames = func() []string {
	var names []string
	for name, command := range spellingCommands {
		if !strings.HasPrefix(name, "-") && (command.Parser == "parse-options" || command.Parser == "revision" || command.Parser == "diff") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}()
var spellingRevision = registry.RevisionOptions()
var spellingDiff = registry.DiffOptions()
var spellingValues = []string{"main", "topic", "HEAD", "HEAD~1", "v1", "origin", "file.txt", "1", "x", ".", "origin/main"}

// The seed drives grammar selection as well as option/value spellings, so Go's
// saved fuzz input reproduces the entire invocation without Git or subprocesses.
func FuzzSpelling(f *testing.F) {
	for _, seed := range []int64{0, 1, 2, 17, 42, 256, 20261109, 8675309} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		argv, grammar, start := spellingInvocation(rand.New(rand.NewSource(seed)))
		base := gitnormalize.Normalize(model.Invocation{Argv: argv})
		for _, op := range base.Operations {
			if op.UnknownReason != model.UnknownNone {
				return
			}
		}
		want := spellingResult(t, base)
		for _, variant := range spellingVariants(grammar, argv, start) {
			got := spellingResult(t, gitnormalize.Normalize(model.Invocation{Argv: variant}))
			if !bytes.Equal(got, want) {
				t.Fatalf("seed=%d argv=%q equivalent=%q\noriginal: %s\nrewritten: %s", seed, argv, variant, want, got)
			}
		}
	})
}

func spellingResult(t *testing.T, result model.Result) []byte {
	t.Helper()
	result.Source.Argv = nil
	// JSON excludes execution-only Editable locators (raw argv spans); all
	// public fields, including origin, errors, and classification, must agree.
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func spellingInvocation(r *rand.Rand) ([]string, registry.Grammar, int) {
	name := spellingNames[r.Intn(len(spellingNames))]
	g := spellingCommands[name].Grammar
	argv := []string{name}
	if len(g.Subcommands) > 0 && r.Intn(5) != 0 {
		sub := g.Subcommands[r.Intn(len(g.Subcommands))]
		argv, g = append(argv, sub.Name), sub.Grammar
	}
	start := len(argv)
	g.Options = append([]registry.Option(nil), g.Options...)
	switch g.Parser {
	case "revision":
		g.Options = append(g.Options, spellingRevision...)
	case "diff":
		g.Options = append(g.Options, spellingDiff...)
	}
	var options []registry.Option
	for _, o := range g.Options {
		if o.Long != "" || o.Short != "" {
			options = append(options, o)
		}
	}
	count := min(len(options), 1+r.Intn(3))
	for _, index := range r.Perm(len(options))[:count] {
		o, value := options[index], spellingValues[r.Intn(len(spellingValues))]
		flag := "-" + o.Short
		long := o.Long != "" && (o.Short == "" || r.Intn(5) < 3)
		if long {
			flag = "--" + o.Long
			if o.Negatable && r.Intn(5) == 0 {
				argv = append(argv, "--no-"+o.Long)
				continue
			}
		}
		switch {
		case o.Arity == "required":
			if r.Intn(2) == 0 {
				argv = append(argv, flag, value)
			} else if long {
				argv = append(argv, flag+"="+value)
			} else {
				argv = append(argv, flag+value)
			}
		case long && o.Arity == "optional" && r.Intn(5) < 2:
			argv = append(argv, flag+"="+value)
		default:
			argv = append(argv, flag)
		}
	}
	for range r.Intn(3) {
		argv = append(argv, spellingValues[r.Intn(len(spellingValues))])
	}
	return argv, g, start
}

func spellingVariants(g registry.Grammar, argv []string, start int) [][]string {
	var variants [][]string
	rewrite := func(index, count int, tokens ...string) {
		v := append([]string(nil), argv[:index]...)
		v = append(v, tokens...)
		variants = append(variants, append(v, argv[index+count:]...))
	}
	for i := start; i < len(argv); i++ {
		token := argv[i]
		if token == "--" || token == "--end-of-options" {
			break
		}
		long := strings.HasPrefix(token, "--")
		name, value, attached := strings.Cut(strings.TrimPrefix(token, "--"), "=")
		var option registry.Option
		for _, o := range g.Options {
			if long && o.Long == name || !long && len(token) >= 2 && token[0] == '-' && o.Short == token[1:2] {
				option = o
				break
			}
		}
		if option.Long == "" && option.Short == "" {
			if !strings.HasPrefix(token, "-") && slices.Contains(g.ParseFlags, "PARSE_OPT_STOP_AT_NON_OPTION") {
				break
			}
			continue
		}
		// Low-level callbacks can consume more than their declared arity,
		// notably update-index --again/--unresolve and --cacheinfo.
		if option.Type == "OPTION_LOWLEVEL_CALLBACK" {
			break
		}
		if long && g.Parser == "parse-options" && !slices.Contains(g.ParseFlags, "PARSE_OPT_KEEP_UNKNOWN_OPT") && option.Type != "manual" {
			if prefix := spellingPrefix(g.Options, name); prefix != "" {
				flag := "--" + prefix
				if attached {
					flag += "=" + value
				}
				rewrite(i, 1, flag)
			}
		}
		if option.Arity != "required" {
			continue
		}
		if long && option.Type == "manual" && slices.Contains(option.Flags, "ATTACHED_VALUE") {
			continue
		}
		if long && attached {
			rewrite(i, 1, "--"+name, value)
		} else if !long && len(token) > 2 {
			rewrite(i, 1, token[:2], token[2:])
		} else if i+1 < len(argv) {
			if long {
				rewrite(i, 2, token+"="+argv[i+1])
			} else {
				rewrite(i, 2, token+argv[i+1])
			}
			i++
		}
	}
	return variants
}

func spellingPrefix(options []registry.Option, name string) string {
	for n := 3; n < len(name); n++ {
		prefix, unique := name[:n], true
		for _, o := range options {
			if o.Long == "" {
				continue
			}
			names := []string{o.Long}
			if o.Negatable {
				names = append(names, "no-"+o.Long)
				if base, ok := strings.CutPrefix(o.Long, "no-"); ok {
					names = append(names, base)
				}
			}
			for _, other := range names {
				if other != name && strings.HasPrefix(other, prefix) {
					unique = false
				}
			}
		}
		if unique {
			return prefix
		}
	}
	return ""
}

func TestSpellingVariants(t *testing.T) {
	for _, tt := range []struct {
		argv []string
		want []string
	}{
		{[]string{"push", "--repo=origin"}, []string{"push", "--repo", "origin"}},
		{[]string{"push", "--repo", "origin"}, []string{"push", "--repo=origin"}},
		{[]string{"grep", "-ex"}, []string{"grep", "-e", "x"}},
		{[]string{"grep", "-e", "x"}, []string{"grep", "-ex"}},
		{[]string{"push", "--verbose"}, []string{"push", "--verb"}},
	} {
		t.Run(strings.Join(tt.argv, " "), func(t *testing.T) {
			variants := spellingVariants(spellingCommands[tt.argv[0]].Grammar, tt.argv, 1)
			if !slices.ContainsFunc(variants, func(v []string) bool { return slices.Equal(v, tt.want) }) {
				t.Fatalf("missing %q in %q", tt.want, variants)
			}
		})
	}
	if got := spellingPrefix(spellingCommands["push"].Options, "verbose"); got != "verb" {
		t.Fatalf("--ver also matches the negation of --no-verify: %q", got)
	}
	for _, argv := range [][]string{
		{"push", "--", "--repo=origin"},
		{"update-index", "--again", "--chmod=1"},
		{"update-index", "-g", "--chmod=1"},
		{"update-index", "--unresolve", "--chmod=1"},
		{"cherry-pick", "--allow-empty"},
		{"revert", "--allow-empty"},
		{"fast-export", "--signed-tags=strip"},
		{"stash", "show", "--include-untracked"},
		{"stash", "store", "--quiet"},
		{"log", "--max-count=1"},
	} {
		g, start := spellingCommands[argv[0]].Grammar, 1
		if argv[0] == "stash" {
			for _, sub := range g.Subcommands {
				if sub.Name == argv[1] {
					g, start = sub.Grammar, 2
					break
				}
			}
		}
		variants := spellingVariants(g, argv, start)
		if argv[0] == "update-index" || argv[1] == "--" {
			if len(variants) != 0 {
				t.Errorf("rewrote callback operands/paths: %q -> %q", argv, variants)
			}
		}
		for _, variant := range variants {
			if len(variant) == len(argv) {
				t.Errorf("unsafe prefix rewrite: %q -> %q", argv, variant)
			}
		}
	}
}
