package ui

// Рантайм-настройки отчёта (план 70): чтение пользовательских настроек из
// запроса и вычисление эффективной компоновки. Источник правок — панель
// «Настройки» на форме отчёта, которая пишет скрытое поле __settings (JSON
// report.UserReportSettings).

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/ivantit66/onebase/internal/auth"
	reportpkg "github.com/ivantit66/onebase/internal/report"
	"github.com/ivantit66/onebase/internal/storage"
)

// maxUserSettingsBytes — потолок размера JSON пользовательских настроек отчёта
// (__settings), сохраняемого в _settings. Защищает от раздувания служебной
// таблицы произвольным вводом (issue #23). 64 КиБ с запасом покрывают любую
// разумную презентационную настройку (выбор/порядок колонок, отборы, сортировка).
const maxUserSettingsBytes = 64 * 1024

// errSettingsTooLarge — отказ сохранить слишком большой блок __settings (issue #23).
var errSettingsTooLarge = errors.New("настройки отчёта слишком велики")

// readReportSettings разбирает пользовательские настройки из поля __settings
// запроса (FormValue читает и POST-форму, и GET-query). Пустое или повреждённое
// значение → nil (поведение отчёта по умолчанию).
func readReportSettings(r *http.Request) *reportpkg.UserReportSettings {
	raw := r.FormValue("__settings")
	if raw == "" {
		return nil
	}
	s, err := reportpkg.ParseUserSettings(raw)
	if err != nil {
		return nil
	}
	return s
}

// effectiveComposition вычисляет компоновку, по которой строится отчёт.
//
// БЕЗОПАСНОСТЬ (issue #1): база компоновки берётся ИСКЛЮЧИТЕЛЬНО из доверенной
// конфигурации (rep.ActiveComposition(variant)) — YAML отчёта. Пользовательские
// настройки (__settings, клиентский ввод) применяются ТОЛЬКО к презентационным
// аспектам через mergeUserComposition. Исполняемые выражения — Measures[].Expr и
// Conditional[].When — а также условное оформление (Conditional целиком) и
// навигация (DetailLink/DetailEntity, Chart) всегда остаются из доверенного
// блока. Без этого пользователь с правом report:run мог бы прислать произвольное
// DSL-выражение в Composition и исполнить его на сервере (файловые builtins,
// SSRF, при exec.enabled — запуск команд ОС).
func effectiveComposition(rep *reportpkg.Report, s *reportpkg.UserReportSettings) *reportpkg.Composition {
	variant := ""
	if s != nil {
		variant = s.Variant
	}
	base := rep.ActiveComposition(variant)
	if s == nil || s.Composition == nil {
		return base
	}
	return mergeUserComposition(base, s.Composition)
}

// mergeUserComposition строит итоговую компоновку на базе доверенной (base) и
// накладывает только БЕЗОПАСНЫЕ презентационные правки из пользовательской (u):
//
//   - набор/порядок группировок и колонок (Groupings, Columns) — данные, не код;
//   - набор/порядок и презентация показателей (Measures): для каждого показателя
//     пользователя берём презентационные поля (Agg/Title/Align/Format), но Expr
//     ВСЕГДА из доверенного показателя с тем же Field; показатель, которого нет
//     в доверенной компоновке, добавляется БЕЗ Expr (Expr обнуляется), чтобы
//     инъекция вычисляемого показателя не исполнялась;
//   - сортировка (Sort), итоги (Totals), детальные строки (Detail).
//
// Из доверенной компоновки целиком наследуются исполняемые/оформительские и
// навигационные аспекты: Conditional (When+Style), Chart, DetailLink,
// DetailEntity. Они НИКОГДА не берутся из пользовательского ввода.
func mergeUserComposition(base, u *reportpkg.Composition) *reportpkg.Composition {
	if base == nil {
		// Нет доверенной базы (отчёт без composition) — не исполняем ничего из
		// пользовательского ввода: возвращаем пустую презентационную компоновку.
		base = &reportpkg.Composition{}
	}
	// Доверенные Expr показателей по имени поля (регистронезависимо, как DSL).
	trustedExpr := make(map[string]string, len(base.Measures))
	for _, m := range base.Measures {
		trustedExpr[strings.ToLower(m.Field)] = m.Expr
	}

	out := *base // копия: Conditional/Chart/DetailLink/DetailEntity — из доверенной.

	// Презентационные коллекции берём из пользовательского ввода (это данные).
	out.Groupings = append([]string(nil), u.Groupings...)
	out.Columns = append([]string(nil), u.Columns...)
	out.Sort = append([]reportpkg.SortKey(nil), u.Sort...)
	out.Totals = u.Totals
	out.Detail = u.Detail

	// Показатели: презентация — из пользовательского ввода, Expr — только из
	// доверенной компоновки (по совпадению Field), иначе обнуляем.
	if u.Measures != nil {
		measures := make([]reportpkg.Measure, 0, len(u.Measures))
		for _, m := range u.Measures {
			safe := reportpkg.Measure{
				Field:  m.Field,
				Agg:    m.Agg,
				Title:  m.Title,
				Align:  m.Align,
				Format: m.Format,
				// Expr НЕ из пользовательского ввода: берём доверенное значение
				// по имени поля; если показателя нет в доверенной — Expr пуст.
				Expr: trustedExpr[strings.ToLower(m.Field)],
			}
			measures = append(measures, safe)
		}
		out.Measures = measures
	}
	return &out
}

// loadUserSettings загружает сохранённые рантайм-настройки отчёта пользователя
// из _settings. Нет настроек или повреждённый JSON → nil (стандартный вид).
func loadUserSettings(ctx context.Context, store *storage.DB, report, user string) *reportpkg.UserReportSettings {
	raw, err := store.GetReportUserSettings(ctx, report, user)
	if err != nil || raw == "" {
		return nil
	}
	st, err := reportpkg.ParseUserSettings(raw)
	if err != nil {
		return nil
	}
	return st
}

// currentUserLogin возвращает логин текущего пользователя или "" для анонимной/
// однопользовательской сессии (настройки хранятся под пустым пользователем).
func currentUserLogin(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.Login
	}
	return ""
}

// reportFormURL — путь формы отчёта для редиректа после save/reset.
func reportFormURL(name string) string {
	return "/ui/report/" + url.PathEscape(strings.ToLower(name))
}

// reportSettingsSave сохраняет рантайм-настройки текущего пользователя (POST
// поля __settings) и возвращает на форму отчёта.
func (s *Server) reportSettingsSave(w http.ResponseWriter, r *http.Request) {
	rep := s.getReport(w, r)
	if rep == nil {
		return
	}
	if !s.requirePerm(w, r, "report", rep.Name, "run") {
		return
	}
	raw := r.FormValue("__settings")
	// Лимит размера: не пускаем в _settings произвольно большой ввод (issue #23).
	if len(raw) > maxUserSettingsBytes {
		http.Error(w, s.errText(r, errSettingsTooLarge), http.StatusRequestEntityTooLarge)
		return
	}
	// Валидируем JSON и сохраняем реканонизированный вид (issue #23): битый JSON
	// отвергаем, а каноничная сериализация отбрасывает мусорные/лишние поля.
	st, err := reportpkg.ParseUserSettings(raw)
	if err != nil {
		http.Error(w, s.errText(r, err), http.StatusBadRequest)
		return
	}
	canon := ""
	if st != nil {
		if canon, err = st.JSON(); err != nil {
			http.Error(w, s.errText(r, err), http.StatusBadRequest)
			return
		}
	}
	_ = s.store.SaveReportUserSettings(r.Context(), rep.Name, currentUserLogin(r), canon)
	http.Redirect(w, r, reportFormURL(rep.Name), http.StatusSeeOther)
}

// reportSettingsReset удаляет рантайм-настройки текущего пользователя — возврат
// к стандартному виду из конфигурации — и возвращает на форму отчёта.
func (s *Server) reportSettingsReset(w http.ResponseWriter, r *http.Request) {
	rep := s.getReport(w, r)
	if rep == nil {
		return
	}
	if !s.requirePerm(w, r, "report", rep.Name, "run") {
		return
	}
	_ = s.store.DeleteReportUserSettings(r.Context(), rep.Name, currentUserLogin(r))
	http.Redirect(w, r, reportFormURL(rep.Name), http.StatusSeeOther)
}
