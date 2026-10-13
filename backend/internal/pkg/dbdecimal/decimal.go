// Package dbdecimal preserves database NUMERIC values without a float64 round trip.
package dbdecimal

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/shopspring/decimal"
)

// Decimal implements the Scanner/Valuer contract used by Ent custom fields.
// Nullability belongs to the field, not to a magic zero value.
type Decimal struct{ decimal.Decimal }

func Parse(s string) (Decimal, error) {
	if len(s) == 0 || len(s) > 4096 {
		return Decimal{}, errors.New("invalid decimal length")
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return Decimal{}, fmt.Errorf("invalid decimal: %w", err)
	}
	// Bound exponent expansion before rounding, formatting or comparisons.
	if v.Exponent() < -4096 || v.Exponent() > 4096 {
		return Decimal{}, errors.New("decimal exponent out of range")
	}
	return Decimal{v}, nil
}

func FromFloat(v float64) (Decimal, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Decimal{}, errors.New("non-finite decimal input")
	}
	return Parse(strconv.FormatFloat(v, 'f', -1, 64))
}

func (d *Decimal) Scan(src any) error {
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	case int64:
		s = strconv.FormatInt(v, 10)
	default:
		return fmt.Errorf("decimal requires exact SQL text/integer, got %T", src)
	}
	v, err := Parse(s)
	if err == nil {
		*d = v
	}
	return err
}

func (d Decimal) Value() (driver.Value, error) { return d.String(), nil }

// Quantize matches NUMERIC(p,s): round half away from zero, then check range.
func (d Decimal) Quantize(precision, scale int32) (string, error) {
	if precision < 1 || precision > 1000 || scale < 0 || scale > precision {
		return "", errors.New("invalid numeric precision/scale")
	}
	rounded := d.Round(scale)
	limit := decimal.New(1, precision-scale)
	if rounded.Abs().Cmp(limit) >= 0 {
		return "", fmt.Errorf("numeric(%d,%d) overflow", precision, scale)
	}
	return rounded.StringFixed(scale), nil
}

func Canonical(s string, precision, scale int32) bool {
	d, err := Parse(s)
	if err != nil {
		return false
	}
	value, err := d.Quantize(precision, scale)
	return err == nil && value == s
}
