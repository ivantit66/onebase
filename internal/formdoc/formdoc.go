// Package formdoc редактирует управляемую форму как дерево yaml.Node.
//
// Канонический документ — yaml.Node, а не модель: round-trip (Load → Bytes)
// сохраняет комментарии и порядок ключей, поэтому визуальный конструктор форм
// (issue #164) и текстовый YAML-редактор не затирают ручные правки. Структурные
// операции (вставка элемента, правка свойства) меняют узел хирургически, а
// metadata.FormModule выводится из дерева только для отрисовки холста.
package formdoc

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// Doc — управляемая форма как дерево yaml.Node (документ-источник истины).
type Doc struct {
	root yaml.Node // DocumentNode, полученный из Unmarshal
}

// Load разбирает YAML в дерево узлов, сохраняя комментарии, порядок и
// форматирование (насколько их несёт yaml.Node).
func Load(data []byte) (*Doc, error) {
	d := &Doc{}
	if err := yaml.Unmarshal(data, &d.root); err != nil {
		return nil, err
	}
	return d, nil
}

// Bytes сериализует дерево обратно в YAML. Комментарии и порядок ключей
// сохраняются; отступ нормализуется в 2 пробела (стиль yaml.v3 и наших форм).
func (d *Doc) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&d.root); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
