# Local resolver notes

Use a fixed, bounded query builder against the discovered real Git, never a
user-supplied arbitrary query string. Supported reads include rev-parse metadata,
symbolic-ref, config get/list, ref enumeration, remote get-url, limited ls-files
and bounded cat-file type/OID reads. Disable pager/prompts and optional locks.

Never use ls-remote, push --dry-run, SSH, credential helpers or network to fill
remote state. Retain incomplete TargetSets and unresolved evidence instead.
Effective root options/environment must reach every local query. Offline
normalize --no-resolve must not launch any subprocess.
See [architecture](../architecture.md#local-resolution).
