// Testes do roteamento, da sessão e do CORS. O servidor é montado com handlers
// "marcadores" (cada um responde só o próprio nome), para provar QUAL handler
// atendeu, e com um session.Manager REAL: o cookie de teste é emitido pelo
// mesmo código que emite o de produção.
package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jorgelzz/sgotify/internal/handlers"
	"github.com/jorgelzz/sgotify/internal/session"
	"github.com/jorgelzz/sgotify/internal/spotify"
)

const (
	frontOrigin = "http://front.example"
	testSecret  = "0123456789abcdef0123456789abcdef"
)

// marker devolve um handler que responde com o texto dado.
func marker(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(name))
	})
}

// stubAuth/stubIdentity/stubSessions satisfazem as dependências do
// AuthHandler. Aqui só importa o ROTEAMENTO, então não fazem nada de útil.
type stubAuth struct{}

func (stubAuth) AuthURL(state, challenge string) string { return "https://accounts.example/authorize" }
func (stubAuth) ExchangeCode(context.Context, string, string) (spotify.Token, error) {
	return spotify.Token{}, nil
}

type stubIdentity struct{}

func (stubIdentity) UserID(context.Context, string) (string, error) { return "", nil }

type stubTokens struct{}

func (stubTokens) Save(context.Context, spotify.Token) error   { return nil }
func (stubTokens) Load(context.Context) (spotify.Token, error) { return spotify.Token{}, nil }

// newSessions cria o session.Manager real usado nos testes.
func newSessions(t *testing.T, secret string) *session.Manager {
	t.Helper()
	m, err := session.New(secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// sessionCookie emite um cookie de sessão legítimo com o manager dado.
func sessionCookie(t *testing.T, m *session.Manager) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Issue(rec)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Issue set %d cookies, want 1", len(cookies))
	}
	return cookies[0]
}

// newTestServer monta o servidor com marcadores e a sessão dada.
func newTestServer(sessions *session.Manager) http.Handler {
	return New(Routes{
		Auth:       handlers.NewAuthHandler(stubAuth{}, stubIdentity{}, stubTokens{}, sessions, "me", "http://front"),
		Sessions:   sessions,
		NowPlaying: marker("now-playing"),
		TopWeek:    marker("top-week"),
		Calendar:   marker("calendar"),
	}, frontOrigin)
}

// request faz uma requisição (com cookies opcionais) e devolve a resposta.
func request(h http.Handler, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ---------- roteamento (COM sessão válida) ----------

func TestRoutes(t *testing.T) {
	m := newSessions(t, testSecret)
	h := newTestServer(m)
	cookie := sessionCookie(t, m)

	tests := []struct {
		target   string
		wantBody string
	}{
		{"/api/now-playing", "now-playing"},
		{"/api/top-week", "top-week"},
		{"/api/top-week?limit=5", "top-week"},
		{"/api/calendar?month=2026-11", "calendar"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			rec := request(h, http.MethodGet, tt.target, cookie)
			if rec.Code != http.StatusOK || rec.Body.String() != tt.wantBody {
				t.Errorf("got %d %q, want 200 %q", rec.Code, rec.Body.String(), tt.wantBody)
			}
		})
	}
}

// ESCOPO: não existe "GET /" nem rota fora da tabela do guia.
func TestNoExtraRoutes(t *testing.T) {
	m := newSessions(t, testSecret)
	h := newTestServer(m)
	cookie := sessionCookie(t, m)
	for _, target := range []string{"/", "/index.html", "/api", "/api/top-month", "/api/player/pause", "/static/app.js", "/logout"} {
		// Com sessão válida: se existisse, responderia. Tem que ser 404.
		if rec := request(h, http.MethodGet, target, cookie); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}

// A API só lê: outros métodos são recusados (405), mesmo com sessão.
func TestOnlyGET(t *testing.T) {
	m := newSessions(t, testSecret)
	h := newTestServer(m)
	cookie := sessionCookie(t, m)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if rec := request(h, method, "/api/now-playing", cookie); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rec.Code)
		}
	}
}

// ---------- AUTENTICAÇÃO das rotas /api/* ----------

var apiRoutes = []string{"/api/now-playing", "/api/top-week", "/api/calendar?month=2026-11"}

// Sem cookie: 401 com o login_url, e o handler NÃO é chamado.
func TestAPIRequiresSession(t *testing.T) {
	h := newTestServer(newSessions(t, testSecret))
	for _, target := range apiRoutes {
		t.Run(target, func(t *testing.T) {
			rec := request(h, http.MethodGet, target)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("401 body is not JSON: %v", err)
			}
			if body["login_url"] != "/login" || body["error"] == "" {
				t.Errorf("body = %v, want error and login_url=/login", body)
			}
			if strings.Contains(rec.Body.String(), "top-week") || strings.Contains(rec.Body.String(), "now-playing") {
				t.Error("the protected handler ran without a session")
			}
		})
	}
}

// Cookies que NÃO valem: adulterado, de outro segredo, vencido, lixo.
func TestAPIRejectsInvalidSessions(t *testing.T) {
	m := newSessions(t, testSecret)
	h := newTestServer(m)

	good := sessionCookie(t, m)
	tampered := &http.Cookie{Name: session.CookieName, Value: good.Value[:len(good.Value)-2] + "xx"}

	otherManager := newSessions(t, "fedcba9876543210fedcba9876543210")
	foreign := sessionCookie(t, otherManager)

	// Vencido: assinado com o segredo CERTO, mas com a validade no passado.
	expired := handSignedCookie(testSecret, time.Now().Add(-time.Minute))

	tests := map[string]*http.Cookie{
		"tampered":                    tampered,
		"other secret":                foreign,
		"expired":                     expired,
		"garbage":                     {Name: session.CookieName, Value: "not-a-session"},
		"empty":                       {Name: session.CookieName, Value: ""},
		"right value, wrong cookie":   {Name: "sgotify_state", Value: good.Value},
		"right value, another cookie": {Name: "sgotify_verifier", Value: good.Value},
	}
	for name, c := range tests {
		t.Run(name, func(t *testing.T) {
			if rec := request(h, http.MethodGet, "/api/top-week", c); rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

// handSignedCookie monta o cookie à mão, só com crypto/hmac, seguindo o formato
// documentado em session.go: "<validade unix>.<base64url(HMAC-SHA256(validade))>".
// Não usa o código do pacote session: é uma implementação INDEPENDENTE do mesmo
// formato.
func handSignedCookie(secret string, expires time.Time) *http.Cookie {
	payload := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return &http.Cookie{Name: session.CookieName, Value: payload + "." + sig}
}

// Um cookie assinado à mão, com o segredo certo e validade futura, é aceito:
// confirma que o formato real é o documentado (HMAC-SHA256 padrão), e não algo
// que só o próprio pacote entende.
func TestAPIAcceptsIndependentlySignedCookie(t *testing.T) {
	h := newTestServer(newSessions(t, testSecret))
	c := handSignedCookie(testSecret, time.Now().Add(time.Hour))
	if rec := request(h, http.MethodGet, "/api/top-week", c); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// As rotas abertas NÃO exigem sessão: /health, /login e /callback.
func TestOpenRoutes(t *testing.T) {
	h := newTestServer(newSessions(t, testSecret))

	if rec := request(h, http.MethodGet, "/health"); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("/health = %d %q", rec.Code, rec.Body.String())
	}
	if rec := request(h, http.MethodGet, "/login"); rec.Code != http.StatusFound {
		t.Errorf("/login = %d, want 302 (open, redirects to Spotify)", rec.Code)
	}
	// /callback sem cookies de login: 400 do próprio handler, e NÃO 401 do
	// middleware (a rota é aberta; é por ela que se ganha a sessão).
	if rec := request(h, http.MethodGet, "/callback?code=x&state=y"); rec.Code != http.StatusBadRequest {
		t.Errorf("/callback = %d, want 400 from the handler (not 401)", rec.Code)
	}
}

// O middleware sozinho: sessão inválida não chama o próximo handler; válida chama.
func TestRequireSessionGate(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ })

	m := newSessions(t, testSecret)
	gate := RequireSession(m)(next)

	request(gate, http.MethodGet, "/x")
	if called != 0 {
		t.Fatal("next handler ran without a session")
	}
	request(gate, http.MethodGet, "/x", sessionCookie(t, m))
	if called != 1 {
		t.Errorf("next handler called %d times with a valid session, want 1", called)
	}
}

// ---------- CORS ----------

func TestCORSHeaders(t *testing.T) {
	m := newSessions(t, testSecret)
	rec := request(newTestServer(m), http.MethodGet, "/api/now-playing", sessionCookie(t, m))

	got := rec.Header().Get("Access-Control-Allow-Origin")
	if got != frontOrigin {
		t.Errorf("Allow-Origin = %q, want %q", got, frontOrigin)
	}
	if got == "*" {
		t.Error("CORS must never be *")
	}
	// Com cookie de sessão, o browser só entrega a resposta ao front se a API
	// declarar Allow-Credentials (e com origem explícita, como acima).
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("Allow-Credentials = %q, want true", rec.Header().Get("Access-Control-Allow-Credentials"))
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Errorf("Vary = %q, want Origin", rec.Header().Get("Vary"))
	}
}

// O 401 também leva CORS: sem isso, o front não conseguiria LER o 401 e saber
// que precisa mandar o usuário para o /login.
func TestUnauthorizedHasCORS(t *testing.T) {
	rec := request(newTestServer(newSessions(t, testSecret)), http.MethodGet, "/api/now-playing")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != frontOrigin ||
		rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("401 without CORS headers: %v", rec.Header())
	}
}

// Preflight: OPTIONS => 204, sem corpo, com os headers; e NÃO chega ao handler.
func TestCORSPreflight(t *testing.T) {
	called := false
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := CORS(frontOrigin)(probe)

	rec := request(h, http.MethodOptions, "/api/calendar")
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("preflight body = %q, want empty", rec.Body.String())
	}
	if called {
		t.Error("preflight reached the next handler")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != frontOrigin {
		t.Error("preflight without Allow-Origin")
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("preflight without Allow-Credentials")
	}
	if m := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(m, "GET") {
		t.Errorf("Allow-Methods = %q", m)
	}
}

// O preflight funciona pelo servidor completo e SEM sessão: o browser o envia
// sem cookies, e o mux só registra GET (sem o CORS por fora, seria 405).
func TestPreflightThroughServerWithoutSession(t *testing.T) {
	rec := request(newTestServer(newSessions(t, testSecret)), http.MethodOptions, "/api/top-week")
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 (not 401)", rec.Code)
	}
}

// ---------- Logger ----------

// O Logger registra método, caminho e status, mas NUNCA a query (no /callback
// ela traz o code do OAuth).
func TestLoggerOmitsQuery(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer() // guardado ANTES de trocar, para restaurar depois
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })

	h := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	request(h, http.MethodGet, "/callback?code=SECRET&state=ALSO")

	out := buf.String()
	if !strings.Contains(out, "GET /callback -> 418") {
		t.Errorf("log = %q, want method, path and status", out)
	}
	if strings.Contains(out, "SECRET") || strings.Contains(out, "ALSO") {
		t.Errorf("query leaked into log: %q", out)
	}
}
