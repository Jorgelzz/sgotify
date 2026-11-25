// ORDEM DE CRIAÇÃO: 10º (Etapa 10 — calendário) · junto com service/calendar.go
//
// POR QUE EXISTE:
//
//	Expõe service.CalendarService.Month como JSON em
//	GET /api/calendar?month=2026-11.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jorgelzz/sgotify/internal/service"
)

// monthProvider é o que este handler precisa do service.
type monthProvider interface {
	Month(ctx context.Context, year int, month time.Month) ([]service.DayTop, error)
}

// calendarResponse é o corpo de /api/calendar.
type calendarResponse struct {
	Month string           `json:"month"`
	Days  []service.DayTop `json:"days"`
}

// Calendar responde GET /api/calendar?month=YYYY-MM.
//
// month é opcional: sem ele, vale o mês ATUAL no fuso configurado. Formato
// inválido => 400. Dias sem nenhum play simplesmente NÃO aparecem em "days";
// quem desenha o calendário (o front) preenche os dias vazios.
func Calendar(svc monthProvider, loc *time.Location) http.HandlerFunc {
	return calendarHandler(svc, loc, time.Now)
}

// calendarHandler é o Calendar com o relógio injetado, para o teste fixar a
// "data de hoje" e verificar o mês padrão sem depender do dia em que roda.
func calendarHandler(svc monthProvider, loc *time.Location, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var first time.Time // o primeiro dia do mês pedido

		if raw := r.URL.Query().Get("month"); raw != "" {
			// "2006-01" é o formato de referência do Go para AAAA-MM. O
			// ParseInLocation já recusa meses inexistentes (ex.: 2026-13).
			parsed, err := time.ParseInLocation("2006-01", raw, loc)
			if err != nil {
				writeError(w, http.StatusBadRequest, "month must be in the format YYYY-MM")
				return
			}
			first = parsed
		} else {
			n := now().In(loc)
			first = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
		}

		days, err := svc.Month(r.Context(), first.Year(), first.Month())
		if err != nil {
			internalError(w, err)
			return
		}
		if days == nil {
			// Mês sem plays: "days":[] e não "days":null.
			days = []service.DayTop{}
		}
		writeJSON(w, http.StatusOK, calendarResponse{
			Month: first.Format("2006-01"),
			Days:  days,
		})
	}
}
