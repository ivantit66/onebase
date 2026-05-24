package onec_forms

import "github.com/ivantit66/onebase/internal/metadata"

// eventMap — соответствие имён событий: ключ — имя из Form.xml (англ),
// значение — FormEventType OneBase (русский).
var eventMap = map[string]metadata.FormEventType{
	"OnOpen":           metadata.FormEventOnOpen,
	"BeforeWrite":      metadata.FormEventBeforeWrite,
	"OnWrite":          metadata.FormEventOnWrite,
	"AfterWrite":       metadata.FormEventAfterWrite,
	"BeforeClose":      metadata.FormEventBeforeClose,
	"OnClose":          metadata.FormEventOnClose,
	"OnCreateAtServer": metadata.FormEventOnCreate,
	"OnChange":         metadata.FormEventOnChange,
	"OnActivate":       metadata.FormEventOnActivate,
	"Choice":           metadata.FormEventItemChoice,
	"Choose":           metadata.FormEventItemChoice,
	"StartChoice":      metadata.FormEventStartChoice,
	"AfterDeleteRow":   metadata.FormEventBeforeRowDelete, // ближайший аналог; см. mapping_in
	"BeforeAddRow":     metadata.FormEventBeforeRowAdd,
	"OnClick":          metadata.FormEventOnClick,
	"Click":            metadata.FormEventOnClick,
	"Command":          metadata.FormEventExecuteCommand,
	"StartListChoice":  metadata.FormEventStartListChoice,
	"AutoComplete":     metadata.FormEventAutoComplete,
}

var eventMapInverse map[metadata.FormEventType]string

func init() {
	eventMapInverse = make(map[metadata.FormEventType]string, len(eventMap))
	for k, v := range eventMap {
		if _, exists := eventMapInverse[v]; !exists {
			eventMapInverse[v] = k
		}
	}
}

// Event1CToOneBase возвращает FormEventType OneBase по имени события 1С.
func Event1CToOneBase(name1c string) (metadata.FormEventType, bool) {
	v, ok := eventMap[name1c]
	return v, ok
}

// EventOneBaseTo1C — обратное направление.
func EventOneBaseTo1C(t metadata.FormEventType) (string, bool) {
	v, ok := eventMapInverse[t]
	return v, ok
}
