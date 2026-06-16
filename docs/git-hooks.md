# Git Hooks

`agent-memoryd` includes hook templates under `contrib/git-hooks/`.

The templates enqueue commit events and return quickly. They do not run
summarization or retrieval work inside git hooks.

## Included Hooks

`post-commit` enqueues the new `HEAD`.

`post-merge` enqueues the merge result.

`post-rewrite` enqueues rewritten commits, such as commits produced by rebase or
amend flows.

## Install In One Repository

Copy the hook templates into a repository's `.git/hooks` directory and make
them executable:

```sh
cp contrib/git-hooks/post-commit /path/to/repo/.git/hooks/post-commit
cp contrib/git-hooks/post-merge /path/to/repo/.git/hooks/post-merge
cp contrib/git-hooks/post-rewrite /path/to/repo/.git/hooks/post-rewrite
chmod +x /path/to/repo/.git/hooks/post-commit
chmod +x /path/to/repo/.git/hooks/post-merge
chmod +x /path/to/repo/.git/hooks/post-rewrite
```

The hooks expect `agent-memoryd` to be available on `PATH`.

## Manual Enqueue

You can enqueue a commit directly:

```sh
agent-memoryd enqueue-git \
  --repo "$(git rev-parse --show-toplevel)" \
  --sha "$(git rev-parse HEAD)"
```

The daemon converts queued events into `git-summary` memories.

## Scope

`agent-memoryd uninstall --yes` removes resources that `agent-memoryd init`
tracks. It does not edit repositories where you manually installed hook files.
