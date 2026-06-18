# Git Hooks

Git has a global hook mechanism: `core.hooksPath`. When `memoryd init` runs, it writes managed hooks under the data root and sets global `core.hooksPath` to that directory if no global hook path is already configured.

The hooks enqueue commit events and return quickly. They do not run summarization or retrieval work inside git hooks.

## Default Install

Run:

```sh
memoryd init
```

The managed hook directory defaults to:

```text
~/.local/share/agent-memoryd/git-hooks
```

`init` installs `post-commit`, `post-merge`, and `post-rewrite` there. If global `core.hooksPath` is unset or already points at that directory, `init` configures Git to use the managed hooks. If another global hook path is already set, `init` does not overwrite it.

Check the current state:

```sh
memoryd status
git config --global --get core.hooksPath
```

## Included Hooks

`post-commit` enqueues the new `HEAD`.

`post-merge` enqueues the merge result.

`post-rewrite` enqueues rewritten commits, such as commits produced by rebase or amend flows.

## Existing Hooks

Setting a global `core.hooksPath` changes where Git looks for hooks. To avoid skipping existing repository-local hooks, the managed hooks first look for an executable hook with the same name in the repository's normal hooks directory and run it before enqueueing the memory event.

If you already have a global hooks directory, `memoryd init` leaves it configured. In that case you can either chain to `~/.local/share/agent-memoryd/git-hooks` from your existing global hooks or enqueue commits manually.

## Install In One Repository

Copy the hook templates into a repository's `.git/hooks` directory and make them executable:

```sh
cp contrib/git-hooks/post-commit /path/to/repo/.git/hooks/post-commit
cp contrib/git-hooks/post-merge /path/to/repo/.git/hooks/post-merge
cp contrib/git-hooks/post-rewrite /path/to/repo/.git/hooks/post-rewrite
chmod +x /path/to/repo/.git/hooks/post-commit
chmod +x /path/to/repo/.git/hooks/post-merge
chmod +x /path/to/repo/.git/hooks/post-rewrite
```

The hooks expect `memoryd` to be available on `PATH`.

## Manual Enqueue

You can enqueue a commit directly:

```sh
memoryd enqueue-git \
  --repo "$(git rev-parse --show-toplevel)" \
  --sha "$(git rev-parse HEAD)"
```

The daemon reads queued events, runs `git show`, and passes that output to the configured summarizer. Stored memories are distilled notes with `repo@sha` source references, not raw hook output.

## Scope

`memoryd uninstall --yes` removes resources that `memoryd init` tracks. If global `core.hooksPath` points at the managed hook directory, uninstall unsets it. It does not edit repositories where you manually installed hook files, and it does not modify a different global hooks directory that you configured yourself.
