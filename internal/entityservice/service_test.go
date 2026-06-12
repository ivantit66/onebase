package entityservice

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// Регресс (M2): «Записать» уже проведённого документа должно сбрасывать движения
// по ВСЕМ типам регистров, включая регистры сведений. Раньше чистились только
// регистры накопления (s.Reg.Registers()), и строки инфорегистра оставались
// осиротевшими после отмены проведения.
func TestSave_UnpostClearsInfoRegisterMovements(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	doc := &metadata.Entity{
		Name:    "УстановкаЦен",
		Kind:    metadata.KindDocument,
		Posting: true,
		Fields:  []metadata.Field{{Name: "Номер", Type: metadata.FieldTypeString}},
	}
	ir := &metadata.InfoRegister{
		Name:       "Цены",
		Periodic:   true,
		Dimensions: []metadata.Field{{Name: "Номенклатура", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Цена", Type: metadata.FieldTypeNumber}},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{doc}); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateInfoRegisters(ctx, []*metadata.InfoRegister{ir}); err != nil {
		t.Fatal(err)
	}

	registry := runtime.NewRegistry()
	registry.Load(runtime.LoadOptions{
		Entities: []*metadata.Entity{doc},
		InfoRegs: []*metadata.InfoRegister{ir},
	})
	svc := &Service{Store: db, Reg: registry, Interp: interpreter.New()}

	// Готовим «проведённый» документ: создаём строку, пишем движение
	// инфорегистра и ставим флаг проведения.
	docID := uuid.New()
	if err := db.Upsert(ctx, "УстановкаЦен", docID, map[string]any{"Номер": "1"}, doc); err != nil {
		t.Fatal(err)
	}
	period := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := db.WriteInfoMovements(ctx, "Цены", "УстановкаЦен", docID,
		[]map[string]any{{"Номенклатура": "Гвоздь", "Цена": float64(90)}}, ir, &period); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPosted(ctx, "УстановкаЦен", docID, true); err != nil {
		t.Fatal(err)
	}

	if rows, err := db.InfoRegList(ctx, ir, storage.RegFilter{}); err != nil {
		t.Fatal(err)
	} else if len(rows) != 1 {
		t.Fatalf("предусловие: ожидалась 1 строка инфорегистра, получено %d", len(rows))
	}

	// «Записать» (Action="") уже проведённого документа → отмена проведения.
	if _, err := svc.Save(ctx, SaveRequest{
		Entity: doc,
		ID:     docID,
		IsNew:  false,
		Fields: map[string]any{"Номер": "1"},
		Action: "",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rows, err := db.InfoRegList(ctx, ir, storage.RegFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("движения инфорегистра не сброшены при отмене проведения: осталось %d строк: %v", len(rows), rows)
	}
}
