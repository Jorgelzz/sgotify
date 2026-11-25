// ORDEM DE CRIAÇÃO: 6º (Etapa 6 — música atual) · junto com spotify/player.go e handlers/nowplaying.go
//
// POR QUE EXISTE:
//
//	É o PRIMEIRO service do projeto, então também é onde se vê o padrão: o
//	handler não fala com o Spotify; ele pede ao service, e o service decide o
//	que fazer. Aqui a regra é pequena, mas já existe: transformar a resposta
//	do Spotify (cheia de campos) em um modelo enxuto nosso, e tratar "nada
//	tocando", "podcast" e "anúncio".
package service

import (
	"context"

	"github.com/jorgelzz/sgotify/internal/spotify"
)

// playerAPI é só o que ESTE service precisa do Spotify.
//
// É declarada aqui, do lado de quem CONSOME (e não no pacote spotify):
// *spotify.Client satisfaz a interface automaticamente (interfaces em Go são
// implícitas), e nos testes passamos um falso, sem internet.
type playerAPI interface {
	CurrentlyPlaying(ctx context.Context) (*spotify.NowPlaying, error)
}

// NowPlaying é o nosso modelo enxuto do que está tocando.
type NowPlaying struct {
	Playing    bool   `json:"playing"`
	Track      string `json:"track"`
	Artist     string `json:"artist"`
	Image      string `json:"image"`
	ProgressMS int    `json:"progress_ms"`
	DurationMS int    `json:"duration_ms"`
}

// NowPlayingService responde "o que estou ouvindo agora?".
type NowPlayingService struct {
	api playerAPI
}

// NewNowPlayingService cria o service com a fonte de dados injetada.
func NewNowPlayingService(api playerAPI) *NowPlayingService {
	return &NowPlayingService{api: api}
}

// Get devolve o que está tocando.
//
// Regra: "nada tocando", anúncio e podcast NÃO são erro: devolvem
// NowPlaying{Playing: false}. Só falha de verdade (rede, sem token...) vira
// erro, e o erro original é repassado intacto para o handler decidir o status
// HTTP (por exemplo, errors.Is(err, spotify.ErrNoToken)).
//
// Música PAUSADA: Playing é false, mas os dados da faixa vêm preenchidos (o
// Spotify continua informando qual era a faixa).
func (s *NowPlayingService) Get(ctx context.Context) (NowPlaying, error) {
	np, err := s.api.CurrentlyPlaying(ctx)
	if err != nil {
		return NowPlaying{}, err
	}
	if np == nil {
		return NowPlaying{}, nil
	}
	return NowPlaying{
		Playing:    np.IsPlaying,
		Track:      np.Track.Name,
		Artist:     np.Track.ArtistNames(),
		Image:      np.Track.ImageURL(),
		ProgressMS: np.ProgressMS,
		DurationMS: np.Track.DurationMS,
	}, nil
}
