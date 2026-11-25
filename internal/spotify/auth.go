// ORDEM DE CRIAÇÃO: 3º (Etapa 3 — autenticação) · junto com token.go
//
// POR QUE EXISTE:
//
//	Nada na API do Spotify funciona sem um access token do usuário. Este
//	arquivo concentra TODO o conhecimento do protocolo OAuth 2.0 (Authorization
//	Code + PKCE) para que nenhum outro arquivo precise saber dele. Vem antes
//	do client/rotas porque eles dependem de ter um token.
//
// ORDEM DE ESCRITA DENTRO DO ARQUIVO (cada passo é testável sozinho):
//
//	A) PKCE        randomString, NewVerifier, ChallengeFor, NewState   (sem rede)
//	B) AuthURL     struct Auth, NewAuth, AuthURL                       (só texto)
//	C) token.go    newToken, Expired                                   (outro arquivo)
//	D) Rede        requestToken, ExchangeCode, Refresh                 (httptest)
//
// ATENÇÃO: o Spotify às vezes NÃO devolve um refresh_token novo no refresh;
// nesse caso mantenha o antigo.
package spotify

// Imports que você vai precisar (adicione conforme escrever cada passo; Go não
// compila com import sem uso):
//   Passo A: crypto/rand, crypto/sha256, encoding/base64
//   Passo B: strings (net/url já está importado)
//   Passo D: encoding/json, fmt, io, strings, time (context, net/http e net/url já estão)
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	authorizeURL    = "https://accounts.spotify.com/authorize"
	defaultTokenURL = "https://accounts.spotify.com/api/token"
)

// scopes são as permissões que pedimos ao usuário.
//   - user-read-currently-playing : música atual
//   - user-read-recently-played   : histórico (coletor)
//   - user-top-read               : top tracks do Spotify (comparação)
var scopes = []string{
	"user-read-currently-playing",
	"user-read-recently-played",
	"user-top-read",
}

// ============================================================================
// PASSO A — PKCE (funções soltas, sem struct, sem rede)
// ============================================================================

// randomString devolve n bytes aleatórios codificados em base64url.
//
// O que tratar:
//   - usar crypto/rand (NUNCA math/rand: é previsível e anula o propósito);
//   - rand.Read(b) preenche um []byte e devolve erro: trate-o;
//   - codificar com base64.RawURLEncoding.EncodeToString (o "Raw" = sem "=").
//
// Quem usa: NewVerifier e NewState.
//
// Teste: n=32 deve gerar 43 caracteres; duas chamadas devem ser diferentes.
func randomString(n int) (string, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewVerifier gera o code_verifier do PKCE: a "senha descartável" que o
// servidor guarda até o /callback.
//
// O que tratar:
//   - o padrão exige 43 a 128 caracteres; 32 bytes aleatórios em base64url
//     dão exatamente 43. Basta chamar randomString(32).
//
// Teste: len(verifier) == 43.
func NewVerifier() (string, error) {
	return randomString(32)
}

// ChallengeFor calcula o code_challenge a partir do verifier:
// base64url( SHA256(verifier) ).
//
// O que tratar:
//   - sha256.Sum256([]byte(verifier)) devolve um ARRAY [32]byte; para passar
//     ao base64 você precisa de uma fatia: hash[:];
//   - usar base64.RawURLEncoding (sem padding), senão o Spotify rejeita.
//   - não devolve erro: é uma conta que não falha.
//
// Teste (vetor oficial do RFC 7636, apêndice B):
//
//	verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
//	challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
func ChallengeFor(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// NewState gera o parâmetro "state": um valor aleatório que mandamos ao
// Spotify e que ele devolve igual no /callback. Se não bater, a requisição
// não veio do nosso /login (proteção contra CSRF).
//
// O que tratar:
//   - mesma receita do verifier (randomString). Pode ser um valor menor,
//     ex.: 16 bytes.
//   - quem guarda e compara o state é o handler (Etapa 4), não este arquivo.
func NewState() (string, error) {
	return randomString(16)
}

// ============================================================================
// PASSO B — struct Auth e AuthURL
// ============================================================================

// Auth reúne o que as funções de autenticação precisam, para não repassar os
// mesmos parâmetros em toda chamada.
//
//   - ClientID, RedirectURI: vêm da config.
//   - TokenURL: endereço onde trocamos code/refresh por token. É um CAMPO (e
//     não só a constante) para os testes apontarem para um servidor falso
//     (httptest.NewServer) em vez do Spotify de verdade.
//   - HTTP: cliente HTTP. Precisa de TIMEOUT: sem ele, um Spotify travado
//     trava o seu programa para sempre.
type Auth struct {
	ClientID    string
	RedirectURI string
	TokenURL    string
	HTTP        *http.Client
}

// NewAuth cria um Auth com os padrões.
//
// O que tratar:
//   - TokenURL = defaultTokenURL;
//   - HTTP = &http.Client{Timeout: 10 * time.Second}.
//
// Teste: NewAuth("id", "uri") tem TokenURL padrão e HTTP.Timeout > 0.
func NewAuth(clientID, redirectURI string) *Auth {
	return &Auth{
		ClientID:    clientID,
		RedirectURI: redirectURI,
		TokenURL:    defaultTokenURL,
		HTTP:        &http.Client{Timeout: 10 * time.Second},
	}
}

// AuthURL monta a URL para onde o usuário é redirecionado para aprovar o acesso.
//
// O que tratar (parâmetros da query):
//
//		client_id              = a.ClientID
//		response_type          = "code"
//		redirect_uri           = a.RedirectURI
//		scope                  = scopes separados por ESPAÇO (strings.Join(scopes, " "))
//		state                  = o recebido
//		code_challenge_method  = "S256"
//		code_challenge         = o recebido
//
//	  - usar url.Values (v.Set(...)) e v.Encode(): ele faz o escape correto.
//	    Nunca concatene texto na mão.
//	  - resultado: authorizeURL + "?" + v.Encode().
//
// Teste: url.Parse(resultado) e conferir cada parâmetro com u.Query().Get("...").
func (a *Auth) AuthURL(state, challenge string) string {
	v := url.Values{}
	v.Set("client_id", a.ClientID)
	v.Set("response_type", "code")
	v.Set("redirect_uri", a.RedirectURI)
	v.Set("scope", strings.Join(scopes, " "))
	v.Set("state", state)
	v.Set("code_challenge_method", "S256")
	v.Set("code_challenge", challenge)

	return authorizeURL + "?" + v.Encode()
}

// ============================================================================
// PASSO D — chamadas de rede ao endpoint de token
// ============================================================================

// tokenResponse é o JSON que o Spotify devolve em POST /api/token.
// Struct só da RESPOSTA (modele apenas o que usamos).
//
// Quando for implementar, descomente e ajuste:
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"` // pode vir vazio no refresh!
	ExpiresIn    int    `json:"expires_in"`    // segundos
}

// requestToken faz o POST ao endpoint de token. É compartilhado por
// ExchangeCode e Refresh, que só diferem no formulário enviado.
//
// O que tratar:
//   - corpo = form.Encode() no formato application/x-www-form-urlencoded
//     (NÃO é JSON); header "Content-Type: application/x-www-form-urlencoded";
//   - http.NewRequestWithContext(ctx, http.MethodPost, a.TokenURL,
//     strings.NewReader(form.Encode())) e enviar com a.HTTP.Do(req);
//   - defer resp.Body.Close() logo após checar o erro do Do;
//   - se resp.StatusCode != http.StatusOK: ler o corpo (io.ReadAll) e devolver
//     um erro que o inclua (o Spotify explica: invalid_grant, invalid_client...);
//   - se 200: decodificar em tokenResponse (json.NewDecoder(resp.Body).Decode)
//     e converter com newToken(...);
//   - embrulhar erros com contexto: fmt.Errorf("...: %w", err).
//
// Teste (httptest): servidor falso devolve 200 + JSON -> Token correto;
// devolve 400 -> erro contendo o corpo.
func (a *Auth) requestToken(ctx context.Context, form url.Values) (Token, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("erro ao criar request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("erro ao executar request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Token{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return Token{}, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	return newToken(tr.AccessToken, tr.RefreshToken, tr.ExpiresIn), nil
}

// ExchangeCode troca o "code" recebido no /callback pelo primeiro Token.
//
// O que tratar (campos do formulário):
//
//		grant_type    = "authorization_code"
//		code          = o recebido
//		redirect_uri  = a.RedirectURI (IDÊNTICO ao usado no AuthURL, senão o Spotify recusa)
//		client_id     = a.ClientID
//		code_verifier = o verifier guardado no /login
//
//	  - não há client_secret (é o ponto do PKCE) nem header Authorization.
//	  - delega para a.requestToken.
func (a *Auth) ExchangeCode(ctx context.Context, code, verifier string) (Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", a.RedirectURI)
	form.Set("client_id", a.ClientID)
	form.Set("code_verifier", verifier)

	return a.requestToken(ctx, form)
}

// Refresh troca um refresh_token por um novo access token, sem pedir login.
// Será usado pelo client.go (Etapa 5) quando Token.Expired() for true.
//
// O que tratar (campos do formulário):
//
//		grant_type    = "refresh_token"
//		refresh_token = o recebido
//		client_id     = a.ClientID
//
//	  - PEGADINHA: o Spotify às vezes NÃO devolve refresh_token novo. Se o Token
//	    retornado vier com RefreshToken vazio, copie o antigo (o parâmetro
//	    refreshToken); senão você perde o refresh e precisa logar de novo.
//	  - delega para a.requestToken.
//
// Teste (httptest): resposta SEM refresh_token -> Token.RefreshToken continua
// sendo o antigo.
func (a *Auth) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", a.ClientID)

	token, err := a.requestToken(ctx, form)
	if err != nil {
		return Token{}, err
	}

	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}

	return token, nil
}
