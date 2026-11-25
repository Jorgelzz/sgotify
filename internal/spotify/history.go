// ORDEM DE CRIAÇÃO: 9º (Etapa 9 — coleta do histórico) · junto com collector/collector.go
//
// POR QUE EXISTE:
//
//	É a rota que alimenta o nosso histórico. Fica no pacote spotify (e não no
//	collector) porque é conhecimento sobre a API: URL, parâmetros, formato.
//	O collector só decide QUANDO chamar. Converter a resposta para
//	storage.Play também é trabalho do collector, para o pacote spotify não
//	depender do storage.
//
// Rota do Spotify: GET /me/player/recently-played
// Scope: user-read-recently-played
package spotify

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// PlayedItem é uma vez que a música foi ouvida: o quê e quando.
//
// PlayedAt é time.Time: o decoder JSON converte o texto RFC 3339 do Spotify
// ("2026-11-24T15:03:11.123Z") sozinho.
type PlayedItem struct {
	PlayedAt time.Time `json:"played_at"`
	Track    Track     `json:"track"`
}

// recentLimit é o MÁXIMO que o Spotify aceita por chamada (o teto da API).
const recentLimit = 50

// RecentlyPlayed devolve as últimas faixas ouvidas (no máximo 50).
//
// after: se não for o valor zero, o Spotify devolve só o que foi tocado DEPOIS
// desse instante. O parâmetro vai em MILISSEGUNDOS Unix (não segundos!). O
// coletor passa o último played_at que já guardou, para pedir só o novo.
func (c *Client) RecentlyPlayed(ctx context.Context, after time.Time) ([]PlayedItem, error) {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(recentLimit))
	if !after.IsZero() {
		query.Set("after", strconv.FormatInt(after.UnixMilli(), 10))
	}

	var resp struct {
		Items []PlayedItem `json:"items"`
	}
	if _, err := c.get(ctx, "/me/player/recently-played", query, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}
