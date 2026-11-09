package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fixtureMacros = `
#define OPT_BOOL(s,l,v,h) { .type=OPTION_SET_INT, .short_name=(s), .long_name=(l), .value=(v), .flags=PARSE_OPT_NOARG }
#define OPT_STRING(s,l,v,a,h) { .type=OPTION_STRING, .short_name=(s), .long_name=(l), .value=(v) }
#define OPT__FORCE(v,h,f) { .type=OPTION_COUNTUP, .short_name='f', .long_name="force", .value=v, .flags=PARSE_OPT_NOARG | (f) }
#define BUNDLE(v) OPT_BOOL('b', "bundle", v, "comma, here"), OPT_STRING('m', "message", v, "arg", "help")
#define OPT_END() { .type=OPTION_END }
`

func fixture(t *testing.T, body string) (*sourceFile, error) {
	t.Helper()
	macros := map[string]macro{}
	if err := readMacros(fixtureMacros, macros); err != nil {
		t.Fatal(err)
	}
	return parseSource("fixture.c", body, macros)
}

func TestMacroAndInitializerExtraction(t *testing.T) {
	f, err := fixture(t, `
int cmd_fixture(int argc, const char **argv) {
 struct option options[] = {
   /* braces { and commas , inside comments do not count */
   BUNDLE(&value),
   OPT__FORCE(&force, "count", PARSE_OPT_NONEG),
   { .type = OPTION_CALLBACK, .long_name = "lease", .flags = PARSE_OPT_OPTARG, .callback = lease_cb },
   { .type = OPTION_CALLBACK, .long_name = "contains", .flags = PARSE_OPT_LASTARG_DEFAULT },
   OPT_END()
 };
 return parse_options(argc, argv, NULL, options, usage, PARSE_OPT_KEEP_DASHDASH);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.tables) != 1 {
		t.Fatalf("tables: %+v", f.tables)
	}
	options := f.tables[0].grammar.Options
	if len(options) != 5 {
		t.Fatalf("options: %+v", options)
	}
	if options[0].Long != "bundle" || options[0].Arity != "none" || options[1].Short != "m" || options[1].Arity != "required" {
		t.Fatalf("bundle: %+v", options[:2])
	}
	if options[2].Type != "OPTION_COUNTUP" || options[2].Kind != "OPT__FORCE" || options[2].Negatable || options[2].Source.Line != 6 {
		t.Errorf("force: %+v", options[2])
	}
	if options[3].Arity != "optional" || options[3].Callback != "lease_cb" {
		t.Errorf("callback: %+v", options[3])
	}
	if options[4].Arity != "required" || !slices.Contains(options[4].Flags, "PARSE_OPT_LASTARG_DEFAULT") {
		t.Errorf("lastarg: %+v", options[4])
	}
	e := &extractor{entrypoints: map[string]*sourceFile{}}
	g := e.functionGrammar(f, "cmd_fixture", map[string]bool{})
	if !slices.Contains(g.ParseFlags, "PARSE_OPT_KEEP_DASHDASH") {
		t.Errorf("parse flags: %+v", g)
	}
}

func TestUnknownOptionConstructorsFailClosed(t *testing.T) {
	for _, test := range []struct{ body, want string }{
		{`OPT_FUTURE('x', "future", &value)`, "unexpanded option constructor"},
		{`{ .type=OPTION_FUTURE, .long_name="future" }`, "unmapped option type"},
		{`{ .type=OPTION_CALLBACK, .long_name="future", .flags=PARSE_OPT_FUTURE }`, "unmapped flag"},
		{`{ .type=OPTION_CALLBACK, .long_name="future", .flags=UNKNOWN_FLAG_ALIAS }`, "unmapped flag"},
		{`{ .type=OPTION_CALLBACK, .long_name="future", .flags=1 }`, "unmapped numeric flag"},
		{`{ .type=OPTION_CALLBACK, .long_name="future", .flags=0x80 }`, "unmapped numeric flag"},
		{`{ .type=OPTION_CALLBACK, .long_name="future", .flags=~PARSE_OPT_NONEG }`, "unmapped flag expression"},
		{`{ .type=OPTION_LOWLEVEL_CALLBACK, .long_name="future", .ll_callback=future_callback }`, "unmapped low-level callback"},
		{`{ .type=OPTION_CALLBACK, .long_name=dynamic_name }`, "nonliteral option spelling"},
	} {
		_, err := fixture(t, `int cmd_fixture(void) { struct option options[] = { `+test.body+`, OPT_END() }; }`)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: got %v, want %s", test.body, err, test.want)
		}
	}
}

func TestUnknownParserFlagsFailClosed(t *testing.T) {
	_, err := fixture(t, `int cmd_fixture(void) { return parse_options(argc,argv,prefix,options,usage,PARSE_OPT_FUTURE); }`)
	if err == nil || !strings.Contains(err.Error(), "unmapped flag") {
		t.Fatalf("unknown parser flag: %v", err)
	}
}

func TestSubcommandsRetainSharedHelpers(t *testing.T) {
	f, err := fixture(t, `
static void add_common_options(void) {
 struct option options[] = { OPT_BOOL(0, "common", &v, "help"), OPT_END() };
}
static void child(int argc, const char **argv) {
 add_common_options();
}
int cmd_fixture(int argc, const char **argv) {
 struct option options[] = {
  { .type=OPTION_SUBCOMMAND, .long_name="child", .subcommand_fn=child },
  OPT_END()
 };
 add_common_options();
 parse_options(argc, argv, NULL, options, usage, 0);
}`)
	if err != nil {
		t.Fatal(err)
	}
	e := &extractor{entrypoints: map[string]*sourceFile{}}
	g := e.functionGrammar(f, "cmd_fixture", map[string]bool{})
	if len(g.Subcommands) != 1 || len(g.Subcommands[0].Options) != 1 || g.Subcommands[0].Options[0].Long != "common" {
		t.Fatalf("shared helper lost: %+v", g.Subcommands)
	}
}

func TestExecutionHelpersDoNotLeakOptions(t *testing.T) {
	f, err := fixture(t, `
static void execution_only(void) {
 struct option options[] = { OPT_BOOL(0, "wrong", &v, "help"), OPT_END() };
}
static void add_common_options(void) {
 struct option options[] = { OPT_BOOL(0, "common", &v, "help"), OPT_END() };
}
int cmd_fixture(int argc, const char **argv) {
 struct option options[] = { OPT_BOOL(0, "own", &v, "help"), OPT_END() };
 add_common_options();
 parse_options(argc, argv, NULL, options, usage, 0);
 execution_only();
 die_for_incompatible_opt2(own, "--own", common, "--common");
}`)
	if err != nil {
		t.Fatal(err)
	}
	e := &extractor{entrypoints: map[string]*sourceFile{}}
	g := e.functionGrammar(f, "cmd_fixture", map[string]bool{})
	if len(g.Options) != 2 || g.Options[0].Long != "own" || g.Options[1].Long != "common" {
		t.Fatalf("leaked execution table: %+v", g.Options)
	}
	if len(g.Exclusive) != 1 || !slices.Equal(g.Exclusive[0], []string{"--own", "--common"}) {
		t.Errorf("exclusivity lost after parse: %+v", g.Exclusive)
	}
	if !f.functions["execution_only"].static || f.functions["cmd_fixture"].static {
		t.Fatal("static function scope lost")
	}
}

func TestConditionalTablesRequireAdapter(t *testing.T) {
	f, err := fixture(t, `
int cmd_fixture(void) {
 if (mode) {
  struct option conditional[] = { OPT_BOOL(0, "conditional", &v, "help"), OPT_END() };
 }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.tables) != 1 || len(f.tables[0].grammar.Adapters) != 1 || !strings.HasPrefix(f.tables[0].grammar.Adapters[0], "conditional-options:") {
		t.Fatalf("missing adapter: %+v", f.tables)
	}
}

func TestManualArity(t *testing.T) {
	f, err := fixture(t, `
int handle_options(const char **argv) {
 if (!strcmp(arg, "--pattern")) { run(); }
 else if (skip_prefix(arg, "--pattern=", &value)) { run(value); }
 else if (!strcmp(arg, "-C")) { use(argv[1]); }
 else if (parse_long_opt("count", argv, &value)) { use(value); }
 else if (!strcmp("--quiet", arg)) { run(); }
}`)
	if err != nil {
		t.Fatal(err)
	}
	options, err := manualOptions(f, "handle_options")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"--pattern": "optional", "-C": "required", "--count": "required", "--quiet": "none"}
	if len(options) != len(want) {
		t.Fatalf("options: %+v", options)
	}
	for _, o := range options {
		if o.Arity != want[spelling(o)] {
			t.Errorf("%s: %s", spelling(o), o.Arity)
		}
	}
}

func TestManualLongOptOverridesAttached(t *testing.T) {
	f, err := fixture(t, `
int handle_revision_opt(const char **argv) {
 if (starts_with(arg, "--glob=") || starts_with(arg, "--exclude=") ||
     starts_with(arg, "--exclude-hidden=")) { forward(argv[0]); }
 if (!strcmp(arg, "--branches") || starts_with(arg, "--branches=") ||
     !strcmp(arg, "--tags") || starts_with(arg, "--tags=") ||
     !strcmp(arg, "--remotes") || starts_with(arg, "--remotes=")) { forward(argv[0]); }
}
int handle_revision_pseudo_opt(const char **argv) {
 if (parse_long_opt("glob", argv, &value)) { use(value); }
 else if (parse_long_opt("exclude", argv, &value)) { use(value); }
 else if (parse_long_opt("exclude-hidden", argv, &value)) { use(value); }
 else if (skip_prefix(arg, "--branches=", &value)) { use(value); }
 else if (skip_prefix(arg, "--tags=", &value)) { use(value); }
 else if (skip_prefix(arg, "--remotes=", &value)) { use(value); }
}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, names := range [][]string{
		{"handle_revision_opt", "handle_revision_pseudo_opt"},
		{"handle_revision_pseudo_opt", "handle_revision_opt"},
	} {
		options, err := manualOptions(f, names...)
		if err != nil {
			t.Fatal(err)
		}
		if len(options) != 6 {
			t.Fatalf("options: %+v", options)
		}
		for _, o := range options {
			optional := slices.Contains([]string{"branches", "tags", "remotes"}, o.Long)
			arity := "required"
			if optional {
				arity = "optional"
			}
			if o.Arity != arity || slices.Contains(o.Flags, "ATTACHED_VALUE") != optional {
				t.Errorf("%v: option %+v", names, o)
			}
		}
	}
}

func TestManualMergeDiffOptions(t *testing.T) {
	f, err := fixture(t, `
int diff_merges_parse_opts(struct rev_info *revs, const char **argv) {
 const char *arg = argv[0];
 if (!suppress_m_parsing && !strcmp(arg, "-m")) { set_default(revs); }
 else if (!strcmp(arg, "-c")) { set_combined(revs); }
 else if (!strcmp(arg, "--cc")) { set_dense_combined(revs); }
 else if (!strcmp(arg, "--dd")) { set_first_parent(revs); }
 else if (!strcmp(arg, "--remerge-diff")) { set_remerge(revs); }
 else if (!strcmp(arg, "--no-diff-merges")) { set_none(revs); }
 else if (!strcmp(arg, "--combined-all-paths")) { revs->combined_all_paths = 1; }
 else if ((argcount = parse_long_opt("diff-merges", argv, &optarg))) { set_diff_merges(revs, optarg); }
 return argcount;
}`)
	if err != nil {
		t.Fatal(err)
	}
	options, err := manualOptions(f, "diff_merges_parse_opts")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"-m": "none", "-c": "none", "--cc": "none", "--dd": "none", "--remerge-diff": "none", "--no-diff-merges": "none", "--combined-all-paths": "none", "--diff-merges": "required"}
	if len(options) != len(want) {
		t.Fatalf("options: %+v", options)
	}
	for _, o := range options {
		if o.Arity != want[spelling(o)] || o.Negatable || len(o.Flags) != 0 || o.Source.Entrypoint != "diff_merges_parse_opts" {
			t.Errorf("merge-diff option: %+v", o)
		}
	}
}

func TestLexerRejectsMalformedC(t *testing.T) {
	for _, s := range []string{`/* incomplete`, `"incomplete`} {
		if _, err := lex(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	ts, err := lex("{ (broken }")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := matching(ts, 0); err == nil {
		t.Fatal("accepted unbalanced initializer")
	}
}

func TestRegeneratePinnedGit(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(home, "src/github.com/git/git")
	if _, err := os.Stat(filepath.Join(src, "parse-options.h")); os.IsNotExist(err) {
		t.Skip("pinned Git source is not installed")
	} else if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "--version")
	cmd.Env = isolatedEnv(t.TempDir())
	version, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(version)) != "git version "+gitVersion {
		t.Skip("completion verification needs local Git " + gitVersion)
	}
	first, second := t.TempDir(), t.TempDir()
	for _, out := range []string{first, second} {
		if err := generate(src, out, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"commands.json", "grammar.json", "source-map.json", "manifest.json"} {
		got, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		again, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		checked, err := os.ReadFile(filepath.Join("..", "data", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, again) {
			t.Errorf("%s regeneration is not deterministic", name)
		}
		if !bytes.Equal(got, checked) {
			t.Errorf("%s differs from checked-in data; regenerate with -verify", name)
		}
	}
}
