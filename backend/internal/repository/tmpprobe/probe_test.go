package tmpprobe

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type timeTime = time.Time

func timeNowProbe() time.Time { return time.Now().UTC() }

func TestProbe(t *testing.T) {
	db, err := sql.Open("sqlite", "file:probe?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	must := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Errorf("EXEC %q -> %v", q, err)
		}
	}
	q := func(label, query string, args ...any) {
		rows, err := db.Query(query, args...)
		if err != nil {
			t.Logf("%-46s ERR  %v", label, err)
			return
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		out := ""
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Logf("%-46s SCANERR %v", label, err)
				return
			}
			out += "["
			for i, v := range vals {
				out += fmt.Sprintf("%s=%v(%T) ", cols[i], v, v)
			}
			out += "] "
		}
		t.Logf("%-46s OK   %s", label, out)
	}

	must(`CREATE TABLE t (id INTEGER PRIMARY KEY, extra TEXT, num REAL, s TEXT)`)
	must(`INSERT INTO t (id, extra, num, s) VALUES (1, '{"a":1,"b":{"c":"x"},"arr":[1,2]}', 2.5, 'Hello')`)

	// --- jsonb equivalents
	q("json_type(number)", `SELECT json_type(extra,'$.a') FROM t`)
	q("json_type missing", `SELECT json_type(extra,'$.zz') FROM t`)
	q("json_type nested null", `SELECT json_type(extra,'$.nope') FROM t`)
	q("json_set simple", `SELECT json_set(COALESCE(extra,'{}'),'$.k','v') FROM t`)
	q("json_extract ->> num", `SELECT json_extract(extra,'$.a') FROM t`)
	q("json_extract ->> text", `SELECT json_extract(extra,'$.b.c') FROM t`)
	q("json_remove", `SELECT json_remove(extra,'$.a') FROM t`)
	q("json array_length", `SELECT json_array_length(extra,'$.arr') FROM t`)
	q("json_each", `SELECT value FROM t, json_each(t.extra,'$.arr')`)
	q("json patch merge", `SELECT json_patch(extra,'{"a":9}') FROM t`)

	// --- IS NOT semantics (task says IS DISTINCT FROM -> IS NOT)
	q("a IS NOT b (nulls)", `SELECT 1 IS NOT 1, NULL IS NOT 1, NULL IS NOT NULL`)
	q("json IS NOT", `SELECT (SELECT extra FROM t) IS NOT '{"a":1,"b":{"c":"x"},"arr":[1,2]}'`)
	q("json IS NOT diff", `SELECT (SELECT extra FROM t) IS NOT '{"z":1}'`)

	// --- return cols on UPDATE
	q("UPDATE RETURNING", `UPDATE t SET num = 3 RETURNING num, id`)

	// --- replace 'x' behavior for json_set merge of numbers
	q("json_set number replace", `SELECT json_set('{"a":1}','$.a', 5.0)`)
	q("json set then remove arithmetic", `SELECT json_extract(json_set('{"a":1}','$.b', json_extract(json_set('{"a":1}','$.b',5.0),'$.b')),'$.b')`)

	// --- datetime functions
	q("strftime now", `SELECT strftime('%Y-%m-%dT%H:%M:%fZ','now')`)
	q("strftime start of day", `SELECT strftime('%Y-%m-%dT%H:%M:%fZ','now','start of day')`)
	q("strftime localtime tz", `SELECT strftime('%Y-%m-%dT%H:%M:%fZ','now','localtime')`)
	q("strftime add hours", `SELECT strftime('%Y-%m-%dT%H:%M:%fZ','now','+5 hours')`)
	q("comparison ts text", `SELECT strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 days') < strftime('%Y-%m-%dT%H:%M:%fZ','now')`)

	// --- now() function
	q("NOW() exists?", `SELECT NOW()`)
	q("current_timestamp", `SELECT CURRENT_TIMESTAMP`)
	q("EXTRACT dow", `SELECT EXTRACT(DOW FROM CURRENT_DATE)`)

	// --- json_each with param
	q("json_each param IN", `SELECT id FROM t WHERE id IN (SELECT value FROM json_each(?))`, "[1]")
	q("json_each param NOT IN", `SELECT id FROM t WHERE id NOT IN (SELECT value FROM json_each(?))`, "[2]")

	// --- tz modifier support?
	q("modifier tz", `SELECT strftime('%Y','now','localtime','+0 days')`)

	// --- window functions / GROUP BY bucket emulation
	q("floor to hour", `SELECT strftime('%Y-%m-%dT%H:00:00Z', created) FROM (SELECT '2024-05-05T13:47:11Z' AS created)`)
	q("substr hour", `SELECT substr('2024-05-05T13:47:11Z',1,13)||':00:00Z'`)

	// --- multi-row INSERT
	must(`INSERT INTO t (id, extra, num, s) VALUES (10,'{}',1,'a'),(11,'{}',2,'b')`)
	q("rows inserted", `SELECT COUNT(*) FROM t WHERE id>=10`)

	// --- json_set deep path create
	q("json_set deep", `SELECT json_set('{}','$.a.b',1)`)
	q("json || merge via json_patch", `SELECT json_patch('{"a":1,"b":2}','{"b":3}')`)

	// --- escape LIKE
	q("LIKE default ci", `SELECT 'Hello' LIKE 'hello'`)
	q("GLOB?", `SELECT 1`)

	// --- does sqlite support `~` regex?
	q("tilde regex", `SELECT 'abc' ~ '^[a-z]+$'`)
	q("regexp fn", `SELECT 'abc' REGEXP '^[a-z]+$'`)
	q("btrim", `SELECT btrim('  a ')`)
	q("split_part", `SELECT split_part('12:30',':',1)`)
	q("gen_random_uuid", `SELECT gen_random_uuid()`)
	q("::jsonb cast", `SELECT '{"a":1}'::jsonb`)
	q("-> operator", `SELECT '{"a":1}'::jsonb -> 'a'`)
	q("#> operator", `SELECT '{"a":{"b":2}}'::jsonb #> '{a,b}'`)
	q("EXISTS subq", `SELECT EXISTS(SELECT 1)`)
	q("to_char", `SELECT to_char(1,'9')`)
	q("strftime with AT TIME ZONE", `SELECT strftime('%Y','now') AT TIME ZONE 'UTC'`)

	// ==== json_type numeric variants (PG jsonb_typeof always says 'number')
	must(`CREATE TABLE n (v TEXT)`)
	must(`INSERT INTO n VALUES ('{"i":1}'),('{"r":1.5}'),('{"s":"x"}'),('{"b":true}'),('{"n":null}'),('{"a":[1]}'),('{"o":{}}')`)
	rows, _ := db.Query(`SELECT v, json_type(v,'$.i'), json_type(v,'$.r'), json_type(v,'$.s'), json_type(v,'$.b'), json_type(v,'$.n'), json_type(v,'$.a'), json_type(v,'$.o') FROM n`)
	for rows.Next() {
		var a, b, c, d, e, f, g, h any
		_ = rows.Scan(&a, &b, &c, &d, &e, &f, &g, &h)
		t.Logf("TYPES src=%s int=%v real=%v str=%v bool=%v null=%v arr=%v obj=%v", a, b, c, d, e, f, g, h)
	}
	rows.Close()

	// ==== json_set with a strftime text value (must stay a JSON string, not raw)
	q("json_set strftime text", `SELECT json_set('{}','$.t', strftime('%Y-%m-%dT%H:%M:%fZ','now'))`)
	q("json_set quote in text", `SELECT json_set('{}','$.t','a"b')`)
	q("json_extract text back", `SELECT json_extract(json_set('{}','$.t','2020-01-01T00:00:00.000Z'),'$.t')`)
	q("json_set numeric as text", `SELECT json_set('{}','$.n','5.25')`)
	q("json_extract that", `SELECT json_extract(json_set('{}','$.n','5.25'),'$.n')`)
	q("typeof numeric stored", `SELECT typeof(json_extract(json_set('{}','$.n',5.25),'$.n'))`)

	// ==== time column handling
	must(`CREATE TABLE tw (id INTEGER PRIMARY KEY, w TIMESTAMP)`)
	nowT := timeNowProbe()
	must(`INSERT INTO tw (id,w) VALUES (1,?)`, nowT.Add(-10*time.Hour))
	q("typeof stored ts", `SELECT typeof(w) FROM tw`)
	q("compare ts col + interval", `SELECT id FROM tw WHERE w <= ?`, nowT.Add(-5*time.Hour))
	q("compare datetime fn", `SELECT id FROM tw WHERE datetime(w) <= datetime(?)`, nowT.Add(-5*time.Hour))
	q("scan into time", `SELECT w FROM tw`)
	var got timeT
	_ = db.QueryRow(`SELECT w FROM tw WHERE id=1`).Scan(&got)
	t.Logf("scanned time = %v", got)

	// ==== containment (@>) equivalent for jsonb
	q("json_tree contains", `SELECT 1 WHERE '{"a":true}' IS NOT NULL`)
	q("json_extract bool", `SELECT json_extract('{"a":true}','$.a')`)
	q("bool compare 1", `SELECT json_extract('{"a":true}','$.a') = 1`)
	q("bool compare true", `SELECT json_extract('{"a":true}','$.a') = 'true'`)
	q("bool compare json", `SELECT json_extract('{"a":true}','$.a') = json('true')`)
	q("deep contains true", `SELECT json_extract('{"x":{"y":true}}','$.x.y') = 1`)

	// ==== RowsAffected / conditional update protocol
	must(`CREATE TABLE g (id INTEGER PRIMARY KEY, deleted_at TIMESTAMP)`)
	must(`INSERT INTO g VALUES (1, NULL), (2, CURRENT_TIMESTAMP)`)
	r2, err := db.Exec(`UPDATE g SET deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`, 1)
	if err != nil {
		t.Logf("cond update ERR %v", err)
	} else {
		n, _ := r2.RowsAffected()
		t.Logf("cond update rowsAffected=%d", n)
	}
	r3, _ := db.Exec(`UPDATE g SET deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`, 2)
	n3, _ := r3.RowsAffected()
	t.Logf("cond update on deleted rows rowsAffected=%d", n3)

	// ==== json_remove multiple keys (PG `- 'a' - 'b'`)
	q("json_remove multi", `SELECT json_remove('{"a":1,"b":2,"c":3}','$.a','$.b')`)

	// ==== exists key (PG ? operator)
	q("json key exists", `SELECT json_type('{"a":1}','$.a') IS NOT NULL`)
	q("json key missing", `SELECT json_type('{"a":1}','$.zz') IS NOT NULL`)
	q("json_valid null", `SELECT json_valid(NULL)`)
	q("coalesce null json", `SELECT json_valid(COALESCE(NULL,'{}'))`)
	q("COALESCE extra NULL", `SELECT json_extract(COALESCE(NULL,'{}'),'$.quota_used')`)

	// ==== numeric coercion
	q("coalesce numeric", `SELECT COALESCE(json_extract('{"q":"3.5"}','$.q'),0) + 1`)
	q("cast real", `SELECT CAST('3.5' AS REAL) + 1`)

	// ==== strftime modifiers combos used for reset windows
	q("start of day", `SELECT strftime('%Y-%m-%dT%H:%M:%SZ','now','start of day')`)
	q("start of day +7 days", `SELECT strftime('%Y-%m-%dT%H:%M:%SZ','now','start of day','+7 days')`)
	q("utc suffix", `SELECT strftime('%Y-%m-%dT%H:%M:%S.%fZ','now')`)
}

type timeT = timeTime
