package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/storage"
)

func writeExchangeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("catalogs/Товар.yaml", `name: Товар
title: Товар
fields:
  - name: Наименование
    type: string
  - name: Цена
    type: number
`)
	mustWrite("exchange/Обмен.yaml", `name: Обмен
title: Обмен
conflict: by_time
content:
  - Справочник.Товар
nodes:
  - { code: center, name: Центр }
  - { code: fil01, name: Филиал }
`)
	return dir
}

func exCmd(sqlite, projectDir string, flags map[string]string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	addBaseFlags(cmd)
	cmd.Flags().String("plan", "", "")
	cmd.Flags().String("node", "", "")
	cmd.Flags().String("to", "", "")
	cmd.Flags().String("out", "", "")
	cmd.Flags().String("in", "", "")
	_ = cmd.Flags().Set("sqlite", sqlite)
	_ = cmd.Flags().Set("project", projectDir)
	for k, v := range flags {
		_ = cmd.Flags().Set(k, v)
	}
	return cmd
}

func TestExchangeCLIRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := writeExchangeProject(t)
	proj, err := project.Load(dir)
	if err != nil {
		t.Fatalf("project.Load: %v", err)
	}
	defer proj.Close()
	var entTovar *metadata.Entity
	for _, e := range proj.Entities {
		if e.Name == "Товар" {
			entTovar = e
		}
	}
	if entTovar == nil {
		t.Fatal("сущность Товар не загрузилась")
	}

	dbA := filepath.Join(t.TempDir(), "a.db")
	dbB := filepath.Join(t.TempDir(), "b.db")
	for _, p := range []string{dbA, dbB} {
		db, err := storage.ConnectSQLite(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(ctx, proj.Entities); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}

	// init: A=center, B=fil01.
	if err := runExchangeInit(exCmd(dbA, dir, map[string]string{"plan": "Обмен", "node": "center"}), nil); err != nil {
		t.Fatalf("init A: %v", err)
	}
	if err := runExchangeInit(exCmd(dbB, dir, map[string]string{"plan": "Обмен", "node": "fil01"}), nil); err != nil {
		t.Fatalf("init B: %v", err)
	}

	// Сеем объект в A и регистрируем изменение (this-node=center → очередь fil01).
	id := uuid.New()
	da, err := storage.ConnectSQLite(ctx, dbA)
	if err != nil {
		t.Fatal(err)
	}
	if err := da.Upsert(ctx, "Товар", id, map[string]any{"Наименование": "Гвоздь", "Цена": "12.50"}, entTovar); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterOnSave(ctx, da, proj.ExchangePlans, entTovar, id, false); err != nil {
		t.Fatal(err)
	}
	da.Close()

	// dump A → файл, load B ← файл.
	out := filepath.Join(t.TempDir(), "pkg.obx")
	if err := runExchangeDump(exCmd(dbA, dir, map[string]string{"plan": "Обмен", "to": "fil01", "out": out}), nil); err != nil {
		t.Fatalf("dump: %v", err)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Fatalf("пакет не создан: %v", err)
	}
	if err := runExchangeLoad(exCmd(dbB, dir, map[string]string{"in": out}), nil); err != nil {
		t.Fatalf("load: %v", err)
	}

	// B получила объект.
	assertTovar := func(wantName string, wantVer int64) {
		db, err := storage.ConnectSQLite(ctx, dbB)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		row, err := db.GetByID(ctx, "Товар", id, entTovar)
		if err != nil {
			t.Fatalf("объект не найден в B: %v", err)
		}
		if row["Наименование"] != wantName {
			t.Errorf("Наименование = %v, want %q", row["Наименование"], wantName)
		}
		if v, _ := db.EntityVersion(ctx, "Товар", id); v != wantVer {
			t.Errorf("_version = %d, want %d", v, wantVer)
		}
	}
	assertTovar("Гвоздь", 1)

	// Повторная загрузка того же пакета идемпотентна — версия не растёт.
	if err := runExchangeLoad(exCmd(dbB, dir, map[string]string{"in": out}), nil); err != nil {
		t.Fatalf("повторный load: %v", err)
	}
	assertTovar("Гвоздь", 1)

	// status выполняется без ошибок на обеих базах (smoke).
	if err := runExchangeStatus(exCmd(dbA, dir, map[string]string{"plan": "Обмен"}), nil); err != nil {
		t.Fatalf("status A: %v", err)
	}
	if err := runExchangeStatus(exCmd(dbB, dir, map[string]string{"plan": "Обмен"}), nil); err != nil {
		t.Fatalf("status B: %v", err)
	}
}
