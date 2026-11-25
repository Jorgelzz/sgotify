// ORDEM DE CRIAÇÃO: 9º (Etapa 9 — coleta do histórico) · junto com spotify/history.go
//
// POR QUE EXISTE:
//
//	É a peça que resolve a limitação central do projeto: roda em segundo
//	plano, de tempos em tempos, e copia o histórico do Spotify para o nosso
//	storage ANTES de ele ser esquecido (só 50 faixas ficam disponíveis).
//	Sem ele, o calendário seria impossível. É o primeiro código do projeto
//	que NÃO é disparado por uma requisição HTTP.
//
// LIMITE HONESTO: o coletor só funciona com o servidor ligado. Se ficar
// desligado e você ouvir mais de 50 faixas nesse intervalo, as mais antigas
// se perdem.
package collector

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jorgelzz/sgotify/internal/spotify"
	"github.com/jorgelzz/sgotify/internal/storage"
)

// historyAPI é só o que o coletor precisa do Spotify (declarada aqui, do lado
// de quem consome; *spotify.Client a satisfaz). Nos testes: um falso.
type historyAPI interface {
	RecentlyPlayed(ctx context.Context, after time.Time) ([]spotify.PlayedItem, error)
}

// Collector copia periodicamente o histórico do Spotify para o storage.
type Collector struct {
	api      historyAPI
	store    storage.Store
	interval time.Duration
}

// New cria o coletor. interval vem da config (COLLECT_INTERVAL_MINUTES) e
// precisa ser maior que zero (a config já garante): time.NewTicker entra em
// pânico com intervalo <= 0.
func New(api historyAPI, store storage.Store, interval time.Duration) *Collector {
	return &Collector{api: api, store: store, interval: interval}
}

// Run coleta uma vez IMEDIATAMENTE (sem esperar o primeiro intervalo) e depois
// a cada "interval", até o ctx ser cancelado. Feito para rodar numa goroutine:
//
//	go collector.Run(ctx)
//
// Cancelar o ctx é o jeito de pará-lo (shutdown limpo do programa).
func (c *Collector) Run(ctx context.Context) {
	c.collectAndLog(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop() // libera o recurso do ticker ao sair

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectAndLog(ctx)
		}
	}
}

// collectAndLog roda uma coleta e registra o resultado no log. NUNCA derruba o
// programa: uma falha (rede fora, Spotify instável, 429) só vai para o log, e
// a próxima rodada tenta de novo.
func (c *Collector) collectAndLog(ctx context.Context) {
	added, err := c.Collect(ctx)
	switch {
	case errors.Is(err, spotify.ErrNoToken):
		// Esperado antes do primeiro login: não é um defeito.
		log.Printf("collector: waiting for login (open /login once)")
	case err != nil:
		// Se o programa está encerrando, o ctx cancelado explica o erro: não
		// poluir o log com isso.
		if ctx.Err() == nil {
			log.Printf("collector: %v", err)
		}
	default:
		log.Printf("collector: %d new plays", added)
	}
}

// Collect faz UMA rodada de coleta e devolve quantos plays eram novos.
//
// Passos:
//  1. pergunta ao storage qual foi o último play guardado;
//  2. pede ao Spotify só o que veio DEPOIS disso (parâmetro "after");
//  3. converte para o nosso modelo e grava.
//
// É IDEMPOTENTE: rodar duas vezes seguidas não duplica nada, porque o storage
// ignora o que já existe (chave played_at + track_id).
func (c *Collector) Collect(ctx context.Context) (int, error) {
	after, err := c.store.LastPlayedAt(ctx)
	if err != nil {
		return 0, err
	}

	items, err := c.api.RecentlyPlayed(ctx, after)
	if err != nil {
		return 0, err
	}

	plays := toPlays(items)
	if len(plays) == 0 {
		return 0, nil
	}
	return c.store.Add(ctx, plays)
}

// toPlays converte a resposta do Spotify para o nosso modelo.
//
// Faixas SEM ID são ignoradas: são arquivos locais do computador do usuário, que
// o Spotify não cataloga. Sem ID não dá para contar "quantas vezes ouvi esta
// música" (todas virariam a mesma), então não entram no histórico.
func toPlays(items []spotify.PlayedItem) []storage.Play {
	plays := make([]storage.Play, 0, len(items))
	for _, it := range items {
		if it.Track.ID == "" {
			continue
		}
		plays = append(plays, storage.Play{
			PlayedAt:      it.PlayedAt.UTC(),
			TrackID:       it.Track.ID,
			TrackName:     it.Track.Name,
			Artist:        it.Track.ArtistNames(),
			AlbumImageURL: it.Track.ImageURL(),
		})
	}
	return plays
}
