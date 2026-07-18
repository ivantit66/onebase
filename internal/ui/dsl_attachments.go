package ui

// DSL-builtins работы с вложениями (план 105): присоединение файла с диска к
// записи справочника/документа, список вложений, путь к файлу вложения,
// удаление. Нужны прикладным сценариям вида «сканер папки релизов регистрирует
// файл вложением», «отправить вложение письмом/в мессенджер через файл».
//
//	ИдВложения = ПрисоединитьФайл(Ссылка, "/path/file.zip");        // или + имя
//	Вложения   = СписокВложений(Ссылка);   // Массив Структур {ИД, ИмяФайла, Размер, ТипMIME, Загружен}
//	Путь       = ПутьКВложению(ИдВложения);
//	УдалитьВложение(ИдВложения);

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// resolveAttachmentOwner превращает первый аргумент builtin'а (ссылку) в
// сущность-владельца и UUID записи.
func (s *Server) resolveAttachmentOwner(arg any) (*metadata.Entity, uuid.UUID, error) {
	ref, ok := arg.(*interpreter.Ref)
	if !ok || ref == nil {
		return nil, uuid.Nil, fmt.Errorf("ожидается ссылка на элемент справочника или документ, получено %T", arg)
	}
	entity := s.reg.GetEntity(ref.Type)
	if entity == nil {
		return nil, uuid.Nil, fmt.Errorf("неизвестный тип ссылки %q", ref.Type)
	}
	id, err := uuid.Parse(ref.UUID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("неверный идентификатор ссылки: %q", ref.UUID)
	}
	return entity, id, nil
}

func (s *Server) attachmentMaxBytes() int64 {
	if s.maxFileSizeBytes > 0 {
		return s.maxFileSizeBytes
	}
	return 50 * 1024 * 1024
}

// registerAttachmentBuiltins добавляет функции вложений в DSL-окружение.
// ctxFn отдаёт живой контекст (по образцу txState в buildDSLVars).
func (s *Server) registerAttachmentBuiltins(vars map[string]any, ctxFn func() context.Context) {
	attachFn := interpreter.BuiltinFunc(func(args []any, _ string, _ int) (any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("ПрисоединитьФайл(Ссылка, ПутьКФайлу[, ИмяФайла]): недостаточно аргументов")
		}
		entity, ownerID, err := s.resolveAttachmentOwner(args[0])
		if err != nil {
			return nil, fmt.Errorf("ПрисоединитьФайл: %w", err)
		}
		ctx := ctxFn()
		if err := s.checkDSLRowAccess(ctx, entity, "write", ownerID, nil); err != nil {
			return nil, fmt.Errorf("ПрисоединитьФайл: %w", err)
		}
		path, err := interpreter.ResolveSafePath(strings.TrimSpace(fmt.Sprint(args[1])))
		if err != nil {
			return nil, fmt.Errorf("ПрисоединитьФайл: %w", err)
		}
		name := filepath.Base(path)
		if len(args) > 2 {
			if n := strings.TrimSpace(fmt.Sprint(args[2])); n != "" {
				name = n
			}
		}
		name = storage.SanitizeAttachmentName(name)
		if !storage.AttachmentExtAllowed(s.allowedAttachmentTypes, name) {
			return nil, fmt.Errorf("ПрисоединитьФайл: недопустимый тип файла %q", name)
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("ПрисоединитьФайл: открытие файла: %w", err)
		}
		defer f.Close()
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		uploadedBy := ""
		if u := auth.UserFromContext(ctx); u != nil {
			uploadedBy = u.Login
		}
		att, err := s.store.UploadAttachment(ctx, string(entity.Kind), entity.Name, ownerID,
			name, mimeType, uploadedBy, f, s.attachmentMaxBytes())
		if err != nil {
			return nil, fmt.Errorf("ПрисоединитьФайл: %w", err)
		}
		return att.ID.String(), nil
	})

	listFn := interpreter.BuiltinFunc(func(args []any, _ string, _ int) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("СписокВложений(Ссылка): не передана ссылка")
		}
		entity, ownerID, err := s.resolveAttachmentOwner(args[0])
		if err != nil {
			return nil, fmt.Errorf("СписокВложений: %w", err)
		}
		ctx := ctxFn()
		if err := s.checkDSLRowAccess(ctx, entity, "read", ownerID, nil); err != nil {
			return nil, fmt.Errorf("СписокВложений: %w", err)
		}
		atts, err := s.store.ListAttachments(ctx, string(entity.Kind), entity.Name, ownerID)
		if err != nil {
			return nil, fmt.Errorf("СписокВложений: %w", err)
		}
		items := make([]any, 0, len(atts))
		for _, a := range atts {
			items = append(items, interpreter.NewStructFromMap(map[string]any{
				"ИД":       a.ID.String(),
				"ИмяФайла": a.Filename,
				"Размер":   float64(a.SizeBytes),
				"ТипMIME":  a.MimeType,
				"Загружен": a.UploadedAt,
			}))
		}
		return interpreter.NewArray(items), nil
	})

	pathFn := interpreter.BuiltinFunc(func(args []any, _ string, _ int) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("ПутьКВложению(ИдВложения): не передан идентификатор")
		}
		id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(args[0])))
		if err != nil {
			return nil, fmt.Errorf("ПутьКВложению: неверный идентификатор %q", fmt.Sprint(args[0]))
		}
		ctx := ctxFn()
		f, att, err := s.store.OpenAttachment(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("ПутьКВложению: %w", err)
		}
		path := f.Name()
		f.Close()
		entity := s.reg.GetEntity(att.OwnerName)
		if entity != nil {
			if err := s.checkDSLRowAccess(ctx, entity, "read", att.OwnerID, nil); err != nil {
				return nil, fmt.Errorf("ПутьКВложению: %w", err)
			}
		}
		return path, nil
	})

	delFn := interpreter.BuiltinFunc(func(args []any, _ string, _ int) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("УдалитьВложение(ИдВложения): не передан идентификатор")
		}
		id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(args[0])))
		if err != nil {
			return nil, fmt.Errorf("УдалитьВложение: неверный идентификатор %q", fmt.Sprint(args[0]))
		}
		ctx := ctxFn()
		att, err := s.store.GetAttachment(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("УдалитьВложение: %w", err)
		}
		entity := s.reg.GetEntity(att.OwnerName)
		if entity != nil {
			if err := s.checkDSLRowAccess(ctx, entity, "write", att.OwnerID, nil); err != nil {
				return nil, fmt.Errorf("УдалитьВложение: %w", err)
			}
		}
		if err := s.store.DeleteAttachment(ctx, id); err != nil {
			return nil, fmt.Errorf("УдалитьВложение: %w", err)
		}
		return nil, nil
	})

	vars["ПрисоединитьФайл"] = attachFn
	vars["AttachFile"] = attachFn
	vars["СписокВложений"] = listFn
	vars["ListAttachments"] = listFn
	vars["ПутьКВложению"] = pathFn
	vars["AttachmentPath"] = pathFn
	vars["УдалитьВложение"] = delFn
	vars["DeleteAttachment"] = delFn
}
