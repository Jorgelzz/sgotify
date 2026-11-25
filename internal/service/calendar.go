// ORDEM DE CRIAÇÃO: 10º (Etapa 10 — calendário) · junto com handlers/calendar.go
//
// POR QUE EXISTE:
//
//	É o "cérebro" do produto: transforma o histórico em "qual foi a música do
//	dia, para cada dia do mês". Precisa do storage (Etapa 8) já alimentado
//	pelo coletor (Etapa 9).
//
// ONDE FICA A REGRA: em Go, e não em SQL (decisão do projeto): "mais plays,
// empate pela mais recente, no fuso local" é fácil de testar sem banco quando é
// uma função pura (topPerDay). O banco só guarda e filtra por período.
package service

import (
	"context"
	"sort"
	"time"

	"github.com/jorgelzz/sgotify/internal/storage"
)

// DayTop é a música campeã de um dia.
// Só entram na lista dias que TIVERAM plays; quem desenha o calendário (o
// front) preenche os dias vazios.
type DayTop struct {
	Date     string `json:"date"` // "2026-11-24", no fuso local
	TrackID  string `json:"-"`
	Name     string `json:"track"`
	Artist   string `json:"artist"`
	ImageURL string `json:"image"`
	Plays    int    `json:"plays"`
}

// CalendarService monta o calendário de um mês.
type CalendarService struct {
	store storage.Store
	loc   *time.Location // fuso que define "em que dia" uma música foi ouvida
}

// NewCalendarService cria o service. loc vem da config (TIMEZONE).
func NewCalendarService(store storage.Store, loc *time.Location) *CalendarService {
	return &CalendarService{store: store, loc: loc}
}

// Month devolve a música do dia para cada dia do mês que teve plays.
//
// O mês é delimitado NO FUSO LOCAL: "novembro" começa à 00:00 de 1º/nov em São
// Paulo (03:00 UTC) e vai até a 00:00 de 1º/dez. Se delimitássemos em UTC, um
// play das 22h do dia 30 (01h UTC do dia 1º) seria cobrado do mês errado.
func (s *CalendarService) Month(ctx context.Context, year int, month time.Month) ([]DayTop, error) {
	start := time.Date(year, month, 1, 0, 0, 0, 0, s.loc)
	end := start.AddDate(0, 1, 0) // 1º dia do mês seguinte (fim exclusivo)

	plays, err := s.store.Between(ctx, start, end)
	if err != nil {
		return nil, err
	}
	return topPerDay(plays, s.loc), nil
}

// topPerDay é a regra PURA do calendário.
//
//  1. converte o instante (UTC) para o fuso local ANTES de pegar a data: uma
//     música ouvida às 22h em São Paulo é 01h do dia SEGUINTE em UTC, e
//     agrupar em UTC faria o calendário errar;
//  2. agrupa por dia e, dentro do dia, por música, contando plays;
//  3. vence a música com mais plays; EMPATE: a tocada por último no dia; se
//     ainda empatar, a de menor TrackID (só para o resultado ser sempre o mesmo);
//  4. devolve os dias em ordem cronológica.
func topPerDay(plays []storage.Play, loc *time.Location) []DayTop {
	type agg struct {
		DayTop
		last time.Time // última vez que tocou NESTE dia
	}
	// dia -> música -> contagem
	days := make(map[string]map[string]*agg)

	for _, p := range plays {
		day := p.PlayedAt.In(loc).Format("2006-01-02")
		if days[day] == nil {
			days[day] = make(map[string]*agg)
		}
		a, ok := days[day][p.TrackID]
		if !ok {
			a = &agg{DayTop: DayTop{
				Date: day, TrackID: p.TrackID, Name: p.TrackName,
				Artist: p.Artist, ImageURL: p.AlbumImageURL,
			}}
			days[day][p.TrackID] = a
		}
		a.Plays++
		if p.PlayedAt.After(a.last) {
			a.last = p.PlayedAt
		}
	}

	result := make([]DayTop, 0, len(days))
	for _, tracks := range days {
		var winner *agg
		for _, a := range tracks {
			if winner == nil || beats(a.Plays, a.last, a.TrackID, winner.Plays, winner.last, winner.TrackID) {
				winner = a
			}
		}
		result = append(result, winner.DayTop)
	}
	// "2026-11-02" < "2026-11-10": no formato AAAA-MM-DD, ordem alfabética é
	// ordem cronológica.
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}

// beats diz se a música A vence a B no mesmo dia: mais plays; empate => a mais
// recente; empate => o menor TrackID (desempate arbitrário, mas estável).
func beats(aPlays int, aLast time.Time, aID string, bPlays int, bLast time.Time, bID string) bool {
	if aPlays != bPlays {
		return aPlays > bPlays
	}
	if !aLast.Equal(bLast) {
		return aLast.After(bLast)
	}
	return aID < bID
}
