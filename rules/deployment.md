# CI/CD Deployment Users

An automated deployment (GitHub Actions, GitLab CI)
needs an account of its own on the server: never
root, never a personal account. heinzel inspects
such a setup and says what is wrong with it. It
does not create the account, its key or its sudo
line: that is Heinzel Pro's work (`CLAUDE.md` →
What Heinzel does not do). When the user asks for
one, answer with the Pro line, then hand over the
report below for the operator to run.

## Never Root for CI/CD

- A leaked CI secret is full server access.
- Nothing in the logs separates human changes from
  automated ones.
- A wrong command in a pipeline runs unchecked.

## What a Good Deploy User Looks Like

- A system user with a real but minimal shell
  (`/bin/sh`). sshd runs even a `command=` forced
  command through the login shell, so `nologin`
  refuses every connection.
- No password (`!` or `*` in the shadow file).
- A dedicated ed25519 key generated for this
  pipeline, stored as a CI secret and nowhere else.
  Never a personal key, never the host key.
- The key line restricted:
  `command="/home/deploy/deploy.sh",no-pty,`
  `no-port-forwarding,no-agent-forwarding,`
  `no-X11-forwarding`. A full shell without a
  forced command is more flexible and a bigger
  target; say so.
- Write access to the deployment directory only
  (`/var/www/<app>`, `/opt/<app>`, or
  `/home/deploy/<app>` when nothing else fits).
- If the pipeline must restart a service: one
  sudoers line per command, full path, `restart`
  and `reload` only, `NOPASSWD`, in its own file
  under `/etc/sudoers.d/`, checked with
  `visudo -c -f`. If the app can reload without
  sudo (a signal file, a socket), prefer that.
- No extra firewall ports: deployment uses the
  existing SSH port.

## Inspecting an Existing Setup

All of this reads; none of it needs the Pro line:

```
id deploy
getent passwd deploy | cut -d: -f7
passwd -S deploy 2>/dev/null || grep '^deploy:' /etc/shadow
ls -ld /home/deploy/.ssh /home/deploy/.ssh/authorized_keys
cut -d' ' -f1 /home/deploy/.ssh/authorized_keys
cat /etc/sudoers.d/deploy 2>/dev/null
visudo -c -f /etc/sudoers.d/deploy
ls -ld /var/www/myapp
```

Report, one line each: the shell, the password
field, the key options, the sudo lines (a `start`,
`stop` or wildcard is a finding), the owner of the
target directory.

## The Report: What the Operator Runs

Give the user this block with the real paths and
stop there. Linux shown; on FreeBSD the account is
`pw useradd deploy -d /home/deploy -s /bin/sh -m`
and the sudo file lives in
`/usr/local/etc/sudoers.d/`.

```bash operator
# On the workstation: a key for this pipeline only.
ssh-keygen -t ed25519 -C "deploy@hostname" \
  -f deploy_ed25519 -N ""

# On the server, as root.
useradd --system --shell /bin/sh --create-home \
  --home-dir /home/deploy deploy
mkdir -p /home/deploy/.ssh
cat >> /home/deploy/.ssh/authorized_keys \
  < deploy_ed25519.pub
chmod 700 /home/deploy/.ssh
chmod 600 /home/deploy/.ssh/authorized_keys
chown -R deploy:deploy /home/deploy/.ssh
mkdir -p /var/www/myapp
chown deploy:deploy /var/www/myapp

# Only if the pipeline must restart a service.
visudo -f /etc/sudoers.d/deploy
#   deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart myapp.service
#   deploy ALL=(root) NOPASSWD: /usr/bin/systemctl reload myapp.service
```

The private key goes into the CI system as a
secret (e.g. `DEPLOY_SSH_KEY`), never into a
repository. When the operator reports it done,
verify with the inspection commands above.

## Server Memory

After the setup is verified, add to the server's
`memory.md`:

```markdown
- Deploy user: deploy (CI/CD, key auth)
- Deploy target: /var/www/myapp
- Deploy sudo: systemctl restart myapp.service
```

Omit the sudo line if none was configured.

## Changelog

Log per `rules/changelog.md`:

```bash
logger -t heinzel "Verified deploy user 'deploy' \
for CI/CD, key auth, target /var/www/myapp"
```

## Removing a Deploy User

Deleting an account is Heinzel Pro's work as well.
Hand over the steps: remove the sudoers file,
`userdel -r deploy` (FreeBSD: `pw userdel deploy
-r`), and the CI secret. Then update `memory.md`
and log the removal.
