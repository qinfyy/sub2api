// Package dbtime reads native database timestamps and SQLite text timestamps.
package dbtime

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func Parse(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case []byte:
		return Parse(string(v))
	case int64:
		return time.UnixMicro(v).UTC(), nil
	case float64:
		return time.UnixMicro(int64(v)).UTC(), nil
	case string:
		v = strings.TrimSpace(v)
		for _, layout := range []string{
			time.RFC3339Nano,
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05.999999999 -0700 MST",
			"2006-01-02 15:04:05.999999999 -0700",
			"2006-01-02 15:04:05.999999999",
			"2006-01-02T15:04:05.999999999",
			"2006-01-02",
		} {
			if parsed, err := time.ParseInLocation(layout, v, time.UTC); err == nil {
				return parsed, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse database timestamp %T", value)
}

type destination struct{ target *time.Time }

func Dest(target *time.Time) sql.Scanner { return destination{target} }

func (d destination) Scan(value any) error {
	parsed, err := Parse(value)
	if err == nil {
		*d.target = parsed
	}
	return err
}

type NullTime struct{ sql.NullTime }

func (n *NullTime) Scan(value any) error {
	if value == nil {
		n.Time, n.Valid = time.Time{}, false
		return nil
	}
	parsed, err := Parse(value)
	n.Time, n.Valid = parsed, err == nil
	return err
}
