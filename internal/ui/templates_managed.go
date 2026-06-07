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
          <select id="ref-{{$fn}}" name="{{$fn}}" style="flex:1" data-ref-entity="{{$f.RefEntity}}"{{if $f.InlineCreateEnabled false}} data-ref-allow-create="1"{{end}}{{if $el.ReadOnly}} disabled{{end}}{{if $hChg}} onchange="obFire('{{$el.Name}}','ПриИзменении')"{{end}}>
            <option value="">— выбрать —</option>
            {{range index $ctx.RefOptions $fn}}
            <option value="{{index . "id"}}" {{if eq (index . "id") (index $ctx.Values $fn)}}selected{{end}}>{{index . "_label"}}</option>
            {{end}}
          </select>
          <button type="button" onclick="openRefPicker('ref-{{$fn}}')" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px">…</button>
          <button type="button" onclick="openRefCurrent('ref-{{$fn}}')" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px" title="Открыть карточку">🔍</button>
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
      {{else if eq (str $el.Type) "file"}}
        <div style="display:flex;gap:6px;align-items:center">
          <input type="text" name="{{$fn}}" id="file-path-{{$fn}}" placeholder="Путь к файлу или выберите …" style="flex:1"{{if $el.ReadOnly}} readonly{{end}}>
          <textarea name="_fc_{{$fn}}" id="file-content-{{$fn}}" style="display:none"></textarea>
          <input type="file" id="file-pick-{{$fn}}" style="display:none" onchange="obFilePick(this,'file-path-{{$fn}}','file-content-{{$fn}}')">
          <button type="button" onclick="document.getElementById('file-pick-{{$fn}}').click()" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px;white-space:nowrap" title="Выбрать файл">…</button>
        </div>
      {{else}}
        <input type="text" name="{{$fn}}" value="{{index $ctx.Values $fn}}" placeholder="{{$fn}}"{{if $el.ReadOnly}} readonly{{end}}{{if $el.Mask}} pattern="{{$el.Mask}}"{{end}}{{if $hChg}} onchange="obFire('{{$el.Name}}','ПриИзменении')"{{end}}>
      {{end}}
    {{else if eq (str $el.Type) "file"}}
      {{/* Поле не найдено в Entity, но элемент объявлен как file */}}
      <div style="display:flex;gap:6px;align-items:center">
        <input type="text" name="{{$fn}}" id="file-path-{{$fn}}" placeholder="Путь к файлу или выберите …" style="flex:1">
        <textarea name="_fc_{{$fn}}" id="file-content-{{$fn}}" style="display:none"></textarea>
        <input type="file" id="file-pick-{{$fn}}" style="display:none" onchange="obFilePick(this,'file-path-{{$fn}}','file-content-{{$fn}}')">
        <button type="button" onclick="document.getElementById('file-pick-{{$fn}}').click()" style="padding:8px 12px;border:1px solid #e2e8f0;border-radius:7px;background:#f8fafc;cursor:pointer;font-size:13px;white-space:nowrap" title="Выбрать файл">…</button>
      </div>
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
  {{$tpCmds := tpCommandButtons $el}}
  <h3 style="margin:18px 0 8px;font-size:14px">{{fieldTitleRU $el.TitleMap $tpName}}</h3>
  {{if $tpMeta}}
  {{if $tpCmds}}
  <div style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:6px">
    {{range $tpCmds}}
    <button type="button" class="btn btn-sm" style="background:#eef2ff;color:#3730a3;border:1px solid #c7d2fe"
      {{if $el.ReadOnly}}disabled{{end}}{{if hasHandler . "Нажатие"}} onclick="obFire('{{.Name}}','Нажатие',{_tp:'{{$tpName}}'})"{{end}}>
      {{fieldTitleRU .TitleMap .Name}}
    </button>
    {{end}}
  </div>
  {{end}}
  {{if $el.UseGrid}}
  <div id="sg-{{$tpName}}" class="ob-grid" style="height:{{if gt (len $tpRows) 8}}300{{else}}200{{end}}px;width:100%"
       data-sg-tp="{{$tpName}}"
       data-sg-el="{{$el.Name}}"
       {{if hasHandler $el "ПриИзменении"}}data-sg-recalc="1"{{end}}
       data-sg-cols='[{{range $i, $f := $tpMeta.Fields}}{{if $i}},{{end}}{"id":"{{$f.Name}}","name":"{{$f.Name}}","type":"{{$f.Type}}"{{if $f.RefEntity}},"ref":"{{$f.RefEntity}}"{{end}}}{{end}}]'
       data-sg-ref='{{jsJSON $tpRef}}'
       data-sg-rows='{{jsJSON $tpRows}}'
       {{if $tpCmds}}data-sg-cmd="1"{{end}}></div>
  <input type="hidden" name="tp_json.{{$tpName}}" id="tp-json-{{$tpName}}" value="">
  <div style="display:flex;gap:6px;margin-top:4px">
    <button type="button" class="btn btn-sm" style="background:#e2e8f0;color:#475569"
      onclick="obGridAddRow('{{$tpName}}')">+ Добавить строку</button>
    <button type="button" class="btn btn-sm" style="background:#fee2e2;color:#991b1b"
      onclick="obGridDelRow('{{$tpName}}')">− Удалить строку</button>
  </div>
{{else}}
<table class="tp-table" data-tp="{{$tpName}}">
    <thead>
      <tr>
        {{if $tpCmds}}<th style="width:30px"></th>{{end}}
        {{range $tpMeta.Fields}}<th>{{.Name}}</th>{{end}}
        <th style="width:40px"></th>
      </tr>
    </thead>
    <tbody id="tp-body-{{$tpName}}" {{if $tpCmds}}data-tp-cmd="1" {{end}}data-tp-fields="{{range $i, $f := $tpMeta.Fields}}{{if $i}},{{end}}{{$f.Name}}|{{$f.Type}}{{if $f.RefEntity}}:{{$f.RefEntity}}{{end}}{{end}}">
    {{range $i, $row := $tpRows}}
      <tr>
        {{if $tpCmds}}<td style="text-align:center"><input type="checkbox" class="_tp-sel"></td>{{end}}
        {{range $f := $tpMeta.Fields}}
        <td>
          {{$v := index $row $f.Name}}
          {{if isRef (str $f.Type)}}
            <div style="display:flex;gap:4px;align-items:center">
              <select name="tp.{{$tpName}}.{{$i}}.{{$f.Name}}" style="flex:1" data-ref-entity="{{$f.RefEntity}}"{{if $f.InlineCreateEnabled true}} data-ref-allow-create="1"{{end}}>
                <option value="">— выбрать —</option>
                {{range index $tpRef $f.Name}}
                <option value="{{index . "id"}}" {{if eq (str (index . "id")) (refID $v)}}selected{{end}}>{{index . "_label"}}</option>
                {{end}}
              </select>
              <button type="button" onclick="openRefPicker(this.parentElement.querySelector('select'))" style="padding:4px 8px;border:1px solid #e2e8f0;border-radius:5px;background:#f8fafc;cursor:pointer;font-size:12px;flex-shrink:0" title="Выбрать из списка">...</button>
              <button type="button" onclick="openRefCurrent(this.parentElement.querySelector('select'))" style="padding:4px 7px;border:1px solid #e2e8f0;border-radius:5px;background:#f8fafc;cursor:pointer;font-size:12px;flex-shrink:0" title="Открыть карточку">🔍</button>
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
    <tfoot id="tp-foot-{{$tpName}}" class="tp-footer" style="display:none"><tr>
      {{if $tpCmds}}<td></td>{{end}}
      {{range $f := $tpMeta.Fields}}{{if eq (str $f.Type) "number"}}<td class="tp-total" data-tp-total="{{$tpName}}.{{$f.Name}}" style="text-align:right;font-variant-numeric:tabular-nums">0</td>{{else}}<td></td>{{end}}{{end}}<td></td>
    </tr></tfoot>
  </table>
  <button type="button" class="btn btn-sm" style="background:#e2e8f0;color:#475569;margin:0 0 12px"
    onclick="addTpRow('{{$tpName}}', [{{range $tpMeta.Fields}}'{{.Name}}',{{end}}], [{{range $tpMeta.Fields}}{{if eq (str .Type) "number"}}'{{.Name}}',{{end}}{{end}}], document.getElementById('tp-body-{{$tpName}}').rows.length)">
    + Добавить строку
{{end}}
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
{{if hasGridTP .Form}}
<link rel="stylesheet" href="/vendor/slickgrid/slick.grid.css">
<link rel="stylesheet" href="/vendor/slickgrid/slick-default-theme.css">
<style>
.ob-grid{font-size:13px;border:1px solid #e2e8f0;border-radius:6px;overflow:hidden}
.ob-grid .slick-header-columns{background:#f8fafc;border-bottom:2px solid #e2e8f0}
.ob-grid .slick-header-column{font-weight:600;color:#475569;font-size:12px;padding:6px 8px;border-right:1px solid #e2e8f0}
.ob-grid .slick-row{border-bottom:1px solid #f1f5f9}
.ob-grid .slick-row:hover .slick-cell{background:#f0f4ff}
.ob-grid .slick-cell{padding:4px 8px;border-right:1px solid #f1f5f9}
.ob-grid .slick-cell.selected{background:#dbeafe}
.ob-grid .slick-cell.active{border:2px solid #3b82f6;padding:2px 6px}
.ob-grid .ob-num{text-align:right;font-variant-numeric:tabular-nums}
.ob-grid .ob-ref{color:#2563eb;cursor:pointer}
.ob-grid .ob-ref:hover{text-decoration:underline}
</style>
{{end}}
<main>
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;max-width:1400px">
  <h2 style="margin-bottom:0">
    {{if .IsProcessor}}{{.Processor.DisplayName $.Lang}}{{else}}{{if .IsNew}}{{t $.Lang "Создать"}}{{else}}{{t $.Lang "Редактировать"}}{{end}} — {{.Entity.DisplayName $.Lang}}{{end}}
    <span style="font-size:11px;color:#10b981;background:#d1fae5;padding:2px 8px;border-radius:10px;vertical-align:middle;font-weight:500" title="Управляемая форма из forms/{{if .IsProcessor}}{{lower .Processor.Name}}{{else}}{{lower .Entity.Name}}{{end}}/">◇ managed</span>
  </h2>
  {{if .IsPopup}}
  <a href="javascript:void(0)" onclick="try{parent.postMessage({source:'obRefCancel'}, '*')}catch(e){}" title="Закрыть" style="font-size:22px;line-height:1;color:#94a3b8;text-decoration:none;padding:2px 8px;border-radius:5px;background:#f1f5f9;font-weight:300">×</a>
  {{else}}
  <a href="{{if .IsProcessor}}/ui/{{else}}/ui/{{lower (str .Entity.Kind)}}/{{lower .Entity.Name}}{{end}}" title="Закрыть" style="font-size:22px;line-height:1;color:#94a3b8;text-decoration:none;padding:2px 8px;border-radius:5px;background:#f1f5f9;font-weight:300">×</a>
  {{end}}
</div>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
{{if .RunError}}<div class="error">{{.RunError}}</div>{{end}}
{{if .Messages}}{{range .Messages}}<div class="msg-info">{{.}}</div>{{end}}{{end}}

{{if not .IsPopup}}
<div style="display:flex;align-items:center;gap:8px;margin-bottom:16px;flex-wrap:wrap">
  {{if .IsProcessor}}
  {{/* Кнопка «Выполнить» скрыта: managed-форма использует свои кнопки (Предпросмотр, Записать, ЗаписатьИПровести) */}}
  {{else}}
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
    {{if .CanPost}}<button class="btn btn-post" type="submit" name="_action" value="post_and_close" form="main-form">Провести и закрыть</button>{{end}}
    {{if not .IsNew}}
      {{if eq (index .Values "posted") "true"}}
        {{if .CanPost}}<button class="btn btn-primary btn-sm" type="submit" name="_action" value="post" form="main-form">Перепровести</button>{{end}}
      {{else}}
        {{if .CanPost}}<button class="btn btn-primary" type="submit" name="_action" value="post" form="main-form">Провести</button>{{end}}
      {{end}}
    {{end}}
  {{end}}
  {{if not .IsNew}}
    <a href="/ui/{{lower (str .Entity.Kind)}}/{{.Entity.Name}}/{{.ID}}/history" class="btn btn-sm btn-secondary">История</a>
    {{if or .PrintForms .DSLPrintForms .HasPrintProc}}
    <div style="position:relative">
      <button type="button" class="btn btn-sm btn-secondary" onclick="var d=this.nextElementSibling;d.style.display=d.style.display==='none'?'block':'none'">{{t $.Lang "Печать"}} ▾</button>
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
           style="display:block;padding:9px 16px;color:#334155;text-decoration:none;font-size:13px;border-bottom:1px solid #f1f5f9">📋 {{t $.Lang "Печать (модуль)"}}</a>
        {{end}}
      </div>
    </div>
    {{end}}
    {{if .Receivers}}
    <div style="position:relative;display:inline-block">
      <button type="button" class="btn btn-sm btn-secondary" onclick="var d=this.nextElementSibling;d.style.display=d.style.display==='none'?'block':'none'">{{t $.Lang "Ввести на основании"}} ▾</button>
      <div style="display:none;position:absolute;top:100%;left:0;background:#fff;border:1px solid #e2e8f0;border-radius:8px;box-shadow:0 4px 16px rgba(0,0,0,.1);min-width:200px;z-index:50;margin-top:4px">
        {{range .Receivers}}
        <a href="/ui/{{lower (str .Kind)}}/{{.Name}}/new?based_on={{$.Entity.Name}}&based_on_id={{$.ID}}"
           style="display:block;padding:9px 16px;color:#334155;text-decoration:none;font-size:13px;border-bottom:1px solid #f1f5f9">{{.DisplayName $.Lang}}</a>
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
  {{end}}{{/* end if not .IsProcessor */}}
</div>
{{end}}{{/* end if not .IsPopup */}}

{{/* Движения регистров: свёрнутые «таблеточки» по каждому регистру с
     количеством строк. Симметрично page-form, чтобы пользователь видел
     результат проведения и в managed-форме. */}}
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
<form id="main-form" method="POST" onsubmit="if(window.obGridSync)obGridSync()" {{if .IsProcessor}}action="/ui/processor/{{lower .Processor.Name}}" enctype="multipart/form-data"{{end}}>
{{if and (not .IsNew) (index .Values "_version")}}<input type="hidden" name="_version" value="{{index .Values "_version"}}">{{end}}
{{if .IsPopup}}<input type="hidden" name="_popup" value="1">{{end}}

{{$ctx := .}}
{{range .Form.Elements}}
  {{template "managed-element" (dict "El" . "Ctx" $ctx)}}
{{end}}

<div style="margin-top:16px">
  {{if .IsPopup}}
  {{if .CanWrite}}<button class="btn btn-primary" type="submit" name="_action" value="" form="main-form">Записать и выбрать</button>{{end}}
  <a href="javascript:void(0)" onclick="try{parent.postMessage({source:'obRefCancel'}, '*')}catch(e){}" class="btn btn-cancel">Отмена</a>
  {{else if .IsProcessor}}
  {{/* Кнопка «Выполнить» скрыта: managed-форма использует свои кнопки */}}
  <a href="/ui/" class="btn btn-cancel">Отмена</a>
  {{else}}
  {{if .CanWrite}}<button class="btn btn-secondary" type="submit" name="_action" value="" form="main-form">Записать</button>{{end}}
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
  {{if .IsProcessor}}
  const URL    = "/ui/processor/{{lower .Processor.Name}}/form-event";
  const DOC_ID = "";
  {{else}}
  const URL    = "/ui/" + KIND + "/" + ENTITY + "/form-event";
  const DOC_ID = {{if .ID}}"{{.ID}}"{{else}}""{{end}};
  {{end}}

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
      // Пропускаем файловые поля: не подставляем содержимое в поле пути
      const fc = form.querySelector('[name="_fc_' + (window.CSS && CSS.escape ? CSS.escape(k) : k) + '"]');
      if (fc) return;
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
      const hasCmd = tbody.getAttribute('data-tp-cmd') === '1';
      tbody.innerHTML = '';
      rows.forEach(function(row, idx){
        const tr = document.createElement('tr');
        if (hasCmd) {
          const tdSel = document.createElement('td');
          tdSel.style.textAlign = 'center';
          const cbSel = document.createElement('input');
          cbSel.type = 'checkbox'; cbSel.className = '_tp-sel';
          tdSel.appendChild(cbSel);
          tr.appendChild(tdSel);
        }
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

  // Экспортируем в window, чтобы grid-aware обёртка (план 48) могла
  // переопределить applyTableParts и обновлять SlickGrid после round-trip.
  // obFire ниже зовёт именно window.applyTableParts — так обёртка попадает
  // в цепочку, а не остаётся мёртвым кодом.
  window.applyTableParts = applyTableParts;

  // obFilePick — при выборе файла: имя в текстовое поле, содержимое в скрытый
  // textarea. Кодировка: UTF-8 → fallback Windows-1251 (TextDecoder).
  // В webview/Electron — file.path вместо содержимого.
  window.obFilePick = function(input, pathId, contentId) {
    const file = input.files[0];
    if (!file) return;
    const pathEl = document.getElementById(pathId);
    const contentEl = contentId ? document.getElementById(contentId) : null;
    if (!pathEl) return;
    if (file.path) {
      pathEl.value = file.path;
      if (contentEl) contentEl.value = '';
      return;
    }
    pathEl.value = file.name;
    if (!contentEl) return;
    const reader = new FileReader();
    reader.onload = function() {
      const bytes = new Uint8Array(reader.result);
      let text;
      try {
        text = new TextDecoder('utf-8', {fatal: true}).decode(bytes);
      } catch(e) {
        text = new TextDecoder('windows-1251').decode(bytes);
      }
      contentEl.value = text;
    };
    reader.readAsArrayBuffer(file);
  };

  // obFire(elementName, eventName[, extraParams]) — extraParams (объект)
  // добавляются к телу запроса. Используется подбором (план 46): фаза 2
  // шлёт {_pick_result}, команды ТЧ — {_tp, _tp_selected}.
  window.obFire = async function(elementName, eventName, extraParams){
    if (window.obGridSync) obGridSync();
    const form = document.getElementById('main-form');
    if (!form) return;
    const fd = new FormData(form);
    fd.set('_element', elementName || '');
    fd.set('_event', eventName || '');
    fd.set('_kind', 'object');
    if (DOC_ID) fd.set('_id', DOC_ID);
    const body = new URLSearchParams();
    fd.forEach((v, k) => {
      if (k.startsWith('_fc_')) return; // skip file-content helper fields
      if (typeof v !== 'string') { body.append(k, ''); return; }
      // If a _fc_ counterpart exists with content, prefer it over the path
      const fcEl = form.querySelector('[name="_fc_' + k + '"]');
      if (fcEl && fcEl.value) { body.append(k, fcEl.value); }
      else { body.append(k, v); }
    });
    // Команда над ТЧ: подмешать индексы выделенных строк (_tp_selected) по
    // имени ТЧ из extraParams._tp.
    if (extraParams && extraParams._tp) {
      // Plan 48: check if SlickGrid exists for this TP
      var obg = (window._obGrids || {})[extraParams._tp];
      if (obg) {
        var sel = obg.grid.getSelectedRows();
        body.append('_tp_selected', sel.join(','));
      } else {
        // Legacy: read from DOM checkboxes
        const tbody = document.getElementById('tp-body-' + extraParams._tp);
        if (tbody) {
          const sel = [];
          Array.prototype.forEach.call(tbody.rows, (tr, i) => {
            const cb = tr.querySelector('._tp-sel');
            if (cb && cb.checked) sel.push(i);
          });
          body.append('_tp_selected', sel.join(','));
        }
      }
    }
    if (extraParams) {
      Object.keys(extraParams).forEach(k => body.append(k, extraParams[k]));
    }
    try {
      const res = await fetch(URL, {
        method: 'POST',
        body: body,
        headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=utf-8' },
        credentials: 'same-origin'
      });
      const data = await res.json();
      // Подбор фазы 1: сервер вернул pickerData — открыть диалог, не трогая
      // ТЧ (её обновит фаза 2 после «Перенести»).
      if (data.pickerData) {
        (data.messages || []).forEach(m => flash(m, 'ok'));
        if (data.error) flash(data.error, 'err');
        openItemPicker(data.pickerData, elementName);
        return;
      }
      window.applyTableParts(data.tableparts);
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

{{if hasGridTP .Form}}
<script src="/vendor/slickgrid/slick.core.js"></script>
<script src="/vendor/slickgrid/slick.interactions.js"></script>
<script src="/vendor/slickgrid/slick.grid.js"></script>
<script src="/vendor/slickgrid/slick.dataview.js"></script>
<script src="/vendor/slickgrid/slick.editors.js"></script>
<script src="/vendor/slickgrid/slick.formatters.js"></script>
<script>
// SlickGrid initializer for managed-form table parts (plan 48).
// Grids are stored in window._obGrids = {tpName: {grid, dataView, columns}}.
(function(){
  window._obGrids = window._obGrids || {};

  // Serialize ref value: extract id from {id,_label} object or return raw value
  function refId(v) {
    if (v && typeof v === "object" && v.id !== undefined) return v.id;
    return (v == null) ? "" : String(v);
  }

  // Custom ref editor with dropdown search and picker button (plan 48, phase 4).
  function ObRefEditor(refField, refOptsList, args) {
    var wrapper, input, dropBtn, list, isOpen = false, selectedId = '', defaultValue = '';

    function label(id) {
      for (var k = 0; k < refOptsList.length; k++) {
        if (String(refOptsList[k].id) === String(id)) return refOptsList[k]._label;
      }
      return '';
    }

    this.init = function() {
      wrapper = document.createElement('div');
      wrapper.style.cssText = 'display:flex;align-items:center;width:100%;height:100%;gap:2px';

      input = document.createElement('input');
      input.type = 'text';
      input.style.cssText = 'flex:1;border:none;outline:none;padding:2px 4px;font-size:13px;min-width:0';
      var cur = args.item[args.column.field];
      defaultValue = cur;
      selectedId = (cur && typeof cur === 'object') ? (cur.id || '') : (cur || '');
      input.value = label(selectedId) || String(selectedId);

      dropBtn = document.createElement('button');
      dropBtn.type = 'button';
      dropBtn.textContent = '…';
      dropBtn.title = 'Выбрать из списка';
      dropBtn.style.cssText = 'border:none;background:none;cursor:pointer;font-size:12px;padding:0 4px;color:#2563eb;flex-shrink:0';

      wrapper.appendChild(input);
      wrapper.appendChild(dropBtn);
      args.container.appendChild(wrapper);

      // Dropdown list
      list = document.createElement('div');
      list.style.cssText = 'position:absolute;z-index:9999;background:#fff;border:1px solid #e2e8f0;border-radius:6px;box-shadow:0 4px 12px rgba(0,0,0,.12);max-height:200px;overflow-y:auto;min-width:160px;font-size:13px';

      function buildList(filter) {
        list.innerHTML = '';
        var f = (filter || '').toLowerCase();
        var found = false;
        for (var k = 0; k < refOptsList.length; k++) {
          var opt = refOptsList[k];
          if (f && opt._label.toLowerCase().indexOf(f) < 0) continue;
          found = true;
          var item = document.createElement('div');
          item.textContent = opt._label;
          item.setAttribute('data-id', opt.id);
          item.style.cssText = 'padding:6px 10px;cursor:pointer;border-bottom:1px solid #f1f5f9';
          item.addEventListener('mouseenter', function() { this.style.background = '#eef2ff'; });
          item.addEventListener('mouseleave', function() { this.style.background = ''; });
          (function(o) {
            item.addEventListener('mousedown', function(e) {
              e.preventDefault();
              selectedId = o.id;
              input.value = o._label;
              closeList();
              args.commitChanges();
            });
          })(opt);
          list.appendChild(item);
        }
        if (!found) {
          var empty = document.createElement('div');
          empty.textContent = 'Ничего не найдено';
          empty.style.cssText = 'padding:8px 10px;color:#94a3b8;font-style:italic';
          list.appendChild(empty);
        }
      }

      function openList() {
        if (isOpen) return;
        isOpen = true;
        buildList(input.value);
        var rect = input.getBoundingClientRect();
        list.style.left = rect.left + 'px';
        list.style.top = rect.bottom + 'px';
        list.style.width = Math.max(rect.width, 160) + 'px';
        document.body.appendChild(list);
      }

      function closeList() {
        if (!isOpen) return;
        isOpen = false;
        if (list.parentElement) list.remove();
      }

      input.addEventListener('focus', openList);
      input.addEventListener('input', function() {
        if (isOpen) buildList(input.value);
        else openList();
      });
      input.addEventListener('blur', function() { setTimeout(closeList, 150); });

      dropBtn.addEventListener('click', function(e) {
        e.preventDefault();
        e.stopPropagation();
        // Use existing openRefPicker mechanism
        var selEl = document.createElement('select');
        selEl.setAttribute('data-ref-entity', args.column.refEntity || '');
        refOptsList.forEach(function(opt) {
          var o = document.createElement('option');
          o.value = opt.id; o.textContent = opt._label;
          selEl.appendChild(o);
        });
        selEl.value = selectedId;
        // Monkey-patch: picker callback sets our cell value
        var origPicker = window.openRefPicker;
        window.openRefPicker(selEl);
        // Poll for picker result via the select value
        var pickerInterval = setInterval(function() {
          var modal = document.getElementById('_ref-picker-modal');
          if (!modal) {
            clearInterval(pickerInterval);
            // Picker closed - check if value changed
            var newVal = selEl.value;
            if (newVal !== selectedId) {
              selectedId = newVal;
              input.value = label(newVal);
              args.commitChanges();
            }
          }
        }, 300);
      });

      input.focus();
      input.select();
    };

    this.destroy = function() {
      closeList();
      if (wrapper && wrapper.parentElement) wrapper.remove();
    };
    this.focus = function() { input && input.focus(); };
    this.getValue = function() { return selectedId; };
    this.setValue = function(val) { selectedId = val; input.value = label(val); };
    this.loadValue = function(item) {
      var v = item[args.column.field];
      defaultValue = v;
      selectedId = (v && typeof v === 'object') ? (v.id || '') : (v || '');
      input.value = label(selectedId);
    };
    this.serializeValue = function() { return selectedId; };
    this.applyValue = function(item, state) { item[args.column.field] = state; };
    this.isValueChanged = function() { return selectedId !== defaultValue; };
    this.validate = function() { return {valid: true, msg: null}; };
    this.init();
  }

  // Custom number editor with locale-aware parsing (plan 48, phase 3).
  function ObNumberEditor(args) {
    var input, defaultValue;
    this.init = function() {
      input = document.createElement('input');
      input.type = 'text';
      input.className = 'editor-text';
      input.style.cssText = 'width:100%;height:100%;border:none;outline:none;padding:2px 4px;text-align:right;font-variant-numeric:tabular-nums;font-size:13px';
      args.container.appendChild(input);
      input.focus();
      var val = args.item[args.column.field];
      defaultValue = val;
      if (val != null && val !== '') input.value = String(val).replace('.', ',');
      input.select();
    };
    this.destroy = function() { input.remove(); };
    this.focus = function() { input.focus(); };
    this.getValue = function() { return input.value; };
    this.setValue = function(val) { input.value = val; };
    this.loadValue = function(item) {
      var v = item[args.column.field];
      defaultValue = v;
      input.value = (v != null && v !== '') ? String(v).replace('.', ',') : '';
    };
    this.serializeValue = function() {
      var v = input.value.replace(/\s/g, '').replace(',', '.');
      return v === '' ? '' : v;
    };
    this.applyValue = function(item, state) { item[args.column.field] = state; };
    this.isValueChanged = function() {
      return input.value !== ((defaultValue != null) ? String(defaultValue).replace('.', ',') : '');
    };
    this.validate = function() {
      var v = input.value.replace(/\s/g, '').replace(',', '.');
      if (v !== '' && isNaN(Number(v))) return {valid: false, msg: 'Введите число'};
      return {valid: true, msg: null};
    };
    this.init();
  }

  // Build SlickGrid columns from metadata with editors (plan 48, phase 3).
  function buildColumns(colsMeta, refOpts) {
    var columns = [];
    for (var i = 0; i < colsMeta.length; i++) {
      var c = colsMeta[i];
      var col = {id: c.id, name: c.name, field: c.id, width: 120, resizable: true};
      if (c.type === "number") {
        col.cssClass = "ob-num";
        col.editor = ObNumberEditor;
        col.formatter = function(row, cell, value) {
          if (value == null || value === "") return "";
          var n = Number(String(value).replace(',', '.'));
          if (isNaN(n)) return "<span>" + value + "</span>";
          return "<span>" + n.toLocaleString("ru-RU", {minimumFractionDigits:0, maximumFractionDigits:2}) + "</span>";
        };
      } else if (c.ref) {
        col.cssClass = "ob-ref";
        col.editor = (function(refField, refOptsList) {
          return ObRefEditor.bind(null, refField, refOptsList);
        })(c.id, refOpts[c.id] || []);
        col.formatter = (function(refField) {
          return function(row, cell, value, colDef, dataCtx) {
            if (!value) return "";
            if (typeof value === "object" && value._label) return "<span>" + value._label + "</span>";
            var opts = (refOpts && refOpts[refField]) || [];
            for (var k = 0; k < opts.length; k++) {
              if (String(opts[k].id) === String(value)) return "<span>" + opts[k]._label + "</span>";
            }
            return "<span>" + String(value) + "</span>";
          };
        })(c.id);
      } else {
        col.editor = Slick.Editors.Text;
      }
      columns.push(col);
    }
    return columns;
  }

  // Serialize ref value
  function refId(v) {
    if (v && typeof v === "object" && v.id !== undefined) return v.id;
    return (v == null) ? "" : String(v);
  }

  // Serialize all grid data into hidden inputs (for form submit / obFire)
  window.obGridSync = function() {
    var grids = window._obGrids || {};
    for (var tpName in grids) {
      var g = grids[tpName];
      var items = g.dataView.getItems();
      var rows = items.map(function(item) {
        var row = {};
        var cols = g.columnsMeta || [];
        for (var i = 0; i < cols.length; i++) {
          row[cols[i].id] = refId(item[cols[i].id]);
        }
        return row;
      });
      var inp = document.getElementById("tp-json-" + tpName);
      if (inp) inp.value = JSON.stringify(rows);
    }
  };

  // Add empty row to grid
  window.obGridAddRow = function(tpName) {
    var g = (window._obGrids || {})[tpName];
    if (!g) return;
    var nextId = 0;
    g.dataView.getItems().forEach(function(it) { if (it.id >= nextId) nextId = it.id + 1; });
    var item = {id: nextId};
    var cols = g.columnsMeta || [];
    for (var i = 0; i < cols.length; i++) item[cols[i].id] = "";
    g.dataView.addItem(item);
    g.grid.invalidate();
    g.grid.scrollRowIntoView(nextId);
    var rowIdx = g.dataView.getRowById(nextId);
    if (rowIdx !== undefined && g.columns.length > 0) {
      g.grid.setActiveCell(rowIdx, 0);
      g.grid.editActiveCell();
    }
  };

  // Delete selected row from grid
  window.obGridDelRow = function(tpName) {
    var g = (window._obGrids || {})[tpName];
    if (!g) return;
    var sel = g.grid.getSelectedRows();
    if (!sel || !sel.length) return;
    var items = g.dataView.getItems();
    var toRemove = sel.map(function(r) { return items[r]; });
    for (var i = 0; i < toRemove.length; i++) g.dataView.deleteItem(toRemove[i].id);
    g.grid.invalidate();
    g.grid.setSelectedRows([]);
  };

  // SlickGrid-aware applyTableParts. Оборачивает window.applyTableParts (DOM-
  // версию из obFire-IIFE): для ТЧ с гридом обновляет модель грида, для
  // остальных вызывает origApplyTP. Активную ячейку сохраняем, чтобы серверный
  // пересчёт сумм (Р2) не сбивал курсор при быстром вводе с клавиатуры.
  var origApplyTP = window.applyTableParts;
  window.applyTableParts = function(tps) {
    if (!tps) return;
    var grids = window._obGrids || {};
    Object.keys(tps).forEach(function(tpName) {
      var g = grids[tpName];
      if (!g) return;
      var active = g.grid.getActiveCell();
      var rows = tps[tpName] || [];
      var cols = g.columnsMeta || [];
      var items = rows.map(function(r, idx) {
        var item = {id: idx};
        for (var i = 0; i < cols.length; i++) item[cols[i].id] = r[cols[i].id] || "";
        return item;
      });
      g.dataView.setItems(items);
      g.grid.invalidate();
      if (active && active.row < items.length) {
        g.grid.setActiveCell(active.row, active.cell);
      }
    });
    if (origApplyTP) origApplyTP(tps);
  };

  // setupGrid инициализирует один грид. Вынесено из цикла в отдельную функцию,
  // чтобы каждый грид замыкал свои grid/dataView/tpName (иначе при нескольких
  // ТЧ на форме все подписки замыкали бы последний грид — классический баг var
  // в цикле).
  function setupGrid(div) {
    var tpName = div.getAttribute("data-sg-tp");
    if (window._obGrids[tpName]) return;

    var colsRaw = JSON.parse(div.getAttribute("data-sg-cols") || "[]");
    var refOpts = JSON.parse(div.getAttribute("data-sg-ref") || "null") || {};
    var rowsRaw = JSON.parse(div.getAttribute("data-sg-rows") || "[]");

    var columns = buildColumns(colsRaw, refOpts);
    var items = rowsRaw.map(function(r, idx) {
      var item = {id: idx};
      for (var i = 0; i < colsRaw.length; i++) item[colsRaw[i].id] = r[colsRaw[i].id] || "";
      return item;
    });

    var dataView = new Slick.Data.DataView();
    dataView.setItems(items);

    var options = {
      enableCellNavigation: true,
      enableColumnReorder: false,
      editable: true,
      autoEdit: true,
      autoHeight: false,
      rowHeight: 28,
      headerRowHeight: 30,
      syncColumnCellResize: true,
      enableTextSelectionOnCells: true,
      enableAddRow: false,
      multiSelect: true
    };

    var grid = new Slick.Grid(div, dataView, columns, options);
    dataView.onRowCountChanged.subscribe(function() { grid.updateRowCount(); grid.render(); });
    dataView.onRowsChanged.subscribe(function(e, args) { grid.invalidateRows(args.rows); grid.render(); });

    // Delete key removes selected rows
    grid.onKeyDown.subscribe(function(e) {
      if (e.key === 'Delete' && !grid.getEditorLock().isActive()) {
        var sel = grid.getSelectedRows();
        if (sel && sel.length) {
          var its = dataView.getItems();
          var toRemove = sel.map(function(r) { return its[r]; });
          for (var i = 0; i < toRemove.length; i++) dataView.deleteItem(toRemove[i].id);
          grid.invalidate();
          grid.setSelectedRows([]);
          e.stopImmediatePropagation();
        }
      }
    });

    // План 48 (Р2): серверный пересчёт зависимых колонок (Сумма = Кол × Цена)
    // через round-trip. Дёргаем obFire('ПриИзменении') только если у элемента
    // ТЧ есть такой обработчик (data-sg-recalc) — иначе впустую гоняли бы сеть
    // на каждый ввод. Debounce 250 мс коалесцирует быстрые правки (вопрос O3).
    // Деньги считаются на сервере (decimal), клиент лишь отображает результат.
    if (div.getAttribute("data-sg-recalc") === "1") {
      var elName = div.getAttribute("data-sg-el") || tpName;
      var recalcTimer = null;
      grid.onCellChange.subscribe(function() {
        if (recalcTimer) clearTimeout(recalcTimer);
        recalcTimer = setTimeout(function() {
          if (window.obFire) window.obFire(elName, "ПриИзменении", {_tp: tpName});
        }, 250);
      });
    }

    window._obGrids[tpName] = {
      grid: grid,
      dataView: dataView,
      columns: columns,
      columnsMeta: colsRaw,
      refOpts: refOpts
    };

    grid.autosizeColumns();
  }

  // Initialize all grids
  function initGrids() {
    var divs = document.querySelectorAll(".ob-grid[data-sg-tp]");
    for (var d = 0; d < divs.length; d++) setupGrid(divs[d]);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initGrids);
  } else {
    initGrids();
  }
})();</script>
{{end}}

{{template "form-shared-js" .}}

{{/* Авто-вызов ПриОткрытииФормы при загрузке страницы. Без этого
     серверный handler на event="ПриОткрытии" никогда не запустится —
     браузер не генерирует такое событие. План 37, этап 8. */}}
{{if hasFormHandler .Form "ПриОткрытии"}}
<script>
document.addEventListener('DOMContentLoaded', function(){
  // setTimeout 0 → даём obFire-IIFE выше зарегистрировать window.obFire.
  setTimeout(function(){ if (window.obFire) obFire('', 'ПриОткрытии'); }, 0);
});
</script>
{{end}}

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
