package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func newExchangeDB(t *testing.T) (*DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureExchangeSchema(ctx); err != nil {
		t.Fatalf("EnsureExchangeSchema: %v", err)
	}
	return db, ctx
}

func TestExchangeThisNode(t *testing.T) {
	db, ctx := newExchangeDB(t)
	if got, _ := db.GetExchangeThisNode(ctx, "Обмен"); got != "" {
		t.Errorf("незаданный узел должен быть \"\", got %q", got)
	}
	if err := db.SaveExchangeThisNode(ctx, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetExchangeThisNode(ctx, "обмен"); got != "center" {
		t.Errorf("this_node = %q, want center (регистронезависимо по плану)", got)
	}
	// Разные планы — независимые настройки.
	if got, _ := db.GetExchangeThisNode(ctx, "Другой"); got != "" {
		t.Errorf("узел другого плана должен быть \"\", got %q", got)
	}
}

func TestExchangeRegisterAndPending(t *testing.T) {
	db, ctx := newExchangeDB(t)
	ch := ExchangeChange{Plan: "Обмен", ObjectType: "Номенклатура", ObjectID: "id-1", Version: 1, ChangedAt: 1000}
	for _, node := range []string{"center", "fil01"} {
		ch.NodeCode = node
		if err := db.RegisterExchangeChange(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	// По одной строке на узел.
	for _, node := range []string{"center", "fil01"} {
		got, err := db.PendingExchangeChanges(ctx, "Обмен", node)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Version != 1 {
			t.Fatalf("узел %s: %+v", node, got)
		}
	}
	// Повторная правка (upsert) поднимает версию и сбрасывает sent_no.
	sent := []ExchangeChange{{Plan: "Обмен", ObjectType: "Номенклатура", ObjectID: "id-1", NodeCode: "fil01"}}
	if err := db.MarkExchangeChangesSent(ctx, sent, 5); err != nil {
		t.Fatal(err)
	}
	ch.NodeCode = "fil01"
	ch.Version = 2
	ch.ChangedAt = 2000
	if err := db.RegisterExchangeChange(ctx, ch); err != nil {
		t.Fatal(err)
	}
	got, _ := db.PendingExchangeChanges(ctx, "Обмен", "fil01")
	if len(got) != 1 || got[0].Version != 2 || got[0].SentNo != 0 {
		t.Fatalf("после повторной правки: %+v (ожидали version=2, sent_no=0)", got)
	}
}

func TestExchangeMessageNoAndAck(t *testing.T) {
	db, ctx := newExchangeDB(t)
	// Счётчик сообщений монотонно растёт.
	for want := int64(1); want <= 3; want++ {
		got, err := db.NextExchangeMessageNo(ctx, "Обмен", "fil01")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("NextExchangeMessageNo = %d, want %d", got, want)
		}
	}

	// Две строки, выгружены в сообщении 4; ack до 4 снимает обе.
	changes := []ExchangeChange{
		{Plan: "Обмен", ObjectType: "Номенклатура", ObjectID: "a", NodeCode: "fil01", Version: 1, ChangedAt: 1},
		{Plan: "Обмен", ObjectType: "Номенклатура", ObjectID: "b", NodeCode: "fil01", Version: 1, ChangedAt: 2},
	}
	for _, ch := range changes {
		if err := db.RegisterExchangeChange(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.MarkExchangeChangesSent(ctx, changes, 4); err != nil {
		t.Fatal(err)
	}
	// ack на сообщение 3 (меньше 4) ничего не снимает.
	if n, _ := db.AckExchangeChanges(ctx, "Обмен", "fil01", 3); n != 0 {
		t.Errorf("ack<sent должен снять 0 строк, снял %d", n)
	}
	n, err := db.AckExchangeChanges(ctx, "Обмен", "fil01", 4)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("ack=4 должен снять 2 строки, снял %d", n)
	}
	if left, _ := db.PendingExchangeChanges(ctx, "Обмен", "fil01"); len(left) != 0 {
		t.Errorf("после ack очередь непуста: %+v", left)
	}
	peer, _ := db.GetExchangePeer(ctx, "Обмен", "fil01")
	if peer.AckNo != 4 {
		t.Errorf("ack_no = %d, want 4", peer.AckNo)
	}
}

func TestExchangeRecvNoMonotonic(t *testing.T) {
	db, ctx := newExchangeDB(t)
	if err := db.SetExchangeRecvNo(ctx, "Обмен", "center", 5); err != nil {
		t.Fatal(err)
	}
	// Меньший номер не откатывает счётчик.
	if err := db.SetExchangeRecvNo(ctx, "Обмен", "center", 3); err != nil {
		t.Fatal(err)
	}
	peer, _ := db.GetExchangePeer(ctx, "Обмен", "center")
	if peer.RecvNo != 5 {
		t.Errorf("recv_no = %d, want 5 (монотонно)", peer.RecvNo)
	}
}
