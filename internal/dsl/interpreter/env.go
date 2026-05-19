package interpreter

import "strings"

// This is implemented by runtime.Object; defined here to avoid import cycles.
type This interface {
	Get(name string) any
	Set(name string, v any)
}

// MethodCallable is implemented by objects that support obj.Method(args) calls.
type MethodCallable interface {
	CallMethod(method string, args []any) any
}

// MapThis wraps map[string]any as a This (used for tablepart rows and register movement records).
type MapThis struct{ M map[string]any }

func (m *MapThis) Get(name string) any {
	low := strings.ToLower(name)
	for k, v := range m.M {
		if strings.ToLower(k) == low {
			return v
		}
	}
	return nil
}

func (m *MapThis) Set(name string, v any) {
	low := strings.ToLower(name)
	for k := range m.M {
		if strings.ToLower(k) == low {
			m.M[k] = v
			return
		}
	}
	m.M[low] = v
}

type env struct {
	vars   map[string]any
	parent *env
	this   This
}

func newEnv(this This) *env {
	return &env{vars: make(map[string]any), this: this}
}

func (e *env) child() *env {
	return &env{vars: make(map[string]any), parent: e, this: e.this}
}

func (e *env) get(name string) (any, bool) {
	low := strings.ToLower(name)
	if low == "this" || low == "этотобъект" {
		return e.this, true
	}
	name = low
	if v, ok := e.vars[name]; ok {
		return v, true
	}
	if e.parent != nil {
		return e.parent.get(name)
	}
	return nil, false
}

func (e *env) set(name string, v any) {
	name = strings.ToLower(name)
	// Если переменная уже объявлена в родительском scope — обновляем там.
	if _, ok := e.vars[name]; !ok && e.parent != nil {
		if e.parent.has(name) {
			e.parent.set(name, v)
			return
		}
	}
	e.vars[name] = v
}

func (e *env) has(name string) bool {
	name = strings.ToLower(name)
	if _, ok := e.vars[name]; ok {
		return true
	}
	if e.parent != nil {
		return e.parent.has(name)
	}
	return false
}
