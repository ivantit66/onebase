package ui

// Рантайм-настройки отчёта (план 70): чтение пользовательских настроек из
// запроса и вычисление эффективной компоновки. Источник правок — панель
// «Настройки» на форме отчёта, которая пишет скрытое поле __settings (JSON
// report.UserReportSettings).

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/ivantit66/onebase/internal/auth"
	reportpkg "github.com/ivantit66/onebase/internal/report"
	"github.com/ivantit66/onebase/internal/storage"
)

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
// Приоритет: пользовательский override (settings.Composition) → выбранный
// вариант (settings.Variant) → основной composition конфигурации.
func effectiveComposition(rep *reportpkg.Report, s *reportpkg.UserReportSettings) *reportpkg.Composition {
	if s != nil && s.Composition != nil {
		return s.Composition
	}
	if s != nil {
		return rep.ActiveComposition(s.Variant)
	}
	return rep.ActiveComposition("")
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
	_ = s.store.SaveReportUserSettings(r.Context(), rep.Name, currentUserLogin(r), r.FormValue("__settings"))
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
