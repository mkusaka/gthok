//go:build unix

// Package cli wires the management commands and transparent git wrapper.
package cli

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/install"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize"
	native "github.com/mkusaka/gthok/internal/normalize/git"
	"github.com/mkusaka/gthok/internal/policy"
	"github.com/mkusaka/gthok/internal/process"
	"github.com/mkusaka/gthok/internal/protocol"
)

func environment() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

// Helpers share one invocation token, never injected into the user's git command.
func nestedEnvironment(env map[string]string) (map[string]string, func(), error) {
	token, cleanup, err := install.NewNestedToken(config.Dir(env))
	if err != nil {
		return nil, nil, fmt.Errorf("create nested invocation token: %w", err)
	}
	values := make(map[string]string, len(env)+2)
	for k, v := range env {
		values[k] = v
	}
	values["GTHOK_NESTED"], values["GTHOK_NESTED_TOKEN"] = "1", token
	return values, cleanup, nil
}

func realGit(c *config.Config, env map[string]string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	override := env["GTHOK_REAL_GIT"]
	if override == "" && c != nil {
		override = c.Runtime.RealGit
	}
	real, candidates, err := install.FindRealGit(install.DiscoverOptions{Self: self, PathEnv: env["PATH"], Override: override})
	if err != nil && override == "" && len(candidates) > 0 {
		var skipped []string
		for _, candidate := range candidates {
			if candidate.Skipped == "gthok shim" {
				skipped = append(skipped, candidate.Path)
			}
		}
		detail := "none"
		if len(skipped) > 0 {
			detail = strings.Join(skipped, ", ")
		}
		err = fmt.Errorf("real git not found on PATH (skipped: %s). Install Git, or set runtime.realGit / GTHOK_REAL_GIT. Run 'gthok doctor' for details", detail)
	}
	return real, err
}

func invocation(c *config.Config, args []string, env map[string]string, cwd string, stdin model.StdinData, resolveAliases bool) model.Invocation {
	inv := model.Invocation{Argv: args, Env: env, Cwd: cwd, Stdin: stdin}
	g, err := gitargs.Parse(args)
	inv.ReadFile = func(path string) ([]byte, error) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(g.Dir(cwd), path)
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		limit := c.Runtime.FileMaxBytes
		if limit < math.MaxInt64 {
			limit++
		}
		data, err := io.ReadAll(io.LimitReader(f, limit))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > c.Runtime.FileMaxBytes {
			return nil, fmt.Errorf("input too large: file exceeds %d bytes", c.Runtime.FileMaxBytes)
		}
		return data, nil
	}
	if resolveAliases && err == nil && g.Command != "" && !native.IsBuiltinCommand(g.Command) {
		if real, err := realGit(c, env); err == nil {
			inv.Aliases = loadGitAliases(real, g, cwd, env)
		}
	}
	return inv
}

// Config queries already applied root alias overrides in record order. Do not
// apply them a second time, especially after a dashed executable shadowed one.
func aliasAnalysisArgv(argv []string) []string {
	g, _ := gitargs.Parse(argv)
	out := append([]string(nil), argv...)
	for i := 0; i < len(g.Tokens); i++ {
		key, value, attached := strings.Cut(g.Tokens[i], "=")
		switch key {
		case "-c", "--config-env":
			if !attached {
				i++
				value = g.Tokens[i]
			}
			if strings.HasPrefix(strings.ToLower(value), "alias.") {
				value = "gthok." + value[len("alias."):]
				if attached {
					value = key + "=" + value
				}
				out[i] = value
			}
		case "-C", "--git-dir", "--work-tree", "--namespace", "--attr-source", "--shallow-file":
			if !attached {
				i++
			}
		}
	}
	return out
}

func normalizeInvocation(c *config.Config, inv model.Invocation) model.Result {
	analysis := inv
	if inv.Aliases != nil {
		analysis.Argv = aliasAnalysisArgv(inv.Argv)
	}
	result := normalize.Normalize(analysis)
	globals, _ := gitargs.Parse(inv.Argv)
	if inv.Aliases != nil && len(result.Source.Argv) >= len(globals.Tokens) && len(result.Source.Global) >= len(globals.Tokens) {
		copy(result.Source.Argv, globals.Tokens)
		copy(result.Source.Global, globals.Tokens)
	}
	if native.StdinProtocol(result.Source.Argv) && inv.Stdin.Status != model.StdinCaptured {
		for i := range result.Operations {
			op := &result.Operations[i]
			op.Effect, op.Coverage, op.UnknownReason = model.EffectUnknown, model.CoverageOpaque, model.UnknownInputUnavailable
		}
		result.Classification.MutationFree, result.Classification.RetrySafe = false, false
		result.Classification.Coverage, result.Classification.Resolution = model.CoverageOpaque, model.ResolutionUnresolved
	}
	if inv.Aliases == nil {
		return result
	}
	hops, _, _ := native.ExpandAliasHops(analysis.Argv, inv.Aliases, inv.Env)
	for _, argv := range hops {
		copy(argv, globals.Tokens)
		g, err := gitargs.Parse(argv)
		if err != nil || g.Command == "" || native.IsBuiltinCommand(g.Command) {
			continue
		}
		real, err := realGit(c, inv.Env)
		if err != nil || !dashedExecutable(g.Command, gitExecPath(real, g.Tokens, inv.Cwd, inv.Env), g.Dir(inv.Cwd), inv.Env["PATH"]) {
			continue
		}
		unknown := model.NewUnknown(model.LevelL1, model.UnknownExternal, "external Git executable")
		unknown.Registry, unknown.Source = result.Registry, model.Source{Kind: "command", Argv: argv, Global: g.Tokens}
		unknown.Operations[0].Origin.Command = []string{g.Command}
		return unknown
	}
	return result
}

func expandedArgv(inv model.Invocation) []string {
	analysis := inv.Argv
	if inv.Aliases != nil {
		analysis = aliasAnalysisArgv(analysis)
	}
	args, _, reason := native.ExpandAlias(analysis, inv.Aliases)
	if reason != model.UnknownNone {
		return inv.Argv
	}
	g, _ := gitargs.Parse(inv.Argv)
	if len(args) >= len(g.Tokens) {
		copy(args, g.Tokens)
	}
	return args
}

func hookInput(inv model.Invocation, real string, result model.Result, ctx *model.ResolvedContext, evaluation policy.Evaluation, eventID string) protocol.Input {
	return protocol.Input{
		SchemaVersion: model.SchemaVersion, EventID: eventID,
		Invocation:     protocol.InvocationInfo{Via: "shim", Argv: inv.Argv, Alias: result.Source.Alias, Expanded: result.Source.Argv, Cwd: inv.Cwd, RealGit: real, IsTTY: install.IsTTY(os.Stdout), StdinTTY: install.IsTTY(os.Stdin)},
		Classification: result.Classification, Operations: result.Operations, Errors: result.Errors, Context: ctx,
		Policy: &protocol.PolicyInfo{Mode: evaluation.Mode, Decision: evaluation.Decision, Reasons: evaluation.Reasons, Rules: evaluation.Rules},
	}
}

func execInfo(result process.Result) *protocol.ExecResult {
	info := &protocol.ExecResult{ExitCode: result.ExitCode, Signal: signalName(result.Signal), CoreDump: result.CoreDump, DurationMs: result.Duration.Milliseconds()}
	if result.StartErr != nil {
		info.StartError = result.StartErr.Error()
	}
	capture := func(out *process.Captured) *protocol.Output {
		if out == nil {
			return nil
		}
		return &protocol.Output{Base64: base64.StdEncoding.EncodeToString(out.Data), Size: out.Size, Truncated: out.Truncated}
	}
	info.Stdout, info.Stderr = capture(result.Stdout), capture(result.Stderr)
	return info
}

func signalName(sig syscall.Signal) string {
	if sig == 0 {
		return ""
	}
	switch sig {
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	case syscall.SIGILL:
		return "SIGILL"
	case syscall.SIGTRAP:
		return "SIGTRAP"
	case syscall.SIGABRT:
		return "SIGABRT"
	case syscall.SIGBUS:
		return "SIGBUS"
	case syscall.SIGFPE:
		return "SIGFPE"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGUSR1:
		return "SIGUSR1"
	case syscall.SIGSEGV:
		return "SIGSEGV"
	case syscall.SIGUSR2:
		return "SIGUSR2"
	case syscall.SIGPIPE:
		return "SIGPIPE"
	case syscall.SIGALRM:
		return "SIGALRM"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGSTOP:
		return "SIGSTOP"
	case syscall.SIGTSTP:
		return "SIGTSTP"
	case syscall.SIGCONT:
		return "SIGCONT"
	case syscall.SIGTTIN:
		return "SIGTTIN"
	case syscall.SIGTTOU:
		return "SIGTTOU"
	default:
		return fmt.Sprintf("SIG%d", sig)
	}
}

// Source notes identify the config being loaded when validation failed.
func configFailurePath(fallback string, notes []string) string {
	for i := len(notes) - 1; i >= 0; i-- {
		for _, prefix := range []string{"Project config: ", "Global config: "} {
			if path, ok := strings.CutPrefix(notes[i], prefix); ok {
				if end := strings.LastIndex(path, " ("); end >= 0 {
					return path[:end]
				}
			}
		}
	}
	return fallback
}

func ignoredHooksWarning(c *config.Config, dir string, now time.Time) string {
	project := c.IgnoredProject
	if project == nil || c.Logging.IgnoredHooksWarning != nil && !*c.Logging.IgnoredHooksWarning {
		return ""
	}
	var inactive []string
	for _, h := range c.IgnoredHooks {
		if h.Enabled == nil || *h.Enabled {
			inactive = append(inactive, ignoredHookDescription(h))
		}
	}
	if len(inactive) == 0 || !warnThrottled(dir, "ignored-hooks", project.Path, project.Digest, now) {
		return ""
	}
	kind := "hooks"
	for _, h := range c.IgnoredHooks {
		if h.Delegate {
			kind = "hooks/transforms/delegates"
			break
		}
		if h.Transform {
			kind = "hooks/transforms"
		}
	}
	return fmt.Sprintf("%d project %s in %s are not active: %s; review and run 'gthok config trust'; for permanent repository trust: gthok config trust repo .", len(inactive), kind, project.Path, strings.Join(inactive, "; "))
}

func ignoredHookDescription(h config.IgnoredHook) string {
	reason := h.Reason
	if len(h.ChangedFiles) > 0 {
		files := h.ChangedFiles
		reason += ": " + strings.Join(files[:min(3, len(files))], ", ")
		if len(files) > 3 {
			reason += fmt.Sprintf(", …and %d more", len(files)-3)
		}
	} else if h.Reason == "changed since trust" {
		reason += ": definition changed"
	}
	if h.Delegate {
		return fmt.Sprintf("delegate %s inactive: %s", h.ID, reason)
	}
	if h.Transform {
		return fmt.Sprintf("transform %s inactive: %s", h.ID, reason)
	}
	if h.GateBlocked || h.Phase == config.PhasePre && h.OnError == config.OnErrorDeny {
		return fmt.Sprintf("gate hook %s inactive: %s", h.ID, reason)
	}
	return fmt.Sprintf("hook %s inactive: %s", h.ID, reason)
}

// State is best-effort: any filesystem failure leaves the warning visible.
// A per-pair flock makes the ten-minute window apply across wrapper processes.
func warnInvalidConfig(dir, path string, configErr error, now time.Time) (warn bool) {
	return warnThrottled(dir, "invalid-config", path, configErr.Error(), now)
}

func warnThrottled(dir, kind, path, version string, now time.Time) (warn bool) {
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0700); err != nil {
		return true
	}
	info, err := os.Lstat(state)
	if err != nil || !info.IsDir() {
		return true
	}
	if err := os.Chmod(state, 0700); err != nil {
		return true
	}
	digest := sha256.Sum256([]byte(path + "\x00" + version))
	name := filepath.Join(state, fmt.Sprintf("%s-%x.warning", kind, digest))
	fd, err := syscall.Open(name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return true
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() {
		if err := file.Close(); err != nil {
			warn = true
		}
	}()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return true
	}
	if err := file.Chmod(0600); err != nil {
		return true
	}
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return true
	}
	data, err := io.ReadAll(io.LimitReader(file, 128))
	if err != nil {
		return true
	}
	if len(data) != 0 {
		last, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
		if err != nil {
			return true
		}
		if elapsed := now.Sub(last); elapsed >= 0 && elapsed < 10*time.Minute {
			return false
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return true
	}
	if err := file.Truncate(0); err != nil {
		return true
	}
	_, _ = file.WriteString(now.UTC().Format(time.RFC3339Nano) + "\n")
	return true
}
