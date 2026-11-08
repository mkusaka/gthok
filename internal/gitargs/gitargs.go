// Package gitargs parses Git's root options before its command word.
package gitargs

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Global struct {
	Chdir        []string
	GitDir       string
	WorkTree     string
	Namespace    string
	Bare         bool
	ConfigParams []string
	Tokens       []string
	Command      string
	Args         []string
	Early        string
}

// Parse follows git.c handle_options, not parse-options. In particular, root
// short options cannot be bundled and "--" is not an option terminator.
func Parse(argv []string) (g Global, err error) {
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") {
			g.Command, g.Args = arg, argv[i+1:]
			return g, nil
		}
		switch arg {
		case "--help", "-h", "--version", "-v":
			command := "help"
			if arg == "--version" || arg == "-v" {
				command = "version"
			}
			if i+1 == len(argv) {
				g.Early = command
			} else {
				g.Command, g.Args = command, argv[i+1:]
			}
			return g, nil
		}
		start := i
		key, value, attached := strings.Cut(arg, "=")
		switch {
		// Git deliberately tests this prefix, even for --exec-path-suffix.
		case strings.HasPrefix(arg, "--exec-path"):
			if !strings.HasPrefix(arg, "--exec-path=") {
				g.Tokens = append(g.Tokens, arg)
				g.Early = "exec-path"
				return g, nil
			}
		case arg == "--html-path" || arg == "--man-path" || arg == "--info-path" || strings.HasPrefix(arg, "--list-cmds="):
			g.Tokens = append(g.Tokens, arg)
			g.Early = strings.TrimPrefix(key, "--")
			return g, nil
		case arg == "-C" || arg == "-c" || key == "--git-dir" || key == "--work-tree" || key == "--namespace" || key == "--config-env" || key == "--attr-source" || arg == "--shallow-file":
			if !attached {
				i++
				if i == len(argv) {
					return g, fmt.Errorf("%s requires a value", key)
				}
				value = argv[i]
			}
			switch key {
			case "-C":
				g.Chdir = append(g.Chdir, value)
			case "-c", "--config-env":
				if key == "--config-env" {
					eq := strings.LastIndexByte(value, '=')
					if eq < 0 || eq == len(value)-1 {
						return g, fmt.Errorf("invalid config environment specification %q", value)
					}
				}
				g.ConfigParams = append(g.ConfigParams, value)
			case "--git-dir":
				g.GitDir = value
			case "--work-tree":
				g.WorkTree = value
			case "--namespace":
				g.Namespace = value
			}
		case arg == "--bare":
			g.Bare = true
		case arg == "-p" || arg == "--paginate" || arg == "-P" || arg == "--no-pager" ||
			arg == "--no-replace-objects" || arg == "--no-lazy-fetch" || arg == "--no-optional-locks" ||
			arg == "--no-advice" || arg == "--literal-pathspecs" || arg == "--no-literal-pathspecs" ||
			arg == "--glob-pathspecs" || arg == "--noglob-pathspecs" || arg == "--icase-pathspecs":
		default:
			return g, fmt.Errorf("unknown global option %s", arg)
		}
		g.Tokens = append(g.Tokens, argv[start:i+1]...)
	}
	return g, nil
}

// Dir applies relative -C paths in order; an empty -C is a no-op.
func (g Global) Dir(cwd string) string {
	for _, dir := range g.Chdir {
		if dir == "" {
			continue
		}
		path := dir
		if !filepath.IsAbs(path) {
			// Join would clean link/.. before following the directory symlink.
			path = cwd + string(filepath.Separator) + path
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			cwd = resolved
		} else {
			// Parse also works offline for nonexistent paths; Git reports chdir errors.
			cwd = filepath.Clean(path)
		}
	}
	return cwd
}
