package repository

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
	moderncsqlite "modernc.org/sqlite"
)

// sqliteTimeDest scans SQLite TEXT timestamps into time.Time. Non-SQLite
// callers get the original pointer and keep database/sql's native behavior.
type sqliteTimeDest struct {
	target *time.Time
}

func (d sqliteTimeDest) Scan(src any) error {
	if d.target == nil {
		return nil
	}
	if src == nil {
		*d.target = time.Time{}
		return nil
	}
	parsed, ok := sqliteTimeValue(src)
	if !ok {
		return fmt.Errorf("sqlite: cannot parse timestamp %T as time.Time", src)
	}
	*d.target = parsed
	return nil
}

func timeScanDest(sqlite bool, target *time.Time) any {
	if sqlite {
		return sqliteTimeDest{target: target}
	}
	return target
}

// SQLite 兼容层：把 PostgreSQL 方言里高频出现的内置函数映射到 SQLite。
//
// 现有业务 SQL 大量使用 NOW()、GREATEST()、LEAST() 等 PostgreSQL 函数，
// 逐条改写既容易遗漏、又会把 PG 与 SQLite 两条 SQL 分支长期分叉。这里在
// modernc 驱动层注册同名标量函数，让这些语句在 SQLite 下直接可执行。
//
// 函数在进程内全局注册一次，作用于所有 SQLite 连接（包括测试临时库）。
// 只注册 PostgreSQL 语义明确、SQLite 无同名冲突的函数；`jsonb_*`、
// `AT TIME ZONE`、`INTERVAL` 等无法用标量函数等价表达的语法仍需按调用点
// 改写。
var registerSQLiteCompatOnce sync.Once

func init() {
	registerSQLiteCompatFunctions()
}

func registerSQLiteCompatFunctions() {
	registerSQLiteCompatOnce.Do(func() {
		register := func(name string, nArg int32, fn func(args []driver.Value) (driver.Value, error)) {
			// 注册失败只可能因为重名；忽略即可，保持幂等。
			_ = moderncsqlite.RegisterScalarFunction(name, nArg, func(_ *moderncsqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				return fn(args)
			})
		}

		register("now", 0, func([]driver.Value) (driver.Value, error) {
			return time.Now().UTC().Format(time.RFC3339Nano), nil
		})
		register("statement_timestamp", 0, func([]driver.Value) (driver.Value, error) {
			return time.Now().UTC().Format(time.RFC3339Nano), nil
		})
		register("clock_timestamp", 0, func([]driver.Value) (driver.Value, error) {
			return time.Now().UTC().Format(time.RFC3339Nano), nil
		})
		register("greatest", -1, func(args []driver.Value) (driver.Value, error) {
			return extremum(args, true)
		})
		register("least", -1, func(args []driver.Value) (driver.Value, error) {
			return extremum(args, false)
		})
		// PostgreSQL 的 btrim(str) 去掉两端空白；SQLite 只有单参数 trim。
		register("btrim", 1, func(args []driver.Value) (driver.Value, error) {
			if len(args) != 1 || args[0] == nil {
				return nil, nil
			}
			s, ok := args[0].(string)
			if !ok {
				return args[0], nil
			}
			return strings.TrimSpace(s), nil
		})
		register("btrim", 2, func(args []driver.Value) (driver.Value, error) {
			if len(args) != 2 || args[0] == nil || args[1] == nil {
				return nil, nil
			}
			s, ok := args[0].(string)
			if !ok {
				return args[0], nil
			}
			cutset, ok := args[1].(string)
			if !ok {
				return nil, nil
			}
			return strings.Trim(s, cutset), nil
		})
		// gen_random_uuid() 供账号指纹种子等 SQL 默认路径使用。
		register("gen_random_uuid", 0, func([]driver.Value) (driver.Value, error) {
			return uuid.NewString(), nil
		})
		// to_jsonb(x) / jsonb_build_object(...) 在 SQLite 下按 JSON 文本处理。
		register("to_jsonb", 1, func(args []driver.Value) (driver.Value, error) {
			if len(args) != 1 {
				return nil, nil
			}
			return jsonText(args[0])
		})
		register("jsonb_build_object", -1, func(args []driver.Value) (driver.Value, error) {
			return jsonBuildObject(args)
		})
		register("jsonb_typeof", 1, func(args []driver.Value) (driver.Value, error) {
			if len(args) != 1 || args[0] == nil {
				return nil, nil
			}
			raw, ok := args[0].(string)
			if !ok {
				return nil, nil
			}
			return jsonTypeOf(raw)
		})
		// SQLite has no timezone-aware RFC3339 parser. Business timestamps are
		// written by Go as RFC3339Nano UTC values, so parse them here and expose
		// microseconds since epoch for numeric comparisons. Invalid calendar
		// dates return NULL, matching the PostgreSQL jsonpath fail-open path.
		register("pg_rfc3339_micros", 1, func(args []driver.Value) (driver.Value, error) {
			return parseSQLiteRFC3339Micros(args[0])
		})
		register("to_char", 2, func(args []driver.Value) (driver.Value, error) {
			return sqliteToChar(args[0], args[1])
		})
		register("sub2api_time_bucket", 3, func(args []driver.Value) (driver.Value, error) {
			if args[0] == nil {
				return nil, nil
			}
			t, err := dbtime.Parse(args[0])
			if err != nil {
				return nil, err
			}
			loc, err := time.LoadLocation(fmt.Sprint(args[2]))
			if err != nil {
				return nil, err
			}
			t = t.In(loc)
			switch args[1] {
			case "day":
				return t.Format("2006-01-02"), nil
			case "hour":
				return t.Add(-time.Duration(t.Minute())*time.Minute - time.Duration(t.Second())*time.Second - time.Duration(t.Nanosecond())).UTC().Format(time.RFC3339Nano), nil
			case "minute":
				return t.Truncate(time.Minute).UTC().Format(time.RFC3339Nano), nil
			default:
				return nil, fmt.Errorf("unsupported time bucket %v", args[1])
			}
		})
	})
}

// sqliteToChar implements the small subset of PostgreSQL TO_CHAR used by the
// usage dashboard. Timestamps are stored as RFC3339 text by the application.
func sqliteToChar(value, format driver.Value) (driver.Value, error) {
	if value == nil {
		return nil, nil
	}
	layout, ok := format.(string)
	if !ok {
		return nil, nil
	}
	t, ok := sqliteTimeValue(value)
	if !ok {
		return fmt.Sprint(value), nil
	}
	t = t.In(timezone.Location())
	switch strings.TrimSpace(layout) {
	case "YYYY-MM-DD HH24:00":
		return t.Format("2006-01-02 15:00"), nil
	case "YYYY-MM-DD":
		return t.Format("2006-01-02"), nil
	case "IYYY-IW":
		year, week := t.ISOWeek()
		return fmt.Sprintf("%04d-%02d", year, week), nil
	case "YYYY-MM":
		return t.Format("2006-01"), nil
	default:
		return t.Format("2006-01-02"), nil
	}
}

func sqliteTimeValue(value any) (time.Time, bool) {
	switch v := value.(type) {
	case time.Time:
		return v.UTC(), true
	case string:
		return sqliteTimeString(v)
	case []byte:
		return sqliteTimeString(string(v))
	case int64:
		return time.UnixMicro(v).UTC(), true
	case float64:
		return time.UnixMicro(int64(v)).UTC(), true
	default:
		return time.Time{}, false
	}
}

func sqliteTimeString(raw string) (time.Time, bool) {
	parsed, err := dbtime.Parse(raw)
	return parsed.UTC(), err == nil
}

func parseSQLiteRFC3339Micros(value driver.Value) (driver.Value, error) {
	if value == nil {
		return nil, nil
	}
	var raw string
	switch v := value.(type) {
	case time.Time:
		return v.UTC().UnixMicro(), nil
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		if valuer, ok := value.(driver.Valuer); ok {
			converted, err := valuer.Value()
			if err != nil {
				return nil, nil
			}
			return parseSQLiteRFC3339Micros(converted)
		}
		raw = fmt.Sprint(value)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := dbtime.Parse(raw)
	if err == nil {
		return parsed.UTC().UnixMicro(), nil
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
	} {
		parsed, err = time.ParseInLocation(layout, raw, time.UTC)
		if err == nil {
			return parsed.UTC().UnixMicro(), nil
		}
	}
	return nil, nil
}

// extremum 实现 GREATEST / LEAST 的 PostgreSQL 语义：忽略 NULL，全部为
// NULL 时返回 NULL；数值按数值比较，其余按字符串比较。
func extremum(args []driver.Value, wantMax bool) (driver.Value, error) {
	var best driver.Value
	for _, arg := range args {
		if arg == nil {
			continue
		}
		if best == nil {
			best = arg
			continue
		}
		cmp, err := compareSQLiteValues(arg, best)
		if err != nil {
			return nil, err
		}
		if (wantMax && cmp > 0) || (!wantMax && cmp < 0) {
			best = arg
		}
	}
	return best, nil
}

func compareSQLiteValues(left, right driver.Value) (int, error) {
	lf, lok := numericValue(left)
	rf, rok := numericValue(right)
	if lok && rok {
		switch {
		case lf < rf:
			return -1, nil
		case lf > rf:
			return 1, nil
		default:
			return 0, nil
		}
	}
	ls := fmt.Sprint(left)
	rs := fmt.Sprint(right)
	return strings.Compare(ls, rs), nil
}

func numericValue(value driver.Value) (float64, bool) {
	switch v := value.(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case []byte:
		var f float64
		if _, err := fmt.Sscan(string(v), &f); err == nil {
			return f, true
		}
	case string:
		var f float64
		if _, err := fmt.Sscan(v, &f); err == nil {
			return f, true
		}
	}
	return math.NaN(), false
}

// jsonText 把 SQLite 值编码为 JSON 文本，供 to_jsonb 使用。
func jsonText(value driver.Value) (driver.Value, error) {
	switch v := value.(type) {
	case nil:
		return "null", nil
	case []byte:
		// 已是 JSON 文本时保持原样，否则按字符串编码。
		if json.Valid(v) {
			return string(v), nil
		}
		encoded, err := json.Marshal(string(v))
		if err != nil {
			return nil, err
		}
		return string(encoded), nil
	case string:
		if json.Valid([]byte(v)) {
			return v, nil
		}
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(encoded), nil
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(encoded), nil
	}
}

// jsonBuildObject 实现 jsonb_build_object(key, value, ...)。
func jsonBuildObject(args []driver.Value) (driver.Value, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("jsonb_build_object requires an even number of arguments")
	}
	out := make(map[string]any, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key := fmt.Sprint(args[i])
		out[key] = jsonBuildValue(args[i+1])
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func jsonBuildValue(value driver.Value) any {
	switch v := value.(type) {
	case []byte:
		var decoded any
		if err := json.Unmarshal(v, &decoded); err == nil {
			return decoded
		}
		return string(v)
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			return decoded
		}
		return v
	default:
		return v
	}
}

// jsonTypeOf 实现 jsonb_typeof，返回 null / boolean / number / string /
// array / object；无法解析时返回 NULL。
func jsonTypeOf(raw string) (driver.Value, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, nil
	}
	switch decoded.(type) {
	case nil:
		return "null", nil
	case bool:
		return "boolean", nil
	case float64, json.Number:
		return "number", nil
	case string:
		return "string", nil
	case []any:
		return "array", nil
	case map[string]any:
		return "object", nil
	default:
		return nil, nil
	}
}
