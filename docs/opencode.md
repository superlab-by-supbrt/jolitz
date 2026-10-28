# OpenCode and local models

Back to the [README](../README.md).

Claude Code is what jolitz is developed with. OpenCode reads
the same `CLAUDE.md` and `.claude/skills/` while
`OPENCODE_DISABLE_CLAUDE_CODE` is unset. Other tools get the
rule layer; the skills (housekeeping, security, email, fleet
audit, incident, updates, docker, macOS cleanup) need Skills
support. `memory/opencode.json.example` carries a
`permission` block that denies in OpenCode what the guard
denies in Claude Code, as far as its glob patterns reach.

With a local model (Ollama or another provider on your own
hardware) every host is read-only by default: smaller models
keep long rule sets worse, and OpenCode runs no guard. The
line `Local model changes: allowed` in `memory/user.md`
lifts it (`rules/local-model.md`).

## A local model

With [Ollama](https://ollama.com) and OpenCode jolitz runs on
your own hardware:

```bash
ollama pull qwen3.5:9b
ollama run qwen3.5:9b
>>> /set parameter num_ctx 16384
>>> /save qwen3.5:9b-16k
>>> /bye
cp memory/opencode.json.example memory/opencode.json
opencode
```

Ollama's default context of 4096 tokens is too small for tool
use. Set `baseURL` and the model in `memory/opencode.json` and
pick it with `/models`. 14B and up call tools more reliably.
See the [OpenCode provider docs](https://opencode.ai/docs/providers/).
