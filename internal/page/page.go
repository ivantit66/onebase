// Package page описывает объект «страница» конфигурации onebase — произвольное
// представление, которое открывается из меню и формируется обработчиком на
// встроенном языке (DSL). Этот пакет — чистые метаданные (YAML-загрузчик);
// исполнение обработчика и рендер блоков живут в internal/ui, а объект-построитель
// «Страница» и его блоки — в internal/dsl/interpreter (план 66).
//
// Структура каталога проекта:
//
//	pages/<имя>.yaml      — метаданные страницы (заголовок, роли, параметры)
//	src/<имя>.page.os     — обработчик (Процедура ПриФормировании(Страница, Параметры) Экспорт …)
package page

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Page — опубликованная страница. Открывается по адресу /ui/page/<Name> и
// регистрируется в навигации подсистемы строкой `pages:` (см. SubsystemContents).
type Page struct {
	Name   string            `yaml:"name"`
	Title  string            `yaml:"title"`
	Titles map[string]string `yaml:"titles"`
	Icon   string            `yaml:"icon"`
	// Roles — если непусто, страница видна/доступна только аутентифицированному
	// пользователю с одной из перечисленных ролей (администратор — всегда).
	Roles []string `yaml:"roles"`
	// Params — объявленные имена параметров строки запроса (?период=…). Доезжают
	// в обработчик как Структура «Параметры». Необъявленные параметры запроса
	// тоже передаются — список нужен прежде всего для документации/конфигуратора.
	Params []string `yaml:"params"`
}

// DisplayName возвращает заголовок страницы с учётом языка.
func (p *Page) DisplayName(lang string) string {
	if lang != "" {
		if v, ok := p.Titles[lang]; ok && v != "" {
			return v
		}
	}
	if p.Title != "" {
		return p.Title
	}
	return p.Name
}

// Normalize приводит страницу к каноничному виду: имя без обрамляющих пробелов,
// дефолтный заголовок. Вызывается загрузчиком; экспортирован для программного
// построения страниц (тесты/конфигуратор).
func (p *Page) Normalize() {
	p.Name = strings.TrimSpace(p.Name)
	if p.Title == "" {
		p.Title = p.Name
	}
}

// LoadFile читает одну страницу из YAML. Если в файле не задано name — берётся
// из имени файла (как у подсистем/сервисов).
func LoadFile(path string) (*Page, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Page
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		p.Name = strings.TrimSuffix(filepath.Base(path), ".yaml")
	}
	p.Normalize()
	return &p, nil
}

// LoadDir читает pages/*.yaml. Отсутствующая папка — не ошибка (возвращает nil).
func LoadDir(dir string) ([]*Page, error) {
	items, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pages []*Page
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			continue
		}
		p, err := LoadFile(filepath.Join(dir, item.Name()))
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Name < pages[j].Name })
	return pages, nil
}
