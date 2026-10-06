package tmpprobe

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func open2(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:probe2?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestProbeTypes(t *testing.T) {
	db := open2(t)
	if _, err := db.Exec(`CREATE TABLE n (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO n VALUES ('{"i":1}'),('{"r":1.5}'),('{"s":"x"}'),('{"b":true}'),('{"bf":false}'),('{"nul":null}'),('{"a":[1]}'),('{"o":{}}'),('{}')`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT v, json_type(v,'$.i'),json_type(v,'$.r'),json_type(v,'$.s'),json_type(v,'$.b'),json_type(v,'$.bf'),json_type(v,'$.nul'),json_type(v,'$.a'),json_type(v,'$.o'),json_type(v,'$.zz') FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var src string
		var a, b, c, d, e, f, g, h, i any
		if err := rows.Scan(&src, &a, &b, &c, &d, &e, &f, &g, &h, &i); err != nil {
			t.Fatal(err)
		}
	t.Logf("%-14s int=%-7v real=%-6v str=%-6v bool=%-6v boolf=%-6v null=%-6v arr=%-6v obj=%-6v miss=%v", src, a, b, c, d, e, f, g, h, i)
	}
}

func TestProbeTimeStorage(t *testing.T) {
	db := open2(t)
	if _, err := db.Exec(`CREATE TABLE tw (id INTEGER PRIMARY KEY, w TIMESTAMP, s TEXT)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.Exec(`INSERT INTO tw (id,w,s) VALUES (1,?,?)`, now.Add(-10*time.Hour), "2020-01-02T03:04:05.123456Z"); err != nil {
		t.Fatal(err)
	}

	var typ, rawW, rawS string
	if err := db.QueryRow(`SELECT typeof(w), w, s FROM tw WHERE id=1`).Scan(&typ, &rawW, &rawS); err != nil {
		t.Fatal(err)
	}
	t.Logf("typeof=%s raw_w=%v raw_s=%v", typ, rawW, rawS)

	for _, tc := range []struct {
		label string
		q     string
		args  []any
	}{
		{"raw <= param(time)", `SELECT id FROM tw WHERE w <= ?`, []any{now.Add(-5 * time.Hour)}},
		{"raw <= strftime Z", `SELECT id FROM tw WHERE w <= strftime('%Y-%m-%dT%H:%M:%SZ','now','-5 hours')`, nil},
		{"strftime(w) <= strftime", `SELECT id FROM tw WHERE strftime('%Y-%m-%dT%H:%M:%SZ', w) <= strftime('%Y-%m-%dT%H:%M:%SZ','now','-5 hours')`, nil},
		{"unixepoch compare", `SELECT id FROM tw WHERE unixepoch(w) <= unixepoch('now','-5 hours')`, nil},
		{"julianday compare", `SELECT id FROM tw WHERE julianday(w) <= julianday('now','-5 hours')`, nil},
		{"w <= datetime now", `SELECT id FROM tw WHERE w <= datetime('now','-5 hours')`, nil},
	} {
		var got int
		err := db.QueryRow(tc.q, tc.args...).Scan(&got)
		t.Logf("%-28s => %v (err=%v)", tc.label, got, err)
	}

	if _, err := db.Exec(`INSERT INTO tw (id,s) VALUES (2,'2020-06-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM tw WHERE s <= strftime('%Y-%m-%dT%H:%M:%SZ','now','-5 hours')`).Scan(&n)
	t.Logf("ISO text compare => %d err=%v", n, err)

	for _, f := range []string{`%Y-%m-%dT%H:%M:%S.%fZ`, `%Y-%m-%dT%H:%M:%fZ`, `%Y-%m-%dT%H:%M:%SZ`} {
		var v string
		_ = db.QueryRow(`SELECT strftime(?, 'now')`, f).Scan(&v)
		t.Logf("fmt %-26s => %s", f, v)
	}

	var v string
	_ = db.QueryRow(`SELECT strftime('%Y-%m-%dT%H:%M:%S.', 'now') || substr(strftime('%f','now'),4) || 'Z'`).Scan(&v)
	t.Logf("ms RFC3339 => %s", v)

	var v2 string
	err = db.QueryRow(`SELECT strftime('%Y-%m-%dT%H:%M:%SZ', w) FROM tw WHERE id=1`).Scan(&v2)
	t.Logf("parse stored ts => %q err=%v", v2, err)

	var v3 string
	err = db.QueryRow(`SELECT datetime(w) FROM tw WHERE id=1`).Scan(&v3)
	t.Logf("datetime(stored ts) => %q err=%v", v3, err)

	var v4 any
	err = db.QueryRow(`SELECT unixepoch(w) FROM tw WHERE id=1`).Scan(&v4)
	t.Logf("unixepoch(stored ts) => %v err=%v", v4, err)

	// ent stores timestamps how? check quoted
	rows, _ := db.Query(`SELECT quote(w), quote(s) FROM tw`)
	for rows.Next() {
		var a, b string
		_ = rows.Scan(&a, &b)
		t.Logf("quoted w=%s s=%s", a, b)
	}
	rows.Close()
}
