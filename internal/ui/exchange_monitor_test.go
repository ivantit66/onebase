package ui

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

func TestExchangeMonitorRenders(t *testing.T) {
	db, reg, ctx, ent := newExchangeBaseDB(t)
	_ = db.SaveExchangeThisNode(ctx, "Обмен", "center")
	id := uuid.New()
	if err := db.Upsert(ctx, "Товар", id, map[string]any{"Наименование": "X"}, ent); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterOnSave(ctx, db, reg.ExchangePlans(), ent, id, false); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: db, reg: reg}

	w := httptest.NewRecorder()
	s.exchangeMonitor(w, httptest.NewRequest("GET", "/ui/admin/exchange", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Обмен", "center", "fil01", "← этот"} {
		if !strings.Contains(body, want) {
			t.Errorf("монитор не содержит %q", want)
		}
	}
}

func TestExchangeMonitorSync(t *testing.T) {
	ctx := context.Background()

	// Партнёр B (fil01): сервер с эндпоинтами обмена и токеном.
	bDB, bReg, _, ent := newExchangeBaseDB(t)
	_ = bDB.SaveExchangeThisNode(ctx, "Обмен", "fil01")
	_ = bDB.SaveExchangeToken(ctx, "Обмен", "T")
	sB := &Server{store: bDB, reg: bReg}
	rB := chi.NewRouter()
	sB.MountExchange(rB)
	peer := httptest.NewServer(rB)
	t.Cleanup(peer.Close)

	// База A (center): план с адресом fil01 = peer.URL, токен, изменение в очереди.
	aDB, _, _, aEnt := newExchangeBaseDB(t)
	_ = aDB.SaveExchangeThisNode(ctx, "Обмен", "center")
	_ = aDB.SaveExchangeToken(ctx, "Обмен", "T")
	planA := exchPlan()
	planA.Nodes[1].URL = peer.URL // fil01
	regA := runtime.NewRegistry()
	regA.Load(runtime.LoadOptions{Entities: []*metadata.Entity{aEnt}})
	regA.LoadExchangePlans([]*metadata.ExchangePlan{planA})
	sA := &Server{store: aDB, reg: regA}

	id := uuid.New()
	if err := aDB.Upsert(ctx, "Товар", id, map[string]any{"Наименование": "Синх"}, aEnt); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterOnSave(ctx, aDB, regA.ExchangePlans(), aEnt, id, false); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"plan": {"Обмен"}, "node": {"fil01"}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/ui/admin/exchange/sync", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sA.exchangeMonitorSync(w, r)

	if w.Code != 302 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); strings.Contains(loc, "err=") {
		t.Fatalf("sync вернул ошибку: %s", loc)
	}
	// Объект доставлен партнёру.
	if row, err := bDB.GetByID(ctx, "Товар", id, ent); err != nil || row["Наименование"] != "Синх" {
		t.Fatalf("объект не синхронизирован партнёру: row=%v err=%v", row, err)
	}
	// Очередь A→fil01 дренирована подтверждением из обратного пакета.
	if p, _ := aDB.PendingExchangeChanges(ctx, "Обмен", "fil01"); len(p) != 0 {
		t.Errorf("очередь A→fil01 не дренирована после sync: %+v", p)
	}
}
