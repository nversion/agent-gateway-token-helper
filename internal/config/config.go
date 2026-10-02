// Package config merges provider presets, the profile file, AGW_* environment
// variables and command line flags into one Config.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// AppName names the per-user directories and the keychain service.
const AppName = "agw-token"

// Keys lists every setting, in the spelling used for flags and profiles. The
// environment variable for a key is AGW_ plus the key upper-cased with dashes
// replaced by underscores.
var Keys = []string{
	"provider",
	"issuer",
	"tenant",
	"client-id",
	"client-secret",
	"scopes",
	"extra-auth-params",
	"min-valid",
	"redirect-host",
	"redirect-port",
	"callback-path",
	"no-browser",
	"login-timeout",
	"store",
	"output",
}

// portSearchRange is how many ports a "<port>+" redirect port tries, the
// same range gcloud uses.
const portSearchRange = 100

// Config is the fully resolved configuration.
type Config struct {
	Profile      string
	Provider     string
	Issuer       string
	Tenant       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	AuthParams   map[string]string
	MinValid     time.Duration
	RedirectHost string
	RedirectPort int
	// PortSearch is the number of consecutive ports from RedirectPort to try;
	// 1 means only RedirectPort.
	PortSearch   int
	CallbackPath string
	NoBrowser    bool
	LoginTimeout time.Duration
	Store        string
	Output       string
	Dirs         Dirs
}

// Dirs are the per-user directories the helper uses.
type Dirs struct {
	// Config holds config.json.
	Config string
	// Cache holds discovery documents.
	Cache string
	// State holds the file token store and lock files.
	State string
}

// EnvName returns the environment variable for a setting key.
func EnvName(key string) string {
	return "AGW_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
}

// Sources are the inputs Load merges.
type Sources struct {
	// Flags holds only the flags that were set on the command line, so an
	// explicit empty value still overrides lower layers.
	Flags  map[string]string
	Getenv func(string) string
}

// Load resolves the configuration with precedence flag > env > profile >
// preset > default.
func Load(src Sources) (*Config, error) {
	getenv := src.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	dirs, err := ResolveDirs(getenv)
	if err != nil {
		return nil, err
	}

	profile, explicit := src.Flags["profile"], true
	if profile == "" {
		profile = getenv("AGW_PROFILE")
	}
	if profile == "" {
		profile, explicit = "default", false
	}
	profileValues, err := loadProfile(filepath.Join(dirs.Config, "config.json"), profile, explicit)
	if err != nil {
		return nil, err
	}

	lookup := func(key string) (string, bool) {
		if v, ok := src.Flags[key]; ok {
			return v, true
		}
		if v := getenv(EnvName(key)); v != "" {
			return v, true
		}
		v, ok := profileValues[key]
		return v, ok
	}

	provider, ok := lookup("provider")
	if !ok || provider == "" {
		provider = defaults["provider"]
	}
	preset, ok := Presets[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (known: %s)", provider, strings.Join(slices.Sorted(maps.Keys(Presets)), ", "))
	}

	values := make(map[string]string, len(Keys))
	for _, k := range Keys {
		if v, ok := lookup(k); ok {
			values[k] = v
			// Extra auth params add to the preset's instead of replacing
			// them, so e.g. hd=example.com keeps Google's access_type.
			if p := preset[k]; k == "extra-auth-params" && p != "" {
				values[k] = p + "," + v
			}
		} else if v, ok := preset[k]; ok {
			values[k] = v
		} else {
			values[k] = defaults[k]
		}
	}
	values["provider"] = provider

	c, err := parse(values)
	if err != nil {
		return nil, err
	}
	c.Profile = profile
	c.Dirs = dirs
	return c, nil
}

func parse(v map[string]string) (*Config, error) {
	c := &Config{
		Provider:     v["provider"],
		Tenant:       v["tenant"],
		ClientID:     v["client-id"],
		ClientSecret: v["client-secret"],
		Scopes:       splitList(v["scopes"]),
		RedirectHost: v["redirect-host"],
		CallbackPath: v["callback-path"],
		Store:        v["store"],
		Output:       v["output"],
	}

	c.Issuer = strings.TrimRight(v["issuer"], "/")
	if strings.Contains(c.Issuer, "{tenant}") {
		if c.Tenant == "" {
			return nil, errors.New("issuer " + c.Issuer + " needs --tenant")
		}
		c.Issuer = strings.ReplaceAll(c.Issuer, "{tenant}", url.PathEscape(c.Tenant))
	}
	if c.Issuer == "" {
		return nil, errors.New("--issuer is required (or use --provider google|entra)")
	}
	if err := CheckIssuerURL(c.Issuer); err != nil {
		return nil, err
	}
	if c.ClientID == "" {
		return nil, errors.New("--client-id is required")
	}
	if !slices.Contains(c.Scopes, "openid") {
		return nil, errors.New("--scopes must include openid")
	}

	params, err := parseParams(v["extra-auth-params"])
	if err != nil {
		return nil, err
	}
	c.AuthParams = params

	if c.MinValid, err = parseDuration("min-valid", v["min-valid"]); err != nil {
		return nil, err
	}
	if c.LoginTimeout, err = parseDuration("login-timeout", v["login-timeout"]); err != nil {
		return nil, err
	}
	if c.NoBrowser, err = strconv.ParseBool(v["no-browser"]); err != nil {
		return nil, fmt.Errorf("invalid no-browser %q: %w", v["no-browser"], err)
	}

	port, search := v["redirect-port"], 1
	if p, ok := strings.CutSuffix(port, "+"); ok {
		port, search = p, portSearchRange
	}
	if c.RedirectPort, err = strconv.Atoi(port); err != nil || c.RedirectPort < 0 || c.RedirectPort > 65535 {
		return nil, fmt.Errorf("invalid redirect-port %q: want 0-65535, optionally followed by + to try the next ports", v["redirect-port"])
	}
	if c.RedirectPort == 0 {
		search = 1
	}
	c.PortSearch = search

	switch c.RedirectHost {
	case "localhost", "127.0.0.1", "[::1]":
	default:
		return nil, fmt.Errorf("invalid redirect-host %q: want localhost, 127.0.0.1 or [::1]", c.RedirectHost)
	}
	if c.CallbackPath != "" && !strings.HasPrefix(c.CallbackPath, "/") {
		return nil, fmt.Errorf("invalid callback-path %q: must start with /", c.CallbackPath)
	}
	switch c.Store {
	case "auto", "file":
	default:
		return nil, fmt.Errorf("invalid store %q: want auto or file", c.Store)
	}
	switch c.Output {
	case "raw", "json", "headers", "decoded":
	default:
		return nil, fmt.Errorf("invalid output %q: want raw, json, headers or decoded", c.Output)
	}
	return c, nil
}

// CheckIssuerURL requires https, except for loopback hosts so a local test
// provider can be used without custom CA roots.
func CheckIssuerURL(issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil {
		return fmt.Errorf("invalid issuer %q: %w", issuer, err)
	}
	switch {
	case u.Scheme == "https" && u.Host != "":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	default:
		return fmt.Errorf("invalid issuer %q: must be an https URL (http is only allowed for loopback hosts)", issuer)
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Key identifies the token cache entry for this configuration. Anything that
// changes which token the provider would issue is part of it, so a config
// change never returns a stale token.
func (c *Config) Key() string {
	params := make([]string, 0, len(c.AuthParams))
	for k, v := range c.AuthParams {
		params = append(params, k+"="+v)
	}
	slices.Sort(params)
	scopes := slices.Sorted(slices.Values(c.Scopes))
	sum := sha256.Sum256([]byte(strings.Join([]string{
		c.Issuer,
		c.ClientID,
		strings.Join(scopes, " "),
		strings.Join(params, "&"),
	}, "\n")))
	return hex.EncodeToString(sum[:16])
}

// ResolveDirs returns the per-user directories. AGW_HOME puts all of them in
// one directory, which tests use for isolation.
func ResolveDirs(getenv func(string) string) (Dirs, error) {
	if home := getenv("AGW_HOME"); home != "" {
		return Dirs{Config: home, Cache: filepath.Join(home, "cache"), State: home}, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("locating user config dir: %w", err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("locating user cache dir: %w", err)
	}
	d := Dirs{
		Config: filepath.Join(configDir, AppName),
		Cache:  filepath.Join(cacheDir, AppName),
	}
	d.State = d.Config
	if runtime.GOOS == "windows" {
		// %LOCALAPPDATA% rather than the roaming %APPDATA%: tokens and
		// locks belong to this machine.
		d.State = d.Cache
	}
	return d, nil
}

// fileConfig is the layout of config.json:
//
//	{"profiles": {"default": {"provider": "entra", "tenant": "contoso.com"}}}
type fileConfig struct {
	Profiles map[string]map[string]any `json:"profiles"`
}

func loadProfile(path, name string, explicit bool) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if explicit {
			return nil, fmt.Errorf("profile %q not found: %s does not exist", name, path)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	raw, ok := fc.Profiles[name]
	if !ok {
		if explicit {
			return nil, fmt.Errorf("profile %q not found in %s", name, path)
		}
		return nil, nil
	}

	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if !slices.Contains(Keys, k) {
			return nil, fmt.Errorf("%s: profile %q: unknown setting %q", path, name, k)
		}
		s, err := profileValue(v)
		if err != nil {
			return nil, fmt.Errorf("%s: profile %q: %s: %w", path, name, k, err)
		}
		out[k] = s
	}
	return out, nil
}

// profileValue flattens a JSON value to the flag syntax: arrays become
// comma-separated lists and objects k=v pairs.
func profileValue(v any) (string, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case bool, float64:
		return fmt.Sprint(v), nil
	case []any:
		parts := make([]string, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return "", errors.New("arrays may only contain strings")
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), nil
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			s, ok := v[k].(string)
			if !ok {
				return "", errors.New("objects may only contain string values")
			}
			parts = append(parts, k+"="+s)
		}
		return strings.Join(parts, ","), nil
	default:
		return "", fmt.Errorf("unsupported value %v", v)
	}
}

func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
}

func parseParams(s string) (map[string]string, error) {
	out := map[string]string{}
	for kv := range strings.SplitSeq(s, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid extra-auth-params entry %q: want key=value", kv)
		}
		// Later entries win; an empty value removes the key, which is how a
		// preset parameter is turned off (prompt=).
		if v == "" {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out, nil
}

func parseDuration(key, s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid %s %q: want a duration like 5m", key, s)
	}
	return d, nil
}
