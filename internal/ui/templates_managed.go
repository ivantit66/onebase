package ui

// tplManagedForm — шаблон рендеринга «управляемой формы» из FormModule
// (план 37, этап 3). В отличие от tplForm, который автоматически выводит
// все поля Entity подряд, этот шаблон обходит дерево FormModule.Elements
// и отрисовывает каждый элемент по его Kind: ГруппаФормы → fieldset,
// СтраницыФормы → tabs, ПолеВвода → input/select (зависит от типа поля),
// и т.д.
//
// data_path вида "Объект.Контрагент" мапится на поле объекта по имени
// "Контрагент" (отбрасываем префикс "Объект."). Префикс "Список." и другие
// реквизиты формы — пока игнорируются (заглушка), будут добавлены позже.
//
// Опциональность: managed-форма выбирается в handlers.go только если в
// Entity.Forms есть FormModule с IsManaged()==true и подходящим Kind.
// Иначе работает старая авто-форма (tplForm) — backward-compat.
const tplManagedForm = `
{{define "managed-element"}}
{{$el := .El}}{{$ctx := .Ctx}}
{{if eq (str $el.Kind) "ГруппаФормы"}}
  <fieldset class="form-group-box" style="border:1px solid #e2e8f0;border-radius:8px;padding:12px 14px;margin-bottom:14px">
    {{if $el.TitleMap}}<legend style="font-weight:600;color:#475569;padding:0 6px;font-size:13px">{{fieldTitleRU $el.TitleMap $el.Name}}</legend>{{end}}
    {{range $el.Children}}{{template "managed-element" (dict "El" . "Ctx" $ctx)}}{{end}}
  </fieldset>
{{else if eq (str $el.Kind) "СтраницыФормы"}}
  {{/* CSS активной вкладки вынесен в стиль managed-форм (см. в конце шаблона)
       чтобы inline-style не побеждал .active по приоритету. */}}
  <div class="managed-tabs" data-tabs="{{$el.Name}}">
    <div class="managed-tab-headers" style="display:flex;gap:2px;border-bottom:2px solid #e2e8f0;margin-bottom:12px">
      {{range $i, $page := $el.Children}}
        {{if eq (str $page.Kind) "Страница"}}
        <button type="button" class="managed-tab-btn{{if eq $i 0}} active{{end}}" data-tab-idx="{{$i}}"
          onclick="this.closest('.managed-tabs').querySelectorAll('.managed-tab-btn').forEach(b=>b.classList.remove('active'));this.classList.add('active');this.closest('.managed-tabs').querySelectorAll('.managed-tab-content').forEach(c=>c.style.display='none');this.closest('.managed-tabs').querySelectorAll('.managed-tab-content')[{{$i}}].style.display='block'">
          {{fieldTitleRU $page.TitleMap $page.Name}}
        </button>
        {{end}}
      {{end}}
    </div>
    {{range $i, $page := $el.Children}}
      {{if eq (str $page.Kind) "Страница"}}
      <div class="managed-tab-content" data-tab-content="{{$i}}" style="display:{{if eq $i 0}}block{{else}}none{{end}}">
        {{range $page.Children}}{{template "managed-element" (dict "El" . "Ctx" $ctx)}}{{end}}
      </div>
      {{end}}
    {{end}}
  </div>
{{else if eq (str $el.Kind) "ПолеВвода"}}
  {{$fn := dpField $el.DataPath}}
  {{$f := fieldByName $ctx.Entity $fn}}
  {{$hChg := hasHandler $el "ПриИзменении"}}
  <div class="form-group">
    <label>{{fieldTitleRU $el.TitleMap $fn}}{{if $el.Required}} <span style="color:#dc2626">*</span>{{end}}</label>
    {{if $f}}
      {{if isRef (str $f.Type)}}
        <div style="display:flex;gap:6px;align-items:center">
          <select id="ref-{{$fn}}" name="{{$fn}}" style="flex:1"{{if $el.ReadOnly}} disabled{{end}}{{if $hChg}} onchange="obFire('{{$el.Name}}','ПриИзменении')"{{end}}>
            <option value="">— выбрать —</option>
            {{range index $ctx.RefOptions $fn}}
            <option value="{{index . "id"}}" {{if eq (index . "id") (index $ctx.Values $fn)}}selected{{end}}>{{index . "_label"}}</option>
            {{end}}
          </select>
          <button type="button" onclick="openRefPicker('ref-{{$fn}}')" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px">…</button>
          <button type="button" onclick="openRefCreate(document.getElementById('ref-{{$fn}}'), '{{$f.RefEntity}}')" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px;color:#16a34a;font-weight:600">+</button>
        </div>
      {{else if isEnum (str $f.Type)}}
        <select name="{{$fn}}"{{if $el.ReadOnly}} disabled{{end}}{{if $hChg}} onchange="obFire('{{$el.Name}}','ПриИзменении')"{{end}}>
          <option value="">— выбрать —</option>
          {{range index $ctx.EnumOptions $fn}}
          <option value="{{.}}" {{if eq . (index $ctx.Values $fn)}}selected{{end}}>{{.}}</option>
          {{end}}
        </select>
      {{else if eq (str $f.Type) "date"}}
        <input type="datetime-local" name="{{$fn}}" value="{{index $ctx.Values $fn}}"{{if $el.ReadOnly}} readonly{{end}}{{if $hChg}} onchange="obFire('{{$el.Name}}','ПриИзменении')"{{end}}>
      {{else if eq (str $f.Type) "bool"}}
        <select name="{{$fn}}"{{if $el.ReadOnly}} disabled{{end}}{{if $hChg}} onchange="obFire('{{$el.Name}}','ПриИзменении')"{{end}}>
          <option value="false" {{if eq (index $ctx.Values $fn) "false"}}selected{{end}}>Нет</option>
          <option value="true" {{if eq (index $ctx.Values $fn) "true"}}selected{{end}}>Да</option>
        </select>
      {{else}}
        <input type="text" name="{{$fn}}" value="{{index $ctx.Values $fn}}" placeholder="{{$fn}}"{{if $el.ReadOnly}} readonly{{end}}{{if $el.Mask}} pattern="{{$el.Mask}}"{{end}}{{if $hChg}} onchange="obFire('{{$el.Name}}','ПриИзменении')"{{end}}>
      {{end}}
    {{else}}
      {{/* Поле не найдено в Entity (возможно реквизит формы, ещё не привязан) */}}
      <input type="text" name="{{$fn}}" value="{{index $ctx.Values $fn}}" placeholder="{{$fn}}" style="background:#fef9c3"
        title="Реквизит формы '{{$el.DataPath}}' не найден среди полей сущности"{{if $hChg}} onchange="obFire('{{$el.Name}}','ПриИзменении')"{{end}}>
    {{end}}
    {{if $el.Hint}}<small style="color:#94a3b8;font-size:11px">{{$el.Hint}}</small>{{end}}
  </div>
{{else if eq (str $el.Kind) "Флажок"}}
  {{$fn := dpField $el.DataPath}}
  <div class="form-group" style="display:flex;align-items:center;gap:8px">
    <input type="checkbox" id="cb-{{$fn}}" name="{{$fn}}" value="true"
      {{if eq (index $ctx.Values $fn) "true"}}checked{{end}}{{if $el.ReadOnly}} disabled{{end}}>
    <label for="cb-{{$fn}}" style="margin-bottom:0;cursor:pointer">{{fieldTitleRU $el.TitleMap $fn}}</label>
  </div>
{{else if eq (str $el.Kind) "Надпись"}}
  <div class="form-decoration" style="padding:6px 0;color:#475569;font-size:13px">
    {{fieldTitleRU $el.TitleMap $el.Name}}
  </div>
{{else if eq (str $el.Kind) "Кнопка"}}
  <button type="button" class="btn btn-secondary" style="margin:6px 4px 6px 0"{{if $el.ReadOnly}} disabled{{end}}{{if hasHandler $el "Нажатие"}} onclick="obFire('{{$el.Name}}','Нажатие')"{{end}}>
    {{fieldTitleRU $el.TitleMap $el.Name}}
  </button>
{{else if eq (str $el.Kind) "ПолеКартинки"}}
  {{if $el.Picture}}
    <img src="/static/forms/{{$el.Picture}}" alt="{{$el.Name}}" style="max-width:{{if $el.Width}}{{$el.Width}}px{{else}}100px{{end}};max-height:{{if $el.Height}}{{$el.Height}}px{{else}}100px{{end}}">
  {{else}}
    <span style="color:#cbd5e1">[Картинка: {{$el.Name}}]</span>
  {{end}}
{{else if eq (str $el.Kind) "ТабличнаяЧасть"}}
  {{/* Табличная часть в managed-форме (план 37, этап 8). Имена name= совпадают
       с парсером parseTablePartRows: "tp.<TPName>.<idx>.<field>". obFire-JS
       перерисовывает tbody#mtp-body-<TPName> при изменении tableparts.
       Ссылочные колонки — select с TPRefOptions, иначе UUID без имени. */}}
  {{$tpName := dpField $el.DataPath}}
  {{$tpMeta := tablePartByName $ctx.Entity $tpName}}
  {{$tpRows := index $ctx.TablePartRows $tpName}}
  {{$tpRef := index $ctx.TPRefOptions $tpName}}
  <h3 style="margin:18px 0 8px;font-size:14px">{{fieldTitleRU $el.TitleMap $tpName}}</h3>
  {{if $tpMeta}}
  <table class="tp-table" data-tp="{{$tpName}}">
    <thead>
      <tr>
        {{range $tpMeta.Fields}}<th>{{.Name}}</th>{{end}}
        <th style="width:40px"></th>
      </tr>
    </thead>
    <tbody id="tp-body-{{$tpName}}" data-tp-fields="{{range $i, $f := $tpMeta.Fields}}{{if $i}},{{end}}{{$f.Name}}|{{$f.Type}}{{if $f.RefEntity}}:{{$f.RefEntity}}{{end}}{{end}}">
    {{range $i, $row := $tpRows}}
      <tr>
        {{range $f := $tpMeta.Fields}}
        <td>
          {{$v := index $row $f.Name}}
          {{if isRef (str $f.Type)}}
            <div style="display:flex;gap:4px;align-items:center">
              <select name="tp.{{$tpName}}.{{$i}}.{{$f.Name}}" style="flex:1">
                <option value="">— выбрать —</option>
                {{range index $tpRef $f.Name}}
                <option value="{{index . "id"}}" {{if eq (str (index . "id")) (refID $v)}}selected{{end}}>{{index . "_label"}}</option>
                {{end}}
              </select>
              <button type="button" onclick="openRefPicker(this.parentElement.querySelector('select'))" style="padding:4px 8px;border:1px solid #e2e8f0;border-radius:5px;background:#f8fafc;cursor:pointer;font-size:12px;flex-shrink:0" title="Выбрать из списка">...</button>
              <button type="button" onclick="openRefCreate(this.parentElement.querySelector('select'), '{{$f.RefEntity}}')" style="padding:4px 7px;border:1px solid #e2e8f0;border-radius:5px;background:#f8fafc;cursor:pointer;font-size:12px;flex-shrink:0;font-weight:600;color:#16a34a" title="Создать новый">+</button>
            </div>
          {{else if eq (str $f.Type) "number"}}
            <input type="number" step="any" name="tp.{{$tpName}}.{{$i}}.{{$f.Name}}" value="{{$v}}" data-tp-num="{{$f.Name}}" oninput="recalcTpRow(this)">
          {{else}}
            <input type="text" name="tp.{{$tpName}}.{{$i}}.{{$f.Name}}" value="{{$v}}" oninput="recalcTpRow(this)">
          {{end}}
        </td>
        {{end}}
        <td><button type="button" class="del-btn" onclick="this.closest('tr').remove()">×</button></td>
      </tr>
    {{end}}
    </tbody>
  </table>
  <button type="button" class="btn btn-sm" style="background:#e2e8f0;color:#475569;margin:0 0 12px"
    onclick="addTpRow('{{$tpName}}', [{{range $tpMeta.Fields}}'{{.Name}}',{{end}}], [{{range $tpMeta.Fields}}{{if eq (str .Type) "number"}}'{{.Name}}',{{end}}{{end}}], document.getElementById('tp-body-{{$tpName}}').rows.length)">
    + Добавить строку
  </button>
  {{else}}
  <div style="background:#fef9c3;padding:8px;border-radius:6px;font-size:12px;color:#92400e">
    Табличная часть «{{$tpName}}» не найдена в метаданных сущности.
  </div>
  {{end}}
{{else if eq (str $el.Kind) "СтраницаКоманднаяПанель"}}
  {{/* пропускаем — отрисовывается через toolbar в обвязке формы */}}
{{else if eq (str $el.Kind) "КоманднаяПанель"}}
  {{/* пропускаем — отрисовывается через toolbar в обвязке формы */}}
{{else}}
  <div class="form-group" style="background:#fef9c3;padding:8px;border-radius:6px;font-size:11px;color:#92400e">
    Элемент «{{$el.Name}}» типа «{{$el.Kind}}»: рендеринг не реализован.
  </div>
{{end}}
{{end}}

{{define "page-managed-form"}}
{{template "head" .}}{{if not .IsPopup}}{{template "nav" .}}{{end}}
<main>
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;max-width:900px">
  <h2 style="margin-bottom:0">
    {{if .IsNew}}Создать{{else}}Редактировать{{end}} — {{.Entity.DisplayName}}
    <span style="font-size:11px;color:#10b981;background:#d1fae5;padding:2px 8px;border-radius:10px;vertical-align:middle;font-weight:500" title="Управляемая форма из forms/{{lower .Entity.Name}}/">◇ managed</span>
  </h2>
  {{if .IsPopup}}
  <a href="javascript:void(0)" onclick="try{parent.postMessage({source:'obRefCancel'}, '*')}catch(e){}" title="Закрыть" style="font-size:22px;line-height:1;color:#94a3b8;text-decoration:none;padding:2px 8px;border-radius:5px;background:#f1f5f9;font-weight:300">×</a>
  {{else}}
  <a href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}" title="Закрыть" style="font-size:22px;line-height:1;color:#94a3b8;text-decoration:none;padding:2px 8px;border-radius:5px;background:#f1f5f9;font-weight:300">×</a>
  {{end}}
</div>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}

{{if not .IsPopup}}
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
  <button class="btn btn-secondary" type="submit" name="_action" value="" form="main-form">Записать</button>
  {{if .Entity.Posting}}
    <button class="btn btn-post" type="submit" name="_action" value="post_and_close" form="main-form">Провести и закрыть</button>
    {{if not .IsNew}}
      {{if eq (index .Values "posted") "true"}}
        <button class="btn btn-primary btn-sm" type="submit" name="_action" value="post" form="main-form">Перепровести</button>
      {{else}}
        <button class="btn btn-primary" type="submit" name="_action" value="post" form="main-form">Провести</button>
      {{end}}
    {{end}}
  {{end}}
  {{if not .IsNew}}
    <a href="/ui/{{lower (str .Entity.Kind)}}/{{.Entity.Name}}/{{.ID}}/history" class="btn btn-sm btn-secondary">История</a>
    <form method="POST" action="/ui/{{lower (str .Entity.Kind)}}/{{.Entity.Name}}/{{.ID}}/delete"
          onsubmit="return confirm('{{if .IsAdmin}}Удалить запись навсегда?{{else}}Пометить запись на удаление?{{end}}')" style="margin-left:auto">
      <button class="btn btn-danger btn-sm" type="submit">{{if .IsAdmin}}Удалить{{else}}Пометить на удаление{{end}}</button>
    </form>
  {{end}}
</div>
{{end}}{{/* end if not .IsPopup */}}

<div class="card">
<form id="main-form" method="POST">
{{if and (not .IsNew) (index .Values "_version")}}<input type="hidden" name="_version" value="{{index .Values "_version"}}">{{end}}
{{if .IsPopup}}<input type="hidden" name="_popup" value="1">{{end}}

{{$ctx := .}}
{{range .Form.Elements}}
  {{template "managed-element" (dict "El" . "Ctx" $ctx)}}
{{end}}

<div style="margin-top:16px">
  {{if .IsPopup}}
  <button class="btn btn-primary" type="submit" name="_action" value="" form="main-form">Записать и выбрать</button>
  <a href="javascript:void(0)" onclick="try{parent.postMessage({source:'obRefCancel'}, '*')}catch(e){}" class="btn btn-cancel">Отмена</a>
  {{else}}
  <button class="btn btn-secondary" type="submit" name="_action" value="" form="main-form">Записать</button>
  <a href="/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}" class="btn btn-cancel">Отмена</a>
  {{end}}
</div>
</form>
</div>

{{/* ── Рантайм событий managed-формы (план 37, этап 8) ────────────────── */}}
<script>
// Опции справочников для ТЧ — используются при JS-перерисовке после
// событий obFire, чтобы ссылочные колонки рендерились как select, а не
// как text input с UUID. Структура: {tpName: {fieldName: [{id, _label}, ...]}}.
window._tpRefOpts = {{jsJSON .TPRefOptions}};
// obFire — общая JS-обвязка для onclick/onchange элементов формы.
// Отправляет текущие form-values + имя элемента/события в /ui/.../form-event,
// получает JSON с новыми значениями и сообщениями от Сообщить(), применяет их.
(function(){
  const KIND   = "{{lower (str .Entity.Kind)}}";
  const ENTITY = "{{.Entity.Name}}";
  const URL    = "/ui/" + KIND + "/" + ENTITY + "/form-event";
  const DOC_ID = {{if .ID}}"{{.ID}}"{{else}}""{{end}};

  function ensureBanner(){
    let b = document.getElementById('ob-fmevt-banner');
    if (b) return b;
    b = document.createElement('div');
    b.id = 'ob-fmevt-banner';
    b.style.cssText = 'position:fixed;top:14px;right:14px;max-width:380px;z-index:9999;display:flex;flex-direction:column;gap:6px;pointer-events:none';
    document.body.appendChild(b);
    return b;
  }
  function flash(text, kind){
    const b = ensureBanner();
    const d = document.createElement('div');
    const palette = kind === 'err'
      ? 'background:#fee2e2;color:#991b1b;border:1px solid #fecaca'
      : 'background:#d1fae5;color:#065f46;border:1px solid #a7f3d0';
    d.style.cssText = palette + ';padding:8px 12px;border-radius:6px;font-size:12px;box-shadow:0 1px 3px rgba(0,0,0,.08);pointer-events:auto;cursor:pointer';
    d.textContent = text;
    d.onclick = () => d.remove();
    b.appendChild(d);
    setTimeout(() => d.remove(), kind === 'err' ? 9000 : 5000);
  }
  function applyValues(values){
    if (!values) return;
    const form = document.getElementById('main-form');
    if (!form) return;
    Object.keys(values).forEach(function(k){
      const v = values[k];
      const inp = form.querySelector('[name="' + (window.CSS && CSS.escape ? CSS.escape(k) : k) + '"]');
      if (!inp) return;
      if (inp.type === 'checkbox') {
        inp.checked = v === true || v === 'true' || v === 1;
      } else {
        inp.value = (v === null || v === undefined) ? '' : v;
      }
    });
  }
  // Перерисовка табчастей по ответу сервера. tbody у нас имеет
  // id=mtp-body-<TP> и атрибут data-tp-fields="name|type[:Ref],name|type,..."
  // где field-meta использовалось для определения типа input при первичном рендере;
  // тот же формат используется тут для повторного создания строк.
  function applyTableParts(tps){
    if (!tps) return;
    Object.keys(tps).forEach(function(tpName){
      const tbody = document.getElementById('tp-body-' + tpName);
      if (!tbody) return;
      const fieldsMeta = (tbody.getAttribute('data-tp-fields') || '').split(',').map(function(s){
        const idx = s.indexOf('|');
        if (idx < 0) return { name: s, type: 'string', ref: '' };
        const name = s.slice(0, idx);
        const rest = s.slice(idx + 1);
        const refIdx = rest.indexOf(':');
        if (refIdx >= 0) return { name: name, type: rest.slice(0, refIdx), ref: rest.slice(refIdx + 1) };
        return { name: name, type: rest, ref: '' };
      });
      const rows = tps[tpName] || [];
      const refOpts = (window._tpRefOpts && window._tpRefOpts[tpName]) || {};
      tbody.innerHTML = '';
      rows.forEach(function(row, idx){
        const tr = document.createElement('tr');
        fieldsMeta.forEach(function(f){
          const td = document.createElement('td');
          const v = row[f.name];
          const isRef = f.type === 'reference' || f.type.indexOf('reference') === 0;
          if (isRef && refOpts[f.name]) {
            const sel = document.createElement('select');
            sel.name = 'tp.' + tpName + '.' + idx + '.' + f.name;
            const empty = document.createElement('option');
            empty.value = ''; empty.textContent = '— выбрать —';
            sel.appendChild(empty);
            // v приходит сериализованным как UUID-string (serializeTablePartRows),
            // но на всякий случай учитываем и legacy-формат с GetRefUUID-методом.
            const cur = (v && typeof v === 'object' && v.GetRefUUID) ? v.GetRefUUID() : (v == null ? '' : String(v));
            refOpts[f.name].forEach(function(opt){
              const o = document.createElement('option');
              o.value = opt.id;
              o.textContent = opt._label;
              if (String(opt.id) === cur) o.selected = true;
              sel.appendChild(o);
            });
            td.appendChild(sel);
          } else {
            const inp = document.createElement('input');
            inp.name = 'tp.' + tpName + '.' + idx + '.' + f.name;
            if (isRef) {
              inp.type = 'text';
              inp.placeholder = 'UUID';
              inp.value = (v && typeof v === 'object' && v.GetRefUUID) ? v.GetRefUUID() : (v == null ? '' : String(v));
            } else if (f.type === 'number') {
              inp.type = 'number';
              inp.step = 'any';
              inp.value = (v == null ? '' : v);
            } else {
              inp.type = 'text';
              inp.value = (v == null ? '' : v);
            }
            td.appendChild(inp);
          }
          tr.appendChild(td);
        });
        const tdDel = document.createElement('td');
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'del-btn';
        btn.textContent = '×';
        btn.onclick = function(){ tr.remove(); };
        tdDel.appendChild(btn);
        tr.appendChild(tdDel);
        tbody.appendChild(tr);
      });
    });
  }

  window.obFire = async function(elementName, eventName){
    const form = document.getElementById('main-form');
    if (!form) return;
    const fd = new FormData(form);
    fd.set('_element', elementName || '');
    fd.set('_event', eventName || '');
    fd.set('_kind', 'object');
    if (DOC_ID) fd.set('_id', DOC_ID);
    const body = new URLSearchParams();
    fd.forEach((v, k) => body.append(k, typeof v === 'string' ? v : ''));
    try {
      const res = await fetch(URL, {
        method: 'POST',
        body: body,
        headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=utf-8' },
        credentials: 'same-origin'
      });
      const data = await res.json();
      applyTableParts(data.tableparts);
      applyValues(data.values);
      (data.messages || []).forEach(m => flash(m, 'ok'));
      if (data.error) flash(data.error, 'err');
    } catch (e) {
      flash('Сетевая ошибка: ' + (e && e.message ? e.message : e), 'err');
    }
  };
})();
</script>

{{/* Общие JS-функции addTpRow / openRefCreate / openRefPicker — те же,
     что в обычной auto-форме, чтобы "+" рядом со ссылкой и "Добавить
     строку" в ТЧ работали и в managed-форме. */}}
{{template "form-shared-js" .}}

{{/* Стиль активной вкладки. Inline-style на кнопке управляет базовым
     видом, а .active переопределяет цвет/border (выше по специфичности
     не получается без !important — поэтому используем именно класс). */}}
<style>
.managed-tab-btn{padding:8px 14px;border:none;background:none;cursor:pointer;font-size:13px;color:#64748b;border-bottom:2px solid transparent;margin-bottom:-2px;font-family:inherit}
.managed-tab-btn:hover{color:#1a4a80;background:#f5f8ff}
.managed-tab-btn.active{color:#1a4a80;border-bottom-color:#1a4a80;font-weight:600}
</style>

</main>
</body></html>
{{end}}
`
