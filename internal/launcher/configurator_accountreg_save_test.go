package launcher

import (
	"net/http"
	"net/url"
	"testing"
)

// Регрессия: сохранение плана счетов собирало свежий saveAccountReg без полей
// subconto/titles и без чтения файла, молча стирая блок subconto (аналитические
// разрезы) и многоязычные titles. Фикс: read-merge сохраняет их.
func TestConfiguratorSaveAccountRegister_PreservesSubcontoTitles(t *testing.T) {
	h, cfgDir := newFileBaseHandler(t)
	h.runner = NewRunner()
	p := writeCfgFileRv(t, cfgDir, "accountregs", "бухучёт.yaml", `name: Бухучёт
title: Бухучёт
titles:
  en: Accounting
accounts: ОсновнойПланСчетов
subconto:
  - name: Контрагенты
    type: string
  - name: Договоры
    type: string
resources:
  - name: Сумма
    type: number
`)
	form := url.Values{}
	form.Set("accountreg", "Бухучёт")
	form.Set("title", "Бухучёт")
	form.Set("accounts", "ОсновнойПланСчетов")
	form.Set("res.0.name", "Сумма")
	form.Set("res.0.type", "number")

	rec := postCfgRv(t, "test", "/bases/test/configurator/account-register", form, h.configuratorSaveAccountRegister)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	assertFileContainsRv(t, p,
		"subconto:", "name: Контрагенты", "name: Договоры",
		"titles:", "en: Accounting",
		"name: Сумма")
}
