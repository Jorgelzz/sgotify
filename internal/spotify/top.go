// ORDEM DE CRIAÇÃO: 7º (Etapa 7 — mais ouvidos) · junto com handlers/top.go
//
// POR QUE EXISTE:
//
//	Segunda rota do Spotify, agora com parâmetros de query e uma LISTA como
//	resposta. É também onde descobrimos a limitação da API: não existe
//	"semana". Este arquivo implementa o que o Spotify oferece (opcional /
//	comparação) e nos ensina por que precisamos do histórico próprio (Etapas 8-9).
//
// O QUE VAI AQUI (a implementar):
//   - Rota: GET /me/top/tracks?time_range=short_term&limit=10 (scope: user-top-read)
//     time_range: short_term (~4 semanas) | medium_term (~6 meses) | long_term.
//   - func (c *Client) TopTracks(ctx, timeRange string, limit int) ([]Track, error)
//   - struct Track reaproveitável (id, name, artists, album image) —
//     provavelmente a mesma usada por history.go. Se for, mova para um
//     arquivo types.go.
//
// NOTA: esta rota NÃO diz quantas vezes você ouviu nem em que dia. Só uma
// ordem de ranking calculada pelo Spotify.
package spotify
