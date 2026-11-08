package resolve

import (
	"context"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
)

// Selection only: --auto eligibility still depends on repository state.
func (r *Resolver) maintenanceTasks(ctx context.Context, op *model.Operation, p gitnormalize.ParsedCommand) bool {
	if r.config == nil || len(p.Path) == 0 {
		return false
	}
	scheduled, schedule := false, 0
	priority := func(value string) int {
		switch strings.ToLower(value) {
		case "hourly":
			return 3
		case "daily":
			return 2
		case "weekly":
			return 1
		}
		return 0
	}
	if option, ok := p.Option("schedule"); ok {
		scheduled, schedule = true, priority(option.Value)
		if schedule == 0 {
			return false
		}
	}
	strategy := strings.ToLower(r.last("maintenance.strategy"))
	if strategy == "" && !scheduled {
		strategy = "geometric"
	}
	defaults := map[string]int{}
	switch strategy {
	case "":
	case "gc":
		defaults["gc"] = 2
	case "incremental":
		if scheduled {
			defaults = map[string]int{"commit-graph": 3, "prefetch": 3, "incremental-repack": 2, "loose-objects": 2, "pack-refs": 1}
		} else {
			defaults["gc"] = 2
		}
	case "geometric":
		defaults = map[string]int{"commit-graph": 3, "geometric-repack": 2, "pack-refs": 2, "reflog-expire": 1, "worktree-prune": 1, "rerere-gc": 1}
	default:
		return false
	}
	var tasks []string
	for _, task := range []string{"prefetch", "loose-objects", "incremental-repack", "geometric-repack", "gc", "commit-graph", "pack-refs", "reflog-expire", "worktree-prune", "rerere-gc"} {
		enabled, ok := r.boolConfig(ctx, "maintenance."+task+".enabled", defaults[task] != 0)
		if !ok {
			return false
		}
		if !enabled {
			continue
		}
		if scheduled {
			taskSchedule := defaults[task]
			if value := r.last("maintenance." + task + ".schedule"); value != "" {
				taskSchedule = priority(value)
				if taskSchedule == 0 {
					return false
				}
			}
			if taskSchedule < schedule {
				continue
			}
		}
		tasks = append(tasks, task)
		if task == "prefetch" {
			addCapability(op, model.CapabilityNetwork)
		}
	}
	op.Parameters["tasks"] = tasks
	return true
}
