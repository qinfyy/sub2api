package repository

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/migrate"
)

// These non-ID references exist in the SQL migrations, but cannot be expressed
// by the current Ent edges. Include them in SQLite's desired schema so a later
// Ent table rebuild does not remove the constraints restored by migration 014.
func createSQLiteEntSchema(ctx context.Context, client *ent.Client) error {
	tables, err := schema.CopyTables(migrate.Tables)
	if err != nil {
		return err
	}
	byName := make(map[string]*schema.Table, len(tables))
	for _, table := range tables {
		byName[table.Name] = table
	}
	jobs := byName["batch_image_jobs"]
	if jobs == nil {
		return fmt.Errorf("missing Ent batch_image_jobs schema")
	}
	batchID, ok := jobs.Column("batch_id")
	if !ok {
		return fmt.Errorf("missing Ent batch_id column")
	}
	for _, name := range []string{"batch_image_items", "batch_image_events"} {
		table := byName[name]
		if table == nil {
			return fmt.Errorf("missing Ent %s schema", name)
		}
		jobID, ok := table.Column("job_id")
		if !ok {
			return fmt.Errorf("missing Ent %s.job_id column", name)
		}
		table.AddForeignKey(&schema.ForeignKey{Symbol: name + "_job_id_fkey", Columns: []*schema.Column{jobID}, RefTable: jobs, RefColumns: []*schema.Column{batchID}, OnDelete: schema.Cascade})
	}
	return migrate.Create(ctx, client.Schema, tables)
}
