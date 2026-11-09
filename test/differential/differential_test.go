//go:build unix

package differential

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
	"github.com/mkusaka/gthok/internal/registry"
)

const defaultSeed int64 = 17

// Commands which can start servers, external tools, or interactive protocols
// are deliberately outside this finite, local-only survey (difftest.py's SKIP).
var skipped = strings.Fields(`daemon instaweb send-email difftool mergetool gui citool gitk gitweb
credential credential-cache credential-store credential-cache--daemon imap-send
web--browse help svn p4 cvsserver cvsimport cvsexportcommit archimport
shell http-backend upload-pack receive-pack upload-archive fsmonitor--daemon
filter-branch remote-ext remote-fd fast-import index-pack unpack-objects
bugreport diagnose scalar submodule subtree version maintenance`)

var usageMessage = regexp.MustCompile(`(?m)^(?:error:|fatal:).*` +
	`(?:unknown (?:option|switch)|ambiguous option|requires a value|takes no value|` +
	`cannot be used together|are incompatible|is incompatible|only one .* allowed|` +
	`unknown subcommand|needs a value|mutually exclusive|option .* expects|` +
	`cannot be combined|not allowed with|invalid option)`)

// shortcut: isolation relies on this fixed relative operand pool; review paths
// and executable options before adding arbitrary fuzz-controlled operands.
var values = []string{"main", "topic", "HEAD", "HEAD~1", "v1", "origin", "file.txt", "1", "x", ".", "origin/main"}

type outcome struct {
	stdout, stderr string
	code           int
	timedOut       bool
}

type sandbox struct {
	t    *testing.T
	git  string
	root string
	env  []string
}

func TestDifferential(t *testing.T) {
	casesValue := os.Getenv("GTHOK_DIFF_CASES")
	if casesValue == "" {
		t.Skip("set GTHOK_DIFF_CASES to run the sandboxed Git 2.56.0 survey")
	}
	cases, err := strconv.Atoi(casesValue)
	if err != nil || cases < 1 {
		t.Fatal("GTHOK_DIFF_CASES must be a positive integer")
	}
	seed := defaultSeed
	if value := os.Getenv("GTHOK_DIFF_SEED"); value != "" {
		seed, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			t.Fatalf("GTHOK_DIFF_SEED: %v", err)
		}
	}
	t.Logf("GTHOK_DIFF_SEED=%d GTHOK_DIFF_CASES=%d", seed, cases)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	template := newSandbox(t, git, filepath.Join(root, "template"))
	if version := template.run("version"); version.code != 0 || strings.TrimSpace(version.stdout) != "git version 2.56.0" {
		t.Skipf("requires exactly git version 2.56.0, got %q", strings.TrimSpace(version.stdout))
	}
	template.initialize()

	// Check the oracle itself before trusting randomly generated cases.
	t.Run("state-oracle", func(t *testing.T) {
		checkStateOracle(t, git, template.root, filepath.Join(root, "oracle"))
	})

	commands, names := surveyCommands()
	revision, diff := registry.RevisionOptions(), registry.DiffOptions()
	random := rand.New(rand.NewSource(seed))
	start := time.Now()
	for i := 0; i < cases; i++ {
		argv := generate(random, commands, names, revision, diff)
		s := copySandbox(t, git, template.root, filepath.Join(root, "case"))
		result := gitnormalize.Normalize(model.Invocation{Argv: argv, Cwd: filepath.Join(s.root, "repo"), Env: s.envMap()})
		var before map[string]string
		if allRead(result) {
			before = s.state()
		}
		actual := s.run(argv...)
		if before != nil {
			if changed := stateChanges(before, s.state()); len(changed) > 0 {
				t.Errorf("seed=%d case=%d read-but-changed argv=%q git=%d timeout=%v changed=%q\nresult=%+v\nstderr=%s", seed, i, argv, actual.code, actual.timedOut, changed, result, actual.stderr)
			}
		}
		usage := isUsage(actual)
		// Git can die on a bad revision before checking leftover options. As in
		// the prototype, neither fatal (128) nor usage (129) proves acceptance;
		// nor does a fatal from a child Git whose exit status the parent maps
		// (pull exits 1 when fetch rejects `--all <repo>`).
		if !actual.timedOut && actual.code != 128 && actual.code != 129 && !strings.Contains(actual.stderr, "fatal: ") && parseReason(result) {
			t.Errorf("seed=%d case=%d git-accepts-gthok-rejects argv=%q git=%d\nresult=%+v\nstderr=%s", seed, i, argv, actual.code, result, actual.stderr)
		} else if usage && !parseReason(result) && result.Classification.Coverage == model.CoverageSemantic {
			t.Logf("seed=%d case=%d git-rejects-gthok-accepts argv=%q git=%d stderr=%s", seed, i, argv, actual.code, actual.stderr)
		}
		if actual.timedOut {
			t.Logf("seed=%d case=%d timeout argv=%q", seed, i, argv)
		}
		if err := removeAll(s.root); err != nil {
			t.Fatalf("seed=%d case=%d argv=%q: %v", seed, i, argv, err)
		}
	}
	t.Logf("seed=%d cases=%d survey elapsed=%s", seed, cases, time.Since(start).Round(time.Millisecond))
}

// removeAll retries briefly: a detached Git child can still be writing into
// the case repository just after the command returns.
func removeAll(path string) error {
	var err error
	for range 20 {
		if err = os.RemoveAll(path); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

func surveyCommands() (map[string]registry.Command, []string) {
	commands := registry.Commands()
	var names []string
	for name, command := range commands {
		if !strings.HasPrefix(name, "-") && !strings.HasSuffix(name, "--helper") && !slices.Contains(skipped, name) &&
			(command.Parser == "parse-options" || command.Parser == "revision" || command.Parser == "diff") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return commands, names
}

func generate(r *rand.Rand, commands map[string]registry.Command, names []string, revision, diff []registry.Option) []string {
	name := names[r.Intn(len(names))]
	g := commands[name].Grammar
	argv := []string{name}
	if len(g.Subcommands) > 0 && r.Intn(5) != 0 {
		sub := g.Subcommands[r.Intn(len(g.Subcommands))]
		argv, g = append(argv, sub.Name), sub.Grammar
	}
	options := append([]registry.Option(nil), g.Options...)
	switch g.Parser {
	case "revision":
		options = append(options, revision...)
	case "diff":
		options = append(options, diff...)
	}
	options = slices.DeleteFunc(options, func(o registry.Option) bool { return o.Long == "" && o.Short == "" })
	count := min(len(options), 1+r.Intn(3))
	for _, index := range r.Perm(len(options))[:count] {
		o, value := options[index], values[r.Intn(len(values))]
		long := o.Long != "" && (o.Short == "" || r.Intn(5) < 3)
		flag := "-" + o.Short
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
		argv = append(argv, values[r.Intn(len(values))])
	}
	return argv
}

func newSandbox(t *testing.T, git, root string) *sandbox {
	t.Helper()
	for _, dir := range []string{root, filepath.Join(root, "home"), filepath.Join(root, "xdg"), filepath.Join(root, "hooks")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return &sandbox{t: t, git: git, root: root, env: []string{
		"PATH=" + filepath.Dir(git) + ":/usr/bin:/bin", "LANG=C", "LC_ALL=C", "TZ=UTC",
		"HOME=" + filepath.Join(root, "home"), "XDG_CONFIG_HOME=" + filepath.Join(root, "xdg"),
		"GIT_CONFIG_GLOBAL=" + filepath.Join(root, "home", ".gitconfig"),
		// Explicit config --system must not write the host's /etc/gitconfig.
		"GIT_CONFIG_SYSTEM=" + filepath.Join(root, "system.config"), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=file", "GIT_SSH_COMMAND=false", "GIT_ASKPASS=false",
		"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "GIT_PAGER=cat", "PAGER=cat", "GIT_ATTR_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Survey", "GIT_AUTHOR_EMAIL=survey@example.test",
		"GIT_COMMITTER_NAME=Survey", "GIT_COMMITTER_EMAIL=survey@example.test",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
		"GIT_CONFIG_COUNT=3", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0=" + filepath.Join(root, "hooks"),
		"GIT_CONFIG_KEY_1=gc.auto", "GIT_CONFIG_VALUE_1=0",
		"GIT_CONFIG_KEY_2=maintenance.auto", "GIT_CONFIG_VALUE_2=false",
	}}
}

func copySandbox(t *testing.T, git, template, root string) *sandbox {
	t.Helper()
	if err := os.CopyFS(root, os.DirFS(template)); err != nil {
		t.Fatal(err)
	}
	return newSandbox(t, git, root)
}

func (s *sandbox) envMap() map[string]string {
	env := make(map[string]string, len(s.env))
	for _, entry := range s.env {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	return env
}

func (s *sandbox) run(argv ...string) outcome {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.git, argv...)
	cmd.Dir = filepath.Join(s.root, "repo")
	if _, err := os.Stat(cmd.Dir); errors.Is(err, os.ErrNotExist) {
		cmd.Dir = s.root
	}
	cmd.Env = s.env
	// Kill the entire command group on timeout, including child Git processes.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// A nil Stdin is /dev/null, never the caller's terminal or a live protocol.
	err := cmd.Run()
	// Children left in the group (e.g. auto-detached maintenance) would race
	// the per-case cleanup.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	out := outcome{stdout: stdout.String(), stderr: stderr.String(), timedOut: ctx.Err() != nil}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) && !out.timedOut {
			s.t.Fatalf("start git %q: %v", argv, err)
		}
		out.code = -1
		if exit != nil {
			out.code = exit.ExitCode()
		}
	}
	return out
}

func (s *sandbox) must(argv ...string) {
	s.t.Helper()
	if out := s.run(argv...); out.code != 0 || out.timedOut {
		s.t.Fatalf("git %q: code=%d timeout=%v\n%s", argv, out.code, out.timedOut, out.stderr)
	}
}

func (s *sandbox) write(name, content string) {
	s.t.Helper()
	if err := os.WriteFile(filepath.Join(s.root, "repo", name), []byte(content), 0600); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sandbox) initialize() {
	s.t.Helper()
	s.must("init", "-q", "--bare", "--template="+filepath.Join(s.root, "hooks"), "origin.git")
	s.must("init", "-q", "-b", "main", "--template="+filepath.Join(s.root, "hooks"), "repo")
	for _, args := range [][]string{{"config", "gc.auto", "0"}, {"config", "maintenance.auto", "false"},
		{"config", "core.hooksPath", filepath.Join(s.root, "hooks")}, {"remote", "add", "origin", "../origin.git"}} {
		s.must(args...)
	}
	for i, file := range []string{"file.txt", "other.txt"} {
		s.write(file, fmt.Sprintf("%d\n", i))
		s.must("add", file)
		s.must("commit", "-qm", fmt.Sprintf("c%d", i))
	}
	s.must("branch", "topic", "HEAD~1")
	s.must("tag", "v1")
	s.must("push", "-q", "origin", "main")
	s.must("fetch", "-q", "origin")
	s.write("file.txt", "0\ndirty\n")
	s.must("stash", "push", "-q")
	s.write("file.txt", "0\ndirty2\n")
	s.write("untracked.txt", "u\n")
}

func (s *sandbox) state() map[string]string {
	s.t.Helper()
	state := map[string]string{}
	query := *s
	query.env = append(slices.Clone(s.env), "GIT_OPTIONAL_LOCKS=0")
	for key, args := range map[string][]string{
		"refs":        {"for-each-ref", "--format=%(refname) %(objectname) %(symref)"},
		"origin-refs": {"--git-dir=../origin.git", "for-each-ref", "--format=%(refname) %(objectname) %(symref)"},
		"index":       {"ls-files", "-s"},
		"stash":       {"stash", "list", "--format=%H"},
	} {
		out := query.run(args...)
		if out.timedOut {
			s.t.Fatalf("state query %q timed out", args)
		}
		state[key] = fmt.Sprintf("%d:%s", out.code, out.stdout)
	}
	// Index stat-cache refreshes, reflogs, and loose/unreachable objects are
	// not logical changes. Config and HEAD are, as are all worktree files.
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != s.root && (entry.Name() == ".git" || rel == "origin.git") {
				for _, file := range []string{"HEAD", "config", "config.worktree"} {
					data, readErr := os.ReadFile(filepath.Join(path, file))
					if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
						return readErr
					}
					if readErr == nil {
						state[filepath.Join(rel, file)] = string(data)
					}
				}
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var data []byte
		if entry.Type()&os.ModeSymlink != 0 {
			link, linkErr := os.Readlink(path)
			data, err = []byte(link), linkErr
		} else {
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return err
		}
		state[rel] = fmt.Sprintf("%s:%x", info.Mode(), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return state
}

func stateChanges(before, after map[string]string) []string {
	var changed []string
	for key, value := range before {
		if other, ok := after[key]; !ok || other != value {
			changed = append(changed, key)
		}
	}
	for key := range after {
		if _, ok := before[key]; !ok {
			changed = append(changed, key)
		}
	}
	slices.Sort(changed)
	return changed
}

func allRead(result model.Result) bool {
	return len(result.Operations) > 0 && !slices.ContainsFunc(result.Operations, func(op model.Operation) bool { return op.Effect != model.EffectRead })
}

func parseReason(result model.Result) bool {
	return slices.ContainsFunc(result.Operations, func(op model.Operation) bool {
		switch op.UnknownReason {
		case model.UnknownFlag, model.UnknownInvalidCombination, model.UnknownParseError, model.UnknownMalformed:
			return true
		}
		return false
	})
}

func isUsage(out outcome) bool {
	return (out.code == 128 || out.code == 129) && usageMessage.MatchString(out.stderr)
}

func checkStateOracle(t *testing.T, git, template, root string) {
	t.Helper()
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"refs", []string{"branch", "oracle"}},
		{"HEAD", []string{"symbolic-ref", "HEAD", "refs/heads/topic"}},
		{"index", []string{"add", "file.txt"}},
		{"stash", []string{"stash", "drop", "-q"}},
		{"worktree", []string{"clean", "-f"}},
		{"config", []string{"config", "survey.changed", "true"}},
		{"global-config", []string{"config", "--global", "survey.changed", "true"}},
		{"system-config", []string{"config", "--system", "survey.changed", "true"}},
		{"origin-refs", []string{"push", "-q", "origin", "HEAD:refs/heads/oracle"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := copySandbox(t, git, template, filepath.Join(root, tt.name))
			before := s.state()
			s.must(tt.args...)
			if reflect.DeepEqual(before, s.state()) {
				t.Fatalf("oracle missed %s mutation", tt.name)
			}
		})
	}
	t.Run("ignore-unreachable-objects", func(t *testing.T) {
		s := copySandbox(t, git, template, filepath.Join(root, "objects"))
		before := s.state()
		s.must("hash-object", "-w", "file.txt")
		s.must("status", "--porcelain")
		if changed := stateChanges(before, s.state()); len(changed) > 0 {
			t.Fatalf("oracle counted objects/stat cache as logical changes: %q", changed)
		}
	})
}

func TestUsageDetection(t *testing.T) {
	for _, tt := range []struct {
		code int
		msg  string
		want bool
	}{
		{129, "error: unknown option `nonsense'\nusage: git push", true},
		{128, "fatal: options --a and --b cannot be used together", true},
		{128, "fatal: not a valid object name: main", false},
		{1, "error: unknown option", false},
		{0, "unknown option", false},
	} {
		if got := isUsage(outcome{code: tt.code, stderr: tt.msg}); got != tt.want {
			t.Errorf("code=%d msg=%q usage=%v, want %v", tt.code, tt.msg, got, tt.want)
		}
	}
}

func TestGeneratorIsolation(t *testing.T) {
	commands, names := surveyCommands()
	r := rand.New(rand.NewSource(defaultSeed))
	revision, diff := registry.RevisionOptions(), registry.DiffOptions()
	for range 1000 {
		argv := generate(r, commands, names, revision, diff)
		if slices.Contains(skipped, argv[0]) || strings.HasPrefix(argv[0], "-") {
			t.Fatalf("unsafe command: %q", argv)
		}
		// All generated paths/URLs come from the fixed, relative value pool.
		for _, token := range argv {
			if strings.Contains(token, "://") || strings.HasPrefix(token, "/") || strings.Contains(token, "../") {
				t.Fatalf("nonlocal operand: %q", argv)
			}
		}
	}
}
