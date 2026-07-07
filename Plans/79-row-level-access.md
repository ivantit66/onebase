# План 79 - Row-level access / строковые политики доступа

Дата проектирования: 2026-07-07.
Статус: первый срез внедрён; этап 79E расширяется точечными безопасными шагами.

## Реализовано в первом срезе

- декларативный `permissions.row_access` в ролях для `catalogs`,
  `documents`, `registers`, `inforegs`;
- `same_as`, `all/any/not`, `eq/ne/in/not_in/empty/not_empty`,
  `{ user: id|login }`, `{ user_attr: ... }`, `{ literal: ... }`,
  `{ list: [...] }`;
- единый компилятор безопасных SQL-предикатов для SQLite/PostgreSQL;
- UI и REST v1/v2 для справочников/документов: list/count/get/create/update/
  delete/post/unpost;
- reference picker, выбранные ссылки, folder picker, fill-from-query;
- печать, экспорт списков, owner-bound вложения и image-blob отдача;
- регистры накопления и сведений: SQL-side фильтрация списков/движений/
  остатков, проверка write/delete для ручных записей регистра сведений;
- отчеты и AI/query: fail-closed gate для источников с активной row policy,
  пока query compiler не умеет безопасно внедрять alias-aware row predicate.
- query compiler уже внедряет row predicates в обычные источники, JOIN-источники,
  виртуальные таблицы регистров, авто-JOIN ссылок и ограниченные источники внутри
  `FROM`-подзапросов через scoped `(SELECT * FROM ... WHERE policy) AS alias`;
  `UNION` с активной row policy остаётся fail-closed.

## Оставшиеся этапы

- расширять alias-aware внедрение row predicates в query compiler для оставшихся
  форм запросов (`UNION`, более богатая диагностика источников);
- расширения политик вроде условий по реквизитам ссылок и автоустановки владельца
  при создании (`user_attr` уже поддержан для встроенных атрибутов пользователя и
  host-provided `User.Attrs`);
- явная модель для trusted DSL/server-code путей и внешних обработок;
- UI-редактор политик и отдельный diagnostics экран; базовые `onebase check
  --lint` warnings `rls.*` уже проверяют unknown object, invalid policy и
  policy без object-level права;
- PostgreSQL native RLS как дополнительный defense-in-depth слой, не как
  единственный источник истины.

## Контекст

В onebase уже есть объектный RBAC:

- `auth.User.Has(kind, entity, op)` решает, можно ли пользователю выполнять
  операцию над объектом конфигурации целиком;
- UI и REST вызывают `requirePerm` / `requireRESTPerm` перед list/get/save/delete;
- AI/query compiler уже возвращает `query.Result.Sources`, чтобы проверять
  object-level `read` на источники запроса;
- форма объекта может объявить `ПриЧтенииНаСервере` и запретить открытие
  конкретной записи, но это не фильтрует списки, REST, reference picker, отчеты
  и AI-запросы.

Этого хватает для "пользователь видит/не видит справочник или документ целиком",
но не хватает для сценариев:

- менеджер видит только свои сделки;
- сотрудник видит только документы своего подразделения;
- руководитель видит строки нескольких ответственных;
- REST и UI не должны расходиться по видимости данных.

## Цель

Добавить платформенную строковую модель доступа поверх текущего RBAC так, чтобы:

- object-level permission оставался первым обязательным фильтром;
- row-level policy одинаково работала в UI, REST, reference picker, печати,
  экспорте, AI/query и отчетах;
- правила были декларативными в ролях, валидировались при старте/lint и не
  зависели только от PostgreSQL RLS;
- SQLite/dev и PostgreSQL/prod имели одинаковую семантику;
- политики применялись SQL-side для списков, без запросов в цикле по каждой
  строке.

## Не цели первого среза

- Не включать PostgreSQL RLS как единственный источник истины.
- Не разрешать произвольные SQL-фрагменты в YAML ролей.
- Не делать условия через реквизиты ссылок вроде `Контрагент.Менеджер` в MVP.
  Первый срез фильтрует физические поля текущей строки. Для сложных условий
  нужно отдельное расширение query compiler с явными JOIN и проверкой планов.
- Не фильтровать табличные части отдельно от шапки документа в MVP.
- Не строить UI-редактор политик в первом PR.
- Не считать серверный hook `ПриЧтенииНаСервере` заменой общей модели доступа.

## Модель в ролях

Политики строк живут рядом с текущими `permissions` и используют те же секции
объектов: `catalogs`, `documents`, `registers`, `inforegs`.

Пример:

```yaml
name: Менеджер продаж
permissions:
  documents:
    Сделка: [read, write, delete, post]
  catalogs:
    Контрагенты: [read]

  row_access:
    documents:
      Сделка:
        read:
          any:
            - field: Ответственный
              op: eq
              value: { user: id }
            - field: Автор
              op: eq
              value: { user: login }
        write:
          same_as: read
        delete:
          same_as: read
        post:
          same_as: read
```

Минимальная грамматика предикатов:

```yaml
row_access:
  documents:
    Заказ:
      read:
        all:
          - field: Ответственный
            op: eq
            value: { user: id }
          - field: deletion_mark
            op: eq
            value: { literal: false }
      write:
        same_as: read
```

Поддержать в MVP:

- логические узлы: `all`, `any`, `not`;
- `same_as: <op>` без циклов;
- операторы: `eq`, `ne`, `in`, `not_in`, `empty`, `not_empty`;
- значения:
  - `{ user: id }` - текущий `auth.User.ID`;
  - `{ user: login }` - текущий `auth.User.Login`;
  - `{ user_attr: full_name|lang|... }` - встроенный атрибут пользователя или
    host-provided `auth.User.Attrs`;
  - `{ literal: ... }` - строка, число, bool, UUID-строка;
  - `{ list: [...] }` - список literal-значений.

Отложить:

- условия по ролям;
- условия по реквизитам ссылок;
- автоустановку поля при создании (`Ответственный = текущий пользователь`).

## Семантика

1. Если пользователей в базе нет (`auth.UserFromContext(ctx) == nil`), поведение
   остается открытым, как сейчас.
2. Администратор проходит без row-фильтров.
3. Если object-level права на операцию нет, доступ запрещен как сейчас.
4. Если роль дает object-level право и не содержит row policy для этой операции,
   эта роль дает неограниченный доступ. Это сохраняет обратную совместимость.
5. Если несколько ролей дают ограниченные политики, они объединяются через OR.
6. Если среди ролей есть одна неограниченная роль на операцию, итоговый доступ
   неограниченный.
7. Если policy объявлена, но невалидна или не может быть применена, поведение
   fail-closed: запретить доступ и зафиксировать ошибку в лог/lint.
8. Для `create` отдельной object-level операции сейчас нет. В MVP создание
   проверяется через `write`; если позже появится `create` в `row_access`, она
   будет переопределять write-политику именно для новых строк.

## Где применять

### UI сущностей

- list/count: добавить row predicate в `storage.ListParams`;
- form edit/get by id: не отдавать HTML, если строка не проходит read policy;
- save/update: перед изменением проверить существующую строку по write policy;
- create: проверить новый набор полей по write/create policy;
- delete: проверить существующую строку по delete policy;
- post/unpost: проверить существующую строку по post/unpost policy.

`ПриЧтенииНаСервере` остается дополнительным конфигурационным hook после
платформенной проверки, а не единственным механизмом безопасности.

### REST API v1/v2

Повторить ту же схему:

- list/get фильтруются read policy;
- create/update проверяют write/create;
- delete проверяет delete;
- post/unpost проверяют post/unpost;
- `X-Total-Count` должен считать только видимые строки.

### Reference picker и ссылки

- `/ui/_ref-options/{entity}` и picker должны использовать read policy;
- открытие выбранной ссылки через shell/form должно снова проверять read policy;
- inline create должен проверять write/create policy целевой сущности.

### Печать, вложения, картинки, экспорт

Все маршруты, которые сейчас проверяют только object-level `read`, должны
добавить проверку конкретной строки:

- печатные формы объекта;
- HTML/PDF/Excel экспорт карточки/списка, если есть;
- owner-bound вложения и изображения.

### DSL runtime, обработки и HTTP-сервисы

Пользовательский контекст уже попадает в request context, поэтому data-proxy
операции в DSL должны получать единый access controller:

- чтение списка/объекта - read policy;
- запись/проведение - write/post policy;
- серверный код без пользователя или admin-контекст остается доверенным.

Если отдельные DSL API сейчас считаются "trusted server code", это надо явно
зафиксировать в документации и тестах, чтобы не получить скрытый обход через
обработку, которую может запустить обычный пользователь.

### Отчеты, query compiler и AI

Это самый рискованный контур.

Сейчас отчеты и AI-запросы компилируются в SQL и выполняются через
`RunQueryLimit` / `QueryAll`. Компилятор уже возвращает `Sources`, но этого
хватает только для object-level RBAC. Для row-level доступа недостаточно просто
добавить строку `WHERE`: запрос может содержать JOIN, виртуальные таблицы,
алиасы, авто-join ссылок, группировки и подзапросы.

Безопасный порядок:

1. В первом PR для отчетов/AI сделать fail-closed gate: если non-admin запускает
   запрос, `Sources` содержит объект с active row policy, а compiler еще не умеет
   доказуемо внедрить policy в SQL, запрос запрещается понятной ошибкой.
2. Отдельным этапом расширить `query.Result.Sources` до alias-aware источников:
   kind, name, table/alias, место использования, тип источника.
3. На основе alias-aware источников внедрять row predicate в SQL compiler, а не
   постфильтровать результат в Go. Постфильтр после SQL неверен для агрегатов:
   сумма/количество уже посчитаны по чужим строкам.

Для `ai.data_scope=all` row-level policy не должна автоматически выключаться.
Неадмин с доступом к AI-инструментам не должен обходить строковые ограничения
через произвольный запрос.

## Архитектура кода

### auth

Расширить `auth.Permission`:

```go
type Permission struct {
    AIDataAccess bool
    Catalogs     map[string][]string
    Documents    map[string][]string
    Registers    map[string][]string
    InfoRegs     map[string][]string
    Reports      map[string][]string
    Processors   map[string][]string
    RowAccess    RowAccess `yaml:"row_access"`
}
```

Добавить структуры для декларативной политики и нормализацию YAML:

- `RowAccess`;
- `RowPolicy`;
- `RowPredicate`;
- `RowValue`.

Текущий формат ролей должен остаться совместимым: роли без `row_access` работают
как раньше.

### access

Добавить пакет `internal/access`, чтобы не тащить `auth` внутрь `storage`.

Ответственность пакета:

- вычислить effective policy для `(user, kind, entity, op)`;
- объединить роли по правилам OR/unrestricted;
- подставить значения текущего пользователя;
- валидировать policy против metadata;
- вернуть `Decision`:

```go
type Decision struct {
    Allowed      bool
    Unrestricted bool
    Predicate    *storage.Predicate
}
```

### storage

Добавить безопасный структурный предикат без SQL-фрагментов из YAML:

```go
type Predicate struct {
    Any []Predicate
    All []Predicate
    Not *Predicate
    Field string
    Op    string
    Value any
    Values []any
}
```

Изменения:

- `ListParams.RowFilter *Predicate`;
- общий builder WHERE-условий для `List` и `CountList`, чтобы не размножить
  расхождения;
- `GetByIDFiltered` или helper `CanReadRow(ctx, entity, id, predicate)`;
- SQL-компиляция через placeholders;
- Go-evaluator того же predicate для проверки уже загруженной строки и для
  create/update payload до записи.

Предикат должен работать с нормализованными именами полей:

- metadata fields;
- системные поля `id`, `posted`, `deletion_mark`, `_version`, `parent_id`,
  `is_folder`, где применимо.

## Этапы реализации

### 79A. Модель, lint и effective policy

- расширить YAML ролей;
- добавить parse/normalize/merge;
- добавить validation/lint:
  - неизвестная секция/сущность/поле;
  - неизвестный оператор;
  - `same_as` на несуществующую операцию или цикл;
  - неподходящее значение для типа поля;
- покрыть тестами роль без row_access, роль с policy, несколько ролей,
  unrestricted override и admin/open deployment.

### 79B. Storage + UI/REST CRUD

- добавить `storage.Predicate`;
- применить predicate в `List`/`CountList`;
- добавить проверку конкретной строки для get/update/delete/post/unpost;
- подключить UI entity handlers;
- подключить REST v1/v2 handlers;
- тесты:
  - список показывает только свои строки;
  - count совпадает со списком;
  - прямой GET чужой строки дает 403/404 без данных;
  - update/delete/post чужой строки запрещены;
  - admin видит все;
  - отсутствие пользователей сохраняет прежнее поведение.

### 79C. Picker, печать, export, attachments, DSL-proxy

- reference options/picker не показывают чужие строки;
- открытие ссылки повторно проверяет read policy;
- печатные формы и owner-bound файлы не раскрывают чужой объект;
- пользовательские обработки/HTTP-сервисы не получают обход через data-proxy,
  если они запускаются в user context;
- тесты на каждый внешний маршрут, где раньше была только object-level проверка.

### 79D. Отчеты и AI fail-closed

- перед выполнением отчета/AI-запроса проверить `query.Result.Sources`;
- если source имеет active row policy и query compiler еще не умеет внедрить
  predicate, отказать non-admin с понятной ошибкой;
- не ломать отчеты без row policies;
- покрыть UI report, export report, REST v2 report и AI tool-use.

### 79E. Alias-aware query compiler и SQL-инъекция policy

Отдельный сложный этап после 79D:

- расширить `query.SourceRef` alias/table информацией для диагностики и будущих
  lint-правил;
- внедрять predicate в SQL на уровне compiler/translator; уже покрыты обычные
  источники, JOIN, авто-join ссылок, виртуальные таблицы регистров и
  `FROM`-подзапросы;
- отдельно проверить и расширить оставшиеся формы (`UNION`, более сложные
  вложенные запросы);
- не использовать Go-постфильтр для агрегатов.

## Проверки приемки

- `go test ./internal/auth ./internal/storage ./internal/ui ./internal/api ./internal/query`
- `go test ./...`
- `go run ./tools/i18ncheck`
- ручной smoke:
  - два пользователя с разными ролями;
  - UI list/form;
  - REST list/get/update;
  - picker;
  - отчет/AI с безопасным отказом, если row policy еще не внедряется в query.

## Главные риски

- Отчеты и AI могут стать каналом утечки, если попытаться фильтровать результат
  после выполнения SQL. Поэтому сначала fail-closed, потом alias-aware compiler.
- Нельзя смешивать "роль без row_access" с "роль ограничена" как AND: это сломает
  совместимость. Семантика должна быть OR, а unrestricted role выигрывает.
- Условия по реквизитам ссылок нельзя добавлять неявно в MVP. Это либо SQL JOIN
  на уровне compiler, либо осознанная денормализация поля в документе.
- Серверные DSL-обработки должны быть явно классифицированы: user-context
  операции применяют row access, trusted admin/system-context операции могут
  обходить его только осознанно.
