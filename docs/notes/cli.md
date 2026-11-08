# CLI implementation notes

Basename git selects wrapper mode; gthok selects management mode. Explicit
`gthok exec -- <git args>` uses the same wrapper pipeline and never strips a
leading git token. Management inspection uses `--` to separate its own options
from Git's options. Root Git flags must be parsed with Git's special semantics,
not by the management command framework.

Normalize emits JSON. Explain runs trusted transforms but not hooks/request;
--no-transforms avoids transform execution. Hooks test runs transforms/hooks
but not request/delegates. Doctor can emit JSON checks. Version emits text by
default; `gthok version --json` emits release metadata matching the version schema.
See [README](../../README.md#cli-reference) for the command list and
[exit status](../exit-status.md) for management versus wrapper status codes.
