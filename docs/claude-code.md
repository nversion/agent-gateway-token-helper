# Claude Code

Claude Code can get its API key and its MCP server headers from a command. Model
credentials and MCP server definitions use different configuration files. The
[setup generator](https://nversion.dk/agent-gateway-token-helper/) fills in both.

## Model traffic through the gateway

In `~/.claude/settings.json` (`%USERPROFILE%\.claude\settings.json` on Windows),
set `ANTHROPIC_BASE_URL` to the gateway and `apiKeyHelper` to the helper command:

```json
{
  "apiKeyHelper": "agw-token-google",
  "env": {
    "ANTHROPIC_BASE_URL": "https://agw.example.com",
    "CLAUDE_CODE_API_KEY_HELPER_TTL_MS": "240000"
  }
}
```

Use `agw-token-entra` for Entra ID, or `agw-token --profile work` for a profile from
[`agw-token.json`](configuration.md#where-settings-come-from).

Claude Code reuses the printed token for `CLAUDE_CODE_API_KEY_HELPER_TTL_MS`. The 4
minutes above stay under the 5 minutes of validity `agw-token` guarantees by default
(`min-valid`), so Claude Code never holds an expired token. To cache longer, raise
`min-valid` in the profile above the TTL, for example `"min-valid": "10m"` with a TTL of
`"300000"`.

## MCP servers behind the gateway

`headersHelper` runs a command that prints a JSON object of HTTP headers. Put this
example in `.mcp.json` at your project root to share it with that project, or merge the
`mcpServers` entry into `~/.claude.json` (`%USERPROFILE%\.claude.json` on Windows) for
your user across projects. Local-scope servers also live in `~/.claude.json`, under the
project's entry; `claude mcp add-json --scope local` can create those. See Claude Code's
[MCP configuration scopes](https://code.claude.com/docs/en/mcp#mcp-installation-scopes).

```json
{
  "mcpServers": {
    "gateway": {
      "type": "http",
      "url": "https://agw.example.com/mcp",
      "headersHelper": "agw-token --profile work --output headers"
    }
  }
}
```

Create the `work` profile first, then sign in from a terminal with the same settings:

```sh
agw-token login --profile work
```

Claude Code gives `headersHelper` only 10 seconds to finish, so an initial browser
sign-in usually needs to happen before connecting. The helper runs on connection and
reconnection; an authentication failure can trigger a refresh and retry. If interactive
sign-in is needed again, run the login command above and reconnect through `/mcp`.
See the upstream [dynamic headers documentation](https://code.claude.com/docs/en/mcp#use-dynamic-headers-for-custom-authentication).
