package runtime

import (
	"fmt"
	"strings"

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
