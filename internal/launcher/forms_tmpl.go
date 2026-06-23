package launcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"

	"github.com/ivantit66/onebase/internal/metadata"
)

// formsTmpl — отдельный набор шаблонов для UI управляемых форм (план 37, этап 4).
// Не подмешан в cfgTmpl чтобы не раздувать огромный configurator_tmpl.go и
// не плодить конфликты define с другими страницами конфигуратора.
//
// Renders:
//   - "forms-editor" — страница split-pane Monaco + live preview
//   - "forms-list"   — список managed-форм проекта (минимальный)
var formsTmpl = template.Must(template.New("forms").Funcs(template.FuncMap{
	"esc": func(s string) string { return html.EscapeString(s) },
	// jsString — встраивание произвольной строки как JS-литерала через
	// json.Marshal. Возвращает с обрамляющими кавычками: `"...escaped..."`.
	// Корректно работает с кириллицей, переносами строк, кавычками,
	// бэкслешами — пригоден для прямой подстановки в JS-выражение без
	// дополнительных манипуляций (replace-цепочки и т.п.).
	// Возвращаемое значение помечается template.JS, чтобы html/template
	// не применил автоматический JS-escape поверх готового литерала.
	"jsString": func(s string) template.JS {
		b, _ := json.Marshal(s)
		return template.JS(b)
	},
}).Parse(tplFormsBase + tplFormsList + tplFormsEditor))

// renderFormsEditor — рендер страницы редактора одной формы.
func renderFormsEditor(w http.ResponseWriter, data *configuratorData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := formsTmpl.ExecuteTemplate(w, "forms-editor", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

// renderFormsList — рендер страницы со списком форм.
func renderFormsList(w http.ResponseWriter, data *configuratorData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := formsTmpl.ExecuteTemplate(w, "forms-list", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

const tplFormsBase = `
{{define "forms-head"}}
<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<title>Управляемые формы — {{.Base.Name}}</title>
<style>
* {box-sizing:border-box}
body{margin:0;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,sans-serif;font-size:13px;color:#334;background:#f4f6fb}
header{background:linear-gradient(135deg,#1a4a80,#2d6cb3);color:#fff;padding:10px 18px;display:flex;align-items:center;gap:14px;box-shadow:0 1px 4px rgba(0,0,0,.1)}
header h1{margin:0;font-size:14px;font-weight:600}
header a{color:#cfe2ff;text-decoration:none;font-size:12px}
header a:hover{color:#fff}
.crumbs{margin-left:auto;font-size:12px;color:#cfe2ff}
.crumbs a{margin-right:6px}
main{padding:18px;max-width:1600px;margin:0 auto}
.panel{background:#fff;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.06);padding:14px 18px;margin-bottom:14px}
.panel h2{margin:0 0 10px;font-size:14px;color:#1a4a80}
.btn{display:inline-block;padding:6px 12px;border-radius:5px;font-size:12px;border:1px solid #d0d7e3;background:#fff;color:#334;cursor:pointer;text-decoration:none;margin-right:4px}
.btn:hover{background:#f0f4ff;border-color:#1a4a80}
.btn-primary{background:#1a4a80;color:#fff;border-color:#1a4a80}
.btn-primary:hover{background:#2d6cb3;color:#fff}
.btn-danger{background:#dc2626;color:#fff;border-color:#dc2626}
.btn-danger:hover{background:#ef4444}
.btn-success{background:#16a34a;color:#fff;border-color:#16a34a}
.btn-success:hover{background:#22c55e}
table{width:100%;border-collapse:collapse;font-size:13px}
table th,table td{padding:8px 12px;text-align:left;border-bottom:1px solid #eef0f5}
table th{background:#f8fafc;font-weight:600;color:#475569;font-size:12px}
table tr:hover{background:#f4f6fb}
.empty{padding:24px;text-align:center;color:#94a3b8;font-size:13px}
.tag{display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:500;margin-left:6px}
.tag-managed{background:#d1fae5;color:#059669}
.tag-autogen{background:#e0e7ff;color:#6366f1}
.flash-ok{background:#d1fae5;color:#059669;padding:8px 14px;border-radius:6px;margin-bottom:12px;font-size:13px}
.flash-err{background:#fee2e2;color:#dc2626;padding:8px 14px;border-radius:6px;margin-bottom:12px;font-size:13px}
</style>
{{end}}

{{define "forms-header"}}
<header>
  <h1>◇ Управляемые формы</h1>
  <a href="/bases/{{.Base.ID}}/configurator">← В конфигуратор</a>
  <span class="crumbs">
    <a href="/bases/{{.Base.ID}}/configurator/forms">Все формы</a>
    {{if .EditingForm}}/ <a href="/bases/{{.Base.ID}}/configurator/forms/edit?entity={{.EditingForm.Entity}}&name={{.EditingForm.Name}}">{{.EditingForm.Entity}}.{{.EditingForm.Name}}</a>{{end}}
  </span>
</header>
{{end}}
`

const tplFormsList = `
{{define "forms-list"}}
{{template "forms-head" .}}
<body>
{{template "forms-header" .}}
<main>

{{if .Error}}<div class="flash-err">{{.Error}}</div>{{end}}
{{if .FieldsSaved}}<div class="flash-ok">✓ Сохранено: {{.FieldsSavedEntity}}</div>{{end}}

<div class="panel">
  <h2>Все управляемые формы проекта</h2>
  <p style="color:#64748b;font-size:12px;margin-top:0">
    Управляемые формы (◇) описаны декларативно в YAML и переопределяют авто-генерируемые формы.
    Без YAML — каждая сущность рендерится по полям метаданных. Опциональность сохраняется:
    у одной сущности может быть авто-форма, у другой — managed, у третьей — обе (managed имеет приоритет).
  </p>
  {{if .ManagedForms}}
  <table>
    <thead><tr><th>Сущность</th><th>Форма</th><th>Тип</th><th>Модуль</th><th></th></tr></thead>
    <tbody>
    {{range .ManagedForms}}
    <tr>
      <td><b>{{.Entity}}</b></td>
      <td>{{.Name}} <span class="tag tag-managed">◇ managed</span></td>
      <td>{{if .Kind}}{{.Kind}}{{else}}—{{end}}</td>
      <td>{{if .HasOS}}есть{{else}}—{{end}}</td>
      <td style="text-align:right">
        <a class="btn" href="/bases/{{$.Base.ID}}/configurator/forms/edit?entity={{.Entity}}&name={{.Name}}">Редактировать</a>
      </td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <div class="empty">
    <p>Управляемых форм ещё нет.</p>
    <p style="font-size:12px">Создайте форму вручную или импортируйте из 1С.</p>
  </div>
  {{end}}
</div>

<div class="panel">
  <h2>Создать форму</h2>
  <form action="/bases/{{.Base.ID}}/configurator/forms/edit" method="GET" style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
    <label>Сущность: <input type="text" name="entity" placeholder="Контрагент" required style="padding:6px 10px;border:1px solid #d0d7e3;border-radius:5px;font-size:13px"></label>
    <label>Имя формы: <input type="text" name="name" placeholder="ФормаОбъекта" required style="padding:6px 10px;border:1px solid #d0d7e3;border-radius:5px;font-size:13px"></label>
    <button type="submit" class="btn btn-primary">Создать</button>
  </form>
</div>

<div class="panel">
  <h2>Импорт из 1С</h2>
  <p style="color:#64748b;font-size:12px;margin-top:0">
    Загрузите Form.xml + Module.bsl (опционально). Архив ZIP со всей формой 1С тоже подойдёт.
    После импорта получите .form.yaml + .form.os + _resources/ с предупреждениями BSL.
  </p>
  <form action="/bases/{{.Base.ID}}/configurator/forms/import-1c" method="POST" enctype="multipart/form-data" style="display:grid;gap:8px;max-width:520px">
    <label>Сущность OneBase: <input type="text" name="entity" required style="padding:6px 10px;border:1px solid #d0d7e3;border-radius:5px;width:100%"></label>
    <label>Имя формы: <input type="text" name="name" value="Форма" style="padding:6px 10px;border:1px solid #d0d7e3;border-radius:5px;width:100%"></label>
    <label>ZIP с формой 1С (или Form.xml внутри): <input type="file" name="zip" accept=".zip"></label>
    <label>либо отдельные файлы:</label>
    <label>Form.xml: <input type="file" name="form_xml" accept=".xml"></label>
    <label>Module.bsl: <input type="file" name="module_bsl" accept=".bsl"></label>
    <button type="submit" class="btn btn-primary">Импортировать</button>
  </form>
</div>

</main>
</body>
</html>
{{end}}
`

const tplFormsEditor = `
{{define "forms-editor"}}
{{template "forms-head" .}}
<style>
.editor-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;height:calc(100vh - 230px);min-height:480px}
.editor-pane{display:flex;flex-direction:column;background:#fff;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.06);overflow:hidden}
.editor-pane-hd{padding:8px 12px;background:#f8fafc;font-size:12px;font-weight:600;color:#475569;border-bottom:1px solid #eef0f5;display:flex;justify-content:space-between;align-items:center}
.editor-pane-body{flex:1;overflow:hidden;display:flex;flex-direction:column}
#yaml-editor,#os-editor{flex:1;min-height:300px}
#preview-frame{flex:1;border:none;background:#fff}
.editor-tools{padding:8px 12px;background:#fff;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.06);margin-bottom:10px;display:flex;gap:6px;flex-wrap:wrap;align-items:center}
.editor-meta{margin-left:auto;color:#64748b;font-size:12px}
.warn-panel{background:#fff;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.06);padding:10px 14px;margin-top:10px;max-height:220px;overflow-y:auto;font-size:12px;display:none}
.warn-panel.active{display:block}
.warn-item{padding:4px 0;border-bottom:1px solid #eef0f5}
.warn-item.error{color:#dc2626}
.warn-item.warn{color:#d97706}
.warn-item.info{color:#64748b}
.editor-tabs{display:flex;background:#f8fafc;border-bottom:1px solid #eef0f5}
.editor-tab{padding:8px 14px;cursor:pointer;font-size:12px;border-bottom:2px solid transparent;color:#64748b}
.editor-tab.active{color:#1a4a80;border-bottom-color:#1a4a80;background:#fff;font-weight:600}
/* Палитра реквизитов объекта — перетаскивание/клик вставляет поле (issue #134) */
.attr-palette{background:#fff;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.06);padding:8px 12px;margin-bottom:10px;display:flex;gap:6px;flex-wrap:wrap;align-items:center}
.attr-palette-label{font-size:12px;color:#64748b;margin-right:4px}
.attr-chip{display:inline-flex;align-items:center;background:#eef4ff;border:1px solid #c7d8f5;border-radius:14px;padding:3px 10px;font-size:12px;color:#1a4a80;cursor:grab;user-select:none}
.attr-chip:hover{background:#dce8ff;border-color:#9cbef0}
.attr-chip:active{cursor:grabbing}
.attr-chip.dragging{opacity:.4}
#yaml-editor.attr-drop-target{outline:2px dashed #1a4a80;outline-offset:-2px}
/* Визуальный конструктор форм (#164): холст, drop-зоны, панель свойств */
.rp-tabs{display:flex;gap:2px}
.rp-tab{padding:4px 10px;font-size:12px;color:#64748b;cursor:pointer;border-radius:5px}
.rp-tab.active{color:#1a4a80;background:#eef4ff;font-weight:600}
#design-wrap{flex:1;display:flex;flex-direction:column;overflow:hidden}
#canvas-host{flex:1;overflow:auto;padding:12px;background:#fff}
.fc-canvas{font-size:13px;color:#334}
.fc-children{display:flex;flex-direction:column;gap:1px;min-height:6px}
.fc-drop{height:6px;border-radius:4px;transition:background .1s,height .1s}
.fc-drop.fc-drop-over{background:#1a4a80;height:14px}
.fc-el{border:1px solid transparent;border-radius:6px;padding:3px 5px;cursor:pointer}
.fc-el.fc-selected{outline:2px solid #1a4a80;background:#eef4ff}
.fc-pick:hover{background:#f5f8ff}
.fc-group{border:1px solid #e2e8f0;padding:5px 9px;margin:1px 0}
.fc-group>legend{font-weight:600;color:#475569;padding:0 5px;font-size:12px}
.fc-pages{border:1px dashed #c7d8f5;border-radius:6px;padding:4px}
.fc-page{border:1px solid #eef0f5;border-radius:6px;margin:3px 0;padding:4px 6px}
.fc-tab{font-size:11px;color:#1a4a80;font-weight:600;margin-bottom:3px}
.fc-field label{display:block;color:#475569;font-size:12px;margin-bottom:2px}
.fc-field input{width:100%;padding:5px 8px;border:1px solid #d0d7e3;border-radius:5px;background:#f8fafc;pointer-events:none}
.fc-req{color:#dc2626}
.fc-check{display:flex;align-items:center;gap:6px}
.fc-label{color:#475569}
.fc-btn button{padding:5px 10px;border:1px solid #d0d7e3;border-radius:5px;background:#f8fafc;pointer-events:none}
.fc-table .fc-tp{background:#fef9c3;color:#92400e;padding:6px 8px;border-radius:6px;font-size:12px}
.fc-unknown{background:#fef2f2;color:#991b1b;font-size:12px}
.fc-kind{color:#94a3b8;font-size:11px}
#canvas-host.fc-canvas-disabled{opacity:.5;pointer-events:none}
.fc-banner{background:#fee2e2;color:#dc2626;padding:6px 10px;border-radius:6px;font-size:12px;margin-bottom:8px;display:none}
.fc-banner.active{display:block}
.prop-panel{border-top:1px solid #eef0f5;background:#fafbff;max-height:44%;overflow:auto;padding:10px 12px;font-size:12px}
.prop-panel .prop-empty{color:#94a3b8}
.prop-panel h4{margin:0 0 8px;font-size:12px;color:#1a4a80}
.prop-panel h4 .prop-kind{color:#94a3b8;font-weight:400;margin-left:6px}
.prop-row{margin-bottom:8px}
.prop-row>label{display:block;color:#64748b;margin-bottom:2px}
.prop-row input[type=text]{width:100%;padding:5px 8px;border:1px solid #d0d7e3;border-radius:5px;font-size:12px}
.prop-row.prop-check{display:flex;align-items:center;gap:6px}
.prop-row.prop-check>label{margin:0}
</style>
<body>
{{template "forms-header" .}}
<main>

{{if .Error}}<div class="flash-err">{{.Error}}</div>{{end}}
{{if .FieldsSaved}}<div class="flash-ok">✓ Сохранено: {{.FieldsSavedEntity}}</div>{{end}}

<form id="save-form" action="/bases/{{.Base.ID}}/configurator/forms/save" method="POST">
<input type="hidden" name="entity" value="{{.EditingForm.Entity}}">
<input type="hidden" name="name" value="{{.EditingForm.Name}}">
<input type="hidden" name="yaml" id="yaml-hidden">
<input type="hidden" name="os" id="os-hidden">
</form>

<div class="editor-tools">
  <button class="btn btn-primary" onclick="saveForm()">Сохранить</button>
  <button class="btn btn-success" onclick="refreshPreview()">Просмотр</button>
  <button class="btn" onclick="validateForm()">Проверить</button>
  <label style="display:inline-flex;align-items:center;gap:5px;margin-left:6px;font-size:12px;color:#475569;cursor:pointer"
         title="Включает SlickGrid (Excel-навигация с клавиатуры, выравнивание чисел) для всех табличных частей формы. В YAML проставляется use_grid: true.">
    <input type="checkbox" id="grid-toggle" onchange="setGridFlag(this.checked)"> Табличные части: SlickGrid
  </label>
  <form action="/bases/{{.Base.ID}}/configurator/forms/delete" method="POST" style="display:inline" onsubmit="return confirm('Удалить эту форму вместе с модулем и ресурсами?')">
    <input type="hidden" name="entity" value="{{.EditingForm.Entity}}">
    <input type="hidden" name="name" value="{{.EditingForm.Name}}">
    <button class="btn btn-danger" type="submit">Удалить</button>
  </form>
  <span class="editor-meta">{{.EditingForm.Entity}}.{{.EditingForm.Name}}{{if .EditingForm.Kind}} · {{.EditingForm.Kind}}{{end}}</span>
</div>

{{if .EditingFormAttrs}}
<div class="attr-palette" id="attr-palette">
  <span class="attr-palette-label">Реквизиты объекта (клик или перетащите в YAML, чтобы добавить поле):</span>
  {{range .EditingFormAttrs}}
  <span class="attr-chip" draggable="true" data-attr="{{.Name}}" data-title="{{if .Title}}{{.Title}}{{else}}{{.Name}}{{end}}" onclick="insertFieldFromChip(this)" title="Вставить поле для «{{.Name}}»">{{.Name}}</span>
  {{end}}
</div>
{{end}}

<div class="editor-grid">
  <div class="editor-pane">
    <div class="editor-pane-hd">
      YAML
      <span style="color:#94a3b8;font-weight:400">{{.EditingForm.YAMLPath}}</span>
    </div>
    <div class="editor-tabs">
      <div class="editor-tab active" data-tab="yaml" onclick="switchTab('yaml')">YAML</div>
      <div class="editor-tab" data-tab="os" onclick="switchTab('os')">Модуль (.form.os)</div>
    </div>
    <div class="editor-pane-body">
      <div id="yaml-editor"></div>
      <div id="os-editor" style="display:none"></div>
    </div>
  </div>
  <div class="editor-pane">
    <div class="editor-pane-hd">
      <div class="rp-tabs">
        <span class="rp-tab active" data-rp="design" onclick="switchRightPane('design')">Конструктор</span>
        <span class="rp-tab" data-rp="preview" onclick="switchRightPane('preview')">Просмотр</span>
      </div>
      <button class="btn" onclick="reloadCanvas()" style="padding:3px 8px;font-size:11px">Обновить</button>
    </div>
    <div class="editor-pane-body">
      <div id="design-wrap">
        <div id="canvas-host">
          <div class="fc-banner" id="fc-banner"></div>
          <div class="empty" style="padding:18px">Загрузка холста…</div>
        </div>
        <div class="prop-panel" id="prop-panel">
          <div class="prop-empty">Выберите элемент на холсте, чтобы изменить его свойства. Перетащите реквизит из палитры на холст, чтобы добавить поле.</div>
        </div>
      </div>
      <iframe id="preview-frame" sandbox="allow-same-origin allow-scripts" style="display:none;flex:1;border:none"></iframe>
    </div>
  </div>
</div>

<div id="warn-panel" class="warn-panel">
  <div style="display:flex;justify-content:space-between;margin-bottom:6px">
    <b>Результат проверки</b>
    <a href="javascript:void(0)" onclick="document.getElementById('warn-panel').classList.remove('active')" style="color:#64748b;text-decoration:none">×</a>
  </div>
  <div id="warn-items"></div>
</div>

<script>
// Самохостинг Monaco: web-воркер из встроенного /vendor/monaco/ (тот же origin).
window.MonacoEnvironment = { getWorkerUrl: function () {
  return 'data:text/javascript;charset=utf-8,' + encodeURIComponent(
    "self.MonacoEnvironment={baseUrl:'" + location.origin + "/vendor/monaco/'};" +
    "importScripts('" + location.origin + "/vendor/monaco/vs/base/worker/workerMain.js');");
}};
var _initialYAML = {{jsString .EditingForm.YAML}};
var _initialOS   = {{jsString .EditingForm.OS}};

function buildFallback() {
  // Monaco не загрузился — деградируем в textarea, чтобы форма всё равно
  // редактировалась и сохранялась (в т.ч. полностью офлайн).
  function ta(host, val) {
    var t = document.createElement('textarea');
    t.value = val;
    t.style.cssText = 'width:100%;height:100%;border:0;outline:0;resize:none;font-family:Consolas,monospace;font-size:12px;padding:8px;box-sizing:border-box';
    var h = document.getElementById(host);
    h.innerHTML = ''; h.appendChild(t);
    return t;
  }
  window._yamlTA = ta('yaml-editor', _initialYAML);
  window._osTA   = ta('os-editor', _initialOS);
  refreshPreview();
}

if (typeof require === 'undefined') {
  buildFallback();
} else {
  require.config({ paths: { vs: '/vendor/monaco/vs' }});
  require(['vs/editor/editor.main'], function () {
    window.yamlEditor = monaco.editor.create(document.getElementById('yaml-editor'), {
      value: _initialYAML,
      language: 'yaml', theme: 'vs-light', automaticLayout: true, minimap: { enabled: false }, fontSize: 12
    });
    window.osEditor = monaco.editor.create(document.getElementById('os-editor'), {
      value: _initialOS,
      language: 'plaintext', theme: 'vs-light', automaticLayout: true, minimap: { enabled: false }, fontSize: 12
    });
    refreshPreview();
    hookYamlChange();
    reloadCanvas();
  });
}

// Единые геттеры/сеттеры — прозрачно работают и с Monaco, и с textarea-fallback.
function getYAML() { return window.yamlEditor ? window.yamlEditor.getValue() : (window._yamlTA ? window._yamlTA.value : ''); }
function getOS()   { return window.osEditor ? window.osEditor.getValue() : (window._osTA ? window._osTA.value : ''); }
function setYAML(v) { if (window.yamlEditor) window.yamlEditor.setValue(v); else if (window._yamlTA) window._yamlTA.value = v; }

// ── Палитра реквизитов: вставка поля ПолеВвода по клику/дропу (issue #134) ──
function _attrFieldSnippet(attr, title, base) {
  var t = String(title || attr).replace(/\\/g, '\\\\').replace(/"/g, '\\"');
  var b = base || '      ';
  return b + '- kind: ПолеВвода\n' +
         b + '  name: Поле' + attr + '\n' +
         b + '  title:\n' +
         b + '    ru: "' + t + '"\n' +
         b + '  data_path: Объект.' + attr;
}
// Куда и с каким отступом вставлять новый элемент списка формы (issue #134).
// Раньше отступ копировался со строки под курсором, а вставка шла сразу после
// неё — поэтому дроп не на строку '- ' давал кривой отступ, а дроп в середину
// элемента разрывал его → невалидный YAML («mapping values are not allowed»).
// Теперь: отступ = как у ближайшего элемента списка ('- ') на/выше курсора, а
// вставка — ПОСЛЕ конца этого элемента (перед следующим '- ' или дедентом).
function _yamlInsertPoint() {
  var fb = { indent: '      ', afterLine: null };
  if (!window.yamlEditor) return fb;
  var model = window.yamlEditor.getModel();
  var pos = window.yamlEditor.getPosition();
  var total = model.getLineCount();
  var startLine = 0, indent = null;
  for (var ln = pos.lineNumber; ln >= 1; ln--) {
    var t = model.getLineContent(ln);
    var mi = t.match(/^(\s*)-\s/);
    if (mi) { startLine = ln; indent = mi[1]; break; }
    var mh = t.match(/^(\s*)(elements|children|groups|fields)\s*:\s*$/);
    if (mh) { return { indent: mh[1] + '  ', afterLine: ln }; }
  }
  if (startLine === 0) {
    for (var dn = pos.lineNumber; dn <= total; dn++) {
      var td = model.getLineContent(dn);
      var mhd = td.match(/^(\s*)(elements|children|groups|fields)\s*:\s*$/);
      if (mhd) { return { indent: mhd[1] + '  ', afterLine: dn }; }
    }
    return fb;
  }
  var endLine = total;
  for (var k = startLine + 1; k <= total; k++) {
    var s = model.getLineContent(k);
    if (!s.trim()) continue;
    var lead = (s.match(/^\s*/) || [''])[0].length;
    if (lead <= indent.length) { endLine = k - 1; break; }
  }
  return { indent: indent, afterLine: endLine };
}
function insertFieldText(attr, title) {
  var ip = _yamlInsertPoint();
  var snippet = _attrFieldSnippet(attr, title, ip.indent);
  if (window.yamlEditor) {
    var ed = window.yamlEditor, model = ed.getModel();
    var line = ip.afterLine != null ? ip.afterLine : ed.getPosition().lineNumber;
    var col = model.getLineMaxColumn(line);
    ed.executeEdits('insert-field', [{
      range: new monaco.Range(line, col, line, col),
      text: '\n' + snippet, forceMoveMarkers: true
    }]);
    ed.setPosition({ lineNumber: line + 1, column: model.getLineMaxColumn(line + 1) });
    ed.focus();
  } else if (window._yamlTA) {
    var ta = window._yamlTA, p = ta.selectionStart != null ? ta.selectionStart : ta.value.length;
    ta.value = ta.value.slice(0, p) + '\n' + snippet + ta.value.slice(p);
  }
  if (typeof refreshPreview === 'function') refreshPreview();
}
function insertFieldFromChip(chip) {
  insertFieldText(chip.getAttribute('data-attr'), chip.getAttribute('data-title'));
}
(function () {
  var pal = document.getElementById('attr-palette');
  if (!pal) return;
  pal.addEventListener('dragstart', function (e) {
    var chip = e.target.closest ? e.target.closest('.attr-chip') : null;
    if (!chip) return;
    chip.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'copy';
    e.dataTransfer.setData('text/onebase-attr',
      JSON.stringify({ attr: chip.getAttribute('data-attr'), title: chip.getAttribute('data-title') }));
  });
  pal.addEventListener('dragend', function (e) {
    var chip = e.target.closest ? e.target.closest('.attr-chip') : null;
    if (chip) chip.classList.remove('dragging');
  });
  var host = document.getElementById('yaml-editor');
  if (!host) return;
  host.addEventListener('dragover', function (e) {
    if ((e.dataTransfer.types || []).indexOf('text/onebase-attr') < 0) return;
    e.preventDefault(); e.dataTransfer.dropEffect = 'copy';
    host.classList.add('attr-drop-target');
  });
  host.addEventListener('dragleave', function () { host.classList.remove('attr-drop-target'); });
  host.addEventListener('drop', function (e) {
    var raw = e.dataTransfer.getData('text/onebase-attr');
    host.classList.remove('attr-drop-target');
    if (!raw) return;
    e.preventDefault();
    var d; try { d = JSON.parse(raw); } catch (_) { return; }
    if (window.yamlEditor && window.yamlEditor.getTargetAtClientPoint) {
      var tgt = window.yamlEditor.getTargetAtClientPoint(e.clientX, e.clientY);
      if (tgt && tgt.position) window.yamlEditor.setPosition(tgt.position);
    }
    insertFieldText(d.attr, d.title);
  });
})();

// setGridFlag — переключатель SlickGrid (план 48). Проставляет/снимает
// use_grid: true у всех элементов kind: ТабличнаяЧасть в YAML формы.
// Делаем построчно, без YAML-парсера: сначала убираем все строки use_grid,
// затем при включении вставляем флаг сразу после строки "- kind: ТабличнаяЧасть"
// с тем же отступом, что у kind. Идемпотентно. Ограничение: kind должен быть
// первым ключом элемента списка (как во всех примерах).
function setGridFlag(enable) {
  var lines = getYAML().split('\n');
  var out = [];
  for (var i = 0; i < lines.length; i++) {
    var line = lines[i];
    if (/^\s*use_grid\s*:/.test(line)) continue; // снять существующие
    out.push(line);
    var m = line.match(/^(\s*)-\s+kind\s*:\s*(?:ТабличнаяЧасть|TablePart)\s*$/);
    if (enable && m) {
      var keyIndent = m[1].length + 2; // ключи-братья выровнены по kind (после "- ")
      out.push(new Array(keyIndent + 1).join(' ') + 'use_grid: true');
    }
  }
  setYAML(out.join('\n'));
  refreshPreview();
}

// syncGridToggle — отражает текущее состояние YAML в чекбоксе (отмечен, если
// хоть одна ТЧ уже с use_grid: true). Вызывается после рендера превью.
function syncGridToggle() {
  var cb = document.getElementById('grid-toggle');
  if (!cb) return;
  cb.checked = /^\s*use_grid\s*:\s*true\s*$/m.test(getYAML());
}

function switchTab(name) {
  document.querySelectorAll('.editor-tab').forEach(function (el) { el.classList.toggle('active', el.dataset.tab === name); });
  document.getElementById('yaml-editor').style.display = name === 'yaml' ? '' : 'none';
  document.getElementById('os-editor').style.display = name === 'os' ? '' : 'none';
  if (window.yamlEditor) window.yamlEditor.layout();
  if (window.osEditor) window.osEditor.layout();
}

function saveForm() {
  document.getElementById('yaml-hidden').value = getYAML();
  document.getElementById('os-hidden').value = getOS();
  document.getElementById('save-form').submit();
}

function refreshPreview() {
  syncGridToggle();
  var body = new URLSearchParams();
  body.append('yaml', getYAML());
  body.append('entity', '{{.EditingForm.Entity}}');
  fetch('/bases/{{.Base.ID}}/configurator/forms/preview', { method: 'POST', body: body, headers: { 'Content-Type': 'application/x-www-form-urlencoded' }})
    .then(function (r) { return r.text(); })
    .then(function (html) {
      document.getElementById('preview-frame').srcdoc = html;
    });
}

function validateForm() {
  if (!window.yamlEditor) return;
  var body = new URLSearchParams();
  body.append('yaml', window.yamlEditor.getValue());
  body.append('entity', '{{.EditingForm.Entity}}');
  fetch('/bases/{{.Base.ID}}/configurator/forms/validate', { method: 'POST', body: body, headers: { 'Content-Type': 'application/x-www-form-urlencoded' }})
    .then(function (r) { return r.json(); })
    .then(function (resp) {
      var panel = document.getElementById('warn-panel');
      var items = document.getElementById('warn-items');
      items.innerHTML = '';
      panel.classList.add('active');
      if (resp.ok && (!resp.items || resp.items.length === 0)) {
        items.innerHTML = '<div class="warn-item info">✓ YAML валиден, замечаний нет.</div>';
        return;
      }
      (resp.items || []).forEach(function (it) {
        var div = document.createElement('div');
        div.className = 'warn-item ' + (it.severity || 'info');
        div.textContent = (it.code ? '[' + it.code + '] ' : '') + it.message;
        items.appendChild(div);
      });
    })
    .catch(function (e) {
      var panel = document.getElementById('warn-panel');
      panel.classList.add('active');
      document.getElementById('warn-items').innerHTML = '<div class="warn-item error">Ошибка проверки: ' + e + '</div>';
    });
}

// ── Визуальный конструктор формы (#164) ──────────────────────────────────────
// Холст серверо-центричен: правка превращается в команду на /forms/edit-op,
// сервер хирургически правит дерево yaml.Node и возвращает {yaml, canvasHtml,
// selectedId, model}. Monaco и холст синхронизируются от одного ответа.
var _editOpURL = '/bases/{{.Base.ID}}/configurator/forms/edit-op';
var _entity = {{jsString .EditingForm.Entity}};
var _selected = '';   // node-id выбранного элемента
var _model = {};      // node-id → свойства (для панели свойств)
var _rightPane = 'design';
var _syncing = false; // защита от рекурсии setYAML → reloadCanvas

function switchRightPane(which) {
  _rightPane = which;
  document.querySelectorAll('.rp-tab').forEach(function (t) { t.classList.toggle('active', t.dataset.rp === which); });
  document.getElementById('design-wrap').style.display = which === 'design' ? 'flex' : 'none';
  document.getElementById('preview-frame').style.display = which === 'preview' ? 'block' : 'none';
  if (which === 'design') reloadCanvas(); else refreshPreview();
}

// editOp — единая точка общения с сервером. mutating=true → результат пишем
// обратно в YAML (направление холст→YAML).
function editOp(params, mutating) {
  var body = new URLSearchParams();
  body.append('yaml', getYAML());
  body.append('entity', _entity);
  Object.keys(params).forEach(function (k) { if (params[k] != null) body.append(k, params[k]); });
  return fetch(_editOpURL, { method: 'POST', body: body, headers: { 'Content-Type': 'application/x-www-form-urlencoded' }})
    .then(function (r) { return r.json(); })
    .then(function (resp) {
      var banner = document.getElementById('fc-banner');
      var host = document.getElementById('canvas-host');
      if (!resp.ok) {
        banner.textContent = 'YAML не разобран — визуальные правки заблокированы: ' + (resp.errors || []).join('; ');
        banner.classList.add('active');
        host.classList.add('fc-canvas-disabled');
        return resp;
      }
      banner.classList.remove('active');
      host.classList.remove('fc-canvas-disabled');
      _model = resp.model || {};
      _selected = resp.selectedId || '';
      renderCanvasHTML(resp.canvasHtml || '');
      if (mutating && typeof resp.yaml === 'string') {
        _syncing = true; setYAML(resp.yaml); _syncing = false;
      }
      renderProps();
      return resp;
    })
    .catch(function (e) {
      var banner = document.getElementById('fc-banner');
      banner.textContent = 'Ошибка конструктора: ' + e;
      banner.classList.add('active');
    });
}

// reloadCanvas — перерисовать холст из текущего YAML (направление YAML→холст).
function reloadCanvas() {
  if (_rightPane !== 'design') return Promise.resolve();
  return editOp({ op: 'render', node: _selected }, false);
}

function renderCanvasHTML(html) {
  var host = document.getElementById('canvas-host');
  var banner = document.getElementById('fc-banner');
  host.innerHTML = '';
  host.appendChild(banner);
  var wrap = document.createElement('div');
  wrap.innerHTML = html;
  while (wrap.firstChild) host.appendChild(wrap.firstChild);
}

// Делегирование на холсте: клик — выбор элемента; drop реквизита на зону — вставка.
(function () {
  var host = document.getElementById('canvas-host');
  if (!host) return;
  host.addEventListener('click', function (e) {
    var el = e.target.closest ? e.target.closest('[data-node-id]') : null;
    if (!el || !host.contains(el)) return;
    e.stopPropagation();
    selectNode(el.getAttribute('data-node-id'));
  });
  host.addEventListener('dragover', function (e) {
    var dz = e.target.closest ? e.target.closest('.fc-drop') : null;
    if (!dz || (e.dataTransfer.types || []).indexOf('text/onebase-attr') < 0) return;
    e.preventDefault(); e.dataTransfer.dropEffect = 'copy';
    dz.classList.add('fc-drop-over');
  });
  host.addEventListener('dragleave', function (e) {
    var dz = e.target.closest ? e.target.closest('.fc-drop') : null;
    if (dz) dz.classList.remove('fc-drop-over');
  });
  host.addEventListener('drop', function (e) {
    var dz = e.target.closest ? e.target.closest('.fc-drop') : null;
    if (!dz) return;
    dz.classList.remove('fc-drop-over');
    var raw = e.dataTransfer.getData('text/onebase-attr');
    if (!raw) return;
    e.preventDefault();
    var d; try { d = JSON.parse(raw); } catch (_) { return; }
    editOp({
      op: 'insert', parent: dz.getAttribute('data-parent'), index: dz.getAttribute('data-index'),
      kind: 'ПолеВвода', name: 'Поле' + d.attr, data_path: 'Объект.' + d.attr, title_ru: d.title || d.attr
    }, true);
  });
})();

function selectNode(nodeId) {
  _selected = nodeId;
  document.querySelectorAll('#canvas-host .fc-selected').forEach(function (el) { el.classList.remove('fc-selected'); });
  var el = document.querySelector('#canvas-host [data-node-id="' + nodeId + '"]');
  if (el) el.classList.add('fc-selected');
  renderProps();
}

// renderProps строит панель свойств выбранного элемента из _model. Обработчики
// вешаются через addEventListener (без inline-onchange — без проблем экранирования).
function renderProps() {
  var panel = document.getElementById('prop-panel');
  panel.innerHTML = '';
  var info = _model[_selected];
  if (!info) {
    var em = document.createElement('div'); em.className = 'prop-empty';
    em.textContent = 'Выберите элемент на холсте, чтобы изменить его свойства. Перетащите реквизит из палитры на холст, чтобы добавить поле.';
    panel.appendChild(em); return;
  }
  var h = document.createElement('h4');
  h.textContent = info.name || info.kind;
  var sk = document.createElement('span'); sk.className = 'prop-kind'; sk.textContent = info.kind;
  h.appendChild(sk); panel.appendChild(h);
  addTextProp(panel, 'Заголовок (ru)', 'title.ru', info.titleRu || '');
  addTextProp(panel, 'Имя', 'name', info.name || '');
  if (!info.container) {
    addTextProp(panel, 'Поле данных (data_path)', 'data_path', info.dataPath || '');
    addTextProp(panel, 'Подсказка', 'hint', info.hint || '');
    addCheckProp(panel, 'Обязательное', 'required', info.required);
    addCheckProp(panel, 'Только чтение', 'readonly', info.readonly);
  }
}
function addTextProp(panel, label, key, val) {
  var row = document.createElement('div'); row.className = 'prop-row';
  var l = document.createElement('label'); l.textContent = label; row.appendChild(l);
  var inp = document.createElement('input'); inp.type = 'text'; inp.value = val;
  inp.addEventListener('change', function () { setProp(key, inp.value); });
  row.appendChild(inp); panel.appendChild(row);
}
function addCheckProp(panel, label, key, checked) {
  var row = document.createElement('div'); row.className = 'prop-row prop-check';
  var inp = document.createElement('input'); inp.type = 'checkbox'; inp.checked = !!checked;
  inp.addEventListener('change', function () { setProp(key, inp.checked ? 'true' : ''); });
  var l = document.createElement('label'); l.textContent = label;
  row.appendChild(inp); row.appendChild(l); panel.appendChild(row);
}
function setProp(key, value) {
  if (!_selected) return;
  editOp({ op: 'setProp', node: _selected, key: key, value: value }, true);
}

// hookYamlChange — живая синхронизация YAML→холст (debounce), с защитой от
// рекурсии при программном setYAML из ответа edit-op.
var _yamlChangeTimer = null;
function hookYamlChange() {
  if (!window.yamlEditor || window._yamlHooked) return;
  window._yamlHooked = true;
  window.yamlEditor.onDidChangeModelContent(function () {
    if (_syncing) return;
    clearTimeout(_yamlChangeTimer);
    _yamlChangeTimer = setTimeout(reloadCanvas, 400);
  });
}

// Инициализация для textarea-fallback (Monaco инициализирует холст в своём
// callback). При вводе в textarea — тот же debounced reload.
if (window._yamlTA) {
  window._yamlTA.addEventListener('input', function () {
    if (_syncing) return;
    clearTimeout(_yamlChangeTimer);
    _yamlChangeTimer = setTimeout(reloadCanvas, 400);
  });
  reloadCanvas();
}
</script>

</main>
</body>
</html>
{{end}}
`

// ── Preview-рендер для iframe ─────────────────────────────────────────────────

// previewErrorHTML рендерит ошибку парсинга/валидации YAML в маленький HTML
// для srcdoc iframe. Не зависит от template — простая обёртка.
func previewErrorHTML(msg string) string {
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;padding:18px;font-family:sans-serif;background:#fef2f2;color:#991b1b}h3{margin:0 0 8px;font-size:14px}pre{background:#fff;padding:10px;border-radius:6px;border:1px solid #fee2e2;white-space:pre-wrap;font-size:12px}</style></head><body><h3>Ошибка YAML</h3><pre>%s</pre></body></html>`,
		html.EscapeString(msg))
}

// renderManagedFormPreview генерирует упрощённый HTML-предпросмотр
// дерева элементов формы. Не использует metadata.Entity — отрисовывает
// абстрактные input/checkbox/group на основе FormModule.Elements.
//
// Этого достаточно для UI-редактора чтобы оценить структуру формы;
// полноценный рендер с реальными данными доступен после сохранения
// через рантайм-handler /ui/.../form (этап 3).
func renderManagedFormPreview(fm *metadata.FormModule) string {
	var buf bytes.Buffer
	buf.WriteString(`<!doctype html><html><head><meta charset="utf-8"><style>
body{margin:0;padding:18px;font-family:-apple-system,sans-serif;background:#fff;color:#334;font-size:13px}
h2{margin:0 0 14px;color:#1a4a80;font-size:16px;display:flex;align-items:center;gap:8px}
.tag{font-size:11px;background:#d1fae5;color:#059669;padding:2px 8px;border-radius:10px}
fieldset{border:1px solid #e2e8f0;border-radius:8px;padding:12px 14px;margin-bottom:12px}
legend{font-weight:600;color:#475569;padding:0 6px;font-size:12px}
.tabs{margin-bottom:10px}
.tabs-hd{display:flex;border-bottom:2px solid #e2e8f0;margin-bottom:10px;gap:2px;flex-wrap:wrap}
.tab{padding:6px 12px;font-size:12px;color:#64748b;border-bottom:2px solid transparent;margin-bottom:-2px;cursor:pointer;user-select:none;background:none;border-left:none;border-right:none;border-top:none;font-family:inherit}
.tab:hover{color:#1a4a80;background:#f5f8ff}
.tab.active{color:#1a4a80;border-bottom-color:#1a4a80;font-weight:600;background:#fff}
.tab-page{display:none}
.tab-page.active{display:block}
.fg{margin-bottom:10px}
.fg label{display:block;color:#475569;margin-bottom:4px;font-size:12px}
.fg input,.fg select{width:100%;padding:6px 10px;border:1px solid #d0d7e3;border-radius:5px;font-size:13px;background:#fff}
.req{color:#dc2626}
.hint{display:block;color:#94a3b8;font-size:11px;margin-top:3px}
.deco{padding:6px 0;color:#475569;font-size:13px}
.btn{padding:6px 12px;border:1px solid #d0d7e3;background:#f8fafc;border-radius:5px;cursor:pointer;margin-right:4px;font-size:12px}
.tp-stub{background:#fef9c3;padding:8px;border-radius:6px;font-size:11px;color:#92400e;margin:6px 0}
.unknown{background:#fef2f2;padding:8px;border-radius:6px;font-size:11px;color:#991b1b;margin:6px 0}
</style></head><body>`)

	title := "Карточка"
	if fm.Title != nil && fm.Title["ru"] != "" {
		title = fm.Title["ru"]
	} else if fm.EntityName != "" {
		title = fm.EntityName
	}
	fmt.Fprintf(&buf, `<h2>%s <span class="tag">◇ managed</span></h2>`, html.EscapeString(title))

	tabsCounter := 0
	for _, el := range fm.Elements {
		renderPreviewElement(&buf, el, &tabsCounter)
	}

	// Inline-JS для переключения вкладок. Работает в iframe sandbox
	// allow-scripts; вложенные tabset-ы изолированы по data-tabset-id.
	buf.WriteString(`<script>
(function(){
  function activate(setId, idx){
    var hdr = document.querySelector('[data-tabset-hdr="'+setId+'"]');
    var body = document.querySelector('[data-tabset-body="'+setId+'"]');
    if(!hdr||!body) return;
    hdr.querySelectorAll('.tab').forEach(function(b,i){ b.classList.toggle('active', i===idx); });
    body.querySelectorAll(':scope > .tab-page').forEach(function(p,i){ p.classList.toggle('active', i===idx); });
  }
  document.querySelectorAll('.tab[data-tabset]').forEach(function(btn){
    btn.addEventListener('click', function(){
      activate(btn.dataset.tabset, parseInt(btn.dataset.idx,10));
    });
  });
})();
</script>`)
	buf.WriteString(`</body></html>`)
	return buf.String()
}

func renderPreviewElement(buf *bytes.Buffer, el *metadata.FormElement, tabsCounter *int) {
	if el == nil {
		return
	}
	title := el.Name
	if el.TitleMap != nil && el.TitleMap["ru"] != "" {
		title = el.TitleMap["ru"]
	} else if el.Title != "" {
		title = el.Title
	}
	switch el.Kind {
	case metadata.FormElementGroupBox:
		fmt.Fprintf(buf, `<fieldset><legend>%s</legend>`, html.EscapeString(title))
		for _, c := range el.Children {
			renderPreviewElement(buf, c, tabsCounter)
		}
		buf.WriteString(`</fieldset>`)
	case metadata.FormElementPages:
		// Уникальный id текущего tabset, чтобы вложенные СтраницыФормы
		// не конфликтовали при переключении.
		setID := *tabsCounter
		*tabsCounter++
		// Заголовки вкладок.
		fmt.Fprintf(buf, `<div class="tabs"><div class="tabs-hd" data-tabset-hdr="%d">`, setID)
		pageIdx := 0
		for _, p := range el.Children {
			if p.Kind != metadata.FormElementPage {
				continue
			}
			cls := "tab"
			if pageIdx == 0 {
				cls += " active"
			}
			ptitle := p.Name
			if p.TitleMap != nil && p.TitleMap["ru"] != "" {
				ptitle = p.TitleMap["ru"]
			}
			fmt.Fprintf(buf, `<button type="button" class="%s" data-tabset="%d" data-idx="%d">%s</button>`,
				cls, setID, pageIdx, html.EscapeString(ptitle))
			pageIdx++
		}
		buf.WriteString(`</div>`)
		// Содержимое всех страниц; неактивные — display:none через CSS.
		fmt.Fprintf(buf, `<div data-tabset-body="%d">`, setID)
		pageIdx = 0
		for _, p := range el.Children {
			if p.Kind != metadata.FormElementPage {
				continue
			}
			cls := "tab-page"
			if pageIdx == 0 {
				cls += " active"
			}
			fmt.Fprintf(buf, `<div class="%s">`, cls)
			for _, c := range p.Children {
				renderPreviewElement(buf, c, tabsCounter)
			}
			buf.WriteString(`</div>`)
			pageIdx++
		}
		buf.WriteString(`</div></div>`)
	case metadata.FormElementField:
		req := ""
		if el.Required {
			req = ` <span class="req">*</span>`
		}
		field := lastSegment(el.DataPath)
		if field == "" {
			field = el.Name
		}
		fmt.Fprintf(buf, `<div class="fg"><label>%s%s</label><input type="text" placeholder="%s"`, html.EscapeString(title), req, html.EscapeString(field))
		if el.ReadOnly {
			buf.WriteString(` readonly`)
		}
		buf.WriteString(`></div>`)
		if el.Hint != "" {
			fmt.Fprintf(buf, `<div class="hint" style="margin-top:-8px">%s</div>`, html.EscapeString(el.Hint))
		}
	case metadata.FormElementCheckbox:
		field := lastSegment(el.DataPath)
		if field == "" {
			field = el.Name
		}
		fmt.Fprintf(buf, `<div class="fg" style="display:flex;align-items:center;gap:8px"><input type="checkbox" id="cb-%s"`, html.EscapeString(field))
		if el.ReadOnly {
			buf.WriteString(` disabled`)
		}
		fmt.Fprintf(buf, `><label for="cb-%s" style="margin-bottom:0">%s</label></div>`, html.EscapeString(field), html.EscapeString(title))
	case metadata.FormElementLabel:
		fmt.Fprintf(buf, `<div class="deco">%s</div>`, html.EscapeString(title))
	case metadata.FormElementButton:
		fmt.Fprintf(buf, `<button type="button" class="btn">%s</button>`, html.EscapeString(title))
	case metadata.FormElementPicture:
		fmt.Fprintf(buf, `<div class="hint">[Картинка: %s]</div>`, html.EscapeString(el.Name))
	case metadata.FormElementTable, metadata.FormElementTablePart:
		mode := "обычная таблица"
		if el.UseGrid {
			mode = "SlickGrid (Excel-навигация)"
		}
		fmt.Fprintf(buf, `<div class="tp-stub">Табличная часть «%s» — %s. Предпросмотр упрощённый.</div>`,
			html.EscapeString(title), mode)
	case metadata.FormElementCommandBar:
		// командная панель — обычно рендерится в toolbar над формой;
		// в preview просто рисуем кнопки в ряд.
		for _, c := range el.Children {
			renderPreviewElement(buf, c, tabsCounter)
		}
	default:
		fmt.Fprintf(buf, `<div class="unknown">Элемент «%s» типа «%s»: предпросмотр не реализован.</div>`,
			html.EscapeString(el.Name), html.EscapeString(string(el.Kind)))
	}
}

// lastSegment — последний компонент пути "Объект.Контрагент" → "Контрагент".
func lastSegment(p string) string {
	if i := lastIndexByte(p, '.'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
