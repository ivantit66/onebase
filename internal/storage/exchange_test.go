package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
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

func TestExchangeReadsDoNotHideStorageErrors(t *testing.T) {
	db, ctx := newExchangeDB(t)
	db.Close()
	if _, err := db.GetExchangeThisNode(ctx, "Обмен"); err == nil {
		t.Fatal("ошибка закрытой БД не должна выглядеть как незаданный this_node")
	}
	if _, err := db.GetExchangeToken(ctx, "Обмен"); err == nil {
		t.Fatal("ошибка закрытой БД не должна выглядеть как отсутствующий token")
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
	sent, err := db.PendingExchangeChanges(ctx, "Обмен", "fil01")
	if err != nil || len(sent) != 1 {
		t.Fatalf("pending перед отправкой: %+v, %v", sent, err)
	}
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

func TestExchangeStatusHelpers(t *testing.T) {
	db, ctx := newExchangeDB(t)
	regs := []ExchangeChange{
		{Plan: "Обмен", ObjectType: "Товар", ObjectID: "a", NodeCode: "fil01", Version: 1, ChangedAt: 1},
		{Plan: "Обмен", ObjectType: "Товар", ObjectID: "b", NodeCode: "fil01", Version: 1, ChangedAt: 2},
		{Plan: "Обмен", ObjectType: "Товар", ObjectID: "a", NodeCode: "center", Version: 1, ChangedAt: 1},
	}
	for _, ch := range regs {
		if err := db.RegisterExchangeChange(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := db.ExchangePendingCounts(ctx, "Обмен")
	if err != nil {
		t.Fatal(err)
	}
	if counts["fil01"] != 2 || counts["center"] != 1 {
		t.Errorf("counts = %+v, want fil01=2 center=1", counts)
	}

	if err := db.SetExchangeRecvNo(ctx, "Обмен", "center", 5); err != nil {
		t.Fatal(err)
	}
	peers, err := db.ExchangePeers(ctx, "Обмен")
	if err != nil {
		t.Fatal(err)
	}
	var centerRecv int64 = -1
	for _, p := range peers {
		if p.NodeCode == "center" {
			centerRecv = p.RecvNo
		}
	}
	if centerRecv != 5 {
		t.Errorf("center recv_no = %d, want 5 (peers=%+v)", centerRecv, peers)
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

func TestMarkExchangeChangesSentDoesNotHideNewerChange(t *testing.T) {
	db, ctx := newExchangeDB(t)
	old := ExchangeChange{
		Plan: "Обмен", ObjectType: "Товар", ObjectID: uuid.NewString(), NodeCode: "fil01",
		Version: 1, ChangedAt: 1000,
	}
	if err := db.RegisterExchangeChange(ctx, old); err != nil {
		t.Fatal(err)
	}
	newer := old
	newer.Version = 2
	newer.ChangedAt = 2000
	if err := db.RegisterExchangeChange(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkExchangeChangesSent(ctx, []ExchangeChange{old}, 7); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingExchangeChanges(ctx, "Обмен", "fil01")
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending: %+v, %v", pending, err)
	}
	if pending[0].Version != 2 || pending[0].SentNo != 0 {
		t.Fatalf("новая правка была ошибочно помечена старым пакетом: %+v", pending[0])
	}
}
