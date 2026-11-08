package transforms

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestGitMessageAdapters(t *testing.T) {
	for _, command := range []string{"commit", "tag", "merge", "notes"} {
		for _, transport := range []string{"paragraphs", "file", "stdin"} {
			if command == "merge" && transport == "stdin" {
				continue // merge -F - reads the literal file named "-".
			}
			t.Run(command+"/"+transport, func(t *testing.T) {
				args := []string{command}
				field := "body"
				if command == "notes" {
					args, field = append(args, "add"), "notes"
				}
				switch transport {
				case "paragraphs":
					args = append(args, "-mone", "-m", "two")
				case "file":
					args = append(args, "-F", "original")
				case "stdin":
					args = append(args, "-F-")
				}
				if command == "tag" || command == "merge" {
					args = append(args, "topic")
				}
				inv := model.Invocation{Argv: args, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("old")}, ReadFile: func(string) ([]byte, error) { return []byte("old"), nil }}
				before := normalize.Normalize(inv)
				value := "new\n\nparagraph"
				next, after, cleanup, err := Apply(inv, before, protocol.TransformResponse{SchemaVersion: 1, Set: map[string]any{"/operations/0/texts/" + field + "/value": value}})
				defer cleanup()
				if err != nil {
					t.Fatal(err, before.Operations)
				}
				if got := text(after.Operations[0].Texts, field).Value; got != value {
					t.Fatal(got)
				}
				if transport == "paragraphs" && strings.Count(strings.Join(next.Argv, " "), "--message") != 1 {
					t.Fatal(next.Argv)
				}
				if transport == "stdin" && string(next.Stdin.Data) != value {
					t.Fatal(next.Stdin)
				}
				if transport == "file" {
					if len(next.TemporaryFiles) != 1 {
						t.Fatal(next.TemporaryFiles)
					}
					path := next.TemporaryFiles[0]
					data, err := os.ReadFile(path)
					if err != nil || string(data) != value {
						t.Fatal(string(data), err)
					}
					cleanup()
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatal("temporary message was not removed")
					}
				}
			})
		}
	}
}

func TestGitPositionalAdapters(t *testing.T) {
	for _, tc := range []struct {
		argv       []string
		key, value string
	}{
		{[]string{"push", "origin", "HEAD:refs/heads/main"}, "target/remote", "other"},
		{[]string{"fetch", "origin", "main"}, "target/remote", "other"},
		{[]string{"pull", "origin", "main"}, "target/remote", "other"},
		{[]string{"branch", "topic"}, "parameters/ref", "other"},
		{[]string{"branch", "-D", "topic"}, "parameters/ref", "other"},
		{[]string{"tag", "topic"}, "parameters/ref", "other"},
		{[]string{"update-ref", "--no-deref", "-d", "refs/heads/topic"}, "parameters/ref", "refs/heads/other"},
		{[]string{"symbolic-ref", "HEAD", "refs/heads/topic"}, "parameters/ref", "refs/heads/other"},
		{[]string{"push", "origin", "HEAD:refs/heads/main"}, "parameters/refspec", "HEAD:refs/heads/other"},
		{[]string{"fetch", "origin", "refs/heads/main:refs/remotes/origin/main"}, "parameters/refspec", "refs/heads/other:refs/remotes/origin/other"},
		{[]string{"merge", "--no-edit", "topic"}, "parameters/revision", "other"},
	} {
		t.Run(strings.Join(tc.argv, " ")+"/"+tc.key, func(t *testing.T) {
			inv := model.Invocation{Argv: tc.argv}
			next, _, cleanup, err := Apply(inv, normalize.Normalize(inv), protocol.TransformResponse{SchemaVersion: 1, Set: map[string]any{"/operations/0/" + tc.key: tc.value}})
			defer cleanup()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(next.Argv, " "), tc.value) {
				t.Fatal(next.Argv)
			}
		})
	}
}

func TestGitBooleanSwitchAdapters(t *testing.T) {
	for _, tc := range []struct {
		argv  []string
		key   string
		value bool
		flag  string
	}{
		{[]string{"commit", "-m", "message"}, "signoff", true, "--signoff"},
		{[]string{"commit", "-sm", "message"}, "signoff", false, "--no-signoff"},
		{[]string{"commit", "-m", "message"}, "noVerify", true, "--no-verify"},
		{[]string{"commit", "-nm", "message"}, "noVerify", false, "--verify"},
		{[]string{"tag", "topic"}, "force", true, "--force"},
		{[]string{"branch", "--force", "topic"}, "force", false, "--no-force"},
		{[]string{"push", "origin", "HEAD:refs/heads/main"}, "atomic", true, "--atomic"},
		{[]string{"push", "--no-verify", "origin", "HEAD:refs/heads/main"}, "noVerify", false, "--verify"},
		{[]string{"fetch", "origin", "main"}, "append", true, "--append"},
		{[]string{"fetch", "origin", "main"}, "force", true, "--force"},
	} {
		inv := model.Invocation{Argv: tc.argv}
		next, _, cleanup, err := Apply(inv, normalize.Normalize(inv), protocol.TransformResponse{SchemaVersion: 1, Set: map[string]any{"/operations/0/parameters/" + tc.key: tc.value}})
		cleanup()
		if err != nil || !strings.Contains(strings.Join(next.Argv, " "), tc.flag) {
			t.Fatalf("%q: %q %v", tc.argv, next.Argv, err)
		}
	}
	if _, err := nativeArgs(model.EditLocator{Flag: "verify", ValueType: "bool"}, false, []string{"tag"}); !errors.Is(err, ErrUnsupportedEdit) {
		t.Fatal("negated a non-negatable registry switch", err)
	}
}

func TestGitUnsafeAdaptersRejected(t *testing.T) {
	for _, tc := range []struct {
		argv       []string
		key, value string
	}{
		{[]string{"commit"}, "texts/body/value", "new"},
		{[]string{"commit", "-mold"}, "texts/body/value", "new\x00message"},
		{[]string{"commit", "-mold", "--edit"}, "texts/body/value", "new"},
		{[]string{"tag", "-a", "topic"}, "texts/body/value", "new"},
		{[]string{"tag", "-mold", "-Ffile", "topic"}, "texts/body/value", "new"},
		{[]string{"notes", "add", "-mold", "-Ffile"}, "texts/notes/value", "new"},
		{[]string{"push", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/other"}, "target/remote", "other"},
		{[]string{"push", "origin", "HEAD:refs/heads/main"}, "parameters/refspec", ":refs/heads/main"},
		{[]string{"push", "origin", "HEAD:refs/heads/main"}, "parameters/refspec", "+HEAD:refs/heads/main"},
		{[]string{"push", "origin", "HEAD:refs/heads/main"}, "target/remote", "--mirror"},
		{[]string{"branch", "topic"}, "parameters/ref", "--delete"},
		{[]string{"remote", "remove", "origin"}, "target/remote", "other"},
	} {
		inv := model.Invocation{Argv: tc.argv, ReadFile: func(string) ([]byte, error) { return []byte("old"), nil }}
		before := normalize.Normalize(inv)
		next, after, cleanup, err := Apply(inv, before, protocol.TransformResponse{SchemaVersion: 1, Set: map[string]any{"/operations/0/" + tc.key: tc.value}})
		cleanup()
		if !errors.Is(err, ErrUnsupportedEdit) || !reflect.DeepEqual(next.Argv, inv.Argv) || !reflect.DeepEqual(after, before) {
			t.Fatalf("accepted unsafe %q %s: %v", tc.argv, tc.key, err)
		}
	}
}

func TestResolvedProjectionAllowsOnlyDeclaredDerivedTargets(t *testing.T) {
	inv := model.Invocation{Argv: []string{"push", "origin", "HEAD:refs/heads/main"}}
	before := normalize.Normalize(inv)
	before.Operations[0].Target.RemoteURL, before.Operations[0].Target.PushURL = "old", "old"
	after := normalize.Normalize(model.Invocation{Argv: []string{"push", "other", "HEAD:refs/heads/main"}})
	after.Operations[0].Target.RemoteURL, after.Operations[0].Target.PushURL = "new", "new"
	response := protocol.TransformResponse{SchemaVersion: 1, Set: map[string]any{"/operations/0/target/remote": "other"}}
	if err := Validate(before, after, response); err != nil {
		t.Fatal(err)
	}
	after.Operations[0].Target.Refs = model.Exact("refs/heads/unrelated")
	if err := Validate(before, after, response); !errors.Is(err, ErrUnsupportedEdit) {
		t.Fatal("remote edit authorized a ref change", err)
	}
}
