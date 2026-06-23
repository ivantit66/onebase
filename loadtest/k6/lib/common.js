// Общие хелперы k6-сценариев onebase.
//
// Аутентификация: проще всего гонять нагрузку по базе БЕЗ пользователей —
// тогда onebase пускает анонимно и cookie не нужен. Если в базе есть юзеры,
// передайте значение cookie onebase_session через OB_SESSION_COOKIE.

import http from 'k6/http';
import { check } from 'k6';

export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SESSION_COOKIE = __ENV.OB_SESSION_COOKIE || '';

// u строит абсолютный URL. Сессионный токен в query (?_tk=) больше не
// принимается приложением; auth передается только cookie onebase_session.
export function u(path) {
  return `${BASE_URL}${path}`;
}

function requestOptions(headers = {}) {
  const h = Object.assign({}, headers);
  if (SESSION_COOKIE) {
    h.Cookie = `onebase_session=${SESSION_COOKIE}`;
  }
  return { headers: h };
}

// Имена сущностей эталонной конфигурации examples/minimal. Под другой конфиг
// поменяйте здесь — в URL пойдёт encodeURIComponent (кириллица допустима).
export const CATALOG_COUNTERPARTY = encodeURIComponent('Контрагент');
export const DOCUMENT_POSTING = encodeURIComponent('Поступление');

export const JSON_HEADERS = requestOptions({ 'Content-Type': 'application/json' });
export const GET_HEADERS = requestOptions();

// createCounterparty создаёт контрагента, возвращает id или null.
export function createCounterparty(suffix) {
  const body = JSON.stringify({
    'Наименование': `ООО Контрагент ${suffix}`,
    'ИНН': `77${String(suffix).padStart(8, '0')}`,
  });
  const res = http.post(u(`/catalogs/${CATALOG_COUNTERPARTY}`), body, JSON_HEADERS);
  check(res, { 'контрагент создан (200)': (r) => r.status === 200 });
  try {
    return res.json('id');
  } catch (_) {
    return null;
  }
}

// postReceipt создаёт и проводит документ поступления на указанного контрагента
// одним вызовом (__action=post). Это самый тяжёлый, репрезентативный путь:
// OnPost (DSL) + движения регистра в транзакции.
export function postReceipt(counterpartyID, itemIdx) {
  const qty = 1 + (itemIdx % 20);
  const price = 10 + (itemIdx % 990);
  const body = JSON.stringify({
    'Дата': new Date().toISOString().slice(0, 10),
    'Поставщик': counterpartyID,
    '__tableparts': {
      'Товары': [{
        'Номенклатура': `Товар ${itemIdx % 50}`,
        'Количество': qty,
        'Цена': price,
        'Сумма': qty * price,
      }],
    },
    '__action': 'post',
  });
  return http.post(u(`/documents/${DOCUMENT_POSTING}`), body, JSON_HEADERS);
}
