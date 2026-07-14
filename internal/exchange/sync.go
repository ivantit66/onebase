package exchange

// Онлайн-цикл обмена с одним узлом (план 86, фаза 2): отправить свои изменения
// партнёру и забрать его изменения для себя. Общая логика для CLI (exchange sync)
// и UI-монитора.

import (
	"context"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// SyncWithNode выполняет полный онлайн-обмен с узлом peer:
//  1. push — собирает пакет своих изменений для peer и отправляет на peerURL;
//  2. pull — забирает пакет, адресованный thisNode, и загружает его (пакет несёт
//     подтверждение — очередь к peer дренируется).
//
// Возвращает итог применения на стороне партнёра (push) и у нас (load).
func SyncWithNode(ctx context.Context, store *storage.DB, resolver EntityResolver, plan *metadata.ExchangePlan, thisNode, peerCode, peerURL, token string, opts ApplyOptions) (push, load LoadResult, err error) {
	out, err := BuildPackage(ctx, store, resolver, plan, peerCode)
	if err != nil {
		return push, load, err
	}
	push, err = PushPackage(ctx, peerURL, plan.Name, token, out)
	if err != nil {
		return push, load, err
	}
	pulled, err := PullPackage(ctx, peerURL, plan.Name, token, thisNode)
	if err != nil {
		return push, load, err
	}
	load, err = ApplyPackage(ctx, store, resolver, plan, pulled, opts)
	return push, load, err
}
