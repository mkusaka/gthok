package gitargs

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRootOptions(t *testing.T) {
	for _, option := range []string{"-p", "--paginate", "-P", "--no-pager", "--no-replace-objects", "--no-lazy-fetch", "--no-optional-locks", "--no-advice", "--literal-pathspecs", "--no-literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs", "--bare", "--exec-path=/tools"} {
		t.Run(option, func(t *testing.T) {
			g, err := Parse([]string{option, "status", "--short"})
			if err != nil || g.Command != "status" || !reflect.DeepEqual(g.Tokens, []string{option}) || !reflect.DeepEqual(g.Args, []string{"--short"}) {
				t.Fatalf("%+v, %v", g, err)
			}
		})
	}
	for _, option := range []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env", "--attr-source", "--shallow-file"} {
		t.Run(option, func(t *testing.T) {
			value := "value"
			if option == "--config-env" {
				value = "a.b=VAR"
			}
			g, err := Parse([]string{option, value, "version"})
			if err != nil || g.Command != "version" || !reflect.DeepEqual(g.Tokens, []string{option, value}) {
				t.Fatalf("%+v, %v", g, err)
			}
			if _, err := Parse([]string{option}); err == nil {
				t.Fatal("missing argument accepted")
			}
		})
	}
	for _, option := range []string{"--git-dir=", "--work-tree=work", "--namespace=ns", "--config-env=a.b=VAR", "--attr-source=HEAD"} {
		if g, err := Parse([]string{option, "status"}); err != nil || g.Command != "status" {
			t.Fatalf("%s: %+v %v", option, g, err)
		}
	}
	for _, option := range []string{"--", "-", "-Cfoo", "-C=foo", "-cfoo=bar", "-c=a.b", "-pP", "--super-prefix", "--list-cmds", "--shallow-file=foo", "--unknown", "--bare=true", "--no-pager=true", "--config-env=a.b", "--config-env=a.b="} {
		if _, err := Parse([]string{option, "status"}); err == nil {
			t.Errorf("accepted %s", option)
		}
	}
}

func TestEarlyOptionsAndHelpRewrite(t *testing.T) {
	for _, option := range []string{"--exec-path", "--exec-path-suffix", "--html-path", "--man-path", "--info-path", "--list-cmds=main"} {
		g, err := Parse([]string{"-C", "", option, "--invalid"})
		if err != nil || g.Early == "" || g.Command != "" || len(g.Tokens) != 3 {
			t.Fatalf("%s: %+v, %v", option, g, err)
		}
	}
	for _, option := range []string{"-h", "--help", "-v", "--version"} {
		command := "help"
		if option == "-v" || option == "--version" {
			command = "version"
		}
		g, err := Parse([]string{option})
		if err != nil || g.Early != command || g.Command != "" {
			t.Fatalf("%s: %+v, %v", option, g, err)
		}
		g, err = Parse([]string{"-C", "", option, "status"})
		if err != nil || g.Early != "" || g.Command != command || !reflect.DeepEqual(g.Args, []string{"status"}) || !reflect.DeepEqual(g.Tokens, []string{"-C", ""}) {
			t.Fatalf("%s: %+v, %v", option, g, err)
		}
	}
}

func TestRootAgainstGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	env := []string{"HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat"}
	for _, argv := range [][]string{
		{"-C", "", "-v"}, {"--no-advice", "--version"}, {"--no-literal-pathspecs", "version"},
		{"--", "version"}, {"-Cfoo", "version"}, {"--super-prefix", "version"}, {"-pP", "version"},
	} {
		g, parseErr := Parse(argv)
		cmd := exec.Command(git, argv...)
		cmd.Dir, cmd.Env = dir, env
		output, gitErr := cmd.CombinedOutput()
		if (parseErr == nil) != (gitErr == nil) {
			t.Errorf("%q: parse %+v %v; git %v: %s", argv, g, parseErr, gitErr, output)
		}
	}
}

func TestDirFollowsSymlinksBeforeParent(t *testing.T) {
	root := t.TempDir()
	start := filepath.Join(root, "start")
	target := filepath.Join(root, "actual", "nested")
	for _, dir := range []string{start, target} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, filepath.Join(start, "link")); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(root, "actual"))
	if err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{{"link", ".."}, {"link/.."}} {
		g := Global{Chdir: paths}
		if got := g.Dir(start); got != want {
			t.Errorf("%q: got %s, want %s", paths, got, want)
		}
	}
}

func FuzzGlobal(f *testing.F) {
	for _, seed := range []string{"", "-C\x00\x00-v", "--help\x00status", "--git-dir=foo\x00push", "--\x00status", "-c\x00alias.p=push\x00p"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		argv := strings.Split(raw, "\x00")
		g, _ := Parse(argv)
		_ = g.Dir("/start")
		if len(g.Tokens) > len(argv) {
			t.Fatalf("consumed too much: %+v", g)
		}
	})
}
