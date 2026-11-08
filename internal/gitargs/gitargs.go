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

// Parse is the bootstrap parser; the full Git root grammar replaces this stub.
func Parse(argv []string) (g Global, err error) {
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			g.Command, g.Args = arg, argv[i+1:]
			return g, nil
		}
		key, value, attached := strings.Cut(arg, "=")
		switch key {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env":
			g.Tokens = append(g.Tokens, arg)
			if !attached {
				i++
				if i == len(argv) {
					return g, fmt.Errorf("%s requires a value", key)
				}
				value = argv[i]
				g.Tokens = append(g.Tokens, value)
			}
			switch key {
			case "-C":
				g.Chdir = append(g.Chdir, value)
			case "-c", "--config-env":
				g.ConfigParams = append(g.ConfigParams, value)
			case "--git-dir":
				g.GitDir = value
			case "--work-tree":
				g.WorkTree = value
			case "--namespace":
				g.Namespace = value
			}
		case "--bare":
			g.Bare = true
			g.Tokens = append(g.Tokens, arg)
		case "--version":
			g.Early = "version"
			g.Tokens = append(g.Tokens, arg)
		case "--help", "-h":
			g.Early = "help"
			g.Tokens = append(g.Tokens, arg)
		case "--":
			if i+1 < len(argv) {
				g.Command, g.Args = argv[i+1], argv[i+2:]
			}
			return g, nil
		default:
			return g, fmt.Errorf("unknown global option %s", arg)
		}
	}
	return g, nil
}

func (g Global) Dir(cwd string) string {
	for _, dir := range g.Chdir {
		if dir == "" {
			continue
		}
		if filepath.IsAbs(dir) {
			cwd = filepath.Clean(dir)
		} else {
			cwd = filepath.Join(cwd, dir)
		}
	}
	return cwd
}
