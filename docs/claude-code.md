# Claude Code

Claude Code can get its API key and its MCP server headers from a command. Both go in
`~/.claude/settings.json` (`%USERPROFILE%\.claude\settings.json` on Windows).

## Model traffic through the gateway

Set `ANTHROPIC_BASE_URL` to the gateway, and set `apiKeyHelper` to `agw-token`:

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

`headersHelper` runs a command that prints a JSON object of HTTP headers:

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
