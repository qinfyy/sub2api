package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/lib/pq"
	moderncsqlite "modernc.org/sqlite"
)

// ─── 方言判定 ───────────────────────────────────────────────────────────────

// dialectOf 返回规范化后的数据库方言名（"postgres" / "sqlite"）。
// cfg 为 nil 或未显式配置时回退到 postgres。
func dialectOf(cfg *config.Config) string {
	if cfg == nil {
		return config.DatabaseTypePostgres
	}
	d := cfg.Database
	d.NormalizeType()
	return d.Type
}

// isSQLiteDB 判断 *sql.DB 是否由 SQLite 驱动打开。
//
// 先按驱动具体类型判定（两者都是具体类型，判定最可靠），未命中时回退到
// 驱动类型的反射名匹配，这样经过包装的驱动也能识别。
func isSQLiteDB(db *sql.DB) bool {
	if db == nil {
		return false
	}
	d := db.Driver()
	if d == nil {
		return false
	}
	if _, ok := d.(*moderncsqlite.Driver); ok {
		return true
	}
	// 明确排除 PostgreSQL，避免名字相近时误判。
	if _, ok := d.(*pq.Driver); ok {
		return false
	}
	return strings.Contains(strings.ToLower(fmt.Sprintf("%T", d)), "sqlite")
}

// isPostgresDB 判断 *sql.DB 是否由 PostgreSQL 驱动打开。
func isPostgresDB(db *sql.DB) bool {
	if db == nil {
		return false
	}
	d := db.Driver()
	if d == nil {
		return false
	}
	if _, ok := d.(*pq.Driver); ok {
		return true
	}
	if _, ok := d.(*moderncsqlite.Driver); ok {
		return false
	}
	name := strings.ToLower(fmt.Sprintf("%T", d))
	return strings.Contains(name, "postgres") || strings.Contains(name, "pq.")
}

// ─── 占位符词法扫描 ──────────────────────────────────────────────────────────

// sqlScanState 是占位符扫描器的状态。
type sqlScanState int

const (
	scanStateCode sqlScanState = iota
	scanStateSingleQuote
	scanStateDoubleQuote
	scanStateBacktick
	scanStateLineComment
	scanStateBlockComment
)

// sqlPlaceholder 记录一个位于 SQL 代码区（不在字符串/注释内）的 $N 占位符。
type sqlPlaceholder struct {
	// start 是占位符 '$' 在原串中的下标。
	start int
	// end 是占位符最后一个数字之后的下标（不含）。
	end int
	// number 是占位符序号（1 起）。
	number int
}

// scanSQLLiterals 逐字符扫描 query，返回代码区内的 $N 占位符列表。
//
// 字符串字面量（'...'）、双引号标识符（"..."）、反引号标识符（`...`）、
// 行注释（-- ...）与块注释（/* ... */）内的内容都会被跳过，因此字面量里的
// '$1' 文本不会被误判为占位符。
func scanSQLLiterals(query string) []sqlPlaceholder {
	if !strings.Contains(query, "$") {
		return nil
	}

	var out []sqlPlaceholder
	state := scanStateCode

	for i := 0; i < len(query); i++ {
		c := query[i]

		switch state {
		case scanStateCode:
			switch {
			case c == '\'':
				state = scanStateSingleQuote
			case c == '"':
				state = scanStateDoubleQuote
			case c == '`':
				state = scanStateBacktick
			case c == '-' && i+1 < len(query) && query[i+1] == '-':
				state = scanStateLineComment
			case c == '/' && i+1 < len(query) && query[i+1] == '*':
				state = scanStateBlockComment
			case c == '$' && i+1 < len(query) && isDigit(query[i+1]):
				j := i + 1
				for j < len(query) && isDigit(query[j]) {
					j++
				}
				number, err := strconv.Atoi(query[i+1 : j])
				if err == nil {
					out = append(out, sqlPlaceholder{start: i, end: j, number: number})
				}
				i = j - 1
			}

		case scanStateSingleQuote:
			// '' 是 SQL 里的转义单引号，连续两个单引号仍留在字符串内。
			if c == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					i++
					continue
				}
				state = scanStateCode
			}

		case scanStateDoubleQuote:
			if c == '"' {
				state = scanStateCode
			}

		case scanStateBacktick:
			if c == '`' {
				state = scanStateCode
			}

		case scanStateLineComment:
			if c == '\n' {
				state = scanStateCode
			}

		case scanStateBlockComment:
			if c == '*' && i+1 < len(query) && query[i+1] == '/' {
				i++
				state = scanStateCode
			}
		}
	}

	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// sqlitePlaceholders 把 PostgreSQL 的 $N 占位符转换为 SQLite 的 ? 占位符。
// 字符串字面量与注释中的 $N 保持原样。
func sqlitePlaceholders(query string) string {
	placeholders := scanSQLLiterals(query)
	if len(placeholders) == 0 {
		return query
	}

	var b strings.Builder
	b.Grow(len(query))
	prev := 0
	for _, p := range placeholders {
		b.WriteString(query[prev:p.start])
		b.WriteByte('?')
		prev = p.end
	}
	b.WriteString(query[prev:])
	return b.String()
}

// ─── 数组参数绑定 ────────────────────────────────────────────────────────────

// errSQLArrayExpansion 表示查询中的数组参数无法安全展开。
type errSQLArrayExpansion struct {
	reason string
}

func (e *errSQLArrayExpansion) Error() string { return e.reason }

// bindArrayArgs 把一条带数组参数的 PostgreSQL 查询改写为 SQLite 可执行形式。
//
// PostgreSQL 侧数组参数通常写成 `= ANY($1)` 并配合 pq.Array 编解码；
// SQLite 没有数组类型，本函数把切片按元素展开成 `(?, ?, ...)`：
//
//	SELECT * FROM users WHERE id IN ($1)
//	args: []any{[]int64{1, 2, 3}}
//	→ "SELECT * FROM users WHERE id IN (?, ?, ?)", []any{1, 2, 3}
//
// 非切片参数原样保留并转换为 ? 占位符。PostgreSQL 方言下查询与参数原样返回，
// 由 pq.Array 负责数组编解码。
//
// 限制：占位符序号必须与 args 下标一一对应（$N → args[N-1]）。若同一序号
// 被重复使用，展开后的参数数量无法与占位符对应，此时返回错误而不是静默错位。
func bindArrayArgs(db *sql.DB, query string, args []any) (string, []any, error) {
	if !isSQLiteDB(db) {
		return query, args, nil
	}
	return bindArrayArgsForSQLite(query, args)
}

// bindArrayArgsForSQLite 是 bindArrayArgs 的方言已知版本：调用方已经确认当前
// 连接是 SQLite，无需再次做驱动类型探测（例如执行器被事务客户端包装时）。
func bindArrayArgsForSQLite(query string, args []any) (string, []any, error) {
	placeholders := scanSQLLiterals(query)
	if len(placeholders) == 0 {
		return query, args, nil
	}

	seen := make(map[int]bool, len(placeholders))
	for _, p := range placeholders {
		if p.number < 1 || p.number > len(args) {
			return "", nil, &errSQLArrayExpansion{
				reason: fmt.Sprintf(
					"sqlite array binding: placeholder $%d has no matching argument (got %d args)",
					p.number, len(args),
				),
			}
		}
		if seen[p.number] {
			return "", nil, &errSQLArrayExpansion{
				reason: fmt.Sprintf(
					"sqlite array binding: placeholder $%d is used more than once, cannot expand safely",
					p.number,
				),
			}
		}
		seen[p.number] = true
	}

	var b strings.Builder
	b.Grow(len(query) + len(placeholders)*2)
	bound := make([]any, 0, len(args))
	prev := 0
	for _, p := range placeholders {
		value := args[p.number-1]
		elements, isArray := arrayElements(value)

		// `= ANY($1)` / `<> ALL($1)` 是 PostgreSQL 的数组比较运算符，
		// SQLite 没有等价语法，需要重写成 IN / NOT IN。
		if opStart, op, ok := findArrayComparisonOperator(query[prev:p.start]); ok {
			// 重写片段自带括号，原文 `ANY($1)` 的收尾括号要一并吃掉。
			if !isArray {
				return "", nil, &errSQLArrayExpansion{
					reason: fmt.Sprintf(
						"sqlite array binding: %s requires an array argument, got %T",
						op.postgres, value),
				}
			}
			rewritten, err := rewriteArrayComparison(op, elements)
			if err != nil {
				return "", nil, err
			}
			b.WriteString(query[prev : prev+opStart])
			b.WriteString(rewritten.query)
			bound = append(bound, rewritten.args...)
			prev = p.end
			if prev < len(query) && query[prev] == ')' {
				prev++
			}
			continue
		}

		b.WriteString(query[prev:p.start])

		if !isArray {
			b.WriteByte('?')
			bound = append(bound, value)
			prev = p.end
			continue
		}

		// 调用点有两种写法：
		//   id IN ($1) → 括号属于 SQL，展开后只补元素
		//   id = $1     → 括号不存在，展开要自带括号
		wrappedInParens := p.start > prev && query[p.start-1] == '(' &&
			p.end < len(query) && query[p.end] == ')'

		if len(elements) == 0 {
			// 空数组在 IN () 中是语法错误，用恒假条件表达“匹配不到任何行”。
			if wrappedInParens {
				b.WriteString("SELECT NULL WHERE 0")
			} else {
				b.WriteString("(SELECT NULL WHERE 0)")
			}
			prev = p.end
			continue
		}

		if !wrappedInParens {
			b.WriteByte('(')
		}
		for i, elem := range elements {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteByte('?')
			bound = append(bound, elem)
		}
		if !wrappedInParens {
			b.WriteByte(')')
		}
		prev = p.end
	}
	b.WriteString(query[prev:])

	return b.String(), bound, nil
}

// bindSQLArray 为 PostgreSQL 方言包装数组参数，供 pq.Array 的既有调用点复用。
// SQLite 方言下返回原值：切片需要在 SQL 里展开成多占位符，不能整体绑定。
func bindSQLArray(db *sql.DB, value any) any {
	if isSQLiteDB(db) {
		return value
	}
	return pq.Array(value)
}

// arrayArg 返回适合当前方言的数组绑定参数：SQLite 保留原始切片（由
// bindArrayArgsForSQLite 展开成多个占位符），PostgreSQL 用 pq.Array 包装。
func arrayArg(sqlite bool, value any) any {
	if sqlite {
		return value
	}
	return pq.Array(value)
}

// jsonArrayArg 把切片编码成 JSON 数组文本，供 SQLite 的 json_each 使用。
// 数字、字符串、布尔等标量元素均按 JSON 语义绑定，避免依赖驱动对切片的支持。
func jsonArrayArg(value any) (string, error) {
	elements, ok := arrayElements(value)
	if !ok {
		return "", &errSQLArrayExpansion{reason: fmt.Sprintf("json array binding requires a slice, got %T", value)}
	}
	encoded, err := json.Marshal(elements)
	if err != nil {
		return "", &errSQLArrayExpansion{reason: fmt.Sprintf("marshal json array: %v", err)}
	}
	return string(encoded), nil
}

// sqliteAnyArrayPlaceholder 用于把 PostgreSQL 的 `= ANY($N)` 改写成 SQLite 的
// `IN (SELECT value FROM json_each(?))`，用于无法静态展开元素个数的场景。
func sqliteAnyArrayPlaceholder(query string) string {
	for i := 1; i <= 9; i++ {
		placeholder := "$" + strconv.Itoa(i)
		question := "?"
		query = strings.ReplaceAll(query, "= ANY("+placeholder+")", "IN (SELECT value FROM json_each("+question+"))")
		query = strings.ReplaceAll(query, "=ANY("+placeholder+")", "IN (SELECT value FROM json_each("+question+"))")
		query = strings.ReplaceAll(query, "<> ALL("+placeholder+")", "NOT IN (SELECT value FROM json_each("+question+"))")
		query = strings.ReplaceAll(query, "<>ALL("+placeholder+")", "NOT IN (SELECT value FROM json_each("+question+"))")
	}
	// ANY(...) 已替换成 json_each 形式，剩下的 $N 统一转成 ?。
	return sqlitePlaceholders(query)
}

// arrayElements 判断 value 是否为可展开的数组/切片，并返回其元素。
//
// []byte 与 string 不视为数组：它们在 database/sql 中是标量绑定语义
// （BLOB / TEXT），展开会破坏语义。
func arrayElements(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	if _, ok := value.([]byte); ok {
		return nil, false
	}
	if s, ok := value.(string); ok {
		_ = s
		return nil, false
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
	default:
		return nil, false
	}
	return sliceElements(rv), true
}

func sliceElements(rv reflect.Value) []any {
	n := rv.Len()
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out
}

// ─── ANY / ALL 运算符重写 ───────────────────────────────────────────────────

// arrayComparison 是 PostgreSQL 的数组比较运算符及其 SQLite 等价形式。
type arrayComparison struct {
	// postgres 是原始运算符写法，例如 "= ANY(" 或 "<> ALL("。
	postgres string
	// sqlite 是重写后的运算符，作为子查询前缀。
	sqlite string
}

// findArrayComparisonOperator 在 prefix（占位符之前的 SQL 片段）中查找紧邻
// 占位符的数组比较运算符。
//
// 只接受这些确切写法，不做通用解析：
//
//	= ANY($1)   →  IN (SELECT ...)
//	=ANY($1)    →  IN (SELECT ...)
//	<> ALL($1)  →  NOT IN (SELECT ...)
//	<>ALL($1)   →  NOT IN (SELECT ...)
//
// 返回运算符在 prefix 中的起始下标与运算符描述。
func findArrayComparisonOperator(prefix string) (int, *arrayComparison, bool) {
	for _, candidate := range []arrayComparison{
		{postgres: "= ANY(", sqlite: "IN "},
		{postgres: "=ANY(", sqlite: "IN "},
		{postgres: "<> ALL(", sqlite: "NOT IN "},
		{postgres: "<>ALL(", sqlite: "NOT IN "},
	} {
		if strings.HasSuffix(prefix, candidate.postgres) {
			return len(prefix) - len(candidate.postgres), &candidate, true
		}
	}
	return 0, nil, false
}

// rewrittenArrayComparison 是一次数组运算符重写的结果。
type rewrittenArrayComparison struct {
	// query 是可直接替换 `ANY($N)` 位置的 SQL 片段（不含外围括号）。
	query string
	// args 是该片段对应的绑定参数。
	args []any
}

// rewriteArrayComparison 把 `ANY` / `ALL` 语义改写成 SQLite 的 IN 子查询。
//
// 语义映射（等价，无需展开元素个数）：
//
//	x = ANY(arr)   ⟺  x IN (arr)
//	x <> ALL(arr) ⟺  x NOT IN (arr)
//
// 只有这两个形式在 SQLite 中有严格等价写法。`ALL`（正向）与
// `ANY`（反向）没有等价形式，遇到时返回错误而不是给出错误语义。
//
// 数组参数以 JSON 文本传给 json_each，因此元素类型必须是可 JSON 序列化的
// 标量（数字、字符串、布尔）。
func rewriteArrayComparison(op *arrayComparison, elements []any) (rewrittenArrayComparison, error) {
	switch {
	case op.postgres == "= ANY(" || op.postgres == "=ANY(":
		// 已由 op.sqlite 给出 "IN " 前缀。
	case op.postgres == "<> ALL(" || op.postgres == "<>ALL(":
		// 已由 op.sqlite 给出 "NOT IN " 前缀。
	default:
		return rewrittenArrayComparison{}, &errSQLArrayExpansion{
			reason: fmt.Sprintf(
				"sqlite array binding: %s has no equivalent rewrite; "+
					"only \"= ANY\" and \"<> ALL\" are supported", op.postgres),
		}
	}

	payload, err := json.Marshal(elements)
	if err != nil {
		return rewrittenArrayComparison{}, &errSQLArrayExpansion{
			reason: fmt.Sprintf("sqlite array binding: marshal array as JSON: %v", err),
		}
	}

	return rewrittenArrayComparison{
		query: op.sqlite + "(SELECT value FROM json_each(?))",
		args:  []any{string(payload)},
	}, nil
}

// ─── keyed mutex ─────────────────────────────────────────────────────────────

var (
	keyedMutexMu    sync.Mutex
	keyedMutexTable = map[string]*keyedMutexEntry{}
)

// keyedMutexEntry 是某个 key 对应的锁及其引用计数。
type keyedMutexEntry struct {
	mu   sync.Mutex
	refs int
}

// keyedMutex 是按 key 隔离的进程内互斥锁。
//
// 用于替代 PostgreSQL 的 advisory lock：SQLite 没有跨进程等价物，而
// SQLite + 内存的部署形态本就是单实例，进程内互斥已足够。
type keyedMutex struct {
	key   string
	entry *keyedMutexEntry
}

// newKeyedMutex 返回 key 对应的进程内互斥锁。
func newKeyedMutex(key string) *keyedMutex {
	keyedMutexMu.Lock()
	defer keyedMutexMu.Unlock()

	entry, ok := keyedMutexTable[key]
	if !ok {
		entry = &keyedMutexEntry{}
		keyedMutexTable[key] = entry
	}
	entry.refs++
	return &keyedMutex{key: key, entry: entry}
}

// Lock 获取该 key 对应的锁。
func (k *keyedMutex) Lock() { k.entry.mu.Lock() }

// Unlock 释放该 key 对应的锁。
func (k *keyedMutex) Unlock() { k.entry.mu.Unlock() }

// release 从全局表中摘除该 key 的条目，避免 key 无限增长。
// 应在不再使用该锁时调用（通常在 Unlock 之后）。
func (k *keyedMutex) release() {
	keyedMutexMu.Lock()
	defer keyedMutexMu.Unlock()

	k.entry.refs--
	if k.entry.refs <= 0 {
		if cur, ok := keyedMutexTable[k.key]; ok && cur == k.entry {
			delete(keyedMutexTable, k.key)
		}
	}
}
