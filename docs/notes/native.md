# Git semantic adapter notes

Classify mode/effect before policy. Reset, restore, checkout and switch can touch
different combinations of refs/index/worktree. Clean needs dry-run, interactive,
ignored-file and force-count semantics. Branch/tag/update-ref deletion should
share the corresponding ref-delete policy meaning without conflating their
specific actions. Push destination refs are not fetch's local tracking refs.

Read effect is not a side-effect-free guarantee: status/diff can refresh index
metadata, diff/textconv and signing can execute programs, output flags write
files. RetrySafe must stay false unless the whole request is proven repeatable.
Maintain source evidence, explicit unresolved fields and focused real-Git tests.
The complete parameters/actions reference is [normalization](../normalization.md).
