// Сценарий list-запросов: чтение списков справочника и документов с пагинацией.
// Моделирует «листание» пользователем — read-heavy нагрузка на индексы и
// сериализацию. Перед запуском засейте данные (seed), иначе списки пустые.
//
// Запуск:
//   k6 run -e BASE_URL=http://localhost:8080 loadtest/k6/scenarios/list_query.js

import http from 'k6/http';
import { check } from 'k6';
import { u, CATALOG_COUNTERPARTY, DOCUMENT_POSTING, GET_HEADERS } from '../lib/common.js';

export const options = {
  scenarios: {
    listing: {
      executor: 'ramping-arrival-rate',
      startRate: 20,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 200,
      stages: [
        { duration: '30s', target: 50 },   // 50 запросов/сек
        { duration: '1m', target: 200 },   // до 200 запросов/сек — ищем потолок
        { duration: '30s', target: 200 },
        { duration: '20s', target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1500'],
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  // REST-список не пагинируется — возвращает весь набор; варьируем сортировку.
  const dir = Math.random() < 0.5 ? 'asc' : 'desc';
  let res;
  if (Math.random() < 0.5) {
    res = http.get(u(`/catalogs/${CATALOG_COUNTERPARTY}?sort=${encodeURIComponent('Наименование')}&dir=${dir}`), GET_HEADERS);
  } else {
    res = http.get(u(`/documents/${DOCUMENT_POSTING}?sort=${encodeURIComponent('Дата')}&dir=${dir}`), GET_HEADERS);
  }
  check(res, { 'список 200': (r) => r.status === 200 });
}
