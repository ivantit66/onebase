package launcher

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/storage"
	"github.com/ivantit66/onebase/internal/version"
	"gopkg.in/yaml.v3"
)

// ── Admin panel handlers for configurator ────────────────────────────────────

func (h *handler) cfgAdminUsers(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		w.Write([]byte(`<div style="padding:16px;color:#c00">Нет подключения к БД</div>`))
		return
	}
	repo := auth.NewRepo(db)
	repo.EnsureSchema(r.Context())
	users, _ := repo.List(r.Context())

	html := `<div style="padding:16px">
	<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:14px">
	  <h3 style="margin:0;font-size:15px">Пользователи</h3>
	  <button onclick="cfgUserNew()" style="background:#1a5fa8;color:#fff;border:none;padding:5px 14px;border-radius:3px;cursor:pointer;font-size:12px">+ Добавить</button>
	</div>
	<div id="cfg-user-new" style="display:none;margin-bottom:14px;padding:12px;background:#f8fafc;border:1px solid #e2e8f0;border-radius:4px">
	  <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:end">
	    <div style="flex:1;min-width:120px"><label style="font-size:11px;color:#666">Логин</label><input id="cfg-un" style="width:100%;padding:5px 7px;border:1px solid #ccc;border-radius:3px;font-size:12px"></div>
	    <div style="flex:1;min-width:120px"><label style="font-size:11px;color:#666">Пароль</label><input id="cfg-up" type="password" style="width:100%;padding:5px 7px;border:1px solid #ccc;border-radius:3px;font-size:12px"></div>
	    <div style="flex:1;min-width:120px"><label style="font-size:11px;color:#666">Полное имя</label><input id="cfg-ufn" style="width:100%;padding:5px 7px;border:1px solid #ccc;border-radius:3px;font-size:12px"></div>
	    <label style="font-size:12px;display:flex;align-items:center;gap:4px"><input type="checkbox" id="cfg-ua"> Админ</label>
	    <button onclick="cfgUserCreate()" style="background:#16a34a;color:#fff;border:none;padding:5px 12px;border-radius:3px;cursor:pointer;font-size:12px">Создать</button>
	    <button onclick="document.getElementById('cfg-user-new').style.display='none'" style="background:#e2e8f0;color:#333;border:none;padding:5px 10px;border-radius:3px;cursor:pointer;font-size:12px">Отмена</button>
	  </div>
	  <div id="cfg-user-err" style="color:#c00;font-size:11px;margin-top:6px;display:none"></div>
	</div>
	<table style="width:100%;border-collapse:collapse;font-size:12px">
	<tr style="background:#f1f5f9"><th style="text-align:left;padding:6px 8px;font-weight:600">Логин</th><th style="text-align:left;padding:6px 8px;font-weight:600">Имя</th><th style="text-align:center;padding:6px 8px;font-weight:600">Админ</th><th style="text-align:left;padding:6px 8px;font-weight:600">Создан</th><th style="padding:6px 8px"></th></tr>`
	for i, u := range users {
		bg := ""
		if i%2 == 1 {
			bg = ` style="background:#f9fafb"`
		}
		admin := ""
		if u.IsAdmin {
			admin = `<span style="color:#16a34a;font-weight:600">Да</span>`
		}
		denyIcon := "🔓"
		denyTitle := "Запретить смену пароля"
		denyStyle := "background:#e2e8f0;color:#374151"
		if u.DenyPasswdChange {
			denyIcon = "🔒"
			denyTitle = "Снять запрет смены пароля"
			denyStyle = "background:#dc2626;color:#fff"
		}
		listIcon := "👁"
		listTitle := "Показывать в списке выбора"
		listStyle := "background:#e2e8f0;color:#374151"
		if u.ShowInList {
			listTitle = "Убрать из списка выбора"
			listStyle = "background:#2563eb;color:#fff"
		}
		html += fmt.Sprintf(`<tr%s><td style="padding:5px 8px">%s</td><td style="padding:5px 8px">%s</td><td style="padding:5px 8px;text-align:center">%s</td><td style="padding:5px 8px;color:#888">%s</td><td style="padding:5px 8px;white-space:nowrap"><button onclick="cfgUserRoles('%s')" style="background:#0e7490;color:#fff;border:none;padding:3px 8px;border-radius:3px;cursor:pointer;font-size:11px;margin-right:4px">Роли</button><button onclick="cfgUserPasswd('%s')" style="background:#f59e0b;color:#fff;border:none;padding:3px 8px;border-radius:3px;cursor:pointer;font-size:11px;margin-right:4px">Пароль</button><button onclick="cfgUserDenyPasswd('%s',%v)" title="%s" style="%s;border:none;padding:3px 8px;border-radius:3px;cursor:pointer;font-size:11px;margin-right:4px">%s</button><button onclick="cfgUserShowInList('%s',%v)" title="%s" style="%s;border:none;padding:3px 8px;border-radius:3px;cursor:pointer;font-size:11px;margin-right:4px">%s</button><button onclick="cfgUserDel('%s')" style="color:#c00;background:none;border:none;cursor:pointer;font-size:11px" title="Удалить">✕</button></td></tr>`,
			bg, escHTML(u.Login), escHTML(u.FullName), admin, u.CreatedAt.Format("02.01.2006"),
			u.ID,
			u.ID,
			u.ID, u.DenyPasswdChange, denyTitle, denyStyle, denyIcon,
			u.ID, u.ShowInList, listTitle, listStyle, listIcon,
			u.ID)
	}
	if len(users) == 0 {
		html += `<tr><td colspan="5" style="padding:20px;text-align:center;color:#999">Нет пользователей</td></tr>`
	}
	html += `</table></div>
<script>
function cfgUserNew(){document.getElementById('cfg-user-new').style.display='block';document.getElementById('cfg-un').focus()}
function cfgUserRoles(id){cfgAdmin('users/roles?uid='+encodeURIComponent(id))}
function cfgUserCreate(){
  var d={login:document.getElementById('cfg-un').value,password:document.getElementById('cfg-up').value,fullName:document.getElementById('cfg-ufn').value,isAdmin:document.getElementById('cfg-ua').checked};
  fetch('/bases/` + b.ID + `/configurator/admin/users/create',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(d)})
    .then(function(r){return r.json()}).then(function(r){
      if(r.error){document.getElementById('cfg-user-err').textContent=r.error;document.getElementById('cfg-user-err').style.display='block';return}
      cfgAdmin('users')
    })
}
function cfgUserDel(id){
  if(!confirm('Удалить пользователя?'))return;
  fetch('/bases/` + b.ID + `/configurator/admin/users/delete',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:id})})
    .then(function(){cfgAdmin('users')})
}
function cfgUserPasswd(id){
  var pw=prompt('Введите новый пароль:');
  if(!pw)return;
  if(pw.length<4){alert('Пароль должен содержать минимум 4 символа');return}
  var pw2=prompt('Повторите новый пароль:');
  if(pw!==pw2){alert('Пароли не совпадают');return}
  fetch('/bases/` + b.ID + `/configurator/admin/users/passwd',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:id,password:pw})})
    .then(function(r){return r.json()}).then(function(r){
      if(r.error){alert('Ошибка: '+r.error)}else{alert('Пароль изменён')}
    })
}
function cfgUserDenyPasswd(id,current){
  fetch('/bases/` + b.ID + `/configurator/admin/users/deny-passwd',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:id,deny:!current})})
    .then(function(r){return r.json()}).then(function(r){
      if(r.error){alert('Ошибка: '+r.error)}else{cfgAdmin('users')}
    })
}
function cfgUserShowInList(id,current){
  fetch('/bases/` + b.ID + `/configurator/admin/users/show-in-list',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:id,show:!current})})
    .then(function(r){return r.json()}).then(function(r){
      if(r.error){alert('Ошибка: '+r.error)}else{cfgAdmin('users')}
    })
}
</script>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (h *handler) cfgAdminUserCreate(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
		FullName string `json:"fullName"`
		IsAdmin  bool   `json:"isAdmin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	if req.Login == "" || req.Password == "" {
		writeJSON(w, 400, map[string]any{"error": "Логин и пароль обязательны"})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	repo := auth.NewRepo(db)
	if _, err := repo.Create(r.Context(), req.Login, req.Password, req.FullName, req.IsAdmin); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) cfgAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	repo := auth.NewRepo(db)
	if err := repo.Delete(r.Context(), req.ID); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) cfgAdminUserPasswd(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var req struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	if req.ID == "" || len(req.Password) < 4 {
		writeJSON(w, 400, map[string]any{"error": "Пароль должен содержать минимум 4 символа"})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	repo := auth.NewRepo(db)
	if err := repo.UpdatePassword(r.Context(), req.ID, req.Password); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) cfgAdminUserDenyPasswd(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var req struct {
		ID   string `json:"id"`
		Deny bool   `json:"deny"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	repo := auth.NewRepo(db)
	if err := repo.SetDenyPasswdChange(r.Context(), req.ID, req.Deny); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) cfgAdminUserShowInList(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var req struct {
		ID   string `json:"id"`
		Show bool   `json:"show"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	repo := auth.NewRepo(db)
	if err := repo.SetShowInList(r.Context(), req.ID, req.Show); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) cfgAdminSessions(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		w.Write([]byte(`<div style="padding:16px;color:#c00">Нет подключения к БД</div>`))
		return
	}
	repo := auth.NewRepo(db)
	repo.EnsureSchema(r.Context())
	sessions, _ := repo.ActiveSessions(r.Context())

	html := `<div style="padding:16px">
	<h3 style="margin:0 0 14px;font-size:15px">Активные пользователи</h3>
	<table style="width:100%;border-collapse:collapse;font-size:12px">
	<tr style="background:#f1f5f9"><th style="text-align:left;padding:6px 8px;font-weight:600">Логин</th><th style="text-align:left;padding:6px 8px;font-weight:600">Имя</th><th style="text-align:center;padding:6px 8px;font-weight:600">Админ</th><th style="text-align:left;padding:6px 8px;font-weight:600">Действует до</th><th style="padding:6px 8px"></th></tr>`
	for i, s := range sessions {
		bg := ""
		if i%2 == 1 {
			bg = ` style="background:#f9fafb"`
		}
		admin := ""
		if s.IsAdmin {
			admin = `<span style="color:#16a34a;font-weight:600">Да</span>`
		}
		html += fmt.Sprintf(`<tr%s><td style="padding:5px 8px">%s</td><td style="padding:5px 8px">%s</td><td style="padding:5px 8px;text-align:center">%s</td><td style="padding:5px 8px;color:#888">%s</td><td style="padding:5px 8px"><button onclick="cfgKick('%s')" style="color:#c00;background:none;border:none;cursor:pointer;font-size:11px" title="Завершить сессию">✕</button></td></tr>`,
			bg, escHTML(s.Login), escHTML(s.FullName), admin, s.ExpiresAt.Format("02.01.2006 15:04"), escHTML(s.Login))
	}
	if len(sessions) == 0 {
		html += `<tr><td colspan="5" style="padding:20px;text-align:center;color:#999">Нет активных сессий</td></tr>`
	}
	html += `</table></div>
<script>
function cfgKick(login){
  if(!confirm('Завершить сессию '+login+'?'))return;
  fetch('/bases/` + b.ID + `/configurator/admin/sessions/kick',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({login:login})})
    .then(function(){cfgAdmin('sessions')})
}
</script>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (h *handler) cfgAdminSessionKick(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var req struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	repo := auth.NewRepo(db)
	repo.KickUser(r.Context(), req.Login)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) cfgAdminAudit(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		w.Write([]byte(`<div style="padding:16px;color:#c00">Нет подключения к БД</div>`))
		return
	}
	s := db.GetAuditSettings(r.Context())
	ck := func(on bool) string {
		if on {
			return " checked"
		}
		return ""
	}

	// ── Блок настроек журнала ──
	html := `<div style="padding:16px">
	<h3 style="margin:0 0 14px;font-size:15px">Журнал регистрации</h3>
	<details style="margin-bottom:18px">
	  <summary style="cursor:pointer;font-size:13px;font-weight:600;padding:6px 0;user-select:none">⚙ Настройки журнала</summary>
	  <div style="margin-top:8px;padding:12px;background:#f8fafc;border:1px solid #e2e8f0;border-radius:4px">
	  <label style="font-size:13px;font-weight:600;display:flex;align-items:center;gap:6px">
	    <input type="checkbox" id="au-enabled"` + ck(s.Enabled) + `> Вести журнал регистрации</label>
	  <div style="margin:10px 0 0 22px;display:flex;flex-direction:column;gap:5px">
	    <div style="font-size:11px;color:#666;margin-bottom:2px">Регистрировать события:</div>
	    <label style="font-size:12px;display:flex;align-items:center;gap:6px"><input type="checkbox" id="au-create"` + ck(s.Create) + `> Создание объектов</label>
	    <label style="font-size:12px;display:flex;align-items:center;gap:6px"><input type="checkbox" id="au-update"` + ck(s.Update) + `> Изменение объектов</label>
	    <label style="font-size:12px;display:flex;align-items:center;gap:6px"><input type="checkbox" id="au-delete"` + ck(s.Delete) + `> Удаление объектов</label>
	    <label style="font-size:12px;display:flex;align-items:center;gap:6px"><input type="checkbox" id="au-post"` + ck(s.Post) + `> Проведение / отмена</label>
	    <label style="font-size:12px;display:flex;align-items:center;gap:6px"><input type="checkbox" id="au-login"` + ck(s.Login) + `> Вход / выход пользователей</label>
	  </div>
	  <button onclick="cfgAuditSave()" style="margin-top:10px;background:#16a34a;color:#fff;border:none;padding:5px 14px;border-radius:3px;cursor:pointer;font-size:12px">Сохранить</button>
	  <span id="au-msg" style="font-size:11px;margin-left:8px"></span>
	  </div>
	</details>
	<div style="font-size:12px;color:#666;margin-bottom:8px">Последние записи:</div>
	<table style="width:100%;border-collapse:collapse;font-size:12px">
	<tr style="background:#f1f5f9"><th style="text-align:left;padding:6px 8px;font-weight:600">Время</th><th style="text-align:left;padding:6px 8px;font-weight:600">Пользователь</th><th style="text-align:left;padding:6px 8px;font-weight:600">Действие</th><th style="text-align:left;padding:6px 8px;font-weight:600">Объект</th></tr>`

	i := 0
	entries, _ := db.AuditSearch(r.Context(), storage.AuditFilter{}, 100, 0)
	for _, e := range entries {
		bg := ""
		if i%2 == 1 {
			bg = ` style="background:#f9fafb"`
		}
		obj := escHTML(e.EntityName)
		if e.EntityKind != "" {
			obj = escHTML(e.EntityKind) + ": " + obj
		}
		who := escHTML(e.UserLogin)
		if who == "" {
			who = `<span style="color:#999">(анонимно)</span>`
		}
		html += fmt.Sprintf(`<tr%s><td style="padding:5px 8px;color:#888;white-space:nowrap">%s</td><td style="padding:5px 8px">%s</td><td style="padding:5px 8px">%s</td><td style="padding:5px 8px">%s</td></tr>`,
			bg, e.At.Format("02.01.2006 15:04:05"), who, escHTML(e.Action), obj)
		i++
	}
	if i == 0 {
		html += `<tr><td colspan="4" style="padding:20px;text-align:center;color:#999">Журнал пуст</td></tr>`
	}
	html += `</table></div>
<script>
function cfgAuditSave(){
  var body={
    enabled:document.getElementById('au-enabled').checked,
    create:document.getElementById('au-create').checked,
    update:document.getElementById('au-update').checked,
    "delete":document.getElementById('au-delete').checked,
    post:document.getElementById('au-post').checked,
    login:document.getElementById('au-login').checked
  };
  fetch('/bases/` + b.ID + `/configurator/admin/audit/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
    .then(function(r){return r.json()})
    .then(function(d){
      var m=document.getElementById('au-msg');
      if(d.ok){m.textContent='Сохранено';m.style.color='#16a34a';}
      else{m.textContent=(d.error||'Ошибка');m.style.color='#c00';}
    })
    .catch(function(){var m=document.getElementById('au-msg');m.textContent='Ошибка сети';m.style.color='#c00';});
}
</script>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// cfgAdminAuditSave сохраняет настройки журнала регистрации.
func (h *handler) cfgAdminAuditSave(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
		Create  bool `json:"create"`
		Update  bool `json:"update"`
		Delete  bool `json:"delete"`
		Post    bool `json:"post"`
		Login   bool `json:"login"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	s := storage.AuditSettings{
		Enabled: req.Enabled, Create: req.Create, Update: req.Update,
		Delete: req.Delete, Post: req.Post, Login: req.Login,
	}
	if err := db.SaveAuditSettings(r.Context(), s); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) cfgAdminAbout(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	// Load app config for configuration name/version/logo
	type appCfg struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
		Logo    string `yaml:"logo"`
	}
	var cfg appCfg
	if b.ConfigSource == "database" {
		db, dbErr := OpenDB(r.Context(), b)
		if dbErr == nil {
			defer db.Close()
			var content []byte
			if qrErr := db.QueryRow(r.Context(), `SELECT content FROM _onebase_config WHERE path = $1`, "config/app.yaml").Scan(&content); qrErr == nil {
				yaml.Unmarshal(content, &cfg)
			}
		}
	} else if b.Path != "" {
		data, err := os.ReadFile(filepath.Join(b.Path, "config", "app.yaml"))
		if err == nil {
			yaml.Unmarshal(data, &cfg)
		}
	}

	// Load logo — use endpoint URL so it works for both file and database sources
	logoHTML := `<div style="font-size:48px;margin-bottom:8px">&#9889;</div>`
	if cfg.Logo != "" {
		logoHTML = fmt.Sprintf(`<img src="/bases/%s/configurator/logo" alt="Logo" style="max-height:140px;max-width:300px">`, b.ID)
	}

	cfgRows := ""
	if cfg.Name != "" {
		cfgRows += fmt.Sprintf(`<tr><td style="padding:6px 0;color:#888;width:140px">Конфигурация</td><td style="padding:6px 0;font-weight:600">%s</td></tr>`, escHTML(cfg.Name))
	}
	if cfg.Version != "" {
		cfgRows += fmt.Sprintf(`<tr><td style="padding:6px 0;color:#888">Версия конфигурации</td><td style="padding:6px 0">%s</td></tr>`, escHTML(cfg.Version))
	}

	userRow := ""
	if u := cfgUserFromContext(r.Context()); u != nil {
		label := escHTML(u.Login)
		if u.FullName != "" {
			label += " <span style='color:#888;font-weight:400'>" + escHTML(u.FullName) + "</span>"
		}
		userRow = fmt.Sprintf(`<tr><td style="padding:6px 0;color:#888;width:140px">Пользователь</td><td style="padding:6px 0">%s</td></tr>`, label)
	}

	html := fmt.Sprintf(`<div style="padding:24px;max-width:400px">
	<div style="text-align:center;margin-bottom:20px">
	  %s
	  <div style="font-size:18px;font-weight:600;color:#1a5fa8">OneBase</div>
	</div>
	<table style="width:100%%;border-collapse:collapse;font-size:13px">
	%s
	<tr><td style="padding:6px 0;color:#888;width:140px">Версия платформы</td><td style="padding:6px 0">%s</td></tr>
	%s
	<tr><td style="padding:6px 0;color:#888">Режим конфигурации</td><td style="padding:6px 0">%s</td></tr>
	<tr><td style="padding:6px 0;color:#888">База данных</td><td style="padding:6px 0">%s</td></tr>
	<tr><td style="padding:6px 0;color:#888">Порт</td><td style="padding:6px 0">:%d</td></tr>
	</table>
	</div>`,
		logoHTML,
		userRow,
		escHTML(version.String()),
		cfgRows,
		escHTML(b.ConfigSource),
		maskDSN(escHTML(b.DB)),
		b.Port)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (h *handler) configuratorLogo(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Load app config to find logo path
	type appCfg struct {
		Logo string `yaml:"logo"`
	}
	var cfg appCfg
	var logoData []byte

	if b.ConfigSource == "database" {
		db, cerr := OpenDB(r.Context(), b)
		if cerr != nil {
			http.NotFound(w, r)
			return
		}
		defer db.Close()
		// Get app.yaml
		var content []byte
		if err := db.QueryRow(r.Context(), `SELECT content FROM _onebase_config WHERE path = $1`, "config/app.yaml").Scan(&content); err != nil {
			http.NotFound(w, r)
			return
		}
		yaml.Unmarshal(content, &cfg)
		if cfg.Logo == "" {
			http.NotFound(w, r)
			return
		}
		// Get logo file
		if err := db.QueryRow(r.Context(), `SELECT content FROM _onebase_config WHERE path = $1`, cfg.Logo).Scan(&logoData); err != nil {
			http.NotFound(w, r)
			return
		}
	} else {
		if b.Path == "" {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(filepath.Join(b.Path, "config", "app.yaml"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		yaml.Unmarshal(data, &cfg)
		if cfg.Logo == "" {
			http.NotFound(w, r)
			return
		}
		logoData, err = os.ReadFile(filepath.Join(b.Path, cfg.Logo))
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	ext := strings.ToLower(filepath.Ext(cfg.Logo))
	switch ext {
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(logoData)
}

func escHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// maskDSN hides password in a connection string.
// postgres://user:secret@host:5432/db → postgres://user:***@host:5432/db
func maskDSN(dsn string) string {
	// URL format: postgres://user:pass@host/db
	if i := strings.Index(dsn, "://"); i >= 0 {
		rest := dsn[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 {
			userPart := rest[:at]
			if colon := strings.LastIndex(userPart, ":"); colon >= 0 {
				return dsn[:i+3+colon+1] + "***" + dsn[i+3+at:]
			}
		}
	}
	// DSN format: host=... password=secret ...
	if i := strings.Index(dsn, "password="); i >= 0 {
		end := i + len("password=")
		rest := dsn[end:]
		if sp := strings.IndexByte(rest, ' '); sp >= 0 {
			return dsn[:end] + "***" + rest[sp:]
		}
		return dsn[:end] + "***"
	}
	return dsn
}
