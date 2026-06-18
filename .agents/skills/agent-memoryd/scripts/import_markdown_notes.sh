#!/usr/bin/env bash
# Bulk-import a tree of markdown/text notes into memoryd as kind=note
# memories, one memory per file. Idempotent: stable ids derived from the file
# path mean re-running upserts instead of duplicating.
#
# Why this exists alongside `memoryd init --import`:
#   - `init --import` only runs during init and does NOT skip arbitrary dirs
#     (only .git/node_modules/vendor/.cache/.DS_Store). This script runs against
#     an already-initialized store, lets you exclude any dir, and lets you map a
#     project per file.
#
# Critical gotcha this script handles:
#   `memoryd add` takes the body as a trailing positional arg. Markdown
#   files often start with "- " (a bullet); cobra then parses the body as a flag
#   ("unknown shorthand flag: ' '"). Fix: pass the body after a `--` sentinel and
#   use `--flag=value` form. Both are applied below.
#
# Usage:
#   import_markdown_notes.sh <notes-dir> [--project NAME] [--id-prefix PFX]
#                                        [--exclude DIR]... [--dry-run]
#
# Examples:
#   import_markdown_notes.sh ~/notes/agent --project notes --exclude git-repair
#   import_markdown_notes.sh ./docs --project myproj --dry-run
#
set -u

BIN="${AGENT_MEMORYD_BIN:-memoryd}"
NOTES_DIR=""
PROJECT=""
ID_PREFIX="note"
DRY_RUN=0
EXCLUDES=()

while [ $# -gt 0 ]; do
  case "$1" in
    --project)   PROJECT="$2"; shift 2 ;;
    --id-prefix) ID_PREFIX="$2"; shift 2 ;;
    --exclude)   EXCLUDES+=("$2"); shift 2 ;;
    --dry-run)   DRY_RUN=1; shift ;;
    -h|--help)   sed -n '2,30p' "$0"; exit 0 ;;
    -*)          echo "unknown flag: $1" >&2; exit 2 ;;
    *)           NOTES_DIR="$1"; shift ;;
  esac
done

[ -n "$NOTES_DIR" ] || { echo "error: notes-dir is required" >&2; exit 2; }
[ -d "$NOTES_DIR" ] || { echo "error: not a directory: $NOTES_DIR" >&2; exit 2; }
command -v "$BIN" >/dev/null 2>&1 || { echo "error: $BIN not on PATH" >&2; exit 2; }

# Build a find prune expression for excluded directory names.
prune=()
for d in "${EXCLUDES[@]:-}"; do
  [ -n "$d" ] || continue
  prune+=( -name "$d" -prune -o )
done

ok=0; skip=0; fail=0
# NUL-delimited to survive spaces in paths.
while IFS= read -r -d '' f; do
  # Skip files with no non-whitespace content (empty body -> add errors).
  if ! grep -qE '\S' "$f" 2>/dev/null; then
    skip=$((skip+1)); echo "SKIP(empty) $f"; continue
  fi
  rel="${f#"$NOTES_DIR"/}"
  stem="$(printf '%s' "$rel" | sed -E 's/\.(md|markdown|txt)$//; s#[/ ]#-#g')"
  id="${ID_PREFIX}:${stem}"
  # Summary: first markdown heading, else first non-blank line, capped.
  title="$(grep -m1 -E '^#{1,6} ' "$f" | sed -E 's/^#+[[:space:]]*//')"
  [ -n "$title" ] || title="$(grep -m1 -E '\S' "$f" | sed -E 's/^[[:space:]]*//')"
  title="$(printf '%s' "$title" | cut -c1-160)"

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "DRY  [${PROJECT:-<none>}] $id  <- $rel"
    ok=$((ok+1)); continue
  fi
  # `--flag=value` plus `--` sentinel so bullet-leading bodies are not parsed as flags.
  if "$BIN" add --id="$id" --kind=note --project="$PROJECT" --source="$f" \
       --summary="$title" -- "$(cat "$f")" >/dev/null 2>/tmp/import-note-err; then
    ok=$((ok+1)); echo "OK   [${PROJECT:-<none>}] $id"
  else
    fail=$((fail+1)); echo "FAIL $id :: $(head -1 /tmp/import-note-err)"
  fi
done < <(
  find "$NOTES_DIR" \( "${prune[@]}" -false \) -o \
    -type f \( -name '*.md' -o -name '*.markdown' -o -name '*.txt' \) -print0
)

echo "================================"
echo "ok=$ok skip=$skip fail=$fail"
[ "$DRY_RUN" -eq 0 ] && [ "$fail" -eq 0 ] && echo "Tip: run 'memoryd reindex' to refresh the retrieval index."
