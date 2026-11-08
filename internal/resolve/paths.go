package resolve

import (
	"context"
	"sort"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
)

func (r *Resolver) pathspecs(ctx context.Context, op *model.Operation, p gitnormalize.ParsedCommand) bool {
	if r.local.RepoRoot == "" {
		return false
	}
	selectors := stringList(op.Parameters["pathspecs"])
	for _, selector := range selectors {
		// Separate queries cannot implement subtraction from a shared set.
		if strings.HasPrefix(selector, ":!") || strings.HasPrefix(selector, ":^") || strings.HasPrefix(selector, ":(") && strings.Contains(selector, "exclude") {
			op.Target.Paths = &model.TargetSet{Kind: model.SetPattern, Values: append([]string(nil), selectors...)}
			return false
		}
	}
	kinds := []queryKind{filesQuery}
	complete := false
	switch {
	case strings.HasPrefix(op.Action, "index.add."):
		complete = true
		if !flag(*op, "update") {
			kinds = []queryKind{allFilesQuery}
			if p.Has("force") {
				kinds = append(kinds, ignoredFilesQuery)
			}
		}
	case strings.HasPrefix(op.Action, "index.rm."):
		complete = flag(*op, "cached")
	case op.Action == "worktree.restore.paths":
		complete = op.Parameters["source"] == "index" && !flag(*op, "merge")
	case strings.HasPrefix(op.Action, "worktree.clean"):
		kinds = []queryKind{untrackedFilesQuery}
		switch op.Parameters["ignored"] {
		case "include":
			kinds = append(kinds, ignoredFilesQuery)
		case "only":
			kinds = []queryKind{ignoredFilesQuery}
		}
	}
	queries := selectors
	if len(queries) == 0 {
		queries = []string{""}
		if strings.HasPrefix(op.Action, "index.add.") && (flag(*op, "all") || flag(*op, "update")) {
			queries = []string{r.local.RepoRoot} // add -A/-u without paths is repository-wide.
		}
	}
	var paths []string
	seen := make(map[string]bool)
	bytes := 0
	for _, kind := range kinds {
		for _, selector := range queries {
			data, err := r.run(ctx, query{kind: kind, value: selector})
			bytes += len(data)
			if err != nil || bytes > maxQueryBytes {
				return false
			}
			for _, path := range strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00") {
				if path != "" && !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
			}
		}
	}
	sort.Strings(paths)
	op.Target.Paths = targetSet(paths)
	op.Target.Paths.Complete = complete
	if !complete {
		for _, selector := range selectors {
			if strings.ContainsAny(selector, "*?[:") {
				op.Target.Paths.Kind = model.SetPattern
				op.Target.Paths.Values = append(op.Target.Paths.Values, selector)
			}
		}
		if len(op.Target.Paths.Values) == 0 {
			op.Target.Paths = model.UnknownSet()
		}
	}
	return complete
}
