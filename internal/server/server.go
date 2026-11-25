// ORDEM DE CRIAÇÃO: 4º (Etapa 4 — rotas e login) · junto com middleware.go e handlers/auth.go
//
// POR QUE EXISTE:
//
//	Este pacote é o ÚNICO lugar que sabe "qual URL vai para qual handler".
//	Em vez de o main.go crescer a cada rota nova, ele monta os handlers e
//	entrega aqui.
//
// ESCOPO: só as rotas da tabela da seção 6 do README. Esta API NÃO serve
// front-end (não existe "GET /") e as rotas /api/* não têm autenticação
// própria (por isso o README manda não expor o servidor publicamente).
package server

import (
	"net/http"

	"github.com/jorgelzz/sgotify/internal/handlers"
)

// Routes reúne os handlers que o servidor precisa para atender cada rota.
// É injeção de dependências: nada de variável global; main.go monta e entrega.
type Routes struct {
	Auth       *handlers.AuthHandler
	NowPlaying http.Handler
	TopWeek    http.Handler
	Calendar   http.Handler
}

// New registra as rotas e devolve o http.Handler pronto para o http.Server.
//
// A sintaxe "GET /caminho" (Go 1.22+) já restringe o método: outro método na
// mesma URL recebe 405 automaticamente, e URL desconhecida recebe 404.
func New(routes Routes, corsOrigin string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handlers.Health)
	mux.HandleFunc("GET /login", routes.Auth.Login)
	mux.HandleFunc("GET /callback", routes.Auth.Callback)
	mux.Handle("GET /api/now-playing", routes.NowPlaying)
	mux.Handle("GET /api/top-week", routes.TopWeek)
	mux.Handle("GET /api/calendar", routes.Calendar)

	// Ordem das camadas, de fora para dentro: Logger -> CORS -> mux.
	// O Logger fica por fora para registrar TAMBÉM os preflights (OPTIONS).
	return Logger(CORS(corsOrigin)(mux))
}
