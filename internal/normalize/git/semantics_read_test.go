package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestT7cReadCommands(t *testing.T) {
	for _, command := range adminReadCommands {
		if command == "cat-file" || command == "format-patch" || command == "diff" || command == "diff-tree" || command == "diff-index" || command == "diff-files" || command == "diff-pairs" || command == "range-diff" || command == "blame" || command == "annotate" || command == "pickaxe" || command == "show" || command == "whatchanged" || command == "verify-commit" || command == "verify-tag" {
			continue
		}
		t.Run(command, func(t *testing.T) {
			r, op := adminTestOperation(t, command)
			if op.Family != "git."+command || op.Action != "git."+command+".read" || op.Effect != model.EffectRead || op.Coverage != model.CoverageSemantic || !r.Classification.MutationFree || !r.Classification.RetrySafe {
				t.Fatal(r)
			}
		})
	}
}

func TestT7cReadSideChannels(t *testing.T) {
	for _, tt := range []struct {
		args   string
		effect model.Effect
		caps   []model.Capability
		safe   bool
	}{
		{"diff", model.EffectRead, []model.Capability{model.CapabilityIndexRefresh, model.CapabilityExternalExec}, false},
		{"--no-optional-locks diff --no-ext-diff --no-textconv", model.EffectRead, nil, true},
		{"--no-optional-locks diff --cached --ext-diff --no-ext-diff --textconv --no-textconv HEAD", model.EffectRead, nil, true},
		{"--no-optional-locks diff --no-ext-diff --no-textconv --ext-diff HEAD", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"--no-optional-locks diff --no-ext-diff --no-textconv --output=report HEAD", model.EffectWrite, []model.Capability{model.CapabilityFileWrite}, false},
		{"--no-optional-locks diff-tree --no-textconv HEAD", model.EffectRead, nil, true},
		{"--no-optional-locks diff-index --cached --no-textconv HEAD", model.EffectRead, nil, true},
		{"diff-files --no-textconv", model.EffectRead, nil, true},
		{"diff-files", model.EffectRead, nil, true},
		{"diff-index --cached HEAD", model.EffectRead, nil, true},
		{"diff-tree HEAD", model.EffectRead, nil, true},
		{"range-diff --no-textconv A..B C..D", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"log --output=report", model.EffectWrite, []model.Capability{model.CapabilityFileWrite}, false},
		{"log --ext-diff", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"log --textconv", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"log -p", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"log -p -s", model.EffectRead, nil, true},
		{"log --show-signature --no-show-signature", model.EffectRead, nil, true},
		{"show", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"show --no-textconv", model.EffectRead, nil, true},
		{"show --no-textconv --output=report", model.EffectWrite, []model.Capability{model.CapabilityFileWrite}, false},
		{"grep --textconv needle -- file", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"grep --no-textconv needle -- file", model.EffectRead, nil, true},
		{"grep -Oless needle -- file", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"blame file", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"blame --no-textconv file", model.EffectRead, nil, true},
		{"cat-file --filters HEAD:file", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"format-patch -1", model.EffectWrite, []model.Capability{model.CapabilityFileWrite}, false},
		{"format-patch --stdout -1", model.EffectRead, nil, true},
		{"format-patch -oout -1", model.EffectWrite, []model.Capability{model.CapabilityFileWrite}, false},
		{"format-patch --output=report -1", model.EffectWrite, []model.Capability{model.CapabilityFileWrite}, false},
		{"verify-commit HEAD", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
		{"verify-tag v1", model.EffectRead, []model.Capability{model.CapabilityExternalExec}, false},
	} {
		t.Run(tt.args, func(t *testing.T) {
			r, op := adminTestOperation(t, strings.Fields(tt.args)...)
			if op.Effect != tt.effect || !reflect.DeepEqual(op.Capabilities, tt.caps) || r.Classification.RetrySafe != tt.safe || r.Classification.MutationFree != tt.safe || op.Coverage != model.CoverageSemantic {
				t.Fatal(r)
			}
		})
	}
	r := Normalize(model.Invocation{Argv: []string{"diff", "--no-ext-diff", "--no-textconv"}, Env: map[string]string{"GIT_OPTIONAL_LOCKS": "0"}})
	if !r.Classification.RetrySafe {
		t.Fatal(r)
	}
}

func TestT7cReadTargetsAndProtocols(t *testing.T) {
	_, op := adminTestOperation(t, "log", "--end-of-options", "-revision")
	if !reflect.DeepEqual(op.Target.Revisions, model.Exact("-revision")) {
		t.Fatal(op.Target)
	}
	_, op = adminTestOperation(t, "log", "HEAD~2..HEAD", "--", "-path")
	if !reflect.DeepEqual(op.Target.Revisions, model.Exact("HEAD~2..HEAD")) || !reflect.DeepEqual(op.Target.Paths.Values, []string{"-path"}) || op.Target.Paths.Kind != model.SetPattern {
		t.Fatal(op.Target)
	}
	_, op = adminTestOperation(t, "log", "--output=first", "--output=second", "--output=second")
	if !reflect.DeepEqual(op.Target.Paths, &model.TargetSet{Kind: model.SetSet, Values: []string{"first", "second"}, Complete: true}) || !reflect.DeepEqual(op.Capabilities, []model.Capability{model.CapabilityFileWrite}) {
		t.Fatal(op)
	}
	_, op = adminTestOperation(t, "format-patch", "--output=explicit", "-1")
	if !reflect.DeepEqual(op.Target.Paths, model.Exact("explicit")) {
		t.Fatal(op.Target)
	}
	_, op = adminTestOperation(t, "format-patch", "-oout", "-1")
	if op.Target.Paths.Kind != model.SetUnknown || op.Parameters["outputDirectory"] != "out" {
		t.Fatal(op)
	}
	_, op = adminTestOperation(t, "ls-files", "--", "-path", "dir/*")
	if !reflect.DeepEqual(op.Target.Paths.Values, []string{"-path", "dir/*"}) || op.Target.Paths.Kind != model.SetPattern {
		t.Fatal(op.Target)
	}
	_, op = adminTestOperation(t, "rev-parse", "--git-common-dir", "--is-bare-repository", "--show-toplevel")
	if op.Action != "git.rev-parse.read" {
		t.Fatal(op)
	}
	for _, mode := range []string{"--batch", "--batch-check", "--batch-command"} {
		r, op := adminTestOperation(t, "cat-file", mode)
		if op.UnknownReason != model.UnknownInputUnavailable || op.Coverage != model.CoverageOpaque || r.Classification.RetrySafe {
			t.Fatal(r)
		}
		if needed, _ := NeedsStdin([]string{"cat-file", mode}); needed {
			t.Fatal("must not capture a live bidirectional pipe")
		}
	}
	for _, tt := range []struct {
		mode string
		data string
	}{
		{"--batch", "HEAD\nHEAD:file\n"},
		{"--batch-check", "HEAD\nHEAD:file\n"},
		{"--batch-command", "info HEAD\ncontents HEAD:file\nflush\n"},
	} {
		r := Normalize(model.Invocation{Argv: []string{"cat-file", tt.mode}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(tt.data)}})
		if !r.Classification.RetrySafe || !reflect.DeepEqual(r.Operations[0].Target.Revisions.Values, []string{"HEAD", "HEAD:file"}) {
			t.Fatal(r)
		}
	}
	r, _ := adminTestOperation(t, "cat-file", "--batch", "--batch-all-objects")
	if !r.Classification.RetrySafe {
		t.Fatal(r)
	}
	for _, args := range [][]string{{"hash-object", "--stdin"}, {"update-index", "--stdin"}} {
		needed, _ := NeedsStdin(args)
		if !needed {
			t.Fatalf("finite one-way input must be requested: %q", args)
		}
	}
}

func TestT7cRealGitEffects(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir, home := t.TempDir(), t.TempDir()
	run := func(input string, argv ...string) string {
		t.Helper()
		cmd := exec.Command(git, argv...)
		cmd.Dir = dir
		cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.test", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.test", "GIT_TERMINAL_PROMPT=0"}
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %q: %v\n%s", argv, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("", "init", "-bmain")
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("", "add", "file")
	run("", "commit", "-mfirst")
	head := run("", "rev-parse", "HEAD")
	tree := run("", "write-tree")
	created := run("", "commit-tree", "-mnot-a-ref-update", tree)
	if created == head || run("", "rev-parse", "HEAD") != head || run("", "cat-file", "-t", created) != "commit" {
		t.Fatal("commit-tree unexpectedly updated HEAD")
	}
	run("", "config", "set", "test.setting", "new")
	if run("", "config", "--get", "test.setting") != "new" {
		t.Fatal("modern set and legacy get disagree")
	}
	run("", "config", "--unset", "test.setting")
	run("", "log", "--output=first-output", "--output=second-output", "-1")
	for _, name := range []string{"first-output", "second-output"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal("repeated --output must account for both truncated files", err)
		}
	}
	patch := run("", "format-patch", "-1")
	if _, err := os.Stat(filepath.Join(dir, patch)); err != nil {
		t.Fatal("default format-patch did not write its advertised file", err)
	}
	r, op := adminTestOperation(t, "format-patch", "-1")
	if r.Classification.MutationFree || !slices.Contains(op.Capabilities, model.CapabilityFileWrite) {
		t.Fatal("default file-writing mode must never be retry-safe", r)
	}
	if !strings.Contains(run("", "format-patch", "--stdout", "-1"), "Subject: [PATCH] first") {
		t.Fatal("format-patch --stdout did not return the patch")
	}
}
