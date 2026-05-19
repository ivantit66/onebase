package interpreter

import (
	"fmt"
	"strings"
)

// ─── Ref (ссылка на объект метаданных) ───────────────────────────────────────

// Ref represents a DSL reference value: UUID for identity/SQL, Name for display.
// Строка(ref) returns Name; SQL parameter expansion uses UUID.
type Ref struct {
	UUID string
	Name string
}

func (r *Ref) String() string     { return r.Name }
func (r *Ref) GetRefUUID() string { return r.UUID }
func (r *Ref) TypeName() string   { return "Ссылка" }

// refKey extracts the comparison key: UUID for Ref, string representation otherwise.
// Used in Map.findIdx and equal() so *Ref and plain UUID strings match each other.
func refKey(v any) string {
	if ref, ok := v.(*Ref); ok {
		return ref.UUID
	}
	return fmt.Sprintf("%v", v)
}

// ─── Array (Массив) ───────────────────────────────────────────────────────────

type Array struct {
	items []any
}

func (a *Array) CallMethod(name string, args []any) any {
	switch name {
	case "добавить", "add":
		if len(args) > 0 {
			a.items = append(a.items, args[0])
		}
	case "количество", "count":
		return float64(len(a.items))
	case "получить", "get":
		idx := int(floatArg(args, 0))
		if idx >= 0 && idx < len(a.items) {
			return a.items[idx]
		}
	case "удалить", "delete":
		idx := int(floatArg(args, 0))
		if idx >= 0 && idx < len(a.items) {
			a.items = append(a.items[:idx], a.items[idx+1:]...)
		}
	case "очистить", "clear":
		a.items = nil
	case "вставить", "insert":
		if len(args) >= 2 {
			idx := int(floatArg(args, 0))
			val := args[1]
			if idx < 0 {
				idx = 0
			}
			if idx >= len(a.items) {
				a.items = append(a.items, val)
			} else {
				a.items = append(a.items, nil)
				copy(a.items[idx+1:], a.items[idx:])
				a.items[idx] = val
			}
		}
	}
	return nil
}

func (a *Array) Index(i int) any {
	if i >= 0 && i < len(a.items) {
		return a.items[i]
	}
	return nil
}

func (a *Array) SetIndex(i int, val any) {
	if i >= 0 && i < len(a.items) {
		a.items[i] = val
	}
}

func (a *Array) Iterate() []any { return a.items }
func (a *Array) String() string  { return fmt.Sprintf("Массив[%d]", len(a.items)) }
func (a *Array) TypeName() string { return "Массив" }

func (m *Map) Keys() []any            { return m.keys }
func (m *Map) Get(key any) any {
	if idx := m.findIdx(key); idx >= 0 {
		return m.vals[idx]
	}
	return nil
}
func (s *Struct) Fields() []string { return s.keys }

type Struct struct {
	keys []string
	vals map[string]any
}

// NewStructFromMap creates a Struct from a string map.
func NewStructFromMap(m map[string]any) *Struct {
	s := &Struct{vals: make(map[string]any, len(m))}
	for k, v := range m {
		kl := strings.ToLower(k)
		s.keys = append(s.keys, kl)
		s.vals[kl] = v
	}
	return s
}

func newStruct(args []any) *Struct {
	s := &Struct{vals: make(map[string]any)}
	if len(args) == 0 {
		return s
	}
	// args[0] — строка с именами полей через запятую
	fields := splitComma(strArg(args, 0))
	for i, f := range fields {
		f = strings.ToLower(f)
		s.keys = append(s.keys, f)
		if i+1 < len(args) {
			s.vals[f] = args[i+1]
		} else {
			s.vals[f] = nil
		}
	}
	return s
}

func (s *Struct) Get(field string) any    { return s.vals[strings.ToLower(field)] }
func (s *Struct) Set(field string, v any) { s.vals[strings.ToLower(field)] = v }
func (s *Struct) String() string          { return fmt.Sprintf("Структура[%d]", len(s.keys)) }
func (s *Struct) TypeName() string        { return "Структура" }

func (s *Struct) CallMethod(name string, args []any) any {
	switch name {
	case "вставить", "insert":
		if len(args) >= 1 {
			key := strings.ToLower(strArg(args, 0))
			if _, exists := s.vals[key]; !exists {
				s.keys = append(s.keys, key)
			}
			var val any
			if len(args) >= 2 {
				val = args[1]
			}
			s.vals[key] = val
		}
	case "удалить", "delete":
		key := strings.ToLower(strArg(args, 0))
		delete(s.vals, key)
		for i, k := range s.keys {
			if k == key {
				s.keys = append(s.keys[:i], s.keys[i+1:]...)
				break
			}
		}
	case "количество", "count":
		return float64(len(s.keys))
	case "свойство", "property":
		key := strings.ToLower(strArg(args, 0))
		v, ok := s.vals[key]
		if !ok {
			return nil
		}
		return v
	}
	return nil
}

// ─── Map / Соответствие ───────────────────────────────────────────────────────

type Map struct {
	keys []any
	vals []any
}

func (m *Map) findIdx(key any) int {
	ks := refKey(key)
	for i, k := range m.keys {
		if refKey(k) == ks {
			return i
		}
	}
	// Fallback: Ref.Name vs plain string (query auto-resolves references to names)
	if ref, ok := key.(*Ref); ok {
		for i, k := range m.keys {
			if s, ok2 := k.(string); ok2 && s == ref.Name {
				return i
			}
		}
	}
	if s, ok := key.(string); ok {
		for i, k := range m.keys {
			if ref, ok2 := k.(*Ref); ok2 && ref.Name == s {
				return i
			}
		}
	}
	return -1
}

func (m *Map) String() string   { return fmt.Sprintf("Соответствие[%d]", len(m.keys)) }
func (m *Map) TypeName() string { return "Соответствие" }

func (m *Map) CallMethod(name string, args []any) any {
	switch name {
	case "вставить", "insert":
		if len(args) >= 1 {
			key := args[0]
			var val any
			if len(args) >= 2 {
				val = args[1]
			}
			if idx := m.findIdx(key); idx >= 0 {
				m.vals[idx] = val
			} else {
				m.keys = append(m.keys, key)
				m.vals = append(m.vals, val)
			}
		}
	case "получить", "get":
		if len(args) >= 1 {
			if idx := m.findIdx(args[0]); idx >= 0 {
				return m.vals[idx]
			}
		}
	case "удалить", "delete":
		if len(args) >= 1 {
			if idx := m.findIdx(args[0]); idx >= 0 {
				m.keys = append(m.keys[:idx], m.keys[idx+1:]...)
				m.vals = append(m.vals[:idx], m.vals[idx+1:]...)
			}
		}
	case "количество", "count":
		return float64(len(m.keys))
	case "очистить", "clear":
		m.keys = nil
		m.vals = nil
	}
	return nil
}

// ─── KeyValue — элемент итерации по Соответствию ─────────────────────────────

type KeyValue struct {
	Key   any
	Value any
}

func (kv *KeyValue) Get(field string) any {
	switch field {
	case "ключ", "key":
		return kv.Key
	case "значение", "value":
		return kv.Value
	}
	return nil
}

func (kv *KeyValue) Set(field string, val any) {}
func (kv *KeyValue) TypeName() string           { return "КлючИЗначение" }

// ─── helpers ─────────────────────────────────────────────────────────────────

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := trimSpace(s[start:i])
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
