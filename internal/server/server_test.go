// Testes do roteamento e do CORS. O servidor é montado com handlers "marcadores"
// (cada um responde só o próprio nome), para provar QUAL handler atendeu.
package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jorgelzz/sgotify/internal/handlers"
)

const frontOrigin = "http://front.example"

// marker devolve um handler que responde com o texto dado.
func marker(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(name))
	})
}

// newTestServer monta o servidor com marcadores. O AuthHandler real (sem
// dependências) serve para /login e /callback: aqui só importa o roteamento.
func newTestServer() http.Handler {
	return New(Routes{
		Auth:       handlers.NewAuthHandler(nil, nil, "http://front"),
		NowPlaying: marker("now-playing"),
		TopWeek:    marker("top-week"),
		Calendar:   marker("calendar"),
	}, frontOrigin)
}

func request(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestRoutes(t *testing.T) {
	h := newTestServer()
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
			rec := request(h, http.MethodGet, tt.target)
			if rec.Code != http.StatusOK || rec.Body.String() != tt.wantBody {
				t.Errorf("got %d %q, want 200 %q", rec.Code, rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestHealthRoute(t *testing.T) {
	rec := request(newTestServer(), http.MethodGet, "/health")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}

// /login redireciona (302), /callback sem cookies é 400: prova que ambos estão
// ligados ao AuthHandler. (/login com Auth sem dependências usaria a URL do
// Spotify, então só checamos /callback, que falha ANTES de usá-las.)
func TestCallbackRouteIsWired(t *testing.T) {
	rec := request(newTestServer(), http.MethodGet, "/callback?code=x&state=y")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (no login cookies)", rec.Code)
	}
}

// ESCOPO: não existe "GET /" nem rota fora da tabela do README.
func TestNoExtraRoutes(t *testing.T) {
	h := newTestServer()
	for _, target := range []string{"/", "/index.html", "/api", "/api/top-month", "/api/player/pause", "/static/app.js"} {
		if rec := request(h, http.MethodGet, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}

// A API só lê: outros métodos são recusados (405).
func TestOnlyGET(t *testing.T) {
	h := newTestServer()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if rec := request(h, method, "/api/now-playing"); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rec.Code)
		}
	}
}

func TestCORSHeaders(t *testing.T) {
	rec := request(newTestServer(), http.MethodGet, "/api/now-playing")
	got := rec.Header().Get("Access-Control-Allow-Origin")
	if got != frontOrigin {
		t.Errorf("Allow-Origin = %q, want %q", got, frontOrigin)
	}
	if got == "*" {
		t.Error("CORS must never be *")
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Errorf("Vary = %q, want Origin", rec.Header().Get("Vary"))
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
	if m := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(m, "GET") {
		t.Errorf("Allow-Methods = %q", m)
	}
}

// O preflight funciona pelo servidor completo (o mux só registra GET: sem o
// CORS por fora, isto seria 405).
func TestPreflightThroughServer(t *testing.T) {
	rec := request(newTestServer(), http.MethodOptions, "/api/top-week")
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

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
