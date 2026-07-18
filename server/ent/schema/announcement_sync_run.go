package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type AnnouncementSyncRun struct{ ent.Schema }

func (AnnouncementSyncRun) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "announcement_sync_runs"}}
}

func (AnnouncementSyncRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").SchemaType(map[string]string{dialect.Postgres: "uuid"}).Immutable(),
		field.String("site_id").SchemaType(map[string]string{dialect.Postgres: "uuid"}).Immutable(),
		field.Enum("status").Values("running", "success", "partial", "failed"),
		field.Int("added_count").NonNegative().Default(0),
		field.String("message").Default(""),
		field.Time("started_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("finished_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Optional().Nillable(),
		field.String("request_id"),
	}
}

func (AnnouncementSyncRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("site", Site.Type).Ref("announcement_sync_runs").Field("site_id").Unique().Required().Immutable(),
	}
}
