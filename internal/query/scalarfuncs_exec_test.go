package query_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
	"github.com/ivantit66/onebase/internal/storage"
)

// E2E-регрессия issue #39: запрос со скалярными 1С-функциями (ОКР/АБС/ЦЕЛ)
// должен не только компилироваться, но и ИСПОЛНЯТЬСЯ на SQLite — раньше падал
// с `no such function: окр`, т.к. имя уходило в БД сырым. Берём выручку 125.5,
// чтобы три функции дали три разных результата и округление нельзя было спутать
// с усечением: ОКР→126, АБС→125.5, ЦЕЛ→125.
func TestScalarFuncs_Execute_SQLite(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "scalar.db")
	db, err := storage.ConnectSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	reg := &metadata.Register{
		Name:      "ВаловаяПрибыль",
		Resources: []metadata.Field{{Name: "Выручка", Type: metadata.FieldTypeNumber}},
	}
	if err := db.MigrateRegisters(ctx, []*metadata.Register{reg}); err != nil {
		t.Fatalf("migrate register: %v", err)
	}

	_, err = db.Exec(ctx,
		`INSERT INTO рег_валоваяприбыль
		   (id, recorder, recorder_type, line_number, period, вид_движения, выручка)
		 VALUES ('1','1','Документ.X',1,'2026-01-01','Приход','125.5')`)
	if err != nil {
		t.Fatalf("insert movement: %v", err)
	}

	src := `ВЫБРАТЬ
	  ОКР(СУММА(Выручка), 0) КАК Округлённо,
	  АБС(СУММА(Выручка))    КАК Модуль,
	  ЦЕЛ(СУММА(Выручка))    КАК Целое
	ИЗ РегистрНакопления.ВаловаяПрибыль`

	res, err := query.Compile(src, query.CompileOpts{
		Dialect:   storage.SQLiteDialect{},
		Registers: []*metadata.Register{reg},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var okruglyonno, modul, celoe float64
	if err := db.QueryRow(ctx, res.SQL, res.Args...).Scan(&okruglyonno, &modul, &celoe); err != nil {
		// Именно здесь раньше всплывал `no such function: окр`.
		t.Fatalf("execute %q\n  SQL: %s\n  err: %v", src, res.SQL, err)
	}

	if okruglyonno != 126 {
		t.Errorf("ОКР(125.5, 0) = %v, ожидалось 126", okruglyonno)
	}
	if modul != 125.5 {
		t.Errorf("АБС(125.5) = %v, ожидалось 125.5", modul)
	}
	if celoe != 125 {
		t.Errorf("ЦЕЛ(125.5) = %v, ожидалось 125 (усечение, не округление)", celoe)
	}
}
