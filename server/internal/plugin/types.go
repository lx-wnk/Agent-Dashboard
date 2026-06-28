// Package plugin provides runtime plugin discovery and lifecycle management.
package plugin

// Descriptor is read from plugin.json in each plugin directory.
type Descriptor struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	// Addr is the HTTP address the plugin listens on (e.g. "127.0.0.1:13200").
	Addr string `json:"addr"`
	// Command is the executable + args to start the plugin process.
	// If empty, the plugin is expected to already be running.
	Command []string `json:"command"`
	// Env lists env var names the plugin reads from the parent environment.
	Env         []string       `json:"env"`
	Slots       []SlotBinding  `json:"slots"`
	Settings    []SettingField `json:"settings"`
	Lifecycle   LifecycleHooks `json:"lifecycle"`
	Permissions []string       `json:"permissions"`
}

// SlotBinding declares that the plugin contributes UI into a named host slot.
// Mode is "override" (replace) or "extend" (wrap, receiving the parent). Higher
// Priority renders first. Consumed by the frontend (SP4).
type SlotBinding struct {
	Slot     string `json:"slot"`
	Priority int    `json:"priority"`
	Mode     string `json:"mode"`
}

// SettingField declares one configurable setting. Secret fields are encrypted at
// rest and masked in the API.
type SettingField struct {
	Key    string   `json:"key"`
	Type   string   `json:"type"` // string|url|int|bool|enum
	Label  string   `json:"label"`
	Secret bool     `json:"secret"`
	Enum   []string `json:"enum,omitempty"`
}

// LifecycleHooks are optional HTTP paths (on the plugin's Addr) invoked on state
// transitions. An empty path means the transition runs without a hook.
type LifecycleHooks struct {
	Install     string `json:"install"`
	PostInstall string `json:"postInstall"`
	Activate    string `json:"activate"`
	Deactivate  string `json:"deactivate"`
	Update      string `json:"update"`
	Uninstall   string `json:"uninstall"`
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
	// CapUIExtension marks a plugin that contributes frontend UI into named slots
	// via a ui-manifest.json + per-slot JS modules served by the plugin proxy.
	CapUIExtension = "ui_extension"
)
