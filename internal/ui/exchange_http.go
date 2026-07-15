package ui

// Приёмные HTTP-эндпоинты онлайн-обмена (план 86, фаза 2). Монтируются ВНЕ
// session-middleware (как HTTP-сервисы): базы аутентифицируются общим Bearer-
// токеном плана (_settings exchange.token.<план>), не cookie.
//
//	POST /exchange/{plan}/push       — принять пакет и загрузить в эту базу;
//	GET  /exchange/{plan}/pull?to=X  — собрать и отдать пакет для узла X.

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
)

const maxExchangeBody = 64 << 20 // 64 MiB

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
	token, err := s.store.GetExchangeToken(r.Context(), plan.Name)
	if err != nil {
		http.Error(w, "настройки обмена недоступны", http.StatusInternalServerError)
		return nil
	}
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

func (s *Server) exchangePair(w http.ResponseWriter, r *http.Request, plan *metadata.ExchangePlan) (thisNode, peerNode string, ok bool) {
	if len(plan.Nodes) != 2 {
		http.Error(w, "онлайн-обмен поддерживает только планы из двух узлов", http.StatusConflict)
		return "", "", false
	}
	thisNode, err := s.store.GetExchangeThisNode(r.Context(), plan.Name)
	if err != nil {
		http.Error(w, "не удалось прочитать текущий узел", http.StatusInternalServerError)
		return "", "", false
	}
	thisDef := plan.Node(thisNode)
	if thisDef == nil {
		http.Error(w, "текущий узел базы не настроен", http.StatusConflict)
		return "", "", false
	}
	thisNode = thisDef.Code
	for _, node := range plan.Nodes {
		if !strings.EqualFold(node.Code, thisNode) {
			return thisNode, node.Code, true
		}
	}
	http.Error(w, "партнёр плана не найден", http.StatusConflict)
	return "", "", false
}

func (s *Server) exchangePush(w http.ResponseWriter, r *http.Request) {
	plan := s.exchangeAuthedPlan(w, r)
	if plan == nil {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxExchangeBody))
	if err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "тело пакета: "+err.Error(), status)
		return
	}
	thisNode, peerNode, ok := s.exchangePair(w, r, plan)
	if !ok {
		return
	}
	pkg, err := exchange.ParsePackage(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(pkg.FromNode, peerNode) || !strings.EqualFold(pkg.ToNode, thisNode) {
		http.Error(w, "пакет не принадлежит настроенной паре узлов", http.StatusForbidden)
		return
	}
	res, err := exchange.ApplyPackage(r.Context(), s.store, s.reg, plan, body,
		exchange.ApplyOptions{Hook: NewExchangeHook(s.store, s.reg, s.interp)})
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
	_, peerNode, ok := s.exchangePair(w, r, plan)
	if !ok {
		return
	}
	toNode := strings.TrimSpace(r.URL.Query().Get("to"))
	if toNode == "" || !strings.EqualFold(toNode, peerNode) {
		http.Error(w, "пакет можно получить только для узла-партнёра", http.StatusForbidden)
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
