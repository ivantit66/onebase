# dbquery — диагностический инструмент для баз OneBase

Консольная утилита для прямых SQL-запросов к SQLite-базам OneBase.
Не требует установки — один `.exe`, никаких зависимостей.

## Сборка

```bat
cd C:\Project\onebase-1
build_dbquery.bat
```

Появится `dbquery.exe` рядом с `onebase.exe`.

## Синтаксис

```bat
dbquery -db <путь_к_базе> [опции]
```

### Опции

| Опция        | Описание                                  |
|--------------|-------------------------------------------|
| `-sql "..."` | выполнить один SQL-запрос                 |
| `-f format`  | формат вывода: `table` (по умолч.), `csv`, `json` |
| `--tables`   | список таблиц                             |
| `--schema`   | полная схема (DDL)                        |

### Примеры

Список таблиц:
```bat
dbquery -db C:\Project\OneBase\basees\TradeDemo\Tradedemo.db --tables
```

Проверить period в регистре партий:
```bat
dbquery -db Tradedemo.db -sql "SELECT DISTINCT period FROM рег_партиитоваров"
```

Остатки по складам:
```bat
dbquery -db Tradedemo.db -sql "SELECT склад, SUM(количество) FROM рег_остаткитоваров GROUP BY склад"
```

SQL из файла (stdin):
```bat
dbquery -db Tradedemo.db -f csv < query.sql
```

Интерактивный REPL:
```bat
dbquery -db Tradedemo.db
sql> \tables
sql> \schema
sql> \format csv
sql> SELECT * FROM рег_партиитоваров LIMIT 5
sql> \q
```

## Для агентов (Claude Code)

```bash
# Из bash-сессии:
./dbquery.exe -db "C:/Project/OneBase/basees/TradeDemo/Tradedemo.db" --tables

./dbquery.exe -db "C:/Project/OneBase/basees/TradeDemo/Tradedemo.db" \
  -sql "SELECT DISTINCT period FROM рег_партиитоваров"
```

## Имена таблиц

OneBase именует таблицы так (всё в нижнем регистре):

| Тип                       | Правило            | Пример                       |
|---------------------------|--------------------|------------------------------|
| Справочник                | `<имя>`            | `номенклатура`, `склад`      |
| Документ (шапка)          | `<имя>`            | `поступлениетоваров`         |
| Табличная часть документа | `<документ>_<тч>`  | `поступлениетоваров_товары`  |
| Регистр накопления        | `рег_<имя>`        | `рег_партиитоваров`          |
| Регистр сведений          | `инфо_<имя>`       | `инфо_ценыноменклатуры`      |
| Регистр бухгалтерии       | `акк_<имя>`        | `акк_хозрасчётный`           |

Префиксов `спр_` / `док_` НЕТ — справочники и документы лежат под своим
именем напрямую. Точный список всегда можно посмотреть через `--tables`.

Системные колонки регистров накопления: `period`, `recorder`,
`recorder_type`, `вид_движения`, `line_number`.
