package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PluginSetting stores one configurable value for a plugin. Secret values are
// AES-GCM ciphertext (base64) with nonce set; non-secret values are plaintext.
type PluginSetting struct{ ent.Schema }

func (PluginSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("plugin_id"),
		field.String("key"),
		field.String("value").Default(""),
		field.Bool("secret").Default(false),
		field.String("nonce").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PluginSetting) Indexes() []ent.Index {
	return []ent.Index{index.Fields("plugin_id", "key").Unique()}
}
