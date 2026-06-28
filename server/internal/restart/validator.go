package restart

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// authProbe is the registry slice the validator needs.
type authProbe interface {
	AllWithCapability(capability string) []plugin.Entry
}

// AuthProviderValidator refuses a restart that would brick boot: the boot
// fatal-safety check aborts startup when a configured auth_provider is unhealthy,
// so if any running auth_provider is currently unhealthy we must not restart.
type AuthProviderValidator struct{ reg authProbe }

func NewAuthProviderValidator(reg authProbe) *AuthProviderValidator {
	return &AuthProviderValidator{reg: reg}
}

func (v *AuthProviderValidator) Validate(_ context.Context) error {
	for _, e := range v.reg.AllWithCapability(plugin.CapAuthProvider) {
		if !e.Healthy() {
			return fmt.Errorf("auth_provider plugin %q is unhealthy — restart would lock out auth", e.Descriptor.ID)
		}
	}
	return nil
}
