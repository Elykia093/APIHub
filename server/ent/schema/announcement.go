package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Announcement struct{ ent.Schema }

func (Announcement) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "announcements"}}
}

func (Announcement) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").SchemaType(map[string]string{dialect.Postgres: "uuid"}).Immutable(),
		field.String("site_id").SchemaType(map[string]string{dialect.Postgres: "uuid"}).Immutable(),
		field.Enum("source").Values("status", "notice"),
		field.String("fingerprint"),
		field.String("content"),
		field.String("kind").MaxLen(32).Default("default"),
		field.String("extra").Optional().Nillable(),
		field.Time("published_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Optional().Nillable(),
		field.Time("first_seen_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Immutable(),
		field.Time("last_seen_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("read_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Optional().Nillable(),
	}
}

func (Announcement) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("site", Site.Type).Ref("announcements").Field("site_id").Unique().Required().Immutable(),
	}
}

func (Announcement) Indexes() []ent.Index {
	return []ent.Index{index.Fields("site_id", "fingerprint").Unique()}
}
