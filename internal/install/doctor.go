package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusInfo Status = "info"
)

type Check struct {
	Name   string
	Status Status
	Detail string
	Fix    string
}
type DoctorOptions struct {
	Self     string
	Version  string
	PathEnv  string
	Override string
	// BinDir is the fallback when state has no recorded shims.
	BinDir   string
	StateDir string
	// Env is passed to diagnostics and inspected for GTHOK_DEPTH.
	// nil inherits the process environment. Non-nil (even empty) is exact.
	Env []string
	// Stdin defaults to os.Stdin and is inspected, never read.
	Stdin *os.File
}

// Checks returns independent diagnostics; config/hook checks can be appended.
func Checks(o DoctorOptions) []Check {
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	binary := Check{Name: "gthok", Status: StatusOK, Detail: o.Self + " (version " + o.Version + ")"}
	if info, err := os.Stat(o.Self); err != nil {
		binary.Status, binary.Detail = StatusFail, fmt.Sprintf("cannot stat gthok %q: %v", o.Self, err)
	} else if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		binary.Status, binary.Detail = StatusFail, "gthok is not a regular executable file: "+o.Self
	} else if o.Version == "dev" {
		binary.Status = StatusWarn
		binary.Detail += "; development build: version unknown; features may differ from the docs"
	}
	checks := []Check{binary}
	real, candidates, err := FindRealGit(DiscoverOptions{PathEnv: o.PathEnv, Self: o.Self, Override: o.Override})
	if err != nil {
		checks = append(checks, Check{Name: "real-git", Status: StatusFail, Detail: err.Error(), Fix: "Install Git or set runtime.realGit / GTHOK_REAL_GIT to its absolute executable path."})
	} else {
		checks = append(checks, Check{Name: "real-git", Status: StatusOK, Detail: real})
	}
	state, shimErr := loadState(o.StateDir)
	shims := make([]string, 0, len(state))
	for shim := range state {
		shims = append(shims, shim)
	}
	sort.Strings(shims)
	if shimErr == nil && len(shims) == 0 {
		var shim string
		shim, shimErr = shimPath(o.BinDir)
		if shimErr == nil {
			shims = append(shims, shim)
		}
	}
	if shimErr != nil {
		checks = append(checks, Check{Name: "path-precedence", Status: StatusFail, Detail: shimErr.Error()}, Check{Name: "shim-ownership", Status: StatusFail, Detail: shimErr.Error()})
	} else {
		for _, shim := range shims {
			_, detail, fix := pathPrecedence(shim, o.PathEnv)
			status := StatusOK
			if fix != "" {
				status = StatusWarn
			}
			checks = append(checks, Check{Name: "path-precedence", Status: status, Detail: detail, Fix: fix})
			owned, err := ownedShim(shim, o.Self, state)
			ownership := Check{Name: "shim-ownership", Status: StatusOK, Detail: shim + " is an owned symlink"}
			if err != nil {
				ownership.Status, ownership.Detail = StatusFail, shim+": "+err.Error()
			} else if !owned {
				ownership.Status, ownership.Detail = StatusWarn, shim+" is absent or not managed by gthok"
				ownership.Fix = "Run gthok shim install with --bin-dir set to " + strconv.Quote(filepath.Dir(shim)) + "; resolve any foreign path collision first."
			}
			checks = append(checks, ownership)
		}
	}
	if entry := execEntryCheck(o.StateDir, checks, time.Now()); entry != nil {
		checks = append(checks, *entry)
	}
	recursion := Check{Name: "recursion-risk", Status: StatusOK, Detail: "Discovery excludes self and gthok-named executables; nesting is bounded by GTHOK_DEPTH. GTHOK_NESTED=1 alone does not bypass analysis; helper bypass requires a live invocation token in GTHOK_NESTED_TOKEN."}
	var risks []string
	if _, _, err := NextDepth(env); err != nil {
		risks = append(risks, err.Error())
	}
	if o.Override != "" {
		for _, c := range candidates {
			if c.Skipped == "gthok shim" {
				risks = append(risks, "real git override points at a gthok shim")
			}
		}
	}
	if real != "" {
		owned, ownErr := IsOwnedShim(real, o.Self, o.StateDir)
		if owned {
			risks = append(risks, "discovery selected a recorded shim for another renamed gthok installation")
		} else if ownErr != nil {
			risks = append(risks, "cannot verify discovered executable ownership: "+ownErr.Error())
		}
	}
	if len(risks) > 0 {
		recursion.Status, recursion.Detail = StatusFail, strings.Join(risks, "; ")
		recursion.Fix = "Point the real Git override at Git, and inspect nested Git invocations."
	}
	checks = append(checks, recursion)
	version := Check{Name: "git-version", Status: StatusInfo, Detail: "Not checked: no real git was discovered."}
	// Never execute a discovered known shim, even a renamed one caught by state.
	owned, _ := IsOwnedShim(real, o.Self, o.StateDir)
	if real != "" && !owned {
		out, err := runDiagnostic(real, env, "--version")
		if err != nil {
			version.Status, version.Detail = StatusWarn, "git --version failed: "+err.Error()+"; compatibility=unverified"
		} else {
			version.Status, version.Detail = gitVersionStatus(out)
		}
		if version.Status == StatusWarn {
			version.Fix = "Use Git " + GitBaseline + "; " + GitBaseline + " is the verified normalization baseline."
		}
	}
	checks = append(checks, version)
	stdin := o.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	detail := fmt.Sprintf("%s/%s; stdin tty=%t; ", runtime.GOOS, runtime.GOARCH, stdinTTY(stdin))
	tty, ttyErr := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if ttyErr == nil {
		_ = tty.Close()
		detail += "/dev/tty available; "
	} else {
		detail += "/dev/tty unavailable (no controlling terminal or access denied); "
	}
	detail += "Unix job control uses the foreground process group; supervised git must remain in that group."
	if runtime.GOOS == "linux" {
		info, err := os.Stat("/proc")
		detail += fmt.Sprintf(" /proc available=%t.", err == nil && info.IsDir())
	}
	checks = append(checks, Check{Name: "platform-process", Status: StatusInfo, Detail: detail})
	return checks
}

const execEntryFile = "exec-entry"

// RecordExecEntry marks a recent `gthok exec` (shell function or script) for
// doctor. Best-effort: the wrapper must not fail or slow down over it.
func RecordExecEntry(stateDir string, now time.Time) {
	path := filepath.Join(stateDir, execEntryFile)
	if os.Chtimes(path, now, now) == nil || os.MkdirAll(stateDir, 0700) != nil {
		return
	}
	if f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600); err == nil {
		_ = f.Close()
	}
}

// execEntryCheck reports `gthok exec` use within a week. Without an owned shim,
// it downgrades the shim's PATH/ownership warnings to info: the user chose the
// shell entry, whose gap (child-process git) the check itself names.
func execEntryCheck(stateDir string, checks []Check, now time.Time) *Check {
	info, err := os.Stat(filepath.Join(stateDir, execEntryFile))
	if err != nil || now.Sub(info.ModTime()) > 7*24*time.Hour {
		return nil
	}
	used := "gthok exec last used " + info.ModTime().Format("2006-01-02 15:04:05 -07:00")
	shimOK := false
	for _, c := range checks {
		if c.Name == "shim-ownership" && c.Status == StatusOK {
			shimOK = true
		}
	}
	if shimOK {
		return &Check{Name: "shell-entry", Status: StatusInfo, Detail: used + "; the shim also covers git started by scripts and other programs"}
	}
	for i := range checks {
		if (checks[i].Name == "path-precedence" || checks[i].Name == "shim-ownership") && checks[i].Status == StatusWarn {
			checks[i].Status = StatusInfo
		}
	}
	return &Check{Name: "shell-entry", Status: StatusInfo, Detail: used + " (shell function or scripts); without the shim, git started by scripts, programs or agents that skip shell rc files bypasses gthok", Fix: "Run gthok shim install to cover them."}
}

var gitVersionRE = regexp.MustCompile(`^git version ([0-9]+\.[0-9]+\.[0-9]+(?:[-+.][^[:space:]]+)?)(?:[[:space:]]|$)`)

// GitBaseline is the exact Git release used to generate the grammar.
const GitBaseline = "2.56.0"

// GitVersion probes only this executable, without Git globals or stdin. Callers
// retain the result for one invocation, never across executable replacements.
func GitVersion(path string, env []string) (string, error) {
	childEnv, _, err := NextDepth(env)
	if err != nil {
		return "", err
	}
	childEnv = append(childEnv, "GTHOK_VERSION_QUERY=1")
	out, err := runDiagnostic(path, childEnv, "--version")
	if err != nil {
		return "", err
	}
	return parseGitVersion(out)
}

func parseGitVersion(out string) (string, error) {
	m := gitVersionRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("unrecognized git version output: %s", strings.TrimSpace(out))
	}
	return m[1], nil
}

func gitVersionStatus(out string) (Status, string) {
	version, err := parseGitVersion(out)
	if err != nil {
		return StatusWarn, "Unrecognized git version output: " + strings.TrimSpace(out) + "; compatibility=unverified"
	}
	if version != GitBaseline {
		return StatusWarn, "git " + version + " differs from the verified " + GitBaseline + " baseline; compatibility=unverified"
	}
	return StatusInfo, strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]) + "; verified baseline is " + GitBaseline + "; compatibility=verified"
}

// Bounded diagnostics cannot grow stdout indefinitely or wait for inherited
// pipes after timeout (for example, a script that spawns a background child).
type diagnosticOutput struct {
	data      []byte
	truncated bool
}

func (w *diagnosticOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 64*1024 - len(w.data)
	if len(p) > remaining {
		p = p[:remaining]
		w.truncated = true
	}
	w.data = append(w.data, p...)
	return n, nil
}

// diagnosticTimeout bounds `git --version` and probes; generous because a cold
// start on a loaded machine can take seconds. Tests shorten it.
var diagnosticTimeout = 10 * time.Second

func runDiagnostic(path string, env []string, args ...string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), diagnosticTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, abs, args...)
	cmd.Env = env
	cmd.WaitDelay = 100 * time.Millisecond
	var out diagnosticOutput
	cmd.Stdout = &out
	err = cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("diagnostic timed out after %s: %w", diagnosticTimeout, ctx.Err())
	}
	if err != nil {
		return "", err
	}
	if out.truncated {
		return "", fmt.Errorf("diagnostic stdout exceeds 64 KiB")
	}
	return string(out.data), nil
}
