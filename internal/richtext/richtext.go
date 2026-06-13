// Package richtext — санитизация и проекция HTML для полей типа richtext
// (план 65). HTML хранится в TEXT-колонке; картинки — только base64 data-URI
// (внешние URL и javascript:-схемы вырезаются как XSS/приватность-вектор).
//
// Санитайзер применяется на ЗАПИСИ (вход формы → перед сохранением) И на
// ВЫВОДЕ (defense-in-depth перед template.HTML). Plaintext — текстовая
// проекция для списков и поиска.
package richtext

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// MaxBytes — предельный размер richtext-значения (HTML с base64-картинками).
// При превышении сохранение отклоняется с понятной ошибкой. 4 МБ — компромисс:
// несколько встроенных скриншотов помещаются, но БД не раздувается мегабайтными
// вложениями (вынос в attachments — отдельный план).
const MaxBytes = 4 << 20 // 4 МБ

// dataImageSrc — допустимый src у <img>: только base64-кодированная картинка
// в data-URI. Внешние http(s)-URL и javascript: не проходят (вырезаются до
// проверки схемы bluemonday), что закрывает основной XSS/приватность-вектор.
var dataImageSrc = regexp.MustCompile(`^data:image/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/]+={0,2}$`)

// policy — единая bluemonday-политика richtext. Строится один раз: bluemonday
// потокобезопасен для конкурентного Sanitize.
var policy = buildPolicy()

func buildPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// Форматирующие теги (allowlist). Всё прочее (script, style, iframe,
	// object, form, …) вырезается. on*-атрибуты не разрешены нигде.
	p.AllowElements(
		"p", "br",
		"b", "strong", "i", "em", "u", "s",
		"ul", "ol", "li",
		"h1", "h2", "h3",
		"blockquote",
		"span", "div",
	)

	// Ссылки: только безопасные схемы. javascript:/data: у href не проходят.
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireParseableURLs(true)
	p.RequireNoFollowOnLinks(true)

	// Картинки: ТОЛЬКО base64 data-URI. Matching-регексп на src отбрасывает
	// внешние/опасные URL ещё до проверки схемы; AllowDataURIImages добавляет
	// валидацию самого data:image/...;base64,... (mime + корректный base64).
	p.AllowAttrs("src").Matching(dataImageSrc).OnElements("img")
	p.AllowAttrs("alt").OnElements("img")
	p.AllowDataURIImages()

	// style НЕ разрешаем (вырезается целиком) — узкий allowlist стилей легко
	// обходится, безопасность в MVP приоритетнее форматирования через style.

	return p
}

// Sanitize очищает HTML по политике richtext: оставляет форматирующие теги и
// data-URI картинки, вырезает script/on*-атрибуты/style/javascript:/внешние
// src у img. Безопасно для вывода через template.HTML.
func Sanitize(htmlStr string) string {
	return policy.Sanitize(htmlStr)
}

var wsRun = regexp.MustCompile(`\s+`)

// Plaintext возвращает текст без тегов (для списков, проекций, поиска).
// Пробельные последовательности (включая переводы строк от блочных тегов)
// сжимаются в один пробел, края обрезаются.
//
// ВНИМАНИЕ (безопасность): результат HTML-ДЕКОДИРОВАН — html.Tokenizer.Text()
// раскрывает сущности (`&lt;` → `<`, `&amp;` → `&`), поэтому выходная строка
// может содержать «голые» `<`, `>`, `&` и НЕ является безопасным HTML.
// Потребитель ОБЯЗАН экранировать вывод — отдавать только через
// автоэкранирование `{{ }}` (text/template → html/template). НЕЛЬЗЯ оборачивать
// результат в template.HTML или вставлять в DOM как innerHTML: это вернёт
// XSS-вектор, который вырезает Sanitize.
func Plaintext(htmlStr string) string {
	var sb strings.Builder
	z := html.NewTokenizer(strings.NewReader(htmlStr))
	skip := 0 // глубина внутри script/style — текст таких узлов не выводим
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return strings.TrimSpace(wsRun.ReplaceAllString(sb.String(), " "))
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			n := string(name)
			if n == "script" || n == "style" {
				if tt == html.StartTagToken {
					skip++
				}
			} else if isBlockTag(n) {
				sb.WriteByte(' ')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			n := string(name)
			if n == "script" || n == "style" {
				if skip > 0 {
					skip--
				}
			} else if isBlockTag(n) {
				sb.WriteByte(' ')
			}
		case html.TextToken:
			if skip == 0 {
				sb.Write(z.Text())
			}
		}
	}
}

func isBlockTag(n string) bool {
	switch n {
	case "p", "br", "div", "li", "ul", "ol", "h1", "h2", "h3", "blockquote", "tr":
		return true
	}
	return false
}
