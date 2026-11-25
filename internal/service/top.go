// ORDEM DE CRIAÇÃO: 7º (Etapa 7 — mais ouvidos) · junto com handlers/top.go
//
// POR QUE EXISTE:
//
//	Responde "quais foram minhas músicas mais ouvidas da semana?". A rota
//	top/tracks do Spotify NÃO serve (a menor janela dela é de ~4 semanas e não
//	informa quantas vezes ouvi), então a resposta é calculada a partir do NOSSO
//	histórico (tabela plays), contando os plays dos últimos 7 dias.
package service

import (
	"context"
	"sort"
	"time"

	"github.com/jorgelzz/sgotify/internal/storage"
)

// weekDays é o tamanho da janela do "top da semana".
const weekDays = 7

// TrackCount é uma música com quantas vezes foi ouvida no período.
// TrackID não vai para o JSON: a resposta pública é só o que o front exibe.
type TrackCount struct {
	TrackID  string `json:"-"`
	Name     string `json:"track"`
	Artist   string `json:"artist"`
	ImageURL string `json:"image"`
	Plays    int    `json:"plays"`
}

// TopService calcula rankings a partir do histórico.
type TopService struct {
	store storage.Store
	// now é um relógio INJETADO: nos testes fixamos a "data de hoje", e o teste
	// de "últimos 7 dias" deixa de depender do dia em que roda.
	now func() time.Time
}

// NewTopService cria o service usando o relógio real.
func NewTopService(store storage.Store) *TopService {
	return &TopService{store: store, now: time.Now}
}

// Week devolve as músicas mais ouvidas nos últimos 7 dias, da mais para a menos
// ouvida, limitadas a "limit". Nunca devolve nil (sem plays => lista vazia), para
// o JSON sair como [] e não null.
func (s *TopService) Week(ctx context.Context, limit int) ([]TrackCount, error) {
	to := s.now()
	from := to.AddDate(0, 0, -weekDays)

	plays, err := s.store.Between(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return topTracks(plays, limit), nil
}

// topTracks é a regra PURA do ranking (sem banco, sem relógio): conta os plays
// por música e ordena.
//
// Ordem: mais plays primeiro. EMPATE: quem foi tocada por último vem antes;
// persistindo o empate, ordem alfabética do nome (só para o resultado ser
// sempre o mesmo, e não variar a cada execução).
//
// Os dados de exibição (nome, artista, capa) vêm do play MAIS RECENTE da
// música, caso o Spotify tenha mudado algum deles.
func topTracks(plays []storage.Play, limit int) []TrackCount {
	type agg struct {
		TrackCount
		last time.Time // quando foi tocada pela última vez
	}
	byTrack := make(map[string]*agg)

	for _, p := range plays {
		a, ok := byTrack[p.TrackID]
		if !ok {
			a = &agg{}
			byTrack[p.TrackID] = a
		}
		a.Plays++
		if p.PlayedAt.After(a.last) {
			a.last = p.PlayedAt
			a.TrackID, a.Name, a.Artist, a.ImageURL = p.TrackID, p.TrackName, p.Artist, p.AlbumImageURL
		}
	}

	ranked := make([]*agg, 0, len(byTrack))
	for _, a := range byTrack {
		ranked = append(ranked, a)
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.Plays != b.Plays {
			return a.Plays > b.Plays
		}
		if !a.last.Equal(b.last) {
			return a.last.After(b.last)
		}
		return a.Name < b.Name
	})

	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	result := make([]TrackCount, 0, len(ranked))
	for _, a := range ranked {
		result = append(result, a.TrackCount)
	}
	return result
}
