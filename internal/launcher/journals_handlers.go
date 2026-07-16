package launcher

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// ── Журналы документов в конфигураторе ───────────────────────────────────────
//
// Журнал (journals/<Имя>.yaml) — чисто декларативный объект без модуля .os,
// поэтому редактор показывает сырой YAML (как виджет). Сохранение пишет файл
// verbatim из textarea, предварительно проверив, что YAML парсится — иначе
// битый файл сломал бы project.Load всей базы. Имя файла — в нижнем регистре
// (как у ИИ-генератора и прочих объектов), чтобы правка перезаписывала тот же
// файл, а не плодила дубль. saveConfigFile/deleteConfigFile прозрачно работают
// и с файловой конфигурацией, и с конфигурацией в БД.

// journalYAMLRelPath — относительный путь файла журнала (нижний регистр имени).
func journalYAMLRelPath(name string) string { return "journals/" + nameToFilename(name) + ".yaml" }

// configuratorSaveJournal пишет journals/<Имя>.yaml из raw-YAML редактора.
func (h *handler) configuratorSaveJournal(w http.ResponseWriter, r *http.Request) {
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
	name := strings.TrimSpace(r.FormValue("journal_name"))
	if name == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = tr(lang, "Имя журнала не задано")
		renderCfg(w, r, data)
		return
	}
	if !validObjectName(name) {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = tr(lang, "Недопустимое имя журнала")
		renderCfg(w, r, data)
		return
	}
	source := r.FormValue("source")
	// YAML должен как минимум парситься — иначе битый файл journals/*.yaml
	// уронит загрузку всей конфигурации базы.
	var probe map[string]any
	if err := yaml.Unmarshal([]byte(source), &probe); err != nil {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = tr(lang, "Ошибка в YAML журнала") + ": " + err.Error()
		renderCfg(w, r, data)
		return
	}

	saveErr := saveConfigFiles(r, h, b, []configFileEntry{
		{relPath: journalYAMLRelPath(name), content: []byte(source)},
	})

	data := h.loadCfgData(r.Context(), b, "tree")
	if saveErr != nil {
		data.Error = tr(lang, "Ошибка сохранения") + ": " + saveErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = name
		data.SelectedTreeID = "journal-" + name
	}
	renderCfg(w, r, data)
}

// configuratorDeleteJournal удаляет journals/<Имя>.yaml.
func (h *handler) configuratorDeleteJournal(w http.ResponseWriter, r *http.Request) {
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
	name := strings.TrimSpace(r.FormValue("journal_name"))
	if name == "" {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = tr(lang, "Имя журнала не задано")
		renderCfg(w, r, data)
		return
	}
	if !validObjectName(name) {
		data := h.loadCfgData(r.Context(), b, "tree")
		data.Error = tr(lang, "Недопустимое имя журнала")
		renderCfg(w, r, data)
		return
	}
	delErr := deleteConfigFiles(r, h, b, []string{journalYAMLRelPath(name)})
	data := h.loadCfgData(r.Context(), b, "tree")
	if delErr != nil {
		data.Error = tr(lang, "Ошибка удаления") + ": " + delErr.Error()
	}
	renderCfg(w, r, data)
}
