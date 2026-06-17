package interpreter

import (
	"encoding/base64"
	"math"
	"strings"
	"time"
	"unicode"
)

// init регистрирует функции «Этапа A» дорожной карты DSL: недостающие
// строковые/датовые/математические обёртки и Base64. Каждая — тонкая
// обёртка над stdlib; имена попадают в KnownBuiltinNames автоматически,
// т.к. это ключи карты builtins. У всех — рус. и англ. ключи (DSL
// регистронезависим).
func init() {
	// ─── Строки ───────────────────────────────────────────────────────────
	builtins["сокрл"] = trimLeftFn
	builtins["trimleft"] = trimLeftFn
	builtins["сокрп"] = trimRightFn
	builtins["trimright"] = trimRightFn
	builtins["стрчисловхождений"] = strOccurrencesFn
	builtins["stroccurrencecount"] = strOccurrencesFn
	builtins["стрчислострок"] = strLineCountFn
	builtins["strlinecount"] = strLineCountFn
	builtins["стрполучитьстроку"] = strGetLineFn
	builtins["strgetline"] = strGetLineFn
	builtins["кодсимвола"] = charCodeFn
	builtins["charcode"] = charCodeFn
	builtins["стрсравнить"] = strCompareFn
	builtins["strcompare"] = strCompareFn
	builtins["пустаястрока"] = isBlankStringFn
	builtins["isblankstring"] = isBlankStringFn
	builtins["трег"] = titleCaseFn
	builtins["titlecase"] = titleCaseFn
	builtins["нстр"] = nstrFn
	builtins["nstr"] = nstrFn

	// ─── Даты: квартал / порядковые / границы часа и минуты ────────────────
	builtins["началоквартала"] = dateBuiltin(begQuarter)
	builtins["begquarter"] = dateBuiltin(begQuarter)
	builtins["конецквартала"] = dateBuiltin(endQuarter)
	builtins["endquarter"] = dateBuiltin(endQuarter)
	builtins["деньгода"] = dateBuiltin(func(t time.Time) any { return float64(t.YearDay()) })
	builtins["dayofyear"] = dateBuiltin(func(t time.Time) any { return float64(t.YearDay()) })
	builtins["неделягода"] = dateBuiltin(func(t time.Time) any { return float64(weekOfYear(t)) })
	builtins["weekofyear"] = dateBuiltin(func(t time.Time) any { return float64(weekOfYear(t)) })
	builtins["началочаса"] = dateBuiltin(func(t time.Time) any {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	})
	builtins["beghour"] = builtins["началочаса"]
	builtins["конецчаса"] = dateBuiltin(func(t time.Time) any {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 59, 59, 0, t.Location())
	})
	builtins["endhour"] = builtins["конецчаса"]
	builtins["началоминуты"] = dateBuiltin(func(t time.Time) any {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, t.Location())
	})
	builtins["begminute"] = builtins["началоминуты"]
	builtins["конецминуты"] = dateBuiltin(func(t time.Time) any {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 59, 0, t.Location())
	})
	builtins["endminute"] = builtins["конецминуты"]

	// ─── Математика (имена как в 1С — латиница) ────────────────────────────
	builtins["pow"] = mathBin(math.Pow)
	builtins["sqrt"] = mathUn(math.Sqrt)
	builtins["exp"] = mathUn(math.Exp)
	builtins["log"] = mathUn(math.Log)
	builtins["log10"] = mathUn(math.Log10)
	builtins["sin"] = mathUn(math.Sin)
	builtins["cos"] = mathUn(math.Cos)
	builtins["tan"] = mathUn(math.Tan)
	builtins["asin"] = mathUn(math.Asin)
	builtins["acos"] = mathUn(math.Acos)
	builtins["atan"] = mathUn(math.Atan)

	// ─── Base64 ────────────────────────────────────────────────────────────
	// OneBase не имеет типа ДвоичныеДанные, поэтому работаем со строками:
	// Base64Строка(текст) → base64 от UTF-8 байт; Base64Значение(base64) → текст.
	builtins["base64строка"] = base64EncodeFn
	builtins["base64string"] = base64EncodeFn
	builtins["base64значение"] = base64DecodeFn
	builtins["base64value"] = base64DecodeFn
}

// ─── Строковые реализации ──────────────────────────────────────────────────

func trimLeftFn(args []any, _ string, _ int) (any, error) {
	return strings.TrimLeftFunc(strArg(args, 0), unicode.IsSpace), nil
}

func trimRightFn(args []any, _ string, _ int) (any, error) {
	return strings.TrimRightFunc(strArg(args, 0), unicode.IsSpace), nil
}

// СтрЧислоВхождений(Строка, Подстрока) — число непересекающихся вхождений.
func strOccurrencesFn(args []any, _ string, _ int) (any, error) {
	sub := strArg(args, 1)
	if sub == "" {
		return float64(0), nil
	}
	return float64(strings.Count(strArg(args, 0), sub)), nil
}

// СтрЧислоСтрок(Строка) — число строк (всегда ≥1). \r\n нормализуется.
func strLineCountFn(args []any, _ string, _ int) (any, error) {
	s := strings.ReplaceAll(strArg(args, 0), "\r\n", "\n")
	return float64(strings.Count(s, "\n") + 1), nil
}

// СтрПолучитьСтроку(Строка, НомерСтроки) — n-я строка (1-based) или "".
func strGetLineFn(args []any, _ string, _ int) (any, error) {
	s := strings.ReplaceAll(strArg(args, 0), "\r\n", "\n")
	n := int(floatArg(args, 1))
	lines := strings.Split(s, "\n")
	if n < 1 || n > len(lines) {
		return "", nil
	}
	return lines[n-1], nil
}

// КодСимвола(Строка[, НомерСимвола]) — код символа в позиции (1-based).
// По умолчанию позиция 1; вне диапазона → 0.
func charCodeFn(args []any, _ string, _ int) (any, error) {
	runes := []rune(strArg(args, 0))
	pos := 1
	if len(args) >= 2 {
		pos = int(floatArg(args, 1))
	}
	if pos < 1 || pos > len(runes) {
		return float64(0), nil
	}
	return float64(runes[pos-1]), nil
}

// СтрСравнить(Строка1, Строка2) — без учёта регистра: -1 / 0 / 1.
func strCompareFn(args []any, _ string, _ int) (any, error) {
	a := strings.ToLower(strArg(args, 0))
	b := strings.ToLower(strArg(args, 1))
	return float64(strings.Compare(a, b)), nil
}

// ПустаяСтрока(Строка) — Истина, если после обрезки пробелов строка пуста.
func isBlankStringFn(args []any, _ string, _ int) (any, error) {
	return strings.TrimSpace(strArg(args, 0)) == "", nil
}

// ТРег(Строка) — каждое слово с заглавной, остальные строчные.
func titleCaseFn(args []any, _ string, _ int) (any, error) {
	prevLetter := false
	out := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			if prevLetter {
				r = unicode.ToLower(r)
			} else {
				r = unicode.ToUpper(r)
			}
			prevLetter = true
		} else {
			prevLetter = false
		}
		return r
	}, strArg(args, 0))
	return out, nil
}

// nstrFn — глобальный НСтр с языком по умолчанию "ru" (фоновые/headless-контексты,
// где язык интерфейса неизвестен).
var nstrFn = NewNStrFunc("ru")

// NewNStrFunc возвращает НСтр(ИсходнаяСтрока[, КодЯзыка]) — выбор локализованной
// строки формата "ru = 'Привет'; en = 'Hello'". Если явный КодЯзыка не передан,
// используется defaultLang; если язык не найден среди сегментов — возвращается
// первый сегмент. UI-слой (internal/ui) инжектирует НСтр с языком текущего
// пользователя, чтобы НСтр без кода переводил на язык интерфейса (план 66, п.3);
// глобальная версия остаётся на "ru".
func NewNStrFunc(defaultLang string) BuiltinFunc {
	if defaultLang == "" {
		defaultLang = "ru"
	}
	return func(args []any, _ string, _ int) (any, error) {
		src := strArg(args, 0)
		lang := defaultLang
		if len(args) >= 2 {
			if l := strings.TrimSpace(strArg(args, 1)); l != "" {
				lang = strings.ToLower(l)
			}
		}
		return parseNStr(src, lang), nil
	}
}

func parseNStr(src, lang string) string {
	var first string
	for _, seg := range strings.Split(src, ";") {
		eq := strings.Index(seg, "=")
		if eq < 0 {
			continue
		}
		code := strings.ToLower(strings.TrimSpace(seg[:eq]))
		val := strings.TrimSpace(seg[eq+1:])
		val = strings.Trim(val, "'\"")
		if first == "" {
			first = val
		}
		if code == lang {
			return val
		}
	}
	if first != "" {
		return first
	}
	return strings.TrimSpace(src)
}

// ─── Датовые helpers ───────────────────────────────────────────────────────

func begQuarter(t time.Time) any {
	q := (int(t.Month()) - 1) / 3
	return time.Date(t.Year(), time.Month(q*3+1), 1, 0, 0, 0, 0, t.Location())
}

func endQuarter(t time.Time) any {
	q := (int(t.Month()) - 1) / 3
	startNext := time.Date(t.Year(), time.Month(q*3+4), 1, 0, 0, 0, 0, t.Location())
	return startNext.Add(-time.Second)
}

// weekOfYear — номер недели по 1С: неделя 1 начинается 1 января, границы по
// понедельникам.
func weekOfYear(t time.Time) int {
	jan1 := time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
	offset := (int(jan1.Weekday()) - int(time.Monday) + 7) % 7
	return (t.YearDay()+offset-1)/7 + 1
}

// ─── Математические helpers ────────────────────────────────────────────────

func mathUn(fn func(float64) float64) func([]any, string, int) (any, error) {
	return func(args []any, _ string, _ int) (any, error) {
		return fn(floatArg(args, 0)), nil
	}
}

func mathBin(fn func(a, b float64) float64) func([]any, string, int) (any, error) {
	return func(args []any, _ string, _ int) (any, error) {
		return fn(floatArg(args, 0), floatArg(args, 1)), nil
	}
}

// ─── Base64 ────────────────────────────────────────────────────────────────

func base64EncodeFn(args []any, _ string, _ int) (any, error) {
	return base64.StdEncoding.EncodeToString([]byte(strArg(args, 0))), nil
}

func base64DecodeFn(args []any, _ string, _ int) (any, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strArg(args, 0)))
	if err != nil {
		return "", nil
	}
	return string(b), nil
}
