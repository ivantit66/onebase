# План 71b — Визуальный конструктор форм: доводка (follow-up #164)

Продолжение плана `71-visual-form-designer.md`. MVP (слайсы 1–6) влит PR #187 в
`feature/164-visual-form-designer`: round-trip `internal/formdoc`, холст
(`renderFormCanvas`), эндпоинт `POST /forms/edit-op` (op `render`/`setProp`/
`insert`/`move`), клиент (палитра-drag на холст, панель свойств, синхронизация с
Monaco). Этот план — отложенные пункты + баг навигации, найденный при тестировании.

Архитектура без изменений: команда → `/forms/edit-op` → formdoc правит `yaml.Node`
→ `{yaml, canvasHtml, selectedId, model}`. Новые операции/виды встраиваются в тот
же конвейер. TDD, как в плане 71.

## A. Баг: «← В конфигуратор» открывает не тот объект
Симптом: возврат из редактора формы ведёт на корень конфигуратора, теряя объект,
из которого зашли.

Причина: back-link статичен — `internal/launcher/forms_tmpl.go:95`
(`<a href="/bases/{{.Base.ID}}/configurator">`), а форма открывается из узла дерева
объекта (`/configurator?tab=tree&select=e-<Entity>`, см. `treeNodeID`,
`configurator.go:2603`; обработка `select` — `configurator.go:489`).

Фикс — пробросить контекст происхождения (`from`):
- Ссылки на `forms/edit` добавляют `&from=<nodeID>`:
  `configurator_tmpl.go:634,1314,1327,2122,2139,2143` и forms-list в `forms_tmpl.go`.
  nodeID = `treeNodeID(kind, entity)` (`e-<Entity>` для справочника/документа;
  где доступен `$e` — считать `treeNodeID($e.Kind,$e.Name)`; иначе дефолт `e-<Entity>`).
- `configuratorFormsEdit` (`forms_handlers.go:248`) читает `from`, кладёт в
  `configuratorData`; шаблон рендерит back-link как
  `/configurator?tab=tree&select={{from}}`, фолбэк `e-{{.EditingForm.Entity}}`.
- Сохранить `from` в hidden-полях save-формы и в редиректах save/preview/delete
  (`forms_handlers.go:435,441,507`), чтобы после сохранения контекст не терялся.

Тест: рендер редактора с `?from=e-X` → back-link `select=e-X`; без `from` → фолбэк
`e-<Entity>`. Мелкий быстрый фикс — можно отдельным PR вперёд остальных.

## B. Удаление и перестановка элементов
**B1. Удаление.** Сейчас op удаления НЕТ (слайс 3 — только SetProp/Insert/Move).
- formdoc: `func (d *Doc) DeleteElement(nodeID string) error` в `ops.go` — через
  `locate(nodeID)` вырезать узел из родительской sequence (контейнер с детьми
  удаляется целиком). Тест: комментарии соседей целы, round-trip валиден.
- эндпоинт: `case "delete"` в `applyEditOp` (`forms_editop.go`), `selected=""`.
- клиент: кнопка «Удалить элемент» в панели свойств → `op:delete`; для контейнера
  с детьми — `confirm`.

**B2. Перестановка.** `Move` уже реализован и протестирован на сервере (слайс 3),
в клиент не выведен. Варианты:
- MVP (надёжнее): кнопки ↑/↓ в панели свойств → `op:move` в соседний индекс того
  же родителя.
- drag-reorder (опц.): `.fc-el` → `draggable`, drop на `.fc-drop` шлёт `op:move`
  {node,parent,index}. Использовать СВОЙ mime (`text/onebase-node`), чтобы не
  пересекаться с палиткой (`text/onebase-attr`); dragover-зоны различать по типу.

Тесты: applyEditOp delete (узел исчез, в `model` его нет); HTTP move меняет порядок.

## C. Операции с группами и страницами
- Палитра структурных элементов (новые чипы рядом с палитрой реквизитов):
  «Группа», «Страницы»/«Страница», «Надпись». Drop на холст → `op:insert` с нужным
  kind (группа — `children: []`).
- Заголовок/имя группы/страницы — уже через панель свойств (`title.ru`).
- Добавление страницы в `СтраницыФормы`: в рендере холста у `fc-pages` сейчас нет
  drop-зон уровня страниц — добавить зону для вставки `kind:Страница`
  (`forms_canvas.go`, ветка `FormElementPages`).

## D. Табличные части на холсте + состав колонок
- **Палитра ТЧ.** Расширить источник палитры: сейчас `objectScaffoldAttrs`
  (`forms_handlers.go:313`) отдаёт только `entity.Fields`. Добавить секцию
  «Табличные части» из `entity.TableParts` (`metadata/types.go:117`).
- **Drop ТЧ.** chip ТЧ → `op:insert` kind:ТабличнаяЧасть, name:Таб<Name>,
  data_path:Объект.<Name> (формат — как `examples/crm/forms/КоммерческоеПредложение.form.yaml:128`).
- **Рендер ТЧ на холсте.** `renderCanvasElement` для `FormElementTablePart` сейчас
  заглушка `fc-tp` — показать колонки с их node-id (дети `kind:Колонка`,
  `FormElementColumn`, `form_module.go:71`).
- **Состав колонок.** В панели свойств выбранной ТЧ — список колонок из
  `tablePart.Fields` с чекбоксами «показывать» и порядком. Запись — дочерние
  `Колонка` (`data_path`/`field`), управляются теми же Insert/Delete/Move.
- **Источник колонок.** `applyEditOp` сейчас чист (только YAML). Колонки сущности
  знает только metadata → либо прокинуть состав ТЧ с клиента (он уже грузит attrs),
  либо передать `*project.Project` в рендер/эндпоинт (как `objectScaffoldAttrs`).

Тесты: insert ТЧ; рендер ТЧ с колонками (node-id колонок); toggle колонки →
insert/delete `Колонка`.

## Слайсы (порядок реализации)
1. **A** — фикс возврата (быстрый, отдельный коммит/PR вперёд).
2. **B1** — DeleteElement + `op:delete` + кнопка удаления.
3. **B2** — reorder: кнопки ↑/↓ (Move в клиент); drag-reorder — опционально.
4. **C** — структурная палитра (Группа/Страница/Надпись) + drop-зона страниц.
5. **D1** — палитра ТЧ + insert ТЧ + рендер колонок на холсте.
6. **D2** — состав колонок (toggle/порядок) через `Колонка`-элементы.

## Вне объёма
Редактирование данных ТЧ в дизайнере (это рантайм-рендер SlickGrid, не дизайн);
внешние обработки/формы (отдельный путь).
