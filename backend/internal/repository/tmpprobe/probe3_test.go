package tmpprobe

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
)

// TestProbeEntTimestampFormat 用真实的 ent + modernc sqlite 写入，
// 确认 ent 在 SQLite 下把 time.Time 写成什么文本格式。
func TestProbeEntTimestampFormat(t *testing.T) {
	db, err := sql.Open("sqlite", "file:probe3?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := dbent.NewClient(dbent.Driver(drv))
	defer client.Close()

	if err := client.Schema.Create(context.Background()); err != nil {
	t.Fatalf("schema create: %v", err)
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	g, err := client.Group.Create().
		SetName("probe").
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	t.Logf("ent returned created_at=%v", g.CreatedAt)

	var raw string
	var typ string
	if err := db.QueryRow(`SELECT typeof(created_at), created_at FROM groups WHERE id = ?`, g.ID).Scan(&typ, &raw); err != nil {
		t.Fatal(err)
	}
	t.Logf("stored typeof=%s raw=%q", typ, raw)

	for _, tc := range []struct {
		label string
		q     string
		args  []any
	}{
		{"raw >= param", `SELECT id FROM groups WHERE created_at >= ?`, []any{now}},
		{"raw >= strftime ms Z", `SELECT id FROM groups WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%S.','now')||substr(strftime('%f','now'),4)||'Z'`, nil},
		{"strftime(col)", `SELECT id FROM groups WHERE strftime('%Y-%m-%dT%H:%M:%SZ', created_at) >= '2000-01-01T00:00:00Z'`, nil},
		{"unixepoch(col)", `SELECT id FROM groups WHERE unixepoch(created_at) > 0`, nil},
		{"datetime(col)", `SELECT id FROM groups WHERE datetime(created_at) >= '2000-01-01'`, nil},
		{"col is text?", `SELECT id FROM groups WHERE created_at >= '2020-01-01'`, nil},
	} {
		var got int
		err := db.QueryRow(tc.q, tc.args...).Scan(&got)
		t.Logf("%-28s => %v (err=%v)", tc.label, got, err)
	}

	// 现在验证我们即将写的 SQLite 版计量 SQL
	rows, err := db.Query(`
		UPDATE groups SET updated_at = CASE
			WHEN updated_at IS NOT NULL AND datetime(updated_at, '+5 hours') <= ? THEN ?
			ELSE updated_at END
		WHERE id = $1`, g.ID, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Logf("sqlite variant with $1: %v", err)
	} else {
		rows.Close()
	t.Logf("sqlite variant with $1 executed OK (ent translates for exec? no) ")
	}
	_ = dbgroup.FieldName
}
