//go:build unix

package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/gitmeta"
	"github.com/mkusaka/gthok/internal/install"
)

func effectiveContext(args []string, cwd string, env map[string]string) (string, map[string]string) {
	g, _ := gitargs.Parse(args)
	cwd = g.Dir(cwd)
	values := make(map[string]string, len(env)+2)
	for k, v := range env {
		values[k] = v
	}
	if g.GitDir != "" {
		values["GIT_DIR"] = g.GitDir
	}
	if g.WorkTree != "" {
		values["GIT_WORK_TREE"] = g.WorkTree
	}
	if g.Bare {
		if g.GitDir == "" {
			values["GIT_DIR"] = cwd
		}
		delete(values, "GIT_WORK_TREE")
	}
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		if values[key] != "" {
			values[key] = gitmeta.Absolute(cwd, values[key])
		}
	}
	return cwd, values
}

func effectiveConfigDir(args []string, cwd string, env map[string]string) string {
	cwd, values := effectiveContext(args, cwd, env)
	root, dir, _, _, err := gitmeta.Repository(cwd, values)
	if err == nil {
		if root != "" {
			return root
		}
		if dir != "" {
			return dir
		}
	}
	return cwd
}

type aliasOutput struct{ data bytes.Buffer }

func (w *aliasOutput) Write(p []byte) (int, error) {
	if w.data.Len()+len(p) > 1<<20 {
		return 0, fmt.Errorf("git alias output exceeds 1 MiB")
	}
	return w.data.Write(p)
}

func loadGitAliases(real string, g gitargs.Global, cwd string, env map[string]string) map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := append(append([]string(nil), g.Tokens...), "config", "--null", "--get-regexp", "^alias\\.")
	cmd := exec.CommandContext(ctx, real, args...)
	cmd.Dir = cwd
	values := make(map[string]string, len(env)+3)
	for k, v := range env {
		values[k] = v
	}
	values["GIT_TERMINAL_PROMPT"], values["GIT_PAGER"], values["GIT_OPTIONAL_LOCKS"] = "0", "cat", "0"
	childEnv, _, err := install.NextDepth(environmentList(values))
	if err != nil {
		return nil
	}
	cmd.Env = childEnv
	cmd.WaitDelay = 100 * time.Millisecond
	var output aliasOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return nil
	}
	data := output.data.Bytes()
	aliases := make(map[string]string)
	for _, entry := range strings.Split(string(data), "\x00") {
		key, value, ok := strings.Cut(entry, "\n")
		if ok && strings.HasPrefix(key, "alias.") {
			aliases[strings.TrimPrefix(key, "alias.")] = value
		}
	}
	return aliases
}
