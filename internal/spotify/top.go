// ORDEM DE CRIAÇÃO: 7º (Etapa 7 — mais ouvidos)
//
// POR QUE EXISTE:
//
//	Implementa o que o Spotify oferece de "mais ouvidos". É também onde
//	descobrimos a limitação da API: não existe "semana" (a menor janela é de
//	~4 semanas) nem contagem de plays. Por isso o /api/top-week NÃO usa isto:
//	ele é calculado a partir do nosso histórico (service/top.go).
//
//	Este método fica disponível no client (a estrutura do projeto o prevê),
//	mas nenhuma rota o expõe: o README lista só as rotas do escopo.
//
// Rota do Spotify: GET /me/top/tracks
// Scope: user-top-read
package spotify

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// TopTracks devolve as músicas mais ouvidas segundo o Spotify.
//
// timeRange: "short_term" (~4 semanas), "medium_term" (~6 meses) ou
// "long_term" (anos). Qualquer outro valor é recusado ANTES de chamar a rede.
func (c *Client) TopTracks(ctx context.Context, timeRange string, limit int) ([]Track, error) {
	switch timeRange {
	case "short_term", "medium_term", "long_term":
	default:
		return nil, fmt.Errorf("invalid time_range %q", timeRange)
	}

	query := url.Values{}
	query.Set("time_range", timeRange)
	query.Set("limit", strconv.Itoa(limit))

	var resp struct {
		Items []Track `json:"items"`
	}
	if _, err := c.get(ctx, "/me/top/tracks", query, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}
