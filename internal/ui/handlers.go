package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/dslvars"
	"github.com/ivantit66/onebase/internal/entityservice"
	"github.com/ivantit66/onebase/internal/excel"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/printform"
	processorpkg "github.com/ivantit66/onebase/internal/processor"
	"github.com/ivantit66/onebase/internal/query"
	reportpkg "github.com/ivantit66/onebase/internal/report"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
	"github.com/ivantit66/onebase/internal/widget"
	"golang.org/x/text/encoding/charmap"
)

func (s *Server) about(w http.ResponseWriter, r *http.Request) {
	entities := s.reg.Entities()
	var catalogs, docs int
	for _, e := range entities {
		if e.Kind == "catalog" {
			catalogs++
		} else {
			docs++
		}
	}
	cfg := s.cfg
	cfg.DSN = maskDSN(cfg.DSN)
	user := auth.UserFromContext(r.Context())
	s.render(w, r, "page-about", map[string]any{
		"Cfg":       cfg,
		"Catalogs":  catalogs,
		"Documents": docs,
		"Registers": len(s.reg.Registers()),
		"Reports":   len(s.reg.Reports()),
		"User":      user,
	})
}

func maskDSN(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		rest := dsn[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 {
			userPart := rest[:at]
			if colon := strings.LastIndex(userPart, ":"); colon >= 0 {
				return dsn[:i+3+colon+1] + "***" + dsn[i+3+at:]
			}
		}
	}
	if i := strings.Index(dsn, "password="); i >= 0 {
		end := i + len("password=")
		rest := dsn[end:]
		if sp := strings.IndexByte(rest, ' '); sp >= 0 {
			return dsn[:end] + "***" + rest[sp:]
		}
		return dsn[:end] + "***"
	}
	return dsn
}

func (s *Server) logo(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Logo == "" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(s.cfg.Logo)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(s.cfg.Logo))
	switch ext {
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	hp := s.reg.HomePage()
	if sub := r.URL.Query().Get("subsystem"); sub != "" {
		if ss := s.reg.GetSubsystem(sub); ss != nil && ss.HomePage != nil {
			hp = ss.HomePage
		}
	}
	// Соблюдение настроенных рядов (WYSIWYG) включается явно через layout: rows.
	honor := hp != nil && hp.Layout == "rows"
	groups, defaulted := homePageWidgetGroups(hp, s.reg, honor)

	login := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		login = u.Login
	}
	runner := widget.New(s.reg, s.store)
	runner.CurrentUser = login
	runner.Cache = s.widgetCache

	lang := s.resolveLang(r)
	// rows — для раскладки по рядам, flat — для инициализации графиков в JS.
	rows := make([][]widget.Result, 0, len(groups))
	var flat []widget.Result
	run := func(wMeta *metadata.Widget) widget.Result {
		if wMeta.Type == "missing" {
			return widget.Result{
				Name:  wMeta.Name,
				Title: wMeta.DisplayTitle(lang),
				Error: s.tr(lang, "виджет не найден:") + " " + wMeta.Name,
			}
		}
		res := runner.Run(r.Context(), wMeta)
		res.Title = wMeta.DisplayTitle(lang)
		return res
	}
	for _, group := range groups {
		rowRes := make([]widget.Result, 0, len(group))
		for _, wMeta := range group {
			res := run(wMeta)
			rowRes = append(rowRes, res)
			flat = append(flat, res)
		}
		rows = append(rows, rowRes)
	}

	title := s.tr(lang, "Главная")
	if hp != nil {
		if t := hp.DisplayTitle(lang); t != "" && t != "Главная" {
			title = t
		}
	}

	s.render(w, r, "page-index", map[string]any{
		"HomeTitle":     title,
		"WidgetRows":    rows,
		"WidgetResults": flat,
		"DefaultedHome": defaulted,
	})
}

// homePageWidgetGroups resolves the dashboard layout into ordered groups of
// widget metadata, one group per rendered row. Behaviour:
//   - With a HomePage and honor=true (layout: rows): one group per configured
//     row (RowGroups), so row boundaries are preserved (WYSIWYG).
//   - With a HomePage otherwise: a single group with every widget in order
//     (auto-flow — the template wraps them by width).
//   - Unknown widget names become a synthetic "widget not found" entry so the
//     dashboard still renders and the user can spot the typo.
//   - Without a HomePage but with registered widgets: one group in load order.
//   - Otherwise: a transient "recent" widget so a fresh install is never blank.
func homePageWidgetGroups(hp *metadata.HomePage, reg *runtime.Registry, honor bool) ([][]*metadata.Widget, bool) {
	resolve := func(names []string) []*metadata.Widget {
		out := make([]*metadata.Widget, 0, len(names))
		for _, n := range names {
			if w := reg.GetWidget(n); w != nil {
				out = append(out, w)
			} else {
				out = append(out, &metadata.Widget{Name: n, Type: "missing", Title: n})
			}
		}
		return out
	}
	if hp != nil {
		if honor {
			if rg := hp.RowGroups(); len(rg) > 0 {
				groups := make([][]*metadata.Widget, 0, len(rg))
				for _, names := range rg {
					groups = append(groups, resolve(names))
				}
				return groups, false
			}
		}
		if names := hp.WidgetNames(); len(names) > 0 {
			return [][]*metadata.Widget{resolve(names)}, false
		}
	}
	if registered := reg.Widgets(); len(registered) > 0 {
		return [][]*metadata.Widget{registered}, true
	}
	def := &metadata.Widget{
		Name:  "_default_recent",
		Type:  metadata.WidgetTypeRecent,
		Title: "Последние документы",
		Limit: 10,
		Scope: "all",
	}
	return [][]*metadata.Widget{{def}}, true
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	params := parseListParams(r, entity, s.store.GetListPageSize(r.Context()))

	treeView := entity.Hierarchical && r.URL.Query().Get("view") == "tree"

	var breadcrumbs []map[string]string
	var parentStr string
	var upURL string
	if entity.Hierarchical && !treeView {
		parentStr = r.URL.Query().Get("parent")
		if parentStr == "" {
			params.ParentStr = "root"
		} else {
			params.ParentStr = parentStr
			breadcrumbs = s.buildHierarchyBreadcrumbs(r.Context(), entity, parentStr)
			baseListURL := "/ui/" + strings.ToLower(string(entity.Kind)) + "/" + strings.ToLower(entity.Name)
			csys := r.URL.Query().Get("subsystem")
			if len(breadcrumbs) <= 1 {
				if csys != "" {
					upURL = baseListURL + "?subsystem=" + csys
				} else {
					upURL = baseListURL
				}
			} else {
				pid := breadcrumbs[len(breadcrumbs)-2]["ID"]
				if csys != "" {
					upURL = baseListURL + "?parent=" + pid + "&subsystem=" + csys
				} else {
					upURL = baseListURL + "?parent=" + pid
				}
			}
		}
	}

	total, _ := s.store.CountList(r.Context(), entity.Name, entity, params)

	rows, err := s.store.List(r.Context(), entity.Name, entity, params)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.resolveRefs(r.Context(), entity, rows)

	// For tree view: fetch ALL items and build hierarchical order with depth info
	var treeRows []map[string]any
	if treeView {
		allRows, _ := s.store.List(r.Context(), entity.Name, entity, storage.ListParams{})
		s.resolveRefs(r.Context(), entity, allRows)
		treeRows = buildCatalogTree(allRows)
	}

	refFilterOptions, _ := s.loadRefOptions(r.Context(), entity)

	user := auth.UserFromContext(r.Context())
	isAdmin := user == nil || user.IsAdmin

	// Pagination info
	page := 1
	if params.Offset > 0 && params.Limit > 0 {
		page = params.Offset/params.Limit + 1
	}
	totalPages := 1
	if params.Limit > 0 && total > 0 {
		totalPages = (total + params.Limit - 1) / params.Limit
	}

	s.render(w, r, "page-list", map[string]any{
		"Entity":           entity,
		"Rows":             rows,
		"Params":           params,
		"RefFilterOptions": refFilterOptions,
		"IsAdmin":          isAdmin,
		"Breadcrumbs":      breadcrumbs,
		"ParentStr":        parentStr,
		"UpURL":            upURL,
		"TreeView":         treeView,
		"TreeRows":         treeRows,
		"Total":            total,
		"Page":             page,
		"TotalPages":       totalPages,
		"HasPrev":          page > 1,
		"HasNext":          page < totalPages,
		"PrevPage":         page - 1,
		"NextPage":         page + 1,
	})
}

// buildCatalogTree converts a flat list of catalog rows into a depth-first ordered
// list, adding "_depth" (int) and "_label" to each row for tree rendering.
func buildCatalogTree(rows []map[string]any) []map[string]any {
	children := make(map[string][]map[string]any)
	for _, row := range rows {
		pid := ""
		if v := row["parent_id"]; v != nil {
			pid = fmt.Sprintf("%v", v)
		}
		children[pid] = append(children[pid], row)
	}

	var result []map[string]any
	var walk func(pid string, depth int)
	walk = func(pid string, depth int) {
		for _, row := range children[pid] {
			row["_depth"] = depth
			result = append(result, row)
			id := fmt.Sprintf("%v", row["id"])
			walk(id, depth+1)
		}
	}
	walk("", 0)
	return result
}

func (s *Server) form(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "write") {
		return
	}
	refOptions, _ := s.loadRefOptions(r.Context(), entity)
	tpRefOpts, _ := s.loadTPRefOptions(r.Context(), entity)
	enumOpts := s.loadEnumOptions(entity)
	// Pre-fill date fields with current datetime for new documents
	values := map[string]string{}
	if entity.Kind == metadata.KindDocument {
		now := time.Now().Format("2006-01-02T15:04")
		for _, f := range entity.Fields {
			if f.Type == metadata.FieldTypeDate {
				values[f.Name] = now
			}
		}
	}
	tablePartRows := map[string][]map[string]any{}
	var fillError string
	var fillMessages []string

	// Ввод на основании: GET /ui/{kind}/{name}/new?based_on=<src>&based_on_id=<uuid>.
	// Загружаем источник и запускаем ОбработкаЗаполнения у приёмника — её
	// результаты (Fields + TablePartRows) перетирают дефолтные значения.
	if srcType := r.URL.Query().Get("based_on"); srcType != "" {
		fillError = s.applyFillFromQuery(r, entity, srcType, r.URL.Query().Get("based_on_id"), values, tablePartRows, &fillMessages)
	}
	var folderOpts []map[string]any
	if entity.Hierarchical {
		folderOpts = s.loadFolderOptions(r.Context(), entity)
		values["parent_id"] = r.URL.Query().Get("parent")
		if r.URL.Query().Get("is_folder") == "true" {
			values["is_folder"] = "true"
		} else {
			values["is_folder"] = "false"
		}
	}
	s.renderEntityForm(w, r, "object", map[string]any{
		"Entity":        entity,
		"IsNew":         true,
		"Values":        values,
		"RefOptions":    refOptions,
		"EnumOptions":   enumOpts,
		"TPRefOptions":  tpRefOpts,
		"TPRefMeta":     tpRefMeta(entity),
		"TablePartRows": tablePartRows,
		"FolderOptions": folderOpts,
		"Error":         fillError,
		"Messages":      fillMessages,
		// IsPopup — форма открыта в iframe для inline-создания из другой
		// формы (как «новый элемент справочника» из поля документа в 1С).
		// Шаблон скрывает nav/тулбар и меняет кнопку на «Записать и выбрать».
		"IsPopup": r.URL.Query().Get("_popup") == "1",
	})
}

// applyFillFromQuery загружает источник и запускает ОбработкаЗаполнения у
// приёмника, втягивает результаты в values + tablePartRows (модифицируются
// in-place). Возвращает строку для шаблона "Error" — пустая, если всё ок.
//
// Ошибки доступа/валидации (нет прав на источник, srcType не в based_on)
// возвращаются как fillError-строки, форма всё равно открывается (пустая
// или с уже накопленными дефолтами). Так пользователь видит причину
// провала, но может продолжить ввод вручную.
func (s *Server) applyFillFromQuery(r *http.Request, entity *metadata.Entity, srcType, srcIDStr string, values map[string]string, tablePartRows map[string][]map[string]any, messages *[]string) string {
	srcID, err := uuid.Parse(srcIDStr)
	if err != nil {
		return "Некорректный идентификатор основания: " + srcIDStr
	}
	src := s.reg.GetEntity(srcType)
	if src == nil {
		return "Неизвестный тип основания: " + srcType
	}
	if !s.can(r, string(src.Kind), src.Name, "read") {
		return "Нет прав на чтение источника: " + srcType
	}
	result, err := s.entitySvc.Fill(r.Context(), entityservice.FillRequest{
		Receiver:   entity,
		SourceType: srcType,
		SourceID:   srcID,
	})
	if err != nil {
		return err.Error()
	}
	for k, v := range result.Fields {
		if v == nil {
			continue
		}
		values[fieldKeyForForm(entity, k)] = formatFieldValueForInput(v)
	}
	for tpName, rows := range result.TablePartRows {
		if rows != nil {
			tablePartRows[tpName] = rows
		}
	}
	if messages != nil && len(result.DSLMessages) > 0 {
		*messages = append(*messages, result.DSLMessages...)
	}
	return result.DSLError
}

// fieldKeyForForm возвращает имя поля в том регистре, в котором его ждёт
// шаблон (PascalCase из YAML). Object.Set/Fields у нас хранятся в lowercase
// — без приведения значения в форму не попадают (input name="Покупатель",
// values["покупатель"] не найдётся).
func fieldKeyForForm(entity *metadata.Entity, lowerKey string) string {
	for _, f := range entity.Fields {
		if strings.EqualFold(f.Name, lowerKey) {
			return f.Name
		}
	}
	return lowerKey
}

// formatFieldValueForInput приводит значение поля к строке для <input value=...>.
// Для *interpreter.Ref (после enrichHeaderRefs) — UUID, для time — RFC3339-короткий,
// иначе — fmt.Sprint.
func formatFieldValueForInput(v any) string {
	if v == nil {
		return ""
	}
	if t, ok := v.(time.Time); ok {
		return t.In(time.Local).Format("2006-01-02T15:04")
	}
	if ref, ok := v.(interface{ GetRefUUID() string }); ok {
		if s := ref.GetRefUUID(); s != "" {
			return s
		}
	}
	return fmt.Sprintf("%v", v)
}

// parseSubmitForm — общая часть submit и submitEdit. Парсит форму, строит
// объект, проверяет разрешения. Не строит коллектор движений и не вызывает
// enrich/DSL-хук — этим занимается entityservice.Service.Save (вызывается
// caller'ом ниже). Так избегается двойная работа (mc + enrich в двух местах).
//
// Если existingID == nil — создание нового объекта: id берётся из uuid.New,
// для документов с полем "Номер" автогенерируется номер. Если existingID != nil —
// редактирование: id берётся из URL, авто-нумерация не выполняется.
//
// Возвращает (nil,...,false) если запрос отклонён (нет прав / ошибка парсинга);
// в этом случае ответ уже записан в w.
func (s *Server) parseSubmitForm(w http.ResponseWriter, r *http.Request, entity *metadata.Entity, existingID *uuid.UUID) (
	obj *runtime.Object, fields map[string]any, tpRows map[string][]map[string]any, action string, ok bool,
) {
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "write") {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	fields = formToFields(r, entity)
	tpRows = parseTablePartRows(r, entity)

	if entity.Hierarchical {
		fields["parent_id"] = r.FormValue("parent_id")
		fields["is_folder"] = r.FormValue("is_folder") == "true"
	}

	// Объект для new строится через NewObject+Set (ключи нормализуются в lowercase
	// — историческое поведение submit). Для existing — прямое присваивание Fields,
	// ключи остаются как пришли из формы (PascalCase). fieldValueDialect в storage
	// читает значение по обоим вариантам ключа, так что save работает одинаково.
	if existingID == nil {
		obj = runtime.NewObject(entity.Name, entity.Kind)
		for k, v := range fields {
			obj.Set(k, v)
		}
		obj.TablePartRows = tpRows

		// Auto-number: fill Номер if empty for new documents
		if entity.Kind == metadata.KindDocument {
			for _, f := range entity.Fields {
				if f.Name == "Номер" && f.Type == metadata.FieldTypeString {
					if v := fmt.Sprintf("%v", obj.Fields["Номер"]); v == "" || v == "<nil>" {
						obj.Set("Номер", s.generateNumber(r.Context(), entity, obj.Fields))
					}
					break
				}
			}
		}
	} else {
		obj = &runtime.Object{
			Type:          entity.Name,
			Kind:          entity.Kind,
			ID:            *existingID,
			Fields:        fields,
			TablePartRows: tpRows,
		}
	}

	action = r.FormValue("_action")
	isPostingAct := entity.Posting && (action == "post" || action == "post_and_close")
	if isPostingAct && !s.requirePerm(w, r, string(entity.Kind), entity.Name, "post") {
		return
	}
	ok = true
	return
}

func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	obj, fields, tpRows, action, ok := s.parseSubmitForm(w, r, entity, nil)
	if !ok {
		return
	}

	result, err := s.entitySvc.Save(r.Context(), entityservice.SaveRequest{
		Entity:        entity,
		ID:            obj.ID,
		IsNew:         true,
		Fields:        obj.Fields,
		TablePartRows: obj.TablePartRows,
		Action:        action,
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if result.DSLError != "" {
		refOptions, _ := s.loadRefOptions(r.Context(), entity)
		tpRefOpts, _ := s.loadTPRefOptions(r.Context(), entity)
		var fOpts []map[string]any
		if entity.Hierarchical {
			fOpts = s.loadFolderOptions(r.Context(), entity)
		}
		s.renderEntityForm(w, r, "object", map[string]any{
			"Entity":        entity,
			"IsNew":         true,
			"Error":         result.DSLError,
			"Messages":      result.DSLMessages,
			"Values":        formValues(r, entity),
			"RefOptions":    refOptions,
			"EnumOptions":   s.loadEnumOptions(entity),
			"TPRefOptions":  tpRefOpts,
			"TPRefMeta":     tpRefMeta(entity),
			// tpRows мог быть обогащён до *interpreter.Ref хуком проведения
			// (EnrichTPRows мутирует слайсы on place). jsJSON сериализовал бы
			// Ref как объект {UUID,Name,…} → грид показал бы «[object Object]».
			// Нормализуем обратно к UUID-строкам, как в обработчике form-event.
			"TablePartRows": serializeTablePartRowsForEntity(tpRows, entity),
			"FolderOptions": fOpts,
			"IsPopup":       r.FormValue("_popup") == "1",
		})
		return
	}

	// Popup-режим (создание из iframe в родительской форме): не редиректим,
	// а отдаём страничку, которая через postMessage сообщает родителю id
	// и подпись только что созданного объекта, после чего модалка закрывается.
	//
	// Важно: используем локальную fields (ключи в оригинальном регистре
	// после formToFields), а не obj.Fields — Object.Set приводит ключи к
	// нижнему регистру, и firstStringField не находит "Наименование".
	if r.FormValue("_popup") == "1" {
		s.renderPopupSaved(w, obj.ID.String(), firstStringField(fields, entity))
		return
	}

	if action == "post_and_close" {
		http.Redirect(w, r, listURL(entity), http.StatusSeeOther)
		return
	}
	// "post" / "Записать" — остаёмся на форме
	http.Redirect(w, r, "/ui/"+strings.ToLower(string(entity.Kind))+"/"+entity.Name+"/"+obj.ID.String(), http.StatusSeeOther)
}

// refCreateRedirect — точка входа для JS-кнопки «+ Создать» рядом с
// ссылочным полем. Клиент не знает kind целевой сущности (catalog/document)
// — резолвим по имени и редиректим на /ui/<kind>/<name>/new?_popup=1.
func (s *Server) refCreateRedirect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "entity")
	ent := s.reg.GetEntity(name)
	if ent == nil {
		http.Error(w, "Сущность не найдена: "+name, http.StatusNotFound)
		return
	}
	kind := strings.ToLower(string(ent.Kind))
	http.Redirect(w, r, "/ui/"+kind+"/"+ent.Name+"/new?_popup=1", http.StatusFound)
}

// refOpenRedirect — точка входа для иконки-лупы в picker'е («провалиться»
// в карточку выбранного элемента). JS знает имя сущности и id, kind
// резолвим по имени и редиректим на форму редактирования.
func (s *Server) refOpenRedirect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "entity")
	id := chi.URLParam(r, "id")
	ent := s.reg.GetEntity(name)
	if ent == nil {
		http.Error(w, "Сущность не найдена: "+name, http.StatusNotFound)
		return
	}
	kind := strings.ToLower(string(ent.Kind))
	http.Redirect(w, r, "/ui/"+kind+"/"+ent.Name+"/"+id, http.StatusFound)
}

// renderPopupSaved отдаёт минимальную HTML-страницу, которая через
// postMessage передаёт родительскому окну id и подпись созданного объекта.
// Родитель (см. openRefCreate в шаблоне) подставит значение в свой select
// и закроет модалку. Подпись экранируется через encoding/json — то же
// делает шаблон, но здесь без шаблона короче.
func (s *Server) renderPopupSaved(w http.ResponseWriter, id, label string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	idJSON, _ := json.Marshal(id)
	labelJSON, _ := json.Marshal(label)
	fmt.Fprintf(w, `<!doctype html><html><body><script>
try {
  window.parent.postMessage({source:"obRefCreate", id:%s, label:%s}, "*");
} catch (e) {}
</script>Готово.</body></html>`, idJSON, labelJSON)
}

func (s *Server) formEdit(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	row, err := s.store.GetByID(r.Context(), entity.Name, id, entity)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	refOptions, _ := s.loadRefOptions(r.Context(), entity)
	tpRefOpts, _ := s.loadTPRefOptions(r.Context(), entity)
	enumOpts := s.loadEnumOptions(entity)
	vals := make(map[string]string)
	for _, f := range entity.Fields {
		v := row[f.Name]
		if v == nil {
			continue
		}
		if f.Type == metadata.FieldTypeDate {
			if t, ok := v.(time.Time); ok {
				vals[f.Name] = t.In(time.Local).Format("2006-01-02T15:04")
				continue
			}
			// SQLite returns dates as strings — parse and reformat for <input type="datetime-local">
			if s2, ok := v.(string); ok && s2 != "" {
				parsed := false
				for _, layout := range []string{
					time.RFC3339, time.RFC3339Nano,
					"2006-01-02 15:04:05 -0700 MST",
					"2006-01-02 15:04:05.999999999 -0700 MST",
					"2006-01-02T15:04:05", "2006-01-02 15:04:05",
					"2006-01-02T15:04", "2006-01-02",
				} {
					if t, err2 := time.Parse(layout, s2); err2 == nil {
						vals[f.Name] = t.In(time.Local).Format("2006-01-02T15:04")
						parsed = true
						break
					}
				}
				// Last resort: extract just the date prefix
				if !parsed && len(s2) >= 10 {
					if t, err2 := time.ParseInLocation("2006-01-02", s2[:10], time.Local); err2 == nil {
						vals[f.Name] = t.Format("2006-01-02T15:04")
					}
				}
				continue
			}
		}
		vals[f.Name] = fmt.Sprintf("%v", v)
	}
	// Include posted status for documents
	if entity.Kind == metadata.KindDocument {
		vals["posted"] = fmt.Sprintf("%v", row["posted"])
	}
	// _version нужен на форме как hidden — для оптимистической блокировки
	// при последующем POST'е в submitEdit. См. storage.UpsertVersioned.
	if v, ok := row["_version"]; ok && v != nil {
		vals["_version"] = fmt.Sprintf("%v", v)
	}

	tpRows := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		rows, err := s.store.GetTablePartRows(r.Context(), entity.Name, tp.Name, id, tp)
		if err == nil {
			tpRows[tp.Name] = rows
		}
	}

	var folderOptsEdit []map[string]any
	if entity.Hierarchical {
		folderOptsEdit = s.loadFolderOptions(r.Context(), entity)
		if v, ok := row["is_folder"]; ok {
			if asBool(v) {
				vals["is_folder"] = "true"
			} else {
				vals["is_folder"] = "false"
			}
		} else {
			vals["is_folder"] = "false"
		}
		if v, ok := row["parent_id"]; ok && v != nil {
			vals["parent_id"] = fmt.Sprintf("%v", v)
		}
	}

	editUser := auth.UserFromContext(r.Context())
	editIsAdmin := editUser == nil || editUser.IsAdmin

	// Load document movements for posted documents
	var docMovements map[string][]map[string]any
	if entity.Kind == metadata.KindDocument && vals["posted"] == "true" {
		docMovements, _ = s.store.GetDocumentMovements(r.Context(), id, s.reg.Registers())
		for regName, regRows := range docMovements {
			if reg := s.reg.GetRegister(regName); reg != nil {
				s.resolveRegisterRows(r.Context(), regRows, reg)
			}
		}
	}

	s.renderEntityForm(w, r, "object", map[string]any{
		"Entity":        entity,
		"IsNew":         false,
		"Values":        vals,
		"RefOptions":    refOptions,
		"EnumOptions":   enumOpts,
		"TPRefOptions":  tpRefOpts,
		"TPRefMeta":     tpRefMeta(entity),
		"TablePartRows": tpRows,
		"ID":            id.String(),
		"IsAdmin":       editIsAdmin,
		"PrintForms":    s.reg.GetPrintForms(entity.Name),
		"DSLPrintForms": s.reg.GetDSLPrintForms(entity.Name),
		"HasPrintProc":  s.reg.GetProcedure(entity.Name, "Печать") != nil || s.reg.GetProcedure(entity.Name, "Print") != nil,
		"FolderOptions": folderOptsEdit,
		"DocMovements":  docMovements,
		"Error":         buildEditError(r),
		// Receivers — список сущностей, у которых в based_on указан текущий
		// объект. Шаблон рисует выпадающую кнопку «Ввести на основании ▾» —
		// аналог одноимённой команды в 1С:Предприятие.
		"Receivers": s.reg.ReceiversOf(entity.Name),
	})
}

// buildEditError собирает сообщение об ошибке из query-параметров, пришедших
// после redirect'а. posting_error — сбой ОбработкиПроведения; conflict=1 —
// оптимистический конфликт версий (см. storage.ErrVersionConflict).
func buildEditError(r *http.Request) string {
	if r.URL.Query().Get("conflict") == "1" {
		return "Объект был изменён другим пользователем, пока вы редактировали форму. Ваши изменения не сохранены — текущие данные перезагружены."
	}
	return r.URL.Query().Get("posting_error")
}

// renderVersionConflict перезагружает форму редактирования с актуальными
// данными из БД и показывает пользователю сообщение об оптимистическом
// конфликте. Изменения пользователя теряются — это сознательный выбор
// (lost-update лучше тихого перетирания чужих правок).
func (s *Server) renderVersionConflict(w http.ResponseWriter, r *http.Request, entity *metadata.Entity, id uuid.UUID) {
	target := "/ui/" + strings.ToLower(string(entity.Kind)) + "/" + entity.Name + "/" + id.String() + "?conflict=1"
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) submitEdit(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	obj, _, tpRows, action, ok := s.parseSubmitForm(w, r, entity, &id)
	if !ok {
		return
	}

	// Парсим _version для оптимистической блокировки. Если поля нет —
	// expectedVersion=nil, UpsertVersioned не проверяет (поведение как раньше).
	var expectedVersion *int64
	if vStr := r.FormValue("_version"); vStr != "" {
		if v, perr := strconv.ParseInt(vStr, 10, 64); perr == nil {
			expectedVersion = &v
		}
	}

	result, err := s.entitySvc.Save(r.Context(), entityservice.SaveRequest{
		Entity:          entity,
		ID:              obj.ID,
		IsNew:           false,
		Fields:          obj.Fields,
		TablePartRows:   obj.TablePartRows,
		Action:          action,
		ExpectedVersion: expectedVersion,
	})
	if err != nil {
		// Оптимистический конфликт: перечитываем актуальное состояние из БД
		// и показываем пользователю с понятным сообщением. Свои изменения
		// он потеряет — но это лучше, чем тихо перетереть чужие.
		if errors.Is(err, storage.ErrVersionConflict) {
			s.renderVersionConflict(w, r, entity, id)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	if result.DSLError != "" {
		refOptions, _ := s.loadRefOptions(r.Context(), entity)
		tpRefOpts2, _ := s.loadTPRefOptions(r.Context(), entity)
		s.renderEntityForm(w, r, "object", map[string]any{
			"Entity":        entity,
			"IsNew":         false,
			"Error":         result.DSLError,
			"Values":        formValues(r, entity),
			"RefOptions":    refOptions,
			"EnumOptions":   s.loadEnumOptions(entity),
			"TPRefOptions":  tpRefOpts2,
			"TPRefMeta":     tpRefMeta(entity),
			// См. submit: проведение могло обогатить tpRows до *Ref —
			// сериализуем к UUID-строкам, иначе грид покажет «[object Object]».
			"TablePartRows": serializeTablePartRowsForEntity(tpRows, entity),
		})
		return
	}

	if action == "post_and_close" {
		http.Redirect(w, r, listURL(entity), http.StatusSeeOther)
		return
	}
	// "Записать" — остаёмся на форме
	http.Redirect(w, r, "/ui/"+strings.ToLower(string(entity.Kind))+"/"+entity.Name+"/"+id.String(), http.StatusSeeOther)
}

// postDocument posts a document: runs ОбработкаПроведения, writes movements, sets posted=true.
func (s *Server) postDocument(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "post") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}

	row, err := s.store.GetByID(r.Context(), entity.Name, id, entity)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	obj := &runtime.Object{ID: id, Type: entity.Name, Kind: entity.Kind, Fields: make(map[string]any)}
	for _, f := range entity.Fields {
		obj.Fields[f.Name] = row[f.Name]
	}
	tpRows := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		rows, _ := s.store.GetTablePartRows(r.Context(), entity.Name, tp.Name, id, tp)
		s.enrichTPRowsWithRefs(r.Context(), tp, rows)
		tpRows[tp.Name] = rows
	}
	obj.TablePartRows = tpRows

	mc := runtime.NewMovementsCollector(entity.Name, id)
	setPeriodFromFields(mc, entity, obj.Fields)

	docURL := "/ui/" + strings.ToLower(string(entity.Kind)) + "/" + entity.Name + "/" + id.String()
	if errMsg, _ := s.runOnPostCtx(r.Context(), obj, mc); errMsg != "" {
		http.Redirect(w, r, docURL+"?posting_error="+url.QueryEscape(errMsg), http.StatusSeeOther)
		return
	}

	if err := s.store.WithTx(r.Context(), func(ctx context.Context) error {
		if err := s.saveMovements(ctx, entity.Name, id, mc); err != nil {
			return err
		}
		return s.store.SetPosted(ctx, entity.Name, id, true)
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, docURL, http.StatusSeeOther)
}

// clearMovements removes all register movements (accumulation, info, account)
// recorded by the given document, across every register. Passing nil rows to the
// register writers performs only the DELETE-by-recorder step, so iterating all
// registers is safe even for those the document never wrote to.
func (s *Server) clearMovements(ctx context.Context, entityName string, id uuid.UUID) error {
	for _, reg := range s.reg.Registers() {
		if err := s.store.WriteMovements(ctx, reg.Name, entityName, id, nil, reg, nil); err != nil {
			return err
		}
	}
	for _, ir := range s.reg.InfoRegisters() {
		if err := s.store.WriteInfoMovements(ctx, ir.Name, entityName, id, nil, ir, nil); err != nil {
			return err
		}
	}
	for _, ar := range s.reg.AccountRegisters() {
		if err := s.store.WriteAccountMovements(ctx, ar.Name, entityName, id, nil, ar, nil); err != nil {
			return err
		}
	}
	return nil
}

// unpostDocument clears movements and sets posted=false.
func (s *Server) unpostDocument(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "unpost") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}

	if err := s.store.WithTx(r.Context(), func(ctx context.Context) error {
		if err := s.clearMovements(ctx, entity.Name, id); err != nil {
			return err
		}
		return s.store.SetPosted(ctx, entity.Name, id, false)
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, listURL(entity), http.StatusSeeOther)
}

// deleteRecord: admin → permanent delete (with ref check); non-admin → mark for deletion.
func (s *Server) deleteRecord(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "delete") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}

	user := auth.UserFromContext(r.Context())
	isAdmin := user == nil || user.IsAdmin // no auth configured → treat as admin
	markOnly := r.URL.Query().Get("mark") == "1"

	if !isAdmin || markOnly {
		// Non-admin or explicit mark-only: mark for deletion
		if err := s.store.MarkForDeletion(r.Context(), entity.Name, id, true); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, listURL(entity), http.StatusSeeOther)
		return
	}

	// Admin: check references before permanent delete
	refs := s.store.CheckRefs(r.Context(), entity.Name, id, s.reg.Entities())
	if len(refs) > 0 {
		var msg strings.Builder
		lang := s.resolveLang(r)
		msg.WriteString(s.tr(lang, "Невозможно удалить: объект используется в:") + "\n")
		for _, ref := range refs {
			fmt.Fprintf(&msg, "  • %s.%s (%d записей)\n", ref.EntityName, ref.FieldName, ref.Count)
		}
		http.Error(w, msg.String(), 409)
		return
	}

	if err := s.store.WithTx(r.Context(), func(ctx context.Context) error {
		if entity.Posting {
			if err := s.clearMovements(ctx, entity.Name, id); err != nil {
				return err
			}
		}
		return s.store.Delete(ctx, entity.Name, id)
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, listURL(entity), http.StatusSeeOther)
}

// deleteMarkedAll is the global "Удалить помеченные" page accessible from the system menu.
// GET: shows all marked records across every entity.
// POST: deletes all marked records that have no references.
func (s *Server) deleteMarkedAll(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		s.renderForbidden(w, r)
		return
	}

	type markedEntry struct {
		EntityName string
		Kind       string
		ID         string
		Label      string
		HasRefs    bool
	}

	if r.Method == http.MethodPost {
		deleted, skipped := 0, 0
		for _, entity := range s.reg.Entities() {
			marked, err := s.store.ListMarked(r.Context(), entity.Name, entity)
			if err != nil {
				continue
			}
			for _, row := range marked {
				idStr, _ := row["id"].(string)
				id, err := uuid.Parse(idStr)
				if err != nil {
					continue
				}
				refs := s.store.CheckRefs(r.Context(), entity.Name, id, s.reg.Entities())
				if len(refs) > 0 {
					skipped++
					continue
				}
				s.store.WithTx(r.Context(), func(ctx context.Context) error {
					if entity.Posting {
						if err := s.clearMovements(ctx, entity.Name, id); err != nil {
							return err
						}
					}
					for _, tp := range entity.TableParts {
						s.store.Exec(ctx, "DELETE FROM "+metadata.TablePartTableName(entity.Name, tp.Name)+" WHERE parent_id = "+s.store.Dialect().Placeholder(1), id)
					}
					return s.store.Delete(ctx, entity.Name, id)
				})
				deleted++
			}
		}
		http.Redirect(w, r,
			fmt.Sprintf("/ui/delete-marked?deleted=%d&skipped=%d", deleted, skipped),
			http.StatusSeeOther)
		return
	}

	// GET: collect all marked records
	var entries []markedEntry
	for _, entity := range s.reg.Entities() {
		rows, err := s.store.ListMarked(r.Context(), entity.Name, entity)
		if err != nil {
			continue
		}
		for _, row := range rows {
			idStr, _ := row["id"].(string)
			id, _ := uuid.Parse(idStr)
			refs := s.store.CheckRefs(r.Context(), entity.Name, id, s.reg.Entities())
			entries = append(entries, markedEntry{
				EntityName: entity.Name,
				Kind:       string(entity.Kind),
				ID:         idStr,
				Label:      firstStringField(row, entity),
				HasRefs:    len(refs) > 0,
			})
		}
	}

	deleted, _ := strconv.Atoi(r.URL.Query().Get("deleted"))
	skipped, _ := strconv.Atoi(r.URL.Query().Get("skipped"))
	s.render(w, r, "page-delete-marked", map[string]any{
		"Entries": entries,
		"Deleted": deleted,
		"Skipped": skipped,
	})
}

// deleteMarked permanently deletes all deletion_mark=true records without references.
func (s *Server) deleteMarked(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}

	user := auth.UserFromContext(r.Context())
	if user != nil && !user.IsAdmin {
		s.renderForbidden(w, r)
		return
	}

	marked, err := s.store.ListMarked(r.Context(), entity.Name, entity)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	deleted, skipped := 0, 0
	for _, row := range marked {
		idStr, _ := row["id"].(string)
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		refs := s.store.CheckRefs(r.Context(), entity.Name, id, s.reg.Entities())
		if len(refs) > 0 {
			skipped++
			continue
		}
		s.store.WithTx(r.Context(), func(ctx context.Context) error {
			if entity.Posting {
				if err := s.clearMovements(ctx, entity.Name, id); err != nil {
					return err
				}
			}
			return s.store.Delete(ctx, entity.Name, id)
		})
		deleted++
	}

	http.Redirect(w, r,
		fmt.Sprintf("%s?deleted=%d&skipped=%d", listURL(entity), deleted, skipped),
		http.StatusSeeOther)
}

func (s *Server) saveMovements(ctx context.Context, docType string, docID uuid.UUID, mc *runtime.MovementsCollector) error {
	for regName, rows := range mc.All() {
		// try accumulation register first
		reg := s.reg.GetRegister(regName)
		if reg != nil {
			if err := s.store.WriteMovements(ctx, regName, docType, docID, rows, reg, mc.Period); err != nil {
				return err
			}
			continue
		}
		// try account register
		ar := s.reg.GetAccountRegister(regName)
		if ar != nil {
			if err := s.store.WriteAccountMovements(ctx, regName, docType, docID, rows, ar, mc.Period); err != nil {
				return err
			}
			continue
		}
		// try info register (замечание #23)
		ir := s.reg.GetInfoRegister(regName)
		if ir != nil {
			if err := s.store.WriteInfoMovements(ctx, regName, docType, docID, rows, ir, mc.Period); err != nil {
				return err
			}
		}
	}
	return nil
}

// setPeriodFromFields sets the movements period from the first date field of the document.
func setPeriodFromFields(mc *runtime.MovementsCollector, entity *metadata.Entity, fields map[string]any) {
	for _, f := range entity.Fields {
		if f.Type != metadata.FieldTypeDate {
			continue
		}
		// Регистронезависимый поиск: ключи Fields бывают и в PascalCase
		// (formToFields / GetByID), и в lower-case (после Object.Set).
		// Прямой fields[f.Name] промахивался на пути submit → period = time.Now().
		low := strings.ToLower(f.Name)
		for k, v := range fields {
			if strings.ToLower(k) != low {
				continue
			}
			if t := runtime.AsTime(v); !t.IsZero() {
				mc.SetPeriod(t)
			}
			break
		}
		return
	}
}

func (s *Server) registerMovements(w http.ResponseWriter, r *http.Request) {
	name := capitalize(chi.URLParam(r, "name"))
	reg := s.reg.GetRegister(name)
	if reg == nil {
		http.Error(w, "unknown register: "+name, 404)
		return
	}
	if !s.requirePerm(w, r, "register", reg.Name, "read") {
		return
	}
	rows, err := s.store.GetMovements(r.Context(), name, reg)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.resolveRegisterRows(r.Context(), rows, reg)
	s.render(w, r, "page-register-movements", map[string]any{
		"Register": reg,
		"Rows":     rows,
	})
}

func (s *Server) registerBalances(w http.ResponseWriter, r *http.Request) {
	name := capitalize(chi.URLParam(r, "name"))
	reg := s.reg.GetRegister(name)
	if reg == nil {
		http.Error(w, "unknown register: "+name, 404)
		return
	}
	if !s.requirePerm(w, r, "register", reg.Name, "read") {
		return
	}
	rows, err := s.store.GetBalances(r.Context(), name, reg)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.resolveRegisterRows(r.Context(), rows, reg)
	s.render(w, r, "page-register-balances", map[string]any{
		"Register": reg,
		"Rows":     rows,
	})
}

func (s *Server) reportForm(w http.ResponseWriter, r *http.Request) {
	rep := s.getReport(w, r)
	if rep == nil {
		return
	}
	if !s.requirePerm(w, r, "report", rep.Name, "run") {
		return
	}
	// If report has no params, run immediately.
	if len(rep.Params) == 0 {
		s.runReport(w, r, rep, map[string]any{})
		return
	}
	s.render(w, r, "page-report", map[string]any{
		"Report":       rep,
		"ParamValues":  map[string]any{},
		"ReportParams": s.buildReportParams(r.Context(), s.resolveLang(r), rep.Params),
	})
}

func (s *Server) reportRun(w http.ResponseWriter, r *http.Request) {
	rep := s.getReport(w, r)
	if rep == nil {
		return
	}
	if !s.requirePerm(w, r, "report", rep.Name, "run") {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	paramValues := make(map[string]any, len(rep.Params))
	for _, p := range rep.Params {
		val := r.FormValue(p.Name)
		if val == "" {
			paramValues[p.Name] = nil
		} else {
			paramValues[p.Name] = val
		}
	}
	s.runReport(w, r, rep, paramValues)
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request) *reportpkg.Report {
	name := chi.URLParam(r, "name")
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	rep := s.reg.GetReport(name)
	if rep == nil {
		http.Error(w, "unknown report: "+name, 404)
		return nil
	}
	return rep
}

func (s *Server) runReport(w http.ResponseWriter, r *http.Request, rep *reportpkg.Report, paramValues map[string]any) {
	// Build query params: convert date strings to time.Time for proper PG type inference.
	// Keep paramValues unchanged so the form repopulates with the original strings.
	queryValues := make(map[string]any, len(paramValues))
	for k, v := range paramValues {
		queryValues[k] = v
	}
	for _, p := range rep.Params {
		switch p.Type {
		case "date":
			if str, ok := queryValues[p.Name].(string); ok && str != "" {
				if t, err2 := time.ParseInLocation("2006-01-02", str, time.Local); err2 == nil {
					queryValues[p.Name] = t
				}
			}
		case "bool":
			str, _ := queryValues[p.Name].(string)
			queryValues[p.Name] = parseParamValue(str, "bool")
		}
	}
	compiled, err := query.Compile(rep.Query, query.CompileOpts{
		Entities:    s.reg.Entities(),
		Params:      queryValues,
		Registers:   s.reg.Registers(),
		InfoRegs:    s.reg.InfoRegisters(),
		AccountRegs: s.reg.AccountRegisters(),
		Dialect:     s.store.Dialect(),
	})
	reportParams := s.buildReportParams(r.Context(), s.resolveLang(r), rep.Params)
	if err != nil {
		s.render(w, r, "page-report", map[string]any{
			"Report":       rep,
			"QueryError":   err.Error(),
			"ParamValues":  paramValues,
			"ReportParams": reportParams,
		})
		return
	}
	rows, cols, err := s.store.RunQuery(r.Context(), compiled.SQL, compiled.Args)
	if err != nil {
		s.render(w, r, "page-report", map[string]any{
			"Report":       rep,
			"QueryError":   err.Error(),
			"ParamValues":  paramValues,
			"ReportParams": reportParams,
		})
		return
	}
	s.resolveUUIDsInReport(r.Context(), rows)

	var chartOption map[string]any
	if rep.ChartProc != "" {
		chartOption = s.runChartProc(r.Context(), rep, rows, paramValues)
	}

	s.render(w, r, "page-report", map[string]any{
		"Report":       rep,
		"Cols":         cols,
		"Rows":         rows,
		"ParamValues":  paramValues,
		"ChartOption":  chartOption,
		"ReportParams": reportParams,
	})
}

func (s *Server) runChartProc(ctx context.Context, rep *reportpkg.Report, rows []map[string]any, paramValues map[string]any) map[string]any {
	procDecl := s.reg.GetProcedure(rep.Name, rep.ChartProc)
	if procDecl == nil {
		procDecl = s.reg.GetModuleProc(rep.ChartProc)
	}
	if procDecl == nil {
		return nil
	}

	mc := runtime.NewMovementsCollector("report", uuid.Nil)
	dslVars := s.buildDSLVars(ctx, mc)

	resultArray := &interpreter.Array{}
	for _, row := range rows {
		st := interpreter.NewStructFromMap(row)
		resultArray.CallMethod("добавить", []any{st})
	}
	dslVars["Результат"] = resultArray
	dslVars["Result"] = resultArray
	dslVars["Параметры"] = &interpreter.MapThis{M: paramValues}

	var result any
	if err := s.interp.RunWithResult(procDecl, &interpreter.MapThis{M: paramValues}, &result, dslVars); err != nil {
		return nil
	}

	chart, ok := result.(*interpreter.Chart)
	if !ok {
		return nil
	}
	return chart.ToEChartsOption()
}

func (s *Server) processorForm(w http.ResponseWriter, r *http.Request) {
	proc := s.getProcessor(w, r)
	if proc == nil {
		return
	}

	// Managed form path
	if mf := proc.ManagedForm(); mf != nil {
		virtEntity := processorVirtualEntity(proc)
		paramValues := map[string]string{}
		for _, p := range proc.Params {
			if p.Default != nil {
				paramValues[p.Name] = fmt.Sprintf("%v", paramDefaultValue(p.Default, p.Type))
			}
		}
		refOpts, _ := s.loadRefOptions(r.Context(), virtEntity)
		enumOpts := s.loadEnumOptions(virtEntity)
		for k, v := range processorEnumOptions(proc) {
			enumOpts[k] = v
		}
		s.render(w, r, "page-managed-form", map[string]any{
			"Entity":        virtEntity,
			"Form":          mf,
			"IsNew":         true,
			"Values":        paramValues,
			"RefOptions":    refOpts,
			"EnumOptions":   enumOpts,
			"TPRefOptions":  map[string]map[string][]map[string]any{},
			"TPRefMeta":     map[string]map[string]any{},
			"TablePartRows": map[string][]map[string]any{},
			"IsProcessor":   true,
			"Processor":     proc,
		})
		return
	}

	// Auto-generated form (legacy)
	paramValues := map[string]any{}
	for _, p := range proc.Params {
		if p.Default != nil {
			paramValues[p.Name] = paramDefaultValue(p.Default, p.Type)
		}
	}
	refOpts := s.loadProcessorRefOpts(r.Context(), proc.Params)
	s.render(w, r, "page-processor", map[string]any{
		"Processor":   proc,
		"ParamValues": paramValues,
		"RefOptions":  refOpts,
	})
}

// loadProcessorRefOpts returns select options for reference-typed processor params.
func (s *Server) loadProcessorRefOpts(ctx context.Context, params []processorpkg.Param) map[string][]map[string]any {
	opts := make(map[string][]map[string]any)
	for _, p := range params {
		if !strings.HasPrefix(p.Type, "reference:") {
			continue
		}
		entityName := strings.TrimPrefix(p.Type, "reference:")
		entity := s.reg.GetEntity(entityName)
		if entity == nil {
			continue
		}
		rows, err := s.store.List(ctx, entity.Name, entity, storage.ListParams{})
		if err != nil {
			continue
		}
		rows = filterOutFolders(rows)
		for _, row := range rows {
			row["_label"] = firstStringField(row, entity)
		}
		opts[p.Name] = rows
	}
	return opts
}

// paramDefaultValue приводит значение default из YAML обработки к виду,
// пригодному для подстановки в форму параметров.
func paramDefaultValue(def any, typ string) any {
	switch typ {
	case "bool":
		switch d := def.(type) {
		case bool:
			return d
		case string:
			return d == "true" || d == "1" || strings.EqualFold(d, "да")
		default:
			return false
		}
	case "date":
		if t, ok := def.(time.Time); ok {
			return t.Format("2006-01-02")
		}
		return fmt.Sprint(def)
	default:
		return def
	}
}

func (s *Server) processorRun(w http.ResponseWriter, r *http.Request) {
	proc := s.getProcessor(w, r)
	if proc == nil {
		return
	}
	r.ParseMultipartForm(32 << 20) // 32 MB max
	paramValues := map[string]any{}
	for _, p := range proc.Params {
		if p.Type == "file" {
			file, _, err := r.FormFile(p.Name)
			if err == nil {
				data, err := io.ReadAll(file)
				file.Close()
				if err == nil {
					paramValues[p.Name] = decodeUploadText(data)
					continue
				}
			}
			paramValues[p.Name] = ""
			continue
		}
		paramValues[p.Name] = parseParamValue(r.FormValue(p.Name), p.Type)
	}

	procDecl := s.reg.GetProcedure(proc.Name, "Выполнить")
	if procDecl == nil {
		runErr := s.tr(s.resolveLang(r), "Процедура Выполнить() не найдена в src/") + strings.ToLower(string([]rune(proc.Name)[:1])) + string([]rune(proc.Name)[1:]) + ".proc.os"
		if proc.ManagedForm() != nil {
			s.renderProcessorManagedResult(w, r, proc, paramValues, nil, runErr)
		} else {
			refOpts := s.loadProcessorRefOpts(r.Context(), proc.Params)
			s.render(w, r, "page-processor", map[string]any{
				"Processor":   proc,
				"ParamValues": paramValues,
				"RefOptions":  refOpts,
				"RunError":    runErr,
			})
		}
		return
	}

	userKey := userKeyFromRequest(r)
	var messages []string
	msgFunc := interpreter.BuiltinFunc(func(args []any, file string, line int) (any, error) {
		if len(args) > 0 {
			text := fmt.Sprintf("%v", args[0])
			messages = append(messages, text)
			s.messages.Push(userKey, text)
		}
		return nil, nil
	})

	paramsThis := &interpreter.MapThis{M: paramValues}
	mc := runtime.NewMovementsCollector("processor", uuid.Nil)
	dslVars := s.buildDSLVars(r.Context(), mc)
	dslVars["Параметры"] = paramsThis
	dslVars["Сообщить"] = msgFunc
	dslVars["Message"] = msgFunc
	err := s.interp.Run(procDecl, paramsThis, dslVars)

	var runErr string
	if err != nil {
		runErr = err.Error()
	}

	if proc.ManagedForm() != nil {
		s.renderProcessorManagedResult(w, r, proc, paramValues, messages, runErr)
	} else {
		refOpts := s.loadProcessorRefOpts(r.Context(), proc.Params)
		s.render(w, r, "page-processor", map[string]any{
			"Processor":   proc,
			"ParamValues": paramValues,
			"RefOptions":  refOpts,
			"Messages":    messages,
			"RunError":    runErr,
			"Ran":         true,
		})
	}
}

// renderProcessorManagedResult renders processor results via managed form template.
func (s *Server) renderProcessorManagedResult(w http.ResponseWriter, r *http.Request, proc *processorpkg.Processor, paramValues map[string]any, messages []string, runErr string) {
	virtEntity := processorVirtualEntity(proc)
	refOpts, _ := s.loadRefOptions(r.Context(), virtEntity)
	enumOpts := s.loadEnumOptions(virtEntity)
	for k, v := range processorEnumOptions(proc) {
		enumOpts[k] = v
	}
	strValues := make(map[string]string, len(paramValues))
	for k, v := range paramValues {
		strValues[k] = fmt.Sprintf("%v", v)
	}
	s.render(w, r, "page-managed-form", map[string]any{
		"Entity":        virtEntity,
		"Form":          proc.ManagedForm(),
		"IsNew":         true,
		"Values":        strValues,
		"RefOptions":    refOpts,
		"EnumOptions":   enumOpts,
		"TPRefOptions":  map[string]map[string][]map[string]any{},
		"TPRefMeta":     map[string]map[string]any{},
		"TablePartRows": map[string][]map[string]any{},
		"IsProcessor":   true,
		"Processor":     proc,
		"Messages":      messages,
		"RunError":      runErr,
		"Ran":           true,
	})
}

func (s *Server) getProcessor(w http.ResponseWriter, r *http.Request) *processorpkg.Processor {
	name := chi.URLParam(r, "name")
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	proc := s.reg.GetProcessor(name)
	if proc == nil {
		http.Error(w, "unknown processor: "+name, 404)
		return nil
	}
	if !s.requirePerm(w, r, "processor", proc.Name, "run") {
		return nil
	}
	return proc
}

// decodeUploadText tries UTF-8; falls back to Windows-1251.
func decodeUploadText(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(data)
	if err != nil {
		return string(data)
	}
	return string(decoded)
}

// processorVirtualEntity создаёт виртуальную Entity из параметров обработки,
// чтобы managed-форма могла рендерить поля через стандартный pipeline.
func processorVirtualEntity(proc *processorpkg.Processor) *metadata.Entity {
	fields := make([]metadata.Field, 0, len(proc.Params))
	for _, p := range proc.Params {
		f := metadata.Field{
			Name:   p.Name,
			Title:  p.Label,
			Titles: p.Labels,
		}
		switch {
		case p.Type == "string", p.Type == "text":
			f.Type = metadata.FieldTypeString
		case p.Type == "number":
			f.Type = metadata.FieldTypeNumber
		case p.Type == "date":
			f.Type = metadata.FieldTypeDate
		case p.Type == "bool":
			f.Type = metadata.FieldTypeBool
		case p.Type == "choice":
			enumName := "_" + p.Name + "_choice"
			f.Type = metadata.FieldType("enum:" + enumName)
			f.EnumName = enumName
		case strings.HasPrefix(p.Type, "reference:"):
			f.Type = metadata.FieldType("reference:" + strings.TrimPrefix(p.Type, "reference:"))
			f.RefEntity = strings.TrimPrefix(p.Type, "reference:")
		default:
			f.Type = metadata.FieldTypeString
		}
		fields = append(fields, f)
	}
	return &metadata.Entity{
		Name:       proc.Name,
		Title:      proc.Title,
		Titles:     proc.Titles,
		Kind:       metadata.KindCatalog,
		Fields:     fields,
		TableParts: proc.TableParts,
	}
}

// processorEnumOptions возвращает synthetic enum options для choice-параметров
// обработки, дополняя результат loadEnumOptions.
func processorEnumOptions(proc *processorpkg.Processor) map[string][]string {
	opts := make(map[string][]string)
	for _, p := range proc.Params {
		if p.Type == "choice" && len(p.Options) > 0 {
			opts[p.Name] = p.Options
		}
	}
	return opts
}

func parseParamValue(s, typ string) any {
	if typ == "bool" {
		// Чекбокс: значение приходит в форме только когда флажок установлен.
		return s == "true" || s == "on" || s == "1" || strings.EqualFold(s, "да")
	}
	if s == "" {
		return nil
	}
	switch typ {
	case "date":
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
				return t
			}
		}
		return s
	case "number":
		if f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64); err == nil {
			return f
		}
		return s
	default:
		return s
	}
}

// resolveUUIDsInReport replaces UUID-looking strings in report rows with entity display names.
func (s *Server) resolveUUIDsInReport(ctx context.Context, rows []map[string]any) {
	uuidToLabel := make(map[string]string)
	for _, row := range rows {
		for _, v := range row {
			if str, ok := v.(string); ok {
				if _, err := uuid.Parse(str); err == nil {
					uuidToLabel[str] = ""
				}
			}
		}
	}
	if len(uuidToLabel) == 0 {
		return
	}
	for _, entity := range s.reg.Entities() {
		for idStr, label := range uuidToLabel {
			if label != "" {
				continue
			}
			id, _ := uuid.Parse(idStr)
			if refRow, err := s.store.GetByID(ctx, entity.Name, id, entity); err == nil {
				uuidToLabel[idStr] = firstStringField(refRow, entity)
			}
		}
	}
	for _, row := range rows {
		for col, v := range row {
			if str, ok := v.(string); ok {
				if label, found := uuidToLabel[str]; found && label != "" {
					row[col] = label
				}
			}
		}
	}
}

// saveTablePartsDirect persists tablepart rows from the provided map (possibly modified by DSL).
func (s *Server) saveTablePartsDirect(ctx context.Context, entity *metadata.Entity, parentID uuid.UUID, tpRows map[string][]map[string]any) error {
	for _, tp := range entity.TableParts {
		rows := tpRows[tp.Name]
		if rows == nil {
			rows = []map[string]any{}
		}
		if err := s.store.UpsertTablePartRows(ctx, entity.Name, tp.Name, parentID, rows, tp); err != nil {
			return err
		}
	}
	return nil
}

// parseTablePartRows reads tp.{TpName}.{idx}.{FieldName} form values.
func parseTablePartRows(r *http.Request, entity *metadata.Entity) map[string][]map[string]any {
	result := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		// Plan 48 (SlickGrid): check for tp_json.{TPName} first.
		if jsonBlob := r.FormValue("tp_json." + tp.Name); jsonBlob != "" {
			var rows []map[string]any
			if err := json.Unmarshal([]byte(jsonBlob), &rows); err == nil {
				// Normalize field names to original case + convert types.
				cleaned := make([]map[string]any, 0, len(rows))
				for _, row := range rows {
					if len(row) == 0 {
						continue
					}
					converted := make(map[string]any, len(row))
					for _, f := range tp.Fields {
						// Normalize case: try exact, then case-insensitive match.
						var raw string
						if v, ok := row[f.Name]; ok {
							raw = fmt.Sprintf("%v", v)
						} else {
							for k, v := range row {
								if strings.EqualFold(k, f.Name) {
									raw = fmt.Sprintf("%v", v)
									break
								}
							}
						}
						switch f.Type {
						case metadata.FieldTypeNumber:
							if n, err := strconv.ParseFloat(raw, 64); err == nil {
								converted[f.Name] = n
							} else {
								converted[f.Name] = raw
							}
						case metadata.FieldTypeBool:
							converted[f.Name] = raw == "true"
						default:
							converted[f.Name] = raw
						}
					}
					// Skip rows where all fields are blank (like legacy path).
					empty := true
					for _, f := range tp.Fields {
						if v, ok := converted[f.Name]; ok && fmt.Sprintf("%v", v) != "" {
							empty = false
							break
						}
					}
					if !empty {
						cleaned = append(cleaned, converted)
					}
				}
				result[tp.Name] = cleaned
				continue // skip legacy named-input parsing for this TP
			}
		}
		// collect max index
		maxIdx := -1
		prefix := "tp." + tp.Name + "."
		for key := range r.Form {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			rest := strings.TrimPrefix(key, prefix)
			parts := strings.SplitN(rest, ".", 2)
			if len(parts) < 2 {
				continue
			}
			if idx, err := strconv.Atoi(parts[0]); err == nil && idx > maxIdx {
				maxIdx = idx
			}
		}
		if maxIdx < 0 {
			result[tp.Name] = []map[string]any{}
			continue
		}
		rows := make([]map[string]any, maxIdx+1)
		for i := range rows {
			rows[i] = make(map[string]any)
		}
		for key, vals := range r.Form {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			rest := strings.TrimPrefix(key, prefix)
			parts := strings.SplitN(rest, ".", 2)
			if len(parts) < 2 {
				continue
			}
			idx, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			fieldName := parts[1]
			if len(vals) > 0 {
				rows[idx][fieldName] = vals[0]
			}
		}
		// filter empty rows (all fields blank) and convert types
		var cleaned []map[string]any
		for _, row := range rows {
			empty := true
			for _, f := range tp.Fields {
				if v, ok := row[f.Name]; ok && fmt.Sprintf("%v", v) != "" {
					empty = false
					break
				}
			}
			if !empty {
				converted := make(map[string]any, len(row))
				for _, f := range tp.Fields {
					raw := fmt.Sprintf("%v", row[f.Name])
					switch f.Type {
					case metadata.FieldTypeNumber:
						if n, err := strconv.ParseFloat(raw, 64); err == nil {
							converted[f.Name] = n
						} else {
							converted[f.Name] = raw
						}
					case metadata.FieldTypeBool:
						converted[f.Name] = raw == "true"
					default:
						converted[f.Name] = raw
					}
				}
				cleaned = append(cleaned, converted)
			}
		}
		result[tp.Name] = cleaned
	}
	return result
}

// parseListParams reads filter, search, sort and pagination URL params.
// defaultLimit задаёт размер страницы по умолчанию (приходит из настроек базы
// _settings.ui.list_page_size; см. storage.GetListPageSize).
func parseListParams(r *http.Request, entity *metadata.Entity, defaultLimit int) storage.ListParams {
	q := r.URL.Query()
	params := storage.ListParams{
		Filters: make(map[string]storage.FilterValue),
		Sort:    q.Get("sort"),
		Dir:     q.Get("dir"),
		Search:  q.Get("q"),
	}

	// Pagination
	if defaultLimit <= 0 {
		defaultLimit = storage.DefaultListPageSize
	}
	limit := defaultLimit
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= storage.MaxListPageSize {
		limit = l
	}
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 1 {
		page = p
	}
	params.Limit = limit
	params.Offset = (page - 1) * limit

	for _, f := range entity.Fields {
		switch f.Type {
		case metadata.FieldTypeDate:
			from := q.Get("f." + f.Name + ".from")
			to := q.Get("f." + f.Name + ".to")
			if from != "" || to != "" {
				params.Filters[f.Name] = storage.FilterValue{From: from, To: to}
			}
		default:
			val := q.Get("f." + f.Name)
			if val != "" {
				params.Filters[f.Name] = storage.FilterValue{Value: val}
			}
		}
	}
	return params
}

func (s *Server) runOnWrite(obj *runtime.Object, mc *runtime.MovementsCollector) string {
	errMsg, _ := s.runOnWriteCtx(context.Background(), obj, mc)
	return errMsg
}

func (s *Server) buildDSLVars(ctx context.Context, mc *runtime.MovementsCollector) map[string]any {
	// Базовый набор (Перечисления, Константы, Запрос, Предопределённые,
	// Движения, HTTP, Email) — общий с scheduler, см. internal/dslvars.
	vars := dslvars.Common{
		Ctx: ctx, Reg: s.reg, Store: s.store, Mailer: s.mailer, Movements: mc,
	}.Build()

	// TxState несёт «живой» контекст. Транзакционные функции
	// (НачатьТранзакцию и т.д.) и запись справочников из обработки
	// (Справочники.X.Создать().Записать()) используют txState.Ctx(),
	// поэтому запись участвует в открытой DSL-транзакции.
	txState := interpreter.NewTxState(ctx)
	// Caller подключается ДО создания CatalogsRoot.WithManagerCaller —
	// он использует ctx как контекст для вызова процедур менеджера.
	mgrCaller := &managerCaller{s: s, ctx: ctx}
	catalogs := interpreter.NewCatalogsRoot(txState, s.store, s.reg).WithManagerCaller(mgrCaller)
	// Документы.X.Создать()/.Записать()/.Провести() из обработки.
	documents := newDocsRoot(s, txState)
	// РегистрыНакопления.X.Остатки()/.Движения()/.ВыбратьПоРегистратору(Док).
	accumRegs := newAccumRegsRoot(s, txState)
	// #2 managed locks: builtin БлокировкаДанных() возвращает свежий LockObject,
	// привязанный к глобальному менеджеру server'а.
	lockFactory := interpreter.BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
		return runtime.NewLockObject(s.lockMgr), nil
	})

	// API текущего пользователя для персональных настроек.
	// ТекущийПользователь() → объект {ИД, Имя, ПолноеИмя, Админ}.
	// ИмяПользователя()     → строка-логин (или "" для фоновых заданий).
	var curUserID, curUserLogin, curUserFullName string
	var curUserAdmin bool
	if u := auth.UserFromContext(ctx); u != nil {
		curUserID, curUserLogin, curUserFullName, curUserAdmin = u.ID, u.Login, u.FullName, u.IsAdmin
	}
	userObj := &interpreter.MapThis{M: map[string]any{
		"ИД": curUserID, "Имя": curUserLogin, "ПолноеИмя": curUserFullName, "Админ": curUserAdmin,
		"ID": curUserID, "Login": curUserLogin, "FullName": curUserFullName, "IsAdmin": curUserAdmin,
	}}
	currentUserFn := interpreter.BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
		return userObj, nil
	})
	userNameFn := interpreter.BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
		return curUserLogin, nil
	})

	// ЗначениеРеквизитаОбъекта(Ссылка, "Реквизит") — чтение реквизита по
	// ссылке (ссылка несёт лишь UUID/наименование). Использует txState.Ctx(),
	// поэтому видит данные открытой DSL-транзакции.
	attrValueFn := interpreter.BuiltinFunc(func(args []any, _ string, _ int) (any, error) {
		return s.objectAttributeValue(txState.Ctx(), args)
	})

	vars["Справочники"] = catalogs
	vars["Catalogs"] = catalogs
	vars["Документы"] = documents
	vars["Documents"] = documents
	vars["РегистрыНакопления"] = accumRegs
	vars["AccumulationRegisters"] = accumRegs
	vars["БлокировкаДанных"] = lockFactory
	vars["DataLock"] = lockFactory
	vars["ТекущийПользователь"] = currentUserFn
	vars["CurrentUser"] = currentUserFn
	vars["ИмяПользователя"] = userNameFn
	vars["UserName"] = userNameFn
	vars["ЗначениеРеквизитаОбъекта"] = attrValueFn
	vars["ObjectAttributeValue"] = attrValueFn

	// транзакции из DSL (обработки/проведение). Раньше NewTxFunctions
	// использовался только в тестах — отсюда «unknown function
	// НачатьТранзакцию». Теперь подключаем к реальному рантайму.
	for k, v := range interpreter.NewTxFunctions(txState, s.store) {
		vars[k] = v
	}
	for k, v := range interpreter.NewSpreadsheetFunctions() {
		vars[k] = v
	}
	for k, v := range interpreter.NewChartFunctions() {
		vars[k] = v
	}
	return vars
}

func (s *Server) buildDSLVarsWithMessages(ctx context.Context, mc *runtime.MovementsCollector, msgs *[]string) map[string]any {
	vars := s.buildDSLVars(ctx, mc)
	userKey := userKeyFromCtx(ctx)
	msgFunc := interpreter.BuiltinFunc(func(args []any, file string, line int) (any, error) {
		if len(args) > 0 {
			text := fmt.Sprintf("%v", args[0])
			if msgs != nil {
				*msgs = append(*msgs, text)
			}
			s.messages.Push(userKey, text)
		}
		return nil, nil
	})
	vars["Сообщить"] = msgFunc
	vars["Message"] = msgFunc
	return vars
}

func (s *Server) runOnWriteCtx(ctx context.Context, obj *runtime.Object, mc *runtime.MovementsCollector) (string, []string) {
	proc := s.reg.GetProcedure(obj.Type, "OnWrite")
	if proc == nil {
		return "", nil
	}
	// Симметрично runOnPostCtx: ссылки в полях шапки из формы приходят
	// сырыми UUID — обогащаем до *Ref{UUID,Name}, чтобы ЗначениеРеквизитаОбъекта
	// и Строка(ref) работали в ПриЗаписи так же, как при проведении.
	if entity := s.reg.GetEntity(obj.Type); entity != nil {
		s.enrichHeaderRefs(ctx, entity, obj)
	}
	var msgs []string
	vars := s.buildDSLVarsWithMessages(ctx, mc, &msgs)
	if err := s.interp.Run(proc, obj, vars); err != nil {
		if dslErr, ok := err.(*interpreter.DSLError); ok {
			return dslErr.Error(), msgs
		}
		return err.Error(), msgs
	}
	return "", msgs
}

// callManagerProc вызывает процедуру модуля менеджера (X.manager.os) для
// сущности entityName. found=true если процедура объявлена — независимо от
// успеха/ошибки. Используется CatalogProxy/docProxy в качестве fallback после
// встроенных методов (Создать, НайтиПо…, Удалить).
//
// MovementsCollector создаётся пустой (UUID.Nil): методы менеджера не привязаны
// к экземпляру и не пишут движения; если пользователю нужны движения — он
// должен делать Документы.X.Создать().Записать() явно.
func (s *Server) callManagerProc(ctx context.Context, entityName, method string, args []any) (any, bool, error) {
	proc := s.reg.GetManagerProc(entityName, method)
	if proc == nil {
		return nil, false, nil
	}
	mc := runtime.NewMovementsCollector(entityName, uuid.Nil)
	vars := s.buildDSLVars(ctx, mc)
	result, err := s.interp.Call(proc, nil, args, vars)
	return result, true, err
}

// managerCaller адаптер для interpreter.ManagerCaller. Используется в
// buildDSLVars для подключения fallback к CatalogsRoot.
type managerCaller struct {
	s   *Server
	ctx context.Context
}

func (m *managerCaller) CallManager(entityName, method string, args []any) (any, bool, error) {
	return m.s.callManagerProc(m.ctx, entityName, method, args)
}

func (s *Server) runOnPostCtx(ctx context.Context, obj *runtime.Object, mc *runtime.MovementsCollector) (string, []string) {
	proc := s.reg.GetProcedure(obj.Type, "OnPost")
	if proc == nil {
		return "", nil
	}
	// Симметрично табличным частям: ссылки в полях шапки из формы приходят
	// сырыми UUID — обогащаем до *Ref{UUID,Name}, чтобы string-измерения
	// (Склад, Касса, Контрагент) фильтровались по имени, как при проведении
	// из обработки. См. П.37.
	if entity := s.reg.GetEntity(obj.Type); entity != nil {
		s.enrichHeaderRefs(ctx, entity, obj)
	}
	var msgs []string
	vars := s.buildDSLVarsWithMessages(ctx, mc, &msgs)
	if err := s.interp.Run(proc, obj, vars); err != nil {
		if dslErr, ok := err.(*interpreter.DSLError); ok {
			return dslErr.Error(), msgs
		}
		return err.Error(), msgs
	}
	return "", msgs
}

func (s *Server) getEntity(w http.ResponseWriter, r *http.Request) *metadata.Entity {
	raw := chi.URLParam(r, "entity")
	// chi may return the raw percent-encoded path segment — decode it
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		decoded = raw
	}
	if e := s.reg.GetEntityBySlug(decoded); e != nil {
		return e
	}
	http.Error(w, "unknown entity: "+raw, 404)
	return nil
}

// loadEnumOptions returns enum values for each enum-type field of the entity.
func (s *Server) loadEnumOptions(entity *metadata.Entity) map[string][]string {
	opts := make(map[string][]string)
	for _, f := range entity.Fields {
		if f.EnumName == "" {
			continue
		}
		en := s.reg.GetEnum(f.EnumName)
		if en == nil {
			continue
		}
		opts[f.Name] = en.Values
	}
	return opts
}

func (s *Server) usersForSelection(ctx context.Context) []map[string]any {
	if s.authRepo == nil {
		return nil
	}
	users, err := s.authRepo.ListForSelection(ctx)
	if err != nil {
		return nil
	}
	rows := make([]map[string]any, 0, len(users))
	for _, u := range users {
		label := u.Login
		if u.FullName != "" {
			label = u.FullName
		}
		rows = append(rows, map[string]any{"id": u.ID, "_label": label})
	}
	return rows
}

func (s *Server) loadRefOptions(ctx context.Context, entity *metadata.Entity) (map[string][]map[string]any, error) {
	opts := make(map[string][]map[string]any)
	for _, f := range entity.Fields {
		if f.RefEntity == "" {
			continue
		}
		// Special handling: _users is not a catalog entity, but a system table.
		if f.RefEntity == "_users" {
			opts[f.Name] = s.usersForSelection(ctx)
			continue
		}
		refEntity := s.reg.GetEntity(f.RefEntity)
		if refEntity == nil {
			continue
		}
		rows, err := s.store.List(ctx, refEntity.Name, refEntity, storage.ListParams{})
		if err != nil {
			return nil, err
		}
		rows = filterOutFolders(rows)
		for _, row := range rows {
			row["_label"] = firstStringField(row, refEntity)
		}
		opts[f.Name] = rows
	}
	return opts, nil
}

// reportParamUI is a template-friendly wrapper around a report parameter.
type reportParamUI struct {
	Name    string
	Label   string
	Type    string // raw type string
	IsDate  bool
	IsNum   bool
	IsBool  bool
	IsSel   bool
	IsRef   bool
	Options []string         // for IsSel
	Opts    []map[string]any // for IsRef: [{id, _label}]
	// RefEntity — имя сущности (для IsRef), используется на UI для лупы
	// в picker'е (открытие карточки через /ui/_ref-open/<entity>/<id>).
	RefEntity string
}

// buildReportParams builds UI-ready param descriptors, loading reference options inline.
func (s *Server) buildReportParams(ctx context.Context, lang string, params []reportpkg.Param) []reportParamUI {
	out := make([]reportParamUI, 0, len(params))
	for _, p := range params {
		ui := reportParamUI{
			Name:  p.Name,
			Label: p.DisplayLabel(lang),
			Type:  p.Type,
		}
		switch {
		case p.Type == "date":
			ui.IsDate = true
		case p.Type == "number":
			ui.IsNum = true
		case p.Type == "bool":
			ui.IsBool = true
		case p.Type == "select":
			ui.IsSel = true
			ui.Options = p.Options
		case strings.HasPrefix(p.Type, "reference:"):
			ui.IsRef = true
			entityName := strings.TrimPrefix(p.Type, "reference:")
			ui.RefEntity = entityName
			if entity := s.reg.GetEntity(entityName); entity != nil {
				rows, _ := s.store.List(ctx, entity.Name, entity, storage.ListParams{})
				rows = filterOutFolders(rows)
				for _, row := range rows {
					row["_label"] = firstStringField(row, entity)
				}
				ui.Opts = rows
			}
		}
		out = append(out, ui)
	}
	return out
}

// loadReportRefOpts returns select options for report params with type "reference:EntityName".
func (s *Server) loadReportRefOpts(ctx context.Context, params []reportpkg.Param) map[string][]map[string]any {
	opts := make(map[string][]map[string]any)
	for _, p := range params {
		if !strings.HasPrefix(p.Type, "reference:") {
			continue
		}
		entityName := strings.TrimPrefix(p.Type, "reference:")
		entity := s.reg.GetEntity(entityName)
		if entity == nil {
			continue
		}
		rows, err := s.store.List(ctx, entity.Name, entity, storage.ListParams{})
		if err != nil {
			continue
		}
		rows = filterOutFolders(rows)
		for _, row := range rows {
			row["_label"] = firstStringField(row, entity)
		}
		opts[p.Name] = rows
	}
	return opts
}

// loadTPRefOptions returns select options for reference fields in all table parts.
// Result: tpName → fieldName → [{id, _label, ...}]
func (s *Server) loadTPRefOptions(ctx context.Context, entity *metadata.Entity) (map[string]map[string][]map[string]any, error) {
	result := make(map[string]map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		tpOpts := make(map[string][]map[string]any)
		for _, f := range tp.Fields {
			if f.RefEntity == "" {
				continue
			}
			// Always mark the field as a reference (even if catalog empty or missing)
			tpOpts[f.Name] = []map[string]any{}
			refEntity := s.reg.GetEntity(f.RefEntity)
			if refEntity == nil {
				continue
			}
			rows, err := s.store.List(ctx, refEntity.Name, refEntity, storage.ListParams{})
			if err != nil {
				continue
			}
			rows = filterOutFolders(rows)
			for _, row := range rows {
				row["_label"] = firstStringField(row, refEntity)
			}
			tpOpts[f.Name] = rows
		}
		// Always add TP entry so JS knows which fields are references
		result[tp.Name] = tpOpts
	}
	return result, nil
}

// resolveRegisterRows enriches register movement rows with human-readable values:
// recorder_label = "TypeName №Num от Date", dimension UUID values → catalog names.
// refCol описывает колонку строки, значение которой — UUID объекта RefEntity и
// должно быть заменено на наименование. Пустой RefEntity → поиск по всем сущностям
// (legacy string-колонки, хранящие UUID).
type refCol struct {
	Key       string
	RefEntity string
}

// resolveRefColumns заменяет UUID-значения в указанных колонках строк на
// наименования соответствующих объектов. Общее ядро для регистров накопления и
// регистра бухгалтерии (субконто).
func (s *Server) resolveRefColumns(ctx context.Context, rows []map[string]any, cols []refCol) {
	// Build lookup: RefEntity → set of UUIDs to resolve
	entityUUIDs := make(map[string]map[string]string) // entityName → {uuid: label}
	for _, row := range rows {
		for _, c := range cols {
			v := asString(row[c.Key])
			if v == "" {
				continue
			}
			if _, err := uuid.Parse(v); err != nil {
				continue
			}
			if entityUUIDs[c.RefEntity] == nil {
				entityUUIDs[c.RefEntity] = make(map[string]string)
			}
			entityUUIDs[c.RefEntity][v] = ""
		}
	}

	// Resolve UUIDs: for known RefEntity — targeted lookup; for unknown — scan all.
	uuidToLabel := make(map[string]string)
	for entName, uuids := range entityUUIDs {
		var entities []*metadata.Entity
		if entName != "" {
			if e := s.reg.GetEntity(entName); e != nil {
				entities = []*metadata.Entity{e}
			}
		}
		if len(entities) == 0 {
			entities = s.reg.Entities()
		}
		for idStr := range uuids {
			if _, done := uuidToLabel[idStr]; done {
				continue
			}
			id, _ := uuid.Parse(idStr)
			for _, entity := range entities {
				refRow, err := s.store.GetByID(ctx, entity.Name, id, entity)
				if err == nil {
					uuidToLabel[idStr] = firstStringField(refRow, entity)
					break
				}
			}
		}
	}

	for _, row := range rows {
		for _, c := range cols {
			v := asString(row[c.Key])
			if v == "" {
				continue
			}
			if label, found := uuidToLabel[v]; found && label != "" {
				row[c.Key] = label
			}
		}
	}
}

func (s *Server) resolveRegisterRows(ctx context.Context, rows []map[string]any, reg *metadata.Register) {
	// Резолвим UUID и в измерениях, и в атрибутах: reference-атрибут
	// (например Организация) тоже хранит UUID и должен показываться именем.
	refFields := append(append([]metadata.Field{}, reg.Dimensions...), reg.Attributes...)
	cols := make([]refCol, len(refFields))
	for i, f := range refFields {
		cols[i] = refCol{Key: f.Name, RefEntity: f.RefEntity}
	}
	s.resolveRefColumns(ctx, rows, cols)

	// recorder label
	for _, row := range rows {
		recType, _ := row["recorder_type"].(string)
		recIDStr := asString(row["recorder"])
		if recType != "" && recIDStr != "" {
			if recID, err := uuid.Parse(recIDStr); err == nil {
				if entity := s.reg.GetEntityBySlug(recType); entity != nil {
					if docRow, err2 := s.store.GetByID(ctx, entity.Name, recID, entity); err2 == nil {
						num := fmt.Sprintf("%v", docRow["Номер"])
						date := regFmtDate(docRow["Дата"])
						row["recorder_label"] = fmt.Sprintf("%s №%s от %s", entity.Name, num, date)
					}
				}
			}
		}
	}
}

// resolveAccountRows резолвит reference-субконто (хранятся под ключами субконто<N>)
// в наименования. String/enum-субконто оставляет как есть.
func (s *Server) resolveAccountRows(ctx context.Context, rows []map[string]any, ar *metadata.AccountRegister) {
	var cols []refCol
	for i, f := range ar.Subconto {
		if f.RefEntity == "" {
			continue
		}
		cols = append(cols, refCol{Key: metadata.SubcontoColumn(i + 1), RefEntity: f.RefEntity})
	}
	if len(cols) == 0 {
		return
	}
	s.resolveRefColumns(ctx, rows, cols)
}

// asString returns a string from row values that may be string or []byte
// (SQLite drivers differ in what they return for TEXT columns).
func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	}
	return ""
}

func regFmtDate(v any) string {
	if t, ok := v.(time.Time); ok {
		return t.Format("02.01.2006")
	}
	if s, ok := v.(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Format("02.01.2006")
		}
	}
	return fmt.Sprintf("%v", v)
}

func (s *Server) renderForbidden(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusForbidden)
	s.render(w, r, "page-forbidden", map[string]any{})
}

// can reports whether the current request may perform op on (kind, entity).
// A nil user means auth is not configured or no users exist → open access
// (mirrors the IsAdmin defaulting used elsewhere). Admins pass via User.Has,
// which returns true for IsAdmin.
func (s *Server) can(r *http.Request, kind, entity, op string) bool {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return true
	}
	return u.Has(kind, entity, op)
}

// requirePerm renders the 403 page and returns false when op is not allowed.
func (s *Server) requirePerm(w http.ResponseWriter, r *http.Request, kind, entity, op string) bool {
	if s.can(r, kind, entity, op) {
		return true
	}
	s.renderForbidden(w, r)
	return false
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if data == nil {
		data = make(map[string]any)
	}
	if _, ok := data["Cfg"]; !ok {
		data["Cfg"] = s.cfg
	}
	if _, ok := data["Lang"]; !ok {
		data["Lang"] = s.resolveLang(r)
	}
	if _, ok := data["AvailableLangs"]; !ok {
		if s.cfg.Bundle != nil {
			data["AvailableLangs"] = s.cfg.Bundle.Available()
		}
	}
	if _, ok := data["Nav"]; !ok {
		// Нейтральный старт: подсистему не подставляем — на /ui/ ничего не
		// подсвечено, активна «Главная», сайдбар плоский (см. buildNav).
		sub := r.URL.Query().Get("subsystem")
		data["Nav"] = s.buildNav(r, sub)
		data["Subsystems"] = s.reg.Subsystems()
		data["CurrentSubsystem"] = sub
	}
	if _, ok := data["CollapsibleNav"]; !ok {
		data["CollapsibleNav"] = s.store.GetNavCollapsible(r.Context())
	}
	if _, ok := data["IsAdmin"]; !ok {
		data["IsAdmin"] = s.isAdmin(r)
	}
	// Default per-entity permission flags so partial render paths (e.g. validation
	// errors) still show the right action buttons.
	if ent, ok := data["Entity"].(*metadata.Entity); ok {
		kind := string(ent.Kind)
		if _, ok := data["CanWrite"]; !ok {
			data["CanWrite"] = s.can(r, kind, ent.Name, "write")
		}
		if _, ok := data["CanDelete"]; !ok {
			data["CanDelete"] = s.can(r, kind, ent.Name, "delete")
		}
		if _, ok := data["CanPost"]; !ok {
			data["CanPost"] = s.can(r, kind, ent.Name, "post")
		}
		if _, ok := data["CanUnpost"]; !ok {
			data["CanUnpost"] = s.can(r, kind, ent.Name, "unpost")
		}
	}
	// Same for info-register views, which key off "InfoReg" instead of "Entity".
	if ir, ok := data["InfoReg"].(*metadata.InfoRegister); ok {
		if _, ok := data["CanWrite"]; !ok {
			data["CanWrite"] = s.can(r, "inforeg", ir.Name, "write")
		}
		if _, ok := data["CanDelete"]; !ok {
			data["CanDelete"] = s.can(r, "inforeg", ir.Name, "delete")
		}
	}
	if _, ok := data["HasAuth"]; !ok {
		u := auth.UserFromContext(r.Context())
		data["HasAuth"] = s.authRepo != nil && u != nil
		if u != nil {
			data["DenyPasswdChange"] = u.DenyPasswdChange
		}
	}
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (s *Server) allFunctions(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		s.renderForbidden(w, r)
		return
	}
	var catalogs, documents []*metadata.Entity
	for _, e := range s.reg.Entities() {
		if e.Kind == metadata.KindCatalog {
			catalogs = append(catalogs, e)
		} else {
			documents = append(documents, e)
		}
	}
	s.render(w, r, "page-all-functions", map[string]any{
		"Catalogs":      catalogs,
		"Documents":     documents,
		"Registers":     s.reg.Registers(),
		"InfoRegisters": s.reg.InfoRegisters(),
		"Enums":         s.reg.Enums(),
		"Reports":       s.reg.Reports(),
		"Processors":    s.reg.Processors(),
		"Constants":     s.reg.Constants(),
	})
}

// tpRefMeta строит карту tpName → fieldName → {entity, allowCreate} для
// JS-помощника addTpRow: динамически добавленные строки ТЧ рендерят кнопку
// «+ Создать» с правильным целевым справочником, а allowCreate решает
// показывать ли кнопку (дефолт в ТЧ — false, переопределяется в YAML).
func tpRefMeta(entity *metadata.Entity) map[string]map[string]any {
	out := make(map[string]map[string]any, len(entity.TableParts))
	for _, tp := range entity.TableParts {
		m := map[string]any{}
		for _, f := range tp.Fields {
			if f.RefEntity != "" {
				m[f.Name] = map[string]any{
					"entity":      f.RefEntity,
					"allowCreate": f.InlineCreateEnabled(true),
				}
			}
		}
		out[tp.Name] = m
	}
	return out
}

// asBool converts DB boolean values to Go bool.
// SQLite stores booleans as int64(0/1); PostgreSQL returns bool directly.
func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int:
		return t != 0
	}
	return false
}

// filterOutFolders removes rows where is_folder=true (hierarchical catalog groups).
// Used to prevent selecting groups in reference fields of documents/table parts.
func filterOutFolders(rows []map[string]any) []map[string]any {
	out := rows[:0:len(rows)]
	for _, row := range rows {
		if asBool(row["is_folder"]) {
			continue
		}
		out = append(out, row)
	}
	return out
}

func firstStringField(row map[string]any, e *metadata.Entity) string {
	for _, f := range e.Fields {
		if f.Type == metadata.FieldTypeString {
			if v, ok := row[f.Name]; ok && v != nil {
				return fmt.Sprintf("%v", v)
			}
		}
	}
	return fmt.Sprintf("%v", row["id"])
}

func formToFields(r *http.Request, entity *metadata.Entity) map[string]any {
	fields := make(map[string]any)
	for _, f := range entity.Fields {
		val := r.FormValue(f.Name)
		if val == "" {
			fields[f.Name] = nil
			continue
		}
		switch f.Type {
		case metadata.FieldTypeDate:
			parsed := false
			for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
				if t, err := time.ParseInLocation(layout, val, time.Local); err == nil {
					fields[f.Name] = t
					parsed = true
					break
				}
			}
			if !parsed {
				fields[f.Name] = val
			}
		case metadata.FieldTypeBool:
			fields[f.Name] = val == "true"
		case metadata.FieldTypeNumber:
			if n, err := strconv.ParseFloat(val, 64); err == nil {
				fields[f.Name] = n
			} else {
				fields[f.Name] = val
			}
		default:
			fields[f.Name] = val
		}
	}
	return fields
}

func formValues(r *http.Request, entity *metadata.Entity) map[string]string {
	vals := make(map[string]string)
	for _, f := range entity.Fields {
		vals[f.Name] = r.FormValue(f.Name)
	}
	return vals
}

// resolveRefs replaces UUID values of reference fields with the display name
// of the referenced entity (first string field). Modifies rows in place.
func (s *Server) resolveRefs(ctx context.Context, entity *metadata.Entity, rows []map[string]any) {
	for _, f := range entity.Fields {
		if f.RefEntity == "" {
			continue
		}
		refEntity := s.reg.GetEntity(f.RefEntity)
		if refEntity == nil {
			continue
		}
		// collect unique IDs referenced in this field
		seen := map[string]bool{}
		for _, row := range rows {
			if v := row[f.Name]; v != nil {
				seen[fmt.Sprintf("%v", v)] = true
			}
		}
		// resolve each unique ID to a display label
		labels := make(map[string]string, len(seen))
		for idStr := range seen {
			id, err := uuid.Parse(idStr)
			if err != nil {
				continue
			}
			refRow, err := s.store.GetByID(ctx, refEntity.Name, id, refEntity)
			if err != nil {
				continue
			}
			labels[idStr] = firstStringField(refRow, refEntity)
		}
		// replace UUIDs with labels in all rows
		for _, row := range rows {
			if v := row[f.Name]; v != nil {
				if label, ok := labels[fmt.Sprintf("%v", v)]; ok {
					row[f.Name] = label
				}
			}
		}
	}
}

// enrichAuditEntries resolves reference UUIDs and formats dates in audit
// OldValue/NewValue so that the history page shows human-readable values.
func (s *Server) enrichAuditEntries(ctx context.Context, entity *metadata.Entity, entries []*storage.AuditEntry) {
	refFields := map[string]string{}
	dateFields := map[string]bool{}
	for _, f := range entity.Fields {
		if f.RefEntity != "" {
			refFields[f.Name] = f.RefEntity
		}
		if f.Type == metadata.FieldTypeDate {
			dateFields[f.Name] = true
		}
	}
	if len(refFields) == 0 && len(dateFields) == 0 {
		return
	}
	refLabels := map[string]string{}
	for _, e := range entries {
		refEntityName, isRef := refFields[e.Field]
		if !isRef {
			continue
		}
		for _, val := range []any{e.OldValue, e.NewValue} {
			if val == nil {
				continue
			}
			idStr := extractUUIDFromAuditVal(val)
			key := refEntityName + ":" + idStr
			if _, ok := refLabels[key]; ok {
				continue
			}
			id, err := uuid.Parse(idStr)
			if err != nil {
				continue
			}
			refEntity := s.reg.GetEntity(refEntityName)
			if refEntity == nil {
				continue
			}
			refRow, err := s.store.GetByID(ctx, refEntityName, id, refEntity)
			if err != nil {
				continue
			}
			refLabels[key] = firstStringField(refRow, refEntity)
		}
	}
	for _, e := range entries {
		if refEntityName, isRef := refFields[e.Field]; isRef {
			if e.OldValue != nil {
				if label, ok := refLabels[refEntityName+":"+extractUUIDFromAuditVal(e.OldValue)]; ok {
					e.OldValue = label
				}
			}
			if e.NewValue != nil {
				if label, ok := refLabels[refEntityName+":"+extractUUIDFromAuditVal(e.NewValue)]; ok {
					e.NewValue = label
				}
			}
		}
		if dateFields[e.Field] {
			e.OldValue = formatAuditDate(e.OldValue)
			e.NewValue = formatAuditDate(e.NewValue)
		}
	}
}

// enrichAuditEntriesGlobal resolves UUIDs in audit entries that span multiple entities
// (used by the global audit journal). For each entry it looks up the entity by name
// and resolves reference field UUIDs to display names.
func (s *Server) enrichAuditEntriesGlobal(ctx context.Context, entries []*storage.AuditEntry) {
	type entInfo struct {
		refFields  map[string]string
		dateFields map[string]bool
	}
	entityCache := map[string]*entInfo{}
	refLabels := map[string]string{}

	for _, e := range entries {
		if e.Field == "" || e.EntityName == "" {
			continue
		}
		info, ok := entityCache[e.EntityName]
		if !ok {
			ent := s.reg.GetEntity(e.EntityName)
			if ent == nil {
				entityCache[e.EntityName] = nil
				continue
			}
			info = &entInfo{
				refFields:  map[string]string{},
				dateFields: map[string]bool{},
			}
			for _, f := range ent.Fields {
				if f.RefEntity != "" {
					info.refFields[f.Name] = f.RefEntity
				}
				if f.Type == metadata.FieldTypeDate {
					info.dateFields[f.Name] = true
				}
			}
			entityCache[e.EntityName] = info
		}
		if info == nil {
			continue
		}
		refEntityName, isRef := info.refFields[e.Field]
		if !isRef {
			if info.dateFields[e.Field] {
				e.OldValue = formatAuditDate(e.OldValue)
				e.NewValue = formatAuditDate(e.NewValue)
			}
			continue
		}
		for _, val := range []any{e.OldValue, e.NewValue} {
			if val == nil {
				continue
			}
			idStr := extractUUIDFromAuditVal(val)
			if idStr == "" {
				continue
			}
			key := refEntityName + ":" + idStr
			if _, ok := refLabels[key]; ok {
				continue
			}
			id, err := uuid.Parse(idStr)
			if err != nil {
				continue
			}
			refEntity := s.reg.GetEntity(refEntityName)
			if refEntity == nil {
				continue
			}
			refRow, err := s.store.GetByID(ctx, refEntityName, id, refEntity)
			if err != nil {
				continue
			}
			refLabels[key] = firstStringField(refRow, refEntity)
		}
		if e.OldValue != nil {
			if idStr := extractUUIDFromAuditVal(e.OldValue); idStr != "" {
				if label, ok := refLabels[refEntityName+":"+idStr]; ok {
					e.OldValue = label
				}
			}
		}
		if e.NewValue != nil {
			if idStr := extractUUIDFromAuditVal(e.NewValue); idStr != "" {
				if label, ok := refLabels[refEntityName+":"+idStr]; ok {
					e.NewValue = label
				}
			}
		}
	}
}

// extractUUIDFromAuditVal extracts a UUID string from an audit value.
// Handles plain UUID strings and JSON-encoded Ref objects like {"UUID":"abc",...}.
func extractUUIDFromAuditVal(v any) string {
	s, ok := v.(string)
	if !ok {
		s = fmt.Sprintf("%v", v)
	}
	if _, err := uuid.Parse(s); err == nil {
		return s
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		if uid, ok2 := m["UUID"].(string); ok2 {
			return uid
		}
	}
	return ""
}
func formatAuditDate(v any) any {
	if v == nil {
		return nil
	}
	s := fmt.Sprintf("%v", v)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("02.01.2006")
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("02.01.2006")
	}
	return v
}

// enrichTPRowsWithRefs replaces UUID strings in reference fields of table-part rows
// with interpreter.Ref{UUID, Name} so that DSL Строка(ref) returns the display name
// while UUID-based map lookups and SQL parameters still work correctly.
func (s *Server) enrichTPRowsWithRefs(ctx context.Context, tp metadata.TablePart, rows []map[string]any) {
	for _, f := range tp.Fields {
		if f.RefEntity == "" {
			continue
		}
		refEntity := s.reg.GetEntity(f.RefEntity)
		if refEntity == nil {
			continue
		}
		// collect unique IDs
		seen := map[string]bool{}
		for _, row := range rows {
			if v := row[f.Name]; v != nil {
				seen[fmt.Sprintf("%v", v)] = true
			}
		}
		// resolve each unique ID to a display label
		labels := make(map[string]string, len(seen))
		for idStr := range seen {
			id, err := uuid.Parse(idStr)
			if err != nil {
				continue
			}
			refRow, err := s.store.GetByID(ctx, refEntity.Name, id, refEntity)
			if err != nil {
				continue
			}
			labels[idStr] = firstStringField(refRow, refEntity)
		}
		// replace plain UUID strings with *interpreter.Ref{UUID, Name, Manager}
		mgr := s.refManagerFor(refEntity, ctx)
		for _, row := range rows {
			if v := row[f.Name]; v != nil {
				idStr := fmt.Sprintf("%v", v)
				if name, ok := labels[idStr]; ok {
					row[f.Name] = &interpreter.Ref{UUID: idStr, Name: name, Type: refEntity.Name, Manager: mgr}
				}
			}
		}
	}
}

// enrichHeaderRefs заменяет UUID-строки в ссылочных полях ШАПКИ объекта на
// *interpreter.Ref{UUID, Name} — симметрично enrichTPRowsWithRefs для строк
// табличных частей. Без этого ссылки шапки (например Склад) приходят в
// ОбработкаПроведения сырым UUID, и Строка(this.Склад) даёт UUID; фильтр по
// string-измерению (ГДЕ Склад = Строка(this.Склад)) не совпадает с движениями,
// записанными по имени из обработок/сидов. После обогащения шапка ведёт себя
// как при создании из обработки. Ref-параметры и reference-измерения остаются
// корректными: unwrapArrayParams приводит *Ref к UUID. См. П.37.
func (s *Server) enrichHeaderRefs(ctx context.Context, entity *metadata.Entity, obj *runtime.Object) {
	low := strings.ToLower
	for _, f := range entity.Fields {
		if f.RefEntity == "" {
			continue
		}
		refEntity := s.reg.GetEntity(f.RefEntity)
		if refEntity == nil {
			continue
		}
		// Find the actual map key (PascalCase or lowercase) and replace in-place.
		var matchKey string
		var matchVal any
		for k, v := range obj.Fields {
			if low(k) == low(f.Name) {
				matchKey = k
				matchVal = v
				break
			}
		}
		if matchKey == "" || matchVal == nil {
			continue
		}
		if _, isRef := matchVal.(*interpreter.Ref); isRef {
			continue
		}
		idStr := fmt.Sprintf("%v", matchVal)
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		refRow, err := s.store.GetByID(ctx, refEntity.Name, id, refEntity)
		if err != nil {
			continue
		}
		obj.Fields[matchKey] = &interpreter.Ref{
			UUID:    idStr,
			Name:    firstStringField(refRow, refEntity),
			Type:    refEntity.Name,
			Manager: s.refManagerFor(refEntity, ctx),
		}
	}
}

// printDocument renders a print form for a specific document/catalog record.
func (s *Server) printDocument(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	formName := chi.URLParam(r, "form")
	if dec, err2 := url.PathUnescape(formName); err2 == nil {
		formName = dec
	}

	forms := s.reg.GetPrintForms(entity.Name)
	var form *printform.PrintForm
	for _, f := range forms {
		if strings.EqualFold(f.Name, formName) {
			form = f
			break
		}
	}
	if form == nil {
		http.Error(w, "print form not found: "+formName, 404)
		return
	}

	row, err := s.store.GetByID(r.Context(), entity.Name, id, entity)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	tpRows := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		rows, _ := s.store.GetTablePartRows(r.Context(), entity.Name, tp.Name, id, tp)
		tpRows[tp.Name] = rows
	}

	refs := s.buildPrintRefs(r.Context(), row, entity, tpRows)

	constants, _ := s.store.ListConstants(r.Context())

	ctx := &printform.RenderContext{
		Document:   row,
		TableParts: tpRows,
		Constants:  constants,
		Refs:       refs,
	}
	pdfURL := r.URL.Path + "/pdf"
	html, err := printform.RenderWithPDFURL(form, ctx, pdfURL)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// buildPrintRefs returns a map of UUID → {fields...} for all reference fields in the entity and table parts.
func (s *Server) buildPrintRefs(ctx context.Context, row map[string]any, entity *metadata.Entity, tpRows map[string][]map[string]any) map[string]map[string]any {
	refs := make(map[string]map[string]any)
	resolveRef := func(refEntityName, idStr string) {
		if idStr == "" {
			return
		}
		if _, dup := refs[idStr]; dup {
			return
		}
		refEntity := s.reg.GetEntity(refEntityName)
		if refEntity == nil {
			return
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return
		}
		refRow, err := s.store.GetByID(ctx, refEntity.Name, id, refEntity)
		if err != nil {
			return
		}
		refs[idStr] = refRow
	}
	for _, f := range entity.Fields {
		if f.RefEntity == "" {
			continue
		}
		idStr, _ := row[f.Name].(string)
		resolveRef(f.RefEntity, idStr)
	}
	for _, tp := range entity.TableParts {
		rows := tpRows[tp.Name]
		for _, f := range tp.Fields {
			if f.RefEntity == "" {
				continue
			}
			for _, r := range rows {
				idStr, _ := r[f.Name].(string)
				resolveRef(f.RefEntity, idStr)
			}
		}
	}
	return refs
}

// resolveDSLRefs replaces reference UUID strings in row with MapThis objects
// so that DSL dot-notation like Документ.Организация.Наименование works.
func (s *Server) resolveDSLRefs(row map[string]any, fields []metadata.Field, refs map[string]map[string]any) {
	for _, f := range fields {
		if f.RefEntity == "" {
			continue
		}
		v, ok := row[f.Name]
		if !ok {
			continue
		}
		idStr, ok := v.(string)
		if !ok || idStr == "" {
			continue
		}
		refData, ok := refs[idStr]
		if !ok {
			continue
		}
		// Wrap ref data as MapThis for DSL dot-notation access
		row[f.Name] = &interpreter.MapThis{M: refData}
	}
}

// buildHierarchyBreadcrumbs returns the ancestor chain from root to parentID (inclusive).
func (s *Server) buildHierarchyBreadcrumbs(ctx context.Context, entity *metadata.Entity, parentID string) []map[string]string {
	id, err := uuid.Parse(parentID)
	if err != nil {
		return nil
	}
	chain, err := s.store.GetAncestorIDs(ctx, metadata.TableName(entity.Name), id)
	if err != nil {
		return nil
	}
	var crumbs []map[string]string
	for _, ancestorID := range chain {
		row, err := s.store.GetByID(ctx, entity.Name, ancestorID, entity)
		if err != nil {
			continue
		}
		crumbs = append(crumbs, map[string]string{
			"ID":    ancestorID.String(),
			"Label": firstStringField(row, entity),
		})
	}
	return crumbs
}

// loadFolderOptions returns all folder items for a hierarchical catalog (for parent select).
func (s *Server) loadFolderOptions(ctx context.Context, entity *metadata.Entity) []map[string]any {
	rows, err := s.store.List(ctx, entity.Name, entity, storage.ListParams{})
	if err != nil {
		return nil
	}
	var folders []map[string]any
	for _, row := range rows {
		if asBool(row["is_folder"]) {
			row["_label"] = firstStringField(row, entity)
			folders = append(folders, row)
		}
	}
	return folders
}

func listURL(entity *metadata.Entity) string {
	return fmt.Sprintf("/ui/%s/%s", strings.ToLower(string(entity.Kind)), strings.ToLower(entity.Name))
}

func capitalize(s string) string {
	if dec, err := url.PathUnescape(s); err == nil {
		s = dec
	}
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

// sortKeys returns map keys in sorted order (for deterministic template output).
func sortKeys(m map[string]storage.FilterValue) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// filterValue returns the FilterValue for a field from ListParams, or empty.
func filterValue(params storage.ListParams, fieldName string) storage.FilterValue {
	if params.Filters == nil {
		return storage.FilterValue{}
	}
	return params.Filters[fieldName]
}

func (s *Server) getInfoReg(w http.ResponseWriter, r *http.Request) *metadata.InfoRegister {
	name := capitalize(chi.URLParam(r, "name"))
	ir := s.reg.GetInfoRegister(name)
	if ir == nil {
		http.Error(w, "unknown info register: "+name, 404)
	}
	return ir
}

func (s *Server) journalList(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	j := s.reg.GetJournal(name)
	if j == nil {
		http.Error(w, "unknown journal: "+name, 404)
		return
	}

	// Build docs map
	docs := make(map[string]*metadata.Entity, len(j.Documents))
	for _, docName := range j.Documents {
		if e := s.reg.GetEntity(docName); e != nil {
			docs[docName] = e
		}
	}

	// Parse filter params from request
	params := storage.ListParams{Filters: make(map[string]storage.FilterValue)}
	for _, jf := range j.Filters {
		fv := storage.FilterValue{}
		switch {
		case jf.Type == "date_range":
			fv.From = r.URL.Query().Get("f." + jf.Field + ".from")
			fv.To = r.URL.Query().Get("f." + jf.Field + ".to")
		default:
			fv.Value = r.URL.Query().Get("f." + jf.Field)
		}
		params.Filters[jf.Field] = fv
	}

	const pageSize = 50
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	rows, total, colRefMap, err := s.store.JournalQuery(r.Context(), j, docs, params, pageSize, offset)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Resolve ref columns
	s.resolveJournalRefs(r.Context(), j, colRefMap, rows)

	// Load filter options for reference filters
	filterOpts := make(map[string][]map[string]any)
	for _, jf := range j.Filters {
		if !strings.HasPrefix(jf.Type, "reference:") {
			continue
		}
		refName := strings.TrimPrefix(jf.Type, "reference:")
		refEntity := s.reg.GetEntity(refName)
		if refEntity == nil {
			continue
		}
		refRows, err := s.store.List(r.Context(), refEntity.Name, refEntity, storage.ListParams{})
		if err != nil {
			continue
		}
		for _, row := range refRows {
			row["_label"] = firstStringField(row, refEntity)
		}
		filterOpts[jf.Field] = refRows
	}

	// Compute column formats from entity metadata
	colFormats := make(map[string]string)
	for _, jcol := range j.Columns {
		if jcol.Format != "" {
			colFormats[jcol.Field] = jcol.Format
			continue
		}
		for _, entity := range docs {
			for _, f := range entity.Fields {
				if strings.EqualFold(f.Name, jcol.Field) {
					if f.Type == metadata.FieldTypeDate {
						colFormats[jcol.Field] = "date"
					}
					goto nextCol
				}
				for _, fb := range jcol.Fallback {
					if strings.EqualFold(f.Name, fb) && f.Type == metadata.FieldTypeDate {
						colFormats[jcol.Field] = "date"
					}
				}
			}
		}
	nextCol:
	}

	hasNext := offset+pageSize < total
	hasPrev := offset > 0
	prevOffset := offset - pageSize
	if prevOffset < 0 {
		prevOffset = 0
	}

	s.render(w, r, "page-journal", map[string]any{
		"Journal":       j,
		"Rows":          rows,
		"Total":         total,
		"Params":        params,
		"FilterOptions": filterOpts,
		"ColFormats":    colFormats,
		"Offset":        offset,
		"Limit":         pageSize,
		"HasPrev":       hasPrev,
		"HasNext":       hasNext,
		"PrevOffset":    prevOffset,
		"NextOffset":    offset + pageSize,
	})
}

// resolveJournalRefs resolves UUID values in reference journal columns to display labels.
func (s *Server) resolveJournalRefs(ctx context.Context, j *metadata.Journal, colRefMap storage.ColRefMap, rows []map[string]any) {
	for colAlias, refEntityName := range colRefMap {
		refEntity := s.reg.GetEntity(refEntityName)
		if refEntity == nil {
			continue
		}
		// Find the JournalColumn with this field name
		var colField string
		for _, jcol := range j.Columns {
			if strings.ToLower(jcol.Field) == colAlias {
				colField = jcol.Field
				break
			}
		}
		if colField == "" {
			continue
		}
		// Collect unique UUIDs
		seen := map[string]bool{}
		for _, row := range rows {
			if v := row[colField]; v != nil {
				seen[fmt.Sprintf("%v", v)] = true
			}
		}
		// Resolve labels
		labels := make(map[string]string, len(seen))
		for idStr := range seen {
			id, err := uuid.Parse(idStr)
			if err != nil {
				continue
			}
			refRow, err := s.store.GetByID(ctx, refEntity.Name, id, refEntity)
			if err != nil {
				continue
			}
			labels[idStr] = firstStringField(refRow, refEntity)
		}
		// Replace in rows
		for _, row := range rows {
			if v := row[colField]; v != nil {
				if label, ok := labels[fmt.Sprintf("%v", v)]; ok {
					row[colField] = label
				}
			}
		}
	}
}

func (s *Server) infoRegList(w http.ResponseWriter, r *http.Request) {
	ir := s.getInfoReg(w, r)
	if ir == nil {
		return
	}
	if !s.requirePerm(w, r, "inforeg", ir.Name, "read") {
		return
	}
	rows, err := s.store.InfoRegList(r.Context(), ir)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, r, "page-inforeg-list", map[string]any{
		"InfoReg": ir,
		"Rows":    rows,
	})
}

func (s *Server) loadInfoRegRefOpts(ctx context.Context, ir *metadata.InfoRegister) map[string][]map[string]any {
	opts := make(map[string][]map[string]any)
	for _, f := range ir.Dimensions {
		if f.RefEntity == "" {
			continue
		}
		opts[f.Name] = []map[string]any{}
		refEntity := s.reg.GetEntity(f.RefEntity)
		if refEntity == nil {
			continue
		}
		rows, err := s.store.List(ctx, f.RefEntity, refEntity, storage.ListParams{})
		if err != nil {
			continue
		}
		for _, row := range filterOutFolders(rows) {
			id, _ := row["id"].(string)
			label := firstStringField(row, refEntity)
			opts[f.Name] = append(opts[f.Name], map[string]any{"id": id, "_label": label})
		}
	}
	return opts
}

func (s *Server) infoRegForm(w http.ResponseWriter, r *http.Request) {
	ir := s.getInfoReg(w, r)
	if ir == nil {
		return
	}
	if !s.requirePerm(w, r, "inforeg", ir.Name, "write") {
		return
	}
	now := time.Now().Format("2006-01-02")
	s.render(w, r, "page-inforeg-form", map[string]any{
		"InfoReg": ir,
		"Values":  map[string]string{"period": now},
		"Error":   "",
		"RefOpts": s.loadInfoRegRefOpts(r.Context(), ir),
	})
}

func (s *Server) infoRegSubmit(w http.ResponseWriter, r *http.Request) {
	ir := s.getInfoReg(w, r)
	if ir == nil {
		return
	}
	if !s.requirePerm(w, r, "inforeg", ir.Name, "write") {
		return
	}
	r.ParseForm()

	var periodPtr *time.Time
	if ir.Periodic {
		pStr := r.FormValue("period")
		if pStr == "" {
			s.render(w, r, "page-inforeg-form", map[string]any{
				"InfoReg": ir,
				"Values":  formValuesFromRequest(r, ir),
				"Error":   "Период обязателен для периодического регистра",
				"RefOpts": s.loadInfoRegRefOpts(r.Context(), ir),
			})
			return
		}
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, pStr, time.Local); err == nil {
				periodPtr = &t
				break
			}
		}
		if periodPtr == nil {
			s.render(w, r, "page-inforeg-form", map[string]any{
				"InfoReg": ir,
				"Values":  formValuesFromRequest(r, ir),
				"Error":   "Неверный формат даты периода",
				"RefOpts": s.loadInfoRegRefOpts(r.Context(), ir),
			})
			return
		}
	}

	dims := parseInfoRegFields(r, ir.Dimensions)
	resources := parseInfoRegFields(r, ir.Resources)

	if err := s.store.InfoRegSet(r.Context(), ir, dims, resources, periodPtr); err != nil {
		s.render(w, r, "page-inforeg-form", map[string]any{
			"InfoReg": ir,
			"Values":  formValuesFromRequest(r, ir),
			"Error":   err.Error(),
			"RefOpts": s.loadInfoRegRefOpts(r.Context(), ir),
		})
		return
	}
	http.Redirect(w, r, "/ui/inforeg/"+strings.ToLower(ir.Name), http.StatusFound)
}

func (s *Server) infoRegDelete(w http.ResponseWriter, r *http.Request) {
	ir := s.getInfoReg(w, r)
	if ir == nil {
		return
	}
	if !s.requirePerm(w, r, "inforeg", ir.Name, "delete") {
		return
	}
	r.ParseForm()

	var periodPtr *time.Time
	if ir.Periodic {
		if pStr := r.FormValue("period"); pStr != "" {
			for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
				if t, err := time.Parse(layout, pStr); err == nil {
					periodPtr = &t
					break
				}
			}
		}
	}
	dims := parseInfoRegFields(r, ir.Dimensions)
	if err := s.store.InfoRegDelete(r.Context(), ir, dims, periodPtr); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/ui/inforeg/"+strings.ToLower(ir.Name), http.StatusFound)
}

func parseInfoRegFields(r *http.Request, fields []metadata.Field) map[string]any {
	result := make(map[string]any, len(fields))
	for _, f := range fields {
		val := r.FormValue(f.Name)
		if val == "" {
			result[f.Name] = nil
			continue
		}
		result[f.Name] = parseInfoRegFieldValue(f, val)
	}
	return result
}

func parseInfoRegFieldValue(f metadata.Field, val string) any {
	switch f.Type {
	case metadata.FieldTypeDate:
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, val, time.Local); err == nil {
				return t
			}
		}
		return val
	case metadata.FieldTypeBool:
		return val == "true" || val == "on"
	default:
		return val
	}
}

func (s *Server) constantsList(w http.ResponseWriter, r *http.Request) {
	consts := s.reg.Constants()
	sort.Slice(consts, func(i, j int) bool { return consts[i].Name < consts[j].Name })

	values, _ := s.store.ListConstants(r.Context())
	valStrs := make(map[string]string, len(values))
	for k, v := range values {
		valStrs[k] = fmt.Sprintf("%v", v)
	}

	// ref options for reference-type constants
	refOpts := make(map[string][]map[string]any)
	for _, c := range consts {
		if c.RefEntity == "" {
			continue
		}
		refEntity := s.reg.GetEntity(c.RefEntity)
		if refEntity == nil {
			continue
		}
		rows, err := s.store.List(r.Context(), refEntity.Name, refEntity, storage.ListParams{})
		if err != nil {
			continue
		}
		for _, row := range rows {
			row["_label"] = firstStringField(row, refEntity)
		}
		refOpts[c.Name] = rows
	}

	msg := r.URL.Query().Get("saved")
	s.render(w, r, "page-constants", map[string]any{
		"Constants": consts,
		"Values":    valStrs,
		"RefOpts":   refOpts,
		"Saved":     msg == "1",
	})
}

func (s *Server) constantsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	consts := s.reg.Constants()
	for _, c := range consts {
		val := r.FormValue(c.Name)
		var v any
		if val == "" {
			v = nil
		} else {
			v = val
		}
		if err := s.store.SetConstant(r.Context(), c.Name, v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, "/ui/constants?saved=1", http.StatusSeeOther)
}

func formValuesFromRequest(r *http.Request, ir *metadata.InfoRegister) map[string]string {
	vals := map[string]string{"period": r.FormValue("period")}
	for _, f := range ir.Dimensions {
		vals[f.Name] = r.FormValue(f.Name)
	}
	for _, f := range ir.Resources {
		vals[f.Name] = r.FormValue(f.Name)
	}
	return vals
}

// printDocumentPDF renders a print form as PDF and sends it as a download.
func (s *Server) printDocumentPDF(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	formName := chi.URLParam(r, "form")
	if dec, err2 := url.PathUnescape(formName); err2 == nil {
		formName = dec
	}

	forms := s.reg.GetPrintForms(entity.Name)
	var form *printform.PrintForm
	for _, f := range forms {
		if strings.EqualFold(f.Name, formName) {
			form = f
			break
		}
	}
	if form == nil {
		http.Error(w, "print form not found: "+formName, 404)
		return
	}

	row, err := s.store.GetByID(r.Context(), entity.Name, id, entity)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	tpRows := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		rows, _ := s.store.GetTablePartRows(r.Context(), entity.Name, tp.Name, id, tp)
		tpRows[tp.Name] = rows
	}

	refs := s.buildPrintRefs(r.Context(), row, entity, tpRows)
	constants, _ := s.store.ListConstants(r.Context())

	ctx := &printform.RenderContext{
		Document:   row,
		TableParts: tpRows,
		Constants:  constants,
		Refs:       refs,
	}
	pdfBytes, err := printform.RenderPDF(form, ctx)
	if err != nil {
		http.Error(w, "PDF error: "+err.Error(), 500)
		return
	}

	filename := sanitizeFilename(form.Name) + ".pdf"
	if num, ok := row["Номер"].(string); ok && num != "" {
		filename = sanitizeFilename(form.Name+"_"+num) + ".pdf"
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Write(pdfBytes)
}

// printDocumentDSLPF renders a DSL (.os) print form for a document/catalog record.
func (s *Server) printDocumentDSLPF(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	pfName := chi.URLParam(r, "pfName")
	if dec, err2 := url.PathUnescape(pfName); err2 == nil {
		pfName = dec
	}

	// 1. Find DSL print form in registry
	dslForm := s.reg.GetDSLPrintForm(entity.Name, pfName)

	// 2. Also check entity module for a "Печать" procedure
	var procDecl *ast.ProcedureDecl
	var source string

	if dslForm != nil {
		source = dslForm.Source
	} else {
		// Try module procedure: entity module → "Печать"
		procDecl = s.reg.GetProcedure(entity.Name, "Печать")
		if procDecl == nil {
			procDecl = s.reg.GetProcedure(entity.Name, "Print")
		}
		if procDecl == nil {
			http.Error(w, "DSL print form not found: "+pfName, 404)
			return
		}
	}

	// 3. Parse .os source if needed (for standalone print form files)
	if procDecl == nil && source != "" {
		l := lexer.New(source, "printforms/"+pfName+".os")
		p := parser.New(l)
		prog, parseErr := p.ParseProgram()
		if parseErr != nil {
			http.Error(w, "parse error: "+parseErr.Error(), 500)
			return
		}
		for _, proc := range prog.Procedures {
			lower := strings.ToLower(proc.Name.Literal)
			if lower == "сформировать" || lower == "сформироватьпечатнуюформу" || lower == "form" {
				procDecl = proc
				break
			}
		}
		if procDecl == nil {
			http.Error(w, "Функция Сформировать() не найдена в "+pfName+".os", 404)
			return
		}
	}

	// 4. Load record data
	row, err := s.store.GetByID(r.Context(), entity.Name, id, entity)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	tpRows := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		rows, _ := s.store.GetTablePartRows(r.Context(), entity.Name, tp.Name, id, tp)
		tpRows[tp.Name] = rows
	}

	// 5. Resolve references so DSL can access Документ.Организация.Наименование etc.
	refs := s.buildPrintRefs(r.Context(), row, entity, tpRows)
	s.resolveDSLRefs(row, entity.Fields, refs)
	for _, tp := range entity.TableParts {
		for _, tpRow := range tpRows[tp.Name] {
			s.resolveDSLRefs(tpRow, tp.Fields, refs)
		}
	}

	// 6. Build DSL environment
	mc := runtime.NewMovementsCollector(entity.Name, id)
	dslVars := s.buildDSLVars(r.Context(), mc)

	// Embed table parts into document row for Документ.Товары access
	for tpName, rows := range tpRows {
		row[tpName] = rows
	}

	// Convert row + table parts into a DSL object
	docData := &interpreter.MapThis{M: row}
	dslVars["Документ"] = docData
	dslVars["Document"] = docData

	// Pass макет layout as DSL variable (if available)
	if dslForm != nil && dslForm.Layout != nil {
		dslVars["Макет"] = interpreter.NewMaket(dslForm.Layout)
	}

	// 7. Execute the DSL function
	var result any
	err = s.interp.RunWithResult(procDecl, docData, &result, dslVars)
	if err != nil {
		http.Error(w, "DSL error: "+err.Error(), 500)
		return
	}

	// 8. Render result
	sd, ok := result.(*interpreter.SpreadsheetDocument)
	if !ok {
		http.Error(w, "Процедура должна возвращать ТабличныйДокумент", 500)
		return
	}

	// Set back URL for the Назад button
	backPath := fmt.Sprintf("/ui/%s/%s/%s", strings.ToLower(string(entity.Kind)), strings.ToLower(entity.Name), id.String())
	sd.BackURL = backPath

	html := sd.HTMLString()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// listExcel exports an entity list (with current filters) as XLSX.
func (s *Server) listExcel(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	params := parseListParams(r, entity, s.store.GetListPageSize(r.Context()))
	rows, err := s.store.List(r.Context(), entity.Name, entity, params)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.resolveRefs(r.Context(), entity, rows)

	cols := make([]string, 0, len(entity.Fields))
	for _, f := range entity.Fields {
		cols = append(cols, f.Name)
	}

	xlsRows := make([][]any, len(rows))
	for i, row := range rows {
		cells := make([]any, len(cols))
		for j, col := range cols {
			cells[j] = row[col]
		}
		xlsRows[i] = cells
	}

	data, err := excel.ExportList(cols, xlsRows)
	if err != nil {
		http.Error(w, "Excel error: "+err.Error(), 500)
		return
	}
	filename := sanitizeFilename(entity.Name) + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Write(data)
}

// reportExcel runs a report query with GET params and returns XLSX.
func (s *Server) reportExcel(w http.ResponseWriter, r *http.Request) {
	rep := s.getReport(w, r)
	if rep == nil {
		return
	}
	if !s.requirePerm(w, r, "report", rep.Name, "run") {
		return
	}
	paramValues := make(map[string]any, len(rep.Params))
	for _, p := range rep.Params {
		val := r.URL.Query().Get(p.Name)
		if p.Type == "bool" {
			paramValues[p.Name] = parseParamValue(val, "bool")
			continue
		}
		if val == "" {
			paramValues[p.Name] = nil
		} else {
			if p.Type == "date" {
				if t, err := time.ParseInLocation("2006-01-02", val, time.Local); err == nil {
					paramValues[p.Name] = t
				} else {
					paramValues[p.Name] = val
				}
			} else {
				paramValues[p.Name] = val
			}
		}
	}
	compiled, err := query.Compile(rep.Query, query.CompileOpts{
		Entities:    s.reg.Entities(),
		Params:      paramValues,
		Registers:   s.reg.Registers(),
		InfoRegs:    s.reg.InfoRegisters(),
		AccountRegs: s.reg.AccountRegisters(),
		Dialect:     s.store.Dialect(),
	})
	if err != nil {
		http.Error(w, "query compile error: "+err.Error(), 400)
		return
	}
	rows, cols, err := s.store.RunQuery(r.Context(), compiled.SQL, compiled.Args)
	if err != nil {
		http.Error(w, "query error: "+err.Error(), 500)
		return
	}
	s.resolveUUIDsInReport(r.Context(), rows)

	xlsRows := make([][]any, len(rows))
	for i, row := range rows {
		cells := make([]any, len(cols))
		for j, col := range cols {
			cells[j] = row[col]
		}
		xlsRows[i] = cells
	}

	data, err := excel.ExportList(cols, xlsRows)
	if err != nil {
		http.Error(w, "Excel error: "+err.Error(), 500)
		return
	}
	filename := sanitizeFilename(rep.Name) + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Write(data)
}

// journalExcel exports a journal as XLSX.
func (s *Server) journalExcel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	j := s.reg.GetJournal(name)
	if j == nil {
		http.Error(w, "unknown journal: "+name, 404)
		return
	}

	docs := make(map[string]*metadata.Entity, len(j.Documents))
	for _, docName := range j.Documents {
		if e := s.reg.GetEntity(docName); e != nil {
			docs[docName] = e
		}
	}

	params := storage.ListParams{Filters: make(map[string]storage.FilterValue)}
	for _, jf := range j.Filters {
		fv := storage.FilterValue{}
		switch {
		case jf.Type == "date_range":
			fv.From = r.URL.Query().Get("f." + jf.Field + ".from")
			fv.To = r.URL.Query().Get("f." + jf.Field + ".to")
		default:
			fv.Value = r.URL.Query().Get("f." + jf.Field)
		}
		params.Filters[jf.Field] = fv
	}

	rows, _, colRefMap, err := s.store.JournalQuery(r.Context(), j, docs, params, 10000, 0)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.resolveJournalRefs(r.Context(), j, colRefMap, rows)

	cols := make([]string, 0, len(j.Columns)+2)
	cols = append(cols, "Дата", "Вид")
	for _, jcol := range j.Columns {
		cols = append(cols, jcol.Label)
	}

	xlsRows := make([][]any, len(rows))
	for i, row := range rows {
		cells := make([]any, len(cols))
		cells[0] = row["date"]
		cells[1] = row["doc_type"]
		for ji, jcol := range j.Columns {
			cells[2+ji] = row[jcol.Field]
		}
		xlsRows[i] = cells
	}

	data, err := excel.ExportList(cols, xlsRows)
	if err != nil {
		http.Error(w, "Excel error: "+err.Error(), 500)
		return
	}
	filename := sanitizeFilename(j.Name) + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Write(data)
}

// sanitizeFilename replaces characters unsafe for Content-Disposition filename.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// generateNumber returns the next document number.
// Uses the entity's Numerator config if present; falls back to legacy NextNum.
func (s *Server) generateNumber(ctx context.Context, entity *metadata.Entity, fields map[string]any) string {
	if entity.Numerator != nil {
		num := entity.Numerator
		periodKey := storage.ComputePeriodKey(num, fields)
		if n, err := s.store.NextNumber(ctx, entity.Name, periodKey); err == nil {
			return storage.FormatNumber(num.Prefix, num.Length, n)
		}
	}
	// legacy fallback: plain sequential number
	if n, err := s.store.NextNum(ctx, entity.Name); err == nil {
		return fmt.Sprintf("%06d", n)
	}
	return ""
}

// attachmentsList returns JSON list of attachments for a record.
func (s *Server) attachmentsList(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}

	atts, err := s.store.ListAttachments(r.Context(), string(entity.Kind), entity.Name, id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if atts == nil {
		atts = []storage.Attachment{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(atts)
}

// attachmentUpload handles file upload for a record.
func (s *Server) attachmentUpload(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}

	maxSize := s.maxFileSizeBytes
	if maxSize == 0 {
		maxSize = 50 * 1024 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSize+1024)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Ошибка разбора формы: "+err.Error(), 400)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Нет файла в форме", 400)
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	uploadedBy := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		uploadedBy = u.Login
	}

	_, err = s.store.UploadAttachment(r.Context(), string(entity.Kind), entity.Name, id,
		header.Filename, mimeType, uploadedBy, file, maxSize)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusSeeOther)
}

// attachmentDownload serves a file attachment for download.
func (s *Server) attachmentDownload(w http.ResponseWriter, r *http.Request) {
	aid, err := uuid.Parse(chi.URLParam(r, "aid"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}

	f, att, err := s.store.OpenAttachment(r.Context(), aid)
	if err != nil {
		http.Error(w, "Файл не найден", 404)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", att.MimeType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(att.Filename)+"\"")
	http.ServeContent(w, r, att.Filename, att.UploadedAt, f)
}

// attachmentDelete removes a file attachment.
func (s *Server) attachmentDelete(w http.ResponseWriter, r *http.Request) {
	aid, err := uuid.Parse(chi.URLParam(r, "aid"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}

	if err := s.store.DeleteAttachment(r.Context(), aid); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, r.Referer(), http.StatusSeeOther)
}
