package exchange

// Сборка и разбор пакетов обмена (.obx, план 86). Пакет — версионированный JSON:
// набор изменённых объектов (шапка + табличные части) с их версией-источником.
// Загрузка идемпотентна по версии: повторная доставка того же пакета не создаёт
// дублей и не наращивает ревизии.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// FormatV1 — значение поля Format в пакете первой версии.
const FormatV1 = "onebase-exchange/1"

const (
	// MaxPackageBytes is shared by file and HTTP transports.
	MaxPackageBytes   = 64 << 20
	MaxPackageObjects = 10_000
)

// EntityResolver отдаёт метаданные сущности по имени. Реализуется
// runtime.Registry (метод GetEntity), но объявлен здесь, чтобы пакет обмена не
// зависел от runtime и был тестируем с фейковым резолвером.
type EntityResolver interface {
	GetEntity(name string) *metadata.Entity
}

// Package — конверт обмена.
type Package struct {
	Format    string `json:"format"`
	Plan      string `json:"plan"`
	FromNode  string `json:"from_node"`
	ToNode    string `json:"to_node"`
	MessageNo int64  `json:"message_no"`
	// AckNo — «я получил от тебя сообщения вплоть до этого номера». Приёмник по
	// нему очищает свою очередь к отправителю (подтверждение приёма пакетов,
	// доставленных ранее в обратную сторону). 0 = нечего подтверждать.
	AckNo   int64           `json:"ack_no,omitempty"`
	Objects []PackageObject `json:"objects"`
}

// PackageObject — один объект в пакете. Fields/TableParts несут канонизированные
// значения (числа — точной строкой, даты — RFC3339, ссылки — UUID-строкой),
// пригодные для обратной записи через тот же путь, что и обычное сохранение.
type PackageObject struct {
	Type       string                      `json:"type"`
	ID         string                      `json:"id"`
	Version    int64                       `json:"version"`
	Deletion   bool                        `json:"deletion,omitempty"`
	Tombstone  bool                        `json:"tombstone,omitempty"`
	ChangedAt  int64                       `json:"changed_at"`
	Fields     map[string]any              `json:"fields,omitempty"`
	TableParts map[string][]map[string]any `json:"table_parts,omitempty"`
}

// LoadResult — итог загрузки пакета.
type LoadResult struct {
	Applied   int `json:"applied"`   // применено (создано/обновлено)
	Skipped   int `json:"skipped"`   // пропущено (идемпотентно: версия не новее, либо сущность неизвестна)
	Deleted   int `json:"deleted"`   // применено с пометкой на удаление
	Conflicts int `json:"conflicts"` // обнаружено встречных правок (разрешено правилом)
}

// BuildPackage собирает пакет незапподтверждённых изменений для узла toNode,
// присваивает ему следующий номер сообщения и помечает вошедшие строки очереди
// как выгруженные. Всё — в одной транзакции.
func BuildPackage(ctx context.Context, store *storage.DB, resolver EntityResolver, plan *metadata.ExchangePlan, toNode string) ([]byte, error) {
	if err := validatePairPlan(plan); err != nil {
		return nil, err
	}
	toNodeDef := plan.Node(toNode)
	if toNodeDef == nil {
		return nil, fmt.Errorf("exchange: узел-получатель %q не описан в плане %q", toNode, plan.Name)
	}
	toNode = toNodeDef.Code
	var out []byte
	err := store.WithTx(ctx, func(ctx context.Context) error {
		thisNode, err := store.GetExchangeThisNode(ctx, plan.Name)
		if err != nil {
			return err
		}
		thisNodeDef := plan.Node(thisNode)
		if thisNode == "" || thisNodeDef == nil {
			return fmt.Errorf("exchange: текущий узел базы не задан или не описан в плане %q", plan.Name)
		}
		thisNode = thisNodeDef.Code
		if strings.EqualFold(thisNode, toNode) {
			return fmt.Errorf("exchange: нельзя выгрузить пакет текущему узлу %q", toNode)
		}
		changes, err := store.PendingExchangeChanges(ctx, plan.Name, toNode)
		if err != nil {
			return err
		}
		if len(changes) > MaxPackageObjects {
			changes = changes[:MaxPackageObjects]
		}
		msgNo, err := store.NextExchangeMessageNo(ctx, plan.Name, toNode)
		if err != nil {
			return err
		}
		// Пиггибэк подтверждения: сообщаем узлу, сколько его сообщений мы приняли,
		// чтобы он очистил свою очередь к нам (recv_no по этому узлу).
		peer, err := store.GetExchangePeer(ctx, plan.Name, toNode)
		if err != nil {
			return err
		}
		pkg := Package{Format: FormatV1, Plan: plan.Name, FromNode: thisNode, ToNode: toNode, MessageNo: msgNo, AckNo: peer.RecvNo}
		var included []storage.ExchangeChange
		for _, ch := range changes {
			ent := resolver.GetEntity(ch.ObjectType)
			if ent == nil {
				return fmt.Errorf("exchange: сущность очереди %q отсутствует в конфигурации; пакет не сформирован", ch.ObjectType)
			}
			id, err := uuid.Parse(ch.ObjectID)
			if err != nil {
				return fmt.Errorf("exchange: неверный id %q в очереди: %w", ch.ObjectID, err)
			}
			version, exists, err := store.EntityVersionExists(ctx, ent.Name, id)
			if err != nil {
				return err
			}
			if !exists && !ch.Deletion {
				return fmt.Errorf("exchange: объект очереди %s/%s физически удалён без tombstone", ent.Name, id)
			}
			// Между чтением очереди и объекта могла завершиться новая запись.
			// Не смешиваем её поля со старой версией пакета: новая строка очереди
			// останется sent_no=0 и попадёт в следующий пакет.
			if exists && version != ch.Version {
				continue
			}
			var row map[string]any
			if exists {
				row, err = store.GetByID(ctx, ent.Name, id, ent)
				if err != nil {
					return fmt.Errorf("exchange: объект очереди %s/%s недоступен: %w", ent.Name, id, err)
				}
			}
			obj := PackageObject{
				Type:      ch.ObjectType,
				ID:        ch.ObjectID,
				Version:   ch.Version,
				Deletion:  ch.Deletion,
				ChangedAt: ch.ChangedAt,
				Fields:    canonicalHeader(ent, row),
			}
			if !exists {
				obj.Tombstone = true
				obj.Fields = emptyCanonicalHeader(ent)
			}
			if len(ent.TableParts) > 0 {
				obj.TableParts = make(map[string][]map[string]any, len(ent.TableParts))
			}
			for _, tp := range ent.TableParts {
				if obj.Tombstone {
					obj.TableParts[tp.Name] = []map[string]any{}
					continue
				}
				rows, err := store.GetTablePartRows(ctx, ent.Name, tp.Name, id, tp)
				if err != nil {
					return err
				}
				canon := make([]map[string]any, 0, len(rows))
				for _, r := range rows {
					canon = append(canon, canonicalRow(tp.Fields, r))
				}
				obj.TableParts[tp.Name] = canon
			}
			pkg.Objects = append(pkg.Objects, obj)
			included = append(included, ch)
		}
		if err := store.MarkExchangeChangesSent(ctx, included, msgNo); err != nil {
			return err
		}
		out, err = json.MarshalIndent(pkg, "", "  ")
		if err != nil {
			return err
		}
		if len(out) > MaxPackageBytes {
			return fmt.Errorf("exchange: сформированный пакет превышает лимит %d байт; уменьшите размер объекта", MaxPackageBytes)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ParsePackage разбирает и валидирует конверт пакета.
func ParsePackage(data []byte) (*Package, error) {
	if len(data) > MaxPackageBytes {
		return nil, fmt.Errorf("exchange: пакет превышает лимит %d байт", MaxPackageBytes)
	}
	var pkg Package
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pkg); err != nil {
		return nil, fmt.Errorf("exchange: разбор пакета: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("exchange: после конверта обнаружены лишние JSON-данные")
		}
		return nil, fmt.Errorf("exchange: разбор хвоста пакета: %w", err)
	}
	if pkg.Format != FormatV1 {
		return nil, fmt.Errorf("exchange: неизвестный формат пакета %q (ожидался %q)", pkg.Format, FormatV1)
	}
	if strings.TrimSpace(pkg.Plan) == "" || strings.TrimSpace(pkg.FromNode) == "" || strings.TrimSpace(pkg.ToNode) == "" {
		return nil, fmt.Errorf("exchange: plan, from_node и to_node обязательны")
	}
	if pkg.MessageNo <= 0 || pkg.AckNo < 0 {
		return nil, fmt.Errorf("exchange: неверные счётчики message_no=%d ack_no=%d", pkg.MessageNo, pkg.AckNo)
	}
	if len(pkg.Objects) > MaxPackageObjects {
		return nil, fmt.Errorf("exchange: объектов в пакете %d, лимит %d", len(pkg.Objects), MaxPackageObjects)
	}
	seen := make(map[string]struct{}, len(pkg.Objects))
	for i := range pkg.Objects {
		obj := &pkg.Objects[i]
		obj.Type = strings.TrimSpace(obj.Type)
		if obj.Type == "" || obj.Version <= 0 || obj.ChangedAt <= 0 {
			return nil, fmt.Errorf("exchange: объект %d имеет неверные type/version/changed_at", i)
		}
		if obj.Tombstone && !obj.Deletion {
			return nil, fmt.Errorf("exchange: tombstone объекта %d должен иметь deletion=true", i)
		}
		id, err := uuid.Parse(obj.ID)
		if err != nil {
			return nil, fmt.Errorf("exchange: объект %d: неверный id: %w", i, err)
		}
		obj.ID = id.String()
		key := strings.ToLower(obj.Type) + "/" + obj.ID
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("exchange: объект %s продублирован в пакете", key)
		}
		seen[key] = struct{}{}
	}
	return &pkg, nil
}

func validateObjectShape(ent *metadata.Entity, obj PackageObject) error {
	wantFields := make(map[string]struct{}, len(ent.Fields)+2)
	for _, f := range ent.Fields {
		wantFields[strings.ToLower(f.Name)] = struct{}{}
	}
	if ent.Hierarchical {
		wantFields["parent_id"] = struct{}{}
		wantFields["is_folder"] = struct{}{}
	}
	if len(obj.Fields) != len(wantFields) {
		return fmt.Errorf("exchange: объект %s/%s содержит неполную или несовместимую шапку", ent.Name, obj.ID)
	}
	for name := range obj.Fields {
		if _, ok := wantFields[strings.ToLower(name)]; !ok {
			return fmt.Errorf("exchange: объект %s/%s содержит неизвестное поле %q", ent.Name, obj.ID, name)
		}
	}
	if len(obj.TableParts) != len(ent.TableParts) {
		return fmt.Errorf("exchange: объект %s/%s содержит неполный набор табличных частей", ent.Name, obj.ID)
	}
	parts := make(map[string]metadata.TablePart, len(ent.TableParts))
	for _, tp := range ent.TableParts {
		parts[strings.ToLower(tp.Name)] = tp
	}
	for name, rows := range obj.TableParts {
		tp, ok := parts[strings.ToLower(name)]
		if !ok {
			return fmt.Errorf("exchange: объект %s/%s содержит неизвестную табличную часть %q", ent.Name, obj.ID, name)
		}
		want := make(map[string]struct{}, len(tp.Fields))
		for _, f := range tp.Fields {
			want[strings.ToLower(f.Name)] = struct{}{}
		}
		for rowNo, row := range rows {
			if len(row) != len(want) {
				return fmt.Errorf("exchange: %s/%s.%s[%d] имеет несовместимый набор полей", ent.Name, obj.ID, tp.Name, rowNo)
			}
			for field := range row {
				if _, ok := want[strings.ToLower(field)]; !ok {
					return fmt.Errorf("exchange: %s/%s.%s[%d] содержит неизвестное поле %q", ent.Name, obj.ID, tp.Name, rowNo, field)
				}
			}
		}
	}
	return nil
}

func validatePackageObjects(pkg *Package, resolver EntityResolver, plan *metadata.ExchangePlan) error {
	for _, obj := range pkg.Objects {
		ent := resolver.GetEntity(obj.Type)
		if ent == nil {
			return fmt.Errorf("exchange: сущность %q неизвестна приёмнику; пакет отклонён без подтверждения", obj.Type)
		}
		if !plan.Includes(ent) {
			return fmt.Errorf("exchange: сущность %q не входит в состав плана %q", obj.Type, plan.Name)
		}
		if err := validateObjectShape(ent, obj); err != nil {
			return err
		}
	}
	return nil
}

// ApplyPackage идемпотентно загружает пакет в план plan. Для каждого объекта:
//   - если приёмник тоже менял его со времени последней синхронизации с
//     источником (есть неотправленная строка очереди этому узлу) — это конфликт,
//     разрешаемый правилом плана (resolveConflict);
//   - иначе применяется, если объекта нет или входящая версия строго новее
//     локальной (_version); иначе пропускается (идемпотентно).
//
// Запись идёт напрямую через storage (минуя entityservice.Save) — без хуков,
// движений и обратной регистрации в обмене (нет эха).
func ApplyPackage(ctx context.Context, store *storage.DB, resolver EntityResolver, plan *metadata.ExchangePlan, data []byte, opts ApplyOptions) (LoadResult, error) {
	pkg, err := ParsePackage(data)
	if err != nil {
		return LoadResult{}, err
	}
	if plan == nil {
		return LoadResult{}, fmt.Errorf("exchange: план %q не найден на приёмнике", pkg.Plan)
	}
	if err := validatePairPlan(plan); err != nil {
		return LoadResult{}, err
	}
	if !strings.EqualFold(pkg.Plan, plan.Name) {
		return LoadResult{}, fmt.Errorf("exchange: пакет плана %q загружается в план %q", pkg.Plan, plan.Name)
	}
	if err := validatePackageObjects(pkg, resolver, plan); err != nil {
		return LoadResult{}, err
	}
	var res LoadResult
	err = store.WithTx(ctx, func(ctx context.Context) error {
		thisNode, err := store.GetExchangeThisNode(ctx, plan.Name)
		if err != nil {
			return err
		}
		thisNodeDef := plan.Node(thisNode)
		if thisNode == "" || thisNodeDef == nil {
			return fmt.Errorf("exchange: текущий узел базы не задан или не описан в плане %q", plan.Name)
		}
		thisNode = thisNodeDef.Code
		fromNodeDef := plan.Node(pkg.FromNode)
		if pkg.FromNode == "" || fromNodeDef == nil {
			return fmt.Errorf("exchange: узел-источник %q не описан в плане %q", pkg.FromNode, plan.Name)
		}
		fromNode := fromNodeDef.Code
		if pkg.ToNode == "" || !strings.EqualFold(pkg.ToNode, thisNode) {
			return fmt.Errorf("exchange: пакет адресован узлу %q, текущий узел %q", pkg.ToNode, thisNode)
		}
		if strings.EqualFold(fromNode, thisNode) {
			return fmt.Errorf("exchange: пакет не может быть отправлен текущим узлом %q самому себе", thisNode)
		}
		peer, err := store.GetExchangePeer(ctx, plan.Name, fromNode)
		if err != nil {
			return err
		}
		if pkg.AckNo > peer.SentNo {
			return fmt.Errorf("exchange: ack_no=%d больше последнего отправленного сообщения %d узлу %q", pkg.AckNo, peer.SentNo, fromNode)
		}
		// Сначала применяем подтверждение. Подтверждённая отправителем строка
		// очереди уже не является встречной правкой и не должна участвовать в
		// разрешении конфликта (особенно при by_node_priority).
		if pkg.AckNo > 0 {
			if _, err := store.AckExchangeChanges(ctx, plan.Name, fromNode, pkg.AckNo); err != nil {
				return err
			}
		}
		if pkg.MessageNo <= peer.RecvNo {
			res.Skipped = len(pkg.Objects)
			return nil
		}
		for _, obj := range pkg.Objects {
			ent := resolver.GetEntity(obj.Type)
			id, _ := uuid.Parse(obj.ID) // ParsePackage already canonicalised it.
			// Встречная правка: приёмник менял тот же объект и ещё не отправил
			// изменение источнику → конфликт, разрешаемый правилом плана.
			objectID := id.String()
			local, hasLocal, err := store.GetExchangeChange(ctx, plan.Name, ent.Name, objectID, fromNode)
			if err != nil {
				return err
			}
			if hasLocal {
				res.Conflicts++
				win, err := resolveConflict(ctx, store, plan, thisNode, fromNode, ent, id, obj, local.ChangedAt, opts.Hook)
				if err != nil {
					return err
				}
				if !win {
					res.Skipped++ // локальное изменение победило — не применяем
					continue
				}
				if err := applyObject(ctx, store, ent, id, obj); err != nil {
					return err
				}
				if err := store.DeleteExchangeChange(ctx, plan.Name, ent.Name, objectID, fromNode); err != nil {
					return err
				}
				if obj.Deletion {
					res.Deleted++
				} else {
					res.Applied++
				}
				continue
			}
			// Нет встречной правки — идемпотентность по версии.
			localVer, exists, err := store.EntityVersionExists(ctx, ent.Name, id)
			if err != nil {
				return err
			}
			if exists && obj.Version <= localVer {
				res.Skipped++
				continue
			}
			if err := applyObject(ctx, store, ent, id, obj); err != nil {
				return err
			}
			if obj.Deletion {
				res.Deleted++
			} else {
				res.Applied++
			}
		}
		// Запоминаем номер принятого сообщения от узла-источника.
		if err := store.SetExchangeRecvNo(ctx, plan.Name, fromNode, pkg.MessageNo); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return LoadResult{}, err
	}
	return res, nil
}

// applyObject записывает один объект пакета: шапку (через db.Upsert — тот же
// путь коэрции значений, что и обычное сохранение), затем принудительно ставит
// системные колонки (точная версия/пометка/непроведён), затем табличные части.
func applyObject(ctx context.Context, store *storage.DB, ent *metadata.Entity, id uuid.UUID, obj PackageObject) error {
	_, exists, err := store.EntityVersionExists(ctx, ent.Name, id)
	if err != nil {
		return err
	}
	if !obj.Tombstone || !exists {
		if err := store.Upsert(ctx, ent.Name, id, obj.Fields, ent); err != nil {
			return err
		}
	}
	if err := store.SetExchangeObjectState(ctx, ent, id, obj.Version, obj.Deletion); err != nil {
		return err
	}
	if obj.Tombstone {
		return nil
	}
	for _, tp := range ent.TableParts {
		rows, ok := obj.TableParts[tp.Name]
		if !ok {
			continue
		}
		if err := store.UpsertTablePartRows(ctx, ent.Name, tp.Name, id, rows, tp); err != nil {
			return err
		}
	}
	return nil
}

// canonicalHeader канонизирует значения полей шапки объекта для транспорта.
func canonicalHeader(ent *metadata.Entity, row map[string]any) map[string]any {
	out := canonicalRow(ent.Fields, row)
	if ent.Hierarchical {
		out["parent_id"] = canonicalScalar(metadata.Field{}, row["parent_id"])
		out["is_folder"] = toBool(row["is_folder"])
	}
	return out
}

func emptyCanonicalHeader(ent *metadata.Entity) map[string]any {
	out := make(map[string]any, len(ent.Fields)+2)
	for _, f := range ent.Fields {
		out[f.Name] = nil
	}
	if ent.Hierarchical {
		out["parent_id"] = nil
		out["is_folder"] = false
	}
	return out
}

// canonicalRow канонизирует значения одного набора полей (шапка или строка ТЧ).
func canonicalRow(fields []metadata.Field, row map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f.Name] = canonicalScalar(f, row[f.Name])
	}
	return out
}

// canonicalScalar приводит значение поля к транспортному виду, устойчивому к
// JSON-раунд-трипу и пригодному для обратной записи через fieldValueDialect:
//   - nil → nil;
//   - булево → bool;
//   - число → точная десятичная строка (без потери точности через float64);
//   - дата (time.Time) → RFC3339;
//   - остальное (ссылки-UUID, строки, перечисления) → строка.
func canonicalScalar(f metadata.Field, v any) any {
	if v == nil {
		return nil
	}
	switch f.Type {
	case metadata.FieldTypeBool:
		return toBool(v)
	case metadata.FieldTypeDate:
		if t, ok := v.(time.Time); ok {
			return t.UTC().Format(time.RFC3339)
		}
		return fmt.Sprintf("%v", v)
	default:
		if t, ok := v.(time.Time); ok {
			return t.UTC().Format(time.RFC3339)
		}
		// Числа приходят как decimal.Decimal (Stringer даёт точную строку),
		// ссылки/строки — уже строки; fmt.Sprintf покрывает оба случая.
		return fmt.Sprintf("%v", v)
	}
}

// toBool нормализует булево из разных представлений БД (bool, int 0/1).
func toBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int:
		return t != 0
	case float64:
		return t != 0
	case string:
		return t == "true" || t == "1"
	}
	return false
}
