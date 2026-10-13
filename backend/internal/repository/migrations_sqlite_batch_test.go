package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestSQLiteBatchForeignKeyMigrationPreservesDataAndRollsBackOrphans(t *testing.T) {
	for _, orphan := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "orphan"}[orphan], func(t *testing.T) {
			ctx := context.Background()
			db := newSQLiteTestDB(t)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
			require.NoError(t, client.Schema.Create(ctx))
			require.NoError(t, initSQLiteKVSchema(ctx, db))
			files, err := loadSQLiteMigrationFiles(sqliteIncrementalMigrationsFS)
			require.NoError(t, err)
			var early []sqliteMigrationFile
			for _, f := range files {
				if f.name < "014_" {
					early = append(early, f)
				}
			}
			require.NoError(t, applySQLiteMigrationFiles(ctx, db, early))
			_, err = db.Exec(`INSERT INTO batch_image_jobs(batch_id,user_id,provider,model,item_count,created_at,updated_at) VALUES('job',1,'gemini_api','test',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
			require.NoError(t, err)
			job := "job"
			if orphan {
				job = "missing"
			}
			_, err = db.Exec(`INSERT INTO batch_image_items(id,job_id,custom_id,status,created_at) VALUES(91,?,'line-1','success',CURRENT_TIMESTAMP)`, job)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO batch_image_events(id,job_id,event_type,event_hash,created_at) VALUES(92,'job','created','event-1',CURRENT_TIMESTAMP)`)
			require.NoError(t, err)
			err = applySQLiteMigrationFiles(ctx, db, files)
			if orphan {
				require.ErrorContains(t, err, "FOREIGN KEY")
				var count int
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM batch_image_items WHERE id=91 AND job_id='missing'`).Scan(&count))
				require.Equal(t, 1, count)
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM ent_schema_versions WHERE filename LIKE '014_%'`).Scan(&count))
				require.Zero(t, count)
				_, err = db.Exec(`UPDATE batch_image_items SET job_id='job' WHERE id=91`)
				require.NoError(t, err)
				require.NoError(t, applySQLiteMigrationFiles(ctx, db, files))
			} else {
				require.NoError(t, err)
			}
			// Re-running Ent initialization must retain the supplemental foreign keys.
			require.ErrorIs(t, initSQLiteSchema(ctx, client, db), ErrUnsupportedSQLiteSchema)
			for _, table := range []string{"batch_image_items", "batch_image_events"} {
				var count int
				require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
				require.Equal(t, 1, count)
				require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_list(?) WHERE on_delete='CASCADE'", table).Scan(&count))
				require.Equal(t, 1, count)
			}
			_, err = db.Exec(`INSERT INTO batch_image_events(job_id,event_type,created_at) VALUES('missing','bad',CURRENT_TIMESTAMP)`)
			require.ErrorContains(t, err, "FOREIGN KEY")
			_, err = db.Exec(`DELETE FROM batch_image_jobs WHERE batch_id='job'`)
			require.NoError(t, err)
			for _, table := range []string{"batch_image_items", "batch_image_events"} {
				var count int
				require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
				require.Zero(t, count)
			}
		})
	}
}
