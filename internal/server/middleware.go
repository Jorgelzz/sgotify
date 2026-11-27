// ORDEM DE CRIAÇÃO: 4º (Etapa 4 — rotas e login) · junto com server.go
//
// POR QUE EXISTE:
//
//	O front-end roda em OUTRA origem (outro projeto/porta). O browser só deixa
//	o JS dele ler nossas respostas se enviarmos headers de CORS. Isso vale para
//	TODAS as rotas, então não se faz dentro de cada handler: faz-se UMA vez,
//	num middleware — uma função que recebe um http.Handler e devolve outro
//	http.Handler "envolvendo" o original.
//
// CONCEITO PARA ENTENDER: o padrão func(http.Handler) http.Handler é a base
// de todos os middlewares em Go.
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// CORS libera a origem do front-end (e SÓ ela) a ler as respostas.
//
//  1. Access-Control-Allow-Origin = allowedOrigin, a origem configurada
//     (CORS_ALLOWED_ORIGIN). Nunca "*": com "*", qualquer site aberto no
//     browser do usuário poderia ler /api/now-playing.
//  2. Access-Control-Allow-Credentials = true: o front precisa enviar o cookie
//     de sessão (fetch com credentials: "include"). O browser só aceita isso
//     com uma origem EXPLÍCITA, nunca com "*", mais um motivo para o item 1.
//  3. "Vary: Origin" avisa caches de que a resposta depende da origem.
//  4. Preflight: antes de certas requisições o browser pergunta "posso?" com um
//     OPTIONS. Respondemos 204 (sem corpo) e NÃO chamamos o handler seguinte.
//     O preflight NÃO exige sessão: o browser o envia sem cookies.
//
// O CORS envolve o mux inteiro, e não cada rota: o mux só registra GET, e um
// OPTIONS cairia em "405 Method Not Allowed" antes de chegar a qualquer
// handler.
func CORS(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", allowedOrigin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")

			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sessionChecker é o que o middleware precisa da sessão (interface do lado de
// quem consome; *session.Manager satisfaz, e os testes podem usar um falso).
type sessionChecker interface {
	Valid(r *http.Request) bool
}

// unauthorizedBody é o corpo do 401: o mesmo formato dos erros da API
// ({"error": "...", "login_url": "..."}), para o front tratar tudo igual.
type unauthorizedBody struct {
	Error    string `json:"error"`
	LoginURL string `json:"login_url"`
}

// RequireSession só deixa passar quem tem um cookie de sessão válido (emitido
// pelo /callback depois de confirmar que a conta Spotify é a permitida).
// Sem ele: 401 com o caminho do login, e o handler NÃO é chamado.
//
// Aplicado só às rotas /api/*. /health, /login e /callback ficam abertos:
// /login e /callback são justamente como se consegue a sessão.
//
// Como o CORS está POR FORA deste middleware, a resposta 401 também leva os
// headers de CORS, e o front consegue lê-la.
func RequireSession(sessions sessionChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !sessions.Valid(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(unauthorizedBody{
					Error:    "authentication required",
					LoginURL: "/login",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder embrulha o ResponseWriter para o Logger descobrir o status
// que o handler escolheu (o ResponseWriter normal não permite ler de volta).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader guarda o status e repassa ao ResponseWriter de verdade.
func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Logger registra uma linha por requisição: método, caminho, status e duração.
// Ótimo para ver o que o front está chamando.
//
// Loga só r.URL.Path, NUNCA a query: em /callback ela contém o "code" e o
// "state" do login, que não devem ficar em log.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Se o handler não chamar WriteHeader, o Go responde 200 por padrão.
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}
