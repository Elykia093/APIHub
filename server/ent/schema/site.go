package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Site struct{ ent.Schema }

func (Site) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "sites"}}
}

func (Site) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").SchemaType(map[string]string{dialect.Postgres: "uuid"}).Immutable(),
		field.String("name").MaxLen(80),
		field.String("base_url"),
		field.Enum("adapter").Values("new-api", "sub2api", "zen-api"),
		field.String("user_id").MaxLen(128),
		field.String("access_token_ciphertext"),
		field.Bool("enabled").Default(true),
		field.Bool("checkin_enabled").Default(true),
		field.Bool("announcement_enabled").Default(true),
		field.String("checkin_cron").MaxLen(100),
		field.String("announcement_cron").MaxLen(100),
		field.String("timezone").MaxLen(100),
		field.Int("consecutive_failures").NonNegative().Default(0),
		field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Immutable(),
		field.Time("updated_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (Site) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("checkin_runs", CheckinRun.Type),
		edge.To("announcement_sync_runs", AnnouncementSyncRun.Type),
		edge.To("announcements", Announcement.Type),
	}
}
