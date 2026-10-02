package config

// Presets hold the defaults for --provider. Every value can be overridden by
// a profile, an AGW_* environment variable or a flag.
//
// The google and entra presets reuse the public OAuth clients of the gcloud
// SDK (googlecloudsdk/core/config.py) and the Azure CLI
// (azure/cli/core/auth/constants.py). Tokens minted for those clients carry
// the client ID as their audience, which every other gcloud/az user also
// gets, so a gateway accepting them must also restrict on hd/email (Google)
// or tid/oid (Entra) claims.
var Presets = map[string]map[string]string{
	"generic": {},
	"google": {
		"issuer":    "https://accounts.google.com",
		"client-id": "32555940559.apps.googleusercontent.com",
		// Not a secret: Google requires one for installed apps and gcloud
		// ships it in its source as CLOUDSDK_CLIENT_NOTSOSECRET.
		"client-secret": "ZmssLNjJy2998hD4CTg2ejr2",
		// Google rejects offline_access; access_type=offline asks for a
		// refresh token and prompt=consent (as gcloud sends) makes sure one is
		// returned. select_account always shows the account picker, like the
		// entra preset, instead of using the only signed-in account silently.
		"scopes":            "openid email",
		"extra-auth-params": "access_type=offline,prompt=select_account consent",
		// Same as gcloud: http://localhost:<first free port from 8085>/.
		"redirect-port": "8085+",
		"callback-path": "/",
	},
	"entra": {
		"issuer":    "https://login.microsoftonline.com/{tenant}/v2.0",
		"tenant":    "organizations",
		"client-id": "04b07795-8ddb-461a-bbee-02f9e1bf7b46",
		"scopes":    "openid profile email offline_access",
		// Same as az login: always show the account picker instead of silently
		// reusing whichever account the browser is signed in with.
		"extra-auth-params": "prompt=select_account",
		// Same as MSAL: http://localhost:<random port> without a path. Entra
		// ignores the port of loopback redirect URIs but matches the path.
		"redirect-port": "0",
		"callback-path": "",
	},
}

// defaults apply when neither the preset nor any other source sets a value.
var defaults = map[string]string{
	"provider":      "generic",
	"scopes":        "openid email profile offline_access",
	"min-valid":     "5m",
	"redirect-host": "localhost",
	"redirect-port": "0",
	"callback-path": "/callback",
	"no-browser":    "false",
	"login-timeout": "5m",
	"store":         "auto",
	"output":        "raw",
}
