package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mkusaka/gthok/internal/gitmeta"
	"github.com/mkusaka/gthok/internal/install"
)

// LocalName is the personal, uncommitted project config (like CLAUDE.local.md).
const LocalName = ".gthok.local.yaml"

// localPath returns the repository-root .gthok.local.yaml when it exists.
func localPath(cwd, home string) (string, error) {
	root, err := projectRoot(cwd, home)
	if root == "" || err != nil {
		return "", err
	}
	p := filepath.Join(root, LocalName)
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return p, nil
}

// LocalPath is where the local config for cwd lives (it may not exist yet).
func LocalPath(cwd string, env map[string]string) (string, error) {
	project, err := ProjectPath(cwd, env)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(project), LocalName), nil
}

// mergeLocal layers .gthok.local.yaml after the project config. It is written by
// the user, so its hooks run without trust; the same restrictions as a project
// config apply otherwise (no runtime/logging, policy can only tighten).
func mergeLocal(c *Config, notes []string, cwd string, env map[string]string) (*Config, []string, error) {
	ignore := func(path string, err error) (*Config, []string, error) {
		message := "local config ignored: " + err.Error()
		c.Diagnostics = append(c.Diagnostics, message)
		notes = append(notes, message)
		if env["GTHOK_FAIL_CLOSED"] == "1" {
			return c, notes, err
		}
		return c, notes, nil
	}
	local, err := localPath(cwd, env["HOME"])
	if err != nil {
		return ignore(local, err)
	}
	if local == "" {
		return c, notes, nil
	}
	// A committed file would let any clone run hooks without trust.
	if gitTracked(filepath.Dir(local), LocalName, c.Runtime.RealGit, env) {
		notes = append(notes, fmt.Sprintf("Local config: %s (ignored: tracked by git; keep it uncommitted, e.g. in .git/info/exclude)", local))
		return c, notes, nil
	}
	warnWritable(local, &notes)
	data, _, err := readProjectData(local, c.Runtime.FileMaxBytes)
	if err != nil {
		return ignore(local, err)
	}
	p, n, err := parseProject(data, local)
	if err != nil {
		return ignore(local, err)
	}
	if field(n, "trust") != nil {
		return ignore(local, fmt.Errorf("%s: trust is only meaningful in .gthok.yaml", local))
	}
	for _, h := range p.trustHooks() {
		if h.Files != nil {
			return ignore(local, fmt.Errorf("%s: hook %s: files is only meaningful in .gthok.yaml (local hooks run without trust)", local, h.ID))
		}
	}
	notes = append(notes, projectUnknownKeys(n, local)...)
	merged := *c
	if err := mergeProject(&merged, p, n, local, cwd, env, &notes); err != nil {
		return ignore(local, err)
	}
	notes = append(notes, fmt.Sprintf("Local config: %s (loaded; hooks run without trust)", local))
	return &merged, notes, nil
}

// A missing index proves untracked; failed queries must never grant local trust.
func gitTracked(dir, name, override string, env map[string]string) bool {
	gitdir, err := gitmeta.Directory(dir, env)
	if err != nil {
		return true
	}
	index := env["GIT_INDEX_FILE"]
	if index == "" {
		index = filepath.Join(gitdir, "index")
	} else {
		index = gitmeta.Absolute(dir, index)
	}
	if _, err := os.Stat(index); os.IsNotExist(err) {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return true
	}
	if env["GTHOK_REAL_GIT"] != "" {
		override = env["GTHOK_REAL_GIT"]
	}
	real, _, err := install.FindRealGit(install.DiscoverOptions{Self: self, PathEnv: env["PATH"], Override: override})
	if err != nil {
		return true
	}
	values := make(map[string]string, len(env)+3)
	for k, v := range env {
		values[k] = v
	}
	values["GIT_TERMINAL_PROMPT"], values["GIT_PAGER"], values["GIT_OPTIONAL_LOCKS"] = "0", "cat", "0"
	var entries []string
	for k, v := range values {
		entries = append(entries, k+"="+v)
	}
	entries, _, err = install.NextDepth(entries)
	if err != nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, real, "-C", dir, "-c", "core.fsmonitor=false", "ls-files", "--error-unmatch", "--", name)
	cmd.Env, cmd.WaitDelay = entries, 100*time.Millisecond
	err = cmd.Run()
	var exit *exec.ExitError
	return err == nil || !errors.As(err, &exit) || exit.ExitCode() != 1 || ctx.Err() != nil
}
