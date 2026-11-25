// ORDEM DE CRIAÇÃO: 0º (já existia no main.go) · movido para cá na Etapa 4
//
// POR QUE EXISTE:
//
//	GET /health diz se o servidor está vivo. Nasceu no main.go; foi movido para
//	cá para o main.go ficar só com "montar as peças e ligar".
package handlers

import "net/http"

// healthResponse é o corpo de /health.
type healthResponse struct {
	Status string `json:"status"`
}

// Health responde 200 {"status":"ok"}. Não consulta o banco nem o Spotify: só
// prova que o processo está de pé e atendendo HTTP.
func Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}
