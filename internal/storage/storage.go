// ORDEM DE CRIAÇÃO: 8º (Etapa 8 — nosso histórico) · junto com playstore.go
//
// POR QUE EXISTE:
//
//	O Spotify só lembra as últimas 50 faixas, então o calendário e o "top da
//	semana" dependem de um histórico NOSSO, guardado no PostgreSQL. Este
//	arquivo define o QUE guardamos (Play) e o CONTRATO de guardar (interface
//	Store), sem dizer COMO. Vem antes do coletor (Etapa 9) porque o coletor
//	precisa de um lugar onde salvar.
//
// POR QUE CONTINUAR COM UMA INTERFACE, JÁ QUE É POSTGRES:
//
//	Os services e o collector só enxergam "Store", então os testes deles usam
//	um Store FALSO em memória: rápidos, sem banco. O Postgres de verdade só
//	aparece nos testes de integração do playstore.go.
package storage

import (
	"context"
	"time"
)

// Play é uma vez que eu ouvi uma música.
//
// Nome, artista e capa ficam junto para o calendário não precisar chamar o
// Spotify de novo. A chave única é PlayedAt + TrackID (veja Key).
type Play struct {
	PlayedAt      time.Time `json:"played_at"`
	TrackID       string    `json:"track_id"`
	TrackName     string    `json:"track_name"`
	Artist        string    `json:"artist"`
	AlbumImageURL string    `json:"album_image_url"`
}

// Store é o contrato do nosso histórico. Todos os métodos recebem context
// porque falam com o banco (permite cancelar e impor prazo).
type Store interface {
	// Add grava os plays e devolve quantos eram NOVOS. Plays que já existem
	// (mesmo PlayedAt + TrackID) são ignorados em silêncio: o coletor sempre
	// reenvia as últimas faixas, então duplicata é o caso normal, não um erro.
	Add(ctx context.Context, plays []Play) (added int, err error)

	// Between devolve os plays com from <= PlayedAt < to, em ordem
	// cronológica. O fim é EXCLUSIVO, para um play exatamente à meia-noite
	// cair em um dia só. Usado por service/calendar e service/top.
	Between(ctx context.Context, from, to time.Time) ([]Play, error)

	// LastPlayedAt devolve o PlayedAt mais recente guardado, ou time.Time{}
	// (valor zero) se não há nenhum. O coletor usa isso no parâmetro ?after=
	// para pedir ao Spotify só o que é novo.
	LastPlayedAt(ctx context.Context) (time.Time, error)
}
