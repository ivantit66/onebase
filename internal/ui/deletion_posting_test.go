package ui

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// newPostingDoc поднимает SQLite-Server с проводимым документом ПоступлениеТоваров
// (ТЧ Товары), который в ОбработкаПроведения пишет приход в регистр ОстаткиТоваров.
func newPostingDoc(t *testing.T) (context.Context, *storage.DB, *Server, *docProxy, *metadata.Entity) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	doc := &metadata.Entity{
		Name:    "ПоступлениеТоваров",
		Kind:    metadata.KindDocument,
		Posting: true,
		Fields:  []metadata.Field{{Name: "Номер", Type: metadata.FieldTypeString}},
		TableParts: []metadata.TablePart{{Name: "Товары", Fields: []metadata.Field{
			{Name: "Номенклатура", Type: metadata.FieldTypeString},
			{Name: "Количество", Type: metadata.FieldTypeNumber},
		}}},
	}
	reg := &metadata.Register{
		Name:       "ОстаткиТоваров",
		Dimensions: []metadata.Field{{Name: "Номенклатура", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{doc}); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateRegisters(ctx, []*metadata.Register{reg}); err != nil {
		t.Fatal(err)
	}

	onPostSrc := `Процедура ОбработкаПроведения()
  Для Каждого Стр Из ЭтотОбъект.Товары Цикл
    Дв = Движения.ОстаткиТоваров.Добавить();
    Дв.ВидДвижения = "Приход";
    Дв.Номенклатура = Стр.Номенклатура;
    Дв.Количество = Стр.Количество;
  КонецЦикла;
КонецПроцедуры`
	registry := runtime.NewRegistry()
	registry.Load(runtime.LoadOptions{
		Entities:  []*metadata.Entity{doc},
		Programs:  map[string]*ast.Program{"ПоступлениеТоваров": mustParse(t, onPostSrc)},
		Registers: []*metadata.Register{reg},
	})
	interp := interpreter.New()
	interp.LookupProc = registry.GetModuleProc
	s := &Server{store: db, reg: registry, interp: interp, lockMgr: runtime.NewLockManager(), messages: NewMessageStore()}
	dp := newDocsRoot(s, interpreter.NewTxState(ctx)).Get("ПоступлениеТоваров").(*docProxy)
	return ctx, db, s, dp, doc
}

// postOne создаёт+заполняет+проводит документ ПОС-001 (Тумбочка x100) и возвращает writer.
func postOne(t *testing.T, dp *docProxy) *docWriter {
	t.Helper()
	w := dp.CallMethod("создать", nil).(*docWriter)
	w.Set("Номер", "ПОС-001")
	tp := w.Get("Товары").(*tpProxy)
	r := tp.CallMethod("добавить", nil).(*interpreter.MapThis)
	r.Set("Номенклатура", "Тумбочка")
	r.Set("Количество", float64(100))
	w.CallMethod("провести", nil)
	return w
}

// DSL .Провести() помеченного документа должно отклоняться РАНО — до запуска
// ОбработкаПроведения и записи движений. Берём непроведённый помеченный документ:
// с ранним guard'ом движений не будет (0); без него хук отработал бы и saveMovements
// записал бы движения до того, как backstop SetPosted вернёт ошибку (утечка движений).
func TestDocWriterPost_BlockedWhenMarked(t *testing.T) {
	ctx, db, _, dp, _ := newPostingDoc(t)

	// Создать + Записать (без проведения) → строка документа есть, движений нет.
	w := dp.CallMethod("создать", nil).(*docWriter)
	w.Set("Номер", "ПОС-002")
	tp := w.Get("Товары").(*tpProxy)
	r := tp.CallMethod("добавить", nil).(*interpreter.MapThis)
	r.Set("Номенклатура", "Тумбочка")
	r.Set("Количество", float64(100))
	w.CallMethod("записать", nil)

	// Пометить непроведённый документ на удаление.
	if err := db.MarkForDeletion(ctx, "ПоступлениеТоваров", w.obj.ID, true); err != nil {
		t.Fatal(err)
	}

	// Провести помеченный → ранний guard, ErrPostingDeletionMarked.
	if err := w.post(); !errors.Is(err, storage.ErrPostingDeletionMarked) {
		t.Fatalf("ожидался ErrPostingDeletionMarked, получили %v", err)
	}

	// Ранний guard сработал ДО хука/saveMovements ⇒ движений нет и posted=false.
	var mov int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM рег_остаткитоваров").Scan(&mov)
	if mov != 0 {
		t.Fatalf("guard должен сработать до saveMovements: движений %d, ожидалось 0", mov)
	}
	var posted bool
	db.QueryRow(ctx, "SELECT posted FROM поступлениетоваров LIMIT 1").Scan(&posted)
	if posted {
		t.Error("помеченный документ не должен быть проведён")
	}
}

// Пометка проведённого документа на удаление авто-отменяет проведение
// (чистит движения, снимает posted). Снятие пометки проведение не возвращает.
func TestServerMarkForDeletion_AutoUnposts(t *testing.T) {
	ctx, db, s, dp, doc := newPostingDoc(t)
	w := postOne(t, dp)

	var mov int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM рег_остаткитоваров").Scan(&mov)
	if mov != 1 {
		t.Fatalf("до пометки ожидалось 1 движение, получили %d", mov)
	}

	if err := s.markForDeletion(ctx, doc, w.obj.ID, true); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(ctx, "SELECT COUNT(*) FROM рег_остаткитоваров").Scan(&mov)
	if mov != 0 {
		t.Errorf("после пометки движений должно быть 0, получили %d", mov)
	}
	var posted, marked bool
	db.QueryRow(ctx, "SELECT posted, deletion_mark FROM поступлениетоваров LIMIT 1").Scan(&posted, &marked)
	if posted {
		t.Error("проведение должно быть снято при пометке")
	}
	if !marked {
		t.Error("документ должен быть помечен на удаление")
	}

	// Снятие пометки проведение НЕ возвращает.
	if err := s.markForDeletion(ctx, doc, w.obj.ID, false); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(ctx, "SELECT posted, deletion_mark FROM поступлениетоваров LIMIT 1").Scan(&posted, &marked)
	if posted {
		t.Error("снятие пометки не должно проводить документ")
	}
	if marked {
		t.Error("пометка должна быть снята")
	}
}
