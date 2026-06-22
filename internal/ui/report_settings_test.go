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
	rep := &reportpkg.Report{
		Composition: main,
		Variants:    []reportpkg.ReportVariant{{Name: "V", Composition: variantComp}},
	}

	// 1) settings.Composition применяется ПРЕЗЕНТАЦИОННО поверх доверенной базы
	//    (вариант по имени), а не подменяет её целиком (issue #1).
	override := &reportpkg.Composition{Groupings: []string{"Override"}}
	got := effectiveComposition(rep, &reportpkg.UserReportSettings{Variant: "V", Composition: override})
	if len(got.Groupings) != 1 || got.Groupings[0] != "Override" {
		t.Fatalf("презентационные правки не применены: %+v", got.Groupings)
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

// TestEffectiveCompositionNoDSLExecution: ключевой тест безопасности (issue #1).
// Пользователь присылает __settings с вычисляемым показателем (Expr) и условием
// оформления (When), содержащими вредоносное DSL. Эффективная компоновка НЕ
// должна нести этот Expr/When — исполняемые выражения берутся только из
// доверенной конфигурации (или обнуляются для показателей, которых там нет).
func TestEffectiveCompositionNoDSLExecution(t *testing.T) {
	const evil = `ЗапуститьПриложение("calc.exe")`
	// Доверенная конфигурация: один обычный показатель «Сумма» и один доверенный
	// вычисляемый «Маржа». Условие оформления — безопасное.
	trusted := &reportpkg.Composition{
		Groupings: []string{"Товар"},
		Measures: []reportpkg.Measure{
			{Field: "Сумма", Agg: "sum"},
			{Field: "Маржа", Expr: "Сумма * 0.2"},
		},
		Conditional: []reportpkg.CondRule{
			{When: "Сумма < 0", Field: "", Style: reportpkg.CellStyle{Color: "#c00"}},
		},
	}
	rep := &reportpkg.Report{Composition: trusted}

	// Пользовательский ввод: пытается (а) внедрить новый показатель с вредоносным
	// Expr, (б) подменить Expr доверенного показателя, (в) подменить When условия.
	userComp := &reportpkg.Composition{
		Groupings: []string{"Товар"},
		Measures: []reportpkg.Measure{
			{Field: "Сумма", Agg: "sum"},
			{Field: "Маржа", Expr: evil},           // подмена доверенного Expr
			{Field: "Хак", Agg: "sum", Expr: evil}, // внедрённый показатель с Expr
		},
		Conditional: []reportpkg.CondRule{
			{When: evil, Field: "", Style: reportpkg.CellStyle{Color: "red"}},
		},
	}
	eff := effectiveComposition(rep, &reportpkg.UserReportSettings{Composition: userComp})

	// Ни один Expr/When эффективной компоновки не должен равняться вредоносному.
	for _, m := range eff.Measures {
		if strings.Contains(m.Expr, evil) {
			t.Fatalf("вредоносный Expr протёк в показатель %q: %q", m.Field, m.Expr)
		}
	}
	for _, c := range eff.Conditional {
		if strings.Contains(c.When, evil) {
			t.Fatalf("вредоносное условие When протекло: %q", c.When)
		}
	}
	// Доверенный Expr «Маржа» сохранён из конфигурации (а не обнулён/подменён).
	var marja *reportpkg.Measure
	for i := range eff.Measures {
		if eff.Measures[i].Field == "Маржа" {
			marja = &eff.Measures[i]
		}
	}
	if marja == nil || marja.Expr != "Сумма * 0.2" {
		t.Fatalf("доверенный Expr «Маржа» должен быть из конфигурации, got %+v", marja)
	}
	// Внедрённый показатель «Хак» (нет в доверенной) — без Expr (не исполняется).
	for _, m := range eff.Measures {
		if m.Field == "Хак" && m.Expr != "" {
			t.Fatalf("внедрённый показатель «Хак» не должен иметь Expr, got %q", m.Expr)
		}
	}
	// Условия оформления (When+Style) целиком из доверенной конфигурации.
	if len(eff.Conditional) != 1 || eff.Conditional[0].When != "Сумма < 0" {
		t.Fatalf("Conditional должен быть из доверенной конфигурации, got %+v", eff.Conditional)
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

// TestReportSettingsSaveRejectsLargeAndInvalid: save отвергает слишком большой
// блок __settings и битый JSON, а корректный — сохраняет в каноничном виде (issue #23).
func TestReportSettingsSaveRejectsLargeAndInvalid(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "rs23.db"))
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

	// 1) Слишком большое значение → 413, в БД ничего не записано.
	big := strings.Repeat("x", maxUserSettingsBytes+1)
	form := url.Values{"__settings": {big}}
	r := reqWithChi("POST", "/ui/report/Продажи/settings/save", form, map[string]string{"name": "Продажи"})
	w := httptest.NewRecorder()
	s.reportSettingsSave(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("большое значение: ожидали 413, получили %d", w.Code)
	}
	if got, _ := db.GetReportUserSettings(ctx, "Продажи", ""); got != "" {
		t.Fatalf("большое значение не должно сохраняться, got %q", got)
	}

	// 2) Битый JSON → 400, ничего не записано.
	form2 := url.Values{"__settings": {"{не json"}}
	r2 := reqWithChi("POST", "/ui/report/Продажи/settings/save", form2, map[string]string{"name": "Продажи"})
	w2 := httptest.NewRecorder()
	s.reportSettingsSave(w2, r2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("битый JSON: ожидали 400, получили %d", w2.Code)
	}
	if got, _ := db.GetReportUserSettings(ctx, "Продажи", ""); got != "" {
		t.Fatalf("битый JSON не должен сохраняться, got %q", got)
	}

	// 3) Корректное значение → 303, в БД лежит каноничный JSON.
	form3 := url.Values{"__settings": {`{"variant":"X"}`}}
	r3 := reqWithChi("POST", "/ui/report/Продажи/settings/save", form3, map[string]string{"name": "Продажи"})
	w3 := httptest.NewRecorder()
	s.reportSettingsSave(w3, r3)
	if w3.Code != http.StatusSeeOther {
		t.Fatalf("корректное значение: ожидали 303, получили %d", w3.Code)
	}
	got, _ := db.GetReportUserSettings(ctx, "Продажи", "")
	if got == "" || !strings.Contains(got, `"variant":"X"`) {
		t.Fatalf("каноничный JSON не сохранён: %q", got)
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
