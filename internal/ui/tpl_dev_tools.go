package ui

const tplQueryConsole = `
{{define "page-query-console"}}
{{template "head" .}}{{template "nav" .}}
<main style="max-width:100%">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
  <h2 style="margin:0">Консоль запросов</h2>
  <div style="display:flex;gap:8px;align-items:center">
    <button onclick="qcExec()" class="btn" style="background:#3b82f6;color:#fff;border:none;border-radius:6px;padding:6px 16px;cursor:pointer;font-size:13px">▶ Выполнить</button>
    <button onclick="qcToggleBuilder()" class="btn" style="background:#e2e8f0;color:#475569;border:none;border-radius:6px;padding:6px 16px;cursor:pointer;font-size:13px" id="qc-builder-btn">Конструктор</button>
    <button onclick="qcClear()" style="background:none;border:1px solid #e2e8f0;border-radius:6px;padding:6px 12px;cursor:pointer;font-size:13px;color:#64748b">Очистить</button>
    <span id="qc-status" style="font-size:12px;color:#64748b"></span>
  </div>
</div>

<!-- Builder panel (collapsible) -->
<div id="qc-builder" style="display:none;margin-bottom:12px">
<div style="display:grid;grid-template-columns:380px 1fr;gap:16px;align-items:start">

<!-- LEFT: builder panels -->
<div>
<div class="card" style="margin-bottom:10px">
<h3 style="margin-top:0">Источник данных</h3>
<select id="qb-src" onchange="qbSetSource(this.value)" style="width:100%;margin-bottom:8px">
  <option value="">— выбрать —</option>
</select>
<div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">
  <span style="font-size:12px;color:#64748b;flex-shrink:0;width:70px">Псевдоним:</span>
  <input id="qb-main-alias" type="text" placeholder="напр. Прод" oninput="qbRebuildAllFields()"
    style="width:110px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px 6px">
</div>
<div id="qb-vt-param" style="display:none;margin-top:4px">
  <label style="font-size:12px;color:#64748b">Параметры виртуальной таблицы</label>
  <input id="qb-vt-param-val" type="text" style="width:100%;margin-top:4px" placeholder="например: &amp;НаДату">
</div>
</div>

<div class="card" style="margin-bottom:10px">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
  <h3 style="margin:0">Соединения</h3>
  <button onclick="qbAddJoin()" style="background:#dbeafe;color:#1d4ed8;border:none;border-radius:4px;padding:2px 8px;font-size:12px;cursor:pointer">+ Добавить</button>
</div>
<div id="qb-joins"><p style="font-size:12px;color:#94a3b8;margin:0" id="qb-joins-hint">Нет соединений</p></div>
</div>

<div class="card" style="margin-bottom:10px">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
  <h3 style="margin:0">Поля (ВЫБРАТЬ)</h3>
  <div style="display:flex;gap:4px">
    <button onclick="qbAllFields(true)" style="background:#e2e8f0;color:#475569;border:none;border-radius:4px;padding:2px 8px;font-size:12px;cursor:pointer">Все</button>
    <button onclick="qbAllFields(false)" style="background:#e2e8f0;color:#475569;border:none;border-radius:4px;padding:2px 8px;font-size:12px;cursor:pointer">Сброс</button>
  </div>
</div>
<div id="qb-fields-list" style="max-height:200px;overflow-y:auto"></div>
</div>

<div class="card" style="margin-bottom:10px">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
  <h3 style="margin:0">Условия (ГДЕ)</h3>
  <button onclick="qbAddCond()" style="background:#dbeafe;color:#1d4ed8;border:none;border-radius:4px;padding:2px 8px;font-size:12px;cursor:pointer">+ Условие</button>
</div>
<div id="qb-conds"></div>
</div>

<div class="card">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
  <h3 style="margin:0">Сортировка</h3>
  <button onclick="qbAddOrder()" style="background:#dbeafe;color:#1d4ed8;border:none;border-radius:4px;padding:2px 8px;font-size:12px;cursor:pointer">+ Поле</button>
</div>
<div id="qb-orders"></div>
</div>
</div><!-- /LEFT -->

<!-- RIGHT: params + apply -->
<div>
<div class="card">
<h3 style="margin-top:0">Параметры запроса</h3>
<p style="font-size:12px;color:#64748b;margin-bottom:8px">Значения &amp;Параметров для выполнения</p>
<div id="qc-params" style="font-size:13px">—</div>
<div style="margin-top:12px">
  <button onclick="qbApplyToEditor()" style="background:#3b82f6;color:#fff;border:none;border-radius:6px;padding:6px 16px;cursor:pointer;font-size:13px">Применить к редактору</button>
</div>
</div>
</div>

</div><!-- /grid -->
</div><!-- /qc-builder -->

<!-- Monaco editor -->
<div class="card" style="margin-bottom:12px">
<div id="qc-editor" style="height:260px;border:1px solid #e2e8f0;border-radius:6px"></div>
</div>

<!-- Parameters (always visible) -->
<div class="card" style="margin-bottom:12px" id="qc-params-card">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
  <h3 style="margin:0">Параметры</h3>
  <button onclick="qcDetectParams()" style="background:#dbeafe;color:#1d4ed8;border:none;border-radius:4px;padding:4px 12px;cursor:pointer;font-size:12px">Заполнить из запроса</button>
</div>
<div id="qc-params" style="font-size:13px"><span style="color:#94a3b8">Нажмите «Заполнить из запроса» или введите вручную</span></div>
</div>

<!-- Results -->
<div class="card" id="qc-results-card" style="display:none">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
  <h3 style="margin:0">Результат</h3>
  <span id="qc-result-info" style="font-size:12px;color:#64748b"></span>
</div>
<div style="overflow-x:auto;max-height:400px;overflow-y:auto">
<table id="qc-result-table" class="tbl" style="font-size:13px;white-space:nowrap">
  <thead id="qc-thead"></thead>
  <tbody id="qc-tbody"></tbody>
</table>
</div>
</div>

<div id="qc-error" class="card" style="display:none;border-color:#fca5a5;background:#fef2f2">
  <pre style="margin:0;font-size:13px;color:#991b1b;white-space:pre-wrap"></pre>
</div>

</main>

<script src="https://cdn.jsdelivr.net/npm/monaco-editor@0.52/min/vs/loader.js"></script>
<script>
var _schema = {{.Schema}};
var _srcMap = {};
_schema.forEach(function(s){ _srcMap[s.id] = s; });

require.config({ paths: { 'vs': 'https://cdn.jsdelivr.net/npm/monaco-editor@0.52/min/vs' }});
require(['vs/editor/editor.main'], function() {
  monaco.languages.register({ id: 'onebase-query' });
  monaco.languages.setMonarchTokensProvider('onebase-query', {
    keywords: ['ВЫБРАТЬ','ИЗ','ГДЕ','СГРУППИРОВАТЬ','УПОРЯДОЧИТЬ','ПО','ИМЕЯ','КАК','И','ИЛИ','НЕ','В','ВЫБОР','КОГДА','ТОГДА','ИНАЧЕ','КОНЕЦ','УБЫВ','ВОЗР','РАЗЛИЧНЫЕ','ЕСТЬ','ПУСТО','ОБЪЕДИНИТЬ','ВСЕ','ЛЕВОЕ','ВНУТРЕННЕЕ','ПРАВОЕ','ПОЛНОЕ','СОЕДИНЕНИЕ','СУММА','КОЛИЧЕСТВО','МИНИМУМ','МАКСИМУМ','СРЕДНЕЕ','SELECT','FROM','WHERE','GROUP','ORDER','BY','ON','AND','OR','NOT','IN','AS','JOIN','INNER','LEFT','RIGHT','FULL','HAVING'],
    tokenizer: {
      root: [
        [/&[А-Яа-яёЁA-Za-z_]\w*/, 'variable.predefined'],
        [/"[^"]*"/, 'string'],
        [/'[^']*'/, 'string'],
        [/\d+(\.\d+)?/, 'number'],
        [/[A-Za-zА-Яа-яёЁ_]\w*/, { cases: { '@keywords': 'keyword', '@default': 'identifier' } }],
      ]
    }
  });
  window.qcEditor = monaco.editor.create(document.getElementById('qc-editor'), {
    value: 'ВЫБРАТЬ *\nИЗ ',
    language: 'onebase-query',
    theme: 'vs',
    minimap: { enabled: false },
    automaticLayout: true,
    fontSize: 14,
    lineNumbers: 'on',
    scrollBeyondLastLine: false
  });
});

function qcToggleBuilder() {
  var el = document.getElementById('qc-builder');
  var btn = document.getElementById('qc-builder-btn');
  if (el.style.display === 'none') {
    el.style.display = '';
    btn.style.background = '#3b82f6';
    btn.style.color = '#fff';
  } else {
    el.style.display = 'none';
    btn.style.background = '#e2e8f0';
    btn.style.color = '#475569';
  }
}

function qcExec() {
  var code = window.qcEditor.getValue();
  // Auto-detect params from query if panel is empty
  var hasParams = document.querySelectorAll('.qc-param-row').length > 0;
  if (!hasParams) qcDetectParams();
  // Collect params
  var params = {};
  var emptyParams = [];
  document.querySelectorAll('.qc-param-row').forEach(function(row) {
    var k = row.querySelector('.qc-pk').value.trim();
    var v = row.querySelector('.qc-pv').value;
    var t = row.dataset.type || 'string';
    if (k) {
      if (v === '') { emptyParams.push(k); return; }
      if (t === 'number') { var n = Number(v); params[k] = isNaN(n) ? v : n; }
      else params[k] = v;
    }
  });
  if (emptyParams.length > 0) {
    document.getElementById('qc-error').style.display = '';
    document.getElementById('qc-error').querySelector('pre').textContent = 'Заполните параметры: ' + emptyParams.join(', ');
    return;
  }
  document.getElementById('qc-status').textContent = 'Выполнение...';
  fetch('/ui/dev/query-exec', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({query: code, params: params})
  }).then(function(r){ return r.json(); }).then(function(data) {
    document.getElementById('qc-status').textContent = '';
    var errDiv = document.getElementById('qc-error');
    var resCard = document.getElementById('qc-results-card');
    if (data.error) {
      errDiv.style.display = '';
      errDiv.querySelector('pre').textContent = data.error;
      resCard.style.display = 'none';
      return;
    }
    errDiv.style.display = 'none';
    resCard.style.display = '';
    document.getElementById('qc-result-info').textContent =
      (data.count || 0) + ' строк' + (data.time ? ', ' + data.time : '');

    var cols = data.columns || [];
    var thead = document.getElementById('qc-thead');
    thead.innerHTML = '<tr>' + cols.map(function(c){ return '<th>' + escHtml(c) + '</th>'; }).join('') + '</tr>';

    var tbody = document.getElementById('qc-tbody');
    tbody.innerHTML = '';
    (data.rows || []).forEach(function(row) {
      var tr = document.createElement('tr');
      row.forEach(function(cell) {
        var td = document.createElement('td');
        td.textContent = cell == null ? '' : String(cell);
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    });
  }).catch(function(e) {
    document.getElementById('qc-status').textContent = 'Ошибка сети';
  });
}

function qcClear() {
  if (window.qcEditor) window.qcEditor.setValue('ВЫБРАТЬ *\nИЗ ');
  document.getElementById('qc-results-card').style.display = 'none';
  document.getElementById('qc-error').style.display = 'none';
  document.getElementById('qc-params').innerHTML = '<span style="color:#94a3b8">Нажмите «Заполнить из запроса» или введите вручную</span>';
}

function qcDetectParams() {
  var code = window.qcEditor.getValue();
  var re = /&([А-Яа-яёЁA-Za-z_]\w*)/g;
  var found = {};
  var m;
  while ((m = re.exec(code)) !== null) { found[m[1]] = true; }
  var names = Object.keys(found);
  if (!names.length) {
    document.getElementById('qc-params').innerHTML = '<span style="color:#94a3b8">Параметры не найдены</span>';
    return;
  }
  // Ask backend to detect types
  fetch('/ui/dev/query-analyze', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({query: code})
  }).then(function(r){ return r.json(); }).then(function(data) {
    var types = data.paramTypes || {};
    var existing = {};
    document.querySelectorAll('.qc-param-row').forEach(function(row) {
      var k = row.querySelector('.qc-pk').value.trim();
      existing[k] = row.querySelector('.qc-pv').value;
    });
    var html = '';
    names.forEach(function(name) {
      var t = types[name] || 'string';
      var prev = existing[name] || '';
      var hint = t === 'uuid' ? 'UUID' : t === 'number' ? 'Число' : t === 'date' ? 'Дата' : 'Строка';
      html += '<div class="qc-param-row" data-type="'+t+'" style="display:flex;gap:8px;align-items:center;margin-bottom:6px">'
        + '<span style="width:140px;font-size:13px;font-weight:600;color:#334155">&amp;'+escHtml(name)+'</span>'
        + '<span style="font-size:11px;color:#94a3b8;width:60px">'+hint+'</span>'
        + '<input class="qc-pv" value="'+escHtml(prev)+'" placeholder="значение" style="flex:1;font-size:13px;border:1px solid #e2e8f0;border-radius:4px;padding:4px 8px">'
        + '</div>';
    });
    document.getElementById('qc-params').innerHTML = html;
  }).catch(function() {
    // Fallback: just show string params without type info
    var html = '';
    names.forEach(function(name) {
      var prev = '';
      document.querySelectorAll('.qc-param-row').forEach(function(row) {
        if (row.querySelector('.qc-pk') && row.querySelector('.qc-pk').value.trim() === name) prev = row.querySelector('.qc-pv').value;
      });
      html += '<div class="qc-param-row" data-type="string" style="display:flex;gap:8px;align-items:center;margin-bottom:6px">'
        + '<span style="width:140px;font-size:13px;font-weight:600;color:#334155">&amp;'+escHtml(name)+'</span>'
        + '<span style="font-size:11px;color:#94a3b8;width:60px">Строка</span>'
        + '<input class="qc-pv" value="'+escHtml(prev)+'" placeholder="значение" style="flex:1;font-size:13px;border:1px solid #e2e8f0;border-radius:4px;padding:4px 8px">'
        + '</div>';
    });
    document.getElementById('qc-params').innerHTML = html;
  });
}

function escHtml(s) {
  var d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

// ─── Query Builder JS (shared with standalone builder) ────────────────────
(function(){
  var sel = document.getElementById('qb-src');
  var groups = {};
  _schema.forEach(function(s){
    if(!groups[s.group]) groups[s.group]=[];
    groups[s.group].push(s);
  });
  Object.keys(groups).forEach(function(g){
    var og = document.createElement('optgroup'); og.label = g;
    groups[g].forEach(function(s){
      var o = document.createElement('option'); o.value=s.id; o.textContent=s.label;
      og.appendChild(o);
    });
    sel.appendChild(og);
  });
})();

var _curFields = [];
var _selFields = {};
var _joins = [];

function qbSetSource(id){
  var src = _srcMap[id];
  _selFields = {};
  document.getElementById('qb-conds').innerHTML = '';
  document.getElementById('qb-orders').innerHTML = '';
  _joins = [];
  document.getElementById('qb-joins').innerHTML = '<p style="font-size:12px;color:#94a3b8;margin:0" id="qb-joins-hint">Нет соединений</p>';
  document.getElementById('qb-main-alias').value = '';
  var vtDiv = document.getElementById('qb-vt-param');
  if(src && src.vtParam){ vtDiv.style.display=''; document.getElementById('qb-vt-param-val').value=src.vtParam; }
  else { vtDiv.style.display='none'; }
  qbRebuildAllFields();
}

function qbRebuildAllFields(){
  var srcId = document.getElementById('qb-src').value;
  var mainSrc = _srcMap[srcId];
  var mainAlias = document.getElementById('qb-main-alias').value.trim();
  var all = [];
  if(mainSrc){
    mainSrc.fields.forEach(function(f){
      var name = mainAlias ? mainAlias+'.'+f.name : f.name;
      all.push({name: name, label: name, type: f.type});
    });
    if(mainAlias) all.push({name: mainAlias+'.Ссылка', label: mainAlias+'.Ссылка (id)', type: 'ref'});
  }
  _joins.forEach(function(j){
    var src = _srcMap[j.srcSel.value]; var alias = j.aliasInp.value.trim();
    if(!src || !alias) return;
    src.fields.forEach(function(f){ all.push({name: alias+'.'+f.name, label: alias+'.'+f.name, type: f.type}); });
    all.push({name: alias+'.Ссылка', label: alias+'.Ссылка (id)', type: 'ref'});
  });
  var nameSet = {}; all.forEach(function(f){ nameSet[f.name]=true; });
  var newSel = {};
  Object.keys(_selFields).forEach(function(k){ if(nameSet[k]) newSel[k]=_selFields[k]; });
  if(!Object.keys(newSel).length && mainSrc){
    all.forEach(function(f){ if(!f.name.endsWith('.Ссылка') && f.name !== 'Ссылка') newSel[f.name]={alias:'',agg:''}; });
  }
  _selFields = newSel; _curFields = all;
  renderFields(); qbGenerate();
}

function qbAddJoin(){
  var mainAliasInp = document.getElementById('qb-main-alias');
  if(!mainAliasInp.value.trim()){
    var mainSrc = _srcMap[document.getElementById('qb-src').value];
    if(mainSrc){ var parts=mainSrc.label.split('.'); mainAliasInp.value=parts.length>=2?parts[1].replace(/\(.*$/,''):parts[0]; }
  }
  var hint = document.getElementById('qb-joins-hint'); if(hint) hint.remove();
  var jid = Date.now();
  var div = document.createElement('div');
  div.dataset.jid = jid;
  div.style.cssText = 'border:1px solid #e2e8f0;border-radius:6px;padding:8px;margin-bottom:8px;background:#f8fafc';
  var row1 = document.createElement('div');
  row1.style.cssText = 'display:flex;gap:6px;align-items:center;margin-bottom:6px;flex-wrap:wrap';
  var typeSel = document.createElement('select');
  typeSel.style.cssText = 'width:126px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px 4px';
  [['ЛЕВОЕ','⬅ ЛЕВОЕ'],['ВНУТРЕННЕЕ','✕ ВНУТРЕННЕЕ'],['ПРАВОЕ','➡ ПРАВОЕ'],['ПОЛНОЕ','⟺ ПОЛНОЕ']].forEach(function(x){
    var o = document.createElement('option'); o.value=x[0]; o.textContent=x[1]; typeSel.appendChild(o);
  });
  var srcSel = document.createElement('select');
  srcSel.style.cssText = 'flex:1;min-width:140px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px 4px';
  var o0 = document.createElement('option'); o0.value=''; o0.textContent='— источник —'; srcSel.appendChild(o0);
  var jgroups = {};
  _schema.forEach(function(s){ if(!jgroups[s.group]) jgroups[s.group]=[]; jgroups[s.group].push(s); });
  Object.keys(jgroups).forEach(function(g){
    var og = document.createElement('optgroup'); og.label=g;
    jgroups[g].forEach(function(s){ var o=document.createElement('option'); o.value=s.id; o.textContent=s.label; og.appendChild(o); });
    srcSel.appendChild(og);
  });
  var aliasInp = document.createElement('input');
  aliasInp.type='text'; aliasInp.placeholder='Псевдоним';
  aliasInp.style.cssText = 'width:86px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px 4px';
  var del = document.createElement('button');
  del.type='button'; del.textContent='×';
  del.style.cssText = 'background:none;border:none;color:#ef4444;cursor:pointer;font-size:18px;line-height:1;flex-shrink:0;padding:0 2px';
  del.onclick = function(){
    div.remove(); _joins=_joins.filter(function(j){ return j.id!==jid; });
    if(!_joins.length) document.getElementById('qb-joins').innerHTML='<p style="font-size:12px;color:#94a3b8;margin:0" id="qb-joins-hint">Нет соединений</p>';
    qbRebuildAllFields();
  };
  row1.appendChild(typeSel); row1.appendChild(srcSel); row1.appendChild(aliasInp); row1.appendChild(del);
  var row2 = document.createElement('div');
  row2.style.cssText = 'display:flex;gap:6px;align-items:center';
  var onLabel = document.createElement('span'); onLabel.textContent='ПО:'; onLabel.style.cssText='font-size:12px;font-weight:600;color:#475569;flex-shrink:0;width:26px';
  var onInp = document.createElement('input'); onInp.type='text'; onInp.placeholder='Псевд1.Поле = Псевд2.Ссылка';
  onInp.style.cssText = 'flex:1;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px 6px';
  row2.appendChild(onLabel); row2.appendChild(onInp);
  div.appendChild(row1); div.appendChild(row2);
  document.getElementById('qb-joins').appendChild(div);
  var jdata = {id:jid, el:div, typeSel:typeSel, srcSel:srcSel, aliasInp:aliasInp, onInp:onInp};
  _joins.push(jdata);
  srcSel.onchange = function(){
    var src=_srcMap[srcSel.value]; if(src&&!aliasInp.value.trim()){ var parts=src.label.split('.'); aliasInp.value=parts.length>=2?parts[1].replace(/\(.*$/,''):parts[0]; }
    qbRebuildAllFields();
  };
  typeSel.onchange = function(){ qbGenerate(); };
  aliasInp.oninput = function(){ qbRebuildAllFields(); };
  onInp.oninput = function(){ qbGenerate(); };
  qbRebuildAllFields();
}

function renderFields(){
  var div = document.getElementById('qb-fields-list');
  div.innerHTML = '';
  if(!_curFields.length){ div.innerHTML='<p style="font-size:12px;color:#94a3b8;margin:4px 0">Сначала выберите источник</p>'; return; }
  var lastPrefix = null;
  _curFields.forEach(function(f){
    var dotIdx = f.name.indexOf('.');
    var prefix = dotIdx >= 0 ? f.name.substring(0, dotIdx) : '';
    if(prefix && prefix !== lastPrefix){
      lastPrefix = prefix;
      var sep = document.createElement('div');
      sep.style.cssText = 'font-size:11px;font-weight:600;color:#64748b;margin:6px 0 2px;padding-left:2px;border-top:1px solid #f1f5f9;padding-top:4px';
      sep.textContent = prefix;
      div.appendChild(sep);
    }
    var row = document.createElement('div');
    row.style.cssText = 'display:flex;align-items:center;gap:6px;margin-bottom:3px;font-size:13px;padding:1px 0';
    var chk = document.createElement('input'); chk.type='checkbox'; chk.checked=!!_selFields[f.name]; chk.dataset.field=f.name;
    chk.onchange = function(){ if(chk.checked) _selFields[f.name]={alias:'',agg:''}; else delete _selFields[f.name]; qbGenerate(); };
    var lbl = document.createElement('label');
    lbl.textContent = dotIdx>=0 ? f.name.substring(dotIdx+1) : f.name;
    lbl.title = f.name; lbl.style.cssText='flex:1;cursor:pointer;overflow:hidden;text-overflow:ellipsis;white-space:nowrap';
    lbl.onclick = function(){ chk.click(); };
    var aggSel = document.createElement('select');
    aggSel.style.cssText = 'font-size:11px;padding:1px 3px;border:1px solid #e2e8f0;border-radius:4px;width:90px';
    ['','СУММА','КОЛИЧЕСТВО','МИНИМУМ','МАКСИМУМ','СРЕДНЕЕ'].forEach(function(a){
      var o=document.createElement('option'); o.value=a; o.textContent=a||'— нет —'; aggSel.appendChild(o);
    });
    if(_selFields[f.name]) aggSel.value=_selFields[f.name].agg||'';
    aggSel.onchange = function(){ if(_selFields[f.name]) _selFields[f.name].agg=aggSel.value; qbGenerate(); };
    var aliasInp = document.createElement('input'); aliasInp.type='text'; aliasInp.placeholder='КАК';
    aliasInp.style.cssText='font-size:11px;width:70px;padding:1px 4px;border:1px solid #e2e8f0;border-radius:4px';
    if(_selFields[f.name]) aliasInp.value=_selFields[f.name].alias||'';
    aliasInp.oninput = function(){ if(_selFields[f.name]) _selFields[f.name].alias=aliasInp.value.trim(); qbGenerate(); };
    row.appendChild(chk); row.appendChild(lbl); row.appendChild(aggSel); row.appendChild(aliasInp);
    div.appendChild(row);
  });
}

function qbAllFields(v){
  _curFields.forEach(function(f){
    if(f.name.endsWith('.Ссылка')||f.name==='Ссылка') return;
    if(v) _selFields[f.name]={alias:'',agg:''}; else delete _selFields[f.name];
  });
  renderFields(); qbGenerate();
}

function qbAddCond(){
  var div = document.createElement('div');
  div.style.cssText = 'display:flex;gap:4px;margin-bottom:6px;align-items:center;flex-wrap:wrap';
  var fsel = document.createElement('select');
  fsel.style.cssText = 'flex:1;min-width:100px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px';
  _curFields.forEach(function(f){ var o=document.createElement('option'); o.value=f.name; o.textContent=f.name; fsel.appendChild(o); });
  var opSel = document.createElement('select');
  opSel.style.cssText = 'width:100px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px';
  ['=','<>','>','<','>=','<=','ЕСТЬ ПУСТО','НЕ ЕСТЬ ПУСТО','ПОДОБНО','В'].forEach(function(op){
    var o=document.createElement('option'); o.value=op; o.textContent=op; opSel.appendChild(o);
  });
  var valInp = document.createElement('input'); valInp.type='text'; valInp.placeholder='&Параметр или значение';
  valInp.style.cssText = 'flex:1;min-width:80px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px 4px';
  opSel.onchange = function(){ var noVal=opSel.value==='ЕСТЬ ПУСТО'||opSel.value==='НЕ ЕСТЬ ПУСТО'; valInp.style.display=noVal?'none':''; qbGenerate(); };
  fsel.onchange = valInp.oninput = function(){ qbGenerate(); };
  var del = document.createElement('button'); del.type='button'; del.textContent='×';
  del.style.cssText = 'background:none;border:none;color:#ef4444;cursor:pointer;font-size:16px;line-height:1';
  del.onclick = function(){ div.remove(); qbGenerate(); };
  div.appendChild(fsel); div.appendChild(opSel); div.appendChild(valInp); div.appendChild(del);
  document.getElementById('qb-conds').appendChild(div);
  qbGenerate();
}

function qbAddOrder(){
  var div = document.createElement('div');
  div.style.cssText = 'display:flex;gap:4px;margin-bottom:6px;align-items:center';
  var fsel = document.createElement('select');
  fsel.style.cssText = 'flex:1;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px';
  _curFields.forEach(function(f){ var o=document.createElement('option'); o.value=f.name; o.textContent=f.name; fsel.appendChild(o); });
  var dirSel = document.createElement('select');
  dirSel.style.cssText = 'width:80px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:2px';
  [['ВОЗР','↑ ВОЗР'],['УБЫВ','↓ УБЫВ']].forEach(function(x){ var o=document.createElement('option'); o.value=x[0]; o.textContent=x[1]; dirSel.appendChild(o); });
  var del = document.createElement('button'); del.type='button'; del.textContent='×';
  del.style.cssText = 'background:none;border:none;color:#ef4444;cursor:pointer;font-size:16px;line-height:1';
  del.onclick = function(){ div.remove(); qbGenerate(); };
  fsel.onchange = dirSel.onchange = function(){ qbGenerate(); };
  div.appendChild(fsel); div.appendChild(dirSel); div.appendChild(del);
  document.getElementById('qb-orders').appendChild(div);
  qbGenerate();
}

function qbGenerate(){
  var srcId = document.getElementById('qb-src').value;
  var src = _srcMap[srcId];
  if(!src) return;

  var mainAlias = document.getElementById('qb-main-alias').value.trim();
  var activeJoins = _joins.filter(function(j){ return !!j.srcSel.value && !!j.aliasInp.value.trim(); });
  var hasJoins = activeJoins.length > 0;

  var selParts = [];
  var hasAgg = false;
  var groupFields = [];
  _curFields.forEach(function(f){
    var info = _selFields[f.name]; if(!info) return;
    var expr = f.name;
    if(info.agg){ expr = info.agg+'('+f.name+')'; hasAgg = true; } else { groupFields.push(f.name); }
    if(info.alias) expr += ' КАК '+info.alias;
    selParts.push('  '+expr);
  });
  if(!selParts.length) selParts=['  *'];

  var fromClause = src.label;
  if(src.vtParam){
    var vtVal = document.getElementById('qb-vt-param-val').value.trim() || src.vtParam;
    fromClause = fromClause.replace(/\(.*?\)/,'('+vtVal+')');
  }
  if(mainAlias || hasJoins) fromClause += ' КАК '+(mainAlias||'Т');

  activeJoins.forEach(function(j){
    var jSrc = _srcMap[j.srcSel.value]; var jAlias = j.aliasInp.value.trim(); var jLabel = jSrc.label;
    var onCond = j.onInp.value.trim();
    fromClause += '\n  '+j.typeSel.value+' СОЕДИНЕНИЕ '+jLabel+' КАК '+jAlias;
    fromClause += '\n  ПО '+(onCond||'/* условие */');
  });

  var whereParts = [];
  var params = {};
  document.getElementById('qb-conds').querySelectorAll('div').forEach(function(row){
    var sels = row.querySelectorAll('select');
    var inp = row.querySelector('input[type=text]');
    if(!sels[0]) return;
    var field = sels[0].value; var op = sels[1]?sels[1].value:'=';
    var val = (inp && inp.style.display!=='none') ? inp.value.trim() : '';
    if(op==='ЕСТЬ ПУСТО'||op==='НЕ ЕСТЬ ПУСТО'){ whereParts.push(field+' '+op); }
    else if(val){ var m=val.match(/&[А-Яа-яёЁA-Za-z_]\w*/g); if(m) m.forEach(function(p){ params[p]=true; }); whereParts.push(op==='В'?field+' В ('+val+')':field+' '+op+' '+val); }
  });
  activeJoins.forEach(function(j){ var m=j.onInp.value.match(/&[А-Яа-яёЁA-Za-z_]\w*/g); if(m) m.forEach(function(p){ params[p]=true; }); });

  var orderParts = [];
  document.getElementById('qb-orders').querySelectorAll('div').forEach(function(row){
    var sels = row.querySelectorAll('select'); if(!sels[0]) return;
    var f=sels[0].value; var d=sels[1]?sels[1].value:'ВОЗР';
    orderParts.push(d==='УБЫВ'?f+' УБЫВ':f);
  });

  var q = 'ВЫБРАТЬ\n'+selParts.join(',\n')+'\nИЗ '+fromClause;
  if(whereParts.length) q += '\nГДЕ '+whereParts.join('\n  И ');
  if(hasAgg && groupFields.length) q += '\nСГРУППИРОВАТЬ ПО '+groupFields.join(', ');
  if(orderParts.length) q += '\nУПОРЯДОЧИТЬ ПО '+orderParts.join(', ');

  // Update params UI
  var pList = Object.keys(params);
  var paramDiv = document.getElementById('qc-params');
  paramDiv.innerHTML = '';
  if(!pList.length){ paramDiv.innerHTML='—'; return; }
  pList.forEach(function(p){
    var row = document.createElement('div');
    row.className = 'qc-param-row';
    row.style.cssText = 'display:flex;gap:8px;align-items:center;margin-bottom:6px';
    row.innerHTML = '<input class="qc-pk" value="'+p+'" readonly style="width:120px;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:4px 6px;background:#f1f5f9">'
      + '<input class="qc-pv" placeholder="значение" style="flex:1;font-size:12px;border:1px solid #e2e8f0;border-radius:4px;padding:4px 6px">';
    paramDiv.appendChild(row);
  });

  // Store generated query for apply
  window._qbGeneratedQuery = q;
}

function qbApplyToEditor(){
  if(window._qbGeneratedQuery && window.qcEditor){
    window.qcEditor.setValue(window._qbGeneratedQuery);
  }
}

document.getElementById('qb-vt-param-val').oninput = qbGenerate;
</script>
</div></body></html>
{{end}}
`

const tplCodeConsole = `
{{define "page-code-console"}}
{{template "head" .}}{{template "nav" .}}
<main style="max-width:100%">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
  <h2 style="margin:0">Консоль кода</h2>
  <div style="display:flex;gap:8px;align-items:center">
    <button onclick="ccExec()" class="btn" style="background:#3b82f6;color:#fff;border:none;border-radius:6px;padding:6px 16px;cursor:pointer;font-size:13px">▶ Выполнить</button>
    <button onclick="ccClear()" style="background:none;border:1px solid #e2e8f0;border-radius:6px;padding:6px 12px;cursor:pointer;font-size:13px;color:#64748b">Очистить</button>
  </div>
</div>

<div class="card" style="margin-bottom:12px">
<div id="cc-editor" style="height:320px;border:1px solid #e2e8f0;border-radius:6px"></div>
</div>

<div class="card">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
  <h3 style="margin:0">Результат</h3>
  <button onclick="document.getElementById('cc-output').innerHTML=''" style="background:none;border:1px solid #e2e8f0;border-radius:4px;padding:2px 8px;cursor:pointer;font-size:12px;color:#64748b">Очистить</button>
</div>
<div id="cc-output" style="background:#1e293b;color:#e2e8f0;font-family:'Cascadia Code',Consolas,monospace;font-size:13px;padding:12px 16px;border-radius:6px;min-height:120px;max-height:400px;overflow-y:auto;white-space:pre-wrap;word-break:break-word"></div>
</div>

</main>

<script src="https://cdn.jsdelivr.net/npm/monaco-editor@0.52/min/vs/loader.js"></script>
<script>
require.config({ paths: { 'vs': 'https://cdn.jsdelivr.net/npm/monaco-editor@0.52/min/vs' }});
require(['vs/editor/editor.main'], function() {
  monaco.languages.register({ id: 'onebase-dsl' });
  monaco.languages.setMonarchTokensProvider('onebase-dsl', {
    keywords: ['Процедура','КонецПроцедуры','Функция','КонецФункции','Если','Тогда','ИначеЕсли','Иначе','КонецЕсли','Для','Каждого','Из','Цикл','КонецЦикла','Пока','По','Прервать','Продолжить','Перем','Новый','Попытка','Исключение','КонецПопытки','ВызватьИсключение','Возврат','Экспорт','И','Или','Не','Null','Неопределено','Истина','Ложь'],
    builtin: ['Сообщить','Строка','Число','Окр','Мин','Макс','Формат','Найти','СтрДлина','Лев','Прав','Сред','ВРег','НРег','СокрЛП','ТипЗнч','Запрос','Массив','ТаблицаЗначений','Соответствие','ЗаписьJSON','ЧтениеJSON'],
    tokenizer: {
      root: [
        [/\/\/.*$/, 'comment'],
        [/"[^"]*"/, 'string'],
        [/'[^']*'/, 'string'],
        [/\d+(\.\d+)?/, 'number'],
        [/&[А-Яа-яёЁA-Za-z_]\w*/, 'variable.predefined'],
        [/[A-Za-zА-Яа-яёЁ_]\w*/, { cases: { '@keywords': 'keyword', '@builtin': 'type', '@default': 'identifier' } }],
      ]
    }
  });
  window.ccEditor = monaco.editor.create(document.getElementById('cc-editor'), {
    value: '// Введите DSL-код\nСообщить("Привет из консоли!");\n',
    language: 'onebase-dsl',
    theme: 'vs',
    minimap: { enabled: false },
    automaticLayout: true,
    fontSize: 14,
    lineNumbers: 'on',
    scrollBeyondLastLine: false
  });
});

function ccExec() {
  var code = window.ccEditor.getValue();
  var out = document.getElementById('cc-output');
  out.innerHTML += '<div style="color:#94a3b8;border-bottom:1px solid #334155;padding:2px 0;font-size:11px">--- ' + new Date().toLocaleTimeString() + ' ---</div>';
  fetch('/ui/dev/code-exec', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({code: code})
  }).then(function(r){ return r.json(); }).then(function(data) {
    if(data.output && data.output.length) {
      data.output.forEach(function(m) {
        out.innerHTML += '<div>' + escHtml(m) + '</div>';
      });
    }
    if(data.error) {
      out.innerHTML += '<div style="color:#fca5a5">' + escHtml(data.error) + '</div>';
    }
    if((!data.output || !data.output.length) && !data.error) {
      out.innerHTML += '<div style="color:#94a3b8">(без вывода)</div>';
    }
    out.scrollTop = out.scrollHeight;
  }).catch(function(e) {
    out.innerHTML += '<div style="color:#fca5a5">Ошибка сети: ' + e.message + '</div>';
  });
}

function ccClear() {
  if(window.ccEditor) window.ccEditor.setValue('// Введите DSL-код\n');
  document.getElementById('cc-output').innerHTML = '';
}

function escHtml(s) {
  var d = document.createElement('div'); d.textContent = s; return d.innerHTML;
}
</script>
</div></body></html>
{{end}}
`
