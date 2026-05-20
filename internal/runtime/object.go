package runtime

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

type Object struct {
	Type          string
	Kind          metadata.Kind
	ID            uuid.UUID
	Fields        map[string]any
	TablePartRows map[string][]map[string]any
}

func NewObject(entityType string, kind metadata.Kind) *Object {
	return &Object{
		Type:          entityType,
		Kind:          kind,
		ID:            uuid.New(),
		Fields:        make(map[string]any),
		TablePartRows: make(map[string][]map[string]any),
	}
}

func (o *Object) Get(name string) any {
	name = strings.ToLower(name)
	if o.TablePartRows != nil {
		for k, v := range o.TablePartRows {
			if strings.ToLower(k) == name {
				return v
			}
		}
	}
	for k, v := range o.Fields {
		if strings.ToLower(k) == name {
			return v
		}
	}
	return nil
}

func (o *Object) Set(name string, v any) {
	o.Fields[strings.ToLower(name)] = v
}

// GetRefUUID — реализует тот же интерфейс, что и *interpreter.Ref.
// Нужно для записи Object в reference:*-колонки регистра без
// двойной диспетчеризации в storage (см. замечание #17 и
// «unsupported type runtime.Object, a struct» при проведении).
func (o *Object) GetRefUUID() string {
	if o == nil {
		return ""
	}
	return o.ID.String()
}

// MomentTime — снимок «момента времени» для виртуальных таблиц регистров
// (замечание #1). Передаётся в .Остатки/.Обороты/.СрезПоследних как
// первый аргумент и обрабатывается query-translator'ом:
//   WHERE period < @Period OR (period = @Period AND recorder != @DocID)
// то есть «всё что было ДО этой документной строки». При перепроведении
// задним числом это даёт корректные остатки — текущий документ исключается
// из своей же сводки.
type MomentTime struct {
	Period  time.Time
	DocID   uuid.UUID
	DocType string // recorder_type для accumulation register
}

// PointInTime реализует контракт, который ищет query-translator (без импорта
// runtime). Возвращает значение Period и string-UUID документа.
func (m *MomentTime) PointInTime() (time.Time, string) {
	if m == nil {
		return time.Time{}, ""
	}
	return m.Period, m.DocID.String()
}

// CallMethod implements interpreter.MethodCallable so DSL can call
// `ЭтотОбъект.МоментВремени()` and `ЭтотОбъект.Дата` style ergonomics.
// Currently the only method is МоментВремени — returns *MomentTime
// initialized from the object's date field.
func (o *Object) CallMethod(method string, args []any) any {
	switch method {
	case "моментвремени", "pointintime":
		var p time.Time
		// первое непустое date-поле
		for _, k := range []string{"дата", "date", "период", "period"} {
			if v, ok := o.Fields[k]; ok && v != nil {
				if t, ok := v.(time.Time); ok && !t.IsZero() {
					p = t
					break
				}
			}
		}
		return &MomentTime{Period: p, DocID: o.ID, DocType: o.Type}
	}
	return nil
}

// String — display-имя объекта для записи в string-колонки регистра
// и DSL-функцию Строка(). Берём первое непустое поле «Наименование»
// (учётный стандарт 1С), иначе короткий префикс UUID. Без metadata
// мы не знаем «первого строкового поля», поэтому полагаемся на
// конвенцию имени поля.
func (o *Object) String() string {
	if o == nil {
		return ""
	}
	for _, k := range []string{"наименование", "name", "номер", "number"} {
		if v, ok := o.Fields[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" {
				return s
			}
		}
	}
	// fallback — короткий хвост UUID, чтобы не путаться при отладке
	id := o.ID.String()
	if len(id) >= 8 {
		return o.Type + ":" + id[:8]
	}
	return o.Type + ":" + id
}
