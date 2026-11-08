//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
)

func TestAliasExecutableLookupAtEachHop(t *testing.T) {
	for _, tt := range []struct {
		name       string
		command    string
		aliases    map[string]string
		executable string
		execPath   string
		want       string
		reason     model.UnknownReason
	}{
		{
			name: "ordinary intermediate hop", command: "a",
			aliases:    map[string]string{"a": "b", "b": "c", "c": "status"},
			executable: "b", want: "b", reason: model.UnknownExternal,
		},
		{
			name: "alias-provided override", command: "lg2",
			aliases:    map[string]string{"lg2": "-c alias.x=status x"},
			executable: "x", want: "x", reason: model.UnknownExternal,
		},
		{
			name: "unused alias override", command: "a",
			aliases:    map[string]string{"a": "-c alias.x=status version"},
			executable: "x", want: "git.version.read",
		},
		{
			name: "intermediate exec-path and chdir", command: "a",
			aliases:    map[string]string{"a": "--exec-path=tools b", "b": "--exec-path=empty c", "c": "status"},
			executable: "b", execPath: "tools", want: "b", reason: model.UnknownExternal,
		},
		{
			name: "later exec-path and chdir", command: "a",
			aliases:    map[string]string{"a": "--exec-path=empty b", "b": "--exec-path=tools c", "c": "status"},
			executable: "c", execPath: "tools", want: "c", reason: model.UnknownExternal,
		},
		{
			name: "builtin precedes executable", command: "a", aliases: map[string]string{"a": "status"},
			executable: "status", want: "status.show",
		},
		{
			name: "executable precedes shell alias", command: "a", aliases: map[string]string{"a": "b", "b": "!echo hi"},
			executable: "b", want: "b", reason: model.UnknownExternal,
		},
		{
			name: "executable precedes ambiguous alias", command: "a",
			aliases:    map[string]string{"a": "b", "b": "status", "b.command": "push"},
			executable: "b", want: "b", reason: model.UnknownExternal,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			real, root, env := gitContextFixture(t)
			repo := filepath.Join(root, "repo")
			contextGit(t, real, root, env, "init", "-q", repo)
			bin := filepath.Join(root, "bin")
			if tt.execPath != "" {
				bin = filepath.Join(repo, tt.execPath)
			} else {
				env["PATH"] = bin + string(os.PathListSeparator) + env["PATH"]
			}
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "git-"+tt.executable), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			c := &config.Config{Runtime: config.Runtime{RealGit: real}}
			c.ApplyDefaults()
			args := []string{"-C", "repo", "-c", "alias.unused=status", tt.command}
			inv := model.Invocation{Argv: args, Aliases: tt.aliases, Env: env, Cwd: root}
			r := normalizeInvocation(c, inv)
			op := r.Operations[0]
			if op.UnknownReason != tt.reason {
				t.Fatalf("executable lookup: %+v", r)
			}
			if tt.reason == model.UnknownExternal {
				if !reflect.DeepEqual(op.Origin.Command, []string{tt.want}) || r.Classification.RetrySafe || r.Classification.Coverage != model.CoverageOpaque {
					t.Fatalf("external command: %+v", r)
				}
			} else if op.Action != tt.want {
				t.Fatalf("alias classification: %+v", r)
			}
			if !reflect.DeepEqual(r.Source.Argv[:4], args[:4]) || !reflect.DeepEqual(r.Source.Global[:4], args[:4]) {
				t.Fatalf("original globals not restored: %+v", r.Source)
			}
			if args[4] != tt.command {
				t.Fatalf("execution argv changed: %q", args)
			}
		})
	}
}
