package ui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/ivantit66/onebase/internal/storage"
)

var tmpl = template.Must(template.New("root").Funcs(template.FuncMap{
	"lower": strings.ToLower,
	"str":   func(v any) string { if v == nil { return "" }; return fmt.Sprintf("%v", v) },
	"add":   func(a, b int) int { return a + b },
	// refID extracts UUID from a *Ref (implements GetRefUUID), otherwise returns fmt.Sprintf.
	// Used in TP row templates so the "selected" comparison works after enrichTPRowsWithRefs.
	"refID": func(v any) string {
		if v == nil {
			return ""
		}
		type uuidGetter interface{ GetRefUUID() string }
		if rp, ok := v.(uuidGetter); ok {
			return rp.GetRefUUID()
		}
		return fmt.Sprintf("%v", v)
	},
	"isRef":  func(t any) bool { return strings.HasPrefix(fmt.Sprintf("%v", t), "reference:") },
	"isEnum": func(t any) bool { return strings.HasPrefix(fmt.Sprintf("%v", t), "enum:") },
	// navLabel вставляет zero-width space на границах слов PascalCase-имени
	// (перед заглавной после строчной), чтобы длинные имена объектов
	// переносились по словам в боковой панели, а не обрезались.
	"navLabel": func(s string) string {
		const zwsp = '​' // zero-width space — невидимая точка переноса
		var b strings.Builder
		var prev rune
		for i, r := range s {
			if i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(prev) {
				b.WriteRune(zwsp)
			}
			b.WriteRune(r)
			prev = r
		}
		return b.String()
	},
	"fmtDate": func(v any) string {
		fmtT := func(t time.Time) string {
			lt := t.In(time.Local)
			h, m, sec := lt.Clock()
			if h != 0 || m != 0 || sec != 0 {
				return lt.Format("02.01.2006 15:04:05")
			}
			return lt.Format("02.01.2006")
		}
		if t, ok := v.(time.Time); ok {
			return fmtT(t)
		}
		if s, ok := v.(string); ok && len(s) >= 10 {
			// Strip Go monotonic clock suffix " m=+..."
			if i := strings.Index(s, " m=+"); i >= 0 {
				s = s[:i]
			}
			for _, layout := range []string{
				time.RFC3339, time.RFC3339Nano,
				"2006-01-02 15:04:05 -0700 MST",
				"2006-01-02 15:04:05.999999999 -0700 MST",
				"2006-01-02T15:04:05", "2006-01-02 15:04:05",
				"2006-01-02T15:04", "2006-01-02",
			} {
				if t, err := time.Parse(layout, s); err == nil {
					return fmtT(t)
				}
			}
			if len(s) >= 10 {
				if t, err := time.ParseInLocation("2006-01-02", s[:10], time.Local); err == nil {
					return fmtT(t)
				}
			}
		}
		return fmt.Sprintf("%v", v)
	},
	"filterVal": func(params storage.ListParams, fieldName string) storage.FilterValue {
		return filterValue(params, fieldName)
	},
	"sortDir": func(params storage.ListParams, fieldName string) string {
		if params.Sort == fieldName {
			if strings.ToLower(params.Dir) == "desc" {
				return "desc"
			}
			return "asc"
		}
		return ""
	},
	"sortIcon": func(params storage.ListParams, fieldName string) string {
		if params.Sort != fieldName {
			return "⇅"
		}
		if strings.ToLower(params.Dir) == "desc" {
			return "↓"
		}
		return "↑"
	},
	"nextDir": func(params storage.ListParams, fieldName string) string {
		if params.Sort == fieldName && strings.ToLower(params.Dir) != "desc" {
			return "desc"
		}
		return "asc"
	},
	"hasFilter": func(params storage.ListParams) bool {
		return len(params.Filters) > 0
	},
	"filterQuery": func(params storage.ListParams) string {
		var parts []string
		for k, v := range params.Filters {
			if v.From != "" {
				parts = append(parts, "f."+k+".from="+v.From)
			}
			if v.To != "" {
				parts = append(parts, "f."+k+".to="+v.To)
			}
			if v.Value != "" {
				parts = append(parts, "f."+k+"="+v.Value)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return "&" + strings.Join(parts, "&")
	},
	"reportParamQuery": func(params any, values map[string]any) string {
		type param interface{ GetName() string }
		// Use reflection-free approach: just iterate over values map
		parts := []string{}
		if values != nil {
			for k, v := range values {
				if v != nil && fmt.Sprintf("%v", v) != "" {
					parts = append(parts, k+"="+url.QueryEscape(fmt.Sprintf("%v", v)))
				}
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return "?" + strings.Join(parts, "&")
	},
	"mul": func(a, b int) int { return a * b },
	"int": func(v any) int {
		switch t := v.(type) {
		case int:
			return t
		case int64:
			return int(t)
		case float64:
			return int(t)
		}
		return 0
	},
	"seq": func(n int) []int {
		s := make([]int, n)
		for i := range s {
			s[i] = i
		}
		return s
	},
	"rowIdx": func(row map[string]any) int {
		if v, ok := row["строка"]; ok {
			switch t := v.(type) {
			case int:
				return t
			case int32:
				return int(t)
			case int64:
				return int(t)
			}
		}
		return 0
	},
	"jsJSON": func(v any) template.JS {
		b, err := json.Marshal(v)
		if err != nil {
			return template.JS("null")
		}
		return template.JS(b)
	},
	"wcell":        widgetCell,
	"echartsJSON":  echartsJSON,
	"splitCamel":   splitCamel,
	"fmtCell":      fmtReportCell,
}).Parse(tplHead + tplNav + tplIndex + tplList + tplForm + tplRegister + tplReport + tplProcessor + tplAbout + tplDeleteMarked + tplInfoReg + tplConstants + tplHistory + tplJournal + tplScheduled + tplAccountReg + tplQueryBuilder + tplAllFunctions + tplQueryConsole + tplCodeConsole + tplForbidden))

const tplHead = `
{{define "head"}}<!DOCTYPE html>
<html lang="ru"><head><meta charset="UTF-8">
<title>{{if .Cfg.AppName}}{{.Cfg.AppName}}{{else}}onebase{{end}}</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:system-ui,sans-serif;display:flex;flex-direction:column;min-height:100vh;background:#f5f5f5}
.topbar{background:#1e293b;color:#fff;padding:0 16px;display:flex;align-items:center;height:38px;flex-shrink:0;position:sticky;top:0;z-index:100}
.topbar-title{font-size:14px;font-weight:600;color:#7dd3fc;flex:1}
.sys-menu{position:relative}
.sys-btn{background:none;border:none;color:#cbd5e1;cursor:pointer;font-size:15px;padding:6px 10px;border-radius:5px;line-height:1}
.sys-btn:hover{background:#334155;color:#fff}
.sys-drop{display:none;position:absolute;right:0;top:calc(100% + 4px);background:#fff;border-radius:8px;box-shadow:0 4px 16px rgba(0,0,0,.18);min-width:170px;padding:4px 0;z-index:200}
.sys-drop.open{display:block}
.sys-drop>a,.sys-drop>button,.sys-drop>.sys-sub>a{display:block;padding:10px 16px;color:#334155;text-decoration:none;font-size:14px;width:100%;text-align:left;background:none;border:none;cursor:pointer;border-bottom:1px solid #f1f5f9}
.sys-drop>:last-child>a,.sys-drop>a:last-child,.sys-drop>button:last-child{border-bottom:none}
.sys-drop>a:hover,.sys-drop>button:hover,.sys-drop>.sys-sub:hover>a{background:#f1f5f9}
.sys-sub{position:relative}
.sys-sub>.sys-submenu{display:none;position:absolute;right:100%;top:-4px;background:#fff;border-radius:8px;box-shadow:0 4px 16px rgba(0,0,0,.18);min-width:220px;padding:4px 0;z-index:200}
.sys-sub:hover>.sys-submenu{display:block}
.sys-submenu a{display:block;padding:10px 16px;color:#334155;text-decoration:none;font-size:14px;border-bottom:1px solid #f1f5f9;white-space:nowrap}
.sys-submenu a:last-child{border-bottom:none}
.sys-submenu a:hover{background:#f1f5f9}
.tbl{width:100%;border-collapse:collapse}
.tbl th{text-align:left;padding:8px 10px;border-bottom:2px solid #e2e8f0;color:#64748b;font-weight:600;font-size:12px;position:sticky;top:0;background:#fff}
.tbl td{padding:6px 10px;border-bottom:1px solid #f1f5f9;color:#334155;font-size:13px}
.tbl tr:hover td{background:#f8fafc}
.app-body{display:flex;flex:1;overflow:hidden}
aside{width:210px;background:#1e293b;color:#fff;padding:16px 0;flex-shrink:0;overflow-y:auto}
aside .sec{font-size:11px;text-transform:uppercase;color:#94a3b8;margin:14px 12px 4px;letter-spacing:.05em}
aside a{display:block;padding:6px 14px;color:#cbd5e1;text-decoration:none;font-size:14px;margin:1px 6px;border-radius:5px;line-height:1.3;overflow-wrap:break-word}
aside a:hover{background:#334155;color:#fff}
main{flex:1;padding:28px;overflow-y:auto}
h2{font-size:22px;font-weight:600;margin-bottom:20px;color:#1e293b}
h3{font-size:16px;font-weight:600;margin:24px 0 10px;color:#1e293b}
.card{background:#fff;border-radius:10px;padding:24px;box-shadow:0 1px 3px rgba(0,0,0,.1);max-width:900px}
.main-list .card,.main-list .row-top,.main-list details,.main-list .breadcrumb{max-width:1400px}
table{width:100%;border-collapse:collapse;font-size:14px}
th{text-align:left;padding:10px 12px;border-bottom:2px solid #e2e8f0;color:#64748b;font-weight:600}
th a{color:#64748b;text-decoration:none}
th a:hover{color:#1e293b}
td{padding:10px 12px;border-bottom:1px solid #f1f5f9;color:#334155;font-size:14px}
tr:last-child td{border-bottom:none}
tr:hover td{background:#f8fafc}
.btn{display:inline-block;padding:8px 18px;border-radius:7px;font-size:14px;font-weight:500;text-decoration:none;cursor:pointer;border:none;line-height:1}
.btn-primary{background:#3b82f6;color:#fff}.btn-primary:hover{background:#2563eb}
.btn-post{background:#e8b400;color:#1a1a1a;font-weight:700}.btn-post:hover{background:#d4a200}
.btn-secondary{background:#e2e8f0;color:#374151}.btn-secondary:hover{background:#cbd5e1}
.btn-cancel{background:transparent;color:#64748b;border:1px solid #e2e8f0}.btn-cancel:hover{background:#f1f5f9}
.btn-sm{padding:5px 12px;font-size:13px}
.btn-danger{background:#ef4444;color:#fff}.btn-danger:hover{background:#dc2626}
.form-group{margin-bottom:16px}
label{display:block;font-size:13px;font-weight:500;margin-bottom:5px;color:#475569}
input[type=text],input[type=datetime-local],input[type=date],input[type=number],select{width:100%;padding:9px 12px;border:1px solid #e2e8f0;border-radius:7px;font-size:14px;outline:none;background:#fff}
input:focus,select:focus{border-color:#3b82f6;box-shadow:0 0 0 3px rgba(59,130,246,.15)}
.error{background:#fef2f2;border:1px solid #fecaca;color:#dc2626;padding:12px 16px;border-radius:7px;margin-bottom:16px;font-size:14px}
.empty{color:#94a3b8;text-align:center;padding:48px;font-size:15px}
.row-top{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;max-width:900px}
details{margin-bottom:16px;max-width:900px;background:#fff;border-radius:10px;box-shadow:0 1px 3px rgba(0,0,0,.1)}
details summary{padding:12px 20px;font-weight:600;font-size:14px;cursor:pointer;color:#475569;list-style:none}
details summary::-webkit-details-marker{display:none}
details summary::before{content:"▶ ";font-size:11px}
details[open] summary::before{content:"▼ "}
.filter-body{padding:0 20px 16px;display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px}
.filter-body label{font-size:12px;color:#64748b;margin-bottom:3px}
.filter-body input,.filter-body select{padding:7px 10px;font-size:13px}
.filter-actions{padding:0 20px 16px;display:flex;gap:10px}
.tp-table{width:100%;border-collapse:collapse;font-size:13px;margin-bottom:8px}
.tp-table th{background:#f1f5f9;padding:7px 10px;text-align:left;font-size:12px;color:#64748b}
.tp-table td{padding:4px 6px;border-bottom:1px solid #f1f5f9}
.tp-table input,.tp-table select{padding:5px 8px;font-size:13px;border:1px solid #e2e8f0;border-radius:5px;width:100%}
.tp-table .del-btn{background:none;border:none;color:#ef4444;cursor:pointer;font-size:16px;padding:0 4px}
.subsys-bar{background:#0f172a;display:flex;padding:0 12px;gap:2px;flex-shrink:0}
.subsys-bar a{display:inline-block;padding:7px 18px;color:#94a3b8;text-decoration:none;font-size:13px;font-weight:500;border-bottom:3px solid transparent;transition:color .15s}
.subsys-bar a:hover{color:#e2e8f0;background:rgba(255,255,255,.04)}
.subsys-bar a.active{color:#7dd3fc;border-bottom-color:#3b82f6}
.breadcrumb{display:flex;align-items:center;gap:6px;font-size:13px;color:#64748b;margin-bottom:12px;max-width:900px;flex-wrap:wrap}
.breadcrumb a{color:#3b82f6;text-decoration:none}.breadcrumb a:hover{text-decoration:underline}
.breadcrumb span{color:#94a3b8;padding:0 2px}
/* Чтобы контент не накрывало панелью сообщений */
body{padding-bottom:32px}
/* Панель сообщений (как «Окно сообщений» в 1С) */
#ob-msg-bar{position:fixed;left:0;right:0;bottom:0;z-index:300;background:#fff;border-top:1px solid #cbd5e1;box-shadow:0 -2px 8px rgba(0,0,0,.08);font-family:system-ui,sans-serif;font-size:13px;color:#1e293b;transform:translateY(calc(100% - 30px));transition:transform .18s ease}
#ob-msg-bar.open{transform:translateY(0)}
#ob-msg-bar.hidden{display:none}
#ob-msg-head{height:30px;display:flex;align-items:center;padding:0 10px;cursor:pointer;background:#f1f5f9;user-select:none;gap:10px}
#ob-msg-head .ttl{font-weight:600;color:#334155;flex:1;display:flex;align-items:center;gap:8px}
#ob-msg-head .cnt{background:#ef4444;color:#fff;border-radius:10px;padding:1px 8px;font-size:11px;font-weight:700;min-width:18px;text-align:center;display:none}
#ob-msg-head .cnt.show{display:inline-block}
#ob-msg-head .arr{color:#64748b;font-size:11px;width:14px;text-align:center}
#ob-msg-bar.open #ob-msg-head .arr{transform:rotate(180deg)}
#ob-msg-head button{background:none;border:none;color:#64748b;cursor:pointer;font-size:12px;padding:4px 8px;border-radius:5px}
#ob-msg-head button:hover{background:#e2e8f0;color:#1e293b}
#ob-msg-list{max-height:200px;overflow-y:auto;padding:6px 0;background:#fff}
#ob-msg-list .it{padding:5px 14px;border-bottom:1px solid #f1f5f9;display:flex;gap:10px;align-items:flex-start;font-family:Consolas,monospace;font-size:12px;white-space:pre-wrap;word-break:break-word}
#ob-msg-list .it:last-child{border-bottom:none}
#ob-msg-list .it .t{color:#94a3b8;flex-shrink:0;font-size:11px;padding-top:1px}
#ob-msg-list .empty{padding:10px 14px;color:#94a3b8;font-style:italic}
</style>
<script>
(function(){
  if(window.__obMsgInit)return;window.__obMsgInit=true;
  function init(){
    if(document.getElementById('ob-msg-bar'))return;
    var bar=document.createElement('div');bar.id='ob-msg-bar';bar.className='hidden';
    bar.innerHTML='<div id="ob-msg-head"><span class="ttl">Сообщения <span class="cnt" id="ob-msg-cnt">0</span></span><button type="button" id="ob-msg-clear" title="Очистить">Очистить</button><span class="arr">▲</span></div><div id="ob-msg-list"><div class="empty">Сообщений нет</div></div>';
    document.body.appendChild(bar);
    var list=document.getElementById('ob-msg-list'),cnt=document.getElementById('ob-msg-cnt'),head=document.getElementById('ob-msg-head'),btnClear=document.getElementById('ob-msg-clear');
    var prevSig=sessionStorage.getItem('obMsgSig')||'',prevOpen=sessionStorage.getItem('obMsgOpen')==='1',lastHtml='';
    function fmtTime(ts){try{var d=new Date(ts);var h=String(d.getHours()).padStart(2,'0'),m=String(d.getMinutes()).padStart(2,'0'),s=String(d.getSeconds()).padStart(2,'0');return h+':'+m+':'+s;}catch(e){return '';}}
    function escapeHtml(s){return String(s).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];});}
    function render(msgs){
      if(!msgs||!msgs.length){bar.classList.add('hidden');bar.classList.remove('open');list.innerHTML='<div class="empty">Сообщений нет</div>';lastHtml='';cnt.classList.remove('show');prevSig='';sessionStorage.removeItem('obMsgSig');return;}
      bar.classList.remove('hidden');
      var html='';for(var i=0;i<msgs.length;i++){var m=msgs[i];html+='<div class="it"><span class="t">'+fmtTime(m.time)+'</span><span>'+escapeHtml(m.text)+'</span></div>';}
      if(html!==lastHtml){
        // не перерисовывать пока пользователь выделяет текст внутри панели — иначе сбрасывается выделение
        var sel=window.getSelection?window.getSelection():null;
        if(!(sel&&!sel.isCollapsed&&sel.anchorNode&&list.contains(sel.anchorNode))){
          list.innerHTML=html;lastHtml=html;list.scrollTop=list.scrollHeight;
        }
      }
      cnt.textContent=msgs.length;cnt.classList.add('show');
      var sig=msgs.length?msgs[msgs.length-1].time+'|'+msgs.length:'';
      if(sig!==prevSig){bar.classList.add('open');prevOpen=true;sessionStorage.setItem('obMsgOpen','1');}
      else if(prevOpen){bar.classList.add('open');}
      prevSig=sig;sessionStorage.setItem('obMsgSig',sig);
    }
    head.addEventListener('click',function(e){if(e.target===btnClear)return;bar.classList.toggle('open');prevOpen=bar.classList.contains('open');sessionStorage.setItem('obMsgOpen',prevOpen?'1':'0');});
    btnClear.addEventListener('click',function(e){e.stopPropagation();fetch('/ui/messages/clear',{method:'POST'}).then(function(){render([]);});});
    function load(){fetch('/ui/messages').then(function(r){return r.json();}).then(function(d){render(d.messages||[]);}).catch(function(){});}
    load();setInterval(load,3000);
    document.addEventListener('submit',function(){setTimeout(load,400);},true);
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',init);else init();
})();
</script>
</head><body>
{{end}}
`

const tplNav = `
{{define "nav"}}
<header class="topbar">
  <span class="topbar-title">{{if .Cfg.Logo}}<img src="/ui/logo" alt="" style="height:22px;max-width:90px;vertical-align:middle;margin-right:6px;border-radius:2px">{{end}}⚡ {{if .Cfg.AppName}}{{.Cfg.AppName}}{{else}}onebase{{end}}</span>
  <div class="sys-menu">
    <button class="sys-btn" onclick="var d=document.getElementById('sysd');d.classList.toggle('open')">&#9881; Система &#9660;</button>
    <div class="sys-drop" id="sysd">
      <a href="/ui/about">О программе</a>
      <a href="/ui/admin/users">Пользователи</a>
      <a href="/ui/admin/roles">Роли и права</a>
      <a href="/ui/admin/sessions">Активные пользователи</a>
      <a href="/ui/admin/audit">Журнал изменений</a>
      <a href="/ui/admin/scheduled">Регламентные задания</a>
      <a href="/ui/delete-marked">Удалить помеченные</a>
      <a href="/ui/admin/cleanup">Очистка регистров</a>
      {{if .IsAdmin}}<a href="/ui/all-functions">Все функции</a>{{end}}
      {{if .IsAdmin}}<div class="sys-sub"><a href="#" onclick="event.preventDefault()">Инструменты разработчика &#9654;</a>
      <div class="sys-submenu">
        <a href="/ui/dev/query-console">Консоль запросов</a>
        <a href="/ui/dev/code-console">Консоль кода</a>
      </div>
    </div>{{end}}
      {{if .HasAuth}}{{if not .DenyPasswdChange}}<a href="/ui/profile/passwd">Сменить пароль</a>{{end}}{{end}}
      <form method="POST" action="/logout" style="margin:0;padding:0"><button type="submit" style="display:block;width:100%;padding:10px 16px;color:#dc2626;text-decoration:none;font-size:14px;text-align:left;background:none;border:none;border-top:1px solid #f1f5f9;cursor:pointer">Выйти</button></form>
    </div>
  </div>
</header>
{{if .Cfg.DemoMode}}
<div style="background:#f59e0b;color:#fff;text-align:center;padding:6px 16px;font-size:13px;font-weight:600;letter-spacing:.02em">
  ⚠️ ДЕМО-РЕЖИМ{{if .Cfg.DemoMessage}} — {{.Cfg.DemoMessage}}{{end}}
</div>
{{end}}
{{if .Subsystems}}
<nav class="subsys-bar">
  {{range .Subsystems}}<a href="/ui/?subsystem={{.Name}}" class="{{if eq .Name $.CurrentSubsystem}}active{{end}}">{{.Title}}</a>{{end}}
</nav>
{{end}}
<div class="app-body">
<aside>
  <a href="/ui{{if .CurrentSubsystem}}/?subsystem={{.CurrentSubsystem}}{{end}}" style="display:block;padding:12px 14px 8px;color:#7dd3fc;font-weight:700;font-size:15px;text-decoration:none">Главная</a>
  {{range .Nav}}
  <div class="sec">{{.Kind}}</div>
  {{range .Items}}<a href="{{.URL}}" title="{{.Label}}">{{navLabel .Label}}</a>
  {{end}}{{end}}
</aside>
{{end}}
`

const tplIndex = `
{{define "page-index"}}
{{template "head" .}}{{template "nav" .}}
<style>
.dash{display:flex;flex-direction:column;gap:14px;max-width:1280px}
.dash-row{display:flex;gap:14px;flex-wrap:wrap}
.dash-row > *{flex:1 1 220px;min-width:0}
/* Виджеты с табличным/широким содержимым шире компактных KPI */
.dash-row > .w-card-list,.dash-row > .w-card-chart,.dash-row > .w-card-recent{flex:1 1 360px}
.dash-grid{display:grid;grid-template-columns:repeat(12,1fr);gap:14px}
.w-card{background:#fff;border-radius:10px;padding:18px 20px;box-shadow:0 1px 3px rgba(0,0,0,.08);display:flex;flex-direction:column;min-height:120px}
.w-title{font-size:12px;text-transform:uppercase;letter-spacing:.05em;color:#64748b;font-weight:600;margin-bottom:8px}
.w-kpi-value{font-size:32px;font-weight:700;color:#0f172a;line-height:1.1;white-space:nowrap}
.w-kpi-sub{font-size:12px;color:#94a3b8;margin-top:6px}
.w-list{overflow-x:auto}
.w-list table{margin-top:4px;font-size:13px}
.w-list th{padding:6px 8px;font-size:11px;color:#64748b;border-bottom:1px solid #e2e8f0;text-align:left;background:transparent}
.w-list td{padding:6px 8px;border-bottom:1px solid #f1f5f9;font-size:13px;color:#334155}
.w-list td.right{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}
.w-list tr:last-child td{border-bottom:none}
.w-chart{min-height:240px}
.w-chart-canvas{width:100%;height:240px}
.w-actions-row{display:flex;flex-wrap:wrap;gap:8px;margin-top:4px}
.w-actions-row a{display:inline-block;padding:7px 14px;border-radius:7px;background:#3b82f6;color:#fff;text-decoration:none;font-size:13px;font-weight:500}
.w-actions-row a:hover{background:#2563eb}
.w-recent-row{display:flex;align-items:center;gap:10px;padding:6px 0;border-bottom:1px solid #f1f5f9;font-size:13px}
.w-recent-row:last-child{border-bottom:none}
.w-recent-row .e{color:#64748b;font-size:11px;text-transform:uppercase;letter-spacing:.04em;flex-shrink:0;min-width:140px;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.w-recent-row a{color:#3b82f6;text-decoration:none;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.w-recent-row a:hover{text-decoration:underline}
.w-recent-row .ts{color:#94a3b8;font-size:11px;flex-shrink:0;font-variant-numeric:tabular-nums}
.w-error{background:#fef2f2;color:#dc2626;font-size:12px;padding:6px 10px;border-radius:6px;margin-top:6px;font-family:Consolas,monospace}
.w-empty{color:#94a3b8;font-size:13px;padding:6px 0;font-style:italic}
.w-default-hint{color:#64748b;font-size:13px;margin:6px 0 14px}
</style>
<main>
  <h2 style="margin-bottom:14px">{{.HomeTitle}}</h2>
  {{if .DefaultedHome}}<div class="w-default-hint">Стартовая страница не настроена — показаны последние документы из аудита. Создайте <code>config/home_page.yaml</code> и виджеты в <code>widgets/</code>, чтобы оформить дашборд.</div>{{end}}
  <div class="dash">
    <div class="dash-row">
      {{range .WidgetResults}}{{template "widget-card" .}}{{end}}
    </div>
  </div>
</main></div>
<script>
window.__obWidgetCharts = window.__obWidgetCharts || {};
{{range .WidgetResults}}{{if and (eq .Type "chart") .Chart}}window.__obWidgetCharts["{{.Name}}"] = {{echartsJSON .Chart}};
{{end}}{{end}}
</script>
<script src="https://cdn.jsdelivr.net/npm/echarts@5.4.3/dist/echarts.min.js"></script>
<script>
(function(){
  function initCharts(){
    if(!window.echarts)return;
    var nodes=document.querySelectorAll('.w-chart-canvas[data-widget]');
    for(var i=0;i<nodes.length;i++){
      var node=nodes[i];
      var name=node.getAttribute('data-widget');
      var opt=window.__obWidgetCharts[name];
      if(!opt)continue;
      try{
        var c=echarts.init(node);
        if(opt.yAxis&&opt.yAxis.type==="value"){opt.yAxis.axisLabel={formatter:function(v){if(Math.abs(v)>=1e6)return(v/1e6).toFixed(1)+"M";if(Math.abs(v)>=1e3)return(v/1e3).toFixed(1)+"k";return v%1===0?v:v.toFixed(2)}};}
        c.setOption(opt);
        (function(c){window.addEventListener('resize',function(){c.resize();});})(c);
      }catch(e){console.error('chart init failed',e);}
    }
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',initCharts);else initCharts();
})();
</script>
</body></html>
{{end}}

{{define "widget-card"}}
<div class="w-card w-card-{{.Type}}">
  {{if .Title}}<div class="w-title">{{.Title}}</div>{{end}}
  {{if .Error}}<div class="w-error">{{.Error}}</div>
  {{else if eq .Type "kpi"}}{{template "widget-kpi-body" .}}
  {{else if eq .Type "list"}}{{template "widget-list-body" .}}
  {{else if eq .Type "chart"}}{{template "widget-chart-body" .}}
  {{else if eq .Type "actions"}}{{template "widget-actions-body" .}}
  {{else if eq .Type "recent"}}{{template "widget-recent-body" .}}
  {{end}}
</div>
{{end}}

{{define "widget-kpi-body"}}
  {{if .KPI}}<div class="w-kpi-value">{{.KPI.Display}}</div>{{else}}<div class="w-empty">нет данных</div>{{end}}
{{end}}

{{define "widget-list-body"}}
<div class="w-list">
  {{if .Rows}}
  <table>
    <thead><tr>{{range .Columns}}<th{{if eq .Align "right"}} style="text-align:right"{{end}}>{{.Label}}</th>{{end}}</tr></thead>
    <tbody>
    {{range .Rows}}
      {{$row := .}}
      <tr>
        {{range $.Columns}}
        <td{{if eq .Align "right"}} class="right"{{end}}>{{wcell $row .Field .Format}}</td>
        {{end}}
      </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<div class="w-empty">нет данных</div>{{end}}
</div>
{{end}}

{{define "widget-chart-body"}}
<div class="w-chart">
  {{if .Chart}}<div class="w-chart-canvas" data-widget="{{.Name}}"></div>
  {{else}}<div class="w-empty">нет данных</div>{{end}}
</div>
{{end}}

{{define "widget-actions-body"}}
<div class="w-actions-row">
  {{range .Actions}}<a href="{{.URL}}">{{.Label}}</a>{{else}}<div class="w-empty">нет действий</div>{{end}}
</div>
{{end}}

{{define "widget-recent-body"}}
{{if .Rows}}
  {{range .Rows}}
  {{$label := splitCamel (str (index . "entity_name"))}}
  <div class="w-recent-row">
    <span class="e" title="{{$label}}">{{$label}}</span>
    <a href="{{index . "_url"}}">{{index . "_title"}}</a>
    <span class="ts">{{fmtDate (index . "_ts")}}</span>
  </div>
  {{end}}
{{else}}<div class="w-empty">нет записей</div>{{end}}
{{end}}
`

const tplList = `
{{define "page-list"}}
{{template "head" .}}{{template "nav" .}}
<main class="main-list">
<div class="row-top">
  <h2>{{.Entity.DisplayName}}</h2>
  <div style="display:flex;gap:8px">
    {{if .Entity.Hierarchical}}
      {{if .TreeView}}
        <a class="btn btn-secondary btn-sm" href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}{{if $.CurrentSubsystem}}?subsystem={{$.CurrentSubsystem}}{{end}}">☰ Список</a>
      {{else}}
        <a class="btn btn-secondary btn-sm" href="?view=tree{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}">📂 Дерево</a>
      {{end}}
      {{if .UpURL}}<a class="btn btn-secondary btn-sm" href="{{.UpURL}}">↑ Наверх</a>{{end}}
      {{if .CanWrite}}
      <a class="btn btn-primary" href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}/new?{{if .ParentStr}}parent={{.ParentStr}}&{{end}}subsystem={{$.CurrentSubsystem}}">+ Элемент</a>
      <a class="btn btn-secondary" href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}/new?is_folder=true{{if .ParentStr}}&parent={{.ParentStr}}{{end}}">📁 Группа</a>
      {{end}}
    {{else}}
      {{if .CanWrite}}<a class="btn btn-primary" href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}/new{{if $.CurrentSubsystem}}?subsystem={{$.CurrentSubsystem}}{{end}}">+ Создать</a>{{end}}
    {{end}}
    <a class="btn btn-sm" href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}/excel{{filterQuery .Params}}" style="background:#16a34a;color:#fff" title="Скачать Excel">Excel ↓</a>
  </div>
</div>
<form method="GET" style="display:flex;gap:8px;margin-bottom:12px;max-width:460px">
  <input type="text" name="q" value="{{.Params.Search}}" placeholder="Поиск..." style="flex:1;padding:7px 12px;border:1px solid #e2e8f0;border-radius:6px;font-size:14px" oninput="clearTimeout(window._srch);window._srch=setTimeout(()=>this.form.submit(),320)">
  {{if .Params.Search}}<a class="btn btn-sm" href="?" style="background:#e2e8f0;color:#475569;align-self:center">✕</a>{{end}}
  {{if $.CurrentSubsystem}}<input type="hidden" name="subsystem" value="{{$.CurrentSubsystem}}">{{end}}
</form>
{{if .Breadcrumbs}}
<nav class="breadcrumb">
  <a href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}{{if $.CurrentSubsystem}}?subsystem={{$.CurrentSubsystem}}{{end}}">Корень</a>
  {{range .Breadcrumbs}}<span>›</span><a href="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}?parent={{.ID}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}">{{.Label}}</a>{{end}}
</nav>
{{end}}

{{$entity := .Entity}}{{$params := .Params}}{{$refOpts := .RefFilterOptions}}
<details{{if hasFilter $params}} open{{end}}>
  <summary>Отбор</summary>
  <form method="GET" action="">
  <div class="filter-body">
  {{range $entity.Fields}}{{$f := .}}
    {{if eq (str .Type) "date"}}
      <div>
        <label>{{.Name}} с</label>
        <input type="date" name="f.{{.Name}}.from" value="{{(filterVal $params .Name).From}}">
      </div>
      <div>
        <label>{{.Name}} по</label>
        <input type="date" name="f.{{.Name}}.to" value="{{(filterVal $params .Name).To}}">
      </div>
    {{else if isRef (str .Type)}}
      <div>
        <label>{{.Name}}</label>
        <select name="f.{{.Name}}">
          <option value="">— все —</option>
          {{range index $refOpts .Name}}
          <option value="{{index . "id"}}" {{if eq (index . "id") (filterVal $params $f.Name).Value}}selected{{end}}>{{index . "_label"}}</option>
          {{end}}
        </select>
      </div>
    {{else}}
      <div>
        <label>{{.Name}}</label>
        <input type="text" name="f.{{.Name}}" value="{{(filterVal $params .Name).Value}}">
      </div>
    {{end}}
  {{end}}
  </div>
  <div class="filter-actions">
    <button class="btn btn-primary btn-sm" type="submit">Применить</button>
    <a class="btn btn-sm" href="?" style="background:#e2e8f0;color:#475569">Сбросить</a>
  </div>
  {{if $params.Sort}}<input type="hidden" name="sort" value="{{$params.Sort}}"><input type="hidden" name="dir" value="{{$params.Dir}}">{{end}}
  {{if $.CurrentSubsystem}}<input type="hidden" name="subsystem" value="{{$.CurrentSubsystem}}">{{end}}
  </form>
</details>

<div class="card">
{{if .TreeView}}
{{/* ===== TREE VIEW ===== */}}
{{if .TreeRows}}
<div style="overflow-x:auto">
<table><thead><tr>
  {{range .Entity.Fields}}<th>{{.Name}}</th>{{end}}
  <th style="width:90px"></th>
</tr></thead><tbody>
{{range .TreeRows}}{{$row := .}}{{$isFolder := index $row "is_folder"}}{{$depth := index $row "_depth"}}
<tr {{if index $row "deletion_mark"}}style="opacity:0.45;text-decoration:line-through;cursor:pointer"{{else}}style="cursor:pointer"{{end}}
  onclick="listRowClick(event,this)"
  ondblclick="listRowDblClick(event,this)"
  oncontextmenu="listCtxMenu(event,this)"
  data-tree-id="{{index $row "id"}}"
  data-tree-parent="{{index $row "parent_id"}}"
  data-predefined="{{if index $row "_is_predefined"}}1{{end}}"
  data-is-folder="{{if $isFolder}}1{{end}}"
  data-folder-url="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}?parent={{index $row "id"}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}"
  data-mark-url="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}/{{index $row "id"}}/delete?mark=1"
  data-del-url="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}/{{index $row "id"}}/delete"
  data-open-url="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}/{{index $row "id"}}{{if $.CurrentSubsystem}}?subsystem={{$.CurrentSubsystem}}{{end}}">
  {{range $.Entity.Fields}}
    {{if eq .Name "Наименование"}}
      <td>
        <span style="display:inline-block;width:{{mul (int $depth) 20}}px"></span>
        {{if $isFolder}}
          <button type="button" class="tree-toggle" data-folder-id="{{index $row "id"}}" title="Свернуть/Развернуть"
            style="background:none;border:none;cursor:pointer;padding:0 2px;font-size:13px">▼</button>
          📁
        {{else}}📄{{end}}
        {{index $row .Name}}{{if index $row "_is_predefined"}} <span title="Предопределённый" style="color:#f59e0b;font-size:11px">★</span>{{end}}
      </td>
    {{else if eq (str .Type) "date"}}<td>{{fmtDate (index $row .Name)}}</td>
    {{else}}<td>{{fmtCell (index $row .Name)}}</td>{{end}}
  {{end}}
  <td>
    {{if $isFolder}}
      <a class="btn btn-sm btn-secondary" href="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}?parent={{index $row "id"}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}">▶ Войти</a>
    {{else}}
      <a class="btn btn-sm btn-primary" href="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}/{{index $row "id"}}{{if $.CurrentSubsystem}}?subsystem={{$.CurrentSubsystem}}{{end}}">Открыть</a>
    {{end}}
  </td>
</tr>{{end}}
</tbody></table>
</div>
{{else}}
<p class="empty">Записей нет — <a href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}/new">создать первую</a></p>
{{end}}
{{else}}
{{/* ===== LIST VIEW (default) ===== */}}
{{if .Rows}}
<div style="overflow-x:auto">
<table><thead><tr>
  {{if eq (str .Entity.Kind) "document"}}<th style="width:36px">✓</th>{{end}}
  {{range .Entity.Fields}}
  <th>
    <a href="?sort={{.Name}}&dir={{nextDir $params .Name}}{{filterQuery $params}}">
      {{.Name}} {{sortIcon $params .Name}}
    </a>
  </th>
  {{end}}
  <th style="width:90px"></th>
</tr></thead><tbody>
{{range .Rows}}{{$row := .}}{{$isFolder := index $row "is_folder"}}
<tr {{if index $row "deletion_mark"}}style="opacity:0.45;text-decoration:line-through;cursor:pointer"{{else}}style="cursor:pointer"{{end}}
  onclick="listRowClick(event,this)"
  ondblclick="listRowDblClick(event,this)"
  oncontextmenu="listCtxMenu(event,this)"
  data-predefined="{{if index $row "_is_predefined"}}1{{end}}"
  data-is-folder="{{if $isFolder}}1{{end}}"
  data-folder-url="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}?parent={{index $row "id"}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}"
  data-mark-url="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}/{{index $row "id"}}/delete?mark=1"
  data-del-url="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}/{{index $row "id"}}/delete"
  data-open-url="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}/{{index $row "id"}}{{if $.CurrentSubsystem}}?subsystem={{$.CurrentSubsystem}}{{end}}">
  {{if eq (str $.Entity.Kind) "document"}}
    <td style="text-align:center">
      {{if index $row "posted"}}<span style="color:#16a34a;font-weight:700" title="Проведён">✓</span>{{else}}<span style="color:#94a3b8" title="Не проведён">—</span>{{end}}
    </td>
  {{end}}
  {{range $.Entity.Fields}}
    {{if eq (str .Type) "date"}}<td style="white-space:nowrap">{{fmtDate (index $row .Name)}}</td>
    {{else}}<td style="white-space:nowrap">{{if and (eq .Name "Наименование") $.Entity.Hierarchical}}{{if $isFolder}}📁 {{else}}📄 {{end}}{{end}}{{fmtCell (index $row .Name)}}{{if and (eq .Name "Наименование") (index $row "_is_predefined")}} <span title="Предопределённый элемент" style="color:#f59e0b;font-size:11px">★</span>{{end}}</td>{{end}}
  {{end}}
  <td>
    {{if and $isFolder $.Entity.Hierarchical}}
      <a class="btn btn-sm btn-secondary" href="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}?parent={{index $row "id"}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}">▶ Войти</a>
    {{else}}
      <a class="btn btn-sm btn-primary" href="/ui/{{lower (str $.Entity.Kind)}}/{{lower $.Entity.Name}}/{{index $row "id"}}{{if $.CurrentSubsystem}}?subsystem={{$.CurrentSubsystem}}{{end}}">Открыть</a>
    {{end}}
  </td>
</tr>{{end}}
</tbody></table>
</div>
{{else}}
<p class="empty">{{if .Params.Search}}Ничего не найдено по запросу «{{.Params.Search}}» — <a href="?">сбросить поиск</a>{{else}}Записей нет — <a href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}/new">создать первую</a>{{end}}</p>
{{end}}
{{end}}
</div>
{{if gt .TotalPages 1}}
<div style="display:flex;align-items:center;gap:8px;margin-top:12px;flex-wrap:wrap">
  {{if .HasPrev}}<a class="btn btn-secondary btn-sm" href="?page={{.PrevPage}}{{if .Params.Search}}&q={{.Params.Search}}{{end}}{{filterQuery .Params}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}">← Назад</a>{{end}}
  <span style="color:#64748b;font-size:13px">Стр. {{.Page}} из {{.TotalPages}} ({{.Total}} записей)</span>
  {{if .HasNext}}<a class="btn btn-secondary btn-sm" href="?page={{.NextPage}}{{if .Params.Search}}&q={{.Params.Search}}{{end}}{{filterQuery .Params}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}">Вперёд →</a>{{end}}
</div>
{{else if gt .Total 0}}
<div style="color:#94a3b8;font-size:12px;margin-top:8px">Всего: {{.Total}}</div>
{{end}}
</main>
<script>
var _isAdmin={{if .IsAdmin}}true{{else}}false{{end}};
var _canDelete={{if .CanDelete}}true{{else}}false{{end}};
var _listSel=null;
function listRowClick(e,tr){
  if(e.target.closest('a,button'))return;
  if(_listSel)_listSel.querySelectorAll('td').forEach(function(td){td.style.background='';});
  _listSel=tr;
  tr.querySelectorAll('td').forEach(function(td){td.style.background='#dbeafe';});
}
function listRowDblClick(e,tr){
  if(e.target.closest('a,button'))return;
  if(tr.dataset.isFolder==='1'){window.location.href=tr.dataset.folderUrl;}
  else{window.location.href=tr.dataset.openUrl;}
}
// Tree view: collapse/expand subtrees
document.querySelectorAll('.tree-toggle').forEach(function(btn){
  btn.addEventListener('click',function(e){
    e.stopPropagation();
    var fid=btn.dataset.folderId;
    var expanded=btn.getAttribute('data-expanded')!=='0';
    treeSetVisible(fid,!expanded);
    btn.setAttribute('data-expanded',expanded?'0':'1');
    btn.textContent=expanded?'▶':'▼';
  });
});
function treeSetVisible(parentId,visible){
  document.querySelectorAll('[data-tree-parent="'+parentId+'"]').forEach(function(row){
    row.style.display=visible?'':'none';
    var childId=row.dataset.treeId;
    if(childId){treeSetVisible(childId,visible&&row.dataset.isFolder!=='1'||row.querySelector('.tree-toggle[data-expanded="1"]')!==null);}
  });
}
function listCtxMenu(e,tr){
  if(e.target.closest('a,button'))return;
  e.preventDefault();
  listRowClick(e,tr);
  var old=document.getElementById('_lctx');if(old)old.remove();
  var m=document.createElement('div');
  m.id='_lctx';
  m.style.cssText='position:fixed;z-index:999;background:#fff;border:1px solid #c8d0de;border-radius:6px;box-shadow:0 4px 16px rgba(0,0,0,.18);padding:4px 0;min-width:190px;font-size:13px';
  m.style.left=e.clientX+'px';m.style.top=e.clientY+'px';
  var isPredefined=tr.dataset.predefined==='1';
  var isFolder=tr.dataset.isFolder==='1';
  var items=[];
  if(isFolder){
    items.push({label:'▶ Войти в группу',fn:function(){window.location.href=tr.dataset.folderUrl;}});
    items.push({label:'Редактировать',fn:function(){window.location.href=tr.dataset.openUrl;}});
  } else {
    items.push({label:'Открыть',fn:function(){window.location.href=tr.dataset.openUrl;}});
  }
  if(_canDelete){
    if(!isPredefined)items.push({label:'Пометить на удаление',danger:true,fn:function(){listSubmit(tr.dataset.markUrl,'Пометить на удаление?');}});
    else items.push({label:'Предопределённый — нельзя удалить',disabled:true});
  }
  if(_isAdmin&&!isPredefined)items.push({label:'Удалить навсегда',danger:true,fn:function(){listSubmit(tr.dataset.delUrl,'Удалить запись навсегда?');}});
  items.forEach(function(item){
    var mi=document.createElement('div');
    mi.textContent=item.label;
    if(item.disabled){
      mi.style.cssText='padding:8px 14px;color:#94a3b8;cursor:default;font-style:italic';
    } else {
      mi.style.cssText='padding:8px 14px;cursor:pointer'+(item.danger?';color:#dc2626':'');
      mi.onmouseenter=function(){mi.style.background='#f8fafc';};
      mi.onmouseleave=function(){mi.style.background='';};
      mi.onclick=function(){m.remove();item.fn();};
    }
    m.appendChild(mi);
  });
  document.body.appendChild(m);
  setTimeout(function(){
    document.addEventListener('click',function h(){m.remove();document.removeEventListener('click',h);},{once:true});
  },0);
}
function listSubmit(url,msg){
  if(!url)return;
  if(confirm(msg)){var f=document.createElement('form');f.method='POST';f.action=url;document.body.appendChild(f);f.submit();}
}
document.addEventListener('keydown',function(e){
  if(e.key==='Delete'&&_listSel&&_canDelete)listSubmit(_listSel.dataset.markUrl,'Пометить на удаление?');
});
</script>
</div></body></html>
{{end}}
`

const tplForm = `
{{define "page-form"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;max-width:900px">
  <h2 style="margin-bottom:0">{{if .IsNew}}Создать{{else}}Редактировать{{end}} — {{.Entity.DisplayName}}</h2>
  <a href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}" title="Закрыть" style="font-size:22px;line-height:1;color:#94a3b8;text-decoration:none;padding:2px 8px;border-radius:5px;background:#f1f5f9;font-weight:300">×</a>
</div>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}

{{/* Top toolbar */}}
<div style="display:flex;align-items:center;gap:8px;margin-bottom:16px;flex-wrap:wrap">
  {{if .Entity.Posting}}
    {{if not .IsNew}}
      {{if eq (index .Values "posted") "true"}}
        <span style="color:#16a34a;font-weight:600;font-size:13px">✓ Проведён</span>
      {{else}}
        <span style="color:#94a3b8;font-size:13px">Не проведён</span>
      {{end}}
    {{end}}
  {{end}}
  {{if .CanWrite}}<button class="btn btn-secondary" type="submit" name="_action" value="" form="main-form">Записать</button>{{end}}
  {{if .Entity.Posting}}
    {{if $.CanPost}}<button class="btn btn-post" type="submit" name="_action" value="post_and_close" form="main-form">Провести и закрыть</button>{{end}}
    {{if not .IsNew}}
      {{if eq (index .Values "posted") "true"}}
        {{if $.CanPost}}<button class="btn btn-primary btn-sm" type="submit" name="_action" value="post" form="main-form">Перепровести</button>{{end}}
        {{if $.CanUnpost}}<button class="btn btn-sm" style="background:#e2e8f0;color:#374151" form="form-unpost" type="submit">Отменить проведение</button>{{end}}
      {{else}}
        {{if $.CanPost}}<button class="btn btn-primary" type="submit" name="_action" value="post" form="main-form">Провести</button>{{end}}
      {{end}}
    {{end}}
  {{end}}
  {{if not .IsNew}}
    <a href="/ui/{{lower (str .Entity.Kind)}}/{{.Entity.Name}}/{{.ID}}/history" class="btn btn-sm btn-secondary">История</a>
    {{if or .PrintForms .DSLPrintForms .HasPrintProc}}
    <div style="position:relative">
      <button type="button" class="btn btn-sm btn-secondary" onclick="var d=this.nextElementSibling;d.style.display=d.style.display==='none'?'block':'none'">Печать ▾</button>
      <div style="display:none;position:absolute;top:100%;left:0;background:#fff;border:1px solid #e2e8f0;border-radius:8px;box-shadow:0 4px 16px rgba(0,0,0,.1);min-width:160px;z-index:50;margin-top:4px">
        {{range .PrintForms}}
        <a href="/ui/{{lower (str $.Entity.Kind)}}/{{$.Entity.Name}}/{{$.ID}}/print/{{.Name}}" target="_blank"
           style="display:block;padding:9px 16px;color:#334155;text-decoration:none;font-size:13px;border-bottom:1px solid #f1f5f9">{{.Name}}</a>
        {{end}}
        {{range .DSLPrintForms}}
        <a href="/ui/{{lower (str $.Entity.Kind)}}/{{$.Entity.Name}}/{{$.ID}}/print-dsl/{{.Name}}" target="_blank"
           style="display:block;padding:9px 16px;color:#334155;text-decoration:none;font-size:13px;border-bottom:1px solid #f1f5f9">📋 {{.Name}}</a>
        {{end}}
        {{if .HasPrintProc}}
        <a href="/ui/{{lower (str .Entity.Kind)}}/{{.Entity.Name}}/{{.ID}}/print-dsl/_module" target="_blank"
           style="display:block;padding:9px 16px;color:#334155;text-decoration:none;font-size:13px;border-bottom:1px solid #f1f5f9">📋 Печать (модуль)</a>
        {{end}}
      </div>
    </div>
    {{end}}
    {{if .CanDelete}}
    <form method="POST" action="/ui/{{lower (str .Entity.Kind)}}/{{.Entity.Name}}/{{.ID}}/delete"
          onsubmit="return confirm('{{if .IsAdmin}}Удалить запись навсегда?{{else}}Пометить запись на удаление?{{end}}')" style="margin-left:auto">
      <button class="btn btn-danger btn-sm" type="submit">{{if .IsAdmin}}Удалить{{else}}Пометить на удаление{{end}}</button>
    </form>
    {{end}}
  {{end}}
</div>

{{/* Movement links (collapsed) */}}
{{if and (not .IsNew) .DocMovements}}
<div style="margin-bottom:12px;display:flex;gap:6px;flex-wrap:wrap">
  {{range $regName, $rows := .DocMovements}}
  <details style="display:inline">
    <summary style="display:inline;cursor:pointer;font-size:12px;padding:4px 10px;background:#f0f4ff;color:#1a4a80;border-radius:4px;font-weight:600;list-style:none">
      {{$regName}} ({{len $rows}}) ▾
    </summary>
    <div style="position:absolute;z-index:100;background:#fff;border:1px solid #e2e8f0;border-radius:8px;box-shadow:0 4px 16px rgba(0,0,0,.12);margin-top:4px;min-width:300px;max-height:300px;overflow:auto">
      <table class="list-tbl" style="font-size:12px;margin:0">
        <tr><th>№</th><th>Вид</th>{{$first := index $rows 0}}{{range $k, $v := $first}}{{if and (ne $k "line_number") (ne $k "вид_движения")}}<th>{{$k}}</th>{{end}}{{end}}</tr>
        {{range $i, $row := $rows}}
        <tr>
          <td>{{add $i 1}}</td>
          <td>{{if eq (index $row "вид_движения") "Приход"}}<span style="color:#16a34a">▲</span>{{else if eq (index $row "вид_движения") "Расход"}}<span style="color:#dc2626">▼</span>{{else}}—{{end}}</td>
          {{range $k, $v := $row}}{{if and (ne $k "line_number") (ne $k "вид_движения")}}<td>{{$v}}</td>{{end}}{{end}}
        </tr>
        {{end}}
      </table>
    </div>
  </details>
  {{end}}
</div>
{{end}}

<div class="card">
<form id="main-form" method="POST">
{{if .Entity.Hierarchical}}
<div class="form-group">
  <label>Тип</label>
  <select name="is_folder">
    <option value="false" {{if ne (index $.Values "is_folder") "true"}}selected{{end}}>Элемент</option>
    <option value="true" {{if eq (index $.Values "is_folder") "true"}}selected{{end}}>Группа</option>
  </select>
</div>
<div class="form-group">
  <label>Родительская группа</label>
  <select name="parent_id">
    <option value="">— корень —</option>
    {{range .FolderOptions}}
    <option value="{{index . "id"}}" {{if eq (index . "id") (index $.Values "parent_id")}}selected{{end}}>{{index . "_label"}}</option>
    {{end}}
  </select>
</div>
{{end}}
{{range .Entity.Fields}}{{$fn := .Name}}
<div class="form-group">
  <label>{{$fn}}</label>
  {{if isRef (str .Type)}}
    <div style="display:flex;gap:6px;align-items:center">
      <select id="ref-{{$fn}}" name="{{$fn}}" style="flex:1">
        <option value="">— выбрать —</option>
        {{range index $.RefOptions $fn}}
        <option value="{{index . "id"}}" {{if eq (index . "id") (index $.Values $fn)}}selected{{end}}>{{index . "_label"}}</option>
        {{end}}
      </select>
      <button type="button" onclick="openRefPicker('ref-{{$fn}}')" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px;white-space:nowrap;flex-shrink:0" title="Выбрать из списка">...</button>
    </div>
  {{else if isEnum (str .Type)}}
    <select name="{{$fn}}">
      <option value="">— выбрать —</option>
      {{range index $.EnumOptions $fn}}
      <option value="{{.}}" {{if eq . (index $.Values $fn)}}selected{{end}}>{{.}}</option>
      {{end}}
    </select>
  {{else if eq (str .Type) "date"}}
    <input type="datetime-local" name="{{$fn}}" value="{{index $.Values $fn}}">
  {{else if eq (str .Type) "bool"}}
    <select name="{{$fn}}">
      <option value="false" {{if eq (index $.Values $fn) "false"}}selected{{end}}>Нет</option>
      <option value="true"  {{if eq (index $.Values $fn) "true"}}selected{{end}}>Да</option>
    </select>
  {{else}}
    <input type="text" name="{{$fn}}" value="{{index $.Values $fn}}" placeholder="{{$fn}}">
  {{end}}
</div>
{{end}}

{{range .Entity.TableParts}}{{$tp := .}}{{$tpName := .Name}}{{$tpRef := index $.TPRefOptions $tpName}}
<h3>{{$tpName}}</h3>
<table class="tp-table">
  <thead><tr>
    {{range .Fields}}<th>{{.Name}}</th>{{end}}
    <th style="width:40px"></th>
  </tr></thead>
  <tbody id="tp-body-{{$tpName}}">
  {{$existingRows := index $.TablePartRows $tpName}}
  {{range $i, $row := $existingRows}}
    <tr>
      {{range $tp.Fields}}{{$fn := .Name}}
        <td>
        {{if isRef (str .Type)}}
          <div style="display:flex;gap:4px;align-items:center">
            <select name="tp.{{$tpName}}.{{$i}}.{{$fn}}" style="flex:1">
              <option value="">— выбрать —</option>
              {{range index $tpRef $fn}}
              <option value="{{index . "id"}}" {{if eq (str (index . "id")) (refID (index $row $fn))}}selected{{end}}>{{index . "_label"}}</option>
              {{end}}
            </select>
            <button type="button" onclick="openRefPicker(this.parentElement.querySelector('select'))" style="padding:4px 8px;border:1px solid #e2e8f0;border-radius:5px;background:#f8fafc;cursor:pointer;font-size:12px;flex-shrink:0" title="Выбрать из списка">...</button>
          </div>
        {{else if eq (str .Type) "number"}}
          <input type="number" name="tp.{{$tpName}}.{{$i}}.{{$fn}}" value="{{index $row $fn}}"
            data-tp-num="{{$fn}}" oninput="recalcTpRow(this)">
        {{else}}
          <input type="text" name="tp.{{$tpName}}.{{$i}}.{{$fn}}" value="{{index $row $fn}}"
            oninput="recalcTpRow(this)">
        {{end}}
        </td>
      {{end}}
      <td><button type="button" class="del-btn" onclick="this.closest('tr').remove()">×</button></td>
    </tr>
  {{end}}
  </tbody>
</table>
<button type="button" class="btn btn-sm" style="background:#e2e8f0;color:#475569;margin-bottom:8px"
  onclick="addTpRow('{{$tpName}}', [{{range .Fields}}'{{.Name}}',{{end}}], [{{range .Fields}}{{if eq (str .Type) "number"}}'{{.Name}}',{{end}}{{end}}], document.getElementById('tp-body-{{$tpName}}').rows.length)">
  + Добавить строку
</button>
{{end}}

<div style="margin-top:16px">
  {{if .CanWrite}}<button class="btn btn-secondary" type="submit" name="_action" value="" form="main-form">Записать</button>{{end}}
  <a href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}" class="btn btn-cancel">Отмена</a>
</div>
</form>
{{if and (not .IsNew) .Entity.Posting}}
{{if eq (index .Values "posted") "true"}}
<form id="form-unpost" method="POST" action="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}/{{.ID}}/unpost"></form>
{{end}}
{{end}}
{{if not .IsNew}}
<div class="card" style="margin-top:16px">
  <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
    <h3 style="margin:0;font-size:14px;font-weight:600;color:#374151">Вложения</h3>
    <span id="att-count" style="color:#94a3b8;font-size:12px"></span>
  </div>
  <div id="att-list" style="margin-bottom:12px"></div>
  <form id="att-upload-form" method="POST" enctype="multipart/form-data"
        action="/ui/{{lower (str .Entity.Kind)}}/{{.Entity.Name}}/{{.ID}}/attachments">
    <input type="file" name="file" id="att-file-input" style="display:none" onchange="document.getElementById('att-upload-form').submit()">
    <button type="button" class="btn btn-sm btn-secondary" onclick="document.getElementById('att-file-input').click()">
      + Прикрепить файл
    </button>
  </form>
</div>
<script>
(function(){
  function fmtSize(b) {
    if(b<1024) return b+' Б';
    if(b<1024*1024) return (b/1024).toFixed(1)+' КБ';
    return (b/1024/1024).toFixed(1)+' МБ';
  }
  function loadAtts() {
    fetch('/ui/{{lower (str .Entity.Kind)}}/{{.Entity.Name}}/{{.ID}}/attachments')
      .then(r=>r.json()).then(atts=>{
        var cnt = document.getElementById('att-count');
        var list = document.getElementById('att-list');
        cnt.textContent = atts.length ? atts.length+' файл(ов)' : '';
        if(!atts.length){ list.innerHTML='<p style="color:#94a3b8;font-size:13px;margin:0">Нет вложений</p>'; return; }
        list.innerHTML = atts.map(a=>
          '<div style="display:flex;align-items:center;gap:8px;padding:6px 0;border-bottom:1px solid #f1f5f9">'+
          '<span style="flex:1;font-size:13px;word-break:break-all">'+a.filename+'</span>'+
          '<span style="color:#94a3b8;font-size:12px;white-space:nowrap">'+fmtSize(a.size_bytes)+'</span>'+
          '<a href="/ui/attachments/'+a.id+'/download" class="btn btn-sm btn-secondary" style="padding:3px 10px;font-size:12px">↓</a>'+
          '<form method="POST" action="/ui/attachments/'+a.id+'/delete" style="margin:0"'+
          ' onsubmit="return confirm(\'Удалить вложение?\')">'+
          '<button type="submit" class="btn btn-sm btn-danger" style="padding:3px 8px;font-size:12px">×</button>'+
          '</form>'+
          '</div>'
        ).join('');
      }).catch(function(){});
  }
  loadAtts();
})();
</script>
{{end}}
</div>
<script>
window._tpRefOpts = {{jsJSON .TPRefOptions}};
function addTpRow(tpName, fields, numFields, idx) {
  var tbody = document.getElementById('tp-body-' + tpName);
  var tr = document.createElement('tr');
  var refOpts = (window._tpRefOpts && window._tpRefOpts[tpName]) || {};
  fields.forEach(function(fn) {
    var td = document.createElement('td');
    if (refOpts[fn] !== undefined) {
      var wrapper = document.createElement('div');
      wrapper.style.cssText = 'display:flex;gap:4px;align-items:center';
      var sel = document.createElement('select');
      sel.name = 'tp.' + tpName + '.' + idx + '.' + fn;
      sel.style.flex = '1';
      var defOpt = document.createElement('option');
      defOpt.value = ''; defOpt.textContent = '— выбрать —';
      sel.appendChild(defOpt);
      (refOpts[fn] || []).forEach(function(opt) {
        var o = document.createElement('option');
        o.value = opt.id; o.textContent = opt._label || opt.id;
        sel.appendChild(o);
      });
      var pickBtn = document.createElement('button');
      pickBtn.type = 'button'; pickBtn.textContent = '...';
      pickBtn.title = 'Выбрать из списка';
      pickBtn.style.cssText = 'padding:4px 8px;border:1px solid #e2e8f0;border-radius:5px;background:#f8fafc;cursor:pointer;font-size:12px;flex-shrink:0';
      (function(s){ pickBtn.onclick = function(){ openRefPicker(s); }; })(sel);
      wrapper.appendChild(sel);
      wrapper.appendChild(pickBtn);
      td.appendChild(wrapper);
    } else {
      var inp = document.createElement('input');
      inp.name = 'tp.' + tpName + '.' + idx + '.' + fn;
      if (numFields.indexOf(fn) !== -1) {
        inp.type = 'number';
        inp.setAttribute('data-tp-num', fn);
        inp.setAttribute('oninput', 'recalcTpRow(this)');
      } else {
        inp.type = 'text';
      }
      td.appendChild(inp);
    }
    tr.appendChild(td);
  });
  var tdDel = document.createElement('td');
  var btn = document.createElement('button');
  btn.type = 'button'; btn.className = 'del-btn'; btn.textContent = '×';
  btn.onclick = function(){ tr.remove(); };
  tdDel.appendChild(btn);
  tr.appendChild(tdDel);
  tbody.appendChild(tr);
}

// If a row has exactly 3 numeric fields (qty, price, sum), auto-calculate the last.
function recalcTpRow(inp) {
  var tr = inp.closest('tr');
  var nums = tr.querySelectorAll('[data-tp-num]');
  if (nums.length === 3) {
    var a = parseFloat(nums[0].value) || 0;
    var b = parseFloat(nums[1].value) || 0;
    nums[2].value = (a * b).toFixed(2);
  }
}
function openRefPicker(selOrId) {
  var sel = (typeof selOrId === 'string') ? document.getElementById(selOrId) : selOrId;
  if (!sel) return;
  var opts = [];
  for (var i = 0; i < sel.options.length; i++) {
    var o = sel.options[i];
    if (o.value) opts.push({id: o.value, label: o.text});
  }
  var old = document.getElementById('_ref-picker-modal');
  if (old) old.remove();
  var modal = document.createElement('div');
  modal.id = '_ref-picker-modal';
  modal.style.cssText = 'position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,.4);z-index:9999;display:flex;align-items:center;justify-content:center';
  var inner = '<div style="background:#fff;border-radius:10px;padding:20px;width:480px;max-width:95vw;max-height:80vh;display:flex;flex-direction:column;box-shadow:0 8px 32px rgba(0,0,0,.18)">';
  inner += '<div style="font-weight:600;font-size:15px;margin-bottom:12px;color:#1e293b">Выбор из списка</div>';
  inner += '<input id="_rp-search" type="text" placeholder="Поиск..." autocomplete="off" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;font-size:14px;margin-bottom:10px;outline:none">';
  inner += '<div id="_rp-list" style="overflow-y:auto;flex:1;border:1px solid #e2e8f0;border-radius:7px">';
  if (opts.length === 0) {
    inner += '<div style="padding:16px;color:#94a3b8;font-size:13px;text-align:center">Список пуст</div>';
  } else {
    for (var i = 0; i < opts.length; i++) {
      inner += '<div data-id="' + opts[i].id.replace(/"/g,'&quot;') + '" class="_rp-item" style="padding:9px 14px;cursor:pointer;border-bottom:1px solid #f1f5f9;font-size:14px;color:#1e293b">' + opts[i].label + '</div>';
    }
  }
  inner += '</div>';
  inner += '<div style="margin-top:12px;text-align:right"><button type="button" id="_rp-cancel" style="padding:6px 18px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px">Отмена</button></div>';
  inner += '</div>';
  modal.innerHTML = inner;
  document.body.appendChild(modal);
  window._rpTarget = sel;
  var search = document.getElementById('_rp-search');
  search.focus();
  search.addEventListener('input', function() {
    var q = this.value.toLowerCase();
    var items = document.querySelectorAll('._rp-item');
    for (var i = 0; i < items.length; i++) {
      items[i].style.display = items[i].textContent.toLowerCase().indexOf(q) >= 0 ? '' : 'none';
    }
  });
  document.getElementById('_rp-list').addEventListener('click', function(e) {
    var item = e.target.closest('._rp-item');
    if (!item) return;
    if (window._rpTarget) window._rpTarget.value = item.getAttribute('data-id');
    modal.remove();
  });
  document.getElementById('_rp-cancel').addEventListener('click', function() { modal.remove(); });
  modal.addEventListener('click', function(e) { if (e.target === modal) modal.remove(); });
}
</script>
</main></div></body></html>
{{end}}
`

const tplReport = `
{{define "page-report"}}
{{template "head" .}}{{template "nav" .}}
<main>
<h2>{{if .Report.Title}}{{.Report.Title}}{{else}}{{.Report.Name}}{{end}}</h2>
{{if .ReportParams}}
<div class="card" style="margin-bottom:16px">
<form method="POST">
  <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px;margin-bottom:16px">
  {{range .ReportParams}}{{$p := .}}{{$pname := .Name}}{{$pval := str (index $.ParamValues .Name)}}
    {{if $p.IsBool}}
    <div class="form-group" style="margin-bottom:0">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
        <input type="checkbox" name="{{$pname}}" value="true" {{if index $.ParamValues $pname}}checked{{end}}>
        <span>{{$p.Label}}</span>
      </label>
    </div>
    {{else}}
    <div class="form-group" style="margin-bottom:0">
      <label>{{$p.Label}}</label>
      {{if $p.IsDate}}
        <input type="date" name="{{$pname}}" value="{{$pval}}">
      {{else if $p.IsNum}}
        <input type="number" name="{{$pname}}" value="{{$pval}}">
      {{else if $p.IsSel}}
        <select name="{{$pname}}">
          {{range $p.Options}}<option value="{{.}}" {{if eq . $pval}}selected{{end}}>{{if .}}{{.}}{{else}}— все —{{end}}</option>{{end}}
        </select>
      {{else if $p.IsRef}}
        <div style="display:flex;gap:4px;align-items:center">
          <select name="{{$pname}}" id="rp-{{$pname}}" style="flex:1;min-width:0">
            <option value="">— все —</option>
            {{range $p.Opts}}
              <option value="{{index . "id"}}" {{if eq $pval (str (index . "id"))}}selected{{end}}>{{index . "_label"}}</option>
            {{end}}
          </select>
          <button type="button" onclick="openRefPicker('rp-{{$pname}}')" style="padding:6px 10px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px;flex-shrink:0" title="Выбрать из списка">...</button>
        </div>
      {{else}}
        <input type="text" name="{{$pname}}" value="{{$pval}}">
      {{end}}
    </div>
    {{end}}
  {{end}}
  </div>
  <button class="btn btn-primary" type="submit">Сформировать</button>
</form>
</div>
{{end}}
{{if .QueryError}}<div class="error">Ошибка запроса: {{.QueryError}}</div>{{end}}
{{if .ChartOption}}
<div class="card" style="margin-bottom:16px">
  <div id="ob-chart" style="width:100%;min-height:400px"></div>
</div>
<script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
<script>
(function(){
  var c=echarts.init(document.getElementById('ob-chart'));
  var _o={{jsJSON .ChartOption}};if(_o.yAxis&&_o.yAxis.type==="value"){_o.yAxis.axisLabel={formatter:function(v){if(Math.abs(v)>=1e6)return(v/1e6).toFixed(1)+"M";if(Math.abs(v)>=1e3)return(v/1e3).toFixed(1)+"k";return v%1===0?v:v.toFixed(2)}};}
  c.setOption(_o);
  window.addEventListener('resize',function(){c.resize()});
})();
</script>
{{end}}
{{if .Cols}}
<div style="display:flex;justify-content:flex-end;margin-bottom:8px">
  <a class="btn btn-sm" href="/ui/report/{{lower .Report.Name}}/excel{{reportParamQuery .Report.Params .ParamValues}}" style="background:#16a34a;color:#fff" title="Скачать Excel">Excel ↓</a>
</div>
<div class="card">
{{if .Rows}}
<table><thead><tr>{{range .Cols}}<th>{{.}}</th>{{end}}</tr></thead>
<tbody>
{{range .Rows}}{{$row := .}}<tr>
  {{range $.Cols}}<td>{{fmtCell (index $row .)}}</td>{{end}}
</tr>{{end}}
</tbody></table>
{{else}}<p class="empty">Нет данных</p>{{end}}
</div>
{{end}}
</main></div></body></html>
{{end}}
`

const tplRegister = `
{{define "page-register-movements"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div class="row-top">
  <h2>{{.Register.Name}} — движения</h2>
  <a class="btn btn-sm" href="/ui/register/{{lower .Register.Name}}/balances" style="background:#e2e8f0;color:#475569">Остатки →</a>
</div>
<div class="card">
{{if .Rows}}
<table><thead><tr>
  <th>Вид движения</th>
  <th>Регистратор</th>
  {{range .Register.Dimensions}}<th>{{.Name}}</th>{{end}}
  {{range .Register.Resources}}<th>{{.Name}}</th>{{end}}
  {{range .Register.Attributes}}<th>{{.Name}}</th>{{end}}
</tr></thead><tbody>
{{range .Rows}}{{$row := .}}<tr>
  <td>{{$v := index $row "вид_движения"}}{{if eq (str $v) "Приход"}}<span style="color:#16a34a;font-weight:600">▲ {{$v}}</span>{{else}}<span style="color:#dc2626;font-weight:600">▼ {{$v}}</span>{{end}}</td>
  <td style="font-size:12px;color:#475569">{{if index $row "recorder_label"}}{{index $row "recorder_label"}}{{else}}{{index $row "recorder_type"}}{{end}}</td>
  {{range $.Register.Dimensions}}<td>{{index $row .Name}}</td>{{end}}
  {{range $.Register.Resources}}<td>{{index $row .Name}}</td>{{end}}
  {{range $.Register.Attributes}}<td>{{index $row .Name}}</td>{{end}}
</tr>{{end}}
</tbody></table>
{{else}}<p class="empty">Движений нет</p>{{end}}
</div></main></div></body></html>
{{end}}

{{define "page-register-balances"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div class="row-top">
  <h2>{{.Register.Name}} — остатки</h2>
  <a class="btn btn-sm" href="/ui/register/{{lower .Register.Name}}" style="background:#e2e8f0;color:#475569">← Движения</a>
</div>
<div class="card">
{{if .Rows}}
<table><thead><tr>
  {{range .Register.Dimensions}}<th>{{.Name}}</th>{{end}}
  {{range .Register.Resources}}<th>{{.Name}}</th>{{end}}
</tr></thead><tbody>
{{range .Rows}}{{$row := .}}<tr>
  {{range $.Register.Dimensions}}<td>{{index $row .Name}}</td>{{end}}
  {{range $.Register.Resources}}<td style="font-weight:600">{{index $row .Name}}</td>{{end}}
</tr>{{end}}
</tbody></table>
{{else}}<p class="empty">Остатков нет</p>{{end}}
</div></main></div></body></html>
{{end}}
`

const tplDeleteMarked = `
{{define "page-delete-marked"}}
{{template "head" .}}{{template "nav" .}}
<main>
<h2>Удалить помеченные</h2>
{{if .Deleted}}<div style="background:#f0fdf4;border:1px solid #bbf7d0;color:#16a34a;padding:12px 16px;border-radius:7px;margin-bottom:16px;font-size:14px">
  Удалено: {{.Deleted}}{{if .Skipped}} &nbsp;·&nbsp; Пропущено (есть ссылки): {{.Skipped}}{{end}}
</div>{{end}}
{{if .Entries}}
<div class="card" style="max-width:900px;margin-bottom:16px">
<table><thead><tr>
  <th>Объект</th><th>Наименование</th><th>Статус</th>
</tr></thead><tbody>
{{range .Entries}}<tr>
  <td><a href="/ui/{{lower .Kind}}/{{lower .EntityName}}/{{.ID}}">{{.EntityName}}</a></td>
  <td>{{.Label}}</td>
  <td>{{if .HasRefs}}<span style="color:#ef4444">Есть ссылки — не будет удалён</span>{{else}}<span style="color:#16a34a">Будет удалён</span>{{end}}</td>
</tr>{{end}}
</tbody></table>
</div>
<form method="POST" action="/ui/delete-marked"
      onsubmit="return confirm('Удалить все помеченные записи без ссылок?')">
  <button class="btn btn-danger" type="submit">Удалить помеченные без ссылок</button>
  <a class="btn btn-secondary" href="/ui" style="margin-left:8px">Отмена</a>
</form>
{{else}}
<div class="card" style="max-width:600px">
  <p class="empty">Помеченных на удаление записей нет.</p>
</div>
{{end}}
</main></div></body></html>
{{end}}
`

const tplProcessor = `
{{define "page-processor"}}
{{template "head" .}}{{template "nav" .}}
<main>
<h2>{{if .Processor.Title}}{{.Processor.Title}}{{else}}{{.Processor.Name}}{{end}}</h2>
{{if .Processor.Params}}
<div class="card" style="margin-bottom:16px">
<form method="POST">
  <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px;margin-bottom:16px">
  {{range .Processor.Params}}{{$pname := .Name}}
    {{if eq .Type "bool"}}
    <div class="form-group" style="margin-bottom:0">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
        <input type="checkbox" name="{{$pname}}" value="true" {{if index $.ParamValues $pname}}checked{{end}}>
        <span>{{.DisplayLabel}}</span>
      </label>
    </div>
    {{else}}
    <div class="form-group" style="margin-bottom:0">
      <label>{{.DisplayLabel}}</label>
      {{if eq .Type "date"}}
        <input type="date" name="{{$pname}}" value="{{index $.ParamValues $pname}}">
      {{else if eq .Type "number"}}
        <input type="number" name="{{$pname}}" value="{{index $.ParamValues $pname}}">
      {{else}}
        <input type="text" name="{{$pname}}" value="{{index $.ParamValues $pname}}">
      {{end}}
    </div>
    {{end}}
  {{end}}
  </div>
  <button class="btn btn-primary" type="submit">Выполнить</button>
</form>
</div>
{{else}}
<div class="card" style="margin-bottom:16px">
<form method="POST">
  <button class="btn btn-primary" type="submit">Выполнить</button>
</form>
</div>
{{end}}
{{if .Ran}}
<div class="card">
{{if .RunError}}
  <div class="error">{{.RunError}}</div>
{{else if .Messages}}
  <table><tbody>
  {{range .Messages}}<tr><td style="font-family:monospace;font-size:13px;padding:6px 12px;border-bottom:1px solid #f1f5f9">{{.}}</td></tr>{{end}}
  </tbody></table>
{{else}}
  <p class="empty">Выполнено без сообщений</p>
{{end}}
</div>
{{end}}
</main></div></body></html>
{{end}}
`

const tplAbout = `
{{define "page-about"}}
{{template "head" .}}{{template "nav" .}}
<main>
<h2>О программе</h2>
<div class="card" style="max-width:560px">
  {{if .Cfg.Logo}}<div style="text-align:center;margin-bottom:20px"><img src="/ui/logo" alt="Logo" style="max-height:160px;max-width:360px"></div>{{end}}
  <table style="width:100%;border-collapse:collapse">
    {{if .User}}
    <tr>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;color:#64748b;width:180px;font-size:14px">Пользователь</td>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;font-size:14px">
        <span style="font-weight:600">{{.User.Login}}</span>
        {{if .User.FullName}}<span style="color:#64748b;margin-left:8px">{{.User.FullName}}</span>{{end}}
        {{if .User.IsAdmin}}<span style="margin-left:8px;background:#dbeafe;color:#1d4ed8;font-size:11px;padding:2px 7px;border-radius:10px;font-weight:600">Администратор</span>{{end}}
      </td>
    </tr>
    {{end}}
    <tr>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;color:#64748b;width:180px;font-size:14px">Версия платформы</td>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;font-weight:600;font-size:14px">onebase {{if .Cfg.PlatVersion}}{{.Cfg.PlatVersion}}{{else}}dev{{end}}</td>
    </tr>
    {{if .Cfg.AppName}}
    <tr>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;color:#64748b;font-size:14px">Конфигурация</td>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;font-size:14px;font-weight:600">{{.Cfg.AppName}}</td>
    </tr>
    {{end}}
    {{if .Cfg.AppVersion}}
    <tr>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;color:#64748b;font-size:14px">Версия конфигурации</td>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;font-size:14px">{{.Cfg.AppVersion}}</td>
    </tr>
    {{end}}
    <tr>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;color:#64748b;font-size:14px">База данных</td>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;font-size:13px;color:#475569;word-break:break-all">{{.Cfg.DSN}}</td>
    </tr>
    <tr>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;color:#64748b;font-size:14px">Метаданные</td>
      <td style="padding:14px 0;border-bottom:1px solid #f1f5f9;font-size:14px">
        Справочники: {{.Catalogs}} &nbsp;·&nbsp;
        Документы: {{.Documents}} &nbsp;·&nbsp;
        Регистры: {{.Registers}} &nbsp;·&nbsp;
        Отчёты: {{.Reports}}
      </td>
    </tr>
  </table>
</div>
</main></div></body></html>
{{end}}
`

const tplInfoReg = `
{{define "page-inforeg-list"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div class="row-top">
  <h2>{{.InfoReg.Name}}{{if .InfoReg.Periodic}} <span style="font-size:13px;color:#64748b;font-weight:400">(периодический)</span>{{end}}</h2>
  {{if .CanWrite}}<a class="btn" href="/ui/inforeg/{{lower .InfoReg.Name}}/new">+ Добавить запись</a>{{end}}
</div>
<div class="card">
{{if .Rows}}
<table><thead><tr>
  {{if .InfoReg.Periodic}}<th>Период</th>{{end}}
  {{range .InfoReg.Dimensions}}<th>{{.Name}}</th>{{end}}
  {{range .InfoReg.Resources}}<th>{{.Name}}</th>{{end}}
  {{if .CanDelete}}<th></th>{{end}}
</tr></thead><tbody>
{{range .Rows}}{{$row := .}}<tr>
  {{if $.InfoReg.Periodic}}<td>{{index $row "period"}}</td>{{end}}
  {{range $.InfoReg.Dimensions}}<td>{{index $row .Name}}</td>{{end}}
  {{range $.InfoReg.Resources}}<td style="font-weight:600">{{index $row .Name}}</td>{{end}}
  {{if $.CanDelete}}<td>
    <form method="POST" action="/ui/inforeg/{{lower $.InfoReg.Name}}/delete" style="display:inline"
          onsubmit="return confirm('Удалить запись?')">
      {{if $.InfoReg.Periodic}}<input type="hidden" name="period" value="{{index $row "period"}}">{{end}}
      {{range $.InfoReg.Dimensions}}<input type="hidden" name="{{.Name}}" value="{{index $row .Name}}">{{end}}
      <button class="btn btn-danger btn-sm" type="submit">×</button>
    </form>
  </td>{{end}}
</tr>{{end}}
</tbody></table>
{{else}}<p class="empty">Записей нет</p>{{end}}
</div></main></div></body></html>
{{end}}

{{define "page-inforeg-form"}}
{{template "head" .}}{{template "nav" .}}
<main>
<h2>{{.InfoReg.Name}} — новая запись</h2>
{{if .Error}}<div style="background:#fef2f2;border:1px solid #fecaca;color:#dc2626;padding:12px 16px;border-radius:7px;margin-bottom:16px;font-size:14px">{{.Error}}</div>{{end}}
<div class="card" style="max-width:560px">
<form method="POST">
  {{if .InfoReg.Periodic}}
  <div class="form-row">
    <label>Период</label>
    <input type="date" name="period" value="{{index .Values "period"}}" required>
  </div>
  {{end}}
  {{range .InfoReg.Dimensions}}
  {{$dn := .Name}}
  <div class="form-row">
    <label>{{.Name}} <span style="color:#94a3b8;font-size:11px">[измерение]</span></label>
    {{if .RefEntity}}
    <div style="display:flex;gap:4px;align-items:center">
      <select name="{{$dn}}" id="ird-{{$dn}}" style="flex:1;min-width:0">
        <option value="">— выбрать —</option>
        {{range index $.RefOpts $dn}}<option value="{{index . "id"}}" {{if eq (index $.Values $dn) (index . "id")}}selected{{end}}>{{index . "_label"}}</option>{{end}}
      </select>
      <button type="button" onclick="openRefPicker('ird-{{$dn}}')" style="padding:6px 10px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px;flex-shrink:0" title="Выбрать из списка">...</button>
    </div>
    {{else}}
    <input type="text" name="{{$dn}}" value="{{index $.Values $dn}}">
    {{end}}
  </div>
  {{end}}
  {{range .InfoReg.Resources}}
  <div class="form-row">
    <label>{{.Name}}</label>
    <input type="text" name="{{.Name}}" value="{{index $.Values .Name}}">
  </div>
  {{end}}
  <div style="margin-top:20px;display:flex;gap:8px">
    <button class="btn" type="submit">Записать</button>
    <a class="btn btn-secondary" href="/ui/inforeg/{{lower .InfoReg.Name}}">Отмена</a>
  </div>
</form>
</div>
<script>
function openRefPicker(selOrId){var sel=(typeof selOrId==='string')?document.getElementById(selOrId):selOrId;if(!sel)return;var opts=[];for(var i=0;i<sel.options.length;i++){var o=sel.options[i];if(o.value)opts.push({id:o.value,label:o.text});}var old=document.getElementById('_ref-picker-modal');if(old)old.remove();var modal=document.createElement('div');modal.id='_ref-picker-modal';modal.style.cssText='position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,.4);z-index:9999;display:flex;align-items:center;justify-content:center';var inner='<div style="background:#fff;border-radius:10px;padding:20px;width:480px;max-width:95vw;max-height:80vh;display:flex;flex-direction:column;box-shadow:0 8px 32px rgba(0,0,0,.18)">';inner+='<div style="font-weight:600;font-size:15px;margin-bottom:12px;color:#1e293b">Выбор из списка</div>';inner+='<input id="_rp-search" type="text" placeholder="Поиск..." autocomplete="off" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;font-size:14px;margin-bottom:10px;outline:none">';inner+='<div id="_rp-list" style="overflow-y:auto;flex:1;border:1px solid #e2e8f0;border-radius:7px">';if(opts.length===0){inner+='<div style="padding:16px;color:#94a3b8;font-size:13px;text-align:center">Список пуст</div>';}else{for(var i=0;i<opts.length;i++){inner+='<div data-id="'+opts[i].id.replace(/"/g,"&quot;")+'" class="_rp-item" style="padding:9px 14px;cursor:pointer;border-bottom:1px solid #f1f5f9;font-size:14px;color:#1e293b">'+opts[i].label+'</div>';}}inner+='</div>';inner+='<div style="margin-top:12px;text-align:right"><button type="button" id="_rp-cancel" style="padding:6px 18px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px">Отмена</button></div>';inner+='</div>';modal.innerHTML=inner;document.body.appendChild(modal);window._rpTarget=sel;var search=document.getElementById('_rp-search');search.focus();search.addEventListener('input',function(){var q=this.value.toLowerCase();document.querySelectorAll('._rp-item').forEach(function(el){el.style.display=el.textContent.toLowerCase().indexOf(q)>=0?'':'none';});});document.getElementById('_rp-list').addEventListener('click',function(e){var item=e.target.closest('._rp-item');if(!item)return;if(window._rpTarget)window._rpTarget.value=item.getAttribute('data-id');modal.remove();});document.getElementById('_rp-cancel').addEventListener('click',function(){modal.remove();});modal.addEventListener('click',function(e){if(e.target===modal)modal.remove();});}
</script>
</main></div></body></html>
{{end}}
`

const tplConstants = `
{{define "page-constants"}}
{{template "head" .}}{{template "nav" .}}
<main>
<h2>Константы</h2>
{{if .Saved}}<div style="background:#f0fdf4;border:1px solid #86efac;color:#15803d;padding:12px 16px;border-radius:7px;margin-bottom:16px;font-size:14px">✓ Константы сохранены</div>{{end}}
<div class="card" style="max-width:700px">
<form method="POST" action="/ui/constants">
{{range .Constants}}{{$c := .}}
<div class="form-group">
  <label>{{if .Label}}{{.Label}}{{else}}{{.Name}}{{end}}</label>
  {{if .RefEntity}}
    <select name="{{.Name}}">
      <option value="">— не выбрано —</option>
      {{range index $.RefOpts .Name}}
      <option value="{{index . "id"}}" {{if eq (index . "id") (index $.Values $c.Name)}}selected{{end}}>{{index . "_label"}}</option>
      {{end}}
    </select>
  {{else if eq (str .Type) "date"}}
    <input type="date" name="{{.Name}}" value="{{index $.Values .Name}}">
  {{else if eq (str .Type) "bool"}}
    <select name="{{.Name}}">
      <option value="false" {{if eq (index $.Values .Name) "false"}}selected{{end}}>Нет</option>
      <option value="true"  {{if eq (index $.Values .Name) "true"}}selected{{end}}>Да</option>
    </select>
  {{else}}
    <input type="text" name="{{.Name}}" value="{{index $.Values .Name}}" placeholder="{{.Name}}">
  {{end}}
</div>
{{end}}
{{if not .Constants}}
<p class="empty">Нет констант в конфигурации</p>
{{else}}
<div style="margin-top:20px">
  <button class="btn btn-primary" type="submit">Сохранить</button>
</div>
{{end}}
</form>
</div></main></div></body></html>
{{end}}
`

const tplJournal = `
{{define "page-journal"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div class="row-top">
  <h2>{{.Journal.Title}}</h2>
  <div style="display:flex;align-items:center;gap:12px">
    <span style="color:#94a3b8;font-size:13px">Всего: {{.Total}}</span>
    <a class="btn btn-sm" href="/ui/journal/{{lower .Journal.Name}}/excel{{filterQuery .Params}}" style="background:#16a34a;color:#fff" title="Скачать Excel">Excel ↓</a>
  </div>
</div>
{{$j := .Journal}}{{$params := .Params}}{{$fmts := .ColFormats}}
<details{{if hasFilter $params}} open{{end}}>
  <summary>Отбор</summary>
  <form method="GET" action="">
  <div class="filter-body">
  {{range $j.Filters}}
    {{if eq .Type "date_range"}}
    <div>
      <label>{{.Field}} с</label>
      <input type="date" name="f.{{.Field}}.from" value="{{(filterVal $params .Field).From}}">
    </div>
    <div>
      <label>{{.Field}} по</label>
      <input type="date" name="f.{{.Field}}.to" value="{{(filterVal $params .Field).To}}">
    </div>
    {{else}}
    <div>
      <label>{{.Field}}</label>
      {{if index $.FilterOptions .Field}}
      <select name="f.{{.Field}}">
        <option value="">— все —</option>
        {{range index $.FilterOptions .Field}}
        <option value="{{index . "id"}}" {{if eq (index . "id") (filterVal $params .Field).Value}}selected{{end}}>{{index . "_label"}}</option>
        {{end}}
      </select>
      {{else}}
      <input type="text" name="f.{{.Field}}" value="{{(filterVal $params .Field).Value}}">
      {{end}}
    </div>
    {{end}}
  {{end}}
  </div>
  <div class="filter-actions">
    <button class="btn btn-primary btn-sm" type="submit">Применить</button>
    <a class="btn btn-sm" href="?" style="background:#e2e8f0;color:#475569">Сбросить</a>
  </div>
  {{if $.CurrentSubsystem}}<input type="hidden" name="subsystem" value="{{$.CurrentSubsystem}}">{{end}}
  </form>
</details>

<div class="card">
{{if .Rows}}
<table><thead><tr>
  <th>Документ</th>
  {{range $j.Columns}}<th>{{.Label}}</th>{{end}}
  <th style="width:90px"></th>
</tr></thead>
<tbody>
{{range .Rows}}{{$row := .}}
<tr style="cursor:pointer"
  onclick="if(event.target.tagName!=='A'&&event.target.tagName!=='BUTTON'){window.location='/ui/document/'+encodeURIComponent('{{lower (str (index . "_doc_kind"))}}')+'/'+'{{str (index . "id")}}'}"
>
  <td>{{index . "_doc_kind"}}</td>
  {{range $j.Columns}}
    {{$v := index $row .Field}}
    {{if eq (index $fmts .Field) "date"}}<td>{{fmtDate $v}}</td>
    {{else}}<td>{{$v}}</td>{{end}}
  {{end}}
  <td><a class="btn btn-sm btn-primary" href="/ui/document/{{lower (str (index . "_doc_kind"))}}/{{str (index . "id")}}">Открыть</a></td>
</tr>
{{end}}
</tbody></table>
{{else}}
<p class="empty">Документов нет</p>
{{end}}
</div>

{{if or .HasPrev .HasNext}}
<div style="display:flex;gap:8px;margin-top:12px">
  {{if .HasPrev}}<a class="btn btn-secondary" href="?offset={{.PrevOffset}}{{filterQuery $params}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}">← Назад</a>{{end}}
  {{if .HasNext}}<a class="btn btn-secondary" href="?offset={{.NextOffset}}{{filterQuery $params}}{{if $.CurrentSubsystem}}&subsystem={{$.CurrentSubsystem}}{{end}}">Вперёд →</a>{{end}}
</div>
{{end}}

</main></div></body></html>
{{end}}
`

const tplHistory = `
{{define "page-history"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;max-width:900px">
  <h2 style="margin-bottom:0">История изменений — {{.EntityName}}</h2>
  <a href="{{.BackURL}}" style="font-size:22px;line-height:1;color:#94a3b8;text-decoration:none;padding:2px 8px;border-radius:5px;background:#f1f5f9;font-weight:300">×</a>
</div>
<div class="card" style="max-width:900px">
{{if .Entries}}
<table style="font-size:13px">
<thead><tr>
  <th>Время</th><th>Пользователь</th><th>Действие</th><th>Поле</th><th>Было</th><th>Стало</th>
</tr></thead>
<tbody>
{{range .Entries}}<tr>
  <td style="white-space:nowrap;color:#94a3b8">{{.At.Format "02.01.2006 15:04:05"}}</td>
  <td>{{.UserLogin}}</td>
  <td><span style="font-family:monospace;font-size:11px;background:#f1f5f9;padding:2px 6px;border-radius:4px">{{.Action}}</span></td>
  <td style="font-family:monospace;font-size:12px">{{.Field}}</td>
  <td style="color:#dc2626;font-size:12px">{{.OldValue}}</td>
  <td style="color:#16a34a;font-size:12px">{{.NewValue}}</td>
</tr>{{end}}
</tbody>
</table>
{{else}}
<p class="empty">История изменений пуста.</p>
{{end}}
</div>
</main></div></body></html>
{{end}}
`


const tplScheduled = `
{{define "page-scheduled-list"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div class="row-top">
  <h2>Регламентные задания</h2>
  <span style="color:#94a3b8;font-size:13px">Всего: {{len .JobRows}}</span>
</div>
<div class="card">
{{if .JobRows}}
<table><thead><tr>
  <th>Название</th>
  <th>Расписание</th>
  <th>Обработка</th>
  <th>Статус</th>
  <th>Последний запуск</th>
  <th>Длительность</th>
  <th style="width:90px"></th>
</tr></thead>
<tbody>
{{range .JobRows}}
{{$job := .Job}}
<tr>
  <td><strong>{{$job.Title}}</strong><br><small style="color:#94a3b8">{{$job.Name}}</small></td>
  <td><code>{{$job.Schedule}}</code></td>
  <td>{{$job.Processor}}</td>
  <td>{{if $job.Enabled}}<span style="color:#22c55e">✓ активно</span>{{else}}<span style="color:#94a3b8">— отключено</span>{{end}}</td>
  <td>
    {{if .LastRun}}
      {{$r := .LastRun}}
      <span style="color:{{if eq $r.Status "success"}}#22c55e{{else if eq $r.Status "error"}}#ef4444{{else if eq $r.Status "timeout"}}#f97316{{else}}#94a3b8{{end}}">{{$r.Status}}</span>
      <br><small style="color:#94a3b8">{{fmtDate $r.StartedAt}}</small>
    {{else}}
      <span style="color:#94a3b8">—</span>
    {{end}}
  </td>
  <td>
    {{if .LastRun}}{{.LastRun.DurationMs}} мс{{else}}—{{end}}
  </td>
  <td>
    <a class="btn btn-sm btn-primary" href="/ui/admin/scheduled/{{$job.Name}}">Подробнее</a>
  </td>
</tr>
{{end}}
</tbody></table>
{{else}}
<p class="empty">Регламентных заданий нет. Создайте файлы в папке <code>scheduled/</code> вашей конфигурации.</p>
{{end}}
</div>
</main></div></body></html>
{{end}}

{{define "page-scheduled-detail"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px">
  <div>
    <h2 style="margin-bottom:4px">{{.Job.Title}}</h2>
    <small style="color:#94a3b8">{{.Job.Name}}</small>
  </div>
  <a href="/ui/admin/scheduled" style="font-size:22px;line-height:1;color:#94a3b8;text-decoration:none;padding:2px 8px;border-radius:5px;background:#f1f5f9">×</a>
</div>

<div class="card" style="margin-bottom:20px">
<table style="width:100%;border-collapse:collapse">
  <tr><td style="padding:6px 12px;color:#64748b;width:160px">Расписание</td><td><code>{{.Job.Schedule}}</code></td></tr>
  <tr><td style="padding:6px 12px;color:#64748b">Обработка</td><td>{{.Job.Processor}}</td></tr>
  <tr><td style="padding:6px 12px;color:#64748b">При ошибке</td><td>{{.Job.OnError}}</td></tr>
  <tr><td style="padding:6px 12px;color:#64748b">Таймаут</td><td>{{.Job.Timeout}} сек.</td></tr>
  <tr><td style="padding:6px 12px;color:#64748b">Состояние</td><td>
    {{if .Job.Enabled}}<span style="color:#22c55e">✓ активно</span>{{else}}<span style="color:#94a3b8">— отключено</span>{{end}}
  </td></tr>
</table>
</div>

<form method="POST" action="/ui/admin/scheduled/{{.Job.Name}}/run-now" style="margin-bottom:20px">
  <button class="btn btn-primary" type="submit">▶ Запустить сейчас</button>
</form>

<h3>История запусков (последние 50)</h3>
<div class="card">
{{if .Runs}}
<table><thead><tr>
  <th>Начало</th>
  <th>Статус</th>
  <th>Длительность</th>
  <th>Вывод</th>
  <th>Ошибка</th>
</tr></thead>
<tbody>
{{range .Runs}}
<tr>
  <td style="white-space:nowrap">{{fmtDate .StartedAt}}</td>
  <td>
    <span style="color:{{if eq .Status "success"}}#22c55e{{else if eq .Status "error"}}#ef4444{{else if eq .Status "timeout"}}#f97316{{else}}#94a3b8{{end}}">
      {{.Status}}
    </span>
  </td>
  <td>{{.DurationMs}} мс</td>
  <td style="max-width:400px;white-space:pre-wrap;font-size:12px;color:#475569">{{.Output}}</td>
  <td style="max-width:300px;white-space:pre-wrap;font-size:12px;color:#ef4444">{{.Error}}</td>
</tr>
{{end}}
</tbody></table>
{{else}}
<p class="empty">Запусков ещё не было</p>
{{end}}
</div>
</main></div></body></html>
{{end}}
`

const tplAccountReg = `
{{define "page-accounts"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div class="row-top">
  <h2>{{.Chart.Title}}</h2>
  <span style="color:#94a3b8;font-size:13px">{{len .Rows}} счетов</span>
</div>
<div class="card">
{{if .Rows}}
<table><thead><tr>
  <th style="width:120px">Код</th>
  <th>Наименование</th>
  <th style="width:140px">Вид</th>
  <th style="width:80px">Родитель</th>
</tr></thead>
<tbody>
{{range .Rows}}
<tr>
  <td><code>{{index . "code"}}</code></td>
  <td>{{index . "name"}}</td>
  <td style="color:#64748b;font-size:13px">
    {{if eq (str (index . "kind")) "active"}}активный
    {{else if eq (str (index . "kind")) "passive"}}пассивный
    {{else}}активно-пассивный{{end}}
  </td>
  <td style="color:#94a3b8;font-size:12px">{{index . "parent"}}</td>
</tr>
{{end}}
</tbody></table>
{{else}}
<p class="empty">Счетов нет</p>
{{end}}
</div>
</main></div></body></html>
{{end}}

{{define "page-accountreg-movements"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div class="row-top">
  <h2>{{.Register.Title}} — Проводки</h2>
  <a class="btn btn-secondary" href="/ui/accountreg/{{lower .Register.Name}}/balances">Остатки</a>
</div>
<div class="card">
{{if .Rows}}
<table><thead><tr>
  <th>Период</th>
  <th>Дт</th>
  <th>Кт</th>
  {{range .Register.Resources}}<th>{{.Name}}</th>{{end}}
  <th>Регистратор</th>
</tr></thead>
<tbody>
{{range .Rows}}
<tr>
  <td style="white-space:nowrap">{{fmtDate (index . "period")}}</td>
  <td><code>{{index . "счётдт"}}</code></td>
  <td><code>{{index . "счёткт"}}</code></td>
  {{range $.Register.Resources}}<td>{{str (index $ (lower .Name))}}</td>{{end}}
  <td style="color:#94a3b8;font-size:12px">{{index . "регистратор"}}</td>
</tr>
{{end}}
</tbody></table>
{{else}}
<p class="empty">Проводок нет</p>
{{end}}
</div>
</main></div></body></html>
{{end}}

{{define "page-accountreg-balances"}}
{{template "head" .}}{{template "nav" .}}
<main>
<div class="row-top">
  <h2>{{.Register.Title}} — Остатки по счетам</h2>
  <div style="display:flex;gap:8px;align-items:center">
    <form method="GET" style="display:flex;gap:8px;align-items:center">
      <label style="color:#64748b;font-size:13px">На дату:</label>
      <input type="date" name="date" value="{{.AsOf}}" style="padding:4px 8px;border:1px solid #e2e8f0;border-radius:4px">
      <button class="btn btn-primary btn-sm" type="submit">Применить</button>
    </form>
    <a class="btn btn-secondary" href="/ui/accountreg/{{lower .Register.Name}}">Проводки</a>
  </div>
</div>
<div class="card">
{{if .Rows}}
<table><thead><tr>
  <th style="width:100px">Счёт</th>
  <th>Наименование</th>
  {{range .Register.Resources}}
  <th style="text-align:right">{{.Name}} Дт</th>
  <th style="text-align:right">{{.Name}} Кт</th>
  <th style="text-align:right">Сальдо</th>
  {{end}}
</tr></thead>
<tbody>
{{range .Rows}}
{{$row := .}}
<tr>
  <td><code>{{index . "code"}}</code></td>
  <td>{{index . "name"}}</td>
  {{range $.Register.Resources}}
  {{$col := lower .Name}}
  <td style="text-align:right;font-family:monospace">{{str (index $row (print $col "_дт"))}}</td>
  <td style="text-align:right;font-family:monospace">{{str (index $row (print $col "_кт"))}}</td>
  <td style="text-align:right;font-family:monospace;font-weight:600">{{str (index $row $col)}}</td>
  {{end}}
</tr>
{{end}}
</tbody></table>
{{else}}
<p class="empty">Нет движений на выбранную дату</p>
{{end}}
</div>
</main></div></body></html>
{{end}}
`
