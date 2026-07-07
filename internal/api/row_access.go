package api

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/access"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
	"github.com/ivantit66/onebase/internal/storage"
)

func rowDecision(ctx context.Context, entity *metadata.Entity, op string) (access.Decision, error) {
	if entity == nil {
		return access.Decision{}, nil
	}
	return access.Decide(auth.UserFromContext(ctx), string(entity.Kind), entity.Name, op, entity)
}

func applyRowFilter(ctx context.Context, entity *metadata.Entity, op string, params storage.ListParams) (storage.ListParams, error) {
	dec, err := rowDecision(ctx, entity, op)
	if err != nil {
		return params, err
	}
	if !dec.Allowed {
		return params, fmt.Errorf("forbidden")
	}
	if !dec.Unrestricted {
		params.RowFilter = dec.Predicate
	}
	return params, nil
}

func (h *handler) rowAllowed(ctx context.Context, entity *metadata.Entity, op string, row map[string]any) bool {
	dec, err := rowDecision(ctx, entity, op)
	if err != nil || !dec.Allowed {
		return false
	}
	return dec.Unrestricted || storage.MatchPredicate(row, dec.Predicate)
}

func (h *handler) rowAllowedID(ctx context.Context, entity *metadata.Entity, op string, id uuid.UUID) bool {
	dec, err := rowDecision(ctx, entity, op)
	if err != nil || !dec.Allowed {
		return false
	}
	if dec.Unrestricted {
		return true
	}
	row, err := h.store.GetByID(ctx, entity.Name, id, entity)
	return err == nil && storage.MatchPredicate(row, dec.Predicate)
}

func (h *handler) rowAllowedUpdate(ctx context.Context, entity *metadata.Entity, op string, id uuid.UUID, fields map[string]any) bool {
	dec, err := rowDecision(ctx, entity, op)
	if err != nil || !dec.Allowed {
		return false
	}
	if dec.Unrestricted {
		return true
	}
	row, err := h.store.GetByID(ctx, entity.Name, id, entity)
	if err != nil || !storage.MatchPredicate(row, dec.Predicate) {
		return false
	}
	return storage.MatchPredicate(storage.MergeRowFields(row, fields), dec.Predicate)
}

func (h *handler) compileQueryWithRowAccess(ctx context.Context, text string, params map[string]any) (query.Result, error) {
	rowFilters, err := access.QueryRowFilters(
		auth.UserFromContext(ctx),
		h.reg.Entities(),
		h.reg.Registers(),
		h.reg.InfoRegisters(),
		h.reg.AccountRegisters(),
	)
	if err != nil {
		return query.Result{}, err
	}
	return query.Compile(text, query.CompileOpts{
		Entities:    h.reg.Entities(),
		Params:      params,
		Registers:   h.reg.Registers(),
		InfoRegs:    h.reg.InfoRegisters(),
		AccountRegs: h.reg.AccountRegisters(),
		RowFilters:  rowFilters,
		Dialect:     h.store.Dialect(),
	})
}

func (h *handler) deniedQuerySource(ctx context.Context, sources []query.SourceRef) string {
	return access.DeniedReadSource(auth.UserFromContext(ctx), sources)
}
