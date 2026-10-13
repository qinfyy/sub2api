package repository

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

// Explicit developer-only export. Production never derives its schema from Ent
// at startup. This captures the pre-v1 experimental schema for review, not data
// from an existing deployment. Run once when establishing the reviewed baseline.
func TestExportSQLiteBaseline(t *testing.T) {
	if os.Getenv("SUB2API_EXPORT_SQLITE_BASELINE") != "1" {
		t.Skip("developer-only schema export")
	}
	db := newSQLiteTestDB(t)
	ctx := context.Background()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	require.NoError(t, createSQLiteEntSchema(ctx, client))
	require.NoError(t, initSQLiteKVSchema(ctx, db))
	require.NoError(t, applySQLiteMigrationsFS(ctx, db))
	var script strings.Builder
	script.WriteString("-- Reviewed SQLite v1 baseline. Generated from the schema and experimental\n-- migrations at the Ent backend refactor boundary. Do not regenerate in production.\n\n")
	rows, err := db.Query(`SELECT name, type, sql FROM sqlite_schema WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' AND name NOT IN ('ent_schema_versions','atlas_schema_revisions') ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END, name`)
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var name, kind, ddl string
		require.NoError(t, rows.Scan(&name, &kind, &ddl))
		script.WriteString(ddl + ";\n\n")
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	for _, name := range tables {
		rows, err := db.Query(`SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`)
		require.NoError(t, err)
		columns, err := rows.Columns()
		require.NoError(t, err)
		for rows.Next() {
			values := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}
			require.NoError(t, rows.Scan(ptrs...))
			encoded := make([]string, len(values))
			for i, value := range values {
				if value != nil && (columns[i] == "created_at" || columns[i] == "updated_at" || columns[i] == "computed_at") {
					encoded[i] = "s2a_utc_now()"
					continue
				}
				switch v := value.(type) {
				case nil:
					encoded[i] = "NULL"
				case string:
					encoded[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
				case []byte:
					encoded[i] = "X'" + hex.EncodeToString(v) + "'"
				case int64:
					encoded[i] = strconv.FormatInt(v, 10)
				case float64:
					encoded[i] = strconv.FormatFloat(v, 'g', -1, 64)
				case bool:
					encoded[i] = "0"
					if v {
						encoded[i] = "1"
					}
				default:
					t.Fatalf("unsupported seed type %T in %s", v, name)
				}
			}
			fmt.Fprintf(&script, "INSERT INTO %s VALUES(%s);\n", name, strings.Join(encoded, ","))
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	output := filepath.Join("..", "..", "migrations", "sqlite_v1", "001_baseline.sql")
	require.NoError(t, os.MkdirAll(filepath.Dir(output), 0755))
	require.NoError(t, os.WriteFile(output, []byte(script.String()), 0644))
}
