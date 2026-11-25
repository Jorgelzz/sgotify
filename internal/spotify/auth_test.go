package spotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestChallengerFor(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	got := ChallengeFor(verifier)

	if got != want {
		t.Errorf("ChallengerFor = %q, want %q", got, want)
	}
}

func TestRandomString(t *testing.T) {
	n := 32
	rs, err := randomString(n)

	if err != nil {
		t.Fatalf("randomString() error = %v", err)
	}

	if len(rs) != 43 {
		t.Errorf("randomString = %d, expected 43 characters", len(rs))
	}
}

func TestNewVerifierRandomness(t *testing.T) {

	a, _ := NewVerifier()
	b, _ := NewVerifier()

	if a == b {
		t.Fatalf("The values a: %q and b: %q are identical", a, b)
	}
}

func TestNewVerifierLength(t *testing.T) {
	v, err := NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Errorf("len(verifier) = %d, want between 43 and 128", len(v))
	}
}

func TestAuthURL(t *testing.T) {

	const (
		clientID    = "my-client"
		redirectURI = "http://127.0.0.1:8080/callback"
		state       = "my-state"
		challenge   = "my-challenge"
	)

	a := NewAuth(clientID, redirectURI)

	raw := a.AuthURL(state, challenge)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("AuthURL() returned an invalid URL: %v", err)
	}

	q := u.Query()

	base := u.Scheme + "://" + u.Host + u.Path
	if base != "https://accounts.spotify.com/authorize" {
		t.Errorf("base URL = %q, want: https://accounts.spotify.com/authorize", base)
	}

	checks := map[string]string{
		"response_type":         "code",
		"code_challenge_method": "S256",
		"client_id":             clientID,
		"redirect_uri":          redirectURI,
		"state":                 state,
		"code_challenge":        challenge,
	}

	for key, want := range checks {
		if got := q.Get(key); got != want {
			t.Errorf("param %q = %q, want %q", key, got, want)
		}
	}

}

func TestNewState(t *testing.T) {
	s, err := NewState()
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	if len(s) < 22 || len(s) > 128 {
		t.Errorf("len(state) = %d, want between 22 and 128", len(s))
	}
}

// TestAuthURLScope protege contra um scope esquecido: o login funcionaria, mas
// uma rota do Spotify devolveria 403 mais tarde, longe da causa.
//
// A lista esperada é escrita À MÃO (e não lida da variável `scopes` do pacote):
// se alguém removesse um scope da variável, comparar com ela passaria sem
// avisar.
func TestAuthURLScope(t *testing.T) {
	a := NewAuth("my-client", "http://127.0.0.1:8080/callback")

	u, err := url.Parse(a.AuthURL("my-state", "my-challenge"))
	if err != nil {
		t.Fatalf("AuthURL() returned an invalid URL: %v", err)
	}

	// O scope é UM texto com espaços; Fields transforma em lista.
	got := strings.Fields(u.Query().Get("scope"))
	want := []string{
		"user-read-currently-playing",
		"user-read-recently-played",
		"user-top-read",
	}

	// Ordena as duas listas: a ordem dos scopes não importa para o Spotify.
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("scopes = %v, want %v", got, want)
	}
}

// TestAuthURLEscapesRedirectURI prova que o url.Values fez o escape: um
// redirect_uri com "?" e "&" tem que voltar INTACTO, e o "&y=2" não pode
// virar um parâmetro separado da URL de login.
func TestAuthURLEscapesRedirectURI(t *testing.T) {
	const redirect = "http://127.0.0.1:8080/callback?x=1&y=2"
	a := NewAuth("my-client", redirect)

	u, err := url.Parse(a.AuthURL("my-state", "my-challenge"))
	if err != nil {
		t.Fatalf("AuthURL() returned an invalid URL: %v", err)
	}
	q := u.Query()

	if got := q.Get("redirect_uri"); got != redirect {
		t.Errorf("redirect_uri = %q, want %q", got, redirect)
	}
	if q.Has("y") {
		t.Errorf("param y leaked into the login URL: redirect_uri was not escaped")
	}
}

func TestNewAuth(t *testing.T) {
	a := NewAuth("my-client", "http://127.0.0.1:8080/callback")

	if a.ClientID != "my-client" {
		t.Errorf("ClientID = %q, want my-client", a.ClientID)
	}
	if a.RedirectURI != "http://127.0.0.1:8080/callback" {
		t.Errorf("RedirectURI = %q, want http://127.0.0.1:8080/callback", a.RedirectURI)
	}
	if a.TokenURL != "https://accounts.spotify.com/api/token" {
		t.Errorf("TokenURL = %q, want https://accounts.spotify.com/api/token", a.TokenURL)
	}

	// Fatal (e não Error): se HTTP for nil, a linha de baixo daria pânico.
	if a.HTTP == nil {
		t.Fatal("HTTP client is nil")
	}
	// Sem timeout nada dá erro, mas um Spotify lento travaria o servidor para sempre.
	if a.HTTP.Timeout <= 0 {
		t.Errorf("HTTP.Timeout = %v, want > 0", a.HTTP.Timeout)
	}

}

func newTestAuth(t *testing.T, handler http.HandlerFunc) *Auth {

	t.Helper()

	server := httptest.NewServer(handler)

	a := NewAuth("my-client", "http://127.0.0.1:8080/callback")

	a.TokenURL = server.URL

	t.Cleanup(server.Close)

	return a
}

func TestExchangeCode(t *testing.T) {

	const (
		clientID    = "my-client"
		redirectURI = "http://127.0.0.1:8080/callback"
		verifier    = "my-verifier"
		code        = "my-code"
	)

	a := newTestAuth(t, func(w http.ResponseWriter, r *http.Request) {
		// 1. O QUE O SEU CÓDIGO ENVIOU (use t.Errorf, nunca t.Fatalf aqui)
		// o método é POST?              r.Method
		//  Content-Type é form?          r.Header.Get("Content-Type")
		method := r.Method
		contentType := r.Header.Get("Content-Type")

		if method != "POST" {
			t.Errorf("método HTTP = %q, queremos POST", method)
		}

		if contentType != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, queremos application/x-www-form-urlencoded", contentType)
		}

		r.ParseForm()
		// conferir os 5 campos em r.PostForm.Get(...):
		//       grant_type, code, redirect_uri, client_id, code_verifier

		want := map[string]string{
			"grant_type":    "authorization_code",
			"code":          code,
			"redirect_uri":  redirectURI,
			"client_id":     clientID,
			"code_verifier": verifier,
		}

		for key, wantValue := range want {
			if got := r.PostForm.Get(key); got != wantValue {
				t.Errorf("campo %q =  %q queremos %q", key, got, wantValue)
			}
		}

		// 2. A RESPOSTA DO "SPOTIFY"
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"acc","refresh_token":"ref","expires_in":3600}`))
	})

	before := time.Now()
	token, err := a.ExchangeCode(context.Background(), "my-code", "my-verifier")
	after := time.Now()
	if err != nil {
		t.Fatalf("ExchangeCode() error = %v", err)
	}

	// conferir tok.AccessToken e tok.RefreshToken
	if token.AccessToken != "acc" {
		t.Errorf("AccessToken error = %v", err)
	}
	if token.RefreshToken != "ref" {
		t.Errorf("RefreshToken error = %v", err)
	}
	// ExpiresAt ~1h à frente (use a técnica before/after do TestNewToken)
	earlist := before.Add(time.Hour)
	latest := after.Add(time.Hour)

	if token.ExpiresAt.Before(earlist) || token.ExpiresAt.After(latest) {
		t.Errorf("ExpiresAt = %v, want between %v and %v", token.ExpiresAt, earlist, latest)
	}
}

func TestRefresh(t *testing.T) {
	const oldRefresh = "refresh-antigo"

	tests := []struct {
		name        string
		body        string // o JSON que o servidor falso devolve
		wantRefresh string // o RefreshToken esperado no resultado
	}{
		{
			name:        "server sends a new refresh token",
			body:        `{"access_token":"novo","refresh_token":"refresh-novo","expires_in":3600}`,
			wantRefresh: "refresh-novo",
		},
		//  o caso em que o servidor OMITE o refresh_token
		//       (esperado: continua oldRefresh)
		{
			name:        "server omits the refresh token: keep the old one",
			body:        `{"access_token":"novo","expires_in":3600}`,
			wantRefresh: oldRefresh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAuth(t, func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				//  mapa com os 3 campos esperados + loop (igual ao ExchangeCode)

				want := map[string]string{
					"grant_type":    "refresh_token",
					"refresh_token": oldRefresh,
					"client_id":     "my-client",
				}
				for key, wantedValue := range want {
					if got := r.PostForm.Get(key); got != wantedValue {
						t.Errorf("campo %q =  %q queremos %q", key, got, wantedValue)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tt.body))
			})

			token, err := a.Refresh(context.Background(), oldRefresh)
			if err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}

			//  conferir token.AccessToken ("novo")
			if token.AccessToken != "novo" {
				t.Errorf("AccessToken = %q, want novo", token.AccessToken)
			}

			//  conferir token.RefreshToken == tt.wantRefresh
			if token.RefreshToken != tt.wantRefresh {
				t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, tt.wantRefresh)
			}
		})
	}
}

// TestRequestTokenError cobre o caminho de FALHA: o Spotify recusa o pedido
// (ex.: code já usado ou expirado => 400 invalid_grant).
//
// O que prova:
//   - o erro chega a quem chamou (err != nil) em vez de virar um Token vazio;
//   - a mensagem carrega o status e o corpo do Spotify. Sem isso, o /callback
//     só diria "falhou" e você não saberia se o problema é o code, o verifier
//     ou o redirect_uri.
func TestRequestTokenError(t *testing.T) {
	a := newTestAuth(t, func(w http.ResponseWriter, r *http.Request) {
		// A ORDEM importa: o status vem ANTES do corpo. O primeiro Write fixa
		// o status em 200, e um WriteHeader depois dele seria ignorado.
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	token, err := a.ExchangeCode(context.Background(), "my-code", "my-verifier")
	if err == nil {
		// Fatal: sem erro não há mensagem para inspecionar abaixo.
		t.Fatalf("ExchangeCode() error = nil, want an error (got token %+v)", token)
	}

	// Em caso de erro, o Token devolvido deve ser o valor zero: nunca um token
	// "meio preenchido" que alguém possa salvar por engano.
	if token != (Token{}) {
		t.Errorf("token = %+v, want the zero Token on error", token)
	}

	// O erro deve dizer o status e repetir o que o Spotify explicou.
	for _, want := range []string{"400", "invalid_grant"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// TestRequestTokenInvalidJSON cobre outro caminho de falha: o Spotify responde
// 200 (sucesso), mas o corpo não é um JSON válido (ex.: uma página de erro de
// um proxy no meio do caminho). O Decode falha, e isso tem que virar um erro,
// e não um Token vazio que parece válido.
func TestRequestTokenInvalidJSON(t *testing.T) {
	a := newTestAuth(t, func(w http.ResponseWriter, r *http.Request) {
		// Sem WriteHeader: o status padrão já é 200.
		w.Write([]byte(`not json`))
	})

	token, err := a.Refresh(context.Background(), "refresh-antigo")
	if err == nil {
		t.Fatalf("Refresh() error = nil, want a decoding error (got token %+v)", token)
	}

	if token != (Token{}) {
		t.Errorf("token = %+v, want the zero Token on error", token)
	}
}
