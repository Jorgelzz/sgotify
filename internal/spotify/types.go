// ORDEM DE CRIAÇÃO: 6º (Etapa 6 — música atual) · primeiro uso: player.go
//
// POR QUE EXISTE:
//
//	A "música" aparece em 3 respostas diferentes do Spotify (música atual, top
//	tracks, histórico) com o MESMO formato. Em vez de repetir a struct em cada
//	arquivo, existe UMA aqui. Modelamos só os campos que usamos; o Spotify manda
//	dezenas de outros e o decoder JSON simplesmente os ignora.
package spotify

import "strings"

// Image é uma capa de álbum. O Spotify devolve vários tamanhos da mesma capa.
type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Artist é um artista da faixa (só o nome nos interessa).
type Artist struct {
	Name string `json:"name"`
}

// Album guarda as capas da faixa.
type Album struct {
	Images []Image `json:"images"`
}

// Track é uma música como o Spotify a descreve.
//
// ID pode vir vazio em arquivos locais (música do próprio computador do
// usuário, que o Spotify não cataloga).
type Track struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	DurationMS int      `json:"duration_ms"`
	Artists    []Artist `json:"artists"`
	Album      Album    `json:"album"`
}

// ArtistNames junta os artistas em um texto: "Artista A, Artista B".
func (t Track) ArtistNames() string {
	names := make([]string, 0, len(t.Artists))
	for _, a := range t.Artists {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}

// ImageURL escolhe UMA capa para exibir.
//
// O Spotify manda as imagens da maior para a menor (ex.: 640, 300 e 64 px).
// Pegamos a do MEIO: boa para um ícone de calendário e bem mais leve que a
// original. Devolve "" se a faixa não tiver imagem.
func (t Track) ImageURL() string {
	imgs := t.Album.Images
	if len(imgs) == 0 {
		return ""
	}
	return imgs[len(imgs)/2].URL
}
