package runtime

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// LockManager — глобальный менеджер блокировок уровня процесса.
//
// Замечание #2: блокировки гранулярные по (регистр, набор измерений).
// Когда DSL зовёт Блок.Заблокировать(), для каждого «элемента» строится
// ключ "регистр|изм1=знач&изм2=знач...", и берётся sync.Mutex по этому
// ключу. Два параллельных проведения с пересекающимся набором (например
// одна и та же Номенклатура+Склад) сериализуются.
//
// Ограничения этой реализации:
//   - Работает только в пределах одного процесса. Для распределённого
//     развёртывания (несколько onebase-инстансов) нужно дополнить
//     SELECT FOR UPDATE или pg_advisory_xact_lock — оставлено на будущее.
//   - Гранулярность ровно та, что задал DSL — если кто-то забыл указать
//     ключ, блокировка не сработает.
//   - Освобождение происходит явно через Разблокировать или автоматически
//     при завершении проведения (defer в handlers.go).
type LockManager struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewLockManager() *LockManager {
	return &LockManager{locks: map[string]*sync.Mutex{}}
}

// Acquire берёт мьютексы по всем ключам в детерминированном порядке
// (отсортированы), чтобы избежать кросс-deadlock'а между двумя
// проведениями, запросившими разный набор в разном порядке.
func (lm *LockManager) Acquire(keys []string) {
	sorted := append([]string{}, keys...)
	sort.Strings(sorted)
	for _, k := range sorted {
		lm.mu.Lock()
		m, ok := lm.locks[k]
		if !ok {
			m = &sync.Mutex{}
			lm.locks[k] = m
		}
		lm.mu.Unlock()
		m.Lock()
	}
}

// Release отпускает мьютексы в обратном порядке.
func (lm *LockManager) Release(keys []string) {
	sorted := append([]string{}, keys...)
	sort.Strings(sorted)
	for i := len(sorted) - 1; i >= 0; i-- {
		lm.mu.Lock()
		m := lm.locks[sorted[i]]
		lm.mu.Unlock()
		if m != nil {
			m.Unlock()
		}
	}
}

// LockObject — DSL-обёртка над LockManager (этап «БлокировкаДанных»).
// Аккумулирует «элементы» (Добавить), для каждого собирает значения
// измерений (УстановитьЗначение), при Заблокировать — берёт мьютексы.
//
// Реализует interpreter.MethodCallable + interpreter.This.
type LockObject struct {
	mgr      *LockManager
	elements []*LockElement
	held     []string // ключи которые удерживаем
}

// NewLockObject — фабрика для DSL builtin БлокировкаДанных().
func NewLockObject(mgr *LockManager) *LockObject {
	return &LockObject{mgr: mgr}
}

func (lo *LockObject) Get(name string) any        { return nil }
func (lo *LockObject) Set(name string, v any)     {}

// CallMethod implements interpreter.MethodCallable.
func (lo *LockObject) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "добавить", "add":
		if len(args) == 0 {
			return nil
		}
		reg, _ := args[0].(string)
		el := &LockElement{registerName: reg, values: map[string]any{}}
		lo.elements = append(lo.elements, el)
		return el
	case "заблокировать", "lock":
		if lo.mgr == nil {
			return nil
		}
		keys := lo.buildKeys()
		lo.mgr.Acquire(keys)
		lo.held = keys
		return nil
	case "разблокировать", "unlock":
		lo.ReleaseAll()
		return nil
	}
	return nil
}

// ReleaseAll отпускает удерживаемые мьютексы. Безопасно вызывать
// несколько раз. Используется как defer в handlers.go на случай если
// DSL забыл .Разблокировать().
func (lo *LockObject) ReleaseAll() {
	if lo.mgr != nil && len(lo.held) > 0 {
		lo.mgr.Release(lo.held)
		lo.held = nil
	}
}

// buildKeys формирует ключи блокировок в виде "регистр|изм1=знач&изм2=знач..."
// Значения отсортированы по имени измерения для детерминированности.
func (lo *LockObject) buildKeys() []string {
	keys := make([]string, 0, len(lo.elements))
	for _, el := range lo.elements {
		var pairs []string
		for k, v := range el.values {
			pairs = append(pairs, fmt.Sprintf("%s=%v", k, v))
		}
		sort.Strings(pairs)
		keys = append(keys, el.registerName+"|"+strings.Join(pairs, "&"))
	}
	return keys
}

// LockElement — отдельный элемент блокировки (соответствует
// БлокировкаДанных.Добавить()).
type LockElement struct {
	registerName string
	values       map[string]any
}

func (le *LockElement) Get(name string) any    { return nil }
func (le *LockElement) Set(name string, v any) {}

// CallMethod implements interpreter.MethodCallable.
func (le *LockElement) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "установитьзначение", "setvalue":
		if len(args) >= 2 {
			name, ok := args[0].(string)
			if !ok {
				return nil
			}
			le.values[strings.ToLower(name)] = args[1]
		}
	}
	return nil
}
