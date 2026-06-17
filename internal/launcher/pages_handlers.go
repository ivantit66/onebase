package launcher

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/page"
)

// ── Страницы в конфигураторе (план 66, доработка 2) ──────────────────────────
//
// Объект «страница» = метаданные pages/<Имя>.yaml + обработчик src/<Имя>.page.os
// (Процедура ПриФормировании). Структурно — близнец обработки (processors/*.yaml
// + src/*.proc.os), поэтому редактор устроен так же: метаданные + код + кнопки
// Сохранить/Проверить/Удалить с перерисовкой дерева через renderCfg.
//
// Имена файлов сохраняем в исходном регистре имени страницы (как демо
// pages/Панель.yaml), чтобы правка перезаписывала тот же файл на регистро-
// зависимых ФС, а не плодила дубль. saveConfigFile/deleteConfigFile прозрачно
// работают и с файловой конфигурацией, и с конфигурацией в БД.

// configuratorSavePage пишет pages/<Имя>.yaml (метаданные) и src/<Имя>.page.os
// (обработчик). Переводы заголовка (titles) переносятся из существующего файла —
// визуальный редактор их не теряет (отдельная правка — план 66 п.3, i18n).
func (h *handler) configuratorSavePage(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	lang := resolveLang(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("page_name"))
	if name == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = tr(lang, "Имя страницы не задано")
		renderCfg(w, r, data)
		return
	}

	pg := &page.Page{
		Name:   name,
		Title:  strings.TrimSpace(r.FormValue("title")),
		Icon:   strings.TrimSpace(r.FormValue("icon")),
		Roles:  splitConfigList(r.FormValue("roles")),
		Params: splitConfigList(r.FormValue("params")),
	}
	// Переносим titles из текущей конфигурации (визуальный редактор их не правит).
	if proj, lerr := h.loadProjectFor(r.Context(), b); lerr == nil && proj != nil {
		for _, ex := range proj.Pages {
			if strings.EqualFold(ex.Name, name) {
				pg.Titles = ex.Titles
				break
			}
		}
		proj.Close()
	}

	yamlBody, merr := page.Marshal(pg)
	if merr != nil {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = tr(lang, "Ошибка сохранения") + ": " + merr.Error()
		renderCfg(w, r, data)
		return
	}

	saveErr := saveConfigFile(r, h, b, pageYAMLRelPath(name), yamlBody)
	if saveErr == nil {
		saveErr = saveConfigFile(r, h, b, pageSrcRelPath(name), []byte(r.FormValue("source")))
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = tr(lang, "Ошибка сохранения") + ": " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = name
	}
	renderCfg(w, r, data)
}

// configuratorDeletePage удаляет pages/<Имя>.yaml и src/<Имя>.page.os.
func (h *handler) configuratorDeletePage(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	lang := resolveLang(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("page_name"))
	if name == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = tr(lang, "Имя страницы не задано")
		renderCfg(w, r, data)
		return
	}

	delErr := deleteConfigFile(r, h, b, pageYAMLRelPath(name))
	if delErr == nil {
		delErr = deleteConfigFile(r, h, b, pageSrcRelPath(name))
	}

	data := h.loadCfgData(r.Context(), b, "tree")
	if delErr != nil {
		data.Error = tr(lang, "Ошибка удаления") + ": " + delErr.Error()
	}
	renderCfg(w, r, data)
}

// pageYAMLRelPath / pageSrcRelPath — относительные пути файлов страницы.
// Регистр имени сохраняется намеренно (см. комментарий к файлу).
func pageYAMLRelPath(name string) string { return "pages/" + name + ".yaml" }
func pageSrcRelPath(name string) string  { return "src/" + name + ".page.os" }

// splitConfigList разбирает список значений из текстового поля редактора:
// разделители — запятая, точка с запятой или перевод строки; пустые элементы
// и пробелы по краям отбрасываются. Пустой ввод → nil (поле опускается в YAML).
func splitConfigList(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	var out []string
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
