# Join a workstation to the hub

Nothing connects to a server here; everything runs on the
workstation. Three questions, one file, one pull.

## 1. Questions, in order

1. **URL.** "Which hub?" `https://hub.example.com`, or
   `http://127.0.0.1:7331` when the hub has no proxy and is
   reached through a tunnel (`ssh -N -L 7331:127.0.0.1:7331
   root@ops.example.com` in a second terminal, the operator's
   own). `curl -fsS <url>/api/v1/health` must answer before the
   next question; otherwise stop and say what it printed.
2. **Token.** The admin created it with `user add` and handed it
   over. It goes into a file the user writes in their own
   terminal; jolitz never puts a token into a command it runs,
   a file it writes, memory or the changelog
   (`rules/notify.md` → Secrets only in files,
   `rules/secrets.md`). Show this block and wait:

   ```sh operator
   mkdir -p -m 700 ~/.config/jolitz
   install -m 600 /dev/null ~/.config/jolitz/hub-token
   printf 'Authorization: Bearer %s\n' '<the token>' \
     > ~/.config/jolitz/hub-token
   ```

   One line, `Authorization: Bearer <token>`: curl reads it
   with `-H @file`, so the value never reaches `argv`. A user
   who already has such a file names its path instead. When
   the user pastes the token into the chat anyway, say that it
   should not be there, tell them to create the file as above,
   and ask the admin for `user rotate` when the chat is not
   private.
3. **Workstation name.** The name the team sees next to this
   person's sessions. Propose `hostname -s` and confirm in one
   line.

Check the file without reading it: `ls -l
~/.config/jolitz/hub-token` shows mode `-rw-------` and a size
over 30 bytes. `wc -l` says 1. Never `cat` it.

## 2. Write hub.conf

```
mkdir -p -m 700 ~/.config/jolitz
install -m 600 /dev/null ~/.config/jolitz/hub.conf
tee ~/.config/jolitz/hub.conf <<'EOF'
url=https://hub.example.com
token_file=/home/alice/.config/jolitz/hub-token
workstation=alices-mbp
EOF
ls -l ~/.config/jolitz/hub.conf
```

`token_file` is an absolute path (`$HOME` spelled out:
`bin/jolitz-sync` parses the file with `sed`, it does not source
it). `workstation` may be left out; the default is `hostname
-s`. Mode 600 is the way `install` made it; `tee` keeps it.
`Way back: rm ~/.config/jolitz/hub.conf`; the token file stays
until the user deletes it.

## 3. First status and pull

```
bin/jolitz-sync status
```

URL and hub version, `token: accepted (<user>)`, the
workstation name, files to push, files to pull, conflicts,
claims, sessions. Exit 3 means unreachable, 2 a config error
(`hub.conf`, the token file, a 401 or 403): fix, do not
continue. Then:

```
bin/jolitz-sync pull
```

Every file the hub has and the workstation lacks lands in
`memory/` byte-identical. A file that exists on both sides and
differs is a conflict on this first pull, because nothing was
synced before: the local file is kept, the hub's version is
written next to it as `<file>.hub` and listed at the end, exit
1. Show that list and `rules/hub.md` → Conflicts: the user
decides per file, `diff -u` shows the two. Never pick a side
yourself. From the second pull on, only a file changed on both
sides since the last sync is a conflict.

`memory/user.md`, `blacklist.md`, `readonly.md`,
`protected-paths.md`, `opencode.json`, `overview/` and `eol/`
are never synced in either direction.

## 4. Push what is here

When `status` listed files to push and the user agrees:

```
bin/jolitz-memory-lint -d memory
bin/jolitz-sync push
```

The lint runs inside `push` too and stops on `ERROR`: a value
that looks like a credential is replaced by its location before
anything leaves the workstation (`rules/server-memory.md`). The
hub refuses such a file as well, with the line number, and
stores nothing.

## 5. Record

One line in `memory/user.md`, next to the other preferences:

```markdown
Hub: https://hub.example.com
```

No token, no token path, no workstation name there; those live
in `hub.conf`. No journal entry on any server, no changelog
line: joining changes the workstation only. From the next
session on, the SessionStart hook pulls and opens a session,
the Stop hook closes it, and the changelog step pushes
(`rules/changelog.md`); `bin/jolitz-doctor` shows a `hub:` line.

## Windows (Git Bash)

Same files, same commands; `~` is the Git Bash home. Aliases in
`memory/servers/` are files holding the canonical name there
(`rules/dns-aliases.md`), and that is also how the hub stores
them, so a pull on Windows writes the file form and a pull on
Unix recreates the symlink.
