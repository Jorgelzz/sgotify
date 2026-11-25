// ORDEM DE CRIAÇÃO: 6º (Etapa 6 — música atual) · junto com handlers/nowplaying.go
//
// POR QUE EXISTE:
//
//	É a primeira funcionalidade "de verdade" e a mais simples (uma chamada,
//	sem armazenamento). Serve para validar que login + client funcionam.
//	Fica em arquivo próprio porque cada endpoint do Spotify tem seu próprio
//	formato de resposta.
//
// O QUE VAI AQUI (a implementar):
//   - Rota do Spotify: GET /me/player/currently-playing (scope:
//     user-read-currently-playing).
//   - structs só com os campos que usamos (item.name, item.artists[].name,
//     item.album.images[].url, is_playing, progress_ms, item.duration_ms).
//     Não copie o JSON inteiro: modele apenas o necessário.
//   - func (c *Client) CurrentlyPlaying(ctx) (*NowPlaying, error)
//     Devolve nil (sem erro) quando o Spotify responde 204 = nada tocando.
//
// ARMADILHAS:
//   - 204 não tem corpo: não tente decodificar JSON.
//   - "item" pode ser um podcast (episode) em vez de música — trate ou ignore.
//   - "item" pode vir null (ex.: anúncio).
package spotify
