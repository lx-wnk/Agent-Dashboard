// Package envsec holds the canonical set of server-secret environment
// variable names that must never be forwarded to a spawned Claude agent or
// plugin process. Consumed by the interactive spawner, the pipeline
// spawner, and the plugin registry so the deny-set is declared once.
package envsec

// DeniedSecretEnvKeys are pure server secrets — consumed only by
// secretbox.go (plugin master key), the JWT signer, config.go's auth
// bypass, and the hooks HMAC. No spawned agent or plugin has any
// legitimate use for them.
var DeniedSecretEnvKeys = map[string]struct{}{
	"DASHBOARD_SECRET_KEY":         {},
	"DASHBOARD_JWT_SECRET":         {},
	"DASHBOARD_AUTH_PLUGIN_SECRET": {},
	"DASHBOARD_HOOKS_SECRET":       {},
}
