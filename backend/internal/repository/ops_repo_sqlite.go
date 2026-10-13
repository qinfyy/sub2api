package repository

import "fmt"

// Time predicates compare instants, not mixed SQLite TEXT representations.
func opsTimePredicate(column, op string, index int, sqlite ...bool) string {
	if len(sqlite) > 0 && sqlite[0] {
		return fmt.Sprintf("pg_rfc3339_micros(%s) %s pg_rfc3339_micros($%d)", column, op, index)
	}
	return fmt.Sprintf("%s %s $%d", column, op, index)
}

// The ordered CTE supplies one-based rn and total, excluding null samples.
func opsSQLitePercentileExpr(column string, fraction float64) string {
	rank := fmt.Sprintf("(1 + (total - 1) * %.2f)", fraction)
	low := "CAST(" + rank + " AS INTEGER)"
	weight := "(" + rank + " - " + low + ")"
	return fmt.Sprintf("MAX(CASE WHEN rn=%s THEN %s END)*(1-%s) + MAX(CASE WHEN rn=MIN(%s+1,total) THEN %s END)*%s", low, column, weight, low, column, weight)
}
