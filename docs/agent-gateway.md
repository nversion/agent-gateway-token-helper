# Minimal agentgateway setup

This example connects `agw-token` to agentgateway **v1.6.0** and one Claude model on
Vertex AI. It accepts Google Workspace users from `example.com` and Entra users from
one tenant. It grants model access to everyone who matches those organization rules.
For an explicit user roster, budgets, UI access, and Cloud Run or Kubernetes deployment,
use the [full deployment recipe](agent-gateway-deployment.md).

## Prerequisites

- Install [agentgateway v1.6.0](https://github.com/agentgateway/agentgateway/releases/tag/v1.6.0)
  and [agw-token](../README.md#install).
- Enable Vertex AI in your Google Cloud project, enable access to the Claude model you
  intend to use, and check its availability and quota in your selected region.
- Give the identity running agentgateway the Vertex AI User role (`roles/aiplatform.user`)
  in that project. Its upstream Google credentials are separate from users' ID tokens.
  For a local run, configure [Application Default Credentials](https://docs.cloud.google.com/docs/authentication/set-up-adc-local-dev-environment):

  ```sh
  gcloud auth application-default login
  ```

## Configure authentication and a model

Save this as `config.yaml`. Replace `example-project`, `example.com` and the Entra tenant
ID everywhere they appear. Remove an unused identity provider and its corresponding
allow rule. The loopback listener is for local testing; remote deployments need HTTPS.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/agentgateway/agentgateway/v1.6.0/schema/config.json
gateways:
  default:
    bindAddress: 127.0.0.1
    port: 4000
    jwtAuth:
      mode: strict
      providers:
      - issuer: https://accounts.google.com
        audiences:
        - 32555940559.apps.googleusercontent.com
        jwks:
          url: https://www.googleapis.com/oauth2/v3/certs
      - issuer: https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0
        audiences:
        - 04b07795-8ddb-461a-bbee-02f9e1bf7b46
        jwks:
          url: https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/discovery/v2.0/keys
    authorization:
      rules:
      - allow: jwt.iss == "https://accounts.google.com" && jwt.email_verified && has(jwt.hd) && jwt.hd == "example.com"
      - allow: jwt.iss == "https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0" && jwt.tid == "11111111-1111-1111-1111-111111111111" && jwt.preferred_username.lowerAscii().endsWith("@example.com")
llm:
  gateways: default
  models:
  - name: claude-haiku
    provider:
      reference: vertex
    params:
      model: claude-haiku-4-5@20251001
  providers:
  - name: vertex
    provider: vertex
    params:
      vertexProject: example-project
      vertexRegion: europe-west1
  policies:
    transformations:
      request:
        remove:
        - anthropic-beta
```

The Google audience is the gcloud preset's client ID, and the Entra audience is the
Azure CLI preset's client ID. When using your own client, replace the corresponding
audience too. Audience checks are required in addition to organization restrictions;
see [Provider presets](configuration.md#provider-presets).

For Google Workspace, `hd` establishes the hosted domain. An email suffix alone does
not establish organization membership, even with `email_verified`; see
[Google's validation guidance](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token).
This example covers human accounts, not service-account token acquisition or admission.

Start the gateway from the directory containing the file:

```sh
agentgateway -f config.yaml
```

## Verify access

In another terminal, send a request without a token. It should be rejected by JWT
validation, normally with HTTP 401:

```sh
curl -i http://127.0.0.1:4000/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"claude-haiku","max_tokens":16,"messages":[{"role":"user","content":"Say hello."}]}'
```

Then sign in with an allowed Google Workspace account and send the same request:

```sh
agw-token-google login
curl --fail-with-body http://127.0.0.1:4000/v1/messages \
  -H "Authorization: Bearer $(agw-token-google)" \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"claude-haiku","max_tokens":16,"messages":[{"role":"user","content":"Say hello."}]}'
```

For Entra, use `agw-token-entra login --tenant YOUR_TENANT_ID` first and replace the
command inside `$(...)` with `agw-token-entra --tenant YOUR_TENANT_ID`. Use the same
identity settings on both calls. A successful request returns a model response and
incurs normal Vertex usage charges. An upstream permission, model or quota error means
the Vertex configuration still needs attention.

Also verify that a Google account outside the allowed Workspace domain is denied and
that a token issued for a different client ID is denied. These checks distinguish
organization and audience enforcement from merely accepting a valid Google signature.

For a remote service, use its HTTPS URL instead of loopback and configure the harness
with the model name this gateway exposes (`claude-haiku` here). The
[deployment recipe](agent-gateway-deployment.md#verify-the-deployment) adds checks for
roster enforcement, UI login, pricing and budgets.
