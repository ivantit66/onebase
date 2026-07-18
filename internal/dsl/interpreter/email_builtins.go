package interpreter

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// EmailSender is the minimal interface required by email DSL functions.
type EmailSender interface {
	Send(to, subject, textBody, htmlBody string) error
	Configured() bool
}

// EmailAttachment — вложение письма (имя файла, MIME-тип, содержимое).
type EmailAttachment struct {
	Name     string
	MimeType string
	Data     []byte
}

// EmailAttachmentSender — необязательное расширение EmailSender: отправка
// письма с вложениями. Реализуется mailer.Mailer; проверяется type-assertion
// в момент отправки, чтобы существующие реализации EmailSender (моки в
// тестах) не требовали доработки.
type EmailAttachmentSender interface {
	SendWithAttachments(to, subject, textBody, htmlBody string, files []EmailAttachment) error
}

// EmailFileResolver optionally authorizes and resolves a path before an email
// attachment is read. UI uses it for RLS-checked attachment-storage paths,
// which intentionally live outside the ordinary DSL file sandbox.
type EmailFileResolver func(path string) (string, error)

// ─── dslEmail (Новый ПисьмоEmail) ────────────────────────────────────────────

type dslEmail struct {
	sender   EmailSender
	guard    NetGuard
	resolver EmailFileResolver
	to       string
	cc       string
	subject  string
	text     string
	html     string
	files    []EmailAttachment
}

func (e *dslEmail) Get(field string) any {
	switch field {
	case "кому", "to":
		return e.to
	case "копия", "cc":
		return e.cc
	case "тема", "subject":
		return e.subject
	case "текст", "text", "body":
		return e.text
	case "htmlтело", "htmlbody":
		return e.html
	}
	return nil
}

func (e *dslEmail) Set(field string, val any) {
	s := fmt.Sprintf("%v", val)
	switch field {
	case "кому", "to":
		e.to = s
	case "копия", "cc":
		e.cc = s
	case "тема", "subject":
		e.subject = s
	case "текст", "text", "body":
		e.text = s
	case "htmlтело", "htmlbody":
		e.html = s
	}
}

func (e *dslEmail) CallMethod(name string, args []any) any {
	switch name {
	case "присоединитьфайл", "attachfile":
		// ПисьмоEmail.ПрисоединитьФайл(Путь[, ИмяВПисьме]) — файл читается с
		// диска в момент вызова (уважая файловую песочницу DSL).
		if len(args) < 1 {
			panic(userError{Msg: "ПисьмоEmail.ПрисоединитьФайл: не указан путь к файлу"})
		}
		pathArg := strings.TrimSpace(fmt.Sprint(args[0]))
		path := ""
		if e.resolver != nil {
			var err error
			path, err = e.resolver(pathArg)
			if err != nil {
				panic(userError{Msg: "ПисьмоEmail.ПрисоединитьФайл: " + err.Error()})
			}
		} else {
			path = safePathOrRaise("ПисьмоEmail.ПрисоединитьФайл", pathArg)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			panic(userError{Msg: "ПисьмоEmail.ПрисоединитьФайл: " + err.Error()})
		}
		fname := filepath.Base(path)
		if len(args) > 1 {
			if n := strings.TrimSpace(fmt.Sprint(args[1])); n != "" {
				fname = n
			}
		}
		mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(fname)))
		if mt == "" {
			mt = "application/octet-stream"
		}
		e.files = append(e.files, EmailAttachment{Name: fname, MimeType: mt, Data: data})
		return nil
	case "отправить", "send":
		checkNet(e.guard)
		if e.to == "" {
			panic(userError{Msg: "ПисьмоEmail.Отправить: поле Кому не задано"})
		}
		if e.subject == "" {
			panic(userError{Msg: "ПисьмоEmail.Отправить: поле Тема не задана"})
		}
		if len(e.files) > 0 {
			as, ok := e.sender.(EmailAttachmentSender)
			if !ok {
				panic(userError{Msg: "ПисьмоEmail.Отправить: отправитель не поддерживает вложения"})
			}
			if err := as.SendWithAttachments(e.to, e.subject, e.text, e.html, e.files); err != nil {
				panic(userError{Msg: "ОтправитьПисьмо: " + err.Error()})
			}
			return nil
		}
		if err := e.sender.Send(e.to, e.subject, e.text, e.html); err != nil {
			panic(userError{Msg: "ОтправитьПисьмо: " + err.Error()})
		}
		return nil
	}
	panic(userError{Msg: "ПисьмоEmail: неизвестный метод " + name})
}

// ─── NewEmailFunctions ────────────────────────────────────────────────────────

// NewEmailFunctions returns DSL functions/factories to inject into extraVars.
// If sender is nil or not configured, functions panic with a user-friendly message.
func NewEmailFunctions(sender EmailSender, guard NetGuard, resolvers ...EmailFileResolver) map[string]any {
	var resolver EmailFileResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	send := func(to, subject, textBody string) {
		checkNet(guard)
		if sender == nil || !sender.Configured() {
			panic(userError{Msg: "email не настроен — добавьте секцию email: в config/app.yaml"})
		}
		if err := sender.Send(to, subject, textBody, ""); err != nil {
			panic(userError{Msg: "ОтправитьПисьмо: " + err.Error()})
		}
	}

	shorthand := BuiltinFunc(func(args []any, file string, line int) (any, error) {
		to := strArg(args, 0)
		subject := strArg(args, 1)
		text := strArg(args, 2)
		send(to, subject, text)
		return nil, nil
	})

	emailFactory := func(args []any) any {
		checkNet(guard)
		if sender == nil || !sender.Configured() {
			panic(userError{Msg: "email не настроен — добавьте секцию email: в config/app.yaml"})
		}
		return &dslEmail{sender: sender, guard: guard, resolver: resolver}
	}

	return map[string]any{
		"ОтправитьПисьмо":        shorthand,
		"SendEmail":              shorthand,
		"__factory_ПисьмоEmail":  emailFactory,
		"__factory_EmailMessage": emailFactory,
	}
}
