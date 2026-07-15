package ui

// Приёмные HTTP-эндпоинты онлайн-обмена (план 86, фаза 2). Монтируются ВНЕ
// session-middleware (как HTTP-сервисы): базы аутентифицируются общим Bearer-
// токеном плана (_settings exchange.token.<план>), не cookie.
//
//	POST /exchange/{plan}/push       — принять пакет и загрузить в эту базу;
//	GET  /exchange/{plan}/pull?to=X  — собрать и отдать пакет для узла X.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
)

const maxExchangeBody = 64 << 20 // 64 MiB

// exchangeApplyOptions собирает параметры загрузки пакета для Go-путей приёма
// (онлайн push и монитор): обработчик правила конфликта hook и перепроведение
// проведённых документов (repost) поверх entityservice. entitySvc есть у сервера,
// поэтому перепроведение доступно на онлайн-приёмнике (в отличие от headless CLI).
func (s *Server) exchangeApplyOptions() exchange.ApplyOptions {
	return exchange.ApplyOptions{
		Hook: NewExchangeHook(s.store, s.reg, s.interp),
		Repost: func(ctx context.Context, entityType string, id uuid.UUID) error {
			return s.entitySvc.Repost(ctx, entityType, id)
		},
	}
}

// MountExchange монтирует эндпоинты онлайн-обмена на верхнеуровневый роутер.
func (s *Server) MountExchange(r chi.Router) {
	r.Post("/exchange/{plan}/push", s.exchangePush)
	r.Get("/exchange/{plan}/pull", s.exchangePull)
}

// exchangeAuthedPlan резолвит план из пути и проверяет Bearer-токен. При любой
// проблеме пишет ответ и возвращает nil.
func (s *Server) exchangeAuthedPlan(w http.ResponseWriter, r *http.Request) *metadata.ExchangePlan {
	plan := s.reg.GetExchangePlan(chi.URLParam(r, "plan"))
	if plan == nil {
		http.Error(w, "план обмена не найден", http.StatusNotFound)
		return nil
	}
	token, _ := s.store.GetExchangeToken(r.Context(), plan.Name)
	if strings.TrimSpace(token) == "" {
		http.Error(w, "онлайн-обмен по плану не настроен (нет токена)", http.StatusForbidden)
		return nil
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		http.Error(w, "неверный токен обмена", http.StatusUnauthorized)
		return nil
	}
	return plan
}

func (s *Server) exchangePush(w http.ResponseWriter, r *http.Request) {
	plan := s.exchangeAuthedPlan(w, r)
	if plan == nil {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxExchangeBody))
	if err != nil {
		http.Error(w, "тело пакета: "+err.Error(), http.StatusBadRequest)
		return
	}
	res, err := exchange.ApplyPackage(r.Context(), s.store, s.reg, plan, body, s.exchangeApplyOptions())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) exchangePull(w http.ResponseWriter, r *http.Request) {
	plan := s.exchangeAuthedPlan(w, r)
	if plan == nil {
		return
	}
	toNode := strings.TrimSpace(r.URL.Query().Get("to"))
	if toNode == "" || plan.Node(toNode) == nil {
		http.Error(w, "неизвестный узел-получатель (параметр to)", http.StatusBadRequest)
		return
	}
	data, err := exchange.BuildPackage(r.Context(), s.store, s.reg, plan, toNode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
