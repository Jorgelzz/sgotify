// ORDEM DE CRIAÇÃO: 4º (Etapa 4 — rotas e login) · junto com middleware.go e handlers/auth.go
//
// POR QUE EXISTE:
//
//	Este pacote é o ÚNICO lugar que sabe "qual URL vai para qual handler".
//	Em vez de o main.go crescer a cada rota nova, ele monta os handlers e
//	entrega aqui.
//
// ESCOPO: só as rotas da tabela do guide-structure.md. Esta API NÃO serve
// front-end (não existe "GET /").
//
// AUTENTICAÇÃO: /api/* exige o cookie de sessão (middleware RequireSession).
// /health, /login e /callback ficam abertos: os dois últimos são como se
// obtém a sessão. A identidade vem do login do Spotify (só a conta permitida
// recebe o cookie), e não de senha própria.
package server

import (
	"net/http"

	"github.com/jorgelzz/sgotify/internal/handlers"
)

// Routes reúne os handlers que o servidor precisa para atender cada rota.
// É injeção de dependências: nada de variável global; main.go monta e entrega.
type Routes struct {
	Auth       *handlers.AuthHandler
	Sessions   sessionChecker // decide quem pode chamar /api/*
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
	protect := RequireSession(routes.Sessions)

	// Abertas: saúde e o fluxo de login (é por ele que se ganha a sessão).
	mux.HandleFunc("GET /health", handlers.Health)
	mux.HandleFunc("GET /login", routes.Auth.Login)
	mux.HandleFunc("GET /callback", routes.Auth.Callback)

	// Protegidas: só com sessão válida.
	mux.Handle("GET /api/now-playing", protect(routes.NowPlaying))
	mux.Handle("GET /api/top-week", protect(routes.TopWeek))
	mux.Handle("GET /api/calendar", protect(routes.Calendar))

	// Ordem das camadas, de fora para dentro: Logger -> CORS -> mux (-> sessão,
	// só nas rotas /api/*).
	// O Logger fica por fora para registrar TAMBÉM os preflights (OPTIONS).
	return Logger(CORS(corsOrigin)(mux))
}
