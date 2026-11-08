//go:build unix

package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/gitmeta"
	"github.com/mkusaka/gthok/internal/install"
	"github.com/mkusaka/gthok/internal/model"
	native "github.com/mkusaka/gthok/internal/normalize/git"
)

func effectiveContext(args []string, cwd string, env map[string]string) (string, map[string]string) {
	g, _ := gitargs.Parse(args)
	values := make(map[string]string, len(env)+2)
	for k, v := range env {
		values[k] = v
	}
	for i := 0; i < len(g.Tokens); i++ {
		key, value, attached := strings.Cut(g.Tokens[i], "=")
		switch key {
		case "-C", "--git-dir", "--work-tree", "-c", "--config-env", "--namespace", "--attr-source", "--shallow-file":
			if !attached {
				i++
				value = g.Tokens[i]
			}
			switch key {
			case "-C":
				cwd = (gitargs.Global{Chdir: []string{value}}).Dir(cwd)
			case "--git-dir":
				values["GIT_DIR"] = value
			case "--work-tree":
				values["GIT_WORK_TREE"] = value
			}
		case "--bare":
			// Git snapshots cwd here, but preserves even an empty inherited GIT_DIR.
			if _, exists := values["GIT_DIR"]; !exists {
				values["GIT_DIR"] = cwd
			}
		}
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
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return nil
		}
	}
	data := output.data.Bytes()
	type record struct {
		name, value string
		exact       bool
	}
	var records []record
	for _, entry := range strings.Split(string(data), "\x00") {
		key, value, ok := strings.Cut(entry, "\n")
		if !ok || !strings.HasPrefix(key, "alias.") {
			continue
		}
		name := strings.TrimPrefix(key, "alias.")
		exact := strings.HasSuffix(name, ".command")
		if exact {
			name = strings.TrimSuffix(name, ".command")
			if name == "" {
				name, exact = "command", false
			}
		}
		records = append(records, record{name, value, exact})
	}
	aliases := make(map[string]string)
	execPath := gitExecPath(real, g.Tokens, cwd, env)
	argv := aliasAnalysisArgv(append(append(append([]string(nil), g.Tokens...), g.Command), g.Args...))
	seen := make(map[string]bool)
	for len(seen) < 128 {
		current, err := gitargs.Parse(argv)
		name := current.Command
		if err != nil || name == "" || seen[name] || native.IsBuiltinCommand(name) {
			break
		}
		seen[name] = true
		if dashedExecutable(name, execPath, current.Dir(cwd), env["PATH"]) {
			break
		}
		for _, r := range records {
			if r.exact && r.name == name || !r.exact && strings.EqualFold(r.name, name) {
				// Resolve ordered records before handing the normalizer its unordered map.
				aliases[name+".command"] = r.value
			}
		}
		result := native.Normalize(model.Invocation{Argv: argv, Aliases: aliases, Env: env})
		if result.Source.Alias == "" || result.Operations[0].UnknownReason == model.UnknownShellAlias {
			break
		}
		argv = result.Source.Argv
	}
	return aliases
}

func gitExecPath(real string, globals []string, cwd string, env map[string]string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, real, append(append([]string(nil), globals...), "--exec-path")...)
	cmd.Dir, cmd.Env = cwd, environmentList(env)
	cmd.WaitDelay = 100 * time.Millisecond
	var output aliasOutput
	cmd.Stdout = &output
	if cmd.Run() != nil {
		return ""
	}
	return strings.TrimSuffix(output.data.String(), "\n")
}

func dashedExecutable(name, execPath, cwd, path string) bool {
	dirs := filepath.SplitList(path)
	if execPath != "" {
		dirs = append([]string{execPath}, dirs...)
	}
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		info, err := os.Stat(filepath.Join(dir, "git-"+name))
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return true
		}
	}
	return false
}
