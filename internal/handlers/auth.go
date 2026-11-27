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
//	           o state, trocamos o code por tokens, CONFERIMOS DE QUEM É A
//	           CONTA (só a permitida passa), gravamos o token, damos ao
//	           browser o cookie de sessão e o mandamos de volta ao front-end.
//
// O login do Spotify é, ao mesmo tempo, a autorização para ler os seus dados
// e a identidade que libera /api/*: não existe senha própria.
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

// identityAPI pergunta ao Spotify "de quem é este access token?"
// (*spotify.Client satisfaz: é o UserID de spotify/me.go).
type identityAPI interface {
	UserID(ctx context.Context, accessToken string) (string, error)
}

// sessionIssuer grava o cookie de sessão na resposta
// (*session.Manager satisfaz).
type sessionIssuer interface {
	Issue(w http.ResponseWriter)
}

// AuthHandler reúne os handlers do login.
type AuthHandler struct {
	auth          authAPI
	identity      identityAPI
	tokens        spotify.TokenStore
	sessions      sessionIssuer
	allowedUserID string // a ÚNICA conta Spotify autorizada (ALLOWED_SPOTIFY_USER_ID)
	frontendURL   string // para onde o browser volta depois do login
}

// NewAuthHandler cria os handlers com as dependências injetadas.
func NewAuthHandler(auth authAPI, identity identityAPI, tokens spotify.TokenStore,
	sessions sessionIssuer, allowedUserID, frontendURL string) *AuthHandler {
	return &AuthHandler{
		auth:          auth,
		identity:      identity,
		tokens:        tokens,
		sessions:      sessions,
		allowedUserID: allowedUserID,
		frontendURL:   frontendURL,
	}
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

	// IDENTIDADE: o login do Spotify só prova que ALGUÉM tem uma conta Spotify.
	// Perguntamos de quem é o token e só seguimos se for a conta permitida.
	// Isto vem ANTES do Save: uma conta de fora não pode sobrescrever o token
	// guardado (senão a API passaria a mostrar a conta DELA).
	id, err := h.identity.UserID(r.Context(), token.AccessToken)
	if err != nil {
		log.Printf("checking spotify account: %v", err)
		http.Error(w, "could not verify the Spotify account", http.StatusBadGateway)
		return
	}
	if !h.isAllowed(id) {
		// O id vai para o log (e não para a resposta): é assim que você
		// descobre o seu ALLOWED_SPOTIFY_USER_ID na primeira vez.
		log.Printf("login rejected: spotify user %q is not the allowed account", id)
		http.Error(w, "this Spotify account is not allowed", http.StatusForbidden)
		return
	}

	if err := h.tokens.Save(r.Context(), token); err != nil {
		log.Printf("saving token: %v", err)
		http.Error(w, "could not save the login", http.StatusInternalServerError)
		return
	}

	// Conta confirmada e token guardado: o browser ganha o cookie de sessão que
	// libera /api/*.
	h.sessions.Issue(w)
	http.Redirect(w, r, h.frontendURL, http.StatusFound)
}

// isAllowed diz se id é a conta autorizada.
//
// Um id vazio, ou uma configuração com id permitido vazio, NUNCA passa: sem
// isso, "" == "" liberaria qualquer um. (A config e o spotify.UserID já
// impedem os dois casos; esta checagem é a última barreira.) A comparação é em
// tempo constante, como a do state.
func (h *AuthHandler) isAllowed(id string) bool {
	if id == "" || h.allowedUserID == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(id), []byte(h.allowedUserID)) == 1
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
