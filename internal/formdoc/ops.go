package formdoc

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Хирургические правки дерева yaml.Node. Каждая операция меняет минимум узлов,
// чтобы комментарии и порядок ключей соседних узлов оставались нетронутыми
// (двусторонняя синхронизация конструктора форм #164 не должна затирать ручные
// правки пользователя).

// scalarKey строит фресh-узел ключа mapping-а.
func scalarKey(key string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
}

// SetProp задаёт скалярное свойство элемента по node-id. Ключ может быть
// вложенным через точку (например "title.ru") — промежуточные mapping-узлы
// создаются. Существующий ключ обновляется in-place (с сохранением его
// комментариев); отсутствующий — дописывается в конец mapping-а.
func (d *Doc) SetProp(nodeID, key string, value any) error {
	m, err := d.NodeByID(nodeID)
	if err != nil {
		return err
	}
	if m.Kind != yaml.MappingNode {
		return fmt.Errorf("formdoc: node-id %q не mapping-узел", nodeID)
	}
	keys := strings.Split(key, ".")
	for _, k := range keys[:len(keys)-1] {
		v := mappingValue(m, k)
		if v == nil {
			v = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			m.Content = append(m.Content, scalarKey(k), v)
		}
		if v.Kind != yaml.MappingNode {
			return fmt.Errorf("formdoc: %q: ключ %q не mapping", key, k)
		}
		m = v
	}
	return setMappingScalar(m, keys[len(keys)-1], value)
}

// setMappingScalar заменяет или дописывает скалярное значение по ключу.
func setMappingScalar(m *yaml.Node, key string, value any) error {
	vn := &yaml.Node{}
	if err := vn.Encode(value); err != nil {
		return fmt.Errorf("formdoc: кодирование значения ключа %q: %w", key, err)
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			// Сохраняем комментарии, привязанные к прежнему значению.
			vn.HeadComment, vn.LineComment, vn.FootComment = old.HeadComment, old.LineComment, old.FootComment
			*old = *vn
			return nil
		}
	}
	m.Content = append(m.Content, scalarKey(key), vn)
	return nil
}

// insertKeyOrder — канонический порядок ключей нового элемента, чтобы YAML
// читался единообразно. Неизвестные ключи дописываются после, по алфавиту.
var insertKeyOrder = []string{"kind", "name", "title", "data_path", "field", "required", "readonly", "hint", "children"}

// buildElementNode строит mapping-узел нового элемента из набора полей.
func buildElementNode(fields map[string]any) (*yaml.Node, error) {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	emitted := make(map[string]bool, len(fields))
	emit := func(key string) error {
		v, ok := fields[key]
		if !ok || emitted[key] {
			return nil
		}
		vn := &yaml.Node{}
		if err := vn.Encode(v); err != nil {
			return fmt.Errorf("formdoc: поле %q: %w", key, err)
		}
		m.Content = append(m.Content, scalarKey(key), vn)
		emitted[key] = true
		return nil
	}
	for _, k := range insertKeyOrder {
		if err := emit(k); err != nil {
			return nil, err
		}
	}
	// Прочие ключи — детерминированно по алфавиту (forward-compat).
	rest := make([]string, 0, len(fields))
	for k := range fields {
		if !emitted[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if err := emit(k); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ensureSeq возвращает sequence-узел по ключу mapping-а, создавая пустой при
// отсутствии. Ошибка, если ключ занят узлом иного вида.
func ensureSeq(m *yaml.Node, key string) (*yaml.Node, error) {
	if v := mappingValue(m, key); v != nil {
		if v.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("formdoc: ключ %q не sequence", key)
		}
		return v, nil
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	m.Content = append(m.Content, scalarKey(key), seq)
	return seq, nil
}

// insertIntoSeq вставляет node в seq по индексу (clamp в [0,len]).
func insertIntoSeq(seq *yaml.Node, index int, node *yaml.Node) {
	if index < 0 {
		index = 0
	}
	if index > len(seq.Content) {
		index = len(seq.Content)
	}
	seq.Content = append(seq.Content, nil)
	copy(seq.Content[index+1:], seq.Content[index:])
	seq.Content[index] = node
}

// targetSeq возвращает sequence-контейнер дочерних элементов для parentID:
// пустой parentID → верхнеуровневый elements; иначе children элемента parentID.
// Также возвращает префикс node-id этого контейнера.
func (d *Doc) targetSeq(parentID string) (seq *yaml.Node, prefix string, err error) {
	if parentID == "" {
		seq, err = ensureSeq(d.topMapping(), elementsKey)
		return seq, elementsKey, err
	}
	pm, err := d.NodeByID(parentID)
	if err != nil {
		return nil, "", err
	}
	if pm.Kind != yaml.MappingNode {
		return nil, "", fmt.Errorf("formdoc: parent %q не mapping-узел", parentID)
	}
	seq, err = ensureSeq(pm, childrenKey)
	return seq, parentID + "." + childrenKey, err
}

// InsertElement вставляет новый элемент в контейнер parentID по индексу и
// возвращает node-id вставленного элемента. Пустой parentID — верхний уровень.
func (d *Doc) InsertElement(parentID string, index int, fields map[string]any) (string, error) {
	node, err := buildElementNode(fields)
	if err != nil {
		return "", err
	}
	seq, prefix, err := d.targetSeq(parentID)
	if err != nil {
		return "", err
	}
	if index < 0 {
		index = 0
	}
	if index > len(seq.Content) {
		index = len(seq.Content)
	}
	insertIntoSeq(seq, index, node)
	return prefix + "." + strconv.Itoa(index), nil
}

// locate находит sequence-родителя элемента, его индекс в нём и сам узел.
func (d *Doc) locate(nodeID string) (seq *yaml.Node, idx int, node *yaml.Node, err error) {
	dot := strings.LastIndex(nodeID, ".")
	if dot < 0 {
		return nil, 0, nil, fmt.Errorf("formdoc: node-id %q без родителя", nodeID)
	}
	idx, err = strconv.Atoi(nodeID[dot+1:])
	if err != nil {
		return nil, 0, nil, fmt.Errorf("formdoc: node-id %q: последний сегмент не индекс", nodeID)
	}
	seq, err = d.NodeByID(nodeID[:dot])
	if err != nil {
		return nil, 0, nil, err
	}
	if seq.Kind != yaml.SequenceNode || idx < 0 || idx >= len(seq.Content) {
		return nil, 0, nil, fmt.Errorf("formdoc: node-id %q: индекс вне диапазона", nodeID)
	}
	return seq, idx, seq.Content[idx], nil
}

// Move переносит элемент nodeID в контейнер newParentID на позицию index.
// Перестановка хирургическая: узел переиспользуется целиком, поэтому его
// комментарии и поддерево сохраняются.
func (d *Doc) Move(nodeID, newParentID string, index int) error {
	srcSeq, srcIdx, node, err := d.locate(nodeID)
	if err != nil {
		return err
	}
	if newParentID != "" && (newParentID == nodeID || strings.HasPrefix(newParentID, nodeID+"."+childrenKey+".")) {
		return fmt.Errorf("formdoc: нельзя перенести узел %q внутрь собственного поддерева", nodeID)
	}
	dstSeq, _, err := d.targetSeq(newParentID)
	if err != nil {
		return err
	}
	// Удаляем из источника.
	srcSeq.Content = append(srcSeq.Content[:srcIdx], srcSeq.Content[srcIdx+1:]...)
	// При переносе внутри одного контейнера вперёд — компенсируем сдвиг.
	if srcSeq == dstSeq && srcIdx < index {
		index--
	}
	insertIntoSeq(dstSeq, index, node)
	return nil
}
