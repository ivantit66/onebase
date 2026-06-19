package report

import "encoding/json"

// Filter — пользовательский отбор по полю результата запроса. Применяется к
// строкам ДО компоновки (см. compose.ApplyFilters). Op: eq|ne|gt|ge|lt|le|contains.
type Filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// UserReportSettings — рантайм-настройки отчёта конкретного пользователя
// (план 70). Variant — имя варианта-базы (пусто = основной composition).
// Composition — эффективная компоновка целиком (не дельта): если задана,
// перекрывает и вариант, и основной блок. Filters — пользовательские отборы
// строк. Хранится одним JSON в служебной таблице _settings, конфигурацию (YAML)
// не меняет.
type UserReportSettings struct {
	Variant     string       `json:"variant"`
	Composition *Composition `json:"composition,omitempty"`
	Filters     []Filter     `json:"filters,omitempty"`
}

// JSON сериализует настройки для хранения в _settings.
func (s *UserReportSettings) JSON() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseUserSettings разбирает JSON настроек. Пустая строка (нет сохранённых
// настроек) — не ошибка: возвращается (nil, nil).
func ParseUserSettings(raw string) (*UserReportSettings, error) {
	if raw == "" {
		return nil, nil
	}
	var s UserReportSettings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, err
	}
	return &s, nil
}
