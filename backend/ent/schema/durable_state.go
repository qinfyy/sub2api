package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DurableState stores opaque domain state in the primary database. Cache
// eviction and the choice of Redis/memory must not determine its lifetime.
type DurableState struct{ ent.Schema }

func (DurableState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "kv_store"}}
}

func (DurableState) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("key").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Bytes("value").SchemaType(map[string]string{dialect.Postgres: "bytea", dialect.SQLite: "blob"}),
		field.Int64("expires_at").Optional().Nillable(),
		field.String("updated_at").DefaultFunc(func() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z") }),
	}
}

func (DurableState) Indexes() []ent.Index {
	return []ent.Index{index.Fields("expires_at").StorageKey("idx_kv_store_expires_at")}
}
