//go:build unix

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/delegates"
	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/hooks"
	"github.com/mkusaka/gthok/internal/install"
	"github.com/mkusaka/gthok/internal/logging"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/policy"
	"github.com/mkusaka/gthok/internal/process"
	"github.com/mkusaka/gthok/internal/protocol"
	"github.com/mkusaka/gthok/internal/redact"
	"github.com/mkusaka/gthok/internal/resolve"
)

// Wrapper executes args as real git, enforcing configured pre-decisions. It
// returns only on denial/error; successful Exec replaces the process, and Run
// terminates through Result.Exit to preserve the child's exact wait status.
func Wrapper(args []string) int {
	return wrapper(args, os.Args[0], "shim")
}

// Version is the gthok version reported under `git --version`; set by main.
var Version = "dev"

// execGit replaces the process with git, except for `git --version` / `git version`
// on a terminal, which runs git and then reports gthok below git's own version.
// Piped output stays byte-identical for scripts and hooks that parse it.
func execGit(opts process.Options, args []string) error {
	if len(args) != 1 || args[0] != "--version" && args[0] != "version" || !install.IsTTY(os.Stdout) {
		return process.Exec(opts)
	}
	result := process.Run(context.Background(), opts)
	if result.StartErr != nil {
		return result.StartErr
	}
	if result.ExitCode == 0 {
		_, _ = fmt.Fprintf(os.Stdout, "gthok version %s (wrapping %s)\n", Version, opts.Path)
	}
	result.Exit()
	return nil
}

func wrapper(args []string, argv0, via string) int {
	if os.Getenv("GTHOK_PROBE") == "1" {
		_, _ = fmt.Fprintln(os.Stdout, "gthok-probe")
		return 0
	}
	env := environment()
	var redactor *redact.Redactor
	if via == "exec" {
		install.RecordExecEntry(filepath.Join(config.Dir(env), "state"), time.Now())
	}
	fatal := func(err error, code int) int {
		if redactor == nil {
			redactor = redact.New(env)
		}
		fmt.Fprintln(os.Stderr, "gthok:", redactor.String(err.Error()))
		return code
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fatal(err, 1)
	}
	configCwd := effectiveConfigDir(args, cwd, env)
	_, configEnv := effectiveContext(args, cwd, env)
	if !config.HasConfig(configCwd, configEnv) {
		real, err := realGit(nil, env)
		if err != nil {
			return fatal(err, 127)
		}
		childEnv, _, err := install.NextDepth(os.Environ())
		if err != nil {
			return fatal(err, 1)
		}
		err = execGit(process.Options{Path: real, Args: append([]string{argv0}, args...), Env: childEnv}, args)
		return fatal(err, process.StartErrorExitCode(err))
	}
	started := time.Now()
	// Snapshot before any Notify call changes the observable dispositions.
	var helperSignals []os.Signal
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			helperSignals = append(helperSignals, sig)
		}
	}
	redactor = redact.New(env)
	cfg, notes, configErr := config.LoadEffective(config.DefaultPath(env), configCwd, configEnv)
	failClosed := config.FailClosed(configErr, env)
	var real string
	var childEnv []string
	var opts process.Options
	if !failClosed {
		real, err = realGit(cfg, env)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gthok: %s.\n", redactor.String(err.Error()))
			return 127
		}
		childEnv, _, err = install.NextDepth(os.Environ())
		if err != nil {
			return fatal(err, 1)
		}
		opts = process.Options{Path: real, Args: append([]string{argv0}, args...), Env: childEnv}
	}
	if configErr != nil {
		// An invalid effective config cannot be trusted, including its log path.
		cfg = &config.Config{Dir: config.Dir(env)}
		cfg.ApplyDefaults()
	}
	nested := configErr == nil && install.ValidNestedToken(config.Dir(env), env)
	warnIgnoredHooks := func() {
		if configErr == nil && !nested {
			if warning := ignoredHooksWarning(cfg, config.Dir(env), started); warning != "" {
				fmt.Fprintln(os.Stderr, "gthok:", strings.Join(strings.Fields(redactor.String(warning)), " "))
			}
		}
	}
	if configErr == nil && cfg.Logging.Enabled != nil && !*cfg.Logging.Enabled && len(cfg.Hooks) == 0 && len(cfg.Transforms) == 0 && len(cfg.Delegates) == 0 && len(cfg.IgnoredHooks) == 0 && len(cfg.Policy.Rules) == 0 && cfg.Policy.Mode == config.ModeObserve {
		warnIgnoredHooks()
		err = execGit(opts, args)
		return fatal(err, process.StartErrorExitCode(err))
	}
	eventID := logging.NewInvocationID()
	record := logging.Record{InvocationID: eventID, Time: started.UTC(), Mode: cfg.Policy.Mode, Argv: args, Decision: "allow", Diagnostics: cfg.Diagnostics}
	record.Invocation.Via = via
	var logger *logging.Logger
	var logOpenErr error
	debug := func(err error) {
		if err != nil && env["GTHOK_DEBUG"] == "1" {
			fmt.Fprintln(os.Stderr, "gthok:", redactor.String(err.Error()))
		}
	}
	if cfg.Logging.Enabled == nil || *cfg.Logging.Enabled {
		logger, logOpenErr = logging.OpenWithRedactor(cfg.Logging.Path, cfg.Logging.Level, redactor)
	}
	logRecord := func() {
		debug(logOpenErr)
		logOpenErr = nil
		record.DurationMs = time.Since(started).Milliseconds()
		// Config failures are errors, not routine diagnostics: retain them at
		// decision log level even when the requested git operation is allowed.
		record.Errors = append(record.Errors, cfg.Diagnostics...)
		if logger != nil {
			debug(logger.Write(record))
		}
	}
	closeLog := func() {
		if logger != nil {
			debug(logger.Close())
		}
	}
	defer closeLog()
	execLogged := func() int {
		warnIgnoredHooks()
		logRecord()
		execStarted := time.Now()
		execErr := execGit(opts, args)
		code := process.StartErrorExitCode(execErr)
		record.Decision = "exec-error"
		record.Exec = execInfo(process.Result{ExitCode: code, StartErr: execErr, Duration: time.Since(execStarted)})
		logRecord()
		return fatal(execErr, code)
	}
	if configErr != nil {
		if failClosed {
			printDenials(os.Stderr, []denial{{Source: "config", Message: configErr.Error() + "\nfail-closed: not running git — run 'gthok config validate'"}}, redactor.String)
			record.Decision = "deny-invalid-config"
			record.Diagnostics = []string{"invalid config: " + configErr.Error()}
			logRecord()
			return 78
		}
		if warnInvalidConfig(config.Dir(env), configFailurePath(config.DefaultPath(env), notes), configErr, started) {
			warning := strings.Join(strings.Fields(redactor.String(configErr.Error())), " ")
			fmt.Fprintf(os.Stderr, "gthok: invalid config (%s); running git without hooks — run 'gthok config validate'\n", warning)
		}
		record.Decision = "passthrough-invalid-config"
		record.Diagnostics = []string{"invalid config: " + configErr.Error()}
		return execLogged()
	}
	if nested {
		record.Decision = "passthrough-nested"
		return execLogged()
	}
	compatibility := checkGitCompatibility(cfg, real, env)
	inv := invocation(cfg, args, env, cwd, model.StdinData{Status: model.StdinNotCaptured}, true)
	result := normalizeInvocation(cfg, inv)
	spoolNeeded, stdinSkipped := shouldSpool(cfg, compatibility.apply(result), env)
	if spoolNeeded {
		spool, spoolErr := process.SpoolStdin(os.Stdin, cfg.Runtime.StdinMaxBytes, cfg.Runtime.StdinTimeout.D())
		if spool == nil {
			return fatal(spoolErr, 1)
		} // Replay could not preserve consumed bytes.
		defer func() { _ = spool.Close() }()
		opts.Stdin, inv.Stdin = spool, spool.Data()
		result = normalizeInvocation(cfg, inv)
	}
	fillTextSizes(&result, cwd)
	var resolved *model.ResolvedContext
	helperEnv, hookEnv := env, childEnv
	releaseNested := func() {}
	if len(cfg.Hooks) > 0 || len(cfg.Transforms) > 0 {
		helperEnv, releaseNested, err = nestedEnvironment(env)
		if err != nil {
			return fatal(err, 1)
		}
		hookEnv = append(childEnv, "GTHOK_NESTED_TOKEN="+helperEnv["GTHOK_NESTED_TOKEN"])
	}
	defer releaseNested()
	runner := &hooks.Runner{Hooks: cfg.Hooks, BaseDir: cfg.Dir, Concurrency: cfg.Runtime.HookConcurrency, Env: hookEnv, RealGit: real, Redact: redactor.String, Redactor: redactor, Source: via}
	ctx, stopHelpers := watchHelpers(helperSignals, len(cfg.Hooks) > 0 || len(cfg.Transforms) > 0)
	transformed := runTransforms(ctx, cfg, runner, inv, result, real, eventID, via, cfg.Runtime.ResolveAPI != "never", compatibility)
	defer transformed.cleanup()
	if transformed.denial != nil {
		caught := stopHelpers()
		record.Hooks = transformed.results
		record.Warnings = resultWarnings("transform", transformed.results)
		record.Decision = "deny"
		if caught != 0 {
			record.Decision = "canceled"
			record.Exec = execInfo(process.Result{ExitCode: -1, Signal: caught})
			logRecord()
			closeLog()
			transformed.cleanup()
			releaseNested()
			process.Result{ExitCode: -1, Signal: caught}.Exit()
		}
		printDenials(os.Stderr, []denial{*transformed.denial}, redactor.String)
		logRecord()
		return 78
	}
	if len(transformed.ids) > 0 {
		record.Transformed = transformedLog(result, transformed.result, transformed.invocation.Argv, transformed.ids, transformed.envChanged(), transformed.envBy)
		opts.Args = append([]string{argv0}, transformed.invocation.Argv...)
		if !bytes.Equal(inv.Stdin.Data, transformed.invocation.Stdin.Data) {
			replay, err := process.SpoolBytes(transformed.invocation.Stdin.Data)
			if err != nil {
				stopHelpers()
				return fatal(err, 1)
			}
			defer func() { _ = replay.Close() }()
			opts.Stdin = replay
		}
	}
	inv, result = transformed.invocation, transformed.result
	if len(transformed.envBy) > 0 {
		// Rebuild from the edited, unincremented invocation environment.
		opts.Env, _, err = install.NextDepth(environmentList(inv.Env))
		if err != nil {
			stopHelpers()
			return fatal(err, 1)
		}
		token := helperEnv["GTHOK_NESTED_TOKEN"]
		helperEnv = make(map[string]string, len(inv.Env)+2)
		for name, value := range inv.Env {
			helperEnv[name] = value
		}
		if token != "" {
			helperEnv["GTHOK_NESTED"], helperEnv["GTHOK_NESTED_TOKEN"] = "1", token
		}
	}
	subject := policy.Subject{Result: result, Argv: result.Source.Argv, Env: inv.Env}
	if cfg.Runtime.ResolveAPI != "never" {
		globals, parseErr := gitargs.Parse(expandedArgv(inv))
		if parseErr != nil {
			record.Diagnostics = append(record.Diagnostics, parseErr.Error())
		} else {
			resolver := &resolve.Resolver{Env: helperEnv, Cwd: inv.Cwd, Global: globals.Tokens, RealGit: real, Timeout: cfg.Runtime.ResolveTimeout.D()}
			local := resolver.Local()
			resolved = &local
			record.Diagnostics = append(record.Diagnostics, resolver.Fill(ctx, &result, nil, false)...)
			subject.Result, subject.Context = result, resolved
		}
	}
	result = compatibility.apply(result)
	subject.Result = result
	evaluation := evaluateConfig(cfg, subject)
	input := hookInput(inv, real, result, resolved, evaluation, eventID)
	input.Invocation.Via = via
	input.Invocation.EnvChanged = transformed.envChanged()
	record.Classification, record.Operations, record.Errors, record.Alias = result.Classification, result.Operations, result.Errors, result.Source.Alias
	record.Source = &result.Source
	if stdinSkipped {
		record.Diagnostics = append(record.Diagnostics, "stdin not analyzed (nothing could act on it)")
	}
	record.Policy = input.Policy
	pre := runner.Run(ctx, config.PhasePre, runner.Select(config.PhasePre, subject), input)
	post := runner.Select(config.PhasePost, subject)
	// All helper children are reaped before relinquishing signal ownership.
	caught := stopHelpers()
	record.Hooks = append(transformed.results, pre.Results...)
	record.Warnings = append(record.Warnings, evaluation.Warnings...)
	transformWarnings := resultWarnings("transform", transformed.results)
	record.Warnings = append(record.Warnings, transformWarnings...)
	record.Warnings = append(record.Warnings, resultWarnings("hook", pre.Results)...)
	if caught != 0 {
		record.Decision = "canceled"
		record.Exec = execInfo(process.Result{ExitCode: -1, Signal: caught})
		logRecord()
		closeLog()
		if opts.Stdin != nil {
			_ = opts.Stdin.Close()
		}
		releaseNested()
		transformed.cleanup()
		process.Result{ExitCode: -1, Signal: caught}.Exit()
	}
	decision := decide(cfg.Policy.Mode, evaluation, pre, stdinSkipped)
	record.Decision = decision.Decision
	if decision.Decision == "deny" {
		attachHookStderr(decision.Denials, pre, cfg.Hooks)
		printDenials(os.Stderr, decision.Denials, redactor.String)
		logRecord()
		return decision.ExitCode
	}
	printWarnings(os.Stderr, evaluation.Warnings, redactor.String)
	printWarnings(os.Stderr, transformWarnings, redactor.String)
	printHookWarnings(os.Stderr, pre, cfg.Hooks, redactor.String)
	if decision.RelabelPolicy {
		logPolicy := *record.Policy
		logPolicy.Decision = protocol.DecisionAllow
		record.Policy = &logPolicy
	}
	selectedDelegate := delegates.Select(cfg, subject)
	if selectedDelegate != nil {
		record.Diagnostics = append(record.Diagnostics, "selected delegate "+selectedDelegate.ID)
	}
	if selectedDelegate == nil && len(post) == 0 && len(inv.TemporaryFiles) == 0 && (opts.Stdin == nil || !opts.Stdin.RequiresSupervision()) {
		releaseNested()
		transformed.cleanup()
		return execLogged()
	}
	for _, h := range post {
		opts.Capture = opts.Capture || h.CaptureOutput
	}
	opts.CaptureLimit = cfg.Runtime.CaptureLimit
	warnIgnoredHooks()
	var execution process.Result
	if selectedDelegate == nil {
		execution = process.Run(context.Background(), opts)
	} else {
		var overrides map[string]string
		for _, d := range cfg.Delegates {
			if d.ID == selectedDelegate.ID {
				overrides = d.Env
				break
			}
		}
		execution = delegates.Run(selectedDelegate, overrides, opts, func(message string) {
			text := "delegate " + selectedDelegate.ID + ": " + message
			record.Warnings = append(record.Warnings, protocol.Warning{Source: "delegate", ID: selectedDelegate.ID, Text: message})
			record.Diagnostics = append(record.Diagnostics, text)
			fmt.Fprintln(os.Stderr, terminalText("gthok: warning: "+strings.Join(strings.Fields(redactor.String(text)), " ")))
		})
	}
	ctx, stopHelpers = watchHelpers(helperSignals, len(post) > 0)
	if execution.StartErr != nil {
		fatal(execution.StartErr, process.StartErrorExitCode(execution.StartErr))
	}
	if execution.WaitErr != nil {
		debug(execution.WaitErr)
	}
	input.Result = execInfo(execution)
	// Candidates were selected before git ran; apply match.result/exitCode now.
	subject.Outcome = input.Result
	post = runner.Select(config.PhasePost, subject)
	runner.Env = opts.Env
	if token := helperEnv["GTHOK_NESTED_TOKEN"]; token != "" {
		runner.Env = append(append([]string(nil), opts.Env...), "GTHOK_NESTED_TOKEN="+token)
	}
	aggregate := runner.Run(ctx, config.PhasePost, post, input)
	if caught := stopHelpers(); caught != 0 {
		record.Diagnostics = append(record.Diagnostics, "post-hooks interrupted by "+signalName(caught))
	}
	record.Hooks = append(record.Hooks, aggregate.Results...)
	postWarnings := resultWarnings("hook", aggregate.Results)
	record.Warnings = append(record.Warnings, postWarnings...)
	record.Exec = input.Result
	logRecord()
	closeLog()
	releaseNested()
	transformed.cleanup()
	printWarnings(os.Stderr, postWarnings, redactor.String)
	execution.Exit()
	return 0
}

// Capture input only when a configured pre-decision can act on it.
func shouldSpool(c *config.Config, result model.Result, env map[string]string) (spool, skipped bool) {
	argv := result.Source.Argv
	if !analysisNeedsStdin(argv, os.Stdin) {
		return false, false
	}
	s := policy.Subject{Result: result, Argv: argv, Env: env}
	// Unknown input cannot safely justify an unless exclusion.
	couldMatch := func(m config.Match) bool {
		yes, _ := policy.Matches(m, nil, s)
		if !yes {
			yes, _ = policy.MatchMaybe(m, nil, s)
		}
		return yes
	}
	if c.Policy.Mode == config.ModeEnforce {
		if c.Policy.UnknownInput == config.UnknownDeny || c.Policy.UnknownMutation == config.UnknownDeny {
			return true, false
		}
		for _, rule := range c.Policy.Rules {
			if rule.Decision == string(protocol.DecisionDeny) && couldMatch(rule.Match) {
				return true, false
			}
		}
	}
	for _, h := range c.Hooks {
		if h.Phase == config.PhasePre && (h.Enabled == nil || *h.Enabled) && couldMatch(h.Match) {
			return true, false
		}
	}
	for _, t := range c.Transforms {
		if couldMatch(t.Match) {
			return true, false
		}
	}
	if env["GTHOK_NO_DELEGATE"] != "1" {
		for _, d := range c.Delegates {
			if couldMatch(d.Match) {
				return true, false
			}
		}
	}
	for _, h := range c.IgnoredHooks {
		if h.GateBlocked && couldMatch(h.Match) {
			return true, false
		}
	}
	return false, true
}

// Native files contain the text itself, not a JSON envelope. Streams stopped at
// the limit have no known total; regular stdin files retain their original offset.
func fillTextSizes(result *model.Result, cwd string) {
	if result.Classification.Level != model.LevelL1 {
		return
	}
	for _, op := range result.Operations {
		if op.Texts == nil {
			continue
		}
		for _, text := range [3]*model.Text{op.Texts.Title, op.Texts.Body, op.Texts.Notes} {
			if text == nil || !text.Truncated {
				continue
			}
			switch text.Source {
			case "file":
				path := text.Path
				if !filepath.IsAbs(path) {
					path = filepath.Join(cwd, path)
				}
				if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
					text.Bytes = info.Size()
				}
			case "stdin":
				info, err := os.Stdin.Stat()
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				if offset, err := os.Stdin.Seek(0, io.SeekCurrent); err == nil && offset <= info.Size() {
					text.Bytes = info.Size() - offset
				}
			}
		}
	}
}

// watchHelpers owns signals only outside Exec/Run. The returned stop function
// must be called after helper cleanup and joins the watcher before hand-off.
func watchHelpers(watched []os.Signal, needed bool) (context.Context, func() syscall.Signal) {
	if !needed || len(watched) == 0 {
		return context.Background(), func() syscall.Signal { return 0 }
	}
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 3)
	if len(watched) > 0 {
		signal.Notify(signals, watched...)
	}
	done, joined := make(chan struct{}), make(chan struct{})
	var caught syscall.Signal
	go func() {
		defer close(joined)
		select {
		case sig := <-signals:
			caught = sig.(syscall.Signal)
			cancel()
		case <-done:
			select {
			case sig := <-signals:
				caught = sig.(syscall.Signal)
				cancel()
			default:
			}
		}
	}()
	return ctx, func() syscall.Signal {
		signal.Stop(signals)
		close(done)
		<-joined
		cancel()
		return caught
	}
}
