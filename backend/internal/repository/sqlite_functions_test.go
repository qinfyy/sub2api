package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteExactDecimalFunctions(t *testing.T) {
	require.NoError(t, registerSQLiteDomainFunctions())
	db := newSQLiteTestDB(t)
	ctx := context.Background()
	require.NoError(t, checkSQLiteDomainFunctions(ctx, db))
	_, err := db.Exec(`CREATE TABLE money(id INTEGER PRIMARY KEY, amount TEXT COLLATE s2a_decimal NOT NULL CHECK(s2a_decimal_valid(amount,20,10)));
CREATE INDEX money_amount ON money(amount);
INSERT INTO money VALUES (1,'9999999999.9999999999'),(2,'-0.0000000001'),(3,'2.0000000000'),(4,'10.0000000000');`)
	require.NoError(t, err)
	var sum string
	require.NoError(t, db.QueryRow(`SELECT s2a_decimal_sum(amount) FROM money`).Scan(&sum))
	require.Equal(t, "10000000011.9999999998", sum)
	var ids string
	require.NoError(t, db.QueryRow(`SELECT group_concat(id) FROM (SELECT id FROM money ORDER BY amount)`).Scan(&ids))
	require.Equal(t, "2,3,4,1", ids)
	for _, value := range []any{"10000000000.0000000000", "NaN", "2.00", float64(0.1)} {
		_, err := db.Exec(`INSERT INTO money(amount) VALUES (?)`, value)
		require.Error(t, err)
	}
	_, err = db.Exec(`UPDATE money SET amount=s2a_decimal_quantize(s2a_decimal_add(amount,'0.0000000001'),20,10) WHERE id=1`)
	require.Error(t, err)
	require.NoError(t, db.QueryRow(`SELECT amount FROM money WHERE id=1`).Scan(&sum))
	require.Equal(t, "9999999999.9999999999", sum)
	var empty sql.NullString
	require.NoError(t, db.QueryRow(`SELECT s2a_decimal_sum(amount) FROM money WHERE id=0`).Scan(&empty))
	require.False(t, empty.Valid)
	_, err = db.Exec(`SELECT s2a_decimal_add(0.1, '0.2')`)
	require.Error(t, err)
}

func TestSQLiteDecimalWindowSum(t *testing.T) {
	require.NoError(t, registerSQLiteDomainFunctions())
	db := newSQLiteTestDB(t)
	rows, err := db.Query(`WITH entries(id,amount) AS (VALUES(1,'0.1'),(2,NULL),(3,'0.2'),(4,'0.3'))
SELECT s2a_decimal_sum(amount) OVER(ORDER BY id ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) FROM entries`)
	require.NoError(t, err)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var value string
		require.NoError(t, rows.Scan(&value))
		got = append(got, value)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"0.1", "0.1", "0.2", "0.5"}, got)
}

func TestSQLiteLocalDayRespectsNamedTimezoneAndDST(t *testing.T) {
	require.NoError(t, registerSQLiteDomainFunctions())
	db := newSQLiteTestDB(t)
	for _, tc := range []struct{ instant, zone, day string }{
		{"2026-03-08T04:59:59.999999Z", "America/New_York", "2026-03-07"},
		{"2026-03-08T05:00:00.000000Z", "America/New_York", "2026-03-08"},
		{"2026-03-09T03:59:59.999999Z", "America/New_York", "2026-03-08"},
		{"2026-03-09T04:00:00.000000Z", "America/New_York", "2026-03-09"},
		{"2026-11-01T03:59:59.999999Z", "America/New_York", "2026-10-31"},
		{"2026-11-01T05:30:00.000000Z", "America/New_York", "2026-11-01"},
		{"2026-11-01T06:30:00.000000Z", "America/New_York", "2026-11-01"},
		{"2026-11-02T04:59:59.999999Z", "America/New_York", "2026-11-01"},
		{"2026-11-02T05:00:00.000000Z", "America/New_York", "2026-11-02"},
		{"2026-01-01T18:14:59.999999Z", "Asia/Kathmandu", "2026-01-01"},
		{"2026-01-01T18:15:00.000000Z", "Asia/Kathmandu", "2026-01-02"},
	} {
		t.Run(tc.instant+"/"+tc.zone, func(t *testing.T) {
			var got string
			require.NoError(t, db.QueryRow(`SELECT s2a_local_day(?,?)`, tc.instant, tc.zone).Scan(&got))
			require.Equal(t, tc.day, got)
		})
	}
	for _, args := range [][]any{{"not a timestamp", "UTC"}, {"2026-01-01T00:00:00Z", "not/a/zone"}} {
		var day string
		require.Error(t, db.QueryRow(`SELECT s2a_local_day(?,?)`, args...).Scan(&day))
	}
}
