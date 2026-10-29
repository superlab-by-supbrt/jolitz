#!/bin/sh
# SessionStart hook: auto-pull latest jolitz changes
# with version awareness and pinning support. Also
# runs bin/jolitz-migrate after every pull, which
# migrates old user state and creates local
# directories new versions need. When the operator
# joined a team hub (~/.config/jolitz/hub.conf), the
# hook then pulls memory/ from the hub and opens a
# presence session; the Stop hook in settings.json
# closes it.

if [ -n "$CLAUDE_PROJECT_DIR" ] && [ -d "$CLAUDE_PROJECT_DIR" ]; then
  cd "$CLAUDE_PROJECT_DIR" || exit 0
fi

# Never hang the SessionStart hook on a credential
# prompt (HTTPS remote with expired token, etc.).
# Fail fast instead and let the user fix it.
export GIT_TERMINAL_PROMPT=0
GIT_ASKPASS=${GIT_ASKPASS:-true}
export GIT_ASKPASS

# Migration is implemented in bin/jolitz-migrate so
# both the Claude Code hook and bin/jolitz-update
# use the same logic.
run_migration() {
  [ -f bin/jolitz-migrate ] && sh bin/jolitz-migrate
}

# Team hub, optional: without hub.conf nothing runs.
# pull keeps a changed local file and lists the hub
# version as <file>.hub (exit 1); the session is
# opened anyway. An unreachable hub (exit 3) says so
# once and the session stays local.
hub_sync() {
  [ -r "$HOME/.config/jolitz/hub.conf" ] || return 0
  [ -f bin/jolitz-sync ] || return 0
  sh bin/jolitz-sync pull --quiet
  [ $? -eq 3 ] && return 0
  sh bin/jolitz-sync session start
  return 0
}

auto_update() {
  # Opt-out via environment variable.
  if [ "$JOLITZ_NO_UPDATE" = "1" ]; then
    echo "jolitz auto-update disabled (JOLITZ_NO_UPDATE=1)"
    return 0
  fi

  # Skip if not on the main branch (user pinned to a
  # version tag or is on a custom branch).
  BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null)
  if [ $? -ne 0 ]; then
    # Detached HEAD — likely pinned to a tag.
    TAG=$(git describe --tags --exact-match 2>/dev/null)
    if [ -n "$TAG" ]; then
      echo "jolitz pinned to $TAG — skipping auto-update"
    else
      echo "jolitz on detached HEAD — skipping auto-update"
    fi
    return 0
  fi

  if [ "$BRANCH" != "main" ]; then
    echo "jolitz on branch '$BRANCH' — skipping auto-update"
    return 0
  fi

  # Remember current version and commit before pulling.
  # The commit is what decides whether anything changed:
  # a pull that leaves VERSION alone still brings new
  # rules, hooks and scripts, and this hook runs
  # bin/jolitz-migrate from the new tree right away. The
  # operator must see that, every time.
  OLD_VERSION=""
  if [ -f VERSION ]; then
    OLD_VERSION=$(cat VERSION)
  fi
  OLD_HEAD=$(git rev-parse HEAD 2>/dev/null)

  # Pull latest changes. --ff-only: a diverged local
  # main should fail loudly here, never produce a
  # silent merge commit.
  OUTPUT=$(git pull --ff-only --quiet 2>&1)
  PULL_STATUS=$?

  if [ $PULL_STATUS -ne 0 ]; then
    echo "jolitz auto-update failed: $OUTPUT"
    echo "Likely causes: local changes, local commits on a"
    echo "diverged main, or no network. Run 'git status' to"
    echo "inspect."
    return 0
  fi

  # Migrate 1.x → 2.0 layout if needed. Idempotent
  # and silent when there's nothing to do.
  run_migration

  # Read new version after pulling.
  NEW_VERSION=""
  if [ -f VERSION ]; then
    NEW_VERSION=$(cat VERSION)
  fi
  NEW_HEAD=$(git rev-parse HEAD 2>/dev/null)

  # Report what happened.
  if [ -z "$OUTPUT" ] && [ "$OLD_HEAD" = "$NEW_HEAD" ] \
     && [ "$OLD_VERSION" = "$NEW_VERSION" ]; then
    # Nothing changed — stay quiet.
    return 0
  fi

  # New commits: name them, whatever VERSION says. The
  # pull was quiet, so this is the only trace.
  if [ -n "$OLD_HEAD" ] && [ -n "$NEW_HEAD" ] \
     && [ "$OLD_HEAD" != "$NEW_HEAD" ]; then
    echo "jolitz repo updated: $(git rev-parse --short "$OLD_HEAD")..$(git rev-parse --short "$NEW_HEAD")"
    git log --oneline "$OLD_HEAD..$NEW_HEAD" 2>/dev/null | sed 's/^/  /'
    echo "Pin a release to stop this: bin/jolitz-update --pin v$NEW_VERSION"
  fi

  if [ "$OLD_VERSION" != "$NEW_VERSION" ] \
     && [ -n "$OLD_VERSION" ] \
     && [ -n "$NEW_VERSION" ]; then
    echo "jolitz updated: $OLD_VERSION -> $NEW_VERSION"

    # Extract changelog section for the new version.
    if [ -f CHANGELOG.md ]; then
      # Print lines between "## $NEW_VERSION" and the
      # next "## " heading (or end of file). Compare
      # the whole second field so "## 2.8.0" does not
      # also match "## 2.8.0-rc1".
      awk -v ver="$NEW_VERSION" '
        /^## / { if (insec) exit; insec = ($2 == ver); next }
        insec && NF { print }
      ' CHANGELOG.md
    fi

    # Warn on major version change.
    OLD_MAJOR=$(echo "$OLD_VERSION" | cut -d. -f1)
    NEW_MAJOR=$(echo "$NEW_VERSION" | cut -d. -f1)
    if [ "$OLD_MAJOR" != "$NEW_MAJOR" ]; then
      echo ""
      echo "BREAKING CHANGES — read CHANGELOG.md"
    fi
  elif [ -n "$OUTPUT" ]; then
    echo "jolitz pull said: $OUTPUT"
  fi
}

# jq first: the guard reads every command through it, and
# without it the guard only scans the raw input and blocks far
# more than it should. Said once per session, at the top, with
# the command for this system; jolitz passes it on.
jq_check() {
  command -v jq >/dev/null 2>&1 && return 0
  hint=$(sh lib/install-hint.sh jq 2>/dev/null)
  echo "jolitz: jq is missing on this workstation. The guard"
  echo "reads commands only roughly without it and will block"
  echo "ordinary work. Install it: ${hint:-your package manager}"
  echo "Tell the user this first, before anything else."
}

jq_check
auto_update
hub_sync
