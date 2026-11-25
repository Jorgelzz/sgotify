// ORDEM DE CRIAÇÃO: 4º (Etapa 4 — rotas e login) · junto com server/server.go
//
// POR QUE EXISTE:
//
//	É a ponta HTTP do login: o browser fala com ESTES handlers, e eles usam
//	spotify/auth.go para o trabalho de verdade. Handlers são "tradutores":
//	leem a requisição, chamam a lógica, escrevem a resposta.
//	(Exceção consciente: o login chama spotify/auth direto, sem service,
//	porque não há regra de negócio — é só protocolo OAuth.)
//
// COMO O LOGIN FUNCIONA (OAuth 2.0 Authorization Code + PKCE):
//
//	/login:    gera verifier e state, guarda os dois em cookies e redireciona
//	           o browser para a tela de aprovação do Spotify.
//	/callback: o Spotify devolve o browser aqui com ?code=...&state=...; conferimos
//	           o state, trocamos o code por tokens, gravamos e mandamos o
//	           browser de volta ao front-end.
package handlers

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"

	"github.com/jorgelzz/sgotify/internal/spotify"
)

const (
	stateCookie    = "sgotify_state"
	verifierCookie = "sgotify_verifier"
	// O login leva segundos; 10 min é folga de sobra, e um cookie esquecido
	// não fica valendo para sempre.
	loginCookieMaxAge = 10 * 60 // segundos
)

// authAPI é só o que este handler precisa do spotify.Auth (interface do lado de
// quem consome: nos testes entra um falso, sem falar com o Spotify).
type authAPI interface {
	AuthURL(state, challenge string) string
	ExchangeCode(ctx context.Context, code, verifier string) (spotify.Token, error)
}

// AuthHandler reúne os handlers do login.
type AuthHandler struct {
	auth        authAPI
	tokens      spotify.TokenStore
	frontendURL string // para onde o browser volta depois do login
}

// NewAuthHandler cria os handlers com as dependências injetadas.
func NewAuthHandler(auth authAPI, tokens spotify.TokenStore, frontendURL string) *AuthHandler {
	return &AuthHandler{auth: auth, tokens: tokens, frontendURL: frontendURL}
}

// Login (GET /login) inicia o OAuth.
//
// Guardamos verifier e state em COOKIES, e não em memória do servidor:
//   - o servidor continua sem estado (um reinício no meio do login não quebra);
//   - o state fica amarrado ao browser que iniciou o login, e é isso que o
//     torna uma proteção CSRF de verdade.
//
// Os cookies são HttpOnly (JavaScript da página não os lê), SameSite=Lax (o
// browser os envia no redirecionamento de volta do Spotify, mas não em
// requisições de outros sites) e restritos ao caminho /callback.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	verifier, err := spotify.NewVerifier()
	if err != nil {
		internalError(w, err)
		return
	}
	state, err := spotify.NewState()
	if err != nil {
		internalError(w, err)
		return
	}

	setLoginCookie(w, stateCookie, state, loginCookieMaxAge)
	setLoginCookie(w, verifierCookie, verifier, loginCookieMaxAge)

	// Ao Spotify vai o CHALLENGE (o hash). O verifier fica só no cookie.
	http.Redirect(w, r, h.auth.AuthURL(state, spotify.ChallengeFor(verifier)), http.StatusFound)
}

// Callback (GET /callback) conclui o login.
//
// Respostas de erro aqui são TEXTO simples (e não JSON): quem está chamando é o
// browser do usuário, no meio de um redirecionamento, e não o front via fetch.
func (h *AuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// O usuário clicou em "Cancelar" na tela do Spotify (ou houve outro erro).
	if e := query.Get("error"); e != "" {
		http.Error(w, "spotify login was not completed: "+e, http.StatusBadRequest)
		return
	}

	stateC, errState := r.Cookie(stateCookie)
	verifierC, errVerifier := r.Cookie(verifierCookie)

	// Os cookies valem para UMA tentativa: apagamos já, antes de qualquer
	// verificação, para o mesmo code/state não poder ser reutilizado.
	setLoginCookie(w, stateCookie, "", -1)
	setLoginCookie(w, verifierCookie, "", -1)

	if errState != nil || errVerifier != nil {
		http.Error(w, "login session not found: start again at /login", http.StatusBadRequest)
		return
	}

	// Compara o state do cookie com o devolvido pelo Spotify. ConstantTimeCompare
	// leva o mesmo tempo qualquer que seja a diferença (evita vazar, pelo
	// tempo de resposta, quantos caracteres batem).
	got := query.Get("state")
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(stateC.Value)) != 1 {
		http.Error(w, "invalid state: start again at /login", http.StatusBadRequest)
		return
	}

	code := query.Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	token, err := h.auth.ExchangeCode(r.Context(), code, verifierC.Value)
	if err != nil {
		log.Printf("exchanging login code: %v", err)
		http.Error(w, "could not complete the login with Spotify", http.StatusBadGateway)
		return
	}
	if err := h.tokens.Save(r.Context(), token); err != nil {
		log.Printf("saving token: %v", err)
		http.Error(w, "could not save the login", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.frontendURL, http.StatusFound)
}

// setLoginCookie grava (ou, com maxAge negativo, APAGA) um cookie do login.
func setLoginCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/callback",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure fica desligado de propósito: o servidor local fala HTTP
		// (127.0.0.1). Atrás de HTTPS, ligue.
	})
}
