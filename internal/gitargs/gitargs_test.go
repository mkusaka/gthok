package gitargs

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	argv := []string{"-C", "repo", "-C", "sub", "-c", "alias.p=push", "--git-dir=../meta", "--work-tree", "/work", "--bare", "push", "origin", "main"}
	g, err := Parse(argv)
	if err != nil {
		t.Fatal(err)
	}
	if g.Command != "push" || g.GitDir != "../meta" || g.WorkTree != "/work" || !g.Bare || g.Dir("/start") != "/start/repo/sub" || !reflect.DeepEqual(g.Args, []string{"origin", "main"}) || !reflect.DeepEqual(g.Tokens, argv[:10]) {
		t.Fatalf("parse: %+v", g)
	}
	for _, argv := range [][]string{{"-C"}, {"-c"}, {"--git-dir"}, {"--work-tree"}, {"--unknown"}} {
		if _, err := Parse(argv); err == nil {
			t.Fatalf("accepted %q", argv)
		}
	}
}
func TestDirAndEarly(t *testing.T) {
	g := Global{Chdir: []string{"", "relative", "/absolute", "child"}}
	if g.Dir("/start") != "/absolute/child" {
		t.Fatal(g.Dir("/start"))
	}
	g, err := Parse([]string{"--version"})
	if err != nil || g.Early != "version" || g.Command != "" {
		t.Fatalf("early: %+v, %v", g, err)
	}
}
