package ui

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// coerceParams не должен превращать UUID-значение ссылочного параметра в
// число: fmt.Sscanf разбирал префикс "123e4567" из UUID как float → в SQL
// получалось "uuid = numeric" (SQLSTATE 42883).
func TestCoerceParams_UUIDStaysString(t *testing.T) {
	uid := "123e4567-e89b-12d3-a456-426614174000"
	params := map[string]any{
		"Ref":  uid,
		"Num":  "42.5",
		"Int":  "100",
		"Date": "15.03.2026",
		"Text": "Привет",
	}
	coerceParams(params)

	if params["Ref"] != uid {
		t.Errorf("UUID-параметр стал %#v — должен остаться строкой", params["Ref"])
	}
	if params["Num"] != 42.5 {
		t.Errorf("Num = %#v, ожидалось 42.5", params["Num"])
	}
	if params["Int"] != float64(100) {
		t.Errorf("Int = %#v, ожидалось 100", params["Int"])
	}
	if _, ok := params["Date"].(time.Time); !ok {
		t.Errorf("Date = %#v, ожидался time.Time", params["Date"])
	}
	if params["Text"] != "Привет" {
		t.Errorf("Text = %#v, ожидалась исходная строка", params["Text"])
	}
}

// queryConsoleAnalyze определял тип параметра по колонке перед плейсхолдером.
// Плейсхолдер искался жёстко как «$N» (PostgreSQL); на SQLite (файловая база)
// плейсхолдер — «?», поэтому детект всегда проваливался в fallback по имени.
// Здесь имя параметра (&П) НЕ совпадает с сущностью — тип обязан определиться
// именно по колонке.
func TestQueryConsoleAnalyze_SQLiteRefByColumn(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	nom := &metadata.Entity{
		Name: "Номенклатура", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	reg := &metadata.Register{
		Name:       "ОстаткиТоваров",
		Dimensions: []metadata.Field{{Name: "Номенклатура", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{nom}); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateRegisters(ctx, []*metadata.Register{reg}); err != nil {
		t.Fatal(err)
	}

	registry := runtime.NewRegistry()
	registry.Load([]*metadata.Entity{nom}, nil, []*metadata.Register{reg}, nil, nil, nil, nil)

	s := &Server{store: db, reg: registry}

	body := `{"query":"ВЫБРАТЬ Количество ИЗ РегистрНакопления.ОстаткиТоваров ГДЕ Номенклатура = &П"}`
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.queryConsoleAnalyze(w, r)

	if w.Code != 200 {
		t.Fatalf("код %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ParamTypes map[string]string `json:"paramTypes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v — тело: %s", err, w.Body.String())
	}
	if got := resp.ParamTypes["П"]; got != "reference:Номенклатура" {
		t.Errorf("параметр &П: тип %q, ожидался reference:Номенклатура (детект по колонке на SQLite)", got)
	}
}
