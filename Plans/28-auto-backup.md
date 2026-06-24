# Этап 28 — Автоматический бэкап по расписанию

**Статус:** 🟡 Ядро реализовано (2026-06-24): `backup:` в `app.yaml`,
регистрация задания `AutoBackup` в существующем scheduler, ротация `keep_last`,
атомарная запись PostgreSQL/SQLite-дампов через временный файл + rename,
панель бэкапов показывает PostgreSQL `.sql.gz` и SQLite `.db`. Отдельная таблица
`_backups` не вводилась: текущий лаунчер уже ведёт список по файлам, а статусы
запусков пишет `_scheduled_runs`.

## Контекст

Этап 25 добавил `onebase backup` для ручного создания бэкапа. Но в реальном использовании нужен автобэкап — «поставил и забыл». Этот этап интегрирует бэкап с уже готовым планировщиком регламентных заданий (этап 17) и добавляет ротацию старых файлов.

## Синтаксис / UX

### Настройка в `config/app.yaml`

```yaml
backup:
  enabled: true
  schedule: "0 2 * * *"     # каждую ночь в 02:00 (cron-формат)
  keep_last: 7               # хранить последние N бэкапов
  directory: ""              # пусто = <project>/backups
```

### UI (Администрирование → Бэкапы)

- Таблица: имя файла, размер, дата создания, кнопка «Скачать» / «Удалить»
- Кнопка **«Создать сейчас»** — запуск внеочередного бэкапа
- Статус последнего автобэкапа (успех/ошибка/время)
- Следующий запланированный бэкап (рассчитывается из `schedule`)

## Хранилище

Файлы: `<project>/backups/backup_<dbname>_<timestamp>.sql.gz` для PostgreSQL и
`<project>/backups/backup_<dbname>_<timestamp>.db` для SQLite. Если `directory`
задан, используется он.

Метаданные бэкапов в таблице `_backups`:

```sql
CREATE TABLE IF NOT EXISTS _backups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    filename    TEXT NOT NULL,
    filepath    TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    status      TEXT NOT NULL DEFAULT 'ok',  -- ok | error
    error       TEXT
);
```

## Изменения в коде

**`internal/project/loader.go`**:
- Добавить `BackupConfig` в `AppConfig`:
  ```go
  type BackupConfig struct {
      Enabled   bool   `yaml:"enabled"`
      Schedule  string `yaml:"schedule"`
      KeepLast  int    `yaml:"keep_last"`
      Directory string `yaml:"directory"`
  }
  ```

**`internal/backup/scheduler.go`** (новый файл):
- `RegisterAutoBackup(cfg BackupConfig, connStr string, sched *scheduler.Scheduler)`
- При старте сервера: если `cfg.Enabled` и `cfg.Schedule != ""` → регистрирует cron-задание
- После каждого успешного бэкапа — ротация: удаляет файлы сверх `KeepLast`

Фактически реализовано в `internal/backup/auto.go`: пустое расписание при
`enabled: true` означает `0 2 * * *`, пустой/нулевой `keep_last` — 7.

**`internal/cli/run.go`** и **`dev.go`**:
- После старта планировщика вызвать `backup.RegisterAutoBackup()`

**`internal/ui/server.go`**:
- Маунт `/ui/admin/backups` → хэндлер списка бэкапов
- `POST /ui/admin/backups/create` → немедленный бэкап

**`internal/ui/handlers.go`**:
- `backupsList` — список из `_backups`
- `backupCreate` — вызов `backup.Dump()` + запись в `_backups`
- `backupDownload` — `http.ServeFile` по `filepath`
- `backupDelete` — удаление файла + записи из `_backups`

## Ротация бэкапов

```go
func rotate(dir string, keepLast int) error {
    files, _ := filepath.Glob(filepath.Join(dir, "backup_*.sql.gz"))
    // sort by mtime desc
    sort.Slice(files, ...)
    for _, f := range files[keepLast:] {
        os.Remove(f)
    }
    return nil
}
```

## Тесты

- `RegisterAutoBackup` с расписанием `* * * * *` создаёт файл в течение минуты
- Ротация: 8 файлов при `keep_last: 7` → удаляет старейший

Фактические автотесты не ждут реальную минуту: отдельно проверяют регистрацию
задания, дефолты, создание через injected dumper и ротацию файлов.

## Эстимейт

3 дня.
