// ai-settings.js — UI настроек ИИ-помощника (провайдеры/модели/профили задач).
// Заменяет сырой JSON-редактор. Конфиг (llm.Config) держится в памяти, рендерится
// формами; «Сохранить» шлёт конфиг целиком на .../ai/save (ключи под масками ****
// объединяются с сохранёнными на сервере). Файл — IIFE, безопасен к повторному
// исполнению: cfgAdmin пересоздаёт <script> при каждом открытии админ-панели.
(function () {
  var root = document.getElementById('ai-settings-root');
  if (!root) return;
  var baseId = root.getAttribute('data-base');
  var cfg;
  try { cfg = JSON.parse(root.getAttribute('data-cfg') || '{}'); }
  catch (e) { root.innerHTML = '<div style="color:#c00">Повреждённый конфиг: ' + e.message + '</div>'; return; }
  normalizeConfig();

  var KINDS = ['anthropic', 'gemini', 'openai', 'compatible'];
  var jsonMode = false;
  var saveURL = '/bases/' + baseId + '/configurator/admin/ai/save';
  var testURL = '/bases/' + baseId + '/configurator/admin/ai/test';
  var warnBox = null;
  var openSections = { endpoints: true, models: false };
  var editingEndpointIndex = -1;
  var editingModelIndex = -1;
  var activeEditors = [];

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }
  function el(html) { var d = document.createElement('div'); d.innerHTML = html; return d.firstElementChild; }
  function modelByName(n) { for (var i = 0; i < cfg.models.length; i++) if (cfg.models[i].name === n) return cfg.models[i]; return null; }
  function modelEndpoint(m) { return m.endpoint || m.provider || ''; }
  function setModelEndpoint(m, endpoint) { m.endpoint = endpoint; if (Object.prototype.hasOwnProperty.call(m, 'provider')) delete m.provider; }
  function normalizeModel(m) { if (m && !m.endpoint && m.provider) setModelEndpoint(m, m.provider); return m; }
  function normalizeConfig() {
    cfg = cfg || {};
    cfg.endpoints = cfg.endpoints || [];
    cfg.models = cfg.models || [];
    cfg.profiles = cfg.profiles || [];
    cfg.models.forEach(normalizeModel);
  }
  function uniqueName(base, items, prop) {
    var seen = {};
    items.forEach(function (x) { seen[x[prop]] = true; });
    if (!seen[base]) return base;
    for (var i = 2; ; i++) {
      var n = base + '-' + i;
      if (!seen[n]) return n;
    }
  }
  function commitActiveEditors() {
    var editors = activeEditors.slice();
    activeEditors = [];
    editors.forEach(function (commit) { commit(); });
  }

  // --- предупреждения: обновление без пересборки панели ---
  function updateWarnings() {
    if (!warnBox) return;
    var ws = warnings();
    warnBox.style.display = ws.length ? 'block' : 'none';
    warnBox.innerHTML = ws.map(function (x) { return '⚠ ' + esc(x); }).join('<br>');
  }

  // --- валидация: массив предупреждений ---
  function warnings() {
    var w = [], ep = {};
    cfg.endpoints.forEach(function (e) { ep[e.name] = true; });
    cfg.models.forEach(function (m) {
      var endpoint = modelEndpoint(m);
      if (endpoint && !ep[endpoint]) w.push('Модель «' + m.name + '» ссылается на несуществующего провайдера «' + endpoint + '»');
    });
    cfg.profiles.forEach(function (p) {
      (p.models || []).forEach(function (mn) {
        var m = modelByName(mn);
        if (!m) w.push('Задача «' + p.task + '» ссылается на несуществующую модель «' + mn + '»');
        else if (p.task === 'документы' && !m.vision) w.push('Задача «документы» содержит модель «' + mn + '» без vision');
      });
    });
    return w;
  }

  // --- рендер всей панели ---
  function render() {
    if (jsonMode) return renderJson();
    activeEditors = [];
    root.innerHTML = '';

    var head = el('<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px"></div>');
    var en = el('<label style="display:flex;gap:6px;align-items:center;font-weight:600;font-size:12px"><input type="checkbox"> ИИ-помощник включён</label>');
    en.querySelector('input').checked = !!cfg.enabled;
    en.querySelector('input').onchange = function () { cfg.enabled = this.checked; };
    head.appendChild(en);
    var jb = el('<span style="color:#64748b;font-size:11px;cursor:pointer">⚙ Показать JSON</span>');
    jb.onclick = function () { commitActiveEditors(); jsonMode = true; render(); };
    head.appendChild(jb);
    root.appendChild(head);

    var limits = el('<div style="display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:-2px 0 10px;font-size:12px;color:#334155"></div>');
    var rounds = el('<label style="display:flex;gap:6px;align-items:center">ai.max_tool_rounds <input type="number" min="0" max="64" style="width:70px;padding:3px 5px;border:1px solid #cbd5e1;border-radius:3px;font-size:12px"></label>');
    rounds.querySelector('input').value = cfg.max_tool_rounds || 0;
    rounds.querySelector('input').onchange = function () { cfg.max_tool_rounds = Math.max(0, Math.min(64, parseInt(this.value, 10) || 0)); this.value = cfg.max_tool_rounds || 0; };
    limits.appendChild(rounds);
    limits.appendChild(el('<span style="font-size:11px;color:#64748b">0 = системный дефолт</span>'));
    root.appendChild(limits);

    warnBox = el('<div style="background:#fef2f2;border:1px solid #fecaca;border-radius:4px;padding:6px 8px;margin-bottom:10px;font-size:11px;color:#b91c1c;display:none"></div>');
    root.appendChild(warnBox);
    updateWarnings();

    root.appendChild(sectionTasks());
    root.appendChild(collapsible('endpoints', 'Провайдеры и ключи (' + cfg.endpoints.length + ')', renderEndpoints(), true));
    root.appendChild(collapsible('models', 'Модели (' + cfg.models.length + ')', renderModels(), false));

    var foot = el('<div style="margin-top:14px;display:flex;justify-content:flex-end;align-items:center;gap:10px"><span id="ais-msg" style="font-size:11px"></span><button style="background:#16a34a;color:#fff;border:none;padding:6px 16px;border-radius:3px;cursor:pointer;font-size:12px">Сохранить</button></div>');
    foot.querySelector('button').onclick = save;
    root.appendChild(foot);
  }

  function collapsible(key, title, bodyNode, open) {
    if (openSections[key] == null) openSections[key] = !!open;
    var isOpen = !!openSections[key];
    var d = el('<div style="margin-top:10px"></div>');
    var hdr = el('<div style="padding:6px 8px;background:#f1f5f9;border-radius:4px;cursor:pointer;font-weight:600;font-size:12px">' + (isOpen ? '▾ ' : '▸ ') + esc(title) + '</div>');
    var body = el('<div></div>');
    body.appendChild(bodyNode);
    body.style.display = isOpen ? 'block' : 'none';
    hdr.onclick = function () {
      var vis = body.style.display === 'none';
      openSections[key] = vis;
      body.style.display = vis ? 'block' : 'none';
      hdr.textContent = (vis ? '▾ ' : '▸ ') + title;
    };
    d.appendChild(hdr); d.appendChild(body);
    return d;
  }

  // --- секция «Что делает ИИ» (профили задач) ---
  function sectionTasks() {
    var wrap = el('<div style="margin-top:6px"></div>');
    wrap.appendChild(el('<div style="font-weight:600;font-size:12px;letter-spacing:.03em;margin-bottom:4px">ЧТО ДЕЛАЕТ ИИ</div>'));
    var box = el('<div style="border:1px solid #e2e8f0;border-radius:4px"></div>');
    cfg.profiles.forEach(function (p, idx) { box.appendChild(taskRow(p, idx)); });
    var add = el('<div style="padding:6px 8px;color:#2563eb;cursor:pointer;font-size:12px">+ добавить задачу</div>');
    add.onclick = function () {
      commitActiveEditors();
      var name = prompt('Имя задачи (например: анализ, чат, конфигуратор, документы):', '');
      if (!name) return;
      cfg.profiles.push({ task: name, models: [] }); render();
    };
    box.appendChild(add);
    wrap.appendChild(box);
    return wrap;
  }

  function taskRow(p, idx) {
    var row = el('<div style="border-bottom:1px solid #eee;padding:7px 8px;font-size:12px"></div>');
    var top = el('<div style="display:flex;align-items:center;gap:8px"></div>');
    top.appendChild(el('<span style="min-width:130px;font-weight:600">' + esc(p.task) + '</span>'));
    var chips = el('<span style="flex:1"></span>');
    function redrawChips() {
      chips.innerHTML = '';
      (p.models || []).forEach(function (mn, i) {
        if (i) chips.appendChild(el('<span style="color:#94a3b8"> → </span>'));
        chips.appendChild(el('<span style="background:#eef2ff;border:1px solid #c7d2fe;border-radius:10px;padding:1px 8px">' + esc(mn) + '</span>'));
      });
      if (!(p.models || []).length) chips.appendChild(el('<span style="color:#94a3b8">нет моделей</span>'));
    }
    redrawChips();
    top.appendChild(chips);
    var test = el('<span style="color:#2563eb;cursor:pointer">Проверить</span>');
    var out = el('<span id="ais-task-' + idx + '" style="font-size:11px"></span>');
    test.onclick = function () { runTest(p.task, out); };
    top.appendChild(test);
    top.appendChild(out);
    var edit = el('<span style="color:#94a3b8;cursor:pointer">✎</span>');
    var editor = el('<div style="display:none;margin-top:6px"></div>');
    edit.onclick = function () {
      var vis = editor.style.display === 'none';
      editor.style.display = vis ? 'block' : 'none';
      if (vis) { editor.innerHTML = ''; editor.appendChild(chainEditor(p, idx, redrawChips)); }
    };
    top.appendChild(edit);
    row.appendChild(top);
    row.appendChild(editor);
    return row;
  }

  // --- редактор цепочки: нумерованный список с ↑↓ ---
  function chainEditor(p, idx, onChange) {
    p.models = p.models || [];
    var box = el('<div style="border:1px solid #cbd5e1;border-radius:4px;padding:8px;max-width:420px;background:#f8fafc"></div>');
    function redraw() {
      box.innerHTML = '';
      p.models.forEach(function (mn, i) {
        var m = modelByName(mn);
        var line = el('<div style="display:flex;align-items:center;gap:8px;padding:3px 0"></div>');
        line.appendChild(el('<span style="color:#94a3b8;width:16px">' + (i + 1) + '.</span>'));
        var label = '<b>' + esc(mn) + '</b>' + (m && m.vision ? ' <span style="background:#dcfce7;border-radius:8px;padding:0 6px;font-size:10px">vision</span>' : '') + (i ? ' <span style="color:#94a3b8">— фолбэк</span>' : '');
        line.appendChild(el('<span style="flex:1">' + label + '</span>'));
        var up = el('<span style="cursor:pointer;color:' + (i ? '#475569' : '#cbd5e1') + '">↑</span>');
        var dn = el('<span style="cursor:pointer;color:' + (i < p.models.length - 1 ? '#475569' : '#cbd5e1') + '">↓</span>');
        var rm = el('<span style="cursor:pointer;color:#c00">✕</span>');
        up.onclick = function () { if (i) { var t = p.models[i - 1]; p.models[i - 1] = p.models[i]; p.models[i] = t; redraw(); } };
        dn.onclick = function () { if (i < p.models.length - 1) { var t = p.models[i + 1]; p.models[i + 1] = p.models[i]; p.models[i] = t; redraw(); } };
        rm.onclick = function () { p.models.splice(i, 1); redraw(); };
        line.appendChild(up); line.appendChild(dn); line.appendChild(rm);
        box.appendChild(line);
      });
      var addWrap = el('<div style="margin-top:6px;border-top:1px dashed #cbd5e1;padding-top:6px"></div>');
      var sel = el('<select style="width:100%;padding:3px 6px;border:1px solid #cbd5e1;border-radius:3px;font-size:12px"></select>');
      sel.appendChild(el('<option value="">+ добавить модель…</option>'));
      cfg.models.forEach(function (m) {
        if (p.models.indexOf(m.name) >= 0) return;
        sel.appendChild(el('<option value="' + esc(m.name) + '">' + esc(m.name) + (m.vision ? ' ✓vision' : '') + '</option>'));
      });
      sel.onchange = function () { if (this.value) { p.models.push(this.value); redraw(); } };
      addWrap.appendChild(sel);
      box.appendChild(addWrap);
      if (onChange) onChange();
      updateWarnings();
    }
    redraw();
    return box;
  }

  // --- таблица провайдеров ---
  function renderEndpoints() {
    var t = el('<div style="border:1px solid #e2e8f0;border-top:none"></div>');
    t.appendChild(rowHTML(['Имя', 'Тип', 'Base URL', 'Ключ', ''], true));
    cfg.endpoints.forEach(function (e, i) { t.appendChild(endpointRow(e, i)); });
    var add = el('<div style="padding:6px 8px;color:#2563eb;cursor:pointer;font-size:12px">+ добавить провайдера</div>');
    add.onclick = function () {
      commitActiveEditors();
      var idx = cfg.endpoints.length;
      cfg.endpoints.push({ name: uniqueName('new', cfg.endpoints, 'name'), kind: 'anthropic', base_url: '', api_key: '' });
      openSections.endpoints = true;
      editingEndpointIndex = idx;
      editingModelIndex = -1;
      render();
    };
    t.appendChild(add);
    return t;
  }
  function endpointRow(e, i) {
    if (editingEndpointIndex === i) return endpointEditRow(e, i);
    var r = el('<div style="display:flex;padding:5px 8px;align-items:center;font-size:12px' + (i % 2 ? ';background:#f9fafb' : '') + '"></div>');
    r.appendChild(el('<span style="flex:2">' + esc(e.name) + '</span>'));
    r.appendChild(el('<span style="flex:2">' + esc(e.kind) + '</span>'));
    r.appendChild(el('<span style="flex:3">' + esc(e.base_url || '—') + '</span>'));
    r.appendChild(el('<span style="flex:2">' + esc(e.api_key || '') + '</span>'));
    var act = el('<span style="width:46px;display:flex;gap:8px;justify-content:flex-end"></span>');
    var edit = el('<span style="color:#94a3b8;cursor:pointer" title="редактировать">✎</span>');
    var del = el('<span style="color:#c00;cursor:pointer" title="удалить">🗑</span>');
    edit.onclick = function () { commitActiveEditors(); editingEndpointIndex = i; editingModelIndex = -1; openSections.endpoints = true; render(); };
    del.onclick = function () { commitActiveEditors(); cfg.endpoints.splice(i, 1); editingEndpointIndex = -1; render(); };
    act.append(edit, del);
    r.appendChild(act);
    return r;
  }
  function endpointEditRow(e, i) {
    var r = el('<div style="display:flex;padding:5px 8px;align-items:center;font-size:12px;background:#fffbeb"></div>');
    var oldName = e.name;
    var name = inp(e.name, 2), kind = sel(KINDS, e.kind, 2), url = inp(e.base_url, 3), key = inp(e.api_key, 2);
    [name, kind, url, key].forEach(function (x) { r.appendChild(x); });
    function commit() {
      var nextName = name.value.trim();
      if (oldName && nextName && oldName !== nextName) {
        cfg.models.forEach(function (m) { if (modelEndpoint(m) === oldName) setModelEndpoint(m, nextName); });
      }
      e.name = nextName;
      e.kind = kind.value;
      e.base_url = url.value;
      e.api_key = key.value;
      oldName = e.name;
      updateWarnings();
    }
    activeEditors.push(commit);
    var ok = el('<span style="color:#16a34a;cursor:pointer">✓</span>');
    var del = el('<span style="color:#c00;cursor:pointer;margin-left:6px" title="удалить">🗑</span>');
    ok.onclick = function () { commit(); editingEndpointIndex = -1; render(); };
    del.onclick = function () { cfg.endpoints.splice(i, 1); editingEndpointIndex = -1; render(); };
    r.appendChild(el('<span style="width:46px"></span>')).append(ok, del);
    return r;
  }

  // --- таблица моделей ---
  function renderModels() {
    var t = el('<div style="border:1px solid #e2e8f0;border-top:none"></div>');
    t.appendChild(rowHTML(['Имя', 'Провайдер', 'Vision', 'MaxTokens', ''], true));
    cfg.models.forEach(function (m, i) { t.appendChild(modelRow(m, i)); });
    var add = el('<div style="padding:6px 8px;color:#2563eb;cursor:pointer;font-size:12px">+ добавить модель</div>');
    add.onclick = function () {
      commitActiveEditors();
      var idx = cfg.models.length;
      cfg.models.push({ name: uniqueName('new-model', cfg.models, 'name'), endpoint: (cfg.endpoints[0] || {}).name || '', vision: false, max_tokens: 0 });
      openSections.models = true;
      editingModelIndex = idx;
      editingEndpointIndex = -1;
      render();
    };
    t.appendChild(add);
    return t;
  }
  function modelRow(m, i) {
    normalizeModel(m);
    if (editingModelIndex === i) return modelEditRow(m, i);
    var r = el('<div style="display:flex;padding:5px 8px;align-items:center;font-size:12px' + (i % 2 ? ';background:#f9fafb' : '') + '"></div>');
    r.appendChild(el('<span style="flex:3">' + esc(m.name) + '</span>'));
    r.appendChild(el('<span style="flex:2">' + esc(modelEndpoint(m) || '—') + '</span>'));
    r.appendChild(el('<span style="flex:1">' + (m.vision ? '✓' : '—') + '</span>'));
    r.appendChild(el('<span style="flex:1">' + (m.max_tokens || '') + '</span>'));
    var act = el('<span style="width:46px;display:flex;gap:8px;justify-content:flex-end"></span>');
    var edit = el('<span style="color:#94a3b8;cursor:pointer" title="редактировать">✎</span>');
    var del = el('<span style="color:#c00;cursor:pointer" title="удалить">🗑</span>');
    edit.onclick = function () { commitActiveEditors(); editingModelIndex = i; editingEndpointIndex = -1; openSections.models = true; render(); };
    del.onclick = function () { commitActiveEditors(); cfg.models.splice(i, 1); editingModelIndex = -1; render(); };
    act.append(edit, del);
    r.appendChild(act);
    return r;
  }
  function modelEditRow(m, i) {
    var r = el('<div style="display:flex;padding:5px 8px;align-items:center;font-size:12px;background:#fffbeb"></div>');
    var oldName = m.name;
    var name = inp(m.name, 3);
    var epNames = cfg.endpoints.map(function (e) { return e.name; });
    var ep = sel(epNames, modelEndpoint(m), 2);
    var vis = el('<span style="flex:1"><input type="checkbox"' + (m.vision ? ' checked' : '') + '></span>');
    var tok = inp(m.max_tokens || '', 1);
    [name, ep, vis, tok].forEach(function (x) { r.appendChild(x); });
    function commit() {
      var nextName = name.value.trim();
      if (oldName && nextName && oldName !== nextName) {
        cfg.profiles.forEach(function (p) {
          (p.models || []).forEach(function (mn, idx) {
            if (mn === oldName) p.models[idx] = nextName;
          });
        });
      }
      m.name = nextName;
      setModelEndpoint(m, ep.value);
      m.vision = vis.querySelector('input').checked;
      m.max_tokens = parseInt(tok.value, 10) || 0;
      oldName = m.name;
      updateWarnings();
    }
    activeEditors.push(commit);
    var ok = el('<span style="color:#16a34a;cursor:pointer">✓</span>');
    var del = el('<span style="color:#c00;cursor:pointer;margin-left:6px">🗑</span>');
    ok.onclick = function () { commit(); editingModelIndex = -1; render(); };
    del.onclick = function () { cfg.models.splice(i, 1); editingModelIndex = -1; render(); };
    var box = el('<span style="width:46px"></span>'); box.append(ok, del); r.appendChild(box);
    return r;
  }

  // --- мелкие хелперы полей ---
  function inp(v, flex) { var i = el('<input style="flex:' + flex + ';min-width:0;padding:3px 6px;border:1px solid #cbd5e1;border-radius:3px;font-size:12px">'); i.value = v == null ? '' : v; return i; }
  function sel(opts, cur, flex) {
    var s = el('<select style="flex:' + flex + ';padding:3px 6px;border:1px solid #cbd5e1;border-radius:3px;font-size:12px"></select>');
    if (cur && opts.indexOf(cur) < 0) opts = [cur].concat(opts);
    opts.forEach(function (o) { s.appendChild(el('<option' + (o === cur ? ' selected' : '') + '>' + esc(o) + '</option>')); });
    return s;
  }
  function rowHTML(cols, head) {
    var r = el('<div style="display:flex;padding:4px 8px;font-size:12px' + (head ? ';background:#f8fafc;font-weight:600' : '') + '"></div>');
    var flexes = [2, 2, 3, 2, 1];
    cols.forEach(function (c, i) { r.appendChild(el('<span style="flex:' + (flexes[i] || 1) + '">' + esc(c) + '</span>')); });
    return r;
  }

  // --- режим сырого JSON ---
  function renderJson() {
    root.innerHTML = '';
    var bar = el('<div style="display:flex;justify-content:space-between;margin-bottom:8px"><span style="font-size:11px;color:#666">Режим JSON — правьте конфиг целиком</span><span style="color:#2563eb;cursor:pointer;font-size:11px">▦ Вернуть формы</span></div>');
    bar.querySelector('span:last-child').onclick = function () {
      try { var v = JSON.parse(ta.value); cfg = v; normalizeConfig(); jsonMode = false; render(); }
      catch (e) { msg('Некорректный JSON: ' + e.message, '#c00'); }
    };
    var ta = el('<textarea spellcheck="false" style="width:100%;height:340px;font-family:monospace;font-size:12px;padding:8px;border:1px solid #cbd5e1;border-radius:4px;resize:vertical"></textarea>');
    ta.value = JSON.stringify(cfg, null, 2);
    var foot = el('<div style="margin-top:10px;display:flex;justify-content:flex-end;gap:10px"><span id="ais-msg" style="font-size:11px"></span><button style="background:#16a34a;color:#fff;border:none;padding:6px 16px;border-radius:3px;cursor:pointer;font-size:12px">Сохранить</button></div>');
    foot.querySelector('button').onclick = function () {
      try { cfg = JSON.parse(ta.value); normalizeConfig(); save(); } catch (e) { msg('Некорректный JSON: ' + e.message, '#c00'); }
    };
    root.appendChild(bar); root.appendChild(ta); root.appendChild(foot);
  }

  function msg(text, color) { var m = document.getElementById('ais-msg'); if (m) { m.textContent = text; m.style.color = color; } }

  // --- сохранение и проверка через существующие эндпоинты ---
  function save() {
    commitActiveEditors();
    msg('Сохранение…', '#666');
    fetch(saveURL, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(cfg) })
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (d.ok) {
          editingEndpointIndex = -1;
          editingModelIndex = -1;
          if (!jsonMode) render();
          msg('Сохранено', '#16a34a');
          if (typeof window.cfgAiRefresh === 'function') window.cfgAiRefresh();
        }
        else msg(d.error || 'Ошибка', '#c00');
      })
      .catch(function () { msg('Ошибка сети', '#c00'); });
  }
  function runTest(task, out) {
    out.textContent = ' запрос…'; out.style.color = '#666';
    fetch(testURL, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ config: cfg, task: task }) })
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (d.ok) { out.textContent = ' ✓ ответила ' + d.model; out.style.color = '#16a34a'; }
        else { out.textContent = ' ✕ ' + (d.error || 'ошибка'); out.style.color = '#c00'; }
      })
      .catch(function () { out.textContent = ' ✕ ошибка сети'; out.style.color = '#c00'; });
  }

  render();
})();
