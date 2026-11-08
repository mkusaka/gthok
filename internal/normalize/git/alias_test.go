package git

import (
	"os/exec"
	"reflect"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestSplitCmdline(t *testing.T) {
	for _, tt := range []struct {
		line string
		want []string
	}{
		{"push origin", []string{"push", "origin"}},
		{`commit -m "two words"`, []string{"commit", "-m", "two words"}},
		{`commit -m 'two words'`, []string{"commit", "-m", "two words"}},
		{`commit -m "one\ two\"three"`, []string{"commit", "-m", `one two"three`}},
		{`commit -m 'one\two'`, []string{"commit", "-m", `one\two`}},
		{`commit -m '' a"b"c`, []string{"commit", "-m", "", "abc"}},
		{" push \t origin ", []string{"", "push", "origin", ""}},
		{"", []string{""}},
	} {
		got, err := splitCmdline(tt.line)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q: %q %v, want %q", tt.line, got, err, tt.want)
		}
	}
	for _, line := range []string{`push "unclosed`, "push ending\\", "push 'unclosed"} {
		if _, err := splitCmdline(line); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
}

func TestExpandAlias(t *testing.T) {
	aliases := map[string]string{"p": "next origin", "next": "push", "shell-alias": "!echo hi", "status": "push", "Mixed.command": "version", "foo.bar": "version", "a": "b", "b": "a", "self": "self", "filter-branch": "version"}
	argv := []string{"-C", "repo", "P", "main"}
	got, alias, reason := ExpandAlias(argv, aliases)
	if alias != "P" || reason != model.UnknownNone || !reflect.DeepEqual(got, []string{"-C", "repo", "push", "origin", "main"}) || argv[2] != "P" {
		t.Fatalf("expanded: %q %s %s", got, alias, reason)
	}
	for _, tt := range []struct {
		name   string
		want   string
		reason model.UnknownReason
	}{
		{"shell-alias", "shell-alias", model.UnknownShellAlias}, {"status", "status", model.UnknownNone},
		{"filter-branch", "filter-branch", model.UnknownNone},
		{"Mixed", "version", model.UnknownNone}, {"mixed", "mixed", model.UnknownNone},
		{"FOO.BAR", "version", model.UnknownNone}, {"a", "a", model.UnknownAliasLoop}, {"self", "self", model.UnknownAliasLoop},
	} {
		got, _, reason := ExpandAlias([]string{tt.name}, aliases)
		if reason != tt.reason || got[0] != tt.want {
			t.Errorf("%s: %q %s", tt.name, got, reason)
		}
	}
}

func TestAliasGlobalsAndSource(t *testing.T) {
	for _, tt := range []struct {
		value  string
		reason model.UnknownReason
	}{
		{"-c a.b=c push --force", model.UnknownNone},
		{"-p push", model.UnknownNone}, {"-C '' push", model.UnknownNone},
		{"--exec-path=/tools push", model.UnknownNone},
		{"--exec-path", model.UnknownNone}, {"--html-path", model.UnknownNone},
		{"--version", model.UnknownCommand}, {"--help version", model.UnknownCommand},
		{"-P push", model.UnknownParseError}, {"-C repo push", model.UnknownParseError},
		{"--no-optional-locks push", model.UnknownParseError},
		{"--git-dir=repo push", model.UnknownParseError},
		{"", model.UnknownParseError},
		{"push 'unclosed", model.UnknownParseError},
	} {
		_, _, reason := ExpandAlias([]string{"p"}, map[string]string{"p": tt.value})
		if reason != tt.reason {
			t.Errorf("%q: %s, want %s", tt.value, reason, tt.reason)
		}
	}
	argv := []string{"-c", "alias.p=push --force", "p", "origin", "HEAD:refs/heads/main"}
	r := Normalize(model.Invocation{Argv: argv})
	if r.Source.Alias != "p" || r.Source.Argv[2] != "push" || argv[2] != "p" || r.Operations[0].Parameters["forceMode"] != "unconditional" {
		t.Fatalf("source: %+v", r)
	}
	r = Normalize(model.Invocation{Argv: []string{"--config-env=alias.p=ALIAS", "p"}, Env: map[string]string{"ALIAS": "version"}})
	if r.Source.Alias != "p" || r.Operations[0].Effect != model.EffectRead {
		t.Fatal(r)
	}
	r = Normalize(model.Invocation{Argv: []string{"-c", "alias.p=version", "p"}, Aliases: map[string]string{"P": "push", "p.command": "push"}})
	if r.Operations[0].Action != "git.version.read" {
		t.Fatalf("inline alias must override loaded aliases: %+v", r)
	}
	r = Normalize(model.Invocation{Argv: []string{"-c", "alias.p.command=version", "p"}, Aliases: map[string]string{"p": "!echo hi"}})
	if r.Operations[0].Action != "git.version.read" {
		t.Fatalf("inline subsection alias must win: %+v", r)
	}
	r = Normalize(model.Invocation{Argv: []string{"p"}, Aliases: map[string]string{"p": "!echo hi", "p.command": "version"}})
	if r.Operations[0].UnknownReason != model.UnknownParseError || r.Classification.MutationFree {
		t.Fatalf("unordered conflicting aliases must fail closed: %+v", r)
	}
	r = Normalize(model.Invocation{Argv: []string{"p"}, Aliases: map[string]string{"p": "!echo hi"}})
	if r.Source.Alias != "p" || r.Operations[0].UnknownReason != model.UnknownShellAlias || r.Classification.Coverage != model.CoverageOpaque {
		t.Fatal(r)
	}
}

func TestExpandAliasHops(t *testing.T) {
	for _, tt := range []struct {
		name    string
		argv    []string
		aliases map[string]string
		env     map[string]string
		want    [][]string
		alias   string
		reason  model.UnknownReason
	}{
		{
			name: "no command", want: [][]string{nil},
		},
		{
			name: "builtin precedes alias", argv: []string{"status"}, aliases: map[string]string{"status": "push"},
			want: [][]string{{"status"}},
		},
		{
			name: "ordinary intermediate aliases", argv: []string{"a", "--short"},
			aliases: map[string]string{"a": "b", "b": "c", "c": "status"}, alias: "a",
			want: [][]string{{"a", "--short"}, {"b", "--short"}, {"c", "--short"}, {"status", "--short"}},
		},
		{
			name: "alias defines alias", argv: []string{"lg2"},
			aliases: map[string]string{"lg2": "-c alias.x=status x", "x": "push"}, alias: "lg2",
			want: [][]string{{"lg2"}, {"-c", "alias.x=status", "x"}, {"-c", "alias.x=status", "status"}},
		},
		{
			name: "per-hop globals", argv: []string{"-C", "repo", "a", "--short"},
			aliases: map[string]string{"a": "--exec-path=one b", "b": "-C '' --exec-path=two status"}, alias: "a",
			want: [][]string{
				{"-C", "repo", "a", "--short"},
				{"-C", "repo", "--exec-path=one", "b", "--short"},
				{"-C", "repo", "--exec-path=one", "-C", "", "--exec-path=two", "status", "--short"},
			},
		},
		{
			name: "config-env", argv: []string{"a"},
			aliases: map[string]string{"a": "--config-env=alias.x=ALIAS x"}, env: map[string]string{"ALIAS": "status"}, alias: "a",
			want: [][]string{{"a"}, {"--config-env=alias.x=ALIAS", "x"}, {"--config-env=alias.x=ALIAS", "status"}},
		},
		{
			name: "shell alias", argv: []string{"a"}, aliases: map[string]string{"a": "b", "b": "!echo hi"},
			want: [][]string{{"a"}, {"b"}}, alias: "a", reason: model.UnknownShellAlias,
		},
		{
			name: "loop", argv: []string{"a"}, aliases: map[string]string{"a": "b", "b": "a"},
			want: [][]string{{"a"}, {"b"}, {"a"}}, alias: "a", reason: model.UnknownAliasLoop,
		},
		{
			name: "ambiguity", argv: []string{"a"}, aliases: map[string]string{"a": "b", "b": "status", "b.command": "push"},
			want: [][]string{{"a"}, {"b"}}, alias: "a", reason: model.UnknownParseError,
		},
		{
			name: "invalid expansion", argv: []string{"a"}, aliases: map[string]string{"a": "b", "b": "-C other status"},
			want: [][]string{{"a"}, {"b"}}, alias: "a", reason: model.UnknownParseError,
		},
		{
			name: "early option", argv: []string{"a"}, aliases: map[string]string{"a": "--exec-path"},
			want: [][]string{{"a"}, {"--exec-path"}}, alias: "a",
		},
		{
			name: "alias help is not rewritten", argv: []string{"a"}, aliases: map[string]string{"a": "--help"},
			want: [][]string{{"a"}, {"--help"}}, alias: "a", reason: model.UnknownCommand,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.argv...)
			hops, alias, reason := ExpandAliasHops(tt.argv, tt.aliases, tt.env)
			if !reflect.DeepEqual(hops, tt.want) || alias != tt.alias || reason != tt.reason {
				t.Fatalf("hops=%q alias=%q reason=%s, want %q %q %s", hops, alias, reason, tt.want, tt.alias, tt.reason)
			}
			expanded, oldAlias, oldReason := expandAlias(tt.argv, tt.aliases, tt.env)
			if !reflect.DeepEqual(expanded, hops[len(hops)-1]) || oldAlias != alias || oldReason != reason {
				t.Fatalf("expansion changed: %q %q %s", expanded, oldAlias, oldReason)
			}
			if len(hops) > 1 {
				hops[len(hops)-1][0] = "changed"
				if !reflect.DeepEqual(hops[:len(hops)-1], tt.want[:len(tt.want)-1]) {
					t.Fatalf("hops share mutable argv: %q", hops)
				}
			}
			if !reflect.DeepEqual(tt.argv, original) {
				t.Fatalf("input changed: %q, want %q", tt.argv, original)
			}
		})
	}
}

func TestAliasesAgainstGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	env := []string{"HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat"}
	for _, tt := range []struct {
		key, name, value string
		ok               bool
	}{
		{"alias.p", "P", `version "--build-options"`, true},
		{"alias.Mixed.command", "Mixed", "version", true},
		{"alias.Mixed.command", "mixed", "version", false},
		{"alias.p", "p", "-c a.b=c version", true},
		{"alias.p", "p", "-C '' version", true},
		{"alias.p", "p", "-P version", false},
		{"alias.p", "p", "--exec-path", true},
		{"alias.p", "p", "--version", false},
		{"alias.p", "p", "--help version", false},
		{"alias.p", "p", "p", false},
	} {
		args := []string{"-c", tt.key + "=" + tt.value, tt.name}
		cmd := exec.Command(git, args...)
		cmd.Dir, cmd.Env = dir, env
		output, gitErr := cmd.CombinedOutput()
		if (gitErr == nil) != tt.ok {
			t.Fatalf("Git %q: %v: %s", args, gitErr, output)
		}
		got := Normalize(model.Invocation{Argv: args})
		if (got.Operations[0].UnknownReason == model.UnknownNone) != tt.ok {
			t.Errorf("normalizer %q: %+v", args, got)
		}
	}
}
