package config

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

func validateGitMatch(p string, m Match, add func(string, string)) {
	for _, criterion := range []struct {
		name    string
		values  []string
		choices []string
	}{
		{"scopes", m.Scopes, []string{"local-ref", "remote-ref", "index", "worktree", "config", "objects", "stash", "reflog", "remote-config", "submodule", "worktree-admin", "hooks", "file-output"}},
		{"capabilities", m.Capabilities, []string{"external-exec", "editor", "network", "file-write", "index-refresh", "interactive", "pager"}},
	} {
		for i, value := range criterion.values {
			if !slices.Contains(criterion.choices, value) {
				add(fmt.Sprintf("%s.%s[%d]", p, criterion.name, i), "must be one of "+strings.Join(criterion.choices, ", "))
			}
		}
	}
	for _, criterion := range []struct {
		name     string
		patterns []string
	}{
		{"repoRoots", m.RepoRoots}, {"worktrees", m.Worktrees}, {"commonDirs", m.CommonDirs},
		{"remoteURLs", m.RemoteURLs}, {"refs", m.Refs}, {"paths", m.Paths},
	} {
		for i, pattern := range criterion.patterns {
			key := fmt.Sprintf("%s.%s[%d]", p, criterion.name, i)
			if pattern == "" || strings.ContainsRune(pattern, 0) {
				add(key, "glob must be non-empty and NUL-free")
			}
			if _, err := filepath.Match(pattern, ""); err != nil {
				add(key, "invalid glob: "+err.Error())
			}
			switch criterion.name {
			case "refs":
				if pattern != "**" && (!strings.HasPrefix(pattern, "refs/") || pattern == "refs/") || strings.Contains(pattern, ":") {
					add(key, "must be a full ref-name glob such as refs/heads/main or refs/heads/* (or **); short names and refspecs are not allowed")
				}
			case "paths":
				clean := path.Clean(pattern)
				if path.IsAbs(pattern) || clean == ".." || strings.HasPrefix(clean, "../") {
					add(key, "must be a repository-relative path glob without parent traversal")
				}
				if strings.HasPrefix(pattern, ":") {
					add(key, "must be a path glob, not a Git pathspec; pathspec magic is not allowed")
				}
			}
		}
	}
	for i, remote := range m.Remotes {
		if remote == "" || strings.ContainsRune(remote, 0) {
			add(fmt.Sprintf("%s.remotes[%d]", p, i), "remote name must be non-empty and NUL-free")
		}
	}
}
