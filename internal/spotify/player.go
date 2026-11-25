// ORDEM DE CRIAÇÃO: 6º (Etapa 6 — música atual) · junto com service/nowplaying.go
//
// POR QUE EXISTE:
//
//	É a primeira funcionalidade "de verdade" e a mais simples (uma chamada,
//	sem armazenamento). Fica em arquivo próprio porque cada endpoint do
//	Spotify tem seu próprio formato de resposta.
//
// Rota do Spotify: GET /me/player/currently-playing
// Scope: user-read-currently-playing
package spotify

import "context"

// NowPlaying é o que está tocando agora.
type NowPlaying struct {
	IsPlaying  bool  // false = pausado
	ProgressMS int   // quanto da música já tocou
	Track      Track // a música
}

// currentlyPlayingResponse é o JSON do Spotify (só os campos que usamos).
// Item é ponteiro porque o Spotify manda "item": null em alguns casos
// (anúncios, por exemplo), e um ponteiro consegue representar "ausente".
type currentlyPlayingResponse struct {
	IsPlaying  bool   `json:"is_playing"`
	ProgressMS int    `json:"progress_ms"`
	Type       string `json:"currently_playing_type"` // "track", "episode", "ad"...
	Item       *Track `json:"item"`
}

// CurrentlyPlaying devolve o que está tocando, ou (nil, nil) quando NÃO há uma
// MÚSICA para mostrar. Isso cobre três situações, e nenhuma é erro:
//   - o Spotify responde 204 (nada tocando);
//   - o item é null (anúncio);
//   - o item é um podcast (type "episode"), que não é música.
func (c *Client) CurrentlyPlaying(ctx context.Context) (*NowPlaying, error) {
	var resp currentlyPlayingResponse
	found, err := c.get(ctx, "/me/player/currently-playing", nil, &resp)
	if err != nil {
		return nil, err
	}
	if !found || resp.Item == nil || resp.Type != "track" {
		return nil, nil
	}
	return &NowPlaying{
		IsPlaying:  resp.IsPlaying,
		ProgressMS: resp.ProgressMS,
		Track:      *resp.Item,
	}, nil
}
