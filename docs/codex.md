# Codex

Codex can send model traffic to a
[custom model provider](https://learn.chatgpt.com/docs/config-file/config-advanced) and get the
bearer token from a command in the provider's `auth` table. Add this to
`~/.codex/config.toml` (`%USERPROFILE%\.codex\config.toml` on Windows), or let the
[setup generator](https://nversion.dk/agent-gateway-token-helper/) fill it in:

```toml
model_provider = "gateway"

[model_providers.gateway]
name = "Agent Gateway"
base_url = "https://agw.example.com/v1"
wire_api = "responses"

[model_providers.gateway.auth]
command = "/usr/local/bin/agw-token-entra"   # or agw-token-google
# Windows: command = 'C:\Program Files\Agent Gateway helper\agw-token-entra.exe'
timeout_ms = 300000
refresh_interval_ms = 240000
```

Codex sends requests to `base_url` plus `/responses`, so the gateway must route the
OpenAI Responses API. `timeout_ms` has to cover a browser sign-in, hence 5 minutes
here. Codex reruns the command every `refresh_interval_ms`, and the 4 minutes
here stays under `agw-token`'s default `min-valid` of 5 minutes, so it never holds an
expired token. Use a single-quoted TOML string for Windows paths so the backslashes are
kept as written. Don't combine the `auth` table with `env_key` or
`requires_openai_auth`.
