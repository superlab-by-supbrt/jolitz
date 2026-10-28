# Notifications

Push a report to a phone or a chat room: the fleet
summary (`bin/jolitz-overview --text`), a
housekeeping result, an incident report. Only when
the user asks, or when a scheduled run
(`rules/scheduled-housekeeping.md`) names it. Email
through the `jolitz-email` skill stays the default
for reports; a push carries the short version and
says where the long one is.

## Channels

Configured in `memory/user.md`, one `Notify:` line
per channel, more than one allowed:

```markdown
Notify: ntfy jolitz-fleet-a8f3
Notify ntfy server: https://ntfy.example.com
Notify ntfy header file: ~/.config/jolitz/ntfy-auth

Notify: matrix !abc123:example.org
Notify matrix server: https://matrix.example.org
Notify matrix header file: ~/.config/jolitz/matrix-auth

Notify: webhook
Notify webhook config file: ~/.config/jolitz/webhook.curlrc
```

- `Notify ntfy server` is optional; the default is
  `https://ntfy.sh`. `Notify ntfy header file` is
  optional: without it the topic is published
  unauthenticated.
- `Notify matrix server` and
  `Notify matrix header file` are required.
- `Notify webhook config file` is required; the
  webhook URL is the secret.

## Secrets only in files

The token, the webhook URL: never in `memory/`,
never on the command line, never in the report
(`rules/secrets.md`). They live in files under
`~/.config/jolitz/`, directory mode 700, files mode
600, which the operator creates:

```sh operator
mkdir -p -m 700 ~/.config/jolitz
install -m 600 /dev/null ~/.config/jolitz/ntfy-auth
printf 'Authorization: Bearer %s\n' 'tk_…' \
  > ~/.config/jolitz/ntfy-auth
install -m 600 /dev/null ~/.config/jolitz/matrix-auth
printf 'Authorization: Bearer %s\n' 'syt_…' \
  > ~/.config/jolitz/matrix-auth
install -m 600 /dev/null ~/.config/jolitz/webhook.curlrc
printf 'url = "%s"\n' 'https://hooks.slack.com/services/T0/B0/XX' \
  > ~/.config/jolitz/webhook.curlrc
```

A header file holds one HTTP header per line; curl
reads it with `-H @file`, so the value never reaches
`argv`. The curlrc holds curl options, one per line,
read with `-K file` (both from curl's manual). When
the user pastes a token in chat, write it that way,
with `install -m 600` first, and nowhere else.

## Build the message

Write the text to a file first, so that the same
file goes to every channel and no command carries
the text:

```
bin/jolitz-overview --text > ~/.cache/jolitz/notify.txt
```

Redact as for any report. Hostnames and findings go
through the channel; credentials never do. An ntfy
message over 4,096 bytes arrives as an attachment,
so keep a push short: headline, counts, where the
full report is.

## ntfy

Verified against https://docs.ntfy.sh/publish/ on
2026-10-28: the message is the request body; `Title`
and `Priority` are headers; authentication is an
`Authorization: Bearer <token>` header or `-u`.
`--data-binary` keeps the line breaks that `-d`
strips (curl's manual).

```
curl -sS --fail -H 'Title: jolitz fleet summary' \
  -H @"$HOME/.config/jolitz/ntfy-auth" \
  --data-binary @"$HOME/.cache/jolitz/notify.txt" \
  https://ntfy.example.com/jolitz-fleet-a8f3
```

Without a header file, drop the `-H @…` line. The
topic name is the address: choose a random one and
treat it like a password.

## Matrix

Verified against
https://spec.matrix.org/latest/client-server-api/ on
2026-10-28. The call is

```
PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}
```

with `Authorization: Bearer <access token>`, the body
`{"msgtype": "m.text", "body": "…"}` and the answer
`{"event_id": "…"}`. The transaction ID changes on
every request from the same device; the spec
suggests a UUID or a timestamp with a counter. The
room ID goes percent-encoded into the path (`!` is
`%21`, `:` is `%3A`).

```
jq -Rs '{msgtype: "m.text", body: .}' \
  ~/.cache/jolitz/notify.txt > ~/.cache/jolitz/notify.json
TXN="jolitz-$(date +%s)-$$"
curl -sS --fail -X PUT \
  -H @"$HOME/.config/jolitz/matrix-auth" \
  -H 'Content-Type: application/json' \
  --data-binary @"$HOME/.cache/jolitz/notify.json" \
  "https://matrix.example.org/_matrix/client/v3/rooms/%21abc123%3Aexample.org/send/m.room.message/$TXN"
```

The token belongs to a user that is a member of the
room; jolitz creates neither users nor rooms.

## Webhook (Slack-compatible)

Verified against
https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks
on 2026-10-28: `POST` to
`https://hooks.slack.com/services/…` with
`Content-type: application/json` and
`{"text": "…"}`. Slack: "Your webhook URL contains a
secret." Other chat servers offer Slack-compatible
webhooks; read their documentation before relying on
the same shape.

```
jq -Rs '{text: .}' ~/.cache/jolitz/notify.txt \
  > ~/.cache/jolitz/notify.json
curl -sS --fail -K "$HOME/.config/jolitz/webhook.curlrc" \
  -H 'Content-type: application/json' \
  --data-binary @"$HOME/.cache/jolitz/notify.json"
```

## Afterwards

- `--fail` makes curl exit non-zero on an HTTP
  error. Report the exit status; one retry at most,
  then the email skill or the plain statement that
  the message did not go out.
- Remove `~/.cache/jolitz/notify.*` after sending.
- A push is no server change: no journal entry, no
  changelog line. The local changelog of a scheduled
  run notes the channel, not the content.
- Never send on your own: no push without a request
  or a scheduled prompt that names it.
