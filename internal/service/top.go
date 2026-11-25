// ORDEM DE CRIAÇÃO: 7º (Etapa 7 — mais ouvidos) · junto com spotify/top.go e handlers/top.go
//
// POR QUE EXISTE:
//
//	Responde "quais foram minhas músicas mais ouvidas?". É aqui que a
//	limitação da API aparece na prática, e o service é o lugar certo para
//	isso: ele esconde de quem chama DE ONDE vêm os dados.
//
// O QUE VAI AQUI (a implementar):
//   - Versão 1 (Etapa 7): usa spotify.TopTracks("short_term") — rápido de
//     fazer, mas cobre ~4 semanas, NÃO a semana. Serve para sentir o limite.
//   - Versão 2 (depois da Etapa 8/9): troca a fonte para o NOSSO histórico:
//     store.Between(agora-7dias, agora) -> contar plays por track_id ->
//     ordenar por contagem (desempate: mais recente) -> cortar em "limit".
//     Aí é "da semana" de verdade, e traz o número de plays.
//   - struct TrackCount { TrackID, Name, Artist, ImageURL string; Plays int }
//   - func (s *TopService) Week(ctx, limit int) ([]TrackCount, error)
//   - Receber "agora" por parâmetro ou por um relógio injetado (func() time.Time)
//     para conseguir testar "últimos 7 dias" sem depender da data real.
//
// POR QUE EM DUAS VERSÕES: o aprendizado está na troca de fonte sem o
// handler precisar mudar — é para isso que a camada de service existe.
package service
