# Action catalogue

Every registered Git action is listed below. Default effect and example scopes
are a discovery aid, not a guarantee for every flag/input combination. Normalize
the actual request before enforcing policy; see [normalization](normalization.md).
Routes omit the leading Git executable; stdin-selected operations share routes.

| Family | Action | Default effect | Example scopes | Example Git argv |
| --- | --- | --- | --- | --- |
| `archive` | `archive.create` | `read` | `objects` | `archive HEAD` |
| `archive` | `archive.list` | `read` | `objects` | `archive --list` |
| `archive` | `archive.output` | `write` | `file-output, objects` | `archive --output=out.tar HEAD` |
| `bundle` | `bundle.create` | `write` | `file-output, objects` | `bundle create data.bundle` |
| `bundle` | `bundle.list-heads` | `read` | `objects` | `bundle list-heads data.bundle` |
| `bundle` | `bundle.stdout` | `read` | `objects` | `bundle create - HEAD` |
| `bundle` | `bundle.unbundle` | `write` | `objects` | `bundle unbundle data.bundle` |
| `bundle` | `bundle.verify` | `read` | `objects` | `bundle verify data.bundle` |
| `config` | `config.add` | `write` | `config` | `config --add core.editor vi` |
| `config` | `config.edit` | `write` | `config` | `config edit` |
| `config` | `config.get` | `read` | `config` | `config get core.editor` |
| `config` | `config.list` | `read` | `config` | `config list` |
| `config` | `config.remove-section` | `write` | `config` | `config remove-section test.old` |
| `config` | `config.rename-section` | `write` | `config` | `config rename-section test.old test.new` |
| `config` | `config.set` | `write` | `config` | `config set core.editor vi` |
| `config` | `config.unset` | `write` | `config` | `config unset core.editor` |
| `git.annotate` | `git.annotate.output` | `write` | `file-output, objects, worktree` | `annotate --output=report` |
| `git.annotate` | `git.annotate.read` | `read` | `objects, worktree` | `annotate` |
| `git.blame` | `git.blame.output` | `write` | `file-output, objects, worktree` | `blame --output=report` |
| `git.blame` | `git.blame.read` | `read` | `objects, worktree` | `blame` |
| `git.cat-file` | `git.cat-file.protocol` | `unknown` | `objects` | `cat-file --batch` |
| `git.cat-file` | `git.cat-file.read` | `read` | `objects` | `cat-file` |
| `git.check-attr` | `git.check-attr.read` | `read` | `index, worktree` | `check-attr` |
| `git.check-ignore` | `git.check-ignore.read` | `read` | `index, worktree` | `check-ignore` |
| `git.check-mailmap` | `git.check-mailmap.read` | `read` | `—` | `check-mailmap` |
| `git.check-ref-format` | `git.check-ref-format.read` | `read` | `—` | `check-ref-format` |
| `git.cherry` | `git.cherry.read` | `read` | `objects` | `cherry` |
| `git.column` | `git.column.read` | `read` | `—` | `column` |
| `git.describe` | `git.describe.read` | `read` | `objects` | `describe` |
| `git.diff` | `git.diff.output` | `write` | `file-output, index, objects, worktree` | `diff --output=report` |
| `git.diff` | `git.diff.read` | `read` | `index, objects, worktree` | `diff` |
| `git.diff-files` | `git.diff-files.output` | `write` | `file-output, index, objects, worktree` | `diff-files --output=report` |
| `git.diff-files` | `git.diff-files.read` | `read` | `index, objects, worktree` | `diff-files` |
| `git.diff-index` | `git.diff-index.output` | `write` | `file-output, index, objects, worktree` | `diff-index --output=report` |
| `git.diff-index` | `git.diff-index.read` | `read` | `index, objects, worktree` | `diff-index` |
| `git.diff-pairs` | `git.diff-pairs.output` | `write` | `file-output, objects` | `diff-pairs --output=report` |
| `git.diff-pairs` | `git.diff-pairs.read` | `read` | `objects` | `diff-pairs` |
| `git.diff-tree` | `git.diff-tree.output` | `write` | `file-output, index, objects` | `diff-tree --output=report` |
| `git.diff-tree` | `git.diff-tree.read` | `read` | `index, objects` | `diff-tree` |
| `git.fmt-merge-msg` | `git.fmt-merge-msg.read` | `read` | `—` | `fmt-merge-msg` |
| `git.format-patch` | `git.format-patch.output` | `write` | `file-output, objects` | `format-patch --output=report` |
| `git.format-patch` | `git.format-patch.read` | `read` | `objects` | `format-patch --stdout` |
| `git.format-rev` | `git.format-rev.output` | `write` | `file-output, objects` | `format-rev --output=report` |
| `git.format-rev` | `git.format-rev.read` | `read` | `objects` | `format-rev` |
| `git.get-tar-commit-id` | `git.get-tar-commit-id.read` | `read` | `—` | `get-tar-commit-id` |
| `git.grep` | `git.grep.read` | `read` | `index, objects, worktree` | `grep` |
| `git.help` | `git.help.read` | `read` | `—` | `--help; help` |
| `git.info` | `git.info.read` | `read` | `—` | `--exec-path; --html-path; --info-path; --list-cmds=main; --man-path` |
| `git.last-modified` | `git.last-modified.output` | `write` | `file-output, objects` | `last-modified --output=report` |
| `git.last-modified` | `git.last-modified.read` | `read` | `objects` | `last-modified` |
| `git.log` | `git.log.output` | `write` | `file-output, objects` | `log --output=report` |
| `git.log` | `git.log.read` | `read` | `objects` | `log` |
| `git.ls-files` | `git.ls-files.read` | `read` | `index, worktree` | `ls-files` |
| `git.ls-tree` | `git.ls-tree.read` | `read` | `objects` | `ls-tree` |
| `git.merge-base` | `git.merge-base.read` | `read` | `objects` | `merge-base` |
| `git.name-rev` | `git.name-rev.read` | `read` | `objects` | `name-rev` |
| `git.pack-redundant` | `git.pack-redundant.read` | `read` | `objects` | `pack-redundant` |
| `git.patch-id` | `git.patch-id.read` | `read` | `—` | `patch-id` |
| `git.pickaxe` | `git.pickaxe.output` | `write` | `file-output, objects, worktree` | `pickaxe --output=report` |
| `git.pickaxe` | `git.pickaxe.read` | `read` | `objects, worktree` | `pickaxe` |
| `git.range-diff` | `git.range-diff.output` | `write` | `file-output, objects` | `range-diff --output=report` |
| `git.range-diff` | `git.range-diff.read` | `read` | `objects` | `range-diff` |
| `git.repo` | `git.repo.read` | `read` | `config, local-ref, objects` | `repo` |
| `git.rev-list` | `git.rev-list.output` | `write` | `file-output, objects` | `rev-list --output=report` |
| `git.rev-list` | `git.rev-list.read` | `read` | `objects` | `rev-list` |
| `git.rev-parse` | `git.rev-parse.output` | `write` | `file-output, objects` | `rev-parse --output=report` |
| `git.rev-parse` | `git.rev-parse.read` | `read` | `objects` | `rev-parse` |
| `git.shortlog` | `git.shortlog.output` | `write` | `file-output, objects` | `shortlog --output=report` |
| `git.shortlog` | `git.shortlog.read` | `read` | `objects` | `shortlog` |
| `git.show` | `git.show.output` | `write` | `file-output, objects` | `show --output=report` |
| `git.show` | `git.show.read` | `read` | `objects` | `show` |
| `git.show-branch` | `git.show-branch.read` | `read` | `local-ref` | `show-branch` |
| `git.show-index` | `git.show-index.read` | `read` | `—` | `show-index` |
| `git.stripspace` | `git.stripspace.read` | `read` | `—` | `stripspace` |
| `git.url-parse` | `git.url-parse.read` | `read` | `—` | `url-parse` |
| `git.var` | `git.var.read` | `read` | `—` | `var` |
| `git.verify-commit` | `git.verify-commit.read` | `read` | `objects` | `verify-commit` |
| `git.verify-tag` | `git.verify-tag.read` | `read` | `objects` | `verify-tag` |
| `git.version` | `git.version.read` | `read` | `—` | `--version; version` |
| `git.whatchanged` | `git.whatchanged.output` | `write` | `file-output, objects` | `whatchanged --output=report` |
| `git.whatchanged` | `git.whatchanged.read` | `read` | `objects` | `whatchanged` |
| `history.am` | `history.am.abort` | `write` | `index, local-ref, objects, worktree` | `am --abort` |
| `history.am` | `history.am.allow-empty` | `write` | `index, local-ref, objects, worktree` | `am --allow-empty` |
| `history.am` | `history.am.continue` | `write` | `index, local-ref, objects, worktree` | `am --continue` |
| `history.am` | `history.am.quit` | `write` | `worktree-admin` | `am --quit` |
| `history.am` | `history.am.retry` | `write` | `index, local-ref, objects, worktree` | `am --retry` |
| `history.am` | `history.am.show-current-patch` | `read` | `worktree-admin` | `am --show-current-patch` |
| `history.am` | `history.am.skip` | `write` | `index, local-ref, objects, worktree` | `am --skip` |
| `history.am` | `history.am.start` | `write` | `index, local-ref, objects, worktree` | `am HEAD` |
| `history.bisect` | `history.bisect.bad` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect bad` |
| `history.bisect` | `history.bisect.good` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect good` |
| `history.bisect` | `history.bisect.log` | `read` | `local-ref, worktree-admin` | `bisect log` |
| `history.bisect` | `history.bisect.mark` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect custom-term HEAD` |
| `history.bisect` | `history.bisect.next` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect next` |
| `history.bisect` | `history.bisect.replay` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect replay` |
| `history.bisect` | `history.bisect.reset` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect reset` |
| `history.bisect` | `history.bisect.run` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect run sh -c true` |
| `history.bisect` | `history.bisect.skip` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect skip` |
| `history.bisect` | `history.bisect.start` | `write` | `index, local-ref, worktree, worktree-admin` | `bisect start` |
| `history.bisect` | `history.bisect.terms` | `read` | `local-ref, worktree-admin` | `bisect terms` |
| `history.bisect` | `history.bisect.visualize` | `read` | `local-ref, worktree-admin` | `bisect visualize` |
| `history.cherry-pick` | `history.cherry-pick.abort` | `write` | `index, local-ref, objects, worktree` | `cherry-pick --abort` |
| `history.cherry-pick` | `history.cherry-pick.apply` | `write` | `index, objects, worktree` | `cherry-pick --no-commit HEAD` |
| `history.cherry-pick` | `history.cherry-pick.continue` | `write` | `index, local-ref, objects, worktree` | `cherry-pick --continue` |
| `history.cherry-pick` | `history.cherry-pick.quit` | `write` | `worktree-admin` | `cherry-pick --quit` |
| `history.cherry-pick` | `history.cherry-pick.skip` | `write` | `index, local-ref, objects, worktree` | `cherry-pick --skip` |
| `history.cherry-pick` | `history.cherry-pick.start` | `write` | `index, local-ref, objects, worktree` | `cherry-pick HEAD` |
| `history.commit` | `history.commit.amend` | `write` | `index, local-ref, objects` | `commit --amend -m message` |
| `history.commit` | `history.commit.create` | `write` | `index, local-ref, objects` | `commit -m message` |
| `history.commit` | `history.commit.preview` | `read` | `index, objects, worktree` | `commit --dry-run` |
| `history.merge` | `history.merge.abort` | `write` | `index, local-ref, objects, worktree` | `merge --abort` |
| `history.merge` | `history.merge.continue` | `write` | `index, local-ref, objects, worktree` | `merge --continue` |
| `history.merge` | `history.merge.quit` | `write` | `stash, worktree-admin` | `merge --quit` |
| `history.merge` | `history.merge.squash` | `write` | `index, local-ref, objects, worktree` | `merge --squash` |
| `history.merge` | `history.merge.start` | `write` | `index, local-ref, objects, worktree` | `merge HEAD` |
| `history.rebase` | `history.rebase.abort` | `write` | `index, local-ref, objects, worktree` | `rebase --abort` |
| `history.rebase` | `history.rebase.continue` | `write` | `index, local-ref, objects, worktree` | `rebase --continue` |
| `history.rebase` | `history.rebase.edit-todo` | `write` | `worktree-admin` | `rebase --edit-todo` |
| `history.rebase` | `history.rebase.quit` | `write` | `stash, worktree-admin` | `rebase --quit` |
| `history.rebase` | `history.rebase.show-current-patch` | `read` | `objects, worktree-admin` | `rebase --show-current-patch` |
| `history.rebase` | `history.rebase.skip` | `write` | `index, local-ref, objects, worktree` | `rebase --skip` |
| `history.rebase` | `history.rebase.start` | `write` | `index, local-ref, objects, worktree` | `rebase HEAD` |
| `history.revert` | `history.revert.abort` | `write` | `index, local-ref, objects, worktree` | `revert --abort` |
| `history.revert` | `history.revert.apply` | `write` | `index, objects, worktree` | `revert --no-commit HEAD` |
| `history.revert` | `history.revert.continue` | `write` | `index, local-ref, objects, worktree` | `revert --continue` |
| `history.revert` | `history.revert.quit` | `write` | `worktree-admin` | `revert --quit` |
| `history.revert` | `history.revert.skip` | `write` | `index, local-ref, objects, worktree` | `revert --skip` |
| `history.revert` | `history.revert.start` | `write` | `index, local-ref, objects, worktree` | `revert HEAD` |
| `hook` | `hook.list` | `read` | `hooks` | `hook list` |
| `hook` | `hook.run` | `unknown` | `config, file-output, hooks, index, local-ref, objects, reflog, remote-config, remote-ref, stash, submodule, worktree, worktree-admin` | `hook run pre-commit` |
| `index` | `index.read-tree` | `write` | `index` | `read-tree HEAD` |
| `index` | `index.read-tree.preview` | `read` | `index` | `read-tree -n HEAD` |
| `index` | `index.update` | `write` | `index` | `update-index --refresh` |
| `index.add` | `index.add.preview` | `read` | `index` | `add -n file` |
| `index.add` | `index.add.refresh` | `write` | `index` | `add --refresh file` |
| `index.add` | `index.add.update` | `write` | `index, objects` | `add file` |
| `index.rm` | `index.rm.preview` | `read` | `index, submodule, worktree` | `rm -n file` |
| `index.rm` | `index.rm.remove` | `write` | `index, submodule, worktree` | `rm file` |
| `maintenance` | `maintenance.is-needed` | `read` | `local-ref, objects, reflog, worktree-admin` | `maintenance is-needed` |
| `maintenance` | `maintenance.register` | `write` | `config` | `maintenance register` |
| `maintenance` | `maintenance.run` | `write` | `local-ref, objects, reflog, worktree-admin` | `maintenance run` |
| `maintenance` | `maintenance.start` | `write` | `config, file-output` | `maintenance start` |
| `maintenance` | `maintenance.stop` | `write` | `config, file-output` | `maintenance stop` |
| `maintenance` | `maintenance.unregister` | `write` | `config` | `maintenance unregister` |
| `notes` | `notes.add` | `write` | `local-ref, objects` | `notes add` |
| `notes` | `notes.append` | `write` | `local-ref, objects` | `notes append` |
| `notes` | `notes.copy` | `write` | `local-ref, objects` | `notes copy` |
| `notes` | `notes.edit` | `write` | `local-ref, objects` | `notes edit` |
| `notes` | `notes.get-ref` | `read` | `local-ref, objects` | `notes get-ref` |
| `notes` | `notes.list` | `read` | `local-ref, objects` | `notes list` |
| `notes` | `notes.merge` | `write` | `local-ref, objects, worktree-admin` | `notes merge` |
| `notes` | `notes.preview` | `read` | `local-ref, objects` | `notes prune -n` |
| `notes` | `notes.prune` | `write` | `local-ref, objects` | `notes prune` |
| `notes` | `notes.remove` | `write` | `local-ref, objects` | `notes remove` |
| `notes` | `notes.show` | `read` | `local-ref, objects` | `notes show` |
| `objects` | `objects.commit-graph.verify` | `read` | `objects` | `commit-graph verify` |
| `objects` | `objects.commit-graph.write` | `write` | `objects` | `commit-graph write` |
| `objects` | `objects.commit-tree` | `write` | `objects` | `commit-tree` |
| `objects` | `objects.count-objects` | `read` | `objects` | `count-objects` |
| `objects` | `objects.fast-export` | `read` | `objects` | `fast-export` |
| `objects` | `objects.fast-export.output` | `write` | `file-output, objects` | `fast-export --export-marks=marks HEAD` |
| `objects` | `objects.fast-import` | `unknown` | `local-ref, objects, reflog` | `fast-import` |
| `objects` | `objects.fsck` | `read` | `objects` | `fsck` |
| `objects` | `objects.fsck.lost-found` | `write` | `file-output, objects` | `fsck --lost-found` |
| `objects` | `objects.gc` | `write` | `local-ref, objects, reflog, worktree-admin` | `gc` |
| `objects` | `objects.hash.read` | `read` | `objects` | `hash-object --no-filters file` |
| `objects` | `objects.hash.write` | `write` | `objects` | `hash-object -w file` |
| `objects` | `objects.index-pack` | `write` | `objects` | `index-pack` |
| `objects` | `objects.index-pack.verify` | `read` | `objects` | `index-pack --verify data.pack` |
| `objects` | `objects.mktag` | `write` | `objects` | `mktag` |
| `objects` | `objects.mktree` | `write` | `objects` | `mktree` |
| `objects` | `objects.multi-pack-index.compact` | `write` | `objects` | `multi-pack-index compact` |
| `objects` | `objects.multi-pack-index.expire` | `write` | `objects` | `multi-pack-index expire` |
| `objects` | `objects.multi-pack-index.repack` | `write` | `objects` | `multi-pack-index repack` |
| `objects` | `objects.multi-pack-index.verify` | `read` | `objects` | `multi-pack-index verify` |
| `objects` | `objects.multi-pack-index.write` | `write` | `objects` | `multi-pack-index write` |
| `objects` | `objects.pack-objects` | `write` | `file-output, objects` | `pack-objects` |
| `objects` | `objects.pack-objects.read` | `read` | `objects` | `pack-objects --stdout` |
| `objects` | `objects.prune` | `write` | `objects` | `prune` |
| `objects` | `objects.prune-packed` | `write` | `objects` | `prune-packed` |
| `objects` | `objects.prune-packed.preview` | `read` | `objects` | `prune-packed --dry-run` |
| `objects` | `objects.prune.preview` | `read` | `objects` | `prune --dry-run` |
| `objects` | `objects.repack` | `write` | `objects` | `repack` |
| `objects` | `objects.repack.preview` | `read` | `objects` | `repack --dry-run` |
| `objects` | `objects.unpack-objects` | `write` | `objects` | `unpack-objects` |
| `objects` | `objects.unpack-objects.preview` | `read` | `objects` | `unpack-objects -n` |
| `objects` | `objects.verify-pack` | `read` | `objects` | `verify-pack` |
| `objects` | `objects.write-tree` | `write` | `index, objects` | `write-tree` |
| `ref` | `ref.create` | `write` | `config, local-ref, reflog` | `branch topic; branch -c old new; tag v1; checkout -b topic; switch -c topic; worktree add -b topic ../topic` |
| `ref` | `ref.delete` | `write` | `config, local-ref, reflog` | `update-ref -d refs/heads/topic; branch -D topic; tag -d v1; symbolic-ref -d refs/heads/alias` |
| `ref` | `ref.list` | `read` | `local-ref` | `for-each-ref; show-ref` |
| `ref` | `ref.pack` | `write` | `local-ref` | `pack-refs --all` |
| `ref` | `ref.rename` | `write` | `config, local-ref, reflog` | `branch -m old new` |
| `ref` | `ref.update` | `write` | `config, local-ref, reflog` | `update-ref refs/heads/topic HEAD; branch -f topic; tag -f v1; symbolic-ref HEAD refs/heads/main; checkout -B topic; switch -C topic; worktree add -B topic ../topic` |
| `ref` | `ref.verify` | `read` | `local-ref` | `update-ref --stdin` |
| `ref.branch` | `ref.branch.description` | `write` | `config` | `branch --edit-description topic` |
| `ref.branch` | `ref.branch.list` | `read` | `local-ref` | `branch --list` |
| `ref.branch` | `ref.branch.upstream` | `write` | `config` | `branch --set-upstream-to=origin/main topic` |
| `ref.reflog` | `ref.reflog.delete` | `write` | `reflog` | `reflog delete` |
| `ref.reflog` | `ref.reflog.drop` | `write` | `reflog` | `reflog drop` |
| `ref.reflog` | `ref.reflog.exists` | `read` | `reflog` | `reflog exists` |
| `ref.reflog` | `ref.reflog.expire` | `write` | `reflog` | `reflog expire` |
| `ref.reflog` | `ref.reflog.list` | `read` | `reflog` | `reflog list` |
| `ref.reflog` | `ref.reflog.show` | `read` | `reflog` | `reflog show` |
| `ref.reflog` | `ref.reflog.write` | `write` | `reflog` | `reflog write` |
| `ref.replace` | `ref.replace.convert` | `write` | `file-output, local-ref, objects` | `replace --convert-graft-file` |
| `ref.replace` | `ref.replace.create` | `write` | `local-ref` | `replace HEAD HEAD~1` |
| `ref.replace` | `ref.replace.delete` | `write` | `local-ref` | `replace --delete HEAD` |
| `ref.replace` | `ref.replace.edit` | `write` | `local-ref, objects` | `replace --edit HEAD` |
| `ref.replace` | `ref.replace.graft` | `write` | `local-ref, objects` | `replace --graft HEAD` |
| `ref.replace` | `ref.replace.list` | `read` | `local-ref` | `replace --list` |
| `ref.symbolic` | `ref.symbolic.read` | `read` | `local-ref` | `symbolic-ref HEAD` |
| `ref.symbolic` | `ref.symbolic.verify` | `read` | `local-ref` | `update-ref --stdin --no-deref` |
| `ref.tag` | `ref.tag.list` | `read` | `local-ref` | `tag --list` |
| `ref.tag` | `ref.tag.verify` | `read` | `local-ref` | `tag --verify v1` |
| `ref.transaction` | `ref.transaction.abort` | `read` | `local-ref` | `update-ref --stdin` |
| `ref.transaction` | `ref.transaction.commit` | `write` | `local-ref` | `update-ref --stdin` |
| `ref.transaction` | `ref.transaction.option` | `read` | `local-ref` | `update-ref --stdin` |
| `ref.transaction` | `ref.transaction.prepare` | `write` | `local-ref` | `update-ref --stdin` |
| `ref.transaction` | `ref.transaction.start` | `read` | `local-ref` | `update-ref --stdin` |
| `remote` | `remote.clone` | `write` | `config, index, local-ref, objects, worktree` | `clone ../source destination` |
| `remote` | `remote.fetch` | `write` | `local-ref, objects` | `fetch origin; remote update` |
| `remote` | `remote.get-url` | `read` | `remote-config` | `ls-remote --get-url origin; remote get-url origin` |
| `remote` | `remote.list` | `read` | `remote-config` | `remote` |
| `remote` | `remote.ls-remote` | `read` | `remote-ref` | `ls-remote origin` |
| `remote` | `remote.prune` | `write` | `local-ref, reflog` | `remote prune origin` |
| `remote` | `remote.pull` | `write` | `index, local-ref, objects, reflog, worktree` | `pull origin main` |
| `remote` | `remote.show` | `read` | `remote-config` | `remote show -n origin` |
| `remote.config` | `remote.config.add` | `write` | `config, remote-config` | `remote add origin ../source` |
| `remote.config` | `remote.config.remove` | `write` | `config, local-ref, reflog, remote-config` | `remote remove origin` |
| `remote.config` | `remote.config.rename` | `write` | `config, local-ref, reflog, remote-config` | `remote rename origin backup` |
| `remote.config` | `remote.config.set-branches` | `write` | `config, remote-config` | `remote set-branches origin main` |
| `remote.config` | `remote.config.set-url` | `write` | `config, remote-config` | `remote set-url origin ../source` |
| `remote.pack` | `remote.pack.advertise` | `read` | `index, local-ref, objects, reflog, worktree` | `receive-pack --advertise-refs repo.git` |
| `remote.pack` | `remote.pack.fetch` | `write` | `objects` | `fetch-pack ../source refs/heads/main` |
| `remote.pack` | `remote.pack.receive` | `write` | `index, local-ref, objects, reflog, worktree` | `receive-pack repo.git` |
| `remote.pack` | `remote.pack.upload` | `read` | `objects` | `upload-pack repo.git` |
| `remote.push` | `remote.push.delete` | `write` | `remote-ref` | `push --delete origin refs/heads/topic` |
| `remote.push` | `remote.push.matching` | `write` | `remote-ref` | `push origin :` |
| `remote.push` | `remote.push.mirror` | `write` | `remote-ref` | `push --mirror origin` |
| `remote.push` | `remote.push.tags` | `write` | `remote-ref` | `push --tags origin` |
| `remote.push` | `remote.push.update` | `write` | `remote-ref` | `push origin HEAD:refs/heads/main; send-pack origin HEAD:refs/heads/main` |
| `repository` | `repository.init` | `write` | `config, local-ref, objects, worktree-admin` | `init repository` |
| `stash` | `stash.apply` | `write` | `index, objects, worktree` | `stash apply` |
| `stash` | `stash.branch` | `write` | `config, index, local-ref, objects, stash, worktree` | `stash branch` |
| `stash` | `stash.clear` | `write` | `stash` | `stash clear` |
| `stash` | `stash.create` | `write` | `index, objects` | `stash create` |
| `stash` | `stash.drop` | `write` | `stash` | `stash drop` |
| `stash` | `stash.export` | `write` | `objects` | `stash export` |
| `stash` | `stash.import` | `write` | `stash` | `stash import` |
| `stash` | `stash.list` | `read` | `stash` | `stash list` |
| `stash` | `stash.pop` | `write` | `index, objects, stash, worktree` | `stash pop` |
| `stash` | `stash.push` | `write` | `index, objects, stash, worktree` | `stash push` |
| `stash` | `stash.show` | `read` | `stash` | `stash show` |
| `stash` | `stash.store` | `write` | `stash` | `stash store` |
| `status` | `status.show` | `read` | `index, worktree` | `status` |
| `submodule` | `submodule.absorbgitdirs` | `write` | `config, submodule, worktree, worktree-admin` | `submodule absorbgitdirs` |
| `submodule` | `submodule.add` | `write` | `config, index, local-ref, objects, submodule, worktree` | `submodule add` |
| `submodule` | `submodule.clone` | `write` | `config, index, local-ref, objects, submodule, worktree` | `submodule--helper clone ../source lib` |
| `submodule` | `submodule.deinit` | `write` | `config, submodule, worktree, worktree-admin` | `submodule deinit` |
| `submodule` | `submodule.foreach` | `unknown` | `config, file-output, hooks, index, local-ref, objects, reflog, remote-config, remote-ref, stash, submodule, worktree, worktree-admin` | `submodule foreach` |
| `submodule` | `submodule.get-default-remote` | `read` | `submodule` | `submodule--helper get-default-remote` |
| `submodule` | `submodule.gitdir` | `read` | `submodule` | `submodule--helper gitdir lib` |
| `submodule` | `submodule.init` | `write` | `config, submodule` | `submodule init` |
| `submodule` | `submodule.set-branch` | `write` | `config, submodule, worktree` | `submodule set-branch` |
| `submodule` | `submodule.set-url` | `write` | `config, remote-config, submodule, worktree` | `submodule set-url` |
| `submodule` | `submodule.status` | `read` | `submodule` | `submodule status` |
| `submodule` | `submodule.summary` | `read` | `submodule` | `submodule summary` |
| `submodule` | `submodule.sync` | `write` | `config, remote-config, submodule` | `submodule sync` |
| `submodule` | `submodule.update` | `write` | `index, local-ref, objects, submodule, worktree` | `submodule update` |
| `unknown` | `unknown` | `unknown` | `—` | `custom-command` |
| `worktree` | `worktree.clean` | `write` | `worktree` | `clean -f` |
| `worktree.admin` | `worktree.admin.add` | `write` | `config, index, local-ref, worktree, worktree-admin` | `worktree add ../topic` |
| `worktree.admin` | `worktree.admin.list` | `read` | `worktree-admin` | `worktree list` |
| `worktree.admin` | `worktree.admin.lock` | `write` | `worktree-admin` | `worktree lock ../topic` |
| `worktree.admin` | `worktree.admin.move` | `write` | `worktree, worktree-admin` | `worktree move ../topic ../renamed` |
| `worktree.admin` | `worktree.admin.prune` | `write` | `worktree-admin` | `worktree prune` |
| `worktree.admin` | `worktree.admin.prune.preview` | `read` | `worktree-admin` | `worktree prune -n` |
| `worktree.admin` | `worktree.admin.remove` | `write` | `worktree, worktree-admin` | `worktree remove ../topic` |
| `worktree.admin` | `worktree.admin.repair` | `write` | `worktree-admin` | `worktree repair ../topic` |
| `worktree.admin` | `worktree.admin.unlock` | `write` | `worktree-admin` | `worktree unlock ../topic` |
| `worktree.apply` | `worktree.apply.check` | `read` | `worktree` | `apply --check patch` |
| `worktree.apply` | `worktree.apply.patch` | `write` | `worktree` | `apply patch` |
| `worktree.apply` | `worktree.apply.stat` | `read` | `worktree` | `apply --stat patch` |
| `worktree.checkout` | `worktree.checkout.create` | `write` | `config, index, local-ref, worktree` | `checkout -b topic` |
| `worktree.checkout` | `worktree.checkout.detach` | `write` | `index, local-ref, worktree` | `checkout --detach HEAD` |
| `worktree.checkout` | `worktree.checkout.orphan` | `write` | `config, index, local-ref, worktree` | `checkout --orphan topic` |
| `worktree.checkout` | `worktree.checkout.paths` | `write` | `worktree` | `checkout -- file` |
| `worktree.checkout` | `worktree.checkout.reset` | `write` | `config, index, local-ref, worktree` | `checkout -B topic` |
| `worktree.checkout` | `worktree.checkout.switch` | `write` | `config, index, local-ref, worktree` | `checkout main` |
| `worktree.clean` | `worktree.clean.preview` | `read` | `worktree` | `clean -n` |
| `worktree.mv` | `worktree.mv.move` | `write` | `config, index, submodule, worktree` | `mv old new` |
| `worktree.mv` | `worktree.mv.preview` | `read` | `index, worktree` | `mv -n old new` |
| `worktree.reset` | `worktree.reset.hard` | `write` | `index, local-ref, worktree` | `reset --hard` |
| `worktree.reset` | `worktree.reset.keep` | `write` | `index, local-ref, worktree` | `reset --keep` |
| `worktree.reset` | `worktree.reset.merge` | `write` | `index, local-ref, worktree` | `reset --merge` |
| `worktree.reset` | `worktree.reset.mixed` | `write` | `index, local-ref` | `reset --mixed` |
| `worktree.reset` | `worktree.reset.patch` | `write` | `index` | `reset --patch` |
| `worktree.reset` | `worktree.reset.paths` | `write` | `index` | `reset -- file` |
| `worktree.reset` | `worktree.reset.soft` | `write` | `local-ref` | `reset --soft` |
| `worktree.restore` | `worktree.restore.paths` | `write` | `worktree` | `restore -- file` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.add` | `write` | `config, index, worktree` | `sparse-checkout add` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.check-rules` | `read` | `config, worktree` | `sparse-checkout check-rules` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.clean` | `write` | `worktree` | `sparse-checkout clean` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.disable` | `write` | `config, index, worktree` | `sparse-checkout disable` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.init` | `write` | `config, index, worktree` | `sparse-checkout init` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.list` | `read` | `config, worktree` | `sparse-checkout list` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.preview` | `read` | `worktree` | `sparse-checkout clean -n` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.reapply` | `write` | `config, index, worktree` | `sparse-checkout reapply` |
| `worktree.sparse-checkout` | `worktree.sparse-checkout.set` | `write` | `config, index, worktree` | `sparse-checkout set` |
| `worktree.switch` | `worktree.switch.create` | `write` | `config, index, local-ref, worktree` | `switch -c topic` |
| `worktree.switch` | `worktree.switch.detach` | `write` | `index, local-ref, worktree` | `switch --detach HEAD` |
| `worktree.switch` | `worktree.switch.orphan` | `write` | `config, index, local-ref, worktree` | `switch --orphan topic` |
| `worktree.switch` | `worktree.switch.reset` | `write` | `config, index, local-ref, worktree` | `switch -C topic` |
| `worktree.switch` | `worktree.switch.switch` | `write` | `config, index, local-ref, worktree` | `switch main` |
