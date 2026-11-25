// ORDEM DE CRIAÇÃO: 6º (Etapa 6 — música atual) · junto com spotify/player.go e service/nowplaying.go
//
// POR QUE EXISTE:
//
//	Expõe service.NowPlayingService como JSON em GET /api/now-playing.
//	O front nunca fala com o Spotify direto (isso vazaria o token); sempre
//	passa pelo nosso servidor.
package handlers

import (
	"context"
	"net/http"

	"github.com/jorgelzz/sgotify/internal/service"
)

// nowPlayingGetter é o que este handler precisa do service (interface do lado
// de quem consome; nos testes entra um falso).
type nowPlayingGetter interface {
	Get(ctx context.Context) (service.NowPlaying, error)
}

// NowPlaying responde GET /api/now-playing.
//
//   - tocando:     200 {"playing":true,"track":...,"artist":...,"image":...,
//     "progress_ms":...,"duration_ms":...}
//   - nada tocando (ou pausado): 200 {"playing":false}. É um estado NORMAL, não
//     um erro, então não é 404.
//   - sem login:   401 (veja upstreamError)
//   - falha no Spotify: 502
//
// O handler só traduz HTTP <-> service: nenhuma regra de negócio aqui.
func NowPlaying(svc nowPlayingGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		np, err := svc.Get(r.Context()) // r.Context() cancela se o cliente desistir
		if err != nil {
			upstreamError(w, err)
			return
		}
		if !np.Playing {
			writeJSON(w, http.StatusOK, map[string]bool{"playing": false})
			return
		}
		writeJSON(w, http.StatusOK, np)
	}
}
