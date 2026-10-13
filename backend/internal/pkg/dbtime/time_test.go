package dbtime

import (
	"testing"
	"time"
)

func TestParseDatabaseTimestampFormats(t *testing.T) {
	want := time.Date(2026, 10, 1, 16, 5, 6, 123000000, time.UTC)
	for _, input := range []any{
		want, want.UnixMicro(), "2026-10-02T00:05:06.123+08:00", []byte("2026-10-01 16:05:06.123"),
		"2026-10-02 00:05:06.123 +0800 CST", "2026-10-01 16:05:06.123+00:00",
	} {
		got, err := Parse(input)
		if err != nil || !got.Equal(want) {
			t.Fatalf("Parse(%v)=%v,%v; want %v", input, got, err, want)
		}
	}
	for _, input := range []any{nil, "not a time", true} {
		if _, err := Parse(input); err == nil {
			t.Fatalf("accepted %v", input)
		}
	}
	var nullable NullTime
	if err := nullable.Scan(want); err != nil || !nullable.Valid {
		t.Fatal(err)
	}
	if err := nullable.Scan(nil); err != nil || nullable.Valid || !nullable.Time.IsZero() {
		t.Fatal("NULL retained prior timestamp")
	}
}
