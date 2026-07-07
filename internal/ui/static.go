package ui

import (
	_ "embed"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/webassets"
)

//go:embed static/ui.js
var uiJS []byte

//go:embed static/managed.js
var managedJS []byte

// mountStatic регистрирует отдачу общих встроенных ассетов. Самохостинг вместо
// CDN: графики и редактор работают офлайн — десктопная база не должна зависеть
// от интернета. ECharts и Monaco вендорятся один раз в webassets и раздаются
// тем же путём, что и в конфигураторе лаунчера, чтобы рабочий стол и
// предпросмотр виджетов рисовались идентично.
func mountStatic(r chi.Router) {
	r.Get("/static/ui.js", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(uiJS)
	})
	r.Get("/static/managed.js", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(managedJS)
	})
	r.Handle("/vendor/echarts/*", http.StripPrefix("/vendor/echarts/", webassets.EChartsHandler()))
	// Monaco editor — инструменты разработчика (консоль кода/запросов, отладчик)
	// грузят его офлайн вместо CDN.
	r.Handle("/vendor/monaco/*", http.StripPrefix("/vendor/monaco/", webassets.MonacoHandler()))
	// SlickGrid — грид для редактируемых табличных частей managed-форм.
	r.Handle("/vendor/slickgrid/*", http.StripPrefix("/vendor/slickgrid/", webassets.SlickGridHandler()))
	// Quill — WYSIWYG-редактор для richtext-полей (план 65). Раздаётся только в
	// пользовательском режиме: формы сущностей с richtext-реквизитами здесь.
	// Конфигуратор лаунчера правит метаданные, а не данные — Quill ему не нужен.
	r.Handle("/vendor/quill/*", http.StripPrefix("/vendor/quill/", webassets.QuillHandler()))
}
