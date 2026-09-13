// Строка поиска диалога подбора при Конфиг.ПоискНаСервере обязана спрашивать
// СЕРВЕР, а не фильтровать уже приехавшие строки. Регрессия здесь не видна
// глазами: диалог выглядит одинаково, просто перестаёт находить то, чего нет в
// привезённой выдаче (предел ВЫБРАТЬ ПЕРВЫЕ N) или что показано маской ПДн.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const test = require('node:test');

const source = fs.readFileSync('static/ui.js', 'utf8');

function extract(name) {
  const start = source.indexOf('function ' + name);
  if (start < 0) throw new Error('в ui.js нет функции ' + name);
  let depth = 0;
  for (let i = source.indexOf('{', start); i < source.length; i++) {
    if (source[i] === '{') depth++;
    else if (source[i] === '}') {
      depth--;
      if (depth === 0) return source.slice(start, i + 1);
    }
  }
  throw new Error('не закрыта функция ' + name);
}

// Узел ровно того объёма, который трогает openItemPicker: дерево, атрибуты,
// подписки и строки tbody. Настоящего DOM в тестах нет намеренно — проверяем
// поведение функции, а не браузер.
function node(tag) {
  return {
    tagName: String(tag || '').toUpperCase(),
    children: [],
    rows: [],
    attrs: {},
    listeners: {},
    style: {cssText: '', display: ''},
    value: '',
    textContent: '',
    innerHTML: '',
    checked: false,
    type: '',
    placeholder: '',
    autocomplete: '',
    colSpan: 0,
    className: '',
    appendChild(child) {
      this.children.push(child);
      if (this.tagName === 'TBODY' && child.tagName === 'TR') this.rows.push(child);
      return child;
    },
    setAttribute(name, value) { this.attrs[name] = String(value); },
    getAttribute(name) {
      return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null;
    },
    addEventListener(type, fn) { (this.listeners[type] = this.listeners[type] || []).push(fn); },
    dispatch(type) { (this.listeners[type] || []).forEach((fn) => fn.call(this, {})); },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    closest() { return null; },
    focus() { this.focused = true; },
    setSelectionRange(from, to) { this.caret = [from, to]; },
    remove() { this.removed = true; },
  };
}

// Диалог собирается из элементов подряд, поэтому строку поиска находим по типу:
// это единственный input вне таблицы.
function findSearchInput(box) {
  return box.children.find((el) => el.tagName === 'INPUT' && el.type === 'text') || null;
}

// Одна среда на несколько открытий: ответ сервера приходит тем же pickerData,
// и клиент СОБИРАЕТ ДИАЛОГ ЗАНОВО. Состояние строки поиска обязано это пережить,
// иначе набранное пропадает после первой же буквы — проверить это можно только
// двумя вызовами в одном контексте.
function pickerContext() {
  const timers = [];
  const fired = [];
  const body = node('body');
  const document = {
    getElementById() { return null; },
    createElement(tag) { return node(tag); },
    body,
  };
  const api = new Function(
    'document', 'window', 'obFire', 'setTimeout', 'clearTimeout',
    'var obPickerSearch = {element: "", query: ""};\n' +
      extract('openItemPicker') + '\n' +
      'return {openItemPicker: openItemPicker, state: function () { return obPickerSearch; }};',
  )(
    document,
    {},
    function (element, event, params) { fired.push({element, event, params}); },
    function (fn) { timers.push(fn); return timers.length; },
    function () {},
  );
  return {
    fired,
    state: api.state,
    flush() { timers.splice(0).forEach((fn) => fn()); },
    open(payload, elementName, eventContext) {
      body.children.length = 0;
      api.openItemPicker(payload, elementName, eventContext || null);
      const box = body.children[0].children[0];
      return {search: findSearchInput(box)};
    },
  };
}

const columns = [{name: 'Номер', title: 'Заявка №', type: 'string'}];
const rows = [{id: 'u-1', data: {Номер: 'ЗАЯ-000001'}}];

test('при ПоискНаСервере набранное уходит событием Поиск', () => {
  const ctx = pickerContext();
  const picker = ctx.open(
    {columns, rows, config: {title: 'Подбор', serverSearch: true}},
    'КнопкаНайти',
    {_tp: 'Строки'},
  );
  assert.ok(picker.search, 'строка поиска не найдена');

  picker.search.value = 'гай';
  picker.search.dispatch('input');
  assert.deepEqual(ctx.fired, [], 'запрос ушёл без задержки — сервер дёрнут на каждую букву');

  ctx.flush();
  assert.equal(ctx.fired.length, 1);
  assert.equal(ctx.fired[0].element, 'КнопкаНайти');
  assert.equal(ctx.fired[0].event, 'Поиск');
  assert.equal(ctx.fired[0].params._pick_query, 'гай');
  assert.equal(ctx.fired[0].params._tp, 'Строки', 'контекст события потерян');
});

test('без флага строка поиска остаётся клиентским фильтром', () => {
  const ctx = pickerContext();
  const picker = ctx.open({columns, rows, config: {title: 'Подбор'}}, 'КнопкаНайти', null);
  picker.search.value = 'гай';
  picker.search.dispatch('input');
  ctx.flush();
  assert.deepEqual(ctx.fired, [], 'клиентский фильтр не должен ходить на сервер');
});

test('ответ сервера пересобирает окно: набранное на месте, каретка в конце', () => {
  const ctx = pickerContext();
  const config = {title: 'Подбор', serverSearch: true};
  const first = ctx.open({columns, rows, config}, 'КнопкаНайти', null);
  first.search.value = '111222';
  first.search.dispatch('input');
  ctx.flush();

  // Сервер ответил своим pickerData — клиент открывает диалог заново.
  const second = ctx.open({columns, rows: [], config}, 'КнопкаНайти', null);
  assert.equal(second.search.value, '111222', 'набранное пропало при пересборке окна');
  assert.deepEqual(second.search.caret, [6, 6], 'каретка не поставлена в конец строки');
});

test('закрытое окно не подставляет старый запрос в следующее открытие', () => {
  const ctx = pickerContext();
  const config = {title: 'Подбор', serverSearch: true};
  const picker = ctx.open({columns, rows, config}, 'КнопкаНайти', null);
  picker.search.value = 'гай';
  picker.search.dispatch('input');
  ctx.flush();

  const other = ctx.open({columns, rows, config}, 'ДругаяКнопка', null);
  assert.equal(other.search.value, '', 'запрос от чужой кнопки подставился в её диалог');
});
