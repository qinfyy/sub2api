package repository

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shopspring/decimal"
)

// probeJSONEqual follows JSONB equality without converting numbers to float64.
// Missing fields are normalized by the caller only where the PG CAS does so.
func probeJSONEqual(left, right string) (bool, error) {
	decode := func(raw string) (any, error) {
		d := json.NewDecoder(strings.NewReader(raw))
		d.UseNumber()
		var value any
		if err := d.Decode(&value); err != nil {
			return nil, fmt.Errorf("decode probe identity JSON: %w", err)
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("invalid trailing probe identity JSON")
		}
		return value, nil
	}
	l, err := decode(left)
	if err != nil {
		return false, err
	}
	r, err := decode(right)
	if err != nil {
		return false, err
	}
	return equalProbeJSONValue(l, r), nil
}

func equalProbeJSONValue(left, right any) bool {
	switch l := left.(type) {
	case nil:
		return right == nil
	case bool:
		r, ok := right.(bool)
		return ok && l == r
	case string:
		r, ok := right.(string)
		return ok && l == r
	case json.Number:
		r, ok := right.(json.Number)
		if !ok {
			return false
		}
		// Compare normalized coefficients and exponents without expanding a
		// potentially very large exponent supplied inside a JSON number.
		normalize := func(n json.Number) (string, int64, bool) {
			d, err := decimal.NewFromString(n.String())
			if err != nil {
				return "", 0, false
			}
			if d.IsZero() {
				return "0", 0, true
			}
			coefficient := d.Coefficient().String()
			trimmed := strings.TrimRight(coefficient, "0")
			return trimmed, int64(d.Exponent()) + int64(len(coefficient)-len(trimmed)), true
		}
		lc, le, lok := normalize(l)
		rc, re, rok := normalize(r)
		return lok && rok && lc == rc && le == re
	case []any:
		r, ok := right.([]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for i := range l {
			if !equalProbeJSONValue(l[i], r[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		r, ok := right.(map[string]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for key, value := range l {
			other, found := r[key]
			if !found || !equalProbeJSONValue(value, other) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
