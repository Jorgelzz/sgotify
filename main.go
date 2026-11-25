// ORDEM DE CRIAÇÃO: 0º (já existia) → será reescrito na 11ª etapa
//
// POR QUE EXISTE:
//
//	Ponto de entrada do programa. Hoje só sobe um servidor com /health, o que
//	serve para provar que o ambiente Go funciona.
//
// COMO VAI FICAR (Etapa 11): main.go deve ficar PEQUENO — só "monta as peças
// e liga": config.Load() -> storage.NewJSONStore() -> spotify.NewClient() ->
// service.New...() -> collector.Run() em uma goroutine -> server.New() ->
// http.ListenAndServe, com shutdown limpo (os/signal + context).
// Toda a lógica mora em internal/.
//
// AJUSTE PENDENTE: o log abaixo diz "localhost", mas o Spotify exige
// 127.0.0.1 no redirect URI — usar 127.0.0.1 e a porta vinda do config.
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/jorgelzz/sgotify/internal/config"
)

type healthResponse struct {
	Status string `json:"status"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}

func main() {

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("port:", cfg.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))

}
