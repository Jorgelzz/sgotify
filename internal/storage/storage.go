// ORDEM DE CRIAÇÃO: 8º (Etapa 8 — nosso histórico) · junto com jsonstore.go
//
// POR QUE EXISTE:
//
//	O Spotify só lembra as últimas 50 faixas, então o calendário e o "top da
//	semana" dependem de um histórico NOSSO. Este arquivo define o QUE
//	guardamos (Play) e o CONTRATO de guardar (interface Store), sem dizer
//	COMO. Vem antes do coletor (Etapa 9) porque o coletor precisa de um
//	lugar onde salvar.
//
// O QUE VAI AQUI (a implementar):
//   - struct Play { PlayedAt time.Time; TrackID, TrackName, Artist,
//     AlbumImageURL string } com tags `json:"..."`.
//   - Chave única de um Play: PlayedAt + TrackID (método Key() string).
//   - interface Store:
//     Add(plays []Play) (novos int, err error) // ignora duplicados
//     Between(from, to time.Time) ([]Play, error) // usado por service/calendar e service/top
//     LastPlayedAt() (time.Time, error) // usado pelo coletor (?after=)
//
// POR QUE UMA INTERFACE: hoje o armazenamento é um JSON; amanhã pode ser
// SQLite. Services e collector só enxergam "Store" e nunca mudam.
package storage
