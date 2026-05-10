// server/internal/db/ent/schema/user.go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// User holds the schema definition for the User entity.
type User struct{ ent.Schema }

// Fields of the User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id"),
		field.String("github_login"),
		field.String("display_name").Optional(),
		field.String("avatar_url").Optional(),
		field.Bool("is_admin").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_login_at").Optional().Nillable(),
	}
}
