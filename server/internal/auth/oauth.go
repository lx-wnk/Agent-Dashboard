package auth

import "context"

// OAuthProvider defines the interface for OAuth provider implementations.
type OAuthProvider interface {
	// BuildAuthURL returns the authorization URL for the OAuth flow.
	// It returns an error if the URL cannot be constructed (e.g. plugin unavailable).
	BuildAuthURL(ctx context.Context, state, redirectURI string) (string, error)

	// ExchangeCode exchanges an OAuth authorization code for an access token.
	ExchangeCode(ctx context.Context, code, redirectURI string) (string, error)

	// GetUser fetches the user profile for the given access token.
	GetUser(ctx context.Context, accessToken string) (*OAuthUserProfile, error)
}

// OAuthUserProfile holds the provider-agnostic user profile data.
type OAuthUserProfile struct {
	ID          string
	Login       string
	DisplayName string
	AvatarURL   string
}
