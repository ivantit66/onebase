// Package pdfimport извлекает черновик макета печатной формы из страницы PDF
// (план 64, этап 6). Вектор с текстовым слоем (выгрузка 1С/Excel) даёт пригодный
// для доводки в редакторе скелет: сетка колонок/строк, ячейки со спанами,
// границы по сторонам, тексты с кеглем/жирностью. Сканы без текстового слоя
// дают честную ошибку.
//
// Безопасность недоверенного PDF: парсер dslipak/pdf (форк rsc.io/pdf) паникует
// на битых файлах — это его API, поэтому вся работа обёрнута в recover; есть
// контекст-таймаут и лимиты на размер файла и номер страницы.
//
// Линии сетки извлекаются проходом по content stream (операторы m/l/S), т.к.
// высокоуровневый Content().Rect отдаёт только клип-прямоугольники, а реальные
// границы 1С рисует штрихами (подтверждено спайком: 282 m/l на УПД).
package pdfimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	pdf "github.com/ivantit66/onebase/internal/pdfimport/pdfparse"

	"github.com/ivantit66/onebase/internal/printform"
)

// MaxFileSize — лимит размера PDF (10 МБ). Защита от распухших/враждебных файлов.
const MaxFileSize = 10 << 20

// importTimeout — таймаут на разбор одной страницы.
const importTimeout = 10 * time.Second

var (
	// ErrNoTextLayer — на странице не найдено ни одного текстового run (скан/картинка).
	ErrNoTextLayer = errors.New("текстовый слой не найден: похоже, это скан или изображение без текста — импорт макета невозможен")
	// ErrFileTooLarge — файл превышает MaxFileSize.
	ErrFileTooLarge = fmt.Errorf("файл больше %d МБ — слишком большой для импорта", MaxFileSize>>20)
	// ErrPageNotFound — запрошенной страницы нет в документе.
	ErrPageNotFound = errors.New("страница не найдена в документе")
	// ErrParse — общий сбой разбора PDF (битый файл, паника парсера).
	ErrParse = errors.New("не удалось разобрать PDF (возможно, файл повреждён или зашифрован)")

	errPageNotFound = ErrPageNotFound
)

// ImportPage извлекает черновик макета из страницы page (1-based) PDF, доступного
// как ReaderAt длиной size. Возвращает LayoutTemplate с одной областью «Страница1»
// или ошибку (ErrNoTextLayer / ErrPageNotFound / ErrParse / ErrFileTooLarge).
//
// Параметр page нумеруется с 1 (как в 1С и dslipak/pdf).
func ImportPage(r io.ReaderAt, size int64, page int) (*printform.LayoutTemplate, error) {
	if size > MaxFileSize {
		return nil, ErrFileTooLarge
	}
	if size <= 0 {
		return nil, ErrParse
	}
	if page < 1 {
		page = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), importTimeout)
	defer cancel()

	type result struct {
		tpl *printform.LayoutTemplate
		err error
	}
	ch := make(chan result, 1)

	go func() {
		tpl, err := safeImport(r, size, page)
		ch <- result{tpl, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("разбор PDF превысил таймаут %s", importTimeout)
	case res := <-ch:
		return res.tpl, res.err
	}
}

// ImportBytes — удобная обёртка над ImportPage для []byte.
func ImportBytes(data []byte, page int) (*printform.LayoutTemplate, error) {
	return ImportPage(bytes.NewReader(data), int64(len(data)), page)
}

// safeImport выполняет фактический разбор под recover: dslipak/pdf паникует на
// битом вводе (это его контракт), панику превращаем в ErrParse.
func safeImport(r io.ReaderAt, size int64, page int) (tpl *printform.LayoutTemplate, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			tpl = nil
			err = fmt.Errorf("%w: %v", ErrParse, rec)
		}
	}()

	reader, perr := pdf.NewReader(r, size)
	if perr != nil {
		return nil, fmt.Errorf("%w: %v", ErrParse, perr)
	}

	if n := reader.NumPage(); page > n {
		return nil, ErrPageNotFound
	}

	ep, eerr := extractPage(reader, page)
	if eerr != nil {
		return nil, eerr
	}
	if len(ep.Runs) == 0 {
		return nil, ErrNoTextLayer
	}

	return buildLayout(ep), nil
}
