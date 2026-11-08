//go:build unix

package main

import (
	"slices"
	"strings"

	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/gitmeta"
)

// Only tagged, fixed local queries can be separated from request executions.
// In particular, a user's identical read command or a delegate still counts.
func resolverQuery(args []string, env []string) bool {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if values["GTHOK_RESOLVER_QUERY"] != "1" || values["GTHOK_DELEGATE_ID"] != "" {
		return false
	}
	g, err := gitargs.Parse(args)
	if err != nil || g.Early != "" {
		return false
	}
	args = g.Args
	operand := func(value string) bool {
		return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "\x00\r\n")
	}
	switch g.Command {
	case "rev-parse":
		return slices.Equal(args, []string{"--git-dir", "--git-common-dir", "--is-bare-repository", "--is-inside-work-tree", "--show-prefix"}) ||
			slices.Equal(args, []string{"--show-toplevel"}) ||
			(len(args) == 4 && slices.Equal(args[:3], []string{"--verify", "--quiet", "--end-of-options"}) && operand(args[3]) && strings.HasSuffix(args[3], "^{commit}")) ||
			(len(args) == 2 && args[0] == "--symbolic-full-name" && operand(args[1]))
	case "symbolic-ref":
		return slices.Equal(args, []string{"-q", "HEAD"})
	case "config":
		return slices.Equal(args, []string{"--null", "--list"}) ||
			(len(args) == 3 && args[0] == "--null" && slices.Contains([]string{"--get", "--get-all", "--get-regexp"}, args[1]) && operand(args[2]))
	case "for-each-ref":
		return (len(args) == 1 || len(args) == 2 && operand(args[1])) && args[0] == "--format=%(refname)"
	case "show-ref":
		return len(args) == 0
	case "remote":
		return (len(args) == 3 && slices.Equal(args[:2], []string{"get-url", "--all"}) && operand(args[2])) ||
			(len(args) == 4 && slices.Equal(args[:3], []string{"get-url", "--all", "--push"}) && operand(args[3]))
	case "ls-files":
		return (len(args) == 3 && slices.Equal(args[:2], []string{"-z", "--"}) && !strings.ContainsRune(args[2], '\x00')) ||
			(len(args) == 4 && slices.Equal(args[:3], []string{"-z", "--error-unmatch", "--"}) && !strings.ContainsRune(args[3], '\x00'))
	case "cat-file":
		return len(args) == 2 && (args[0] == "-t" || args[0] == "-s") && gitmeta.ValidSHA(args[1])
	}
	return false
}
