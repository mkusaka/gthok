package resolve

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
)

func pathFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	for _, path := range []string{"tracked.txt", "sub/one.txt", "sub/two.txt", "sub/line\nbreak.txt", "star*.txt"} {
		full := filepath.Join(f.repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.run("-C", f.repo, "add", ".")
	f.commit()
	for path, contents := range map[string]string{"new.txt": "new", "ignored.txt": "ignored", ".gitignore": "ignored.txt\n"} {
		if err := os.WriteFile(filepath.Join(f.repo, path), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestSemanticPathspecs(t *testing.T) {
	f := pathFixture(t)
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"restore", "sub/*.txt"}, []string{"sub/line\nbreak.txt", "sub/one.txt", "sub/two.txt"}},
		{[]string{"rm", "--cached", "tracked.txt"}, []string{"tracked.txt"}},
		{[]string{"add", "new.txt"}, []string{"new.txt"}},
		{[]string{"add", "-f", "ignored.txt"}, []string{"ignored.txt"}},
		{[]string{"add", "-u", "*.txt"}, []string{"star*.txt", "sub/line\nbreak.txt", "sub/one.txt", "sub/two.txt", "tracked.txt"}},
		{[]string{"restore", ":(literal)star*.txt"}, []string{"star*.txt"}},
		{[]string{"restore", "missing"}, nil},
	} {
		op := normalizeFill(t, f, f.resolver(), tc.args...).Operations[0]
		if !op.Target.Paths.Complete || hasUnresolved(op, "target.paths") || !reflect.DeepEqual(op.Target.Paths.Values, tc.want) {
			t.Fatalf("paths %q: %+v; want %q", tc.args, op, tc.want)
		}
		if len(tc.want) == 0 && op.Target.Paths.Kind != model.SetNone {
			t.Fatal("empty resolved set was not represented as none")
		}
	}
}

func TestSemanticPathspecRepositoryPrefix(t *testing.T) {
	f := pathFixture(t)
	r := f.resolver()
	r.Cwd = f.root
	op := normalizeFill(t, f, r, "-C", "repo", "-C", "sub", "restore", "one.txt").Operations[0]
	if !reflect.DeepEqual(op.Target.Paths, model.Exact("sub/one.txt")) {
		t.Fatalf("effective prefix: %+v", op.Target.Paths)
	}
	r = f.resolver()
	r.Cwd = filepath.Join(f.repo, "sub")
	op = normalizeFill(t, f, r, "restore", ":(top)tracked.txt").Operations[0]
	if !reflect.DeepEqual(op.Target.Paths, model.Exact("tracked.txt")) {
		t.Fatalf("top magic: %+v", op.Target.Paths)
	}
	r = f.resolver()
	r.Cwd = filepath.Join(f.repo, "sub")
	op = normalizeFill(t, f, r, "add", "-u").Operations[0]
	if !op.Target.Paths.Complete || !slices.Contains(op.Target.Paths.Values, "tracked.txt") {
		t.Fatalf("repository-wide add -u was limited to the prefix: %+v", op.Target.Paths)
	}
	op = normalizeFill(t, f, f.resolver(), "--literal-pathspecs", "restore", "star*.txt").Operations[0]
	if !reflect.DeepEqual(op.Target.Paths, model.Exact("star*.txt")) {
		t.Fatalf("global literal pathspec mode: %+v", op.Target.Paths)
	}
}

func TestSemanticIncompletePathsStayConservative(t *testing.T) {
	f := pathFixture(t)
	for _, args := range [][]string{
		{"reset", "HEAD", "--", "sub/*.txt"},
		{"restore", "--source=HEAD", "sub/*.txt"},
		{"rm", "sub/*.txt"},
		{"restore", ".", ":(exclude)tracked.txt"},
	} {
		op := normalizeFill(t, f, f.resolver(), args...).Operations[0]
		if op.Target.Paths.Complete || op.Target.Paths.Kind != model.SetPattern || !hasUnresolved(op, "target.paths") {
			t.Fatalf("incomplete pattern was promoted to an exact set %q: %+v", args, op)
		}
	}
	for _, tc := range []struct {
		args    []string
		include string
		exclude string
	}{
		{[]string{"clean", "-n", "*.txt"}, "new.txt", "ignored.txt"},
		{[]string{"clean", "-nx", "*.txt"}, "ignored.txt", "tracked.txt"},
		{[]string{"clean", "-nX", "*.txt"}, "ignored.txt", "new.txt"},
	} {
		op := normalizeFill(t, f, f.resolver(), tc.args...).Operations[0]
		if op.Target.Paths.Complete || !slices.Contains(op.Target.Paths.Values, tc.include) || slices.Contains(op.Target.Paths.Values, tc.exclude) {
			t.Fatalf("untracked/ignored paths: %+v", op.Target.Paths)
		}
	}
}

func TestSemanticCapturedPathspecFile(t *testing.T) {
	f := pathFixture(t)
	args := []string{"restore", "--pathspec-from-file=-", "--pathspec-file-nul"}
	res := gitnormalize.Normalize(model.Invocation{Argv: args, Env: f.env, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("sub/line\nbreak.txt\x00tracked.txt\x00")}})
	if diagnostics := f.resolver().Fill(t.Context(), &res, nil, false); len(diagnostics) > 0 {
		t.Fatal(diagnostics)
	}
	op := res.Operations[0]
	if hasUnresolved(op, "target.paths") || !op.Target.Paths.Complete || !reflect.DeepEqual(op.Target.Paths.Values, []string{"sub/line\nbreak.txt", "tracked.txt"}) {
		t.Fatalf("captured pathspecs: %+v", op)
	}
}
