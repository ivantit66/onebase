package interpreter

import (
	"fmt"
	"strings"
	"time"
)

// dateBuiltin wraps a function that transforms a time.Time value.
func dateBuiltin(fn func(time.Time) any) func([]any, string, int) (any, error) {
	return func(args []any, _ string, _ int) (any, error) {
		if t, ok := toTime(args, 0); ok {
			return fn(t), nil
		}
		return nil, nil
	}
}

func dateEndMonth(args []any, _ string, _ int) (any, error) {
	t, ok := toTime(args, 0)
	if !ok {
		return nil, nil
	}
	nm := t.Month() + 1
	ny := t.Year()
	if nm > 12 {
		nm = 1
		ny++
	}
	return time.Date(ny, nm, 1, 0, 0, 0, 0, time.Local).Add(-time.Second), nil
}

func dateBegWeek(args []any, _ string, _ int) (any, error) {
	t, ok := toTime(args, 0)
	if !ok {
		return nil, nil
	}
	o := (int(t.Weekday()) - int(time.Monday) + 7) % 7
	return time.Date(t.Year(), t.Month(), t.Day()-o, 0, 0, 0, 0, time.Local), nil
}

func dateEndWeek(args []any, _ string, _ int) (any, error) {
	t, ok := toTime(args, 0)
	if !ok {
		return nil, nil
	}
	o := (int(time.Sunday) - int(t.Weekday()) + 7) % 7
	return time.Date(t.Year(), t.Month(), t.Day()+o, 23, 59, 59, 0, time.Local), nil
}

func addMonthBuiltin(args []any, _ string, _ int) (any, error) {
	t, ok := toTime(args, 0)
	if !ok {
		return nil, nil
	}
	return t.AddDate(0, int(floatArg(args, 1)), 0), nil
}

func dateDiffBuiltin(args []any, _ string, _ int) (any, error) {
	t1, ok1 := toTime(args, 0)
	t2, ok2 := toTime(args, 1)
	if !ok1 || !ok2 {
		return float64(0), nil
	}
	unit := strings.ToLower(strArg(args, 2))
	d := t2.Sub(t1)
	switch unit {
	case "секунда", "second":
		return float64(int(d.Seconds())), nil
	case "минута", "minute":
		return float64(int(d.Minutes())), nil
	case "час", "hour":
		return float64(int(d.Hours())), nil
	case "месяц", "month":
		m := (t2.Year()-t1.Year())*12 + int(t2.Month()) - int(t1.Month())
		return float64(m), nil
	case "год", "year":
		return float64(t2.Year() - t1.Year()), nil
	default:
		return float64(int(d.Hours()) / 24), nil
	}
}

func splitBuiltin(args []any, _ string, _ int) (any, error) {
	parts := strings.Split(strArg(args, 0), strArg(args, 1))
	result := make([]any, len(parts))
	for i, p := range parts {
		result[i] = p
	}
	return result, nil
}

func joinBuiltin(args []any, _ string, _ int) (any, error) {
	sep := strArg(args, 1)
	var parts []string
	if arr, ok := args[0].(*Array); ok {
		for _, v := range arr.Iterate() {
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	} else if arr, ok := args[0].([]any); ok {
		for _, v := range arr {
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	return strings.Join(parts, sep), nil
}

func templateBuiltin(args []any, _ string, _ int) (any, error) {
	s := strArg(args, 0)
	for i := 1; i < len(args); i++ {
		s = strings.ReplaceAll(s, "%"+intToStr(i), fmt.Sprintf("%v", args[i]))
	}
	return s, nil
}

func intToStr(n int) string {
	return fmt.Sprintf("%d", n)
}
