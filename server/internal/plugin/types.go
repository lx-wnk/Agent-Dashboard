// Package plugin provides runtime plugin discovery and lifecycle management.
package plugin

// Descriptor is read from plugin.json in each plugin directory.
type Descriptor struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	// Addr is the HTTP address the plugin listens on (e.g. "127.0.0.1:13200").
	Addr string `json:"addr"`
	// Command is the executable + args to start the plugin process.
	// If empty, the plugin is expected to already be running.
	Command []string `json:"command"`
	// Env lists env var names the plugin reads from the parent environment.
	Env []string `json:"env"`
}

// HasCapability reports whether the plugin declares the given capability.
func (d Descriptor) HasCapability(capability string) bool {
	for _, c := range d.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// Capability constants used in plugin.json.
const (
	CapAuthProvider   = "auth_provider"
	CapRouteExtension = "route_extension"
)
