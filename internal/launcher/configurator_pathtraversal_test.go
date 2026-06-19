package launcher

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// Безопасность: имя страницы/объекта не должно позволять обход каталога —
// page_name вроде "../../evil" не должен писать/удалять файлы вне проекта.
func TestConfiguratorSavePage_RejectsPathTraversal(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()

	form := url.Values{}
	form.Set("page_name", "../../evil")
	form.Set("source", "ПлохойКод")
	rec := postCfgRv(t, "test", "/bases/test/configurator/page", form, h.configuratorSavePage)

	// Файлы вне проекта (parent(cfgDir)/evil.*) не должны появиться.
	for _, outside := range []string{
		filepath.Join(cfgDir, "..", "evil.yaml"),
		filepath.Join(cfgDir, "..", "evil.page.os"),
	} {
		if _, err := os.Stat(outside); err == nil {
			t.Fatalf("traversal записал файл вне проекта: %s", outside)
		}
	}
	// Ответ должен быть ошибкой (ok=false).
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OK {
		t.Errorf("ожидался ok=false для имени с обходом каталога, тело: %s", rec.Body.String())
	}
}
