// ORDEM DE CRIAÇÃO: 9º (Etapa 9 — coleta do histórico) · junto com collector/collector.go
//
// POR QUE EXISTE:
//
//	É a rota que alimenta o nosso histórico. Fica no pacote spotify (e não no
//	collector) porque é conhecimento sobre a API: URL, parâmetros, formato.
//	O collector só decide QUANDO chamar.
//
// O QUE VAI AQUI (a implementar):
//   - Rota: GET /me/player/recently-played?limit=50&after=<ms> (scope:
//     user-read-recently-played).
//   - func (c *Client) RecentlyPlayed(ctx, after time.Time) ([]PlayedItem, error)
//   - PlayedItem: played_at (string RFC3339 -> time.Time) + a Track
//     (reaproveite a de top.go / types.go).
//   - "after" é um timestamp em MILISSEGUNDOS Unix (não segundos!).
//
// ARMADILHAS:
//   - Máximo 50 itens por chamada. É o teto da API.
//   - "after" e "before" não podem ser usados juntos.
//   - A mesma faixa pode aparecer repetida (tocou 2x) — com played_at
//     diferentes. Não é duplicata, são 2 plays.
//   - Converter a resposta para storage.Play é trabalho do collector, para
//     o pacote spotify não depender do storage.
package spotify
