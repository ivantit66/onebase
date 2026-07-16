package query_test

import (
	"context"
	"math"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
	"github.com/ivantit66/onebase/internal/storage"
)

// План 80: Остатки() без момента для регистра с включёнными итогами читает
// таблицу итоги_* (быстрый путь), а не суммирует движения. Результат совпадает
// с обычным путём (регистр без итогов) на тех же данных.
func TestBalancesTotals_FastPathExecutes(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	withTotals := &metadata.Register{
		Name:       "ОстаткиТоваров",
		Dimensions: []metadata.Field{{Name: "Номенклатура", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}},
		Totals:     metadata.RegisterTotals{Enabled: true},
	}
	noTotals := &metadata.Register{
		Name:       "ОстаткиБезИтогов",
		Dimensions: []metadata.Field{{Name: "Номенклатура", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}},
	}
	regs := []*metadata.Register{withTotals, noTotals}
	if err := db.MigrateRegisters(ctx, regs); err != nil {
		t.Fatal(err)
	}

	p := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	// Стол: +5, +3 = 8; Стул: +2 = 2 — в оба регистра.
	for _, reg := range regs {
		if err := db.WriteMovements(ctx, reg.Name, "Пост", uuid.New(),
			[]map[string]any{
				{"ВидДвижения": "Приход", "Номенклатура": "Стол", "Количество": float64(5)},
				{"ВидДвижения": "Приход", "Номенклатура": "Стул", "Количество": float64(2)},
			}, reg, &p); err != nil {
			t.Fatal(err)
		}
		if err := db.WriteMovements(ctx, reg.Name, "Пост", uuid.New(),
			[]map[string]any{{"ВидДвижения": "Приход", "Номенклатура": "Стол", "Количество": float64(3)}},
			reg, &p); err != nil {
			t.Fatal(err)
		}
	}

	balances := func(reg *metadata.Register) map[string]float64 {
		r, err := query.Compile(
			"ВЫБРАТЬ Номенклатура, КоличествоОстаток ИЗ РегистрНакопления."+reg.Name+".Остатки()",
			query.CompileOpts{Registers: regs, Dialect: storage.SQLiteDialect{}},
		)
		if err != nil {
			t.Fatalf("compile %s: %v", reg.Name, err)
		}
		totalsTable := metadata.RegisterTotalsTableName(reg.Name)
		if reg.Totals.Enabled {
			if !strings.Contains(r.SQL, totalsTable) {
				t.Errorf("Остатки() для регистра с итогами должен читать %s, SQL: %s", totalsTable, r.SQL)
			}
		} else if strings.Contains(r.SQL, totalsTable) {
			t.Errorf("регистр без итогов не должен читать %s, SQL: %s", totalsTable, r.SQL)
		}
		rows, err := db.Query(ctx, r.SQL, r.Args...)
		if err != nil {
			t.Fatalf("exec %s: %v\nSQL: %s", reg.Name, err, r.SQL)
		}
		defer rows.Close()
		out := map[string]float64{}
		for rows.Next() {
			var name string
			var qty float64
			if err := rows.Scan(&name, &qty); err != nil {
				t.Fatal(err)
			}
			out[name] = qty
		}
		return out
	}

	fast := balances(withTotals)
	slow := balances(noTotals)
	if fast["Стол"] != 8 || fast["Стул"] != 2 {
		t.Errorf("быстрый путь: Стол=%v Стул=%v, ожидалось 8 и 2", fast["Стол"], fast["Стул"])
	}
	if fast["Стол"] != slow["Стол"] || fast["Стул"] != slow["Стул"] {
		t.Errorf("быстрый путь разошёлся с обычным: %v vs %v", fast, slow)
	}
}

// Остатки(&Момент) через итоги (этап 2) == расчёт на лету. Проверяем тождество
// на данных с движениями в разных месяцах, для набора моментов и с исключением
// регистратора по docID. Регистр с итогами (быстрый путь: месяцы до момента из
// итоги_* + хвост месяца из рег_*) и без итогов (on-the-fly) должны совпадать.
func TestBalancesTotals_MomentMatchesOnTheFly(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mkReg := func(name string, totals bool) *metadata.Register {
		return &metadata.Register{
			Name:       name,
			Dimensions: []metadata.Field{{Name: "Номенклатура", Type: metadata.FieldTypeString}},
			Resources:  []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}},
			Totals:     metadata.RegisterTotals{Enabled: totals},
		}
	}
	withT := mkReg("СИтогами", true)
	noT := mkReg("БезИтогов", false)
	regs := []*metadata.Register{withT, noT}
	if err := db.MigrateRegisters(ctx, regs); err != nil {
		t.Fatal(err)
	}

	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 12, 0, 0, 0, time.UTC) }
	rec1, rec2, rec3 := uuid.New(), uuid.New(), uuid.New()
	// Движения в трёх месяцах — в оба регистра одинаково.
	type wr struct {
		rec    uuid.UUID
		period time.Time
		rows   []map[string]any
	}
	writes := []wr{
		{rec1, d(2026, 1, 10), []map[string]any{{"ВидДвижения": "Приход", "Номенклатура": "Стол", "Количество": float64(5)}}},
		{rec2, d(2026, 2, 15), []map[string]any{
			{"ВидДвижения": "Приход", "Номенклатура": "Стол", "Количество": float64(3)},
			{"ВидДвижения": "Приход", "Номенклатура": "Стул", "Количество": float64(2)}}},
		{rec3, d(2026, 3, 20), []map[string]any{{"ВидДвижения": "Расход", "Номенклатура": "Стол", "Количество": float64(2)}}},
	}
	for _, w := range writes {
		for _, reg := range regs {
			p := w.period
			if err := db.WriteMovements(ctx, reg.Name, "Док", w.rec, w.rows, reg, &p); err != nil {
				t.Fatal(err)
			}
		}
	}

	balancesAt := func(reg *metadata.Register, mt *momentValue) map[string]float64 {
		r, err := query.Compile(
			"ВЫБРАТЬ Номенклатура, КоличествоОстаток ИЗ РегистрНакопления."+reg.Name+".Остатки(&МВ)",
			query.CompileOpts{Registers: regs, Params: map[string]any{"МВ": mt}, Dialect: storage.SQLiteDialect{}},
		)
		if err != nil {
			t.Fatalf("compile %s: %v", reg.Name, err)
		}
		if reg.Totals.Enabled && !strings.Contains(r.SQL, metadata.RegisterTotalsTableName(reg.Name)) {
			t.Errorf("Остатки(&Момент) с итогами должен читать %s, SQL: %s", metadata.RegisterTotalsTableName(reg.Name), r.SQL)
		}
		rows, err := db.Query(ctx, r.SQL, r.Args...)
		if err != nil {
			t.Fatalf("exec %s: %v\nSQL: %s", reg.Name, err, r.SQL)
		}
		defer rows.Close()
		out := map[string]float64{}
		for rows.Next() {
			var name string
			var qty float64
			if err := rows.Scan(&name, &qty); err != nil {
				t.Fatal(err)
			}
			out[name] = qty
		}
		return out
	}

	moments := []*momentValue{
		{period: d(2026, 1, 1)},                        // до всех
		{period: d(2026, 1, 31)},                       // после января
		{period: d(2026, 2, 28)},                       // после февраля
		{period: d(2026, 3, 31)},                       // после марта
		{period: time.Now().UTC()},                     // текущий
		{period: d(2026, 2, 15), docID: rec2.String()}, // момент фев с исключением rec2
	}
	for i, mt := range moments {
		fast := balancesAt(withT, mt)
		slow := balancesAt(noT, mt)
		if len(fast) != len(slow) {
			t.Fatalf("момент %d: число строк итоги=%v на лету=%v", i, fast, slow)
		}
		for k, v := range slow {
			if fast[k] != v {
				t.Errorf("момент %d, %s: итоги=%v, на лету=%v", i, k, fast[k], v)
			}
		}
	}
}

// Рандомизированное тождество момента: случайные движения по нескольким месяцам
// и множество случайных моментов; итоги и on-the-fly совпадают везде.
func TestBalancesTotals_MomentRandomized(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mkReg := func(name string, totals bool) *metadata.Register {
		return &metadata.Register{
			Name:       name,
			Dimensions: []metadata.Field{{Name: "Товар", Type: metadata.FieldTypeString}},
			Resources:  []metadata.Field{{Name: "Кол", Type: metadata.FieldTypeNumber}},
			Totals:     metadata.RegisterTotals{Enabled: totals},
		}
	}
	withT, noT := mkReg("СИт", true), mkReg("БезИт", false)
	regs := []*metadata.Register{withT, noT}
	if err := db.MigrateRegisters(ctx, regs); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(7))
	noms := []string{"A", "B", "C"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		p := base.AddDate(0, 0, rng.Intn(180)) // ~6 месяцев
		vid := "Приход"
		if rng.Intn(2) == 0 {
			vid = "Расход"
		}
		row := []map[string]any{{"ВидДвижения": vid, "Товар": noms[rng.Intn(len(noms))], "Кол": float64(1 + rng.Intn(10))}}
		rec := uuid.New()
		for _, reg := range regs {
			pp := p
			if err := db.WriteMovements(ctx, reg.Name, "Д", rec, row, reg, &pp); err != nil {
				t.Fatal(err)
			}
		}
	}
	balancesAt := func(reg *metadata.Register, mt *momentValue) map[string]float64 {
		r, err := query.Compile("ВЫБРАТЬ Товар, КолОстаток ИЗ РегистрНакопления."+reg.Name+".Остатки(&МВ)",
			query.CompileOpts{Registers: regs, Params: map[string]any{"МВ": mt}, Dialect: storage.SQLiteDialect{}})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := db.Query(ctx, r.SQL, r.Args...)
		if err != nil {
			t.Fatalf("exec: %v\nSQL: %s", err, r.SQL)
		}
		defer rows.Close()
		out := map[string]float64{}
		for rows.Next() {
			var n string
			var q float64
			if err := rows.Scan(&n, &q); err != nil {
				t.Fatal(err)
			}
			out[n] = q
		}
		return out
	}
	for i := 0; i < 30; i++ {
		mt := &momentValue{period: base.AddDate(0, 0, rng.Intn(200))}
		fast, slow := balancesAt(withT, mt), balancesAt(noT, mt)
		if len(fast) != len(slow) {
			t.Fatalf("момент %d (%s): итоги=%v на лету=%v", i, mt.period.Format("2006-01-02"), fast, slow)
		}
		for k, v := range slow {
			if fast[k] != v {
				t.Errorf("момент %d (%s) %s: итоги=%v на лету=%v", i, mt.period.Format("2006-01-02"), k, fast[k], v)
			}
		}
	}
}

// ОстаткиИОбороты(&Начало, &Конец) через итоги сверяется с эталоном, вычисленным
// в Go напрямую из движений (на лету с датами-параметрами на SQLite ломается
// переиспользованием плейсхолдера — отдельный предсуществующий баг). Проверяем
// все колонки: начальный (period<start), приход/расход (в [start,end]),
// конечный (period<=end).
func TestBalancesTurnoversTotals_MatchesGolden(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reg := &metadata.Register{
		Name:       "ОИ",
		Dimensions: []metadata.Field{{Name: "Товар", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Кол", Type: metadata.FieldTypeNumber}},
		Totals:     metadata.RegisterTotals{Enabled: true},
	}
	if err := db.MigrateRegisters(ctx, []*metadata.Register{reg}); err != nil {
		t.Fatal(err)
	}
	type mvRec struct {
		nom, vid string
		period   time.Time
		qty      float64
	}
	var all []mvRec
	rng := rand.New(rand.NewSource(11))
	noms := []string{"A", "B", "C"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		p := base.AddDate(0, 0, rng.Intn(180))
		vid := "Приход"
		if rng.Intn(2) == 0 {
			vid = "Расход"
		}
		nom := noms[rng.Intn(len(noms))]
		qty := float64(1 + rng.Intn(10))
		all = append(all, mvRec{nom, vid, p, qty})
		pp := p
		if err := db.WriteMovements(ctx, reg.Name, "Д", uuid.New(),
			[]map[string]any{{"ВидДвижения": vid, "Товар": nom, "Кол": qty}}, reg, &pp); err != nil {
			t.Fatal(err)
		}
	}
	// Эталон в Go: начальный/приход/расход/конечный по каждому товару.
	golden := func(start, end time.Time) map[string][4]float64 {
		out := map[string][4]float64{}
		for _, m := range all {
			signed := m.qty
			if m.vid == "Расход" {
				signed = -m.qty
			}
			v := out[m.nom]
			if m.period.Before(start) {
				v[0] += signed // начальный
			}
			if !m.period.Before(start) && !m.period.After(end) {
				if m.vid == "Приход" {
					v[1] += m.qty // приход
				} else {
					v[2] += m.qty // расход
				}
			}
			if !m.period.After(end) {
				v[3] += signed // конечный
			}
			out[m.nom] = v
		}
		// строка включается, если есть движение с period <= end (как on-the-fly)
		for nom, v := range out {
			has := false
			for _, m := range all {
				if m.nom == nom && !m.period.After(end) {
					has = true
					break
				}
			}
			if !has {
				delete(out, nom)
			}
			_ = v
		}
		return out
	}
	for i := 0; i < 25; i++ {
		s := base.AddDate(0, 0, rng.Intn(150))
		e := s.AddDate(0, 0, 1+rng.Intn(90))
		r, err := query.Compile(
			"ВЫБРАТЬ Товар, Колначальный, Колприход, Колрасход, Колконечный ИЗ РегистрНакопления.ОИ.ОстаткиИОбороты(&Н, &К)",
			query.CompileOpts{Registers: []*metadata.Register{reg}, Params: map[string]any{"Н": s, "К": e}, Dialect: storage.SQLiteDialect{}})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !strings.Contains(r.SQL, metadata.RegisterTotalsTableName(reg.Name)) {
			t.Fatalf("ОстаткиИОбороты должен читать %s, SQL: %s", metadata.RegisterTotalsTableName(reg.Name), r.SQL)
		}
		rows, err := db.Query(ctx, r.SQL, r.Args...)
		if err != nil {
			t.Fatalf("exec: %v\nSQL: %s", err, r.SQL)
		}
		got := map[string][4]float64{}
		for rows.Next() {
			var n string
			var a, b, c, dd float64
			if err := rows.Scan(&n, &a, &b, &c, &dd); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			got[n] = [4]float64{a, b, c, dd}
		}
		rows.Close()
		want := golden(s, e)
		if len(got) != len(want) {
			t.Fatalf("период %d [%s..%s]: строк итоги=%d эталон=%d\nfast=%v\ngold=%v",
				i, s.Format("01-02"), e.Format("01-02"), len(got), len(want), got, want)
		}
		for k, w := range want {
			g := got[k]
			for j := 0; j < 4; j++ {
				if math.Abs(g[j]-w[j]) > 1e-9 {
					t.Errorf("период %d [%s..%s] %s поле %d: итоги=%v эталон=%v",
						i, s.Format("01-02"), e.Format("01-02"), k, j, g[j], w[j])
				}
			}
		}
	}
}

func TestBalancesTurnoversTotals_MonthKeyUsesUTC(t *testing.T) {
	start := time.Date(2026, 2, 1, 0, 30, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	end := start.Add(time.Hour)
	reg := &metadata.Register{
		Name:       "ОИ",
		Dimensions: []metadata.Field{{Name: "Товар", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Кол", Type: metadata.FieldTypeNumber}},
		Totals:     metadata.RegisterTotals{Enabled: true},
	}
	r, err := query.Compile(
		"ВЫБРАТЬ Товар ИЗ РегистрНакопления.ОИ.ОстаткиИОбороты(&Н, &К)",
		query.CompileOpts{Registers: []*metadata.Register{reg}, Params: map[string]any{"Н": start, "К": end}, Dialect: storage.SQLiteDialect{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := start.UTC().Format("2006-01")
	if got := r.Args[0]; got != want {
		t.Fatalf("month key = %v, want UTC month %q", got, want)
	}
}

func TestBalancesTurnoversTotals_ReversedRangeFallsBack(t *testing.T) {
	reg := &metadata.Register{
		Name:       "ОИ",
		Dimensions: []metadata.Field{{Name: "Товар", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Кол", Type: metadata.FieldTypeNumber}},
		Totals:     metadata.RegisterTotals{Enabled: true},
	}
	start := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	end := start.Add(-time.Hour)
	r, err := query.Compile(
		"ВЫБРАТЬ Товар ИЗ РегистрНакопления.ОИ.ОстаткиИОбороты(&Н, &К)",
		query.CompileOpts{Registers: []*metadata.Register{reg}, Params: map[string]any{"Н": start, "К": end}, Dialect: storage.SQLiteDialect{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.SQL, metadata.RegisterTotalsTableName(reg.Name)) {
		t.Fatalf("reversed range must use the compatibility path, SQL: %s", r.SQL)
	}
}

// Регистр с атрибутами не использует итоги (их таблица атрибуты не хранит):
// Остатки() идёт обычным путём даже при totals.enabled — этап 1.
func TestBalancesTotals_AttributesFallBack(t *testing.T) {
	reg := &metadata.Register{
		Name:       "ОстаткиСАтрибутом",
		Dimensions: []metadata.Field{{Name: "Товар", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Кол", Type: metadata.FieldTypeNumber}},
		Attributes: []metadata.Field{{Name: "Комментарий", Type: metadata.FieldTypeString}},
		Totals:     metadata.RegisterTotals{Enabled: true},
	}
	r, err := query.Compile(
		"ВЫБРАТЬ Товар, КолОстаток ИЗ РегистрНакопления.ОстаткиСАтрибутом.Остатки()",
		query.CompileOpts{Registers: []*metadata.Register{reg}, Dialect: storage.SQLiteDialect{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.SQL, metadata.RegisterTotalsTableName(reg.Name)) {
		t.Errorf("регистр с атрибутами не должен читать итоги, SQL: %s", r.SQL)
	}
}
