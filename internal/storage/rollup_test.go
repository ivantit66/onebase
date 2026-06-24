package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

func rollupTestDB(t *testing.T) (context.Context, *DB) {
	t.Helper()
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "rollup.db"))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return ctx, db
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

// balMap читает остатки регистра как map[измерение]ресурс (для регистра с одним
// строковым измерением и одним ресурсом — как в тестах ниже).
func balMap(t *testing.T, ctx context.Context, db *DB, reg *metadata.Register, dim, res string) map[string]float64 {
	t.Helper()
	rows, err := db.GetBalances(ctx, reg.Name, reg, RegFilter{})
	if err != nil {
		t.Fatalf("GetBalances: %v", err)
	}
	m := make(map[string]float64, len(rows))
	for _, r := range rows {
		m[fmt.Sprintf("%v", r[dim])] = toFloat(r[res])
	}
	return m
}

func sameBal(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if absFloat(v-b[k]) > 1e-6 {
			return false
		}
	}
	return true
}

// TestRollup_FoldsAccumulationRegister — основной сценарий: движения по обе
// стороны даты свёртки сворачиваются в опорные остатки, полный остаток не
// меняется, опорные строки не считаются сиротами, повтор идемпотентен.
func TestRollup_FoldsAccumulationRegister(t *testing.T) {
	ctx, db := rollupTestDB(t)
	reg := &metadata.Register{
		Name:       "ОстаткиТоваров",
		Dimensions: []metadata.Field{{Name: "Товар", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}},
	}
	if err := db.MigrateRegisters(ctx, []*metadata.Register{reg}); err != nil {
		t.Fatalf("MigrateRegisters: %v", err)
	}

	mk := func(date, vid, tovar string, kol float64) {
		d := mustDate(t, date)
		rows := []map[string]any{{"Товар": tovar, "Количество": kol, "ВидДвижения": vid}}
		if err := db.WriteMovements(ctx, reg.Name, "ПоступлениеТоваров", uuid.New(), rows, reg, &d); err != nil {
			t.Fatalf("WriteMovements: %v", err)
		}
	}
	mk("2025-01-10", "Приход", "Молоко", 10) // < cutoff
	mk("2025-02-15", "Расход", "Молоко", 3)  // < cutoff
	mk("2025-01-05", "Приход", "Хлеб", 7)    // < cutoff
	mk("2025-06-20", "Приход", "Молоко", 5)  // >= cutoff

	cutoff := mustDate(t, "2025-03-01")
	opts := RollupOptions{Date: cutoff, Registers: []string{"ОстаткиТоваров"}}

	before := balMap(t, ctx, db, reg, "Товар", "Количество") // Молоко 12, Хлеб 7
	if before["Молоко"] != 12 || before["Хлеб"] != 7 {
		t.Fatalf("исходный остаток неверен: %v", before)
	}

	prev, err := db.RollupPreview(ctx, []*metadata.Register{reg}, nil, nil, nil, opts)
	if err != nil {
		t.Fatalf("RollupPreview: %v", err)
	}
	if len(prev.Registers) != 1 || prev.Registers[0].FoldedMovements != 3 || prev.Registers[0].OpeningRows != 2 {
		t.Fatalf("предпросмотр неверен: %+v", prev.Registers)
	}

	rep, err := db.Rollup(ctx, []*metadata.Register{reg}, nil, nil, nil, opts)
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rep.Registers[0].FoldedMovements != 3 || rep.Registers[0].OpeningRows != 2 {
		t.Fatalf("отчёт неверен: %+v", rep.Registers)
	}

	// Инвариант: полный остаток не изменился.
	after := balMap(t, ctx, db, reg, "Товар", "Количество")
	if !sameBal(before, after) {
		t.Fatalf("остаток изменился: до=%v после=%v", before, after)
	}

	table := metadata.RegisterTableName(reg.Name)
	var total, foldedLeft, opening int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&total)
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE period < ?", cutoff).Scan(&foldedLeft)
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE recorder_type = ?", RollupRecorderType).Scan(&opening)
	if total != 3 {
		t.Errorf("строк в регистре=%d, ждали 3 (2 опорных + 1 после даты)", total)
	}
	if foldedLeft != 0 {
		t.Errorf("остались движения до даты свёртки: %d", foldedLeft)
	}
	if opening != 2 {
		t.Errorf("опорных строк=%d, ждали 2", opening)
	}

	// Дата запрета выставлена на cutoff.
	if lock, ok := db.GetPostingLockDate(ctx); !ok || !lock.Equal(cutoff) {
		t.Errorf("дата запрета=%v ok=%v, ждали %v", lock, ok, cutoff)
	}

	// Опорные движения не считаются сиротами.
	for _, o := range db.OrphanMovements(ctx, []*metadata.Register{reg}, nil) {
		if o.RecorderType == RollupRecorderType {
			t.Errorf("опорные движения помечены сиротами: %+v", o)
		}
	}

	// Идемпотентность: повтор на ту же дату ничего не меняет.
	if _, err := db.Rollup(ctx, []*metadata.Register{reg}, nil, nil, nil, opts); err != nil {
		t.Fatalf("повторная свёртка: %v", err)
	}
	after2 := balMap(t, ctx, db, reg, "Товар", "Количество")
	if !sameBal(before, after2) {
		t.Fatalf("после повторной свёртки остаток изменился: %v", after2)
	}
	var total2 int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&total2)
	if total2 != 3 {
		t.Errorf("после повторной свёртки строк=%d, ждали 3", total2)
	}
}

func rollupDocEntity() *metadata.Entity {
	return &metadata.Entity{
		Name:    "РасходТовара",
		Kind:    metadata.KindDocument,
		Posting: true,
		Fields: []metadata.Field{
			{Name: "Дата", Type: metadata.FieldTypeDate},
			{Name: "Сумма", Type: metadata.FieldTypeNumber},
		},
	}
}

func docPosted(t *testing.T, ctx context.Context, db *DB, e *metadata.Entity, id uuid.UUID) bool {
	t.Helper()
	var p bool
	err := db.QueryRow(ctx,
		fmt.Sprintf("SELECT posted FROM %s WHERE id = ?", metadata.TableName(e.Name)),
		idArg(db.dialect, id)).Scan(&p)
	if err != nil {
		t.Fatalf("чтение posted: %v", err)
	}
	return p
}

// TestRollup_KeepDocumentsAndLock — keep-режим: документы остаются, но старые
// снимаются с проведения, а дата запроведения замораживает их перепроведение.
func TestRollup_KeepDocumentsAndLock(t *testing.T) {
	ctx, db := rollupTestDB(t)
	doc := rollupDocEntity()
	if err := db.Migrate(ctx, []*metadata.Entity{doc}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	mkDoc := func(date string) uuid.UUID {
		idStr, err := db.WriteCatalogRecord(ctx, doc, "", map[string]any{"Дата": mustDate(t, date), "Сумма": 100})
		if err != nil {
			t.Fatalf("WriteCatalogRecord: %v", err)
		}
		id, _ := uuid.Parse(idStr)
		if err := db.SetPosted(ctx, doc.Name, id, true); err != nil {
			t.Fatalf("SetPosted: %v", err)
		}
		return id
	}
	oldID := mkDoc("2025-01-15")
	newID := mkDoc("2025-06-20")
	cutoff := mustDate(t, "2025-03-01")

	rep, err := db.Rollup(ctx, nil, []*metadata.Entity{doc}, nil, nil, RollupOptions{Date: cutoff, DeleteDocuments: false})
	if err != nil {
		t.Fatalf("Rollup keep: %v", err)
	}
	if rep.DeletedDocs != 0 {
		t.Errorf("keep-режим: DeletedDocs=%d, ждали 0", rep.DeletedDocs)
	}
	if docPosted(t, ctx, db, doc, oldID) {
		t.Errorf("старый документ должен быть снят с проведения")
	}
	if !docPosted(t, ctx, db, doc, newID) {
		t.Errorf("новый документ не должен меняться")
	}
	if v, _, _ := db.PostingLockViolation(ctx, doc, oldID); !v {
		t.Errorf("старый документ должен попадать под дату запрета")
	}
	if v, _, _ := db.PostingLockViolation(ctx, doc, newID); v {
		t.Errorf("новый документ не должен попадать под дату запрета")
	}
}

// TestRollup_DeleteDocuments — delete-режим: документы с датой до свёртки
// физически удаляются, поздние остаются.
func TestRollup_DeleteDocuments(t *testing.T) {
	ctx, db := rollupTestDB(t)
	doc := rollupDocEntity()
	if err := db.Migrate(ctx, []*metadata.Entity{doc}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	mkDoc := func(date string) {
		if _, err := db.WriteCatalogRecord(ctx, doc, "", map[string]any{"Дата": mustDate(t, date), "Сумма": 100}); err != nil {
			t.Fatalf("WriteCatalogRecord: %v", err)
		}
	}
	mkDoc("2025-01-15")
	mkDoc("2025-02-20")
	mkDoc("2025-06-20")
	cutoff := mustDate(t, "2025-03-01")

	rep, err := db.Rollup(ctx, nil, []*metadata.Entity{doc}, nil, nil, RollupOptions{Date: cutoff, DeleteDocuments: true})
	if err != nil {
		t.Fatalf("Rollup delete: %v", err)
	}
	if rep.DeletedDocs != 2 {
		t.Errorf("DeletedDocs=%d, ждали 2", rep.DeletedDocs)
	}
	var left int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+metadata.TableName(doc.Name)).Scan(&left)
	if left != 1 {
		t.Errorf("осталось документов=%d, ждали 1", left)
	}
}

// TestRollup_DanglingRefsPreview — предпросмотр delete-режима оценивает, сколько
// ссылок повиснет (запись «Оплата» ссылается на удаляемый документ).
func TestRollup_DanglingRefsPreview(t *testing.T) {
	ctx, db := rollupTestDB(t)
	order := rollupDocEntity() // РасходТовара с полем Дата
	pay := &metadata.Entity{
		Name: "Оплата",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Заказ", Type: metadata.FieldTypeString, RefEntity: "РасходТовара"},
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{order, pay}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	oldStr, err := db.WriteCatalogRecord(ctx, order, "", map[string]any{"Дата": mustDate(t, "2025-01-15"), "Сумма": 100})
	if err != nil {
		t.Fatalf("write order: %v", err)
	}
	if _, err := db.WriteCatalogRecord(ctx, pay, "", map[string]any{"Наименование": "П1", "Заказ": oldStr}); err != nil {
		t.Fatalf("write pay: %v", err)
	}

	cutoff := mustDate(t, "2025-03-01")
	prev, err := db.RollupPreview(ctx, nil, []*metadata.Entity{order, pay}, nil, nil, RollupOptions{Date: cutoff, DeleteDocuments: true})
	if err != nil {
		t.Fatalf("RollupPreview: %v", err)
	}
	if prev.DeletedDocs != 1 {
		t.Errorf("DeletedDocs=%d, ждали 1", prev.DeletedDocs)
	}
	if prev.DanglingRefs != 1 {
		t.Errorf("DanglingRefs=%d, ждали 1", prev.DanglingRefs)
	}
}

// TestRollup_FoldsAccountRegister — свёртка бухрегистра: опорные проводки через
// вспомогательный счёт «000»; остатки счетов (активного и пассивного) не
// меняются, вспомогательный счёт нетит в ноль.
func TestRollup_FoldsAccountRegister(t *testing.T) {
	ctx, db := rollupTestDB(t)
	if err := db.EnsureAccountsTable(ctx); err != nil {
		t.Fatalf("EnsureAccountsTable: %v", err)
	}
	chart := &metadata.ChartOfAccounts{Name: "Основной", Accounts: []metadata.Account{
		{Code: "000", Name: "Вспомогательный", Kind: "active_passive"},
		{Code: "41", Name: "Товары", Kind: "active"},
		{Code: "60", Name: "Поставщики", Kind: "passive"},
	}}
	if err := db.SyncAccounts(ctx, []*metadata.ChartOfAccounts{chart}); err != nil {
		t.Fatalf("SyncAccounts: %v", err)
	}
	ar := &metadata.AccountRegister{
		Name: "Хозрасчетный", Accounts: "Основной",
		Resources: []metadata.Field{{Name: "Сумма", Type: metadata.FieldTypeNumber}},
	}
	if err := db.MigrateAccountRegisters(ctx, []*metadata.AccountRegister{ar}); err != nil {
		t.Fatalf("MigrateAccountRegisters: %v", err)
	}

	post := func(date string, sum float64) {
		d := mustDate(t, date)
		rows := []map[string]any{{"счётдт": "41", "счёткт": "60", "Сумма": sum}}
		if err := db.WriteAccountMovements(ctx, ar.Name, "Поступление", uuid.New(), rows, ar, &d); err != nil {
			t.Fatalf("WriteAccountMovements: %v", err)
		}
	}
	post("2025-01-10", 1000) // < cutoff
	post("2025-02-15", 500)  // < cutoff
	post("2025-06-20", 200)  // >= cutoff
	cutoff := mustDate(t, "2025-03-01")

	// Сырой остаток Дт−Кт счёта по всем движениям.
	bal := func(code string) float64 {
		rows, err := db.AccountBalances(ctx, ar.Name, "Основной", mustDate(t, "2025-12-31"), ar.Resources, nil)
		if err != nil {
			t.Fatalf("AccountBalances: %v", err)
		}
		for _, b := range rows {
			if c, _ := b["code"].(string); c == code {
				return toFloat(b["сумма_дт"]) - toFloat(b["сумма_кт"])
			}
		}
		return 0
	}
	before41, before60 := bal("41"), bal("60") // 1700, -1700

	rep, err := db.Rollup(ctx, nil, nil, []*metadata.AccountRegister{ar}, nil,
		RollupOptions{Date: cutoff, AccountRegisters: []string{"Хозрасчетный"}})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if len(rep.AccountRegisters) != 1 || rep.AccountRegisters[0].Note != "" {
		t.Fatalf("отчёт бухрегистра: %+v", rep.AccountRegisters)
	}
	if rep.AccountRegisters[0].FoldedMovements != 2 || rep.AccountRegisters[0].OpeningRows != 2 {
		t.Fatalf("свёрнуто/опорных: %+v", rep.AccountRegisters[0])
	}

	if bal("41") != before41 || bal("60") != before60 {
		t.Errorf("остатки изменились: 41 %v→%v, 60 %v→%v", before41, bal("41"), before60, bal("60"))
	}
	if a := bal("000"); absFloat(a) > 1e-6 {
		t.Errorf("вспомогательный счёт не обнулился: %v", a)
	}

	table := metadata.AccountRegTableName(ar.Name)
	var foldedLeft, opening int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE period < ?", cutoff).Scan(&foldedLeft)
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE регистратор_тип = ?", RollupRecorderType).Scan(&opening)
	if foldedLeft != 0 {
		t.Errorf("проводки до даты остались: %d", foldedLeft)
	}
	if opening != 2 {
		t.Errorf("опорных проводок=%d, ждали 2", opening)
	}
}

// TestRollup_AccountRegister_NoAuxAccount — нет вспомогательного счёта → бухрегистр
// пропускается с пометкой, движения не трогаются.
func TestRollup_AccountRegister_NoAuxAccount(t *testing.T) {
	ctx, db := rollupTestDB(t)
	if err := db.EnsureAccountsTable(ctx); err != nil {
		t.Fatalf("EnsureAccountsTable: %v", err)
	}
	chart := &metadata.ChartOfAccounts{Name: "ПС", Accounts: []metadata.Account{
		{Code: "41", Name: "Товары", Kind: "active"},
		{Code: "60", Name: "Поставщики", Kind: "passive"},
	}}
	if err := db.SyncAccounts(ctx, []*metadata.ChartOfAccounts{chart}); err != nil {
		t.Fatalf("SyncAccounts: %v", err)
	}
	ar := &metadata.AccountRegister{Name: "БезВспом", Accounts: "ПС",
		Resources: []metadata.Field{{Name: "Сумма", Type: metadata.FieldTypeNumber}}}
	if err := db.MigrateAccountRegisters(ctx, []*metadata.AccountRegister{ar}); err != nil {
		t.Fatalf("MigrateAccountRegisters: %v", err)
	}
	d := mustDate(t, "2025-01-10")
	if err := db.WriteAccountMovements(ctx, ar.Name, "Док", uuid.New(),
		[]map[string]any{{"счётдт": "41", "счёткт": "60", "Сумма": 100}}, ar, &d); err != nil {
		t.Fatalf("WriteAccountMovements: %v", err)
	}

	rep, err := db.Rollup(ctx, nil, nil, []*metadata.AccountRegister{ar}, nil,
		RollupOptions{Date: mustDate(t, "2025-03-01"), AccountRegisters: []string{"БезВспом"}})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if len(rep.AccountRegisters) != 1 || rep.AccountRegisters[0].Note == "" {
		t.Fatalf("ожидалась пометка о пропуске: %+v", rep.AccountRegisters)
	}
	var left int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+metadata.AccountRegTableName(ar.Name)).Scan(&left)
	if left != 1 {
		t.Errorf("движения тронуты при пропуске: осталось %d, ждали 1", left)
	}
}

// TestRollup_TrimInfoRegister — обрезка периодического регистра сведений: на
// каждую комбинацию измерений остаётся последний срез до даты свёртки; записи
// на/после даты не трогаются; СрезПоследних на дату >= cutoff не меняется.
func TestRollup_TrimInfoRegister(t *testing.T) {
	ctx, db := rollupTestDB(t)
	ir := &metadata.InfoRegister{
		Name:       "ЦеныНоменклатуры",
		Periodic:   true,
		Dimensions: []metadata.Field{{Name: "Товар", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Цена", Type: metadata.FieldTypeNumber}},
	}
	if err := db.MigrateInfoRegisters(ctx, []*metadata.InfoRegister{ir}); err != nil {
		t.Fatalf("MigrateInfoRegisters: %v", err)
	}
	set := func(date, tovar string, price float64) {
		d := mustDate(t, date)
		if err := db.InfoRegSet(ctx, ir, map[string]any{"Товар": tovar}, map[string]any{"Цена": price}, &d); err != nil {
			t.Fatalf("InfoRegSet: %v", err)
		}
	}
	set("2025-01-01", "Молоко", 50)
	set("2025-02-01", "Молоко", 55)
	set("2025-02-20", "Молоко", 60) // последний срез до cutoff
	set("2025-06-01", "Молоко", 70) // после cutoff — не трогаем
	set("2025-01-10", "Хлеб", 30)
	set("2025-02-10", "Хлеб", 33) // последний срез до cutoff (после cutoff срезов нет)
	cutoff := mustDate(t, "2025-03-01")
	opts := RollupOptions{Date: cutoff, InfoRegisters: []string{"ЦеныНоменклатуры"}}

	// Предпросмотр: до cutoff 5 строк, 2 комбинации → обрезать 3, оставить 2.
	prev, err := db.RollupPreview(ctx, nil, nil, nil, []*metadata.InfoRegister{ir}, opts)
	if err != nil {
		t.Fatalf("RollupPreview: %v", err)
	}
	if len(prev.InfoRegisters) != 1 || prev.InfoRegisters[0].FoldedMovements != 3 || prev.InfoRegisters[0].OpeningRows != 2 {
		t.Fatalf("предпросмотр обрезки неверен: %+v", prev.InfoRegisters)
	}

	table := metadata.InfoRegTableName(ir.Name)
	// sliceAt — цена по последнему срезу на/до даты (как СрезПоследних), прямым SQL.
	sliceAt := func(tovar, onDate string) float64 {
		var p float64
		if err := db.QueryRow(ctx,
			"SELECT цена FROM "+table+" WHERE товар = ? AND period <= ? ORDER BY period DESC LIMIT 1",
			tovar, mustDate(t, onDate)).Scan(&p); err != nil {
			t.Fatalf("срез %s на %s: %v", tovar, onDate, err)
		}
		return p
	}
	milkAprBefore := sliceAt("Молоко", "2025-04-01") // 60 (срез 2025-02-20)

	rep, err := db.Rollup(ctx, nil, nil, nil, []*metadata.InfoRegister{ir}, opts)
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rep.InfoRegisters[0].FoldedMovements != 3 || rep.InfoRegisters[0].OpeningRows != 2 {
		t.Fatalf("отчёт обрезки неверен: %+v", rep.InfoRegisters)
	}

	var total, beforeCut int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&total)
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE period < ?", cutoff).Scan(&beforeCut)
	if beforeCut != 2 {
		t.Errorf("до cutoff осталось строк=%d, ждали 2 (по последнему срезу на товар)", beforeCut)
	}
	if total != 3 { // 2 последних среза до cutoff + 1 запись Молоко после cutoff
		t.Errorf("всего строк=%d, ждали 3", total)
	}

	// Инвариант: СрезПоследних на дату >= cutoff не изменился.
	if milkAprBefore != 60 || sliceAt("Молоко", "2025-04-01") != 60 {
		t.Errorf("СрезПоследних(Молоко, апрель) изменился: %v→%v (ждали 60)", milkAprBefore, sliceAt("Молоко", "2025-04-01"))
	}
	if sliceAt("Хлеб", "2025-04-01") != 33 {
		t.Errorf("СрезПоследних(Хлеб, апрель)=%v, ждали 33", sliceAt("Хлеб", "2025-04-01"))
	}
	if sliceAt("Молоко", "2025-07-01") != 70 {
		t.Errorf("СрезПоследних(Молоко, июль)=%v, ждали 70 (запись после cutoff цела)", sliceAt("Молоко", "2025-07-01"))
	}

	// Идемпотентность: повтор не меняет данные.
	if _, err := db.Rollup(ctx, nil, nil, nil, []*metadata.InfoRegister{ir}, opts); err != nil {
		t.Fatalf("повторная обрезка: %v", err)
	}
	var total2 int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&total2)
	if total2 != 3 {
		t.Errorf("после повторной обрезки строк=%d, ждали 3", total2)
	}
}

// TestRollup_InfoRegister_NonPeriodicSkipped — непериодический регистр сведений
// в обрезку не попадает: помечается как пропущенный, данные не трогаются.
func TestRollup_InfoRegister_NonPeriodicSkipped(t *testing.T) {
	ctx, db := rollupTestDB(t)
	ir := &metadata.InfoRegister{
		Name:       "Настройки",
		Periodic:   false,
		Dimensions: []metadata.Field{{Name: "Ключ", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Значение", Type: metadata.FieldTypeString}},
	}
	if err := db.MigrateInfoRegisters(ctx, []*metadata.InfoRegister{ir}); err != nil {
		t.Fatalf("MigrateInfoRegisters: %v", err)
	}
	if err := db.InfoRegSet(ctx, ir, map[string]any{"Ключ": "a"}, map[string]any{"Значение": "1"}, nil); err != nil {
		t.Fatalf("InfoRegSet: %v", err)
	}
	rep, err := db.Rollup(ctx, nil, nil, nil, []*metadata.InfoRegister{ir},
		RollupOptions{Date: mustDate(t, "2025-03-01"), InfoRegisters: []string{"Настройки"}})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if len(rep.InfoRegisters) != 1 || rep.InfoRegisters[0].Note == "" {
		t.Fatalf("ожидалась пометка о пропуске непериодического: %+v", rep.InfoRegisters)
	}
	var left int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+metadata.InfoRegTableName(ir.Name)).Scan(&left)
	if left != 1 {
		t.Errorf("данные непериодического регистра тронуты: осталось %d, ждали 1", left)
	}
}

// TestRollup_DanglingRefsGate — жёсткий гейт: при удалении документов свёртка
// отказывает (откат), если на удаляемый документ ссылается СОХРАНЯЕМАЯ запись;
// в keep-режиме (документы не удаляются) гейт не применяется.
func TestRollup_DanglingRefsGate(t *testing.T) {
	ctx, db := rollupTestDB(t)
	order := rollupDocEntity() // РасходТовара с полем Дата
	pay := &metadata.Entity{
		Name: "Оплата",
		Kind: metadata.KindCatalog, // справочник сохраняется → ссылка повиснет
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Заказ", Type: metadata.FieldTypeString, RefEntity: "РасходТовара"},
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{order, pay}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	oldStr, _ := db.WriteCatalogRecord(ctx, order, "", map[string]any{"Дата": mustDate(t, "2025-01-15"), "Сумма": 100})
	if _, err := db.WriteCatalogRecord(ctx, pay, "", map[string]any{"Наименование": "П1", "Заказ": oldStr}); err != nil {
		t.Fatalf("write pay: %v", err)
	}
	cutoff := mustDate(t, "2025-03-01")

	// Гейт срабатывает: свёртка отклонена, документ НЕ удалён (откат транзакции).
	if _, err := db.Rollup(ctx, nil, []*metadata.Entity{order, pay}, nil, nil,
		RollupOptions{Date: cutoff, DeleteDocuments: true}); err == nil {
		t.Fatalf("ожидался отказ свёртки из-за повисшей ссылки")
	}
	var orders int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM "+metadata.TableName(order.Name)).Scan(&orders)
	if orders != 1 {
		t.Errorf("документ удалён вопреки гейту: осталось %d, ждали 1", orders)
	}

	// keep-режим: документы не удаляются — гейт не применяется, свёртка проходит.
	rep, err := db.Rollup(ctx, nil, []*metadata.Entity{order, pay}, nil, nil,
		RollupOptions{Date: cutoff, DeleteDocuments: false})
	if err != nil {
		t.Fatalf("Rollup keep: %v", err)
	}
	if rep.DeletedDocs != 0 {
		t.Errorf("keep-режим: DeletedDocs=%d, ждали 0", rep.DeletedDocs)
	}
}

// TestRollup_DanglingRefs_BetweenDeleted — ссылка от одного удаляемого документа
// на другой удаляемый повисшей не считается (точный счёт): гейт пропускает без
// флага, оба документа удаляются.
func TestRollup_DanglingRefs_BetweenDeleted(t *testing.T) {
	ctx, db := rollupTestDB(t)
	order := rollupDocEntity() // РасходТовара
	payDoc := &metadata.Entity{
		Name: "ОплатаДок",
		Kind: metadata.KindDocument, // документ до cutoff → тоже удаляется
		Fields: []metadata.Field{
			{Name: "Дата", Type: metadata.FieldTypeDate},
			{Name: "Заказ", Type: metadata.FieldTypeString, RefEntity: "РасходТовара"},
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{order, payDoc}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	oldStr, _ := db.WriteCatalogRecord(ctx, order, "", map[string]any{"Дата": mustDate(t, "2025-01-15"), "Сумма": 100})
	if _, err := db.WriteCatalogRecord(ctx, payDoc, "", map[string]any{"Дата": mustDate(t, "2025-01-20"), "Заказ": oldStr}); err != nil {
		t.Fatalf("write payDoc: %v", err)
	}
	cutoff := mustDate(t, "2025-03-01")
	opts := RollupOptions{Date: cutoff, DeleteDocuments: true}

	// Предпросмотр: повисших ссылок нет — обе записи удаляются.
	prev, err := db.RollupPreview(ctx, nil, []*metadata.Entity{order, payDoc}, nil, nil, opts)
	if err != nil {
		t.Fatalf("RollupPreview: %v", err)
	}
	if prev.DanglingRefs != 0 {
		t.Errorf("DanglingRefs=%d, ждали 0 (источник ссылки тоже удаляется)", prev.DanglingRefs)
	}

	// Свёртка проходит без IgnoreDanglingRefs и удаляет оба документа.
	rep, err := db.Rollup(ctx, nil, []*metadata.Entity{order, payDoc}, nil, nil, opts)
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rep.DeletedDocs != 2 {
		t.Errorf("DeletedDocs=%d, ждали 2", rep.DeletedDocs)
	}
}
