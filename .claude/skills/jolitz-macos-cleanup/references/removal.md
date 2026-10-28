# Removal

Only after the user approved the group or the entry.

## Before removing

- **Quit the app.** A running app writes its folders again.
  The uninstall mode lists running apps in `running`. Ask the
  user to quit them, or propose
  `osascript -e 'quit app "<name>"'`.
- **Homebrew casks.** If `brew list --cask` shows the app, use
  `brew uninstall --cask --zap <token>` first. The `zap` stanza
  removes the files the cask knows. Rerun the scanner for the
  rest. `rules/macos.md` covers Homebrew and `sudo`.
- **Profiles and passwords.** Browser, mail and password apps
  keep user data in the matched folders. Name that in the
  proposal.

## Move to the Trash

`/usr/bin/trash` exists since macOS 15. It moves files of the
current user to their Trash:

    /usr/bin/trash "<path>" ...

Before macOS 15, use the Finder:

    osascript -e 'tell application "Finder" to delete POSIX file "<path>"'

Never use `rm -rf` on Library entries. The one exception is a
dead symlink: `rm "<link>"` removes the link, no data.

Harness permission rules may block the move. Then stop, name the
blocked paths, and give the user the command to run. Never work
around the block.

## Root-owned entries

`/Library` entries belong to root. Probe `sudo -n true` per
`rules/privilege-escalation.md`. Without sudo, give the user the
commands.

`sudo /usr/bin/trash` would move the files to root's Trash. Move
them into the user's Trash instead. A `~/Library` entry of the
same name may already be there. `mv` refuses to replace a folder
that is not empty and silently replaces a file. Always add a
suffix and use `-n`:

    sudo mv -n "/Library/Application Support/<name>" \
      ~/.Trash/"<name> (System)"

For a LaunchDaemon, unload the job before moving its plist and
helper (`rules/macos.md`, Service Manager):

    sudo launchctl unload /Library/LaunchDaemons/<label>.plist
    sudo mv -n /Library/LaunchDaemons/<label>.plist \
      ~/.Trash/"<label> (System).plist"
    sudo mv -n /Library/PrivilegedHelperTools/<helper> \
      ~/.Trash/"<helper> (System)"

For a user LaunchAgent, run `launchctl unload <plist>` without
`sudo`.

## Installer receipts

A receipt is a database entry. Removing it frees no space:

    sudo pkgutil --forget <package id>

## After removing

Test each approved path again. Run a separate check for dead
links, because `test -e` fails on them even when they remain:

    for p in "<path>" ...; do
      if [ -e "$p" ] || [ -L "$p" ]; then echo "STILL $p"; fi
    done
