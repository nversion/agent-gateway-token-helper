# Gateway deployment with budgets and UI access

Start with the [minimal gateway example](agent-gateway.md) to check authentication and
model access. This larger recipe targets agentgateway **v1.6.0** and adds a user roster,
per-person budgets, an OIDC-protected UI, and deployment on Cloud Run or Kubernetes.

The [agentgateway](https://agentgateway.dev) configuration accepts the
ID tokens `agw-token` prints. It fronts Claude and Gemini on Vertex AI and accepts both
Google and Microsoft Entra ID sign-ins, so `agw-token-google` and `agw-token-entra` both
work against it. Names, domains, the Google OAuth client and the project are
placeholders; replace them with your own. The human sign-in examples exclude service
accounts; adding one to the roster alone does not authorize it.

The example trusts two Entra tenants. Replace their placeholder IDs everywhere they
appear, in the issuer and JWKS URLs and in the `tid` checks:

| Placeholder tenant ID | Domain it owns |
|---|---|
| `11111111-1111-1111-1111-111111111111` | `example.com` |
| `22222222-2222-2222-2222-222222222222` | `partner.example` |

Look up a tenant's ID with `az account show --query tenantId`, or open
`https://login.microsoftonline.com/<domain>/v2.0/.well-known/openid-configuration` and
read it from the issuer.

The same file runs on Cloud Run and on Kubernetes. Only the way it reaches the container
and how the secrets are injected differ, see [Cloud Run](#cloud-run) and
[Kubernetes](#kubernetes).

## How a request is admitted

agentgateway attaches budgets only to API keys, and the one credential people carry is the
token `agw-token` prints. The config turns that token into a per-person key:

1. The gateway-level `jwtAuth` validates the token in optional mode, because the UI shares
   the listener and signs in with cookies.
2. A request transformation copies the address the token proves (`preferred_username`,
   `upn` or `email`, lower-cased) into `x-api-key`. Whatever `x-api-key` the client sent
   is overwritten, or removed when there is no address.
3. The LLM route checks the token again in strict mode. Its authorization rules pin each
   issuer to an organization: the Workspace domains for Google, and the tenant ID plus the
   domain that tenant owns for Entra. This is the check
   [Provider presets](configuration.md#provider-presets) warns about, since every gcloud or
   Azure CLI user can get a token with the preset audience.
4. The `apiKey` policy looks the address up in the roster. The entry gives the person's
   group and 30-day budget, and an address that is not listed is refused. These are
   consecutive 30-day windows, not calendar months.

The address is a convenient roster and budget key, but it can change or be reassigned.
Review roster entries when users leave or change addresses. If your deployment needs
stable identity across those changes, key admission and budgets by Google `sub` or
Entra `(tid, oid)` and keep the address as display metadata.

## Prepare the deployment

Use a project with the Vertex AI API enabled, access to the models below, and quota in
the selected regions. Check model IDs and region availability for your project before
deploying; the aliases below deliberately route to specific upstream versions.
You also need a reachable PostgreSQL database and a Google OAuth **web** client for
the UI, separate from the helper's native client. Register
`https://agw.example.com/oauth/callback` on that web client after choosing your public URL.

For the Cloud Run or GKE deployment, create a Google service account for upstream Vertex
access (or use an existing one). These examples use the same project for the gateway
and Vertex:

```sh
gcloud services enable aiplatform.googleapis.com --project example-project
gcloud iam service-accounts create agentgateway --project example-project
gcloud projects add-iam-policy-binding example-project \
  --member='serviceAccount:agentgateway@example-project.iam.gserviceaccount.com' \
  --role=roles/aiplatform.user
```

Create `config.yaml` from the next section, replacing the project, domains, tenant IDs,
roster entries and UI client ID. The Google JWT audience lists contain both the gcloud
preset's client ID and the UI web client ID. Replace the latter everywhere it appears;
if you use your own helper client, replace the gcloud audience with that client ID too.

### Generate the price catalog

Install `agctl` from the [agentgateway v1.6.0 release](https://github.com/agentgateway/agentgateway/releases/tag/v1.6.0)
and generate agentgateway's catalog format from models.dev:

```sh
agctl catalog import --source models.dev --pretty --out catalog.json
```

This converts the source data; a raw download of models.dev's API JSON is not the same
format. Inspect the generated provider/model entries and rates for every route below,
including the exact Vertex model IDs. Refresh the file when you change models or prices
and review its diff alongside the configuration. See the
[catalog import reference](https://agentgateway.dev/docs/kubernetes/latest/reference/agctl/agctl-catalog-import/).

Version 1.6.0 also includes a built-in catalog. An imported base catalog competes with it
by generation timestamp; the newer base wins. Missing model prices can leave traffic
unpriced, so verify recorded costs before relying on dollar budgets. This recipe mounts
an explicit catalog to make the selected rates reviewable. See
[model cost configuration](https://agentgateway.dev/docs/standalone/latest/documentation/llm/cost-controls/costs/).

## config.yaml

Values written as dollar-prefixed names (`DB_URL`, `OIDC_CLIENT_SECRET`, `PUBLIC_URL`) are
expanded from the environment at startup, so the file holds no secrets and can be
committed. The expansion runs over the raw file text, so an upper-case dollar-name inside
a comment is looked up too and fails the parse if it isn't set.

The database holds the request log and the budget counters, which is what lets the
service survive cold starts and run more than one instance. `catalog.json` is the file
generated in the preceding step.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/agentgateway/agentgateway/v1.6.0/schema/config.json
#
# agentgateway, fronting Claude and Gemini on Vertex AI in example-project.
#
# Secrets are written as dollar-prefixed names and expanded from the environment
# at startup, so this file is safe to commit; the deployment injects the values
# (Secret Manager on Cloud Run, a Secret on Kubernetes). Note that the expansion
# runs over the raw file text, so an upper-case dollar-name in a comment is
# looked up too and will fail the parse.
config:
  database:
    url: $DB_URL
  # this file is the only source of config: the UI is a viewer, and any write
  # from it is refused. The database still holds the request log and the
  # budget counters, which is what lets the service survive cold starts
  storage:
    mode: readOnly
  # the built-in expression resolves jwt.sub first, so an identity would log as
  # an opaque subject; the address first makes the analytics filters readable,
  # and the group comes off the roster entry the token matched. Same claim order
  # and case fold as the x-api-key stamp below, so the log shows the address the
  # budget is keyed on; sub stays outside the fold because a subject is opaque
  # and case-sensitive. coalesce skips an erroring argument, so the fold of a
  # missing address falls through to sub
  standardAttributes:
    user: coalesce(coalesce(jwt.preferred_username, jwt.upn, jwt.email, null).lowerAscii(), jwt.sub, null)
    group: coalesce(apiKey.group, "unassigned")
  # Import and review prices with agctl; keep the file beside this config.
  # Check that the active catalog prices every routed model before using budgets.
  modelCatalog:
  - file: /config/catalog.json
gateways:
  default:
    port: 4000
    # Budgets only attach to API keys, and the one credential people carry is
    # the ID token agw-token obtains with the public cloud CLI client. Validate
    # it here and stamp the address it proves into x-api-key before the
    # LLM route's apiKey policy runs; that policy treats the address as the key
    # and attaches the budget.
    # Optional mode because the UI shares this gateway and signs in with OIDC
    # cookies. The LLM route re-checks the token in strict mode, so nothing
    # reaches the key lookup without a valid token, and whatever x-api-key a
    # client sends is overwritten with the address the token proves.
    jwtAuth:
      mode: optional
      preserveToken: true
      providers:
      - issuer: https://accounts.google.com
        audiences:
        - 32555940559.apps.googleusercontent.com
        - 123456789012-abcdefghijklmnopqrstuvwxyz012345.apps.googleusercontent.com
        jwks:
          url: https://www.googleapis.com/oauth2/v3/certs
      # agw-token signs in as the Azure CLI client and hands over the ID token,
      # which is v2.0 and minted for that client, so here the audience does gate.
      # One provider per trusted tenant: Entra tenants share signing keys, and the
      # gateway picks the provider by the token's iss before its kid.
      # partner.example, Entra tenant ID 22222222-2222-2222-2222-222222222222
      - issuer: https://login.microsoftonline.com/22222222-2222-2222-2222-222222222222/v2.0
        audiences:
        # Azure CLI client ID for the audience check, exists in every tenant
        - 04b07795-8ddb-461a-bbee-02f9e1bf7b46
        jwks:
          url: https://login.microsoftonline.com/22222222-2222-2222-2222-222222222222/discovery/v2.0/keys
      # example.com, Entra tenant ID 11111111-1111-1111-1111-111111111111
      - issuer: https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0
        audiences:
        - 04b07795-8ddb-461a-bbee-02f9e1bf7b46
        jwks:
          url: https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/discovery/v2.0/keys
    transformations:
      request:
        set:
          # Entra's email claim is the mail attribute, which may be an alias
          # (dave.jensen@ for DJ@); preferred_username is the UPN, the sign-in
          # identity, and the one the roster is keyed on. upn is where v1.0 az
          # tokens carried it, and Google has only email. Entra returns the UPN
          # in directory casing (DJ@example.com) and the key lookup is exact,
          # so fold it. A null or failed expression removes the header, which
          # is what keeps a client-sent x-api-key from surviving
          x-api-key: coalesce(jwt.preferred_username, jwt.upn, jwt.email, null).lowerAscii()
ui:
  gateways: default
  policies:
    oidc:
      issuer: https://accounts.google.com
      clientId: 123456789012-abcdefghijklmnopqrstuvwxyz012345.apps.googleusercontent.com
      clientSecret: $OIDC_CLIENT_SECRET
      # the service is reached on its public URL, which the deployment passes in
      # as PUBLIC_URL so the image carries no address of its own
      redirectURI: $PUBLIC_URL/oauth/callback
      # openid alone yields a token with only a subject; the rule below needs the
      # email claims, which Google only includes when this scope is requested
      scopes:
      - email
    # the OAuth client decides who can sign in at all; this decides who gets the
    # UI once they have, so an External audience on the client does not open the
    # UI to every Google account
    authorization:
      rules:
      - require: jwt.email_verified && has(jwt.hd) && jwt.hd in ["example.org", "example.com"]
mcp:
  gateways: default
  targets: []
llm:
  gateways: default
  models:
  - name: google/*
    provider:
      reference: vertex-eu
  # Pin the upstream Vertex model ID and verify that the active catalog has
  # prices for that provider/model. Routing success alone does not prove that
  # a request was priced.
  - name: claude-sonnet-*
    provider:
      reference: vertex-eu
    params:
      model: claude-sonnet-5@default
  # This example uses europe-west1 for Haiku. Confirm model availability and
  # quota in your project before choosing a region.
  - name: claude-haiku-*
    provider:
      reference: vertex-europe-west1
    params:
      model: claude-haiku-4-5@20251001
  - name: claude-opus-*
    provider:
      reference: vertex-eu
    params:
      model: claude-opus-5@default
  # Two provider configurations let routes select their Vertex region.
  providers:
  - name: vertex-eu
    provider: vertex
    params:
      vertexProject: example-project
      vertexRegion: eu
  - name: vertex-europe-west1
    provider: vertex
    params:
      vertexProject: example-project
      vertexRegion: europe-west1
  virtualModels: []
  policies:
    cors:
      allowOrigins:
      - $PUBLIC_URL
      allowHeaders:
      - '*'
      allowMethods:
      - GET
      - POST
    jwtAuth:
      mode: strict
      providers:
      - issuer: https://accounts.google.com
        audiences:
        - 32555940559.apps.googleusercontent.com
        - 123456789012-abcdefghijklmnopqrstuvwxyz012345.apps.googleusercontent.com
        jwks:
          url: https://www.googleapis.com/oauth2/v3/certs
        jwtValidationOptions:
          requiredClaims:
          - exp
      # partner.example, Entra tenant ID 22222222-2222-2222-2222-222222222222
      - issuer: https://login.microsoftonline.com/22222222-2222-2222-2222-222222222222/v2.0
        audiences:
        - 04b07795-8ddb-461a-bbee-02f9e1bf7b46
        jwks:
          url: https://login.microsoftonline.com/22222222-2222-2222-2222-222222222222/discovery/v2.0/keys
        jwtValidationOptions:
          requiredClaims:
          - exp
      # example.com, Entra tenant ID 11111111-1111-1111-1111-111111111111
      - issuer: https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0
        audiences:
        - 04b07795-8ddb-461a-bbee-02f9e1bf7b46
        jwks:
          url: https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/discovery/v2.0/keys
        jwtValidationOptions:
          requiredClaims:
          - exp
    # the roster: an email here is a person allowed through, and the entry
    # carries their group and 30-day cap. The "key" is the email the gateway
    # stamped into x-api-key from the verified token: a lookup key, not a secret.
    # Windows are aligned to the Unix epoch, so 30d is consecutive 30-day periods
    # rather than calendar months, and the request that crosses the limit still
    # completes; the next one is refused.
    #
    # Each group's cap is written once, on a placeholder anchor entry, as a YAML
    # anchor; everyone in the group aliases it. Raising a cap is one edit, adding
    # a person is three lines.
    apiKey:
      mode: strict
      location:
        header:
          name: x-api-key
      keys:
      # test: 3 USD, for proving that the block actually fires
      # the budget is anchored on a placeholder key so removing a person never
      # takes the group's budget with it, and .invalid can never be a real identity
      - key: test@anchor.invalid
        metadata: {name: test-anchor, group: test}
        # creating a yaml anchor to reference: https://yaml.org/spec/1.2.2/#3222-anchors-and-aliases
        budgets: &test
        - name: monthly-spend
          limit: {unit: USD, amount: 3}
          window: {rolling: 30d}
          onBudgetExceeded: Block
      - key: alice@example.org
        metadata: {name: alice, group: test}
        budgets: *test
      # internals: 1000 USD per person
      # the budget is anchored on a placeholder key so removing a person never
      # takes the group's budget with it, and .invalid can never be a real identity
      - key: internals@anchor.invalid
        metadata: {name: internals-anchor, group: internals}
        budgets: &internals
        - name: monthly-spend
          limit: {unit: USD, amount: 1000}
          window: {rolling: 30d}
          onBudgetExceeded: Block
      # per-person override: 4000 USD, does not follow the internals anchor
      - key: carol.hansen@example.com
        metadata: {name: carol.hansen, group: internals}
        budgets:
        - name: monthly-spend
          limit: {unit: USD, amount: 4000}
          window: {rolling: 30d}
          onBudgetExceeded: Block
      - key: dj@example.com
        metadata: {name: dj, group: internals}
        budgets: *internals
      # externals: 1000 USD per person
      # the budget is anchored on a placeholder key so removing a person never
      # takes the group's budget with it, and .invalid can never be a real identity
      - key: externals@anchor.invalid
        metadata: {name: externals-anchor, group: externals}
        budgets: &externals
        - name: monthly-spend
          limit: {unit: USD, amount: 1000}
          window: {rolling: 30d}
          onBudgetExceeded: Block
      - key: grace@partner.example
        metadata: {name: grace, group: externals}
        budgets: *externals
    # the roster above is the real admission list; this only keeps a stray
    # identity from ever reaching it. All three domains are in use.
    # One rule per issuer, because a Google token carries email_verified and an
    # Azure CLI token has no such claim. Allow rules are ORed, so either admits;
    # a require rule is ANDed and could not express that.
    authorization:
      rules:
      - allow: jwt.iss == "https://accounts.google.com" && jwt.email_verified && has(jwt.hd) && jwt.hd in ["example.org", "example.com"]
      # the agw-token ID token: the provider already checked the audience, so tid
      # is the only pin left. The domain is checked on preferred_username, the
      # UPN, because that is the claim the x-api-key stamp uses; it comes back in
      # directory casing, so fold before comparing.
      # Each tenant is paired with the domain it owns; a tenant that does not own
      # the domain in its rule boots fine and then admits nobody.
      # partner.example, Entra tenant ID 22222222-2222-2222-2222-222222222222
      - allow: jwt.iss == "https://login.microsoftonline.com/22222222-2222-2222-2222-222222222222/v2.0" && jwt.tid == "22222222-2222-2222-2222-222222222222" && jwt.preferred_username.lowerAscii().endsWith("@partner.example")
      # example.com, Entra tenant ID 11111111-1111-1111-1111-111111111111
      - allow: jwt.iss == "https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0" && jwt.tid == "11111111-1111-1111-1111-111111111111" && jwt.preferred_username.lowerAscii().endsWith("@example.com")
    transformations:
      request:
        # Vertex rejects the anthropic-beta header, so strip it here rather than
        # asking every client to set CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS
        remove:
        - anthropic-beta
frontendPolicies:
  http:
    # 32 MB: Claude Code sends large payloads
    maxBufferSize: 33554432
```

## Cloud Run

Enable the deployment APIs:

```sh
gcloud services enable run.googleapis.com artifactregistry.googleapis.com \
  cloudbuild.googleapis.com secretmanager.googleapis.com --project example-project
```

Use an existing Docker repository named `images` in `europe-west1`, or create it:

```sh
gcloud artifacts repositories create images --repository-format=docker \
  --location=europe-west1 --project example-project
```

Bake the config and generated price catalog into an image. Save this as `Dockerfile`
beside `config.yaml` and `catalog.json`, in a directory containing no credential files:

```dockerfile
FROM cr.agentgateway.dev/agentgateway:v1.6.0
COPY config.yaml catalog.json /config/
CMD ["-f", "/config/config.yaml"]
```

Build and push the image from that directory:

```sh
gcloud builds submit --project example-project \
  --tag europe-west1-docker.pkg.dev/example-project/images/agentgateway:1 .
```

Store `DB_URL` and `OIDC_CLIENT_SECRET` in Secret Manager and reference them from the
service. `PUBLIC_URL` is the service's own URL, or the custom domain in front of it. The
runtime service account from [Prepare the deployment](#prepare-the-deployment) needs
Secret Accessor on those two secrets as well as its Vertex role. Create each secret
from a local file containing only its value, kept outside the image build directory:

```sh
gcloud secrets create agentgateway-db-url --project example-project \
  --replication-policy=automatic --data-file=DB_URL_FILE
gcloud secrets create agentgateway-oidc-client-secret --project example-project \
  --replication-policy=automatic --data-file=OIDC_CLIENT_SECRET_FILE
for secret in agentgateway-db-url agentgateway-oidc-client-secret; do
  gcloud secrets add-iam-policy-binding "$secret" --project example-project \
    --member='serviceAccount:agentgateway@example-project.iam.gserviceaccount.com' \
    --role=roles/secretmanager.secretAccessor
done
```

For existing secrets, add a new version instead of creating them. See
[Cloud Run secret configuration](https://docs.cloud.google.com/run/docs/configuring/services/secrets).

Cloud Run must let requests reach agentgateway so the gateway can validate these Google
and Entra tokens. `ingress: all` only controls network ingress; it does not disable the
Cloud Run Invoker IAM check. The service below disables that check, while agentgateway
enforces JWT authentication on model traffic and OIDC on the UI. If organization policy
requires Cloud Run IAM authentication, use an approved ingress/authentication design
instead; the preset tokens alone do not satisfy that separate check. See
[Cloud Run invocation access](https://docs.cloud.google.com/run/docs/authenticating/public).

```yaml
# service.yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: agentgateway
  annotations:
    run.googleapis.com/ingress: all
    # agentgateway validates client JWTs and protects the UI with OIDC.
    run.googleapis.com/invoker-iam-disabled: "true"
spec:
  template:
    metadata:
      annotations:
        # keeps one instance warm; budgets and the log live in the database either way
        autoscaling.knative.dev/minScale: "1"
    spec:
      serviceAccountName: agentgateway@example-project.iam.gserviceaccount.com
      containers:
      - image: europe-west1-docker.pkg.dev/example-project/images/agentgateway:1
        ports:
        - containerPort: 4000
        env:
        - name: PUBLIC_URL
          value: https://agw.example.com
        - name: DB_URL
          valueFrom:
            secretKeyRef:
              name: agentgateway-db-url
              key: latest
        - name: OIDC_CLIENT_SECRET
          valueFrom:
            secretKeyRef:
              name: agentgateway-oidc-client-secret
              key: latest
```

```sh
gcloud run services replace service.yaml --region europe-west1 --project example-project
```

The database connection, such as a Cloud SQL instance or a private IP over a VPC
connector, is configured on the service as for any other Cloud Run workload. Register the
UI's OAuth redirect URI, `<PUBLIC_URL>/oauth/callback`, on the Google OAuth client.

## Kubernetes

Use the release image unchanged and mount the files from a ConfigMap. The examples use
the `default` namespace consistently; change every namespace reference if you use
another one. Create the Kubernetes service account before applying the Deployment:

```sh
kubectl create serviceaccount agentgateway --namespace default
```

On a GKE cluster with Workload Identity Federation enabled for the workload's node pool,
link that Kubernetes account to the Google service account created above. The pool
identifier below assumes the cluster is also in `example-project`:

```sh
gcloud iam service-accounts add-iam-policy-binding \
  agentgateway@example-project.iam.gserviceaccount.com --project example-project \
  --role=roles/iam.workloadIdentityUser \
  --member='serviceAccount:example-project.svc.id.goog[default/agentgateway]'
kubectl annotate serviceaccount agentgateway --namespace default \
  iam.gke.io/gcp-service-account=agentgateway@example-project.iam.gserviceaccount.com
```

See [GKE service-account linking](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#kubernetes-sa-to-iam)
for enabling the cluster and node-pool prerequisites. Outside GKE, configure Google
credentials for the pod before deploying; the manifest alone does not create them.

Keep the database URL and UI client secret in a Kubernetes Secret. Replace the file
placeholders with local files containing the values:

```sh
kubectl create configmap agentgateway-config --namespace default \
  --from-file=config.yaml --from-file=catalog.json
kubectl create secret generic agentgateway-secrets --namespace default \
  --from-file=DB_URL=DB_URL_FILE \
  --from-file=OIDC_CLIENT_SECRET=OIDC_CLIENT_SECRET_FILE
```

The combined ConfigMap data must fit within 1 MiB. If necessary, regenerate the catalog
with `agctl catalog import --source models.dev --providers SOURCE_PROVIDER_IDS --out catalog.json`,
using the models.dev provider IDs that contain your Vertex models, or prune unneeded
models while preserving the catalog structure. Check all routed models still have prices.

Save the following as `deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentgateway
spec:
  replicas: 2
  selector:
    matchLabels:
      app: agentgateway
  template:
    metadata:
      labels:
        app: agentgateway
    spec:
      # Vertex AI credentials: on GKE, bind this to a Google service account with
      # roles/aiplatform.user through Workload Identity. Elsewhere, mount a key
      # file and set GOOGLE_APPLICATION_CREDENTIALS.
      serviceAccountName: agentgateway
      containers:
      - name: agentgateway
        image: cr.agentgateway.dev/agentgateway:v1.6.0
        args: ["-f", "/config/config.yaml"]
        ports:
        - name: http
          containerPort: 4000
        env:
        - name: PUBLIC_URL
          value: https://agw.example.com
        envFrom:
        - secretRef:
            name: agentgateway-secrets
        readinessProbe:
          httpGet:
            path: /healthz/ready
            port: 15021
          periodSeconds: 10
        startupProbe:
          httpGet:
            path: /healthz/ready
            port: 15021
          periodSeconds: 1
          failureThreshold: 60
        volumeMounts:
        - name: config
          mountPath: /config
          readOnly: true
        - name: tmp
          mountPath: /tmp
      volumes:
      - name: config
        configMap:
          name: agentgateway-config
      - name: tmp
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: agentgateway
spec:
  selector:
    app: agentgateway
  ports:
  - name: http
    port: 80
    targetPort: http
```

Apply it and wait for readiness:

```sh
kubectl apply --namespace default -f deployment.yaml
kubectl rollout status deployment/agentgateway --namespace default
```

Put an Ingress or Gateway that terminates TLS for the `PUBLIC_URL` host in front of the
Service. The pods carry no state of their own, so any number of replicas share the
budgets through the database.

The `agentgateway-standalone` Helm chart deploys the same thing. Put the config under its
`config` value and the three variables under `extraEnv`:

```sh
helm install agentgateway oci://cr.agentgateway.dev/charts/agentgateway-standalone \
  --version v1.6.0 -f values.yaml
```

The chart's ConfigMap only holds `config.yaml`, so add `catalog.json` with
`extraVolumes` and `extraVolumeMounts`.

## Verify the deployment

Run the [minimal guide's request checks](agent-gateway.md#verify-access) against your
HTTPS `PUBLIC_URL`, changing the request model to `claude-haiku-4-5` so it matches this
recipe's `claude-haiku-*` route. Test with both an allowed Google user and an allowed
Entra user, and check the gateway log as well as the HTTP response.

| Check | Expected result |
|---|---|
| Missing, expired or wrong-audience bearer token | Rejected before an upstream model request. |
| Google account outside the configured `hd` domains, or an untrusted Entra tenant | Rejected even with a valid signature. |
| Allowed organization, but address absent from the roster | Rejected by the API-key lookup. |
| Client-supplied `x-api-key` naming another person | Overwritten; attribution and budget stay with the verified caller. |
| Listed user with a valid token | Model response and a log entry for that user and group. |
| UI opened without a session | Google sign-in; an account outside the allowed Workspace domains is denied. |
| Small request to each routed model | Nonzero recorded cost at the intended rate; fix catalog gaps before relying on budgets. |

In a separate test deployment, temporarily give a test user a small cap and confirm that
the request after the cap is crossed is blocked. The request crossing the limit may
finish. Restart the deployment and repeat with multiple replicas to verify the shared
database preserves usage. Restore the intended budget before admitting normal traffic.

For Cloud Run, distinguish gateway responses from Google frontend IAM denials. For
Kubernetes, use `kubectl logs deployment/agentgateway --namespace default` and
`kubectl describe pod --namespace default POD_NAME` if readiness or credential discovery
fails.
