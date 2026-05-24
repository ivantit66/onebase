package onec_forms

import "errors"

// ErrNotImplemented возвращается фасадными функциями до реализации
// соответствующих этапов плана 37 (этап 2 — импорт, этап 5 — экспорт).
var ErrNotImplemented = errors.New("onec_forms: not implemented yet (см. План 37)")

// ImportFromOneC читает форму из выгрузки 1С (Form.xml + Module.bsl + Items/*)
// и записывает её в проект OneBase как .form.yaml + .form.os + _resources/.
//
// Аргументы:
//
//	xmlPath        — путь к Form.xml.
//	bslPath        — путь к Module.bsl (может быть пустым: form без модуля).
//	itemsDir       — путь к папке Items/ (может отсутствовать).
//	dstYAMLPath    — куда записать .form.yaml.
//	dstOSPath      — куда записать .form.os.
//	dstResourcesDir — куда копировать бинарные ресурсы.
//
// Реализуется в этапе 2 плана 37.
func ImportFromOneC(xmlPath, bslPath, itemsDir, dstYAMLPath, dstOSPath, dstResourcesDir string) (*ImportReport, error) {
	return nil, ErrNotImplemented
}

// ExportToOneC обратное направление: читает .form.yaml + .form.os
// из проекта OneBase и записывает Form.xml + Module.bsl + Items/*
// в указанный каталог.
//
// Реализуется в этапе 5 плана 37.
func ExportToOneC(yamlPath, osPath, dstFormDir string) (*ExportReport, error) {
	return nil, ErrNotImplemented
}

// Validate проверяет корректность .form.yaml: схема, типы реквизитов,
// существование data_path, наличие процедур-обработчиков в .form.os.
// Возвращает список предупреждений (даже при отсутствии ошибок).
//
// Реализуется в этапе 6 плана 37.
func Validate(yamlPath string) ([]Warning, error) {
	return nil, ErrNotImplemented
}
