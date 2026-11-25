// ORDEM DE CRIAÇÃO: 6º (Etapa 6 — música atual) · junto com spotify/player.go e service/nowplaying.go
//
// POR QUE EXISTE:
//
//	Expõe service.NowPlayingService como JSON em GET /api/now-playing.
//	O front nunca fala com o Spotify direto (isso vazaria o token); sempre
//	passa pelo nosso servidor.
//
// O QUE VAI AQUI (a implementar):
//   - Handler que chama service.Get(ctx) (use r.Context() como ctx).
//   - Nada tocando: 200 com {"playing": false} (não 404 — é um estado
//     normal, não um erro).
//   - Tocando: JSON enxuto {playing, track, artist, image, progress_ms, duration_ms}.
//   - Sem token (nunca fez login): 401 com mensagem apontando para /login.
//   - Outro erro: 502 (falha no Spotify, não no nosso código) e logar o erro
//     real; não vaze detalhes internos no corpo da resposta.
//
// O handler só traduz HTTP <-> service. Se você se pegar escrevendo um "if"
// de regra de negócio aqui, ele pertence ao service.
package handlers
