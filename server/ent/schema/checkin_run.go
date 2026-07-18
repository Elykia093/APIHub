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

type CheckinRun struct{ ent.Schema }

func (CheckinRun) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "checkin_runs"}}
}

func (CheckinRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").SchemaType(map[string]string{dialect.Postgres: "uuid"}).Immutable(),
		field.String("site_id").SchemaType(map[string]string{dialect.Postgres: "uuid"}).Immutable(),
		field.Time("local_date").SchemaType(map[string]string{dialect.Postgres: "date"}).Immutable(),
		field.Enum("status").Values("running", "success", "already_checked", "manual_required", "failed", "skipped"),
		field.Int64("reward_value").Optional().Nillable(),
		field.String("message").Default(""),
		field.String("error_code").MaxLen(64).Optional().Nillable(),
		field.Int("attempt_count").Positive().Default(1),
		field.Time("started_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("finished_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Optional().Nillable(),
		field.String("request_id"),
	}
}

func (CheckinRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("site", Site.Type).Ref("checkin_runs").Field("site_id").Unique().Required().Immutable(),
	}
}

func (CheckinRun) Indexes() []ent.Index {
	return []ent.Index{index.Fields("site_id", "local_date").Unique()}
}
