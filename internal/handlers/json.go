// ORDEM DE CRIAÇÃO: 4º (Etapa 4 — rotas e login) · ferramentas usadas por todos os handlers
//
// POR QUE EXISTE:
//
//	Todo handler da API termina escrevendo JSON e, quando algo dá errado,
//	escolhendo um status HTTP. Fazer isso num lugar só garante que as respostas
//	tenham sempre o mesmo formato, e que a política de erros seja uma só.
package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/jorgelzz/sgotify/internal/spotify"
)

// errorResponse é o corpo de TODA resposta de erro da API.
// LoginURL só aparece quando o problema é "ainda não fez login".
type errorResponse struct {
	Error    string `json:"error"`
	LoginURL string `json:"login_url,omitempty"`
}

// writeJSON escreve v como JSON com o status dado.
//
// A ORDEM importa: primeiro os headers, depois o status, por último o corpo.
// Depois do primeiro Write o status já foi enviado e não dá mais para mudá-lo.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// O status já foi enviado: só resta registrar.
		log.Printf("writing json response: %v", err)
	}
}

// writeError escreve uma resposta de erro padronizada.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

// upstreamError trata uma falha ao consultar o SPOTIFY (rotas now-playing...).
//
//   - Nunca fez login (spotify.ErrNoToken): 401, com o caminho do login, para o
//     front saber o que mostrar ao usuário;
//   - qualquer outra falha: 502 Bad Gateway ("o servidor de cima falhou", e
//     não o nosso). O erro REAL vai só para o log: o corpo da resposta não
//     vaza detalhes internos.
func upstreamError(w http.ResponseWriter, err error) {
	if errors.Is(err, spotify.ErrNoToken) {
		writeJSON(w, http.StatusUnauthorized, errorResponse{
			Error:    "not logged in to Spotify",
			LoginURL: "/login",
		})
		return
	}
	log.Printf("spotify request failed: %v", err)
	writeError(w, http.StatusBadGateway, "spotify request failed")
}

// internalError trata uma falha NOSSA (banco, por exemplo): 500, com o erro
// real só no log.
func internalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
