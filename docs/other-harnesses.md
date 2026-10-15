# Other harnesses

Any harness that can run a command to get a credential can use `agw-token` (raw output)
or `agw-token --output headers`. Calls are cheap when the stored token is still valid,
taking a few milliseconds with no network access. Concurrent calls are serialized per
profile, so parallel tool calls open at most one browser window.

Some harnesses, such as Claude Desktop, run the command without arguments. Configure
those without flags in any of these ways:
[an `agw-token.json` next to the binary](configuration.md#where-settings-come-from), a
[binary named after the profile or provider](configuration.md#executable-name) such as
`agw-token-google`, or a [build with a default provider](configuration.md#build-time-default-provider).
