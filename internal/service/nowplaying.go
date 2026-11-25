// ORDEM DE CRIAÇÃO: 6º (Etapa 6 — música atual) · junto com spotify/player.go e handlers/nowplaying.go
//
// POR QUE EXISTE:
//
//	É o PRIMEIRO service do projeto, então também é onde você entende o
//	padrão: o handler não fala com o Spotify; ele pede ao service, e o
//	service decide o que fazer. Aqui a regra é pequena, mas já existe:
//	transformar a resposta bruta do Spotify (cheia de campos) em um modelo
//	enxuto nosso, e tratar "nada tocando" / "podcast" / "anúncio".
//
// O QUE VAI AQUI (a implementar):
//   - Interface PLAYER declarada AQUI (lado de quem consome), só com o método
//     que este service usa:
//     type playerAPI interface { CurrentlyPlaying(ctx) (*spotify.NowPlaying, error) }
//     *spotify.Client satisfaz isso automaticamente. Vantagem: nos testes
//     você passa um "falso" sem internet.
//   - struct NowPlayingService { api playerAPI }
//   - struct NowPlaying (nosso modelo): Playing bool, Track, Artist, Image
//     string, ProgressMS, DurationMS int.
//   - func (s *NowPlayingService) Get(ctx) (NowPlaying, error)
//     Se o Spotify disser "nada tocando" => NowPlaying{Playing:false}, SEM erro.
//
// CONCEITOS: interfaces implícitas em Go, "accept interfaces, return structs".
package service
