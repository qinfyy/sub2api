package tmpprobe

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func open4(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:probe4?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestProbeTSFormat(t *testing.T) {
	db := open4(t)
	if _, err := db.Exec(`CREATE TABLE w (id INTEGER PRIMARY KEY, ts TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	vals := []time.Time{
		time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 5, 120000000, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 5, 100000000, time.UTC),
	}
	for i, v := range vals {
		if _, err := db.Exec(`INSERT INTO w (id, ts) VALUES (?, ?)`, i+1, v); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := db.Query(`SELECT id, quote(ts) FROM w ORDER BY id`)
	for rows.Next() {
		var id int
		var q string
		_ = rows.Scan(&id, &q)
		t.Logf("id=%d stored=%s", id, q)
	}
	rows.Close()

	rows2, _ := db.Query(`SELECT id FROM w ORDER BY ts`)
	for rows2.Next() {
		var id int
		_ = rows2.Scan(&id)
		t.Logf("ORDER BY ts -> %d", id)
	}
	rows2.Close()

	for _, tc := range []struct {
		label string
		q     string
	}{
		{"strftime plain", `SELECT strftime('%Y-%m-%dT%H:%M:%SZ', ts) FROM w WHERE id=1`},
		{"strftime replace Z", `SELECT strftime('%Y-%m-%dT%H:%M:%SZ', replace(ts,'Z','')) FROM w WHERE id=1`},
		{"unixepoch plain", `SELECT unixepoch(ts) FROM w WHERE id=1`},
		{"unixepoch replace", `SELECT unixepoch(replace(ts,'Z','')) FROM w WHERE id=1`},
		{"datetime plain", `SELECT datetime(ts) FROM w WHERE id=1`},
		{"datetime replace", `SELECT datetime(replace(ts,'Z','')) FROM w WHERE id=1`},
		{"datetime replace +5h", `SELECT datetime(replace(ts,'Z',''), '+5 hours') FROM w WHERE id=1`},
		{"numeric window", `SELECT unixepoch(replace(ts,'Z','')) + 5*3600 FROM w WHERE id=1`},
		{"start of day", `SELECT strftime('%Y-%m-%dT%H:%M:%SZ','now','start of day')`},
	} {
		var got any
		err := db.QueryRow(tc.q).Scan(&got)
		t.Logf("%-24s => %v (err=%v)", tc.label, got, err)
	}

	cut := time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC)
	var cnt int
	err := db.QueryRow(`SELECT COUNT(*) FROM w WHERE unixepoch(replace(ts,'Z','')) + 5*3600 <= unixepoch(replace(?,'Z',''))`, cut).Scan(&cnt)
	t.Logf("numeric-window (cut=%v) => %d err=%v", cut, cnt, err)

	eq := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	err = db.QueryRow(`SELECT COUNT(*) FROM w WHERE unixepoch(replace(ts,'Z','')) + 5*3600 <= unixepoch(replace(?,'Z',''))`, eq).Scan(&cnt)
	t.Logf("equality-boundary => %d err=%v", cnt, err)
}

func TestProbeMisc(t *testing.T) {
	db := open4(t)
	if _, err := db.Exec(`CREATE TABLE k (id INTEGER PRIMARY KEY, n TEXT, v INTEGER, c INTEGER)`); err != nil {
		t.Fatal(err)
	}
	var out string
	if err := db.QueryRow(`INSERT INTO k (id,n,v) VALUES (1,'a',1)
		ON CONFLICT(id) DO UPDATE SET v = v + excluded.v RETURNING v`).Scan(&out); err != nil {
		t.Logf("upsert returning ERR %v", err)
	} else {
		t.Logf("upsert returning => %s", out)
	}

	if _, err := db.Exec(`CREATE TABLE ev (t TIMESTAMP, g INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ev VALUES ('2024-05-05T13:47:11Z',1),('2024-05-05T13:12:03Z',1),('2024-05-05T14:03:00Z',2)`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT strftime('%Y-%m-%dT%H:00:00Z', replace(t,'Z','')) AS b, g, COUNT(*) FROM ev GROUP BY b, g ORDER BY b`)
	if err != nil {
		t.Logf("bucket group ERR %v", err)
	} else {
		for rows.Next() {
			var b string
			var g, c int
			_ = rows.Scan(&b, &g, &c)
			t.Logf("bucket=%s g=%d c=%d", b, g, c)
		}
		rows.Close()
	}

	var r1 string
	_ = db.QueryRow(`SELECT json_set('{}','$.q', 1 + 2.0)`).Scan(&r1)
	t.Logf("json_set numeric => %s", r1)
	var r2 string
	_ = db.QueryRow(`SELECT json_set('{}','$.q', CAST(1+2.0 AS TEXT))`).Scan(&r2)
	t.Logf("json_set text-num => %s", r2)

	rows3, err := db.Query(`SELECT value FROM json_each('[10,20,30]')`)
	if err != nil {
		t.Logf("json_each ERR %v", err)
	} else {
		for rows3.Next() {
			var v int
			_ = rows3.Scan(&v)
			t.Logf("json_each => %d", v)
		}
		rows3.Close()
	}

	if _, err := db.Exec(`CREATE TABLE mr (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatal(err)
	}
	rows4, err := db.Query(`INSERT INTO mr (id,v) VALUES (1,'a'),(2,'b') RETURNING id`)
	if err != nil {
		t.Logf("multi returning ERR %v", err)
	} else {
		for rows4.Next() {
			var id int
			_ = rows4.Scan(&id)
			t.Logf("multi returning => %d", id)
		}
		rows4.Close()
	}

	var v string
	_ = db.QueryRow(`SELECT strftime('%Y-%m-%dT%H:%M:%S.','now') || substr(strftime('%f','now'),4) || 'Z'`).Scan(&v)
	t.Logf("now RFC3339ms => %s", v)

	var v2 string
	_ = db.QueryRow(`SELECT json_extract('{"a.b":1}','$."a.b"')`).Scan(&v2)
	t.Logf("json path quoted key => %q", v2)

	var v3 string
	_ = db.QueryRow(`SELECT json_remove('{"a":1,"b":2}','$.a')`).Scan(&v3)
	t.Logf("json_remove => %s", v3)

	var v4 string
	err = db.QueryRow(`UPDATE mr SET v='z' WHERE id=1 RETURNING v`).Scan(&v4)
	t.Logf("update returning => %q err=%v", v4, err)

	// sqlite bool/param typing of json containers
	var v5 string
	_ = db.QueryRow(`SELECT json_type('{"a":true}','$.a')`).Scan(&v5)
	t.Logf("json_type bool => %q", v5)
	var v6 string
	_ = db.QueryRow(`SELECT json_type('{"a":1}','$.a')`).Scan(&v6)
	t.Logf("json_type int => %q", v6)
}
