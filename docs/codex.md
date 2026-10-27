# Codex

Codex can send model traffic to a
[custom model provider](https://learn.chatgpt.com/docs/config-file/config-advanced) and get the
bearer token from a command in the provider's `auth` table. Add this to
`~/.codex/config.toml` (`%USERPROFILE%\.codex\config.toml` on Windows), or let the
[setup generator](https://agw-token.nversion.dk/) fill it in:

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

## Passing arguments

Recent Codex releases also take `args`, an array of arguments for the command, so the
plain `agw-token` can select a profile from
[`agw-token.json`](configuration.md#where-settings-come-from) or any other flag:

```toml
[model_providers.gateway.auth]
command = "/usr/local/bin/agw-token"
args = ["--profile", "work"]
timeout_ms = 300000
refresh_interval_ms = 240000
```

The arguments can instead be `["--provider", "entra", "--tenant", "contoso.onmicrosoft.com"]`.
Codex runs the command directly, not through a shell, so each argument is one array
element. For a Codex release without `args`, drop the line and point `command` at
`agw-token-entra`, `agw-token-google` or a
[binary named for the profile](configuration.md#executable-name).
