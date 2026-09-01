package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AppSetting is a generic key/value store for DB-backed, non-bootstrap
// configuration. The value is an opaque string interpreted by the Go setting
// registry (internal/settings).
type AppSetting struct{ ent.Schema }

func (AppSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("key").Unique(),
		field.String("value"),
		// secret marks value as AES-256-GCM ciphertext produced by
		// internal/secretbox; nonce is that ciphertext's GCM nonce. Both are
		// base64. Kept in their own columns rather than encoded into value,
		// so a plaintext value that happens to look like a marker cannot be
		// misread as ciphertext — the same reason plugin_setting splits them.
		field.Bool("secret").Default(false),
		field.String("nonce").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
