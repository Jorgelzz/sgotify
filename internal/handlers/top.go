// ORDEM DE CRIAÇÃO: 7º (Etapa 7 — mais ouvidos) · junto com service/top.go
//
// POR QUE EXISTE:
//
//	Expõe o ranking de "mais ouvidos da semana" para o front em
//	GET /api/top-week.
package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jorgelzz/sgotify/internal/service"
)

const (
	defaultTopLimit = 10
	maxTopLimit     = 50
)

// weekRanker é o que este handler precisa do service.
type weekRanker interface {
	Week(ctx context.Context, limit int) ([]service.TrackCount, error)
}

// topResponse é o corpo de /api/top-week.
type topResponse struct {
	Tracks []service.TrackCount `json:"tracks"`
}

// TopWeek responde GET /api/top-week?limit=10.
//
// Validação da ENTRADA é papel do handler: limit é opcional (padrão 10) e, se
// vier, tem que ser um inteiro de 1 a 50; senão, 400. A regra "o que é a
// semana" é do service.
func TopWeek(svc weekRanker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := defaultTopLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > maxTopLimit {
				writeError(w, http.StatusBadRequest, "limit must be an integer between 1 and 50")
				return
			}
			limit = n
		}

		tracks, err := svc.Week(r.Context(), limit)
		if err != nil {
			internalError(w, err) // falha do banco, não do Spotify: 500
			return
		}
		if tracks == nil {
			// Um slice nil viraria "null" no JSON; o front espera uma lista.
			tracks = []service.TrackCount{}
		}
		writeJSON(w, http.StatusOK, topResponse{Tracks: tracks})
	}
}
