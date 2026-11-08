package registry

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestEmbeddedRegistry(t *testing.T) {
	for _, name := range []string{"commands", "grammar", "source-map", "manifest"} {
		b, err := data.ReadFile("data/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(b) {
			t.Errorf("%s is not valid JSON", name)
		}
	}
	m := Manifest()
	if m.Git != "2.56.0" || m.Tag != "v2.56.0" || m.Commit != "a018953688f1b10bddf91bff8747068f5f4746a4" {
		t.Fatalf("wrong pin: %+v", m)
	}
	commands := Commands()
	if len(commands) != m.Coverage.Commands || m.Coverage.Builtins != 149 {
		t.Fatalf("inventory coverage: %+v", m.Coverage)
	}
	builtins := 0
	for name, c := range commands {
		if c.Name != name || c.Parser == "" || c.Source.File == "" || c.Source.Line < 1 {
			t.Errorf("incomplete command %q: %+v", name, c)
		}
		if c.Builtin {
			builtins++
			if c.Entrypoint == "" {
				t.Errorf("builtin %s lacks entrypoint", name)
			}
		}
	}
	if builtins != m.Coverage.Builtins {
		t.Fatalf("%d builtins != %+v", builtins, m.Coverage)
	}
	for _, name := range []string{"remote-http", "http-fetch", "http-push"} {
		if c := commands[name]; c.Implementation != "external" || c.Builtin {
			t.Errorf("%s: %+v", name, c)
		}
	}
	if c := commands["filter-branch"]; c.Implementation != "script" {
		t.Errorf("filter-branch: %+v", c)
	}
	if c := commands["gitattributes"]; c.Implementation != "document" {
		t.Errorf("gitattributes: %+v", c)
	}
	if c := commands["--version"]; c.Implementation != "option-only" || c.Parser != "global" {
		t.Errorf("--version: %+v", c)
	}
	if c := commands["push"]; !slices.Contains(c.Flags, "RUN_SETUP") || !slices.Contains(c.Categories, "mainporcelain") {
		t.Errorf("push metadata: %+v", c)
	}
	if _, ok := Lookup("not-a-git-command"); ok {
		t.Fatal("unknown command found")
	}
}

func option(t *testing.T, options []Option, long, short string) Option {
	t.Helper()
	for _, o := range options {
		if o.Long == long && o.Short == short {
			return o
		}
	}
	t.Fatalf("missing option --%s/-%s", long, short)
	return Option{}
}

func TestCommandOptions(t *testing.T) {
	commands := Commands()
	for _, test := range []struct{ command, long, short, arity string }{
		{"push", "force-with-lease", "", "optional"}, {"push", "delete", "d", "none"},
		{"push", "mirror", "", "none"}, {"push", "dry-run", "n", "none"},
		{"reset", "hard", "", "none"}, {"reset", "soft", "", "none"},
		{"reset", "mixed", "", "none"}, {"reset", "keep", "", "none"}, {"reset", "merge", "", "none"},
		{"clean", "force", "f", "none"}, {"branch", "", "D", "none"},
		{"update-ref", "", "d", "none"}, {"update-ref", "stdin", "", "none"}, {"update-ref", "", "z", "none"},
		{"commit", "message", "m", "required"}, {"switch", "create", "c", "required"},
	} {
		t.Run(test.command+"/"+test.long+test.short, func(t *testing.T) {
			o := option(t, commands[test.command].Options, test.long, test.short)
			if o.Arity != test.arity {
				t.Errorf("arity %q != %q", o.Arity, test.arity)
			}
			if o.Kind == "" || o.Source.File == "" || o.Source.Line < 1 || o.Source.Entrypoint == "" {
				t.Errorf("missing provenance: %+v", o)
			}
		})
	}
	force := option(t, commands["clean"].Options, "force", "f")
	if force.Kind != "OPT__FORCE" || force.Type != "OPTION_COUNTUP" {
		t.Errorf("clean -ff must count: %+v", force)
	}
	lease := option(t, commands["push"].Options, "force-with-lease", "")
	if !lease.Negatable || !slices.Contains(lease.Flags, "PARSE_OPT_OPTARG") {
		t.Errorf("lease: %+v", lease)
	}
	contains := option(t, commands["branch"].Options, "contains", "")
	if contains.Negatable || !slices.Contains(contains.Flags, "PARSE_OPT_LASTARG_DEFAULT") {
		t.Errorf("contains: %+v", contains)
	}
	alias := option(t, commands["push"].Options, "branches", "")
	if alias.Alias != "all" || alias.Arity != "none" || !slices.Contains(alias.Flags, "PARSE_OPT_FROM_ALIAS") {
		t.Errorf("alias: %+v", alias)
	}
	// scalar.c also has a static cmd_clone: it must never replace git clone.
	if c := commands["clone"]; c.Source.File != "builtin/clone.c" || len(c.Options) < 30 {
		t.Errorf("wrong clone entrypoint: %+v", c)
	}
	for _, name := range []string{"apply", "pack-refs"} {
		if len(commands[name].Options) == 0 {
			t.Errorf("%s helper tables were not followed", name)
		}
	}
}

func TestSubcommands(t *testing.T) {
	commands := Commands()
	for name, want := range map[string][]string{
		"stash":    {"push", "pop", "drop", "apply", "clear", "branch", "list", "show", "store", "create", "export", "import", "save"},
		"worktree": {"add", "prune", "list", "lock", "unlock", "move", "remove", "repair"},
		"remote":   {"add", "rename", "remove", "set-url", "prune"},
		"config":   {"list", "get", "set", "unset", "edit"},
	} {
		c := commands[name]
		for _, subname := range want {
			found := false
			for _, sub := range c.Subcommands {
				if sub.Name != subname {
					continue
				}
				found = true
				if sub.Parser == "" || sub.Entrypoint == "" || sub.Source.Line < 1 {
					t.Errorf("incomplete %s %s: %+v", name, subname, sub)
				}
				if name == "stash" && subname == "push" {
					option(t, sub.Options, "message", "m")
				}
				if name == "worktree" && subname == "add" {
					option(t, sub.Options, "force", "f")
				}
			}
			if !found {
				t.Errorf("missing %s %s", name, subname)
			}
		}
	}
	if len(commands["worktree"].Options) != 0 || len(commands["stash"].Options) != 0 {
		t.Fatal("child options leaked onto parent command")
	}
}

func TestSharedGrammars(t *testing.T) {
	for _, test := range []struct{ long, short, arity string }{
		{"", "C", "required"}, {"", "c", "required"}, {"git-dir", "", "required"},
		{"work-tree", "", "required"}, {"namespace", "", "required"}, {"config-env", "", "required"},
		{"bare", "", "none"}, {"paginate", "", "none"}, {"", "p", "none"}, {"", "P", "none"},
		{"exec-path", "", "optional"},
	} {
		if o := option(t, GlobalOptions(), test.long, test.short); o.Arity != test.arity {
			t.Errorf("global %+v", o)
		}
	}
	for _, test := range []struct{ long, arity string }{{"max-count", "required"}, {"author", "required"}, {"branches", "optional"}, {"pretty", "optional"}, {"show-notes", "optional"}, {"first-parent", "none"}} {
		if o := option(t, RevisionOptions(), test.long, ""); o.Arity != test.arity {
			t.Errorf("revision %+v", o)
		}
	}
	option(t, DiffOptions(), "output", "")
	if o := option(t, DiffOptions(), "unified", "U"); o.Arity != "optional" {
		t.Errorf("diff unified: %+v", o)
	}
	sources := SourceMap()
	for _, key := range []string{"push", "push --force-with-lease", "commit -m", "stash push --message", "worktree add --force", "$global -C", "$revision --max-count", "$diff --output"} {
		if s := sources[key]; s.File == "" || s.Line < 1 || s.Entrypoint == "" {
			t.Errorf("missing source %s: %+v", key, s)
		}
	}
}

func TestCompletionEvidence(t *testing.T) {
	v := Manifest().Verification
	if v == nil || v.Git != "2.56.0" || v.Checked != 120 || len(v.Unsupported) != 0 {
		t.Fatalf("completion verification: %+v", v)
	}
	for _, mismatch := range v.Mismatches {
		if len(mismatch.Missing) != 0 {
			t.Errorf("unextracted completion options: %+v", mismatch)
		}
		if mismatch.Command != "cherry-pick" && mismatch.Command != "revert" {
			t.Errorf("unexpected mismatch: %+v", mismatch)
		}
		c, _ := Lookup(mismatch.Command)
		if len(c.Adapters) == 0 {
			t.Errorf("conditional options need an explicit adapter: %s", mismatch.Command)
		}
	}
}

func TestCallerOwnedData(t *testing.T) {
	commands := Commands()
	c := commands["push"]
	c.Options[0].Long = "changed"
	delete(commands, "push")
	if c, ok := Lookup("push"); !ok || c.Options[0].Long == "changed" {
		t.Fatal("caller modified embedded registry")
	}
}
