# 106 — Вложения в письмах (ПисьмоEmail.ПрисоединитьФайл)

## Проблема

`mailer.Mailer` собирал только `multipart/alternative` (text+html); у DSL-объекта
`ПисьмоEmail` не было способа приложить файл. Рассылка обновлений клиентам
(файл релиза вложением — основной сценарий legacy-систем 1С) была невозможна.

## Решение

1. `internal/dsl/interpreter/email_builtins.go`:
   - тип `EmailAttachment{Name, MimeType, Data}`;
   - необязательный интерфейс `EmailAttachmentSender` (type-assertion при
     отправке — существующие реализации `EmailSender` в тестах не ломаются);
   - метод `ПисьмоEmail.ПрисоединитьФайл(Путь[, ИмяВПисьме])` — файл читается
     с диска (через файловую песочницу), MIME по расширению.
2. `internal/mailer/mailer.go`:
   - `SendWithAttachments(...)` → письмо `multipart/mixed`: тело (alternative
     при html) + файлы base64 (перенос строк по RFC 2045);
   - `Send` делегирует в `SendWithAttachments(nil)` — формат прежних писем
     не меняется; имя файла в заголовке санитизируется.

## Тесты

`internal/mailer/attachments_test.go` (разбор стандартным MIME-парсером,
обратная совместимость, санитизация заголовка), DSL-тесты в
`internal/dsl/interpreter/email_test.go`.
