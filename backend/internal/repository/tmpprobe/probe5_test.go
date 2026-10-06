package tmpprobe

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	_ "modernc.org/sqlite"
)

func TestProbeModerncTimeBinding(t *testing.T) {
	db, err := sql.Open("sqlite", "file:probe5?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	v := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	for _, tbl := range []string{"a", "b", "c", "d"} {
		typ := "TIMESTAMP"
		switch tbl {
		case "b":
			typ = "DATETIME"
		case "c":
			typ = "TEXT"
		case "d":
			typ = ""
		}
		if _, err := db.Exec("CREATE TABLE " + tbl + " (ts " + typ + ")"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO "+tbl+" (ts) VALUES (?)", v); err != nil {
			t.Fatal(err)
		}
		var q string
		if err := db.QueryRow("SELECT quote(ts) FROM " + tbl).Scan(&q); err != nil {
			t.Fatal(err)
		}
		t.Logf("table %-2s stored=%s", tbl, q)
	}

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := dbent.NewClient(dbent.Driver(drv))
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Logf("ent schema: %v", err)
		return
	}
	if _, err := client.Group.Create().SetName("p5").SetCreatedAt(v).SetUpdatedAt(v).Save(context.Background()); err != nil {
		t.Logf("ent insert: %v", err)
		return
	}
	var q string
	if err := db.QueryRow("SELECT quote(created_at) FROM groups LIMIT 1").Scan(&q); err != nil {
		t.Fatal(err)
	}
	t.Logf("ent group.created_at stored=%s", q)

	exprs := []string{
		"created_at",
		"replace(created_at,'Z','')",
		"substr(created_at,1,19)",
		"replace(substr(created_at,1,19),'T',' ')",
	}
	for _, e := range exprs {
		var sv, uv, dv any
		_ = db.QueryRow("SELECT " + e + ", unixepoch(" + e + "), datetime(" + e + ") FROM groups LIMIT 1").Scan(&sv, &uv, &dv)
		t.Logf("%-46s => %v | %v | %v", e, sv, uv, dv)
	}

	for _, cut := range []string{"'2026-01-02T08:00:00Z'", "'2026-01-02 08:00:00'"} {
		var n int
		err := db.QueryRow("SELECT COUNT(*) FROM groups WHERE unixepoch(substr(created_at,1,19)) + 5*3600 <= unixepoch(" + cut + ")").Scan(&n)
		t.Logf("window cut=%s => %d err=%v", cut, n, err)
	}
}
