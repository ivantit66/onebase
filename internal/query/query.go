package query

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// CompileOpts holds options for query compilation including register metadata
// needed to resolve virtual table references (Остатки, Обороты, СрезПоследних, …).
type CompileOpts struct {
	Params      map[string]any
	Registers   []*metadata.Register
	InfoRegs    []*metadata.InfoRegister
	AccountRegs []*metadata.AccountRegister
	Entities    []*metadata.Entity
	// Dialect selects the SQL flavour. nil = PgDialect (default).
	Dialect storage.Dialect
}

func dialectOrDefault(d storage.Dialect) storage.Dialect {
	if d == nil {
		return storage.PgDialect{}
	}
	return d
}

// Result holds compiled PostgreSQL SQL and positional arguments.
type Result struct {
	SQL  string
	Args []any
}

// Compile translates a 1C-style query to PostgreSQL SQL.
func Compile(src string, opts CompileOpts) (Result, error) {
	return translate(tokenize(src), opts)
}

// --- tokenizer ---

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tDot
	tComma
	tLParen
	tRParen
	tParam
	tStr
	tNum
	tOp
	tStar
)

type tok struct {
	kind tokKind
	val  string
}

func tokenize(src string) []tok {
	var out []tok
	runes := []rune(src)
	n := len(runes)
	i := 0
	for i < n {
		ch := runes[i]
		if unicode.IsSpace(ch) {
			i++
			continue
		}
		switch {
		case ch == '&':
			i++
			j := i
			for i < n && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			out = append(out, tok{tParam, string(runes[j:i])})
		case ch == '"':
			i++
			j := i
			for i < n && runes[i] != '"' {
				i++
			}
			out = append(out, tok{tStr, string(runes[j:i])})
			if i < n {
				i++
			}
		case ch == '\'':
			i++
			j := i
			for i < n && runes[i] != '\'' {
				i++
			}
			out = append(out, tok{tStr, string(runes[j:i])})
			if i < n {
				i++
			}
		case ch == '.':
			out = append(out, tok{tDot, "."})
			i++
		case ch == ',':
			out = append(out, tok{tComma, ","})
			i++
		case ch == '(':
			out = append(out, tok{tLParen, "("})
			i++
		case ch == ')':
			out = append(out, tok{tRParen, ")"})
			i++
		case ch == '*':
			out = append(out, tok{tStar, "*"})
			i++
		case ch == '<':
			if i+1 < n && runes[i+1] == '>' {
				out = append(out, tok{tOp, "<>"})
				i += 2
			} else if i+1 < n && runes[i+1] == '=' {
				out = append(out, tok{tOp, "<="})
				i += 2
			} else {
				out = append(out, tok{tOp, "<"})
				i++
			}
		case ch == '>':
			if i+1 < n && runes[i+1] == '=' {
				out = append(out, tok{tOp, ">="})
				i += 2
			} else {
				out = append(out, tok{tOp, ">"})
				i++
			}
		case ch == '!' && i+1 < n && runes[i+1] == '=':
			out = append(out, tok{tOp, "<>"})
			i += 2
		case ch == '=' || ch == '+' || ch == '-' || ch == '/':
			out = append(out, tok{tOp, string(ch)})
			i++
		case unicode.IsLetter(ch) || ch == '_':
			j := i
			for i < n && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			out = append(out, tok{tIdent, string(runes[j:i])})
		case unicode.IsDigit(ch):
			j := i
			for i < n && (unicode.IsDigit(runes[i]) || runes[i] == '.') {
				i++
			}
			out = append(out, tok{tNum, string(runes[j:i])})
		default:
			i++
		}
	}
	out = append(out, tok{tEOF, ""})
	return out
}

// --- source type mapping ---

var sourcePrefix = map[string]string{
	"РЕГИСТРНАКОПЛЕНИЯ":    "рег_",
	"ACCUMULATIONREGISTER": "рег_",
	"РЕГИСТРСВЕДЕНИЙ":      "инфо_",
	"INFORMATIONREGISTER":  "инфо_",
	"РЕГИСТРБУХГАЛТЕРИИ":   "акк_",
	"ACCOUNTINGREGISTER":   "акк_",
	"СПРАВОЧНИК":           "",
	"CATALOG":              "",
	"ДОКУМЕНТ":             "",
	"DOCUMENT":             "",
}

func isSourceType(upper string) bool {
	_, ok := sourcePrefix[upper]
	return ok
}

func isAccumRegType(upper string) bool {
	return upper == "РЕГИСТРНАКОПЛЕНИЯ" || upper == "ACCUMULATIONREGISTER"
}

func isInfoRegType(upper string) bool {
	return upper == "РЕГИСТРСВЕДЕНИЙ" || upper == "INFORMATIONREGISTER"
}

func isAccountRegType(upper string) bool {
	return upper == "РЕГИСТРБУХГАЛТЕРИИ" || upper == "ACCOUNTINGREGISTER"
}

func sourceToTable(typeUpper, entityName string) string {
	return sourcePrefix[typeUpper] + strings.ToLower(entityName)
}

// --- virtual table kind maps ---

var accumVTKinds = map[string]string{
	"ОСТАТКИ":               "balances",
	"BALANCES":              "balances",
	"ОБОРОТЫ":               "turnovers",
	"TURNOVERS":             "turnovers",
	"ОСТАТКИИОБОРОТЫ":       "balances_turnovers",
	"BALANCESANDTURNOVERS":  "balances_turnovers",
}

var infoVTKinds = map[string]string{
	"СРЕЗПОСЛЕДНИХ": "last_slice",
	"LASTSLICE":     "last_slice",
	"СРЕЗПЕРВЫХ":    "first_slice",
	"FIRSTSLICE":    "first_slice",
}

// --- keyword mapping ---

var kwMap = map[string]string{
	// Russian structural keywords
	"ВЫБРАТЬ":       "SELECT",
	"РАЗЛИЧНЫЕ":     "DISTINCT",
	"ИЗ":            "FROM",
	"ГДЕ":           "WHERE",
	"СГРУППИРОВАТЬ": "GROUP",
	"УПОРЯДОЧИТЬ":   "ORDER",
	"ПО":            "ON", // standalone ПО without СГРУППИРОВАТЬ/УПОРЯДОЧИТЬ is always JOIN ON
	"ИМЕЯ":          "HAVING",
	"КАК":           "AS",
	"И":             "AND",
	"ИЛИ":           "OR",
	"НЕ":            "NOT",
	"ВЫБОР":         "CASE",
	"КОГДА":         "WHEN",
	"ТОГДА":         "THEN",
	"ИНАЧЕ":         "ELSE",
	"КОНЕЦ":         "END",
	"УБЫВ":          "DESC",
	"ВОЗР":          "ASC",
	"ЕСТЬ":          "IS",
	"ПУСТО":         "NULL",
	"В":             "IN",
	"ОБЪЕДИНИТЬ":    "UNION",
	"ВСЕ":           "ALL",
	// JOIN keywords (Russian)
	"ВНУТРЕННЕЕ": "INNER",
	"ЛЕВОЕ":      "LEFT",
	"ПРАВОЕ":     "RIGHT",
	"ПОЛНОЕ":     "FULL",
	"СОЕДИНЕНИЕ": "JOIN",
	// English pass-through
	"SELECT":   "SELECT",
	"DISTINCT": "DISTINCT",
	"FROM":     "FROM",
	"WHERE":    "WHERE",
	"GROUP":    "GROUP",
	"ORDER":    "ORDER",
	"BY":       "BY",
	"ON":       "ON",
	"HAVING":   "HAVING",
	"AS":       "AS",
	"AND":      "AND",
	"OR":       "OR",
	"NOT":      "NOT",
	"CASE":     "CASE",
	"WHEN":     "WHEN",
	"THEN":     "THEN",
	"ELSE":     "ELSE",
	"END":      "END",
	"DESC":     "DESC",
	"ASC":      "ASC",
	"IS":       "IS",
	"NULL":     "NULL",
	"IN":       "IN",
	"UNION":    "UNION",
	"ALL":      "ALL",
	// JOIN keywords (English pass-through)
	"INNER": "INNER",
	"LEFT":  "LEFT",
	"RIGHT": "RIGHT",
	"FULL":  "FULL",
	"OUTER": "OUTER",
	"JOIN":  "JOIN",
	"CROSS": "CROSS",
}

var aggFuncs = map[string]string{
	"СУММА":      "SUM",
	"КОЛИЧЕСТВО": "COUNT",
	"МИНИМУМ":    "MIN",
	"МАКСИМУМ":   "MAX",
	"СРЕДНЕЕ":    "AVG",
	"SUM":        "SUM",
	"COUNT":      "COUNT",
	"MIN":        "MIN",
	"MAX":        "MAX",
	"AVG":        "AVG",
}

func sqlKW(ident string) (string, bool) {
	kw, ok := kwMap[strings.ToUpper(ident)]
	return kw, ok
}

func sqlAgg(ident string) (string, bool) {
	kw, ok := aggFuncs[strings.ToUpper(ident)]
	return kw, ok
}

// --- translator ---

type querySection int

const (
	sectionSelect  querySection = iota
	sectionFrom
	sectionWhere
	sectionGroupBy
	sectionOrderBy
	sectionHaving
	sectionOther
)

type refDimInfo struct {
	fieldName  string // lowercase dim name: "номенклатура"
	idCol      string // DB column: "номенклатура_id"
	joinAlias  string // SQL alias for auto-JOIN: "ref_номенклатура"
	joinTable  string // referenced catalog table: "номенклатура"
	isVT       bool   // true: VT outer query, JOIN ON uses fieldName instead of idCol
	refIsDoc   bool   // true: referenced entity is a document → display "номер", not "наименование"
}

func (rd refDimInfo) displayCol() string {
	if rd.refIsDoc {
		return rd.joinAlias + ".номер"
	}
	return rd.joinAlias + ".наименование"
}

type translator struct {
	tokens      []tok
	pos         int
	args        []any
	params      map[string]int // param name → 1-based index in args (0 = NULL sentinel)
	paramValues map[string]any
	opts        CompileOpts
	parts       []string
	prevWasDot  bool              // true after emitting "." — used to resolve .Ссылка → .id
	colMap      map[string]string // lowercase field name → actual column name (for reference dims)
	refDims     []refDimInfo      // reference dimensions with auto-JOIN info
	mainTable   string            // main FROM table (set when source is emitted)
	section     querySection      // current clause context
}

func (tr *translator) peek(offset int) tok {
	i := tr.pos + offset
	if i >= len(tr.tokens) {
		return tok{tEOF, ""}
	}
	return tr.tokens[i]
}

func (tr *translator) advance() tok {
	t := tr.tokens[tr.pos]
	tr.pos++
	return t
}

func (tr *translator) emit(s string) {
	tr.parts = append(tr.parts, s)
}

func (tr *translator) build() string {
	var sb strings.Builder
	for i, p := range tr.parts {
		if i > 0 {
			prev := tr.parts[i-1]
			noBefore := p == "," || p == ")" || p == "." || p == "("
			noAfter := prev == "(" || prev == "."
			if !noBefore && !noAfter {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(p)
	}
	return sb.String()
}

// addParam registers a named parameter and returns its SQL placeholder.
// If the value is []any (DSL array converted by unwrapArrayParams), it expands
// to a comma-joined list of placeholders suitable for IN (...) clauses.
func (tr *translator) addParam(name string) string {
	v := tr.paramValues[name]
	if v == nil {
		return "NULL"
	}
	d := dialectOrDefault(tr.opts.Dialect)

	// Expand array parameters for IN clauses: IN (&Param) → IN ($1, $2, $3)
	if items, ok := v.([]any); ok {
		if len(items) == 0 {
			return "NULL"
		}
		var placeholders []string
		for _, item := range items {
			// Skip nil and empty strings — they'd cause cast mismatch (uuid = text)
			if item == nil {
				continue
			}
			if s, ok := item.(string); ok && s == "" {
				continue
			}
			tr.args = append(tr.args, item)
			ph := d.Placeholder(len(tr.args))
			if d.Name() == "postgres" {
				ph += castSuffix(d, item)
			}
			placeholders = append(placeholders, ph)
		}
		if len(placeholders) == 0 {
			return "NULL"
		}
		return strings.Join(placeholders, ", ")
	}

	if d.Name() != "postgres" {
		// SQLite uses positional `?` — each occurrence consumes a separate arg slot.
		// Unlike PostgreSQL's $N, `?` cannot be reused for the same param.
		tr.args = append(tr.args, v)
		return d.Placeholder(len(tr.args))
	}
	// PostgreSQL: $N references can be shared — deduplicate by param name.
	if _, exists := tr.params[name]; !exists {
		tr.args = append(tr.args, v)
		tr.params[name] = len(tr.args)
	}
	return d.Placeholder(tr.params[name]) + castSuffix(d, v)
}

// castSuffix returns the explicit cast suffix for v on the active dialect.
// PG benefits from "::text"/"::numeric" hints; SQLite ignores types so we
// return "".
func castSuffix(d storage.Dialect, v any) string {
	if d.Name() != "postgres" {
		return ""
	}
	return pgCast(v)
}

// parseVTArgs collects argument groups from a virtual-table call.
// The opening "(" has already been consumed; this method consumes until the matching ")".
func (tr *translator) parseVTArgs() [][]tok {
	var groups [][]tok
	var current []tok
	depth := 0
	for {
		t := tr.advance()
		if t.kind == tEOF {
			break
		}
		switch {
		case t.kind == tLParen:
			depth++
			current = append(current, t)
		case t.kind == tRParen && depth > 0:
			depth--
			current = append(current, t)
		case t.kind == tRParen: // depth == 0, closing paren
			groups = append(groups, current)
			return groups
		case t.kind == tComma && depth == 0:
			groups = append(groups, current)
			current = nil
		default:
			current = append(current, t)
		}
	}
	return groups
}

// translateFilterTokens translates a token slice to a SQL expression fragment,
// resolving &params through the translator's shared state.
func (tr *translator) translateFilterTokens(tokens []tok) string {
	var parts []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch t.kind {
		case tParam:
			parts = append(parts, tr.addParam(t.val))
		case tIdent:
			upper := strings.ToUpper(t.val)
			if kw, ok := kwMap[upper]; ok {
				parts = append(parts, kw)
			} else if agg, ok := aggFuncs[upper]; ok && i+1 < len(tokens) && tokens[i+1].kind == tLParen {
				parts = append(parts, agg)
			} else {
				lower := strings.ToLower(t.val)
				if col, ok := tr.colMap[lower]; ok {
					parts = append(parts, col)
				} else {
					parts = append(parts, lower)
				}
			}
		case tStr:
			parts = append(parts, "'"+strings.ReplaceAll(t.val, "'", "''")+"'")
		case tNum, tOp, tStar:
			parts = append(parts, t.val)
		case tComma:
			parts = append(parts, ",")
		case tLParen:
			parts = append(parts, "(")
		case tRParen:
			parts = append(parts, ")")
		case tDot:
			parts = append(parts, ".")
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func (tr *translator) findRegister(name string) *metadata.Register {
	nl := strings.ToLower(name)
	for _, r := range tr.opts.Registers {
		if strings.ToLower(r.Name) == nl {
			return r
		}
	}
	return nil
}

func (tr *translator) findInfoRegister(name string) *metadata.InfoRegister {
	nl := strings.ToLower(name)
	for _, r := range tr.opts.InfoRegs {
		if strings.ToLower(r.Name) == nl {
			return r
		}
	}
	return nil
}

func dimCols(dims []metadata.Field) []string {
	names := make([]string, len(dims))
	for i, d := range dims {
		names[i] = metadata.ColumnName(d)
	}
	return names
}

// dimSelCols returns SELECT list entries for dimension fields, aliasing
// reference columns (e.g. "номенклатура_id AS номенклатура") so that outer
// queries and DSL code can reference dimensions by their logical names.
func dimSelCols(dims []metadata.Field) []string {
	result := make([]string, len(dims))
	for i, d := range dims {
		col := metadata.ColumnName(d)
		name := strings.ToLower(d.Name)
		if col != name {
			result[i] = col + " AS " + name
		} else {
			result[i] = col
		}
	}
	return result
}

// buildAccumVT generates a SQL subquery for an accumulation register virtual table.
func (tr *translator) buildAccumVT(vtKind, regName string, args [][]tok) (subq, alias string, err error) {
	reg := tr.findRegister(regName)
	if reg == nil {
		return "", "", fmt.Errorf("accumulation register %q not found; pass Registers in CompileOpts", regName)
	}
	switch vtKind {
	case "balances":
		return tr.genBalances(reg, args)
	case "turnovers":
		return tr.genTurnovers(reg, args)
	case "balances_turnovers":
		return tr.genBalancesAndTurnovers(reg, args)
	}
	return "", "", fmt.Errorf("unknown accumulation virtual table: %s", vtKind)
}

// buildInfoVT generates a SQL subquery for an information register virtual table.
func (tr *translator) buildInfoVT(vtKind, regName string, args [][]tok) (subq, alias string, err error) {
	ir := tr.findInfoRegister(regName)
	if ir == nil {
		return "", "", fmt.Errorf("information register %q not found; pass InfoRegs in CompileOpts", regName)
	}
	switch vtKind {
	case "last_slice":
		return tr.genLastSlice(ir, args)
	case "first_slice":
		return tr.genFirstSlice(ir, args)
	}
	return "", "", fmt.Errorf("unknown information virtual table: %s", vtKind)
}

func (tr *translator) findAccountRegister(name string) *metadata.AccountRegister {
	nl := strings.ToLower(name)
	for _, r := range tr.opts.AccountRegs {
		if strings.ToLower(r.Name) == nl {
			return r
		}
	}
	return nil
}

// buildAccountVT generates a SQL subquery for an accounting register virtual table.
func (tr *translator) buildAccountVT(vtKind, regName string, args [][]tok) (subq, alias string, err error) {
	ar := tr.findAccountRegister(regName)
	if ar == nil {
		return "", "", fmt.Errorf("accounting register %q not found; pass AccountRegs in CompileOpts", regName)
	}
	switch vtKind {
	case "balances":
		return tr.genAccountBalances(ar, args)
	case "turnovers":
		return tr.genAccountTurnovers(ar, args)
	}
	return "", "", fmt.Errorf("unknown accounting virtual table: %s", vtKind)
}

func (tr *translator) genAccountBalances(ar *metadata.AccountRegister, args [][]tok) (string, string, error) {
	table := metadata.AccountRegTableName(ar.Name)
	alias := "остатки_" + strings.ToLower(ar.Name)

	var resCols []string
	for _, r := range ar.Resources {
		col := strings.ToLower(r.Name)
		resCols = append(resCols,
			"COALESCE(SUM(CASE WHEN r.счётдт = a.code THEN r."+col+" ELSE 0 END),0) AS "+col+"_дт",
			"COALESCE(SUM(CASE WHEN r.счёткт = a.code THEN r."+col+" ELSE 0 END),0) AS "+col+"_кт",
			"COALESCE(SUM(CASE WHEN r.счётдт = a.code THEN r."+col+" ELSE -r."+col+" END),0) AS "+col+"остаток",
		)
	}

	selectList := "a.code AS счёт, a.name AS наименование"
	if len(resCols) > 0 {
		selectList += ", " + strings.Join(resCols, ", ")
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(selectList)
	sb.WriteString(" FROM _accounts a LEFT JOIN ")
	sb.WriteString(table)
	sb.WriteString(" r ON (r.счётдт = a.code OR r.счёткт = a.code)")

	var conds []string
	if len(args) > 0 && len(args[0]) > 0 {
		if s := tr.translateFilterTokens(args[0]); s != "" && s != "NULL" {
			conds = append(conds, "r.period <= "+s)
		}
	}
	if len(conds) > 0 {
		sb.WriteString(" AND ")
		sb.WriteString(strings.Join(conds, " AND "))
	}
	if len(args) > 1 && len(args[1]) > 0 {
		if s := tr.translateFilterTokens(args[1]); s != "" {
			sb.WriteString(" WHERE (")
			sb.WriteString(s)
			sb.WriteString(")")
		}
	}
	sb.WriteString(" GROUP BY a.code, a.name")

	return sb.String(), alias, nil
}

func (tr *translator) genAccountTurnovers(ar *metadata.AccountRegister, args [][]tok) (string, string, error) {
	table := metadata.AccountRegTableName(ar.Name)
	alias := "обороты_" + strings.ToLower(ar.Name)

	var resCols []string
	for _, r := range ar.Resources {
		col := strings.ToLower(r.Name)
		resCols = append(resCols,
			"COALESCE(SUM(CASE WHEN r.счётдт = a.code THEN r."+col+" ELSE 0 END),0) AS "+col+"_дт",
			"COALESCE(SUM(CASE WHEN r.счёткт = a.code THEN r."+col+" ELSE 0 END),0) AS "+col+"_кт",
		)
	}

	selectList := "a.code AS счёт, a.name AS наименование"
	if len(resCols) > 0 {
		selectList += ", " + strings.Join(resCols, ", ")
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(selectList)
	sb.WriteString(" FROM _accounts a LEFT JOIN ")
	sb.WriteString(table)
	sb.WriteString(" r ON (r.счётдт = a.code OR r.счёткт = a.code)")

	var conds []string
	if len(args) > 0 && len(args[0]) > 0 {
		if s := tr.translateFilterTokens(args[0]); s != "" && s != "NULL" {
			conds = append(conds, "r.period >= "+s)
		}
	}
	if len(args) > 1 && len(args[1]) > 0 {
		if s := tr.translateFilterTokens(args[1]); s != "" && s != "NULL" {
			conds = append(conds, "r.period <= "+s)
		}
	}
	if len(conds) > 0 {
		sb.WriteString(" AND ")
		sb.WriteString(strings.Join(conds, " AND "))
	}
	sb.WriteString(" GROUP BY a.code, a.name HAVING SUM(CASE WHEN r.id IS NOT NULL THEN 1 ELSE 0 END) > 0")

	return sb.String(), alias, nil
}

func (tr *translator) genBalances(reg *metadata.Register, args [][]tok) (string, string, error) {
	tableName := metadata.RegisterTableName(reg.Name)
	alias := "остатки_" + strings.ToLower(reg.Name)
	dims := dimCols(reg.Dimensions)        // actual col names for GROUP BY / WHERE
	selDims := dimSelCols(reg.Dimensions)  // aliased names for SELECT

	var cols []string
	cols = append(cols, selDims...)
	for _, r := range reg.Resources {
		col := strings.ToLower(r.Name)
		cols = append(cols,
			"SUM(CASE WHEN вид_движения = 'Приход' THEN "+col+" ELSE -"+col+" END) AS "+col+"остаток")
	}
	// атрибуты — не часть ключа измерения, но должны быть
	// доступны в outer SELECT/WHERE. Берём MIN(col) — детерминированно
	// и работает в обоих диалектах (TEXT/UUID сравнимы лексикографически).
	cols = append(cols, attributeAggCols(reg.Attributes)...)

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(cols, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(tableName)

	var conds []string
	if len(args) > 0 && len(args[0]) > 0 {
		// момент времени документа — особое условие.
		if mt := tr.firstArgMoment(args[0]); mt != nil {
			conds = append(conds, tr.momentTimeCondition(mt))
		} else if s := tr.translateFilterTokens(args[0]); s != "" && s != "NULL" {
			conds = append(conds, "period <= "+s)
		}
	}
	if len(args) > 1 && len(args[1]) > 0 {
		if s := tr.translateFilterTokens(args[1]); s != "" {
			conds = append(conds, s)
		}
	}
	if len(conds) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(conds, " AND "))
	}
	if len(dims) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(dims, ", "))
	}

	return sb.String(), alias, nil
}

func (tr *translator) genTurnovers(reg *metadata.Register, args [][]tok) (string, string, error) {
	tableName := metadata.RegisterTableName(reg.Name)
	alias := "обороты_" + strings.ToLower(reg.Name)
	dims := dimCols(reg.Dimensions)
	selDims := dimSelCols(reg.Dimensions)

	var cols []string
	cols = append(cols, selDims...)
	for _, r := range reg.Resources {
		col := strings.ToLower(r.Name)
		cols = append(cols,
			"SUM(CASE WHEN вид_движения = 'Приход' THEN "+col+" ELSE 0 END) AS "+col+"приход",
			"SUM(CASE WHEN вид_движения = 'Расход' THEN "+col+" ELSE 0 END) AS "+col+"расход",
			"SUM(CASE WHEN вид_движения = 'Приход' THEN "+col+" ELSE -"+col+" END) AS "+col+"оборот",
		)
	}
	cols = append(cols, attributeAggCols(reg.Attributes)...)

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(cols, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(tableName)

	var conds []string
	if len(args) > 0 && len(args[0]) > 0 {
		if s := tr.translateFilterTokens(args[0]); s != "" && s != "NULL" {
			conds = append(conds, "period >= "+s)
		}
	}
	if len(args) > 1 && len(args[1]) > 0 {
		if s := tr.translateFilterTokens(args[1]); s != "" && s != "NULL" {
			conds = append(conds, "period <= "+s)
		}
	}
	if len(args) > 2 && len(args[2]) > 0 {
		if s := tr.translateFilterTokens(args[2]); s != "" {
			conds = append(conds, s)
		}
	}
	if len(conds) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(conds, " AND "))
	}
	if len(dims) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(dims, ", "))
	}

	return sb.String(), alias, nil
}

func (tr *translator) genBalancesAndTurnovers(reg *metadata.Register, args [][]tok) (string, string, error) {
	tableName := metadata.RegisterTableName(reg.Name)
	alias := "остаткиоборотов_" + strings.ToLower(reg.Name)
	dims := dimCols(reg.Dimensions)
	selDims := dimSelCols(reg.Dimensions)

	var startSQL, endSQL, filterSQL string
	if len(args) > 0 && len(args[0]) > 0 {
		if s := tr.translateFilterTokens(args[0]); s != "NULL" {
			startSQL = s
		}
	}
	if len(args) > 1 && len(args[1]) > 0 {
		if s := tr.translateFilterTokens(args[1]); s != "NULL" {
			endSQL = s
		}
	}
	if len(args) > 2 && len(args[2]) > 0 {
		filterSQL = tr.translateFilterTokens(args[2])
	}

	var cols []string
	cols = append(cols, selDims...)
	for _, r := range reg.Resources {
		col := strings.ToLower(r.Name)
		if startSQL != "" {
			cols = append(cols,
				"SUM(CASE WHEN вид_движения = 'Приход' AND period < "+startSQL+
					" THEN "+col+" WHEN вид_движения = 'Расход' AND period < "+startSQL+
					" THEN -"+col+" ELSE 0 END) AS "+col+"начальный")
		}
		periodCond := ""
		if startSQL != "" && endSQL != "" {
			periodCond = " AND period >= " + startSQL + " AND period <= " + endSQL
		} else if startSQL != "" {
			periodCond = " AND period >= " + startSQL
		} else if endSQL != "" {
			periodCond = " AND period <= " + endSQL
		}
		cols = append(cols,
			"SUM(CASE WHEN вид_движения = 'Приход'"+periodCond+" THEN "+col+" ELSE 0 END) AS "+col+"приход",
			"SUM(CASE WHEN вид_движения = 'Расход'"+periodCond+" THEN "+col+" ELSE 0 END) AS "+col+"расход",
		)
		if endSQL != "" {
			cols = append(cols,
				"SUM(CASE WHEN вид_движения = 'Приход' AND period <= "+endSQL+
					" THEN "+col+" WHEN вид_движения = 'Расход' AND period <= "+endSQL+
					" THEN -"+col+" ELSE 0 END) AS "+col+"конечный")
		}
	}
	cols = append(cols, attributeAggCols(reg.Attributes)...)

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(cols, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(tableName)

	var conds []string
	if endSQL != "" {
		conds = append(conds, "period <= "+endSQL)
	}
	if filterSQL != "" {
		conds = append(conds, filterSQL)
	}
	if len(conds) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(conds, " AND "))
	}
	if len(dims) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(dims, ", "))
	}

	return sb.String(), alias, nil
}

// attributeAggCols returns "MIN(col) AS col" expressions for each attribute.
// Атрибуты не часть ключа измерения, поэтому в SELECT их нельзя оставлять
// без агрегата (SQL ошибся бы). MIN — детерминированный выбор. Если атрибут
// варьируется в пределах одного значения измерения, MIN отдаст
// лексикографически минимальное; в стабильных учётных моделях такое
// нехарактерно.
func attributeAggCols(attrs []metadata.Field) []string {
	out := make([]string, 0, len(attrs))
	for _, a := range attrs {
		col := metadata.ColumnName(a)
		out = append(out, "MIN("+col+") AS "+col)
	}
	return out
}

// genInfoSlice generates SrezPoslednih/SrezPervyh SQL.
// direction: "DESC" → СрезПоследних (last); "ASC" → СрезПервых (first).
// For non-periodic info registers, DISTINCT semantics are unnecessary —
// we just SELECT FROM the table with WHERE filter.
func (tr *translator) genInfoSlice(ir *metadata.InfoRegister, args [][]tok, direction string, aliasPrefix string) (string, string, error) {
	d := dialectOrDefault(tr.opts.Dialect)
	tableName := metadata.InfoRegTableName(ir.Name)
	alias := aliasPrefix + strings.ToLower(ir.Name)
	dims := dimCols(ir.Dimensions)
	selDims := dimSelCols(ir.Dimensions)

	var resCols []string
	for _, r := range ir.Resources {
		resCols = append(resCols, strings.ToLower(r.Name))
	}

	periodOp := "<="
	if direction == "ASC" {
		periodOp = ">="
	}

	var conds []string
	if ir.Periodic && len(args) > 0 && len(args[0]) > 0 {
		// МоментВремени для info-регистра — берём только Period,
		// recorder в info-таблицах не используется для исключения.
		if mt := tr.firstArgMoment(args[0]); mt != nil {
			d := dialectOrDefault(tr.opts.Dialect)
			p, _ := mt.PointInTime()
			tr.args = append(tr.args, p)
			conds = append(conds, "period "+periodOp+" "+d.Placeholder(len(tr.args)))
		} else if s := tr.translateFilterTokens(args[0]); s != "" && s != "NULL" {
			conds = append(conds, "period "+periodOp+" "+s)
		}
	}
	filterIdx := 1
	if !ir.Periodic {
		filterIdx = 0
	}
	if len(args) > filterIdx && len(args[filterIdx]) > 0 {
		if s := tr.translateFilterTokens(args[filterIdx]); s != "" {
			conds = append(conds, s)
		}
	}
	where := strings.Join(conds, " AND ")

	if ir.Periodic && len(dims) > 0 {
		// SELECT (per dim) the row with latest/earliest period — uses
		// d.LatestPerKey, which gives DISTINCT ON for PG and ROW_NUMBER()
		// OVER (PARTITION BY) for SQLite.
		// selDims: aliased col names for SELECT; dims: actual col names for PARTITION BY.
		selectCols := append([]string{"period"}, append(append([]string{}, selDims...), resCols...)...)
		return d.LatestPerKey(selectCols, dims, []string{"period " + direction}, tableName, "", where), alias, nil
	}

	// Non-periodic or no dimensions — plain SELECT.
	var sb strings.Builder
	var allCols []string
	allCols = append(allCols, selDims...)
	allCols = append(allCols, resCols...)
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(allCols, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(tableName)
	if where != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(where)
	}
	return sb.String(), alias, nil
}

func (tr *translator) genLastSlice(ir *metadata.InfoRegister, args [][]tok) (string, string, error) {
	return tr.genInfoSlice(ir, args, "DESC", "срезпоследних_")
}

func (tr *translator) genFirstSlice(ir *metadata.InfoRegister, args [][]tok) (string, string, error) {
	return tr.genInfoSlice(ir, args, "ASC", "срезпервых_")
}

// --- reference dimension auto-join ---

// preScanRefDims pre-scans tokens to find the first simple (non-virtual) register source
// and returns reference dimension info for auto-JOIN generation. Only fields actually
// referenced in the query produce JOINs to avoid ambiguous-column errors.
func preScanRefDims(tokens []tok, opts CompileOpts) []refDimInfo {
	return filterUsedRefDims(preScanAllRefDims(tokens, opts), tokens)
}

func preScanAllRefDims(tokens []tok, opts CompileOpts) []refDimInfo {
	for i := 0; i+2 < len(tokens); i++ {
		t := tokens[i]
		if t.kind != tIdent {
			continue
		}
		upper := strings.ToUpper(t.val)
		if !isSourceType(upper) || tokens[i+1].kind != tDot || tokens[i+2].kind != tIdent {
			continue
		}
		regName := tokens[i+2].val
		// VT source: TypeName.EntityName.VTName(...)
		if i+3 < len(tokens) && tokens[i+3].kind == tDot {
			if isAccumRegType(upper) {
				for _, reg := range opts.Registers {
					if strings.EqualFold(reg.Name, regName) {
						return buildVTRefDimInfos(append(reg.Dimensions, reg.Attributes...))
					}
				}
			} else if isInfoRegType(upper) {
				for _, ir := range opts.InfoRegs {
					if strings.EqualFold(ir.Name, regName) {
						return buildVTRefDimInfos(ir.Dimensions)
					}
				}
			}
			return nil
		}
		// Regular source
		if isAccumRegType(upper) {
			for _, reg := range opts.Registers {
				if strings.EqualFold(reg.Name, regName) {
					return buildRefDimInfos(append(reg.Dimensions, reg.Attributes...))
				}
			}
		} else if isInfoRegType(upper) {
			for _, ir := range opts.InfoRegs {
				if strings.EqualFold(ir.Name, regName) {
					return buildRefDimInfos(ir.Dimensions)
				}
			}
		}
		// Document / Catalog sources
		for _, ent := range opts.Entities {
			if strings.EqualFold(ent.Name, regName) {
				return buildRefDimInfosWithEntities(ent.Fields, opts.Entities)
			}
		}
	}
	return nil
}

// filterUsedRefDims removes refDims for fields not referenced in the query tokens.
// This avoids unnecessary JOINs that can cause "ambiguous column name" errors when
// a joined table shares column names (e.g. дата) with the main table.
func filterUsedRefDims(dims []refDimInfo, tokens []tok) []refDimInfo {
	if len(dims) == 0 {
		return nil
	}
	used := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		if t.kind == tIdent {
			used[strings.ToLower(t.val)] = true
		}
	}
	var out []refDimInfo
	for _, rd := range dims {
		if used[rd.fieldName] {
			out = append(out, rd)
		}
	}
	return out
}

// buildVTRefDimInfos creates refDimInfos for VT outer queries where the subquery
// aliases _id columns to logical names, so JOIN ON uses fieldName instead of idCol.
func buildVTRefDimInfos(dims []metadata.Field) []refDimInfo {
	var result []refDimInfo
	for _, d := range dims {
		if d.RefEntity != "" {
			fn := strings.ToLower(d.Name)
			result = append(result, refDimInfo{
				fieldName: fn,
				idCol:     fn, // VT aliased from _id
				joinAlias: "ref_" + fn,
				joinTable: strings.ToLower(d.RefEntity),
				isVT:      true,
			})
		}
	}
	return result
}

func buildRefDimInfos(dims []metadata.Field) []refDimInfo {
	return buildRefDimInfosWithEntities(dims, nil)
}

func buildRefDimInfosWithEntities(dims []metadata.Field, entities []*metadata.Entity) []refDimInfo {
	var result []refDimInfo
	for _, d := range dims {
		if d.RefEntity != "" {
			rd := refDimInfo{
				fieldName: strings.ToLower(d.Name),
				idCol:     strings.ToLower(d.Name) + "_id",
				joinAlias: "ref_" + strings.ToLower(d.Name),
				joinTable: strings.ToLower(d.RefEntity),
			}
			for _, e := range entities {
				if strings.EqualFold(e.Name, d.RefEntity) && e.Kind == metadata.KindDocument {
					rd.refIsDoc = true
					break
				}
			}
			result = append(result, rd)
		}
	}
	return result
}

func (tr *translator) findRefDim(name string) *refDimInfo {
	for i := range tr.refDims {
		if tr.refDims[i].fieldName == name {
			return &tr.refDims[i]
		}
	}
	return nil
}

// emitVTSubquery emits a VT subquery with its alias, detects optional user alias (КАК),
// and adds auto-JOINs for reference dimensions using the correct alias.
func (tr *translator) emitVTSubquery(subq, defaultAlias string) {
	alias := defaultAlias
	if p := tr.peek(0); p.kind == tIdent {
		pUpper := strings.ToUpper(p.val)
		if pUpper == "КАК" || pUpper == "AS" {
			tr.advance()
			if a := tr.peek(0); a.kind == tIdent {
				alias = strings.ToLower(tr.advance().val)
			}
		}
	}
	tr.emit("(" + subq + ") AS " + alias)
	if tr.section == sectionFrom {
		for _, rd := range tr.refDims {
			if rd.isVT {
				tr.emit(fmt.Sprintf("LEFT JOIN %s %s ON %s.id = %s.%s",
					rd.joinTable, rd.joinAlias, rd.joinAlias, alias, rd.fieldName))
			}
		}
	}
}

// --- main translator loop ---

// buildColMap creates a mapping from lowercase field name to actual DB column name
// for reference-type fields. It scans tokens to find the specific source register
// so that reference dims from one register don't pollute queries against another.
func buildColMap(tokens []tok, opts CompileOpts) map[string]string {
	m := map[string]string{}
	addFields := func(fields []metadata.Field) {
		for _, f := range fields {
			name := strings.ToLower(f.Name)
			col := metadata.ColumnName(f)
			if col != name {
				m[name] = col
			}
		}
	}

	// Find the specific register being queried (same scan as preScanRefDims).
	vtSourceFound := false
	for i := 0; i+2 < len(tokens); i++ {
		t := tokens[i]
		if t.kind != tIdent {
			continue
		}
		upper := strings.ToUpper(t.val)
		if !isSourceType(upper) || tokens[i+1].kind != tDot || tokens[i+2].kind != tIdent {
			continue
		}
		if i+3 < len(tokens) && tokens[i+3].kind == tDot {
			vtSourceFound = true
			continue // skip virtual tables
		}
		regName := tokens[i+2].val
		if isAccumRegType(upper) {
			for _, reg := range opts.Registers {
				if strings.EqualFold(reg.Name, regName) {
					addFields(reg.Dimensions)
					addFields(reg.Resources)
					addFields(reg.Attributes)
					return m
				}
			}
		} else if isInfoRegType(upper) {
			for _, ir := range opts.InfoRegs {
				if strings.EqualFold(ir.Name, regName) {
					addFields(ir.Dimensions)
					addFields(ir.Resources)
					return m
				}
			}
		}
		// Found a source but not a register — stop searching (entity query needs no colMap)
		return m
	}

	// VT query: the subquery already aliases _id cols to DSL names, so the outer
	// query must reference those logical names — return empty colMap.
	if vtSourceFound {
		return m
	}

	// Fallback: no explicit register source — build from all (rare, backward-compat)
	for _, reg := range opts.Registers {
		addFields(reg.Dimensions)
		addFields(reg.Resources)
		addFields(reg.Attributes)
	}
	for _, ir := range opts.InfoRegs {
		addFields(ir.Dimensions)
		addFields(ir.Resources)
	}
	return m
}

func translate(tokens []tok, opts CompileOpts) (Result, error) {
	if opts.Params == nil {
		opts.Params = map[string]any{}
	}
	// расширяем НачалоДня/Год/Месяц/... в SQL-эквиваленты
	// до основной трансляции, чтобы остальные шаги ничего не знали о них.
	tokens = rewriteDateFuncs(tokens, dialectName(opts.Dialect))
	tr := &translator{
		tokens:      tokens,
		params:      map[string]int{},
		paramValues: opts.Params,
		opts:        opts,
		colMap:      buildColMap(tokens, opts),
		refDims:     preScanRefDims(tokens, opts),
		section:     sectionOther,
	}
	for {
		t := tr.peek(0)
		if t.kind == tEOF {
			break
		}
		upper := strings.ToUpper(t.val)

		// Source type: TypeName.EntityName[.VirtualTable(args)] → table or subquery
		if t.kind == tIdent && isSourceType(upper) &&
			tr.peek(1).kind == tDot && tr.peek(2).kind == tIdent {

			// Check for virtual table: TypeName.EntityName.VTName(...)
			if tr.peek(3).kind == tDot && tr.peek(4).kind == tIdent &&
				tr.peek(5).kind == tLParen {
				vt4Upper := strings.ToUpper(tr.peek(4).val)

				if vtKind, ok := accumVTKinds[vt4Upper]; ok && isAccumRegType(upper) {
					tr.advance() // TypeName
					tr.advance() // .
					regName := tr.advance().val
					tr.advance() // .
					tr.advance() // VTName
					tr.advance() // (
					vtArgs := tr.parseVTArgs()
					subq, alias, err := tr.buildAccumVT(vtKind, regName, vtArgs)
					if err != nil {
						return Result{}, err
					}
					tr.emitVTSubquery(subq, alias)
					continue
				}

				if vtKind, ok := infoVTKinds[vt4Upper]; ok && isInfoRegType(upper) {
					tr.advance() // TypeName
					tr.advance() // .
					regName := tr.advance().val
					tr.advance() // .
					tr.advance() // VTName
					tr.advance() // (
					vtArgs := tr.parseVTArgs()
					subq, alias, err := tr.buildInfoVT(vtKind, regName, vtArgs)
					if err != nil {
						return Result{}, err
					}
					tr.emitVTSubquery(subq, alias)
					continue
				}

				if vtKind, ok := accumVTKinds[vt4Upper]; ok && isAccountRegType(upper) {
					tr.advance() // TypeName
					tr.advance() // .
					regName := tr.advance().val
					tr.advance() // .
					tr.advance() // VTName
					tr.advance() // (
					vtArgs := tr.parseVTArgs()
					subq, alias, err := tr.buildAccountVT(vtKind, regName, vtArgs)
					if err != nil {
						return Result{}, err
					}
					tr.emitVTSubquery(subq, alias)
					continue
				}
			}

			// Regular source: TypeName.EntityName → table_name [+ КАК alias] [+ auto-JOINs]
			tr.advance()
			tr.advance()
			entity := tr.advance()
			tableName := sourceToTable(upper, entity.val)
			tr.mainTable = tableName
			tr.emit(tableName)
			// Consume optional КАК/AS alias before auto-JOINs
			if p := tr.peek(0); p.kind == tIdent {
				pUpper := strings.ToUpper(p.val)
				if pUpper == "КАК" || pUpper == "AS" {
					tr.advance()
					if a := tr.peek(0); a.kind == tIdent {
						tr.emit("AS " + strings.ToLower(tr.advance().val))
					}
				}
			}
			if tr.section == sectionFrom {
				for _, rd := range tr.refDims {
					tr.emit(fmt.Sprintf("LEFT JOIN %s %s ON %s.id = %s.%s",
						rd.joinTable, rd.joinAlias, rd.joinAlias, tableName, rd.idCol))
				}
			}
			continue
		}

		// Multi-word: СГРУППИРОВАТЬ ПО / УПОРЯДОЧИТЬ ПО
		if t.kind == tIdent && (upper == "СГРУППИРОВАТЬ" || upper == "УПОРЯДОЧИТЬ") {
			tr.advance()
			if upper == "УПОРЯДОЧИТЬ" {
				tr.section = sectionOrderBy
				tr.emit("ORDER BY")
			} else {
				tr.section = sectionGroupBy
				tr.emit("GROUP BY")
			}
			if tr.peek(0).kind == tIdent && strings.ToUpper(tr.peek(0).val) == "ПО" {
				tr.advance()
			}
			continue
		}

		// Parameter: &Name → $N or NULL
		if t.kind == tParam {
			tr.prevWasDot = false
			tr.advance()
			tr.emit(tr.addParam(t.val))
			continue
		}

		// String literal
		if t.kind == tStr {
			tr.prevWasDot = false
			tr.advance()
			tr.emit("'" + strings.ReplaceAll(t.val, "'", "''") + "'")
			continue
		}

		// Number / star / operator
		if t.kind == tNum || t.kind == tStar || t.kind == tOp {
			tr.prevWasDot = false
			tr.advance()
			tr.emit(t.val)
			continue
		}

		// Punctuation
		if t.kind == tComma || t.kind == tLParen || t.kind == tRParen {
			tr.prevWasDot = false
			tr.advance()
			tr.emit(t.val)
			continue
		}

		if t.kind == tDot {
			tr.advance()
			tr.emit(".")
			tr.prevWasDot = true
			continue
		}

		// Identifiers: aggregate function (only before "("), keyword, or lowercase field name
		if t.kind == tIdent {
			tr.advance()
			prevDot := tr.prevWasDot
			tr.prevWasDot = false
			// Ссылка / Reference → id (virtual primary-key field, like 1C).
			// Работает и после точки (Н.Ссылка → н.id), и без алиаса
			// (ВЫБРАТЬ Ссылка ИЗ Справочник.X → SELECT id FROM x).
			if up := strings.ToUpper(t.val); up == "ССЫЛКА" || up == "REFERENCE" || up == "REF" {
				tr.emit("id")
				continue
			}
			// Системные колонки регистра — PascalCase русские алиасы
			// (см. Работает и с префиксом (Х.Период), и без.
			if col, ok := systemColAlias(t.val); ok {
				tr.emit(col)
				continue
			}
			if agg, ok := sqlAgg(t.val); ok && tr.peek(0).kind == tLParen {
				tr.emit(agg)
			} else if kw, ok := sqlKW(t.val); ok {
				tr.emit(kw)
				// track clause context
				switch kw {
				case "SELECT":
					tr.section = sectionSelect
				case "FROM":
					tr.section = sectionFrom
				case "WHERE":
					tr.section = sectionWhere
				case "HAVING":
					tr.section = sectionHaving
				}
			} else {
				lower := strings.ToLower(t.val)
				nextIsDot := tr.peek(0).kind == tDot
				if tr.section == sectionFrom && !prevDot {
					tr.emit(lower)
				} else if rd := tr.findRefDim(lower); rd != nil && !prevDot {
					if nextIsDot {
						tr.emit(rd.joinAlias)
					} else {
						switch tr.section {
						case sectionSelect:
							tr.emit(rd.displayCol())
							if p := strings.ToUpper(tr.peek(0).val); p != "КАК" && p != "AS" {
								tr.emit("AS")
								tr.emit(rd.fieldName)
							}
						case sectionGroupBy, sectionOrderBy:
							tr.emit(rd.displayCol())
						default:
							tr.emit(rd.idCol)
						}
					}
				} else if col, ok := tr.colMap[lower]; ok && !prevDot {
					tr.emit(col)
				} else if prevDot {
					if rd := tr.findRefDim(lower); rd != nil {
						tr.emit(rd.idCol)
					} else if c, ok2 := tr.colMap[lower]; ok2 {
						tr.emit(c)
					} else {
						tr.emit(lower)
					}
				} else {
					tr.emit(lower)
				}
			}
			continue
		}

		tr.advance()
	}
	return Result{SQL: tr.build(), Args: tr.args}, nil
}

// dialectName возвращает строковое имя диалекта SQL для opts.Dialect; nil → "pg"
// (значение по умолчанию из dialectOrDefault). Используется в выборе шаблонов
// функций даты — strftime в SQLite, EXTRACT в PostgreSQL.
func dialectName(d storage.Dialect) string {
	if d == nil {
		return "pg"
	}
	return d.Name()
}

// dateFuncRewrite — пара prefix/suffix токенов, которые оборачивают исходный
// аргумент функции даты. Например, Год(x) в SQLite → CAST(strftime('%Y', x) AS INTEGER).
type dateFuncRewrite struct {
	prefix []tok
	suffix []tok
}

// tokenizeFragment токенизирует SQL-фрагмент и убирает финальный EOF.
func tokenizeFragment(s string) []tok {
	t := tokenize(s)
	if len(t) > 0 && t[len(t)-1].kind == tEOF {
		t = t[:len(t)-1]
	}
	return t
}

// dateFuncRewrites возвращает таблицу замен для функций даты под нужный диалект.
// Ключи в нижнем регистре, сопоставление case-insensitive.
func dateFuncRewrites(dialect string) map[string]dateFuncRewrite {
	rw := func(prefix, suffix string) dateFuncRewrite {
		return dateFuncRewrite{prefix: tokenizeFragment(prefix), suffix: tokenizeFragment(suffix)}
	}
	switch dialect {
	case "sqlite":
		return map[string]dateFuncRewrite{
			"началодня":   rw("date(", ")"),
			"startofday":  rw("date(", ")"),
			"конецдня":    rw("datetime(date(", "), '+1 day', '-1 second')"),
			"endofday":    rw("datetime(date(", "), '+1 day', '-1 second')"),
			"началомесяца": rw("date(", ", 'start of month')"),
			"startofmonth": rw("date(", ", 'start of month')"),
			"началогода":   rw("date(", ", 'start of year')"),
			"startofyear":  rw("date(", ", 'start of year')"),
			"год":          rw("CAST(strftime('%Y',", ") AS INTEGER)"),
			"year":         rw("CAST(strftime('%Y',", ") AS INTEGER)"),
			"месяц":        rw("CAST(strftime('%m',", ") AS INTEGER)"),
			"month":        rw("CAST(strftime('%m',", ") AS INTEGER)"),
			"день":         rw("CAST(strftime('%d',", ") AS INTEGER)"),
			"day":          rw("CAST(strftime('%d',", ") AS INTEGER)"),
		}
	default: // pg
		return map[string]dateFuncRewrite{
			"началодня":    rw("date_trunc('day',", ")"),
			"startofday":   rw("date_trunc('day',", ")"),
			"конецдня":     rw("(date_trunc('day',", ") + INTERVAL '1 day' - INTERVAL '1 microsecond')"),
			"endofday":     rw("(date_trunc('day',", ") + INTERVAL '1 day' - INTERVAL '1 microsecond')"),
			"началомесяца": rw("date_trunc('month',", ")"),
			"startofmonth": rw("date_trunc('month',", ")"),
			"началогода":   rw("date_trunc('year',", ")"),
			"startofyear":  rw("date_trunc('year',", ")"),
			// CAST(... AS INTEGER) — портативно (PG поддерживает обе формы,
			// :: не транслируется нашим токенизатором).
			"год":   rw("CAST(EXTRACT(YEAR FROM", ") AS INTEGER)"),
			"year":  rw("CAST(EXTRACT(YEAR FROM", ") AS INTEGER)"),
			"месяц": rw("CAST(EXTRACT(MONTH FROM", ") AS INTEGER)"),
			"month": rw("CAST(EXTRACT(MONTH FROM", ") AS INTEGER)"),
			"день":  rw("CAST(EXTRACT(DAY FROM", ") AS INTEGER)"),
			"day":   rw("CAST(EXTRACT(DAY FROM", ") AS INTEGER)"),
		}
	}
}

// rewriteDateFuncs обходит токены и разворачивает Год(x), НачалоДня(x), … в
// соответствующий SQL-шаблон диалекта. Раскрытие — на уровне токенов:
// сохраняются tIdent/tStr/tLParen и т.п., чтобы основной транслятор обработал
// внутренний аргумент через обычные правила (resolve ref dims, параметры и т.п.).
// Рекурсивно для вложенных вызовов: Месяц(НачалоМесяца(x)) тоже разворачивается.
func rewriteDateFuncs(tokens []tok, dialect string) []tok {
	rewrites := dateFuncRewrites(dialect)
	var out []tok
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.kind == tIdent && i+1 < len(tokens) && tokens[i+1].kind == tLParen {
			key := strings.ToLower(t.val)
			if rw, ok := rewrites[key]; ok {
				// поиск парной закрывающей )
				depth := 0
				end := -1
				for j := i + 1; j < len(tokens); j++ {
					switch tokens[j].kind {
					case tLParen:
						depth++
					case tRParen:
						depth--
						if depth == 0 {
							end = j
						}
					}
					if end >= 0 {
						break
					}
				}
				if end < 0 {
					// нет пары — оставляем как есть, пусть SQL потом упадёт явно
					out = append(out, t)
					continue
				}
				inner := tokens[i+2 : end]
				inner = rewriteDateFuncs(inner, dialect) // рекурсия
				out = append(out, rw.prefix...)
				out = append(out, inner...)
				out = append(out, rw.suffix...)
				i = end // пропускаем закрывающую )
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// momentTimeValue — контракт для DSL-значения «момент времени».
// Реализуется *runtime.MomentTime; интерфейс объявлен здесь чтобы не
// тянуть импорт runtime в query.
type momentTimeValue interface {
	PointInTime() (period time.Time, docID string)
}

// momentTimeCondition строит SQL-условие «строго до момента»:
//
//	(period < $1 OR (period = $1 AND recorder != $2))
//
// Логика «recorder != docID» гарантирует, что при перепроведении сам
// документ исключается из собственной сводки.
func (tr *translator) momentTimeCondition(mt momentTimeValue) string {
	d := dialectOrDefault(tr.opts.Dialect)
	period, docID := mt.PointInTime()
	tr.args = append(tr.args, period)
	periodPH := d.Placeholder(len(tr.args))
	if docID == "" {
		// document-less moment — простое сравнение
		return "period <= " + periodPH
	}
	id, err := uuid.Parse(docID)
	if err != nil {
		// docID — невалидный UUID, безопасный fallback: только по периоду.
		return "period <= " + periodPH
	}
	// period используется в SQL дважды (period < ... OR period = ...).
	// SQLite-плейсхолдеры анонимные ('?') — каждый '?' это отдельный
	// позиционный аргумент, поэтому period нужно добавить ВТОРЫМ аргументом
	// под второй плейсхолдер, иначе аргументы съезжают («missing argument»).
	// Для нумерованных диалектов ($n) лишний дубль period безвреден.
	tr.args = append(tr.args, period)
	periodPH2 := d.Placeholder(len(tr.args))
	if d.Name() == "sqlite" {
		tr.args = append(tr.args, id.String())
	} else {
		tr.args = append(tr.args, id)
	}
	docPH := d.Placeholder(len(tr.args))
	return fmt.Sprintf("(period < %s OR (period = %s AND recorder != %s))",
		periodPH, periodPH2, docPH)
}

// firstArgMoment возвращает *MomentTime если первый аргумент VT —
// это &Param и paramValues[name] = *MomentTime. Иначе nil — fallback на
// обычное period <= ...
func (tr *translator) firstArgMoment(args []tok) momentTimeValue {
	if len(args) != 1 || args[0].kind != tParam {
		return nil
	}
	v := tr.paramValues[args[0].val]
	if mt, ok := v.(momentTimeValue); ok {
		return mt
	}
	return nil
}

// systemColAlias maps the PascalCase русский alias for register system columns
// (period / вид_движения / recorder / line_number) to the actual DB column name.
// Используется и в SELECT/WHERE верхнего уровня, и после точки (alias.Период).
func systemColAlias(name string) (string, bool) {
	switch strings.ToLower(name) {
	case "период":
		return "period", true
	case "виддвижения":
		return "вид_движения", true
	case "регистратор":
		return "recorder", true
	case "номерстроки":
		return "line_number", true
	}
	return "", false
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// pgCast returns a PostgreSQL explicit cast suffix for v.
func pgCast(v any) string {
	switch v := v.(type) {
	case time.Time:
		return "::timestamptz"
	case string:
		if isUUID(v) {
			return "::uuid"
		}
		return "::text"
	case float64, float32:
		return "::numeric"
	case int, int32, int64, uint, uint32, uint64:
		return "::bigint"
	case bool:
		return "::boolean"
	}
	return ""
}
