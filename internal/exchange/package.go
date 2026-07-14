package exchange

// Сборка и разбор пакетов обмена (.obx, план 86). Пакет — версионированный JSON:
// набор изменённых объектов (шапка + табличные части) с их версией-источником.
// Загрузка идемпотентна по версии: повторная доставка того же пакета не создаёт
// дублей и не наращивает ревизии.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// FormatV1 — значение поля Format в пакете первой версии.
const FormatV1 = "onebase-exchange/1"

// EntityResolver отдаёт метаданные сущности по имени. Реализуется
// runtime.Registry (метод GetEntity), но объявлен здесь, чтобы пакет обмена не
// зависел от runtime и был тестируем с фейковым резолвером.
type EntityResolver interface {
	GetEntity(name string) *metadata.Entity
}

// Package — конверт обмена.
type Package struct {
	Format    string          `json:"format"`
	Plan      string          `json:"plan"`
	FromNode  string          `json:"from_node"`
	ToNode    string          `json:"to_node"`
	MessageNo int64           `json:"message_no"`
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
	Type       string                       `json:"type"`
	ID         string                       `json:"id"`
	Version    int64                        `json:"version"`
	Deletion   bool                         `json:"deletion,omitempty"`
	ChangedAt  int64                        `json:"changed_at"`
	Fields     map[string]any               `json:"fields,omitempty"`
	TableParts map[string][]map[string]any  `json:"table_parts,omitempty"`
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
	if plan == nil {
		return nil, fmt.Errorf("exchange: plan is nil")
	}
	var out []byte
	err := store.WithTx(ctx, func(ctx context.Context) error {
		thisNode, err := store.GetExchangeThisNode(ctx, plan.Name)
		if err != nil {
			return err
		}
		changes, err := store.PendingExchangeChanges(ctx, plan.Name, toNode)
		if err != nil {
			return err
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
				continue // сущность убрана из конфигурации — пропускаем
			}
			id, err := uuid.Parse(ch.ObjectID)
			if err != nil {
				continue
			}
			row, err := store.GetByID(ctx, ent.Name, id, ent)
			if err != nil {
				continue // объект недоступен (например, физически удалён) — пропускаем
			}
			obj := PackageObject{
				Type:      ch.ObjectType,
				ID:        ch.ObjectID,
				Version:   ch.Version,
				Deletion:  ch.Deletion,
				ChangedAt: ch.ChangedAt,
				Fields:    canonicalHeader(ent, row),
			}
			for _, tp := range ent.TableParts {
				rows, err := store.GetTablePartRows(ctx, ent.Name, tp.Name, id, tp)
				if err != nil {
					return err
				}
				if len(rows) == 0 {
					continue
				}
				canon := make([]map[string]any, 0, len(rows))
				for _, r := range rows {
					canon = append(canon, canonicalRow(tp.Fields, r))
				}
				if obj.TableParts == nil {
					obj.TableParts = map[string][]map[string]any{}
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
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ParsePackage разбирает и валидирует конверт пакета.
func ParsePackage(data []byte) (*Package, error) {
	var pkg Package
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("exchange: разбор пакета: %w", err)
	}
	if pkg.Format != FormatV1 {
		return nil, fmt.Errorf("exchange: неизвестный формат пакета %q (ожидался %q)", pkg.Format, FormatV1)
	}
	return &pkg, nil
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
	if !strings.EqualFold(pkg.Plan, plan.Name) {
		return LoadResult{}, fmt.Errorf("exchange: пакет плана %q загружается в план %q", pkg.Plan, plan.Name)
	}
	var res LoadResult
	err = store.WithTx(ctx, func(ctx context.Context) error {
		thisNode, err := store.GetExchangeThisNode(ctx, plan.Name)
		if err != nil {
			return err
		}
		for _, obj := range pkg.Objects {
			ent := resolver.GetEntity(obj.Type)
			if ent == nil {
				res.Skipped++ // сущность неизвестна приёмнику
				continue
			}
			id, err := uuid.Parse(obj.ID)
			if err != nil {
				res.Skipped++
				continue
			}
			// Встречная правка: приёмник менял тот же объект и ещё не отправил
			// изменение источнику → конфликт, разрешаемый правилом плана.
			local, hasLocal, err := store.GetExchangeChange(ctx, plan.Name, obj.Type, obj.ID, pkg.FromNode)
			if err != nil {
				return err
			}
			if hasLocal {
				res.Conflicts++
				win, err := resolveConflict(ctx, store, plan, thisNode, pkg.FromNode, ent, id, obj, local.ChangedAt, opts.Hook)
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
				if obj.Deletion {
					res.Deleted++
				} else {
					res.Applied++
				}
				continue
			}
			// Нет встречной правки — идемпотентность по версии.
			localVer, exists := store.EntityVersionExists(ctx, ent.Name, id)
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
		if pkg.FromNode != "" {
			// Подтверждение из пакета очищает нашу очередь к отправителю: он
			// сообщил, что принял наши сообщения вплоть до AckNo.
			if pkg.AckNo > 0 {
				if _, err := store.AckExchangeChanges(ctx, plan.Name, pkg.FromNode, pkg.AckNo); err != nil {
					return err
				}
			}
			// Запоминаем номер принятого сообщения от узла-источника.
			if err := store.SetExchangeRecvNo(ctx, plan.Name, pkg.FromNode, pkg.MessageNo); err != nil {
				return err
			}
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
	if err := store.Upsert(ctx, ent.Name, id, obj.Fields, ent); err != nil {
		return err
	}
	if err := store.SetExchangeObjectState(ctx, ent, id, obj.Version, obj.Deletion); err != nil {
		return err
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
