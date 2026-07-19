package ui

// Видимость разделов по правам и ролям. Панель разделов раньше показывала все
// подсистемы всем пользователям — у демо-ролей это давало разделы, где нет ни
// одного доступного объекта. Теперь список фильтруется как левое меню
// (buildNavFromContents): раздел виден, когда пользователю доступен хотя бы
// один объект из contents, а непустой whitelist `roles:` дополнительно требует
// одной из перечисленных ролей (та же семантика, что у страниц и HTTP-сервисов).

import (
	"net/http"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
)

// visibleSubsystems возвращает разделы для панели с учётом пользователя.
// Открытый деплой (нет пользователя в контексте) и админ видят всё.
func (s *Server) visibleSubsystems(r *http.Request) []*metadata.Subsystem {
	subs := s.reg.Subsystems()
	u := auth.UserFromContext(r.Context())
	if u == nil || u.IsAdmin {
		return subs
	}
	out := make([]*metadata.Subsystem, 0, len(subs))
	for _, sub := range subs {
		if s.subsystemVisible(r, u, sub) {
			out = append(out, sub)
		}
	}
	return out
}

func (s *Server) subsystemVisible(r *http.Request, u *auth.User, sub *metadata.Subsystem) bool {
	if len(sub.Roles) > 0 && !u.HasAnyRole(sub.Roles) {
		return false
	}
	c := &sub.Contents
	if c.IsEmpty() {
		// Раздел без объектов (только рабочий стол) — гейтить нечем.
		return true
	}
	for _, n := range c.Catalogs {
		if u.Has("catalog", n, "read") {
			return true
		}
	}
	for _, n := range c.Documents {
		if u.Has("document", n, "read") {
			return true
		}
	}
	for _, n := range c.Registers {
		if u.Has("register", n, "read") {
			return true
		}
	}
	for _, n := range c.InfoRegs {
		if u.Has("inforeg", n, "read") {
			return true
		}
	}
	for _, n := range c.Reports {
		if u.Has("report", n, "run") {
			return true
		}
	}
	for _, n := range c.Processors {
		if u.Has("processor", n, "run") {
			return true
		}
	}
	// Журнал объединяет документы: доступен, если читается хотя бы один из них.
	for _, jn := range c.Journals {
		if j := s.reg.GetJournal(jn); j != nil {
			for _, dn := range j.Documents {
				if u.Has("document", dn, "read") {
					return true
				}
			}
		}
	}
	for _, pn := range c.Pages {
		if pg := s.reg.GetPage(pn); pg != nil && s.canSeePage(r, pg) {
			return true
		}
	}
	return false
}

// requireSubsystemVisible пускает запрос рабочего стола с ?subsystem=, только
// если раздел виден пользователю: скрытый раздел нельзя открыть прямой ссылкой.
// Неизвестное имя не гейтим — поведение как раньше (нейтральный стол).
func (s *Server) requireSubsystemVisible(w http.ResponseWriter, r *http.Request) bool {
	name := r.URL.Query().Get("subsystem")
	if name == "" {
		return true
	}
	sub := s.reg.GetSubsystem(name)
	if sub == nil {
		return true
	}
	u := auth.UserFromContext(r.Context())
	if u == nil || u.IsAdmin || s.subsystemVisible(r, u, sub) {
		return true
	}
	s.renderForbidden(w, r)
	return false
}
