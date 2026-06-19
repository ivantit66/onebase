package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/llm"
	"github.com/ivantit66/onebase/internal/storage"
)

// starterLLMConfig — заготовка конфига для пустой базы: vision-распознавание на
// Gemini с фолбэком, остальные задачи — на GLM через z.ai. Ключи — плейсхолдеры.
func starterLLMConfig() llm.Config {
	return llm.Config{
		Enabled: true,
		Endpoints: []llm.Endpoint{
			{Name: "google", Kind: llm.KindGemini, APIKey: "ВАШ_КЛЮЧ_GEMINI"},
			{Name: "z_ai", Kind: llm.KindAnthropic, BaseURL: "https://api.z.ai/api/anthropic", APIKey: "ВАШ_КЛЮЧ_ZAI"},
		},
		Models: []llm.Model{
			{Name: "gemini-2.5-flash", Endpoint: "google", Vision: true},
			{Name: "gemini-2.0-flash", Endpoint: "google", Vision: true},
			{Name: "glm-4.6", Endpoint: "z_ai"},
		},
		Profiles: []llm.Profile{
			{Task: "документы", Models: []string{"gemini-2.5-flash", "gemini-2.0-flash"}},
			{Task: "анализ", Models: []string{"glm-4.6"}},
			{Task: "чат", Models: []string{"glm-4.6"}},
			{Task: "конфигуратор", Models: []string{"glm-4.6"}},
		},
		DefaultProfile: "анализ",
	}
}

// cfgAdminAI — страница «ИИ-помощник» в админ-меню конфигуратора. Конфиг
// (провайдеры, модели, профили задач) редактируется как JSON и хранится в
// _settings одним значением. Ключи показываются как есть — это экран
// администратора базы.
func (h *handler) cfgAdminAI(w http.ResponseWriter, r *http.Request) {
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
	cfg, err := db.GetLLMConfig(r.Context())
	if err != nil {
		w.Write([]byte(`<div style="padding:16px;color:#c00">Повреждённый конфиг ИИ: ` + html.EscapeString(err.Error()) + `</div>`))
		return
	}
	if len(cfg.Endpoints) == 0 && len(cfg.Models) == 0 {
		cfg = starterLLMConfig() // пустую базу заполняем заготовкой
	}
	pretty, _ := json.MarshalIndent(cfg.Redacted(), "", "  ")

	page := fmt.Sprintf(`<div style="padding:16px">
  <h3 style="margin:0 0 6px;font-size:15px">ИИ-помощник</h3>
  <div style="font-size:11px;color:#666;margin-bottom:10px">Провайдеры, модели и маршрутизация по задачам. Распознавание документов идёт на vision-моделях (Gemini) с фолбэком; текстовые задачи — на GLM через z.ai. Задачи: <code>анализ</code>, <code>чат</code>, <code>конфигуратор</code>, <code>документы</code>. Ключи хранятся в служебной таблице базы и не попадают в экспорт конфигурации. API-ключи отображаются замаскированными (<code>****</code>); оставьте маску без изменений — ключ сохранится прежним.</div>
  <textarea id="ai-cfg" spellcheck="false" style="width:100%%;height:340px;font-family:monospace;font-size:12px;padding:8px;border:1px solid #cbd5e1;border-radius:4px;resize:vertical">%s</textarea>
  <div style="margin-top:10px;display:flex;gap:8px;align-items:center;flex-wrap:wrap">
    <button onclick="aiSave()" style="background:#16a34a;color:#fff;border:none;padding:5px 14px;border-radius:3px;cursor:pointer;font-size:12px">Сохранить</button>
    <span style="font-size:11px;color:#666">Проверить задачу:</span>
    <input id="ai-task" value="анализ" style="width:120px;padding:3px 6px;border:1px solid #cbd5e1;border-radius:3px;font-size:12px">
    <button onclick="aiTest()" style="background:#2563eb;color:#fff;border:none;padding:5px 14px;border-radius:3px;cursor:pointer;font-size:12px">Проверить</button>
    <span id="ai-msg" style="font-size:11px"></span>
  </div>
  <pre id="ai-test-out" style="margin-top:10px;background:#f8fafc;border:1px solid #e2e8f0;border-radius:4px;padding:8px;font-size:12px;white-space:pre-wrap;display:none"></pre>
</div>
<script>
function aiCfgText(){return document.getElementById('ai-cfg').value;}
function aiSave(){
  var m=document.getElementById('ai-msg');
  var cfg;
  try{cfg=JSON.parse(aiCfgText());}catch(e){m.textContent='Некорректный JSON: '+e.message;m.style.color='#c00';return;}
  fetch('/bases/%s/configurator/admin/ai/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(cfg)})
    .then(function(r){return r.json()})
    .then(function(d){if(d.ok){m.textContent='Сохранено';m.style.color='#16a34a';if(typeof window.cfgAiRefresh==='function')window.cfgAiRefresh();}else{m.textContent=(d.error||'Ошибка');m.style.color='#c00';}})
    .catch(function(){m.textContent='Ошибка сети';m.style.color='#c00';});
}
function aiTest(){
  var m=document.getElementById('ai-msg');var out=document.getElementById('ai-test-out');
  var cfg;
  try{cfg=JSON.parse(aiCfgText());}catch(e){m.textContent='Некорректный JSON: '+e.message;m.style.color='#c00';return;}
  m.textContent='Запрос...';m.style.color='#666';out.style.display='none';
  fetch('/bases/%s/configurator/admin/ai/test',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({config:cfg,task:document.getElementById('ai-task').value})})
    .then(function(r){return r.json()})
    .then(function(d){
      if(d.ok){m.textContent='Ответила модель: '+d.model;m.style.color='#16a34a';out.textContent=d.text;out.style.display='block';}
      else{m.textContent='Ошибка';m.style.color='#c00';out.textContent=d.error||'';out.style.display='block';}
    })
    .catch(function(){m.textContent='Ошибка сети';m.style.color='#c00';});
}
</script>`, html.EscapeString(string(pretty)), b.ID, b.ID)

	// Режим доступа ИИ-чата к данным (план 54). Управляет тем, кто и как
	// обращается к данным базы через инструмент «выполнить_запрос».
	scopeSection := fmt.Sprintf(`<div style="padding:0 16px 16px;border-top:1px solid #e2e8f0;margin-top:4px;padding-top:14px">
  <h4 style="margin:0 0 6px;font-size:13px">Доступ ИИ-чата к данным</h4>
  <div style="font-size:11px;color:#666;margin-bottom:8px">Кто и как обращается к данным базы через инструмент «выполнить_запрос». <b>admin_only</b> — только администраторы (по умолчанию); <b>rbac</b> — пользователи с флагом «Доступ ИИ-чата к данным», но источники запроса фильтруются по их правам чтения; <b>all</b> — флаг даёт доступ ко всем данным без проверки прав (осознанно).</div>
  <select id="ai-scope" style="padding:4px 8px;border:1px solid #cbd5e1;border-radius:3px;font-size:12px">%s</select>
  <button onclick="aiScopeSave()" style="background:#16a34a;color:#fff;border:none;padding:5px 14px;border-radius:3px;cursor:pointer;font-size:12px;margin-left:6px">Сохранить режим</button>
  <span id="ai-scope-msg" style="font-size:11px;margin-left:6px"></span>
</div>
<script>
function aiScopeSave(){
  var m=document.getElementById('ai-scope-msg');m.textContent='';
  fetch('/bases/%s/configurator/admin/ai/datascope',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({scope:document.getElementById('ai-scope').value})})
    .then(function(r){return r.json()})
    .then(function(d){if(d.ok){m.textContent='Сохранено';m.style.color='#16a34a';}else{m.textContent=(d.error||'Ошибка');m.style.color='#c00';}})
    .catch(function(){m.textContent='Ошибка сети';m.style.color='#c00';});
}
</script>`, aiScopeOptions(db.GetAIDataScope(r.Context())), b.ID)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(page + scopeSection))
}

// aiScopeOptions строит <option> режима доступа ИИ к данным с выбранным текущим.
func aiScopeOptions(cur string) string {
	opts := []struct{ val, label string }{
		{storage.AIDataScopeAdminOnly, "admin_only — только администраторы"},
		{storage.AIDataScopeRBAC, "rbac — по флагу, с проверкой прав на объекты"},
		{storage.AIDataScopeAll, "all — по флагу, доступ ко всем данным"},
	}
	var sb strings.Builder
	for _, o := range opts {
		sel := ""
		if o.val == cur {
			sel = " selected"
		}
		sb.WriteString(fmt.Sprintf(`<option value="%s"%s>%s</option>`, o.val, sel, html.EscapeString(o.label)))
	}
	return sb.String()
}

// cfgAdminAIDataScope сохраняет режим доступа ИИ-чата к данным (admin_only|rbac|all).
func (h *handler) cfgAdminAIDataScope(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var req struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": "Некорректный JSON: " + err.Error()})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if err := db.SaveAIDataScope(r.Context(), req.Scope); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// cfgAdminAISave валидирует и сохраняет конфиг ИИ-помощника.
func (h *handler) cfgAdminAISave(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	var cfg llm.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, 400, map[string]any{"error": "Некорректный JSON: " + err.Error()})
		return
	}
	db, err := getAuthDB(r.Context(), b)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	// Объединяем реальные ключи под масками с ранее сохранёнными. Если текущий
	// конфиг прочитать нельзя (повреждённый JSON) — НЕ сохраняем, иначе под масками
	// `****` затёрлись бы реальные ключи.
	prev, err := db.GetLLMConfig(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "не удалось прочитать текущий конфиг (ключи не объединены): " + err.Error()})
		return
	}
	cfg = cfg.UnmaskKeys(prev)
	if err := db.SaveLLMConfig(r.Context(), cfg); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// cfgAdminAITest выполняет пробный запрос по переданному (ещё не сохранённому)
// конфигу — чтобы администратор проверил ключи и маршрут до сохранения.
func (h *handler) cfgAdminAITest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Config llm.Config `json:"config"`
		Task   string     `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": "Некорректный JSON: " + err.Error()})
		return
	}
	if req.Task == "" {
		req.Task = llm.TaskAnalysis
	}
	// Восстанавливаем реальные ключи из сохранённого конфига, если форма вернула
	// маскированные значения (****). При ошибке загрузки базы — тест идёт с тем,
	// что прислал браузер (позволяет тестировать совершенно новый, ещё не сохранённый конфиг).
	if b, err := h.store.Get(chi.URLParam(r, "id")); err == nil {
		if db, err := getAuthDB(r.Context(), b); err == nil {
			if prev, err := db.GetLLMConfig(r.Context()); err == nil {
				req.Config = req.Config.UnmaskKeys(prev)
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	runner := llm.New(req.Config, nil)
	resp, err := runner.Run(ctx, req.Task, llm.ChatRequest{
		Messages:  []llm.Message{llm.UserText("Ответь одним коротким предложением: соединение работает?")},
		MaxTokens: 64,
	})
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": llm.SafeErr(err)})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "text": resp.Text, "model": resp.Model})
}
