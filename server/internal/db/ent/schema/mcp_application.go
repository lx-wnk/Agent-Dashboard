package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

// CatalogueTool is one entry of a server's tools/list, as last read. The hints
// are the server's own claims and are shown, never trusted.
type CatalogueTool struct {
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
}

type MCPApplication struct{ ent.Schema }

func (MCPApplication) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("resource_id").Unique().Immutable(),
		field.String("server_name").Immutable(),
		field.Bool("attach_all").Default(false),
		field.JSON("required_env", []string{}).
			Default([]string{}).
			Annotations(entsql.Default("[]")),
		field.JSON("catalogue", []CatalogueTool{}).
			Default([]CatalogueTool{}).
			Annotations(entsql.Default("[]")),
		field.String("catalogue_error").Default(""),
		field.Time("catalogue_refreshed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
