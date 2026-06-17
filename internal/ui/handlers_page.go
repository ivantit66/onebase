package ui

// Произвольные страницы на DSL (план 66): /ui/page/{name}. Метаданные —
// pages/<имя>.yaml, обработчик — src/<имя>.page.os (Процедура
// ПриФормировании(Страница, Параметры) Экспорт). Обработчик наполняет
// построитель «Страница» блоками, которые рендерятся в общую оболочку.

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/page"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/widget"
)

// pageChartData конвертирует чарт-блок страницы (план 66) в widget.ChartData,
// чтобы переиспользовать echartsJSON/EChartsOption (та же отрисовка, что у
// виджетов рабочего стола). Подключается в FuncMap как "pageChart".
func pageChartData(c *interpreter.PageChart) *widget.ChartData {
	if c == nil {
		return nil
	}
	cd := &widget.ChartData{Kind: c.Kind, XAxis: c.XAxis}
	for _, s := range c.Series {
		cd.Series = append(cd.Series, widget.ChartSeries{Name: s.Name, Data: s.Data})
	}
	return cd
}

// canSeePage сообщает, видна ли страница пользователю по ролям. Пустые roles —
// видна всем; nil-пользователь (аутентификация не настроена) — видна; иначе
// требуется одна из ролей (администратор проходит через HasAnyRole).
func (s *Server) canSeePage(r *http.Request, pg *page.Page) bool {
	if len(pg.Roles) == 0 {
		return true
	}
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return true
	}
	return u.HasAnyRole(pg.Roles)
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	pg := s.reg.GetPage(name)
	if pg == nil {
		http.NotFound(w, r)
		return
	}

	// Роли (как у HTTP-сервисов): аутентифицированный пользователь без нужной
	// роли страницы не видит. nil-пользователь (аутентификация не настроена) —
	// открытый доступ, как и в can(). Администратор проходит (HasAnyRole).
	if len(pg.Roles) > 0 {
		if u := auth.UserFromContext(r.Context()); u != nil && !u.HasAnyRole(pg.Roles) {
			s.renderForbidden(w, r)
			return
		}
	}

	lang := s.resolveLang(r)
	title := pg.DisplayName(lang)

	proc := s.reg.GetPageProcedure(pg.Name, "ПриФормировании")
	if proc == nil {
		s.render(w, r, "page-custom", map[string]any{
			"PageTitle": title,
			"PageError": s.tr(lang, "обработчик ПриФормировании не найден в") + " src/" + strings.ToLower(pg.Name) + ".page.os",
		})
		return
	}

	// Параметры строки запроса → Структура «Параметры».
	params := map[string]string{}
	for k, vs := range r.URL.Query() {
		if len(vs) > 0 {
			params[k] = vs[0]
		}
	}
	paramsObj := interpreter.NewStringMap(params)
	builder := interpreter.NewPageBuilder()

	var msgs []string
	mc := runtime.NewMovementsCollector("page", uuid.Nil)
	dslVars := s.buildDSLVarsWithMessages(r.Context(), mc, &msgs)
	dslVars["Страница"] = builder
	dslVars["Page"] = builder
	dslVars["Параметры"] = paramsObj
	dslVars["Parameters"] = paramsObj

	if _, err := s.interp.Call(proc, builder, []any{builder, paramsObj}, dslVars); err != nil {
		s.render(w, r, "page-custom", map[string]any{
			"PageTitle": title,
			"PageError": s.errText(r, err),
		})
		return
	}

	blocks := builder.Blocks()
	hasChart := false
	for _, b := range blocks {
		if b.Kind == "chart" {
			hasChart = true
			break
		}
	}
	s.render(w, r, "page-custom", map[string]any{
		"PageTitle":    title,
		"PageBlocks":   blocks,
		"PageHasChart": hasChart,
	})
}
