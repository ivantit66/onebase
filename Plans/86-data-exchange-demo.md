# Демо: обмен данными между базами (план 86, фаза 1)

Сценарий показа файлового обмена двух баз OneBase на примере `examples/trade`.
Занимает ~10 минут. Всё, что нужно, уже в репозитории:
`examples/trade/exchange/ФилиалыЦентр.yaml` (состав: Номенклатура, Контрагент,
РеализацияТоваров; узлы `center` и `fil01`; правило `by_time`).

## 0. Подготовка

```bash
# Собрать бинарь (Windows: если сервер запущен — сначала taskkill /IM onebase.exe /F)
go build -o onebase.exe ./cmd/onebase

# Две отдельные базы из одной конфигурации
./onebase.exe migrate --project examples/trade --sqlite ./center.db
./onebase.exe migrate --project examples/trade --sqlite ./fil01.db

# Назначить узел каждой базе (в YAML этого нет — конфигурация у всех одинаковая)
./onebase.exe exchange init --project examples/trade --sqlite ./center.db --plan ФилиалыЦентр --node center
./onebase.exe exchange init --project examples/trade --sqlite ./fil01.db --plan ФилиалыЦентр --node fil01
```

Проверить состояние (очередь пуста):

```bash
./onebase.exe exchange status --project examples/trade --sqlite ./center.db --plan ФилиалыЦентр
```
```
План обмена: ФилиалыЦентр
Текущий узел: center

Узлы (очередь = ждут выгрузки; отпр./подтв. — номера сообщений):
  center     Центральная база       очередь=0 отпр.=0 подтв.=0 принято=0 ← этот узел
  fil01      Филиал №1              очередь=0 отпр.=0 подтв.=0 принято=0
```

## 1. Распространение изменения (center → fil01)

1. Запустить центр и **создать номенклатуру** в браузере:
   ```bash
   ./onebase.exe run --project examples/trade --sqlite ./center.db --port 8080
   ```
   Открыть http://localhost:8080 → Номенклатура → создать «Гвоздь 4×100», записать.
   Запись из UI идёт через `entityservice.Save` — изменение автоматически попадает
   в очередь обмена. Остановить сервер (Ctrl+C).

2. Убедиться, что объект встал в очередь к fil01 (`очередь=1`):
   ```bash
   ./onebase.exe exchange status --project examples/trade --sqlite ./center.db --plan ФилиалыЦентр
   ```

3. Выгрузить пакет и загрузить в филиал:
   ```bash
   ./onebase.exe exchange dump --project examples/trade --sqlite ./center.db --plan ФилиалыЦентр --to fil01 --out ./center-to-fil01.obx
   ./onebase.exe exchange load --project examples/trade --sqlite ./fil01.db --in ./center-to-fil01.obx
   ```
   ```
   План "ФилиалыЦентр" → узел "fil01": выгружено объектов 1 (сообщение №1) в ./center-to-fil01.obx
   Пакет плана "ФилиалыЦентр" от узла "center" (сообщение №1): применено 1, пропущено 0, удалено 0, конфликтов 0
   ```

4. Проверить в филиале: `./onebase.exe run --project examples/trade --sqlite ./fil01.db --port 8081`
   → «Гвоздь 4×100» на месте. Документ, если бы переносился, пришёл бы **непроведённым**.

## 2. Идемпотентность

Загрузить тот же пакет ещё раз — ничего не меняется (версия не растёт):

```bash
./onebase.exe exchange load --project examples/trade --sqlite ./fil01.db --in ./center-to-fil01.obx
```
```
... применено 0, пропущено 1, удалено 0, конфликтов 0
```

## 3. Дренаж очереди подтверждением (ack)

Очередь центра к fil01 всё ещё держит запись (нет подтверждения приёма). Обратный
пакет из филиала несёт ack и очищает её:

```bash
./onebase.exe exchange dump --project examples/trade --sqlite ./fil01.db --plan ФилиалыЦентр --to center --out ./fil01-to-center.obx
./onebase.exe exchange load --project examples/trade --sqlite ./center.db --in ./fil01-to-center.obx
./onebase.exe exchange status --project examples/trade --sqlite ./center.db --plan ФилиалыЦентр
```
После загрузки обратного пакета `очередь` центра к fil01 стала `0`, `подтв.=1`.

## 4. Конфликт встречной правки (by_time)

1. Изменить **одну и ту же** «Гвоздь 4×100» в обеих базах (разные цены) через UI.
2. Обменяться в обе стороны:
   ```bash
   ./onebase.exe exchange dump --project examples/trade --sqlite ./fil01.db --plan ФилиалыЦентр --to center --out ./f2c.obx
   ./onebase.exe exchange load --project examples/trade --sqlite ./center.db --in ./f2c.obx
   ```
   В отчёте загрузки будет `конфликтов 1`. По правилу `by_time` побеждает позже
   изменённая версия; сменив `conflict` в плане на `by_node_priority`, победит узел
   с большим `priority` (в примере — `center`).

## 5. Из встроенного языка (альтернатива CLI)

В обработке/регламентном задании:
```
Пакет = ПланыОбмена.ФилиалыЦентр.ВыгрузитьИзменения("fil01");
// ...передать Пакет (строка) на приёмник любым транспортом...
Применено = ПланыОбмена.ФилиалыЦентр.ЗагрузитьПакет(Пакет);
```

## 6. Онлайн-обмен по сети (sync)

Вместо ручного файла — прямой обмен база↔база по HTTP.

1. Задать узлам адреса и общий токен плана:
   ```bash
   export OB_EXCHANGE_CENTER_URL=http://127.0.0.1:8080
   export OB_EXCHANGE_FIL01_URL=http://127.0.0.1:8081
   ./onebase.exe exchange init --project examples/trade --sqlite ./center.db --plan ФилиалыЦентр --node center --token S3CRET
   ./onebase.exe exchange init --project examples/trade --sqlite ./fil01.db  --plan ФилиалыЦентр --node fil01  --token S3CRET
   ```
2. Поднять обе базы серверами (в отдельных терминалах):
   ```bash
   ./onebase.exe run --project examples/trade --sqlite ./center.db --port 8080
   ./onebase.exe run --project examples/trade --sqlite ./fil01.db  --port 8081
   ```
3. Синхронизироваться (с любой из баз): отправит свои изменения партнёру и
   заберёт его изменения для себя одной командой:
   ```bash
   ./onebase.exe exchange sync --project examples/trade --sqlite ./center.db --plan ФилиалыЦентр --with fil01
   ```
   ```
   Синхронизация с "fil01" (http://127.0.0.1:8081):
     отправлено → у партнёра применено 1, пропущено 0, конфликтов 0
     получено → у нас применено 0, пропущено 0, конфликтов 0
   ```
   Приёмные эндпоинты (`POST /exchange/<план>/push`, `GET …/pull?to=…`)
   аутентифицируются Bearer-токеном плана; без токена — 403, с неверным — 401.

## Что показать словами

- Регистрация изменений **автоматическая** (при записи из UI/REST), опирается на
  версии объектов — «выгружается ровно изменённое».
- Загрузка **идемпотентна**: повторная доставка безопасна; потеря пакета лечится
  повторной выгрузкой.
- Конфликты разрешаются **правилом плана**, а не молча.

## Ограничения фазы 1 (честно)

- Транспорт: **файловый** (`.obx`, dump/load) и **онлайн** (`sync` по HTTP с
  Bearer-токеном плана).
- Изменения регистрируются на всех путях записи: UI/REST (`entityservice.Save`),
  прямые записи из DSL (`Справочники.X.Создать().Записать()`, `Документы.X.Записать()/.Провести()`)
  и пометка на удаление.
- Правило `hook` (`ПриКонфликтеОбмена` в общем модуле) работает на путях приёма
  push и CLI load/sync; в DSL-`ЗагрузитьПакет` — откат к `by_time`.
- Монитор обмена — в интерфейсе администратора (Система → **Обмен данными**,
  `/ui/admin/exchange`): очередь и счётчики по узлам, кнопка «Синхронизировать».
- Хаб-транзит и перенос движений — фаза 2.

Автоматическая проверка сквозного цикла — `TestExchangeCLIRoundTrip`
(`internal/cli/exchange_test.go`): две базы, dump→load, идемпотентный повтор.
```
