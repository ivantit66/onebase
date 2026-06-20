package launcher

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestSaveEnum_PersistsValueTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "enums", "приоритет.yaml", `name: Приоритет
values:
  - Высокий
  - Низкий
`)
	form := url.Values{}
	form.Set("enum_name", "Приоритет")
	form.Set("value.0.name", "Высокий")
	form.Set("value.0.titles.en", "High")
	form.Set("value.1.name", "Низкий")

	rec := postCfgRv(t, "test", "/bases/test/configurator/enum", form, h.configuratorSaveEnum)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p, "name: Высокий", "titles:", "en: High", "- Низкий")
}

func TestSaveEnum_NoTitlesStaysScalar(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "enums", "статус.yaml", `name: Статус
values:
  - Открыт
  - Закрыт
`)
	form := url.Values{}
	form.Set("enum_name", "Статус")
	form.Set("value.0.name", "Открыт")
	form.Set("value.1.name", "Закрыт")

	rec := postCfgRv(t, "test", "/bases/test/configurator/enum", form, h.configuratorSaveEnum)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	assertFileContainsRv(t, p, "- Открыт", "- Закрыт")
	out, _ := os.ReadFile(p)
	if strings.Contains(string(out), "titles:") {
		t.Errorf("без переводов не должно быть titles:\n%s", out)
	}
}

func TestSaveEnum_SkipsEmptyNameInMiddle(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "enums", "статус2.yaml", `name: Статус2
values:
  - Открыт
  - Закрыт
`)
	form := url.Values{}
	form.Set("enum_name", "Статус2")
	form.Set("value.0.name", "Открыт")
	form.Set("value.1.name", "") // «дыра» в середине (очищенное имя)
	form.Set("value.2.name", "Закрыт")

	rec := postCfgRv(t, "test", "/bases/test/configurator/enum", form, h.configuratorSaveEnum)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	// оба непустых значения должны сохраниться (Закрыт не потерян из-за дыры)
	assertFileContainsRv(t, p, "- Открыт", "- Закрыт")
}
