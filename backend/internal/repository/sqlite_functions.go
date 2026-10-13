package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbdecimal"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/shopspring/decimal"
	sqlite "modernc.org/sqlite"
)

var sqliteDomainFunctions struct {
	sync.Once
	err error
}

func registerSQLiteDomainFunctions() error {
	sqliteDomainFunctions.Do(func() { sqliteDomainFunctions.err = registerSQLiteDomainFunctionsOnce() })
	return sqliteDomainFunctions.err
}

func registerSQLiteDomainFunctionsOnce() error {
	functions := map[string]*sqlite.FunctionImpl{
		"s2a_decimal_add": decimalBinaryFunction(func(a, b decimal.Decimal) decimal.Decimal { return a.Add(b) }),
		"s2a_decimal_sub": decimalBinaryFunction(func(a, b decimal.Decimal) decimal.Decimal { return a.Sub(b) }),
		"s2a_decimal_mul": decimalBinaryFunction(func(a, b decimal.Decimal) decimal.Decimal { return a.Mul(b) }),
		"s2a_decimal_cmp": {NArgs: 2, Deterministic: true, Scalar: func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			a, b, err := decimalPair(args)
			if err != nil || args[0] == nil || args[1] == nil {
				return nil, err
			}
			return int64(a.Cmp(b.Decimal)), nil
		}},
		"s2a_decimal_quantize": {NArgs: 3, Deterministic: true, Scalar: func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if args[0] == nil {
				return nil, nil
			}
			p, s, err := decimalDimensions(args[1], args[2])
			if err != nil {
				return nil, err
			}
			var d dbdecimal.Decimal
			if err := d.Scan(args[0]); err != nil {
				return nil, err
			}
			return d.Quantize(p, s)
		}},
		"s2a_decimal_valid": {NArgs: 3, Deterministic: true, Scalar: func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			p, s, err := decimalDimensions(args[1], args[2])
			if err != nil {
				return nil, err
			}
			value, ok := args[0].(string)
			if ok && dbdecimal.Canonical(value, p, s) {
				return int64(1), nil
			}
			return int64(0), nil
		}},
		"s2a_decimal_sum": {NArgs: 1, Deterministic: true, MakeAggregate: func(sqlite.FunctionContext) (sqlite.AggregateFunction, error) {
			return &sqliteDecimalSum{}, nil
		}},
		"s2a_sha256": {NArgs: 1, Deterministic: true, Scalar: func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if args[0] == nil {
				return nil, nil
			}
			value, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("s2a_sha256 expects text")
			}
			sum := sha256.Sum256([]byte(value))
			return hex.EncodeToString(sum[:]), nil
		}},
		"s2a_utc_now": {NArgs: 0, Scalar: func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
			return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z"), nil
		}},
		"s2a_timezone": {NArgs: 0, Scalar: func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
			return timezone.Location().String(), nil
		}},
		"s2a_local_day": {NArgs: 2, Scalar: func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if args[0] == nil {
				return nil, nil
			}
			instant, ok := sqliteTimeValue(args[0])
			if !ok {
				return nil, fmt.Errorf("invalid bucket timestamp")
			}
			name, ok := args[1].(string)
			if !ok {
				return nil, fmt.Errorf("invalid bucket timezone")
			}
			location := timezone.Location()
			if location.String() != name {
				var err error
				location, err = time.LoadLocation(name)
				if err != nil {
					return nil, err
				}
			}
			return instant.In(location).Format("2006-01-02"), nil
		}},
	}
	for name, impl := range functions {
		if err := sqlite.RegisterFunction(name, impl); err != nil {
			return fmt.Errorf("register sqlite function %s: %w", name, err)
		}
	}
	return sqlite.RegisterCollationUtf8("s2a_decimal", func(a, b string) int {
		x, xerr := dbdecimal.Parse(a)
		y, yerr := dbdecimal.Parse(b)
		// Invalid strings sort before numbers with a stable total order. CHECK
		// constraints reject them on storage; collations cannot return errors.
		if xerr != nil && yerr != nil {
			return strings.Compare(a, b)
		}
		if xerr != nil {
			return -1
		}
		if yerr != nil {
			return 1
		}
		return x.Cmp(y.Decimal)
	})
}

func decimalDimensions(precision, scale driver.Value) (int32, int32, error) {
	p, pok := precision.(int64)
	s, sok := scale.(int64)
	if !pok || !sok || p < 1 || p > 1000 || s < 0 || s > p {
		return 0, 0, fmt.Errorf("invalid numeric precision/scale")
	}
	return int32(p), int32(s), nil
}

func decimalPair(args []driver.Value) (a, b dbdecimal.Decimal, err error) {
	if args[0] == nil || args[1] == nil {
		return a, b, nil
	}
	if err = a.Scan(args[0]); err == nil {
		err = b.Scan(args[1])
	}
	return
}

func decimalBinaryFunction(fn func(decimal.Decimal, decimal.Decimal) decimal.Decimal) *sqlite.FunctionImpl {
	return &sqlite.FunctionImpl{NArgs: 2, Deterministic: true, Scalar: func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		a, b, err := decimalPair(args)
		if err != nil || args[0] == nil || args[1] == nil {
			return nil, err
		}
		return fn(a.Decimal, b.Decimal).String(), nil
	}}
}

type sqliteDecimalSum struct {
	total decimal.Decimal
	count int64
}

func (s *sqliteDecimalSum) Step(_ *sqlite.FunctionContext, args []driver.Value) error {
	if args[0] == nil {
		return nil
	}
	var d dbdecimal.Decimal
	if err := d.Scan(args[0]); err != nil {
		return err
	}
	s.total = s.total.Add(d.Decimal)
	s.count++
	return nil
}

func (s *sqliteDecimalSum) WindowInverse(_ *sqlite.FunctionContext, args []driver.Value) error {
	if args[0] == nil {
		return nil
	}
	var d dbdecimal.Decimal
	if err := d.Scan(args[0]); err != nil {
		return err
	}
	s.total = s.total.Sub(d.Decimal)
	s.count--
	return nil
}

func (s *sqliteDecimalSum) WindowValue(*sqlite.FunctionContext) (driver.Value, error) {
	if s.count == 0 {
		return nil, nil
	}
	return s.total.String(), nil
}

func (s *sqliteDecimalSum) Final(*sqlite.FunctionContext) {}

func checkSQLiteDomainFunctions(ctx context.Context, db *sql.DB) error {
	var sum, digest, aggregate, localDay, utcNow string
	var ordered, valid int
	if err := db.QueryRowContext(ctx, `SELECT
s2a_decimal_add('9999999999.9999999998', '0.0000000001'), s2a_sha256(''),
(SELECT s2a_decimal_sum(amount) FROM (SELECT '0.1' AS amount UNION ALL SELECT '0.2')),
('10' COLLATE s2a_decimal > '2'), s2a_decimal_valid('1.0000000000',20,10),
s2a_local_day('2026-01-02T00:00:00.000000Z',s2a_timezone()), s2a_utc_now()`).
		Scan(&sum, &digest, &aggregate, &ordered, &valid, &localDay, &utcNow); err != nil {
		return fmt.Errorf("sqlite domain function self-test: %w", err)
	}
	wantDay := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).In(timezone.Location()).Format("2006-01-02")
	_, timeErr := time.Parse("2006-01-02T15:04:05.000000Z", utcNow)
	if sum != "9999999999.9999999999" || digest != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" ||
		aggregate != "0.3" || ordered != 1 || valid != 1 || localDay != wantDay || timeErr != nil {
		return fmt.Errorf("sqlite domain function self-test mismatch")
	}
	return nil
}
