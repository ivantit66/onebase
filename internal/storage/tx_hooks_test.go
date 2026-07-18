package storage

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func openTxHooksTestDB(t *testing.T) (*DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "hooks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db, ctx
}

func TestTxHooks_CommitAndRollback(t *testing.T) {
	db, ctx := openTxHooksTestDB(t)

	var committed []string
	tx, txCtx, err := db.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	DeferUntilTxCommit(txCtx, func() { committed = append(committed, "commit") })
	DeferUntilTxRollback(txCtx, func() { committed = append(committed, "rollback") })
	if err := tx.Commit(txCtx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"commit"}; !reflect.DeepEqual(committed, want) {
		t.Fatalf("commit callbacks = %v, want %v", committed, want)
	}

	var rolledBack []string
	tx, txCtx, err = db.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	DeferUntilTxCommit(txCtx, func() { rolledBack = append(rolledBack, "commit") })
	DeferUntilTxRollback(txCtx, func() { rolledBack = append(rolledBack, "rollback") })
	if err := tx.Rollback(txCtx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"rollback"}; !reflect.DeepEqual(rolledBack, want) {
		t.Fatalf("rollback callbacks = %v, want %v", rolledBack, want)
	}
}

func TestTxHooks_SavepointScopes(t *testing.T) {
	db, ctx := openTxHooksTestDB(t)
	tx, txCtx, err := db.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(txCtx) //nolint:errcheck

	var events []string
	DeferUntilTxCommit(txCtx, func() { events = append(events, "outer-commit") })
	DeferUntilTxRollback(txCtx, func() { events = append(events, "outer-rollback") })

	PushTxHookScope(txCtx)
	DeferUntilTxCommit(txCtx, func() { events = append(events, "inner-commit-discarded") })
	DeferUntilTxRollback(txCtx, func() { events = append(events, "inner-rollback") })
	RollbackTxHookScope(txCtx)
	if want := []string{"inner-rollback"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("savepoint rollback callbacks = %v, want %v", events, want)
	}

	PushTxHookScope(txCtx)
	DeferUntilTxCommit(txCtx, func() { events = append(events, "released-commit") })
	DeferUntilTxRollback(txCtx, func() { events = append(events, "released-rollback") })
	CommitTxHookScope(txCtx)
	if err := tx.Commit(txCtx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"inner-rollback", "outer-commit", "released-commit"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("savepoint commit callbacks = %v, want %v", events, want)
	}
}
