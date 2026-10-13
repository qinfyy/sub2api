package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ImageQueueEntry owns the persistent queue claim. The historical table name
// is retained so existing memory-mode queue state does not need rewriting.
type ImageQueueEntry struct{ ent.Schema }

func (ImageQueueEntry) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "memory_batch_queue"}}
}

func (ImageQueueEntry) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("batch_id").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Enum("state").Values("ready", "delayed", "active").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("available_at"),
		field.Int64("heartbeat_at").Default(0),
		field.String("receipt").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
	}
}

func (ImageQueueEntry) Indexes() []ent.Index {
	return []ent.Index{index.Fields("state", "available_at").StorageKey("idx_memory_batch_queue_ready")}
}
