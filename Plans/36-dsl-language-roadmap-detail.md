# План 36 (детальный): DSL Language Extensions — Priority 1

**Статус:** ✅ Реализовано (сверка 2026-06-25). Все пункты priority-1 из этого
детального плана закрыты в `internal/dsl`: S1 `ИначеЕсли`, S2 тернарный
`?(...)`, S3 compound assign, B1/B2 даты и время, B3 строки, B4
`Пустая`/`ЗначениеЗаполнено`, B5 `Формат`, B6 `ТипЗнч`/`Тип`. Детализация ниже
сохранена как исторический чек-лист реализации.

> Детальный план реализации по [[36-dsl-language-roadmap]]. Содержит конкретные файлы, строки и изменения для каждой фичи.

## Context

OneBase DSL не хватает базовых конструкций для комфортной разработки pet-конфигураций: нет `ИначеЕсли`, нет тернарного оператора, нет `+=`, недостаточно встроенных функций для строк/дат/форматирования. План 36 определяет 9 фич приоритета 1 (S1-S3, B1-B6). Runtime-фичи (R1-R3) откладываем — план рекомендует «сначала топ-3, потом runtime».

## Implementation Order

```
S1 (ElseIf) → S3 (+=) → S2 (ternary) → B1 (dates) → B2 (time) → B3 (strings) → B4 (empty) → B5 (format) → B6 (typeof)
```

Каждая фича — независимый набор изменений в 4 слоях: token → lexer → AST → parser → interpreter.

---

## S1: ИначеЕсли / ElseIf

**token.go**: добавить `ELSEIF` константу после `ELSE` (line 22). Keywords: `"иначеесли": ELSEIF`, `"elseif": ELSEIF`. String case.

**ast.go**: добавить `ElseIfBranch struct { Cond Expr; Body []Stmt }`. Добавить поле `ElseIfs []ElseIfBranch` в `IfStmt` (line 25-29). Добавить `nodeType()`.

**parser.go**:
- `isBlockEnd` (line 102): добавить `token.ELSEIF` — критично, иначе Then-блок поглотит ElseIf.
- `parseIf` (line 155-181): после `parseBlock(ENDIF)` добавить loop:
  ```
  for p.cur.Type == token.ELSEIF {
      advance, parseExpr, expect(THEN), parseBlock
      append to elseIfs
  }
  ```
- Передать `ElseIfs` в конструктор `IfStmt`.

**interpreter.go** `execStmt` (line 182-188): после проверки `truthy(cond)`, если false — iterate `v.ElseIfs`, при совпадении — `execBlock`, иначе fall through to `v.Else`.

---

## S2: Тернарный `?(cond, true, false)`

**token.go**: добавить `QUESTION` константу. String case `"?"`.

**lexer.go** (line 67-114): добавить `case '?':` → `token.QUESTION`.

**ast.go**: добавить `TernaryExpr struct { Tok, Cond, True, False Expr }`. Добавить `nodeType()`, `exprNode()`.

**parser.go** `parsePrimary` (line 431-481): добавить `case token.QUESTION:` — advance, expect LPAREN, parseExpr, expect COMMA, parseExpr, expect COMMA, parseExpr, expect RPAREN.

**interpreter.go** `evalExpr` (line 282-326): добавить `case *ast.TernaryExpr:` — eval cond, вернуть true или false ветку.

**debug_hooks.go**: добавить case в `getExprLocation`.

---

## S3: Операторы += -= *= /=

**token.go**: добавить `PLUS_ASSIGN, MINUS_ASSIGN, STAR_ASSIGN, SLASH_ASSIGN`. String cases.

**lexer.go**:
- `+` (line 82): peek `=` → `PLUS_ASSIGN`, иначе `PLUS`.
- `-` (line 84): peek `=` → `MINUS_ASSIGN`, иначе `MINUS`.
- `*` (line 86): peek `=` → `STAR_ASSIGN`, иначе `STAR`.
- `/` (line 88): после проверки `//` комментария, добавить peek `=` → `SLASH_ASSIGN`.

**ast.go**: добавить поле `Op token.Type` в `AssignStmt` (default `ASSIGN`).

**parser.go** `parseExprOrAssign` (line 277-310):
- После проверки `p.cur.Type == token.ASSIGN` добавить блок для compound operators.
- Helper `isCompoundAssign(t)`.
- Обновить существующий конструктор `AssignStmt` (line 294) — добавить `Op: token.ASSIGN`.

**interpreter.go** `execStmt` (line 225-227): если `v.Op != ASSIGN` — прочитать старое значение через `evalExpr(v.Target, e)`, вычислить новое через `applyCompoundOp`, записать результат.

---

## B1: Дата-арифметика (10 функций, 20 записей)

**builtins.go** — добавить в map:
- `begmonth`/`началомесяца`, `endmonth`/`конецмесяца`
- `begyear`/`началогода`, `endyear`/`конецгода`
- `begweek`/`началонедели`, `endweek`/`конецнедели`
- `begday`/`началодня`, `endday`/`конецдня`
- `addmonth`/`добавитьмесяц` — `t.AddDate(0, N, 0)`
- `datediff`/`разностьдат` — разность в заданных единицах (секунда/минута/час/день/месяц/год)

Все используют `toTime(args, 0)` для первого аргумента.

---

## B2: Час / Минута / Секунда / ДеньНедели (4 функции, 8 записей)

**builtins.go** — добавить:
- `hour`/`час` → `t.Hour()`
- `minute`/`минута` → `t.Minute()`
- `second`/`секунда` → `t.Second()`
- `dayofweek`/`деньнедели` → `(t.Weekday()+6)%7 + 1` (1С convention: Пн=1, Вс=7)

---

## B3: Расширенные строковые операции (7 функций, 14 записей)

**builtins.go** — добавить:
- `strreplace`/`стрзаменить` → `strings.ReplaceAll`
- `strsplit`/`стрразделить` → `strings.Split` → конвертировать в `*Array`
- `strjoin`/`стрсоединить` → принять `*Array` или `[]any`, `strings.Join`
- `strstartswith`/`стрначинаетсяс` → `strings.HasPrefix`
- `strendswith`/`стрзаканчиваетсяна` → `strings.HasSuffix`
- `strcontains`/`стрсодержит` → `strings.Contains`
- `strtemplate`/`стршаблон` → заменить `%1`, `%2`, ... на аргументы

---

## B4: Пустая / ЗначениеЗаполнено (2 функции, 4 записи)

**builtins.go** — добавить:
- `isblank`/`пустая` — true для nil, `""`, `0`, пустых коллекций
- `isfilled`/`значениезаполнено` — `!isblank`

---

## B5: Формат (1 функция, 2 записи)

**builtins.go** — `format`/`формат`:
- Парсинг формат-строки: `ЧДЦ=N` (дробные), `ЧРГ=' '` (разделитель тысяч), `ДФ='dd.MM.yyyy'` (дата)
- Для чисел: `strconv.FormatFloat` с precision + thousands separator
- Для дат: конвертация 1С-паттерна в Go `time.Format`

---

## B6: ТипЗнч / Тип (2 функции, 4 записи)

**builtins.go**:
- `typeof`/`типзнч` — использует существующий `getTypeName()` из `debug_hooks.go`
- `type`/`тип` — возвращает строку имени типа (для сравнения `ТипЗнч(x) = Тип("Число")`)
- Добавить `case time.Time: return "Дата"` в `getTypeName`

---

## Critical Files

| File | Changes |
|------|---------|
| `internal/dsl/token/token.go` | ELSEIF, QUESTION, PLUS_ASSIGN etc. + keywords + String() |
| `internal/dsl/lexer/lexer.go` | `?`, `+=`, `-=`, `*=`, `/=` tokenization |
| `internal/dsl/ast/ast.go` | ElseIfBranch, TernaryExpr, AssignStmt.Op |
| `internal/dsl/parser/parser.go` | parseIf ElseIf loop, parsePrimary ?, compound assign |
| `internal/dsl/interpreter/interpreter.go` | execStmt ElseIf/Assign, evalExpr Ternary |
| `internal/dsl/interpreter/builtins.go` | ~52 new map entries (B1-B6) |
| `internal/dsl/interpreter/debug_hooks.go` | TernaryExpr location, getTypeName date case |

## Verification

1. `go build ./...` — компиляция без ошибок
2. `go test ./internal/dsl/...` — существующие тесты проходят
3. Ручной smoke-test через консоль кода конфигуратора:
   - `Если 1 = 2 Тогда Сообщить("no"); ИначеЕсли 1 = 1 Тогда Сообщить("yes"); КонецЕсли`
   - `Сообщить(?(Истина, "yes", "no"))`
   - `x = 5; x += 3; Сообщить(x)`
   - `Сообщить(НачалоМесяца(ТекущаяДата()))`
   - `Сообщить(СтрЗаменить("hello world", "world", "onebase"))`
   - `Сообщить(Формат(1234.5, "ЧДЦ=2"))`
   - `Сообщить(ТипЗнч(42))`
