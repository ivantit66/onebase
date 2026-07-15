package launcher

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/webassets"
)

// TestConfiguratorJS_AiBootWaitsForDOMReady — регрессия: плавающие кнопки ИИ
// (cfgai-btn/cfggen-btn) подключены в шаблоне НИЖЕ <script src=configurator.js>,
// поэтому IIFE конфигуратора выполняется, когда их ещё нет в DOM. Раньше
// initAiHandlers() падал на null.addEventListener, и автозапуск cfgAiRefresh()
// не доходил — кнопки не появлялись, пока не вызвать cfgAiRefresh() вручную
// (куратор чата при этом тоже не открывался на клик). Фикс: автозапуск ждёт
// готовности DOM через bootCfgAi — так же, как initTree/initResizers выше по
// файлу. См. configurator_tmpl_shell.go: <script> идёт раньше cfgai-btn/cfggen-btn.
func TestConfiguratorJS_AiBootWaitsForDOMReady(t *testing.T) {
	js := configuratorJS(t)
	if !strings.Contains(js, "function bootCfgAi") {
		t.Fatal("нет функции bootCfgAi: автозапуск cfgAi не обёрнут в DOMContentLoaded-гард")
	}
	if !strings.Contains(js, "document.readyState") || !strings.Contains(js, "DOMContentLoaded") {
		t.Fatal("ожидалась проверка document.readyState с регистрацией bootCfgAi на DOMContentLoaded")
	}
}

// TestNoStore_DisablesCacheForEmbedAssets — embed-статика (configurator.js,
// Monaco, ECharts, SlickGrid) живёт в бинаре и обновляется только при пересборке.
// embed.FS отдаёт стабильный Last-Modified, поэтому WebView2/браузер отвечают
// 304 Not Modified и бесконечно переиспользуют копию из предыдущей сборки —
// обычный F5 против этого бессилен (это и маскировало регрессию выше). noStore
// должен ставить Cache-Control: no-store на каждый ответ embed-handler'а.
func TestNoStore_DisablesCacheForEmbedAssets(t *testing.T) {
	// EChartsHandler intentionally sets an immutable year-long cache policy.
	// Test the real composition used by the launcher, not a dummy handler that
	// never tries to overwrite Cache-Control.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/vendor/echarts/echarts.min.js", nil)
	h := noStore(http.StripPrefix("/vendor/echarts/", webassets.EChartsHandler()))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET ECharts: status=%d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control: want %q, got %q", "no-store", got)
	}
}
