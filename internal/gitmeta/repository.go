package gitmeta

import (
	"os"
	"path/filepath"
	"strings"
)

// Repository discovers the effective worktree, Git directory and shared directory.
// It uses local metadata only, including gitfiles and linked worktrees.
func Repository(cwd string, env map[string]string) (root, dir, common string, bare bool, err error) {
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return
	}
	dir, err = Directory(cwd, env)
	if err != nil {
		// Bare repositories have no .git entry.
		if _, headErr := os.Stat(filepath.Join(cwd, "HEAD")); headErr == nil {
			if info, objectsErr := os.Stat(filepath.Join(cwd, "objects")); objectsErr == nil && info.IsDir() {
				dir, err = cwd, nil
			}
		}
		if err != nil {
			return
		}
	}
	common = dir
	if data, readErr := ReadBounded(filepath.Join(dir, "commondir")); readErr == nil {
		common = Absolute(dir, strings.TrimSpace(string(data)))
	} else if !os.IsNotExist(readErr) {
		err = readErr
		return
	}
	entries, readErr := ReadConfig(filepath.Join(common, "config"), env["HOME"], 0)
	if readErr != nil && !os.IsNotExist(readErr) {
		err = readErr
		return
	}
	var worktree string
	for _, entry := range entries {
		if entry.Section == "core" && entry.Key == "bare" {
			bare = entry.Value == "true"
		}
		if entry.Section == "core" && entry.Key == "worktree" {
			worktree = entry.Value
		}
	}
	switch {
	case env["GIT_WORK_TREE"] != "":
		root, bare = Absolute(cwd, env["GIT_WORK_TREE"]), false
	case worktree != "":
		root, bare = Absolute(dir, worktree), false
	case bare:
	case filepath.Base(dir) == ".git":
		root = filepath.Dir(dir)
	default:
		if data, readErr := ReadBounded(filepath.Join(dir, "gitdir")); readErr == nil {
			root = filepath.Dir(strings.TrimSpace(string(data)))
		}
		if root == "" && env["GIT_DIR"] == "" {
			for candidate := cwd; ; candidate = filepath.Dir(candidate) {
				if _, statErr := os.Stat(filepath.Join(candidate, ".git")); statErr == nil {
					root = candidate
					break
				}
				if filepath.Dir(candidate) == candidate {
					break
				}
			}
		}
		if root == "" && env["GIT_DIR"] != "" {
			root = cwd
		}
	}
	return
}
