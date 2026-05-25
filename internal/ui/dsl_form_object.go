package ui

import (
	"strings"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

// formObjectThis — обёртка над *runtime.Object, используемая как this/Объект
// в рантайме событий управляемых форм (план 37, этап 8).
//
// Разница с прямой передачей *runtime.Object в interp.Run:
//
//  1. `Объект.Товары` возвращает *formTpProxy, который умеет CallMethod
//     "Добавить"/"Очистить"/"Количество". Без этого `Объект.Товары.Добавить()`
//     в DSL ничего не делает — на пустой slice метод не вызывается.
//
//  2. Set по реквизитам формы (например, Объект.КешИтога) кладёт значение в
//     Fields, как и Object.Set; рантайм событий не знает про form-attributes
//     отдельно от полей сущности — это упрощение MVP, реквизиты формы
//     неотличимы от полей объекта.
//
// formObjectThis передаётся в interp.Run и под именем «Объект», и под
// «ЭтотОбъект» — потому что в 1С-управляемых формах принято писать
// `Объект.Поле`, а в обработках OnPost — `ЭтотОбъект.Поле`. Делаем оба
// варианта рабочими.
type formObjectThis struct {
	obj    *runtime.Object
	entity *metadata.Entity
}

func (f *formObjectThis) Get(name string) any {
	if f == nil || f.obj == nil {
		return nil
	}
	nameLower := strings.ToLower(name)
	// Сначала — табличные части. Возвращаем прокси даже если slice ещё nil,
	// чтобы .Добавить() мог создать первую строку.
	if f.entity != nil {
		for _, tp := range f.entity.TableParts {
			if strings.ToLower(tp.Name) == nameLower {
				return &formTpProxy{obj: f.obj, tpName: tp.Name}
			}
		}
	}
	// Дальше — обычные поля (через Object.Get который ищет в Fields).
	v := f.obj.Get(name)
	// Дефолты по типу: пустой numeric → 0, иначе `Объект.Сумма + 100` в DSL
	// даст concat-строку «<nil>100» (DSL `+` для nil-операнда склеивает
	// строкой), потом форма попытается записать её в PostgreSQL numeric →
	// ERROR 22P02 invalid input syntax for type numeric.
	if v == nil && f.entity != nil {
		for _, fd := range f.entity.Fields {
			if !strings.EqualFold(fd.Name, name) {
				continue
			}
			switch fd.Type {
			case metadata.FieldTypeNumber:
				return float64(0)
			case metadata.FieldTypeBool:
				return false
			}
			break
		}
	}
	return v
}

func (f *formObjectThis) Set(name string, v any) {
	if f == nil || f.obj == nil {
		return
	}
	f.obj.Set(name, v)
}

// formTpProxy — proxy табличной части для рантайма событий формы. В отличие
// от tpProxy (см. dsl_documents.go), привязан напрямую к *runtime.Object, без
// docWriter — потому что в обработчиках формы документ ещё не записан и нет
// открытой транзакции записи.
type formTpProxy struct {
	obj    *runtime.Object
	tpName string
}

func (t *formTpProxy) Get(_ string) any    { return nil }
func (t *formTpProxy) Set(_ string, _ any) {}

func (t *formTpProxy) CallMethod(method string, args []any) any {
	if t == nil || t.obj == nil {
		return nil
	}
	switch strings.ToLower(method) {
	case "добавить", "add":
		if t.obj.TablePartRows == nil {
			t.obj.TablePartRows = map[string][]map[string]any{}
		}
		row := map[string]any{}
		t.obj.TablePartRows[t.tpName] = append(t.obj.TablePartRows[t.tpName], row)
		return &interpreter.MapThis{M: row}
	case "очистить", "clear":
		if t.obj.TablePartRows != nil {
			t.obj.TablePartRows[t.tpName] = nil
		}
	case "количество", "count":
		if t.obj.TablePartRows == nil {
			return float64(0)
		}
		return float64(len(t.obj.TablePartRows[t.tpName]))
	}
	return nil
}

// IterateRows — для `Для Каждого Стр Из Объект.Товары Цикл` интерпретатор
// должен видеть массив строк. Возвращаем срез map'ов; элементы массива
// автоматически оборачиваются в MapThis при доступе через DSL.
func (t *formTpProxy) IterateRows() []map[string]any {
	if t == nil || t.obj == nil || t.obj.TablePartRows == nil {
		return nil
	}
	return t.obj.TablePartRows[t.tpName]
}
