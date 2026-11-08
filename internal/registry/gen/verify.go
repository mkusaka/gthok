package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mkusaka/gthok/internal/registry"
)

func isolatedEnv(home string) []string {
	var env []string
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if strings.HasPrefix(key, "GIT_") || key == "HOME" || key == "XDG_CONFIG_HOME" || key == "LC_ALL" {
			continue
		}
		env = append(env, v)
	}
	if home == "" {
		home = os.DevNull
	}
	return append(env, "HOME="+home, "XDG_CONFIG_HOME="+home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "LC_ALL=C")
}

func verifyCompletion(commands map[string]registry.Command, grammars map[string]registry.Grammar) (*registry.Verification, error) {
	tmp, err := os.MkdirTemp("", "gthok-registry-verify-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	env := isolatedEnv(tmp)
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = tmp
		cmd.Env = env
		return cmd.Output()
	}
	v, err := run("--version")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(v)) != "git version "+gitVersion {
		return nil, fmt.Errorf("-verify requires git %s, got %s", gitVersion, strings.TrimSpace(string(v)))
	}
	if _, err := run("init", "-q", filepath.Join(tmp, "repo")); err != nil {
		return nil, err
	}
	check := &registry.Verification{Git: gitVersion}
	for _, name := range sortedKeys(commands) {
		c := commands[name]
		if !c.Builtin || slices.Contains(c.Flags, "NO_PARSEOPT") {
			continue
		}
		b, err := run("-C", filepath.Join(tmp, "repo"), name, "--git-completion-helper")
		if err != nil {
			check.Unsupported = append(check.Unsupported, name)
			continue
		}
		check.Checked++
		g := grammars[name]
		expected := map[string]bool{}
		all := map[string]bool{}
		for _, o := range g.Options {
			if o.Long == "" {
				continue
			}
			all[o.Long] = true
			if !slices.Contains(o.Flags, "PARSE_OPT_HIDDEN") && !slices.Contains(o.Flags, "PARSE_OPT_NOCOMPLETE") && !slices.Contains(o.Flags, "PARSE_OPT_FROM_ALIAS") {
				expected[o.Long] = true
			}
		}
		if g.Parser == "revision" {
			for _, o := range grammars["$diff"].Options {
				if o.Long != "" {
					all[o.Long] = true
				}
			}
		}
		actual := map[string]bool{}
		for _, word := range strings.Fields(string(b)) {
			if !strings.HasPrefix(word, "--") || word == "--" {
				continue
			}
			word = strings.TrimSuffix(strings.TrimPrefix(word, "--"), "=")
			if !all[word] && strings.HasPrefix(word, "no-") && all[word[3:]] {
				continue
			}
			if !all[word] && all["no-"+word] {
				word = "no-" + word
			}
			actual[word] = true
		}
		m := registry.CompletionCheck{Command: name}
		for _, word := range sortedKeys(actual) {
			if !all[word] {
				m.Missing = append(m.Missing, word)
			}
		}
		for _, word := range sortedKeys(expected) {
			if !actual[word] {
				m.Extra = append(m.Extra, word)
			}
		}
		if len(m.Missing)+len(m.Extra) > 0 {
			check.Mismatches = append(check.Mismatches, m)
		}
	}
	return check, nil
}
