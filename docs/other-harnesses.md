# Other harnesses

Any harness that can run a command to get a credential can use `agw-token` (raw output)
or `agw-token --output headers`. Calls are cheap when the stored token is still valid,
taking a few milliseconds with no network access. Calls sharing the same token cache
key and state directory are serialized, so concurrent token requests reuse the first
successful sign-in. Profile names do not isolate accounts; see
[Shared tokens and account selection](configuration.md#shared-tokens-and-account-selection).

For harnesses that run the command without arguments, configure the helper in any of
these ways:
[an `agw-token.json` next to the binary](configuration.md#where-settings-come-from), a
[binary named after the profile or provider](configuration.md#executable-name) such as
`agw-token-google`, or a [build with a default provider](configuration.md#build-time-default-provider).

[Claude Desktop](claude-desktop.md#passing-arguments) also supports arguments through
`inferenceCredentialHelperArgs`, so it can select a profile with one installed executable.

Set the harness's credential cache lifetime below `min-valid`; 4 minutes works with
the default 5 minutes. Check its command timeout and sign in from a terminal first if
it cannot wait for browser authentication. The
[setup generator](https://nversion.dk/agent-gateway-token-helper/) prints the commands
with the right path and arguments for each platform.
