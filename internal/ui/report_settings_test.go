package ui

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	reportpkg "github.com/ivantit66/onebase/internal/report"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

func TestEffectiveComposition(t *testing.T) {
	main := &reportpkg.Composition{Groupings: []string{"Основной"}}
	variantComp := &reportpkg.Composition{Groupings: []string{"Вариант"}}
	override := &reportpkg.Composition{Groupings: []string{"Override"}}
	rep := &reportpkg.Report{
		Composition: main,
		Variants:    []reportpkg.ReportVariant{{Name: "V", Composition: variantComp}},
	}

	// 1) override (settings.Composition) перекрывает и вариант, и основной.
	if got := effectiveComposition(rep, &reportpkg.UserReportSettings{Variant: "V", Composition: override}); got != override {
		t.Fatalf("override: %+v", got)
	}
	// 2) settings без Composition → активный вариант по имени.
	if got := effectiveComposition(rep, &reportpkg.UserReportSettings{Variant: "V"}); got != variantComp {
		t.Fatalf("variant: %+v", got)
	}
	// 3) settings == nil → основной composition.
	if got := effectiveComposition(rep, nil); got != main {
		t.Fatalf("main: %+v", got)
	}
}

// TestReportSettingsPanel: панель «Настройки» рендерится при наличии ReportCols,
// содержит чекбоксы доступных полей и скрытое поле __settings.
func TestReportSettingsPanel(t *testing.T) {
	rep := &reportpkg.Report{Name: "sales", Title: "Продажи"}
	var buf bytes.Buffer
	data := map[string]any{
		"Report":       rep,
		"ParamValues":  map[string]any{},
		"ReportParams": []reportParamUI{},
		"ReportCols":   []string{"Товар", "Сумма"},
		"UserSettings": &reportpkg.UserReportSettings{
			Composition: &reportpkg.Composition{
				Groupings: []string{"Товар"},
				Measures:  []reportpkg.Measure{{Field: "Сумма", Agg: "sum"}},
			},
		},
		"Cfg":  Config{},
		"Lang": "ru",
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-report", data); err != nil {
		t.Fatalf("execute page-report: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `data-block="settings"`) {
		t.Fatalf("нет блока настроек data-block=settings")
	}
	if !strings.Contains(out, `name="__settings"`) {
		t.Fatalf("нет скрытого поля __settings")
	}
	for _, want := range []string{"Товар", "Сумма"} {
		if !strings.Contains(out, want) {
			t.Errorf("в панели нет поля %q", want)
		}
	}
}

// TestReportSettingsPanelHidden: без ReportCols панель не рендерится
// (обратная совместимость с отчётами без компоновки).
func TestReportSettingsPanelHidden(t *testing.T) {
	rep := &reportpkg.Report{Name: "plain", Title: "Простой"}
	var buf bytes.Buffer
	data := map[string]any{
		"Report":       rep,
		"ParamValues":  map[string]any{},
		"ReportParams": []reportParamUI{},
		"Cfg":          Config{},
		"Lang":         "ru",
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-report", data); err != nil {
		t.Fatalf("execute page-report: %v", err)
	}
	if strings.Contains(buf.String(), `data-block="settings"`) {
		t.Errorf("панель настроек не должна рендериться без ReportCols")
	}
}

// TestReportSettingsFilters: сохранённые отборы предзаполняют строки панели —
// select поля, select оператора (с выбранным gt) и значение.
func TestReportSettingsFilters(t *testing.T) {
	rep := &reportpkg.Report{Name: "sales", Title: "Продажи"}
	var buf bytes.Buffer
	data := map[string]any{
		"Report":       rep,
		"ParamValues":  map[string]any{},
		"ReportParams": []reportParamUI{},
		"ReportCols":   []string{"Товар", "Сумма"},
		"UserSettings": &reportpkg.UserReportSettings{
			Filters: []reportpkg.Filter{{Field: "Сумма", Op: "gt", Value: "100"}},
		},
		"Cfg":  Config{},
		"Lang": "ru",
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-report", data); err != nil {
		t.Fatalf("execute page-report: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="rs-f-field"`) {
		t.Errorf("нет select поля отбора")
	}
	if !strings.Contains(out, `class="rs-f-op"`) {
		t.Fatalf("нет select оператора отбора")
	}
	if !strings.Contains(out, `value="gt" selected`) {
		t.Errorf("оператор gt не помечен selected")
	}
	if !strings.Contains(out, `value="100"`) {
		t.Errorf("значение отбора 100 не предзаполнено")
	}
}

// TestReportSettingsSaveReset: обработчики save/reset пишут и удаляют per-user
// настройки в _settings (для анонима user="").
func TestReportSettingsSaveReset(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "rs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureSettingsSchema(ctx); err != nil {
		t.Fatal(err)
	}

	rep := &reportpkg.Report{Name: "Продажи"}
	registry := runtime.NewRegistry()
	registry.Load(runtime.LoadOptions{Reports: []*reportpkg.Report{rep}})
	s := &Server{store: db, reg: registry}

	raw := `{"variant":"X"}`
	form := url.Values{"__settings": {raw}}
	r := reqWithChi("POST", "/ui/report/Продажи/settings/save", form, map[string]string{"name": "Продажи"})
	w := httptest.NewRecorder()
	s.reportSettingsSave(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("save: ожидался 303, получен %d", w.Code)
	}
	if got, _ := db.GetReportUserSettings(ctx, "Продажи", ""); got != raw {
		t.Fatalf("save: хотели %q, получили %q", raw, got)
	}

	r2 := reqWithChi("POST", "/ui/report/Продажи/settings/reset", url.Values{}, map[string]string{"name": "Продажи"})
	w2 := httptest.NewRecorder()
	s.reportSettingsReset(w2, r2)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("reset: ожидался 303, получен %d", w2.Code)
	}
	if got, _ := db.GetReportUserSettings(ctx, "Продажи", ""); got != "" {
		t.Fatalf("reset: ожидали пусто, получили %q", got)
	}
}

// TestLoadUserSettings: автозагрузка сохранённых настроек по пользователю.
func TestLoadUserSettings(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "load.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureSettingsSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveReportUserSettings(ctx, "Продажи", "alice", `{"variant":"X"}`); err != nil {
		t.Fatal(err)
	}

	if s := loadUserSettings(ctx, db, "Продажи", "alice"); s == nil || s.Variant != "X" {
		t.Fatalf("alice: %+v", s)
	}
	if s := loadUserSettings(ctx, db, "Продажи", "bob"); s != nil {
		t.Fatalf("bob: ожидали nil, получили %+v", s)
	}
}

// TestReportSettingsIndicator: при активных настройках панель показывает
// пометку «изменено», а кнопка сброса активна; без настроек — кнопка disabled.
func TestReportSettingsIndicator(t *testing.T) {
	rep := &reportpkg.Report{Name: "sales", Title: "Продажи"}
	base := map[string]any{
		"Report":       rep,
		"ParamValues":  map[string]any{},
		"ReportParams": []reportParamUI{},
		"ReportCols":   []string{"Товар", "Сумма"},
		"Cfg":          Config{},
		"Lang":         "ru",
	}

	// С активными настройками.
	withSettings := map[string]any{}
	for k, v := range base {
		withSettings[k] = v
	}
	withSettings["UserSettings"] = &reportpkg.UserReportSettings{Variant: "X"}
	var b1 bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b1, "page-report", withSettings); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(b1.String(), "изменено") {
		t.Errorf("нет пометки «изменено» при активных настройках")
	}

	// Без настроек — кнопка сброса disabled.
	var b2 bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b2, "page-report", base); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := b2.String()
	if strings.Contains(out, "изменено") {
		t.Errorf("пометка «изменено» не должна показываться без настроек")
	}
	if !strings.Contains(out, "disabled") {
		t.Errorf("кнопка сброса должна быть disabled без настроек")
	}
}
