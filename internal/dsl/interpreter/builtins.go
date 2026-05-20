package interpreter

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// BuiltinFunc is a callable value that can be injected via extraVars (e.g. Сообщить).
type BuiltinFunc func(args []any, file string, line int) (any, error)

// DSLError is returned by Error() built-in; stops execution and cancels Save.
type DSLError struct {
	File string
	Line int
	Msg  string
}

func (e *DSLError) Error() string {
	if e.File != "" && e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}
	return e.Msg
}

var builtins = map[string]func(args []any, file string, line int) (any, error){

	// ─── Сообщения ────────────────────────────────────────────────────────
	"сообщить": func(args []any, file string, line int) (any, error) { return nil, nil },
	"message":  func(args []any, file string, line int) (any, error) { return nil, nil },

	// ─── Ошибки ───────────────────────────────────────────────────────────
	"error": func(args []any, file string, line int) (any, error) {
		msg := ""
		if len(args) > 0 {
			msg = fmt.Sprintf("%v", args[0])
		}
		panic(userError{Msg: msg})
	},
	"вызватьисключение": func(args []any, file string, line int) (any, error) {
		msg := ""
		if len(args) > 0 {
			msg = fmt.Sprintf("%v", args[0])
		}
		panic(userError{Msg: msg})
	},

	// ─── Даты ─────────────────────────────────────────────────────────────
	"today": func(args []any, file string, line int) (any, error) {
		t := time.Now()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local), nil
	},
	"текущаядата": func(args []any, file string, line int) (any, error) {
		t := time.Now()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local), nil
	},
	"now": func(args []any, file string, line int) (any, error) {
		return time.Now(), nil
	},
	"текущаядатавремя": func(args []any, file string, line int) (any, error) {
		return time.Now(), nil
	},
	"year": func(args []any, file string, line int) (any, error) {
		if t, ok := toTime(args, 0); ok {
			return float64(t.Year()), nil
		}
		return nil, nil
	},
	"год": func(args []any, file string, line int) (any, error) {
		if t, ok := toTime(args, 0); ok {
			return float64(t.Year()), nil
		}
		return nil, nil
	},
	"month": func(args []any, file string, line int) (any, error) {
		if t, ok := toTime(args, 0); ok {
			return float64(t.Month()), nil
		}
		return nil, nil
	},
	"месяц": func(args []any, file string, line int) (any, error) {
		if t, ok := toTime(args, 0); ok {
			return float64(t.Month()), nil
		}
		return nil, nil
	},
	"day": func(args []any, file string, line int) (any, error) {
		if t, ok := toTime(args, 0); ok {
			return float64(t.Day()), nil
		}
		return nil, nil
	},
	"день": func(args []any, file string, line int) (any, error) {
		if t, ok := toTime(args, 0); ok {
			return float64(t.Day()), nil
		}
		return nil, nil
	},

	// ─── Строки ───────────────────────────────────────────────────────────
	"str": func(args []any, file string, line int) (any, error) {
		if len(args) == 0 {
			return "", nil
		}
		if f, ok := toFloat(args[0]); ok {
			if f == math.Trunc(f) {
				return strconv.FormatInt(int64(f), 10), nil
			}
			return strconv.FormatFloat(f, 'f', -1, 64), nil
		}
		if t, ok := args[0].(time.Time); ok {
			return t.Format("02.01.2006 15:04:05"), nil
		}
		return fmt.Sprintf("%v", args[0]), nil
	},
	"строка": func(args []any, file string, line int) (any, error) {
		if len(args) == 0 {
			return "", nil
		}
		if f, ok := toFloat(args[0]); ok {
			if f == math.Trunc(f) {
				return strconv.FormatInt(int64(f), 10), nil
			}
			return strconv.FormatFloat(f, 'f', -1, 64), nil
		}
		if t, ok := args[0].(time.Time); ok {
			return t.Format("02.01.2006 15:04:05"), nil
		}
		return fmt.Sprintf("%v", args[0]), nil
	},
	"number": func(args []any, file string, line int) (any, error) {
		if len(args) == 0 {
			return float64(0), nil
		}
		s := fmt.Sprintf("%v", args[0])
		f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil {
			return float64(0), nil
		}
		return f, nil
	},
	"число": func(args []any, file string, line int) (any, error) {
		if len(args) == 0 {
			return float64(0), nil
		}
		s := fmt.Sprintf("%v", args[0])
		f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil {
			return float64(0), nil
		}
		return f, nil
	},
	"upper": func(args []any, file string, line int) (any, error) {
		return strings.ToUpper(strArg(args, 0)), nil
	},
	"врег": func(args []any, file string, line int) (any, error) {
		return strings.ToUpper(strArg(args, 0)), nil
	},
	"lower": func(args []any, file string, line int) (any, error) {
		return strings.ToLower(strArg(args, 0)), nil
	},
	"нрег": func(args []any, file string, line int) (any, error) {
		return strings.ToLower(strArg(args, 0)), nil
	},
	"trimall": func(args []any, file string, line int) (any, error) {
		return strings.TrimSpace(strArg(args, 0)), nil
	},
	"сокрлп": func(args []any, file string, line int) (any, error) {
		return strings.TrimSpace(strArg(args, 0)), nil
	},
	"left": func(args []any, file string, line int) (any, error) {
		s := []rune(strArg(args, 0))
		n := int(floatArg(args, 1))
		if n > len(s) {
			n = len(s)
		}
		if n < 0 {
			n = 0
		}
		return string(s[:n]), nil
	},
	"лев": func(args []any, file string, line int) (any, error) {
		s := []rune(strArg(args, 0))
		n := int(floatArg(args, 1))
		if n > len(s) {
			n = len(s)
		}
		if n < 0 {
			n = 0
		}
		return string(s[:n]), nil
	},
	"right": func(args []any, file string, line int) (any, error) {
		s := []rune(strArg(args, 0))
		n := int(floatArg(args, 1))
		if n > len(s) {
			n = len(s)
		}
		if n < 0 {
			n = 0
		}
		return string(s[len(s)-n:]), nil
	},
	"прав": func(args []any, file string, line int) (any, error) {
		s := []rune(strArg(args, 0))
		n := int(floatArg(args, 1))
		if n > len(s) {
			n = len(s)
		}
		if n < 0 {
			n = 0
		}
		return string(s[len(s)-n:]), nil
	},
	"mid": func(args []any, file string, line int) (any, error) {
		return midStr(args), nil
	},
	"сред": func(args []any, file string, line int) (any, error) {
		return midStr(args), nil
	},
	"strlen": func(args []any, file string, line int) (any, error) {
		return float64(len([]rune(strArg(args, 0)))), nil
	},
	"стрдлина": func(args []any, file string, line int) (any, error) {
		return float64(len([]rune(strArg(args, 0)))), nil
	},
	"strfind": func(args []any, file string, line int) (any, error) {
		s := strArg(args, 0)
		sub := strArg(args, 1)
		idx := strings.Index(s, sub)
		if idx < 0 {
			return float64(0), nil
		}
		return float64(len([]rune(s[:idx])) + 1), nil
	},
	"стрнайти": func(args []any, file string, line int) (any, error) {
		s := strArg(args, 0)
		sub := strArg(args, 1)
		idx := strings.Index(s, sub)
		if idx < 0 {
			return float64(0), nil
		}
		return float64(len([]rune(s[:idx])) + 1), nil
	},

	// ─── Математика ───────────────────────────────────────────────────────
	"round": func(args []any, file string, line int) (any, error) {
		n := floatArg(args, 0)
		d := int(floatArg(args, 1))
		p := math.Pow(10, float64(d))
		return math.Round(n*p) / p, nil
	},
	"окр": func(args []any, file string, line int) (any, error) {
		n := floatArg(args, 0)
		d := int(floatArg(args, 1))
		p := math.Pow(10, float64(d))
		return math.Round(n*p) / p, nil
	},
	"abs": func(args []any, file string, line int) (any, error) {
		return math.Abs(floatArg(args, 0)), nil
	},
	"абс": func(args []any, file string, line int) (any, error) {
		return math.Abs(floatArg(args, 0)), nil
	},
	"int": func(args []any, file string, line int) (any, error) {
		return math.Trunc(floatArg(args, 0)), nil
	},
	"цел": func(args []any, file string, line int) (any, error) {
		return math.Trunc(floatArg(args, 0)), nil
	},
	"max": func(args []any, file string, line int) (any, error) {
		return math.Max(floatArg(args, 0), floatArg(args, 1)), nil
	},
	"макс": func(args []any, file string, line int) (any, error) {
		return math.Max(floatArg(args, 0), floatArg(args, 1)), nil
	},
	"min": func(args []any, file string, line int) (any, error) {
		return math.Min(floatArg(args, 0), floatArg(args, 1)), nil
	},
	"мин": func(args []any, file string, line int) (any, error) {
		return math.Min(floatArg(args, 0), floatArg(args, 1)), nil
	},

	// ─── JSON ─────────────────────────────────────────────────────────────
	"прочитатьjson": builtinReadJSON,
	"readjson":      builtinReadJSON,
	"записатьjson":  builtinWriteJSON,
	"writejson":     builtinWriteJSON,
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func strArg(args []any, i int) string {
	if i < len(args) && args[i] != nil {
		return fmt.Sprintf("%v", args[i])
	}
	return ""
}

func floatArg(args []any, i int) float64 {
	if i < len(args) {
		if f, ok := toFloat(args[i]); ok {
			return f
		}
	}
	return 0
}

func toTime(args []any, i int) (time.Time, bool) {
	if i < len(args) {
		if t, ok := args[i].(time.Time); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func midStr(args []any) string {
	s := []rune(strArg(args, 0))
	start := int(floatArg(args, 1)) - 1 // 1-based → 0-based
	length := int(floatArg(args, 2))
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return ""
	}
	end := start + length
	if end > len(s) {
		end = len(s)
	}
	return string(s[start:end])
}

// KnownBuiltinNames returns a set of all known callable names (lowercase):
// platform builtins + runtime-injected functions (HTTP, Email, etc.).
// Used by the syntax checker to validate function calls in modules.
func KnownBuiltinNames() map[string]struct{} {
	names := make(map[string]struct{}, len(builtins)+20)
	for k := range builtins {
		names[k] = struct{}{}
	}
	// special context variables
	names["this"] = struct{}{}
	names["этотобъект"] = struct{}{}
	// runtime-injected via buildDSLVars / buildDSLVarsWithMessages
	for _, k := range []string{
		"сообщить", "message",
		"httpполучить", "httpget", "httpотправить", "httppost",
		"отправитьписьмо", "sendemail",
		// transactions (injected via TxState.Builtins)
		"начатьтранзакцию", "begintransaction",
		"зафиксироватьтранзакцию", "committransaction",
		"отменитьтранзакцию", "rollbacktransaction",
		// injected in except-block context
		"описаниеошибки", "errordescription",
		// register movement / lock / current-user globals (buildDSLVars)
		"блокировкаданных", "datalock",
		"текущийпользователь", "currentuser",
		"имяпользователя", "username",
		"справочники", "catalogs",
		"предопределённыезначения", "predefinedvalues",
	} {
		names[k] = struct{}{}
	}
	return names
}
