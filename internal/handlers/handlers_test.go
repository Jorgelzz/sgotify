// Testes dos handlers. Usam httptest.NewRecorder (um ResponseWriter que grava a
// resposta em memória) e "falsos" no lugar dos services: sem rede, sem banco,
// sem servidor de verdade.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jorgelzz/sgotify/internal/service"
	"github.com/jorgelzz/sgotify/internal/spotify"
)

// do executa o handler com um GET em target e devolve a resposta gravada.
func do(h http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// decode lê o corpo JSON da resposta em um map genérico.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	return m
}

func TestHealth(t *testing.T) {
	rec := do(http.HandlerFunc(Health), "/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := decode(t, rec)["status"]; got != "ok" {
		t.Errorf("status field = %v, want ok", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// ---------- now-playing ----------

type fakeNowPlaying struct {
	np  service.NowPlaying
	err error
}

func (f fakeNowPlaying) Get(context.Context) (service.NowPlaying, error) { return f.np, f.err }

func TestNowPlayingPlaying(t *testing.T) {
	h := NowPlaying(fakeNowPlaying{np: service.NowPlaying{
		Playing: true, Track: "Song", Artist: "Band", Image: "http://img",
		ProgressMS: 1000, DurationMS: 200000,
	}})
	rec := do(h, "/api/now-playing")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	m := decode(t, rec)
	want := map[string]any{
		"playing": true, "track": "Song", "artist": "Band", "image": "http://img",
		"progress_ms": float64(1000), "duration_ms": float64(200000),
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
}

// Pausado/nada tocando: SÓ {"playing":false}, sem os outros campos (README).
func TestNowPlayingNotPlaying(t *testing.T) {
	// Pausado: o service ainda preenche a faixa, mas a resposta não pode vazá-la.
	h := NowPlaying(fakeNowPlaying{np: service.NowPlaying{Playing: false, Track: "Paused song"}})
	rec := do(h, "/api/now-playing")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	m := decode(t, rec)
	if len(m) != 1 || m["playing"] != false {
		t.Errorf("body = %v, want only {playing:false}", m)
	}
}

func TestNowPlayingErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantLogin  bool
	}{
		{"not logged in", spotify.ErrNoToken, http.StatusUnauthorized, true},
		{"not logged in, wrapped", fmt.Errorf("get token: %w", spotify.ErrNoToken), http.StatusUnauthorized, true},
		{"spotify down", errors.New("boom: secret detail"), http.StatusBadGateway, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(NowPlaying(fakeNowPlaying{err: tt.err}), "/api/now-playing")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			m := decode(t, rec)
			if (m["login_url"] != nil) != tt.wantLogin {
				t.Errorf("login_url = %v, wantLogin %v", m["login_url"], tt.wantLogin)
			}
			if strings.Contains(rec.Body.String(), "secret detail") {
				t.Errorf("internal error leaked to client: %s", rec.Body.String())
			}
		})
	}
}

// ---------- top-week ----------

type fakeWeek struct {
	tracks []service.TrackCount
	err    error
	limit  int // último limit recebido
}

func (f *fakeWeek) Week(_ context.Context, limit int) ([]service.TrackCount, error) {
	f.limit = limit
	return f.tracks, f.err
}

func TestTopWeek(t *testing.T) {
	svc := &fakeWeek{tracks: []service.TrackCount{
		{TrackID: "internal-id", Name: "A", Artist: "X", ImageURL: "http://a", Plays: 3},
	}}
	rec := do(TopWeek(svc), "/api/top-week")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if svc.limit != 10 {
		t.Errorf("default limit = %d, want 10", svc.limit)
	}
	var body struct {
		Tracks []map[string]any `json:"tracks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tracks) != 1 {
		t.Fatalf("tracks = %v", body.Tracks)
	}
	got := body.Tracks[0]
	if got["track"] != "A" || got["artist"] != "X" || got["image"] != "http://a" || got["plays"] != float64(3) {
		t.Errorf("track = %v", got)
	}
	if _, leaked := got["TrackID"]; leaked || strings.Contains(rec.Body.String(), "internal-id") {
		t.Errorf("TrackID leaked: %s", rec.Body.String())
	}
}

func TestTopWeekLimit(t *testing.T) {
	tests := []struct {
		query     string
		wantCode  int
		wantLimit int
	}{
		{"", 200, 10},
		{"?limit=1", 200, 1},
		{"?limit=50", 200, 50},
		{"?limit=0", 400, 0},
		{"?limit=51", 400, 0},
		{"?limit=-3", 400, 0},
		{"?limit=abc", 400, 0},
		{"?limit=", 200, 10}, // vazio = não informado
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			svc := &fakeWeek{}
			rec := do(TopWeek(svc), "/api/top-week"+tt.query)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if svc.limit != tt.wantLimit {
				t.Errorf("limit passed to service = %d, want %d", svc.limit, tt.wantLimit)
			}
		})
	}
}

// Sem plays: "tracks":[] e não null.
func TestTopWeekEmptyIsList(t *testing.T) {
	rec := do(TopWeek(&fakeWeek{}), "/api/top-week")
	if !strings.Contains(rec.Body.String(), `"tracks":[]`) {
		t.Errorf("body = %s, want empty list", rec.Body.String())
	}
}

func TestTopWeekStoreError(t *testing.T) {
	rec := do(TopWeek(&fakeWeek{err: errors.New("db down")}), "/api/top-week")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// ---------- calendar ----------

type fakeMonth struct {
	days  []service.DayTop
	err   error
	year  int
	month time.Month
	calls int
}

func (f *fakeMonth) Month(_ context.Context, year int, month time.Month) ([]service.DayTop, error) {
	f.calls++
	f.year, f.month = year, month
	return f.days, f.err
}

func TestCalendar(t *testing.T) {
	loc := time.FixedZone("BRT", -3*3600)
	svc := &fakeMonth{days: []service.DayTop{
		{Date: "2026-11-24", TrackID: "id", Name: "A", Artist: "X", ImageURL: "http://a", Plays: 4},
	}}
	rec := do(Calendar(svc, loc), "/api/calendar?month=2026-11")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if svc.year != 2026 || svc.month != time.November {
		t.Errorf("service got %d-%d", svc.year, svc.month)
	}
	var body struct {
		Month string           `json:"month"`
		Days  []map[string]any `json:"days"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Month != "2026-11" || len(body.Days) != 1 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	d := body.Days[0]
	if d["date"] != "2026-11-24" || d["track"] != "A" || d["artist"] != "X" ||
		d["image"] != "http://a" || d["plays"] != float64(4) {
		t.Errorf("day = %v", d)
	}
}

func TestCalendarInvalidMonth(t *testing.T) {
	for _, m := range []string{"2026-13", "2026-1", "202611", "abc", "2026-11-01", "2026-00"} {
		t.Run(m, func(t *testing.T) {
			svc := &fakeMonth{}
			rec := do(Calendar(svc, time.UTC), "/api/calendar?month="+url.QueryEscape(m))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if svc.calls != 0 {
				t.Error("service must not be called with an invalid month")
			}
		})
	}
}

// Sem ?month: usa o mês ATUAL no fuso configurado. O relógio é fixado em um
// instante em que UTC e o fuso local estão em MESES diferentes: 02:00 UTC de
// 1/dez ainda é 30/nov em São Paulo (UTC-3).
func TestCalendarDefaultMonthUsesConfiguredTimezone(t *testing.T) {
	loc := time.FixedZone("BRT", -3*3600)
	now := func() time.Time { return time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC) }
	svc := &fakeMonth{}

	rec := do(calendarHandler(svc, loc, now), "/api/calendar")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if svc.year != 2026 || svc.month != time.November {
		t.Errorf("service got %d-%d, want 2026-11", svc.year, svc.month)
	}
	if got := decode(t, rec)["month"]; got != "2026-11" {
		t.Errorf("month = %v", got)
	}
}

// Mês sem plays: "days":[] e não null.
func TestCalendarEmptyIsList(t *testing.T) {
	rec := do(Calendar(&fakeMonth{}, time.UTC), "/api/calendar?month=2026-11")
	if !strings.Contains(rec.Body.String(), `"days":[]`) {
		t.Errorf("body = %s, want empty list", rec.Body.String())
	}
}

func TestCalendarStoreError(t *testing.T) {
	rec := do(Calendar(&fakeMonth{err: errors.New("db down")}, time.UTC), "/api/calendar?month=2026-11")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// ---------- login ----------

type fakeAuth struct {
	token        spotify.Token
	exchangeErr  error
	gotCode      string
	gotVerifier  string
	gotState     string
	gotChallenge string
}

func (f *fakeAuth) AuthURL(state, challenge string) string {
	f.gotState, f.gotChallenge = state, challenge
	return "https://accounts.example/authorize?state=" + state
}

func (f *fakeAuth) ExchangeCode(_ context.Context, code, verifier string) (spotify.Token, error) {
	f.gotCode, f.gotVerifier = code, verifier
	return f.token, f.exchangeErr
}

type memTokens struct {
	saved   *spotify.Token
	saveErr error
}

func (m *memTokens) Save(_ context.Context, t spotify.Token) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = &t
	return nil
}

func (m *memTokens) Load(context.Context) (spotify.Token, error) {
	if m.saved == nil {
		return spotify.Token{}, spotify.ErrNoToken
	}
	return *m.saved, nil
}

func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLogin(t *testing.T) {
	fa := &fakeAuth{}
	h := NewAuthHandler(fa, &memTokens{}, "http://front")
	rec := do(http.HandlerFunc(h.Login), "/login")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://accounts.example/authorize") {
		t.Errorf("Location = %q", loc)
	}

	state := cookieByName(rec, stateCookie)
	verifier := cookieByName(rec, verifierCookie)
	if state == nil || verifier == nil {
		t.Fatal("login cookies not set")
	}
	for _, c := range []*http.Cookie{state, verifier} {
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/callback" || c.MaxAge <= 0 {
			t.Errorf("cookie %s has weak attributes: %+v", c.Name, c)
		}
	}
	// O state do cookie é o mesmo enviado ao Spotify; ao Spotify vai o
	// CHALLENGE, nunca o verifier.
	if state.Value != fa.gotState {
		t.Errorf("state cookie %q != state sent %q", state.Value, fa.gotState)
	}
	if fa.gotChallenge != spotify.ChallengeFor(verifier.Value) {
		t.Error("challenge sent does not match the verifier cookie")
	}
	if strings.Contains(loc, verifier.Value) {
		t.Error("verifier leaked into the redirect URL")
	}
}

// Cada login gera valores novos.
func TestLoginRandomEachTime(t *testing.T) {
	h := NewAuthHandler(&fakeAuth{}, &memTokens{}, "http://front")
	a := cookieByName(do(http.HandlerFunc(h.Login), "/login"), stateCookie)
	b := cookieByName(do(http.HandlerFunc(h.Login), "/login"), stateCookie)
	if a.Value == b.Value {
		t.Error("state repeated between logins")
	}
}

// callback faz um GET /callback com os cookies e a query dados.
func callback(h *AuthHandler, query string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/callback"+query, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	return rec
}

func loginCookies() []*http.Cookie {
	return []*http.Cookie{
		{Name: stateCookie, Value: "state-123"},
		{Name: verifierCookie, Value: "verifier-abc"},
	}
}

func TestCallbackSuccess(t *testing.T) {
	want := spotify.Token{AccessToken: "acc", RefreshToken: "ref", ExpiresAt: time.Now().Add(time.Hour)}
	fa := &fakeAuth{token: want}
	store := &memTokens{}
	h := NewAuthHandler(fa, store, "http://front")

	rec := callback(h, "?code=the-code&state=state-123", loginCookies()...)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "http://front" {
		t.Fatalf("status=%d location=%q, want 302 to front", rec.Code, rec.Header().Get("Location"))
	}
	if fa.gotCode != "the-code" || fa.gotVerifier != "verifier-abc" {
		t.Errorf("exchange got code=%q verifier=%q", fa.gotCode, fa.gotVerifier)
	}
	if store.saved == nil || *store.saved != want {
		t.Errorf("saved = %+v, want %+v", store.saved, want)
	}
	// Cookies apagados (MaxAge < 0) depois do uso.
	for _, name := range []string{stateCookie, verifierCookie} {
		if c := cookieByName(rec, name); c == nil || c.MaxAge >= 0 {
			t.Errorf("cookie %s not cleared: %+v", name, c)
		}
	}
}

func TestCallbackRejections(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		cookies    []*http.Cookie
		exchange   error
		saveErr    error
		wantStatus int
	}{
		{"user denied", "?error=access_denied&state=state-123", loginCookies(), nil, nil, 400},
		{"no cookies", "?code=c&state=state-123", nil, nil, nil, 400},
		{"only state cookie", "?code=c&state=state-123", loginCookies()[:1], nil, nil, 400},
		{"state mismatch", "?code=c&state=other", loginCookies(), nil, nil, 400},
		{"missing state", "?code=c", loginCookies(), nil, nil, 400},
		{"missing code", "?state=state-123", loginCookies(), nil, nil, 400},
		{"exchange fails", "?code=c&state=state-123", loginCookies(), errors.New("spotify said no"), nil, 502},
		{"save fails", "?code=c&state=state-123", loginCookies(), nil, errors.New("db down"), 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fa := &fakeAuth{exchangeErr: tt.exchange}
			store := &memTokens{saveErr: tt.saveErr}
			h := NewAuthHandler(fa, store, "http://front")

			rec := callback(h, tt.query, tt.cookies...)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if store.saved != nil {
				t.Error("a token was saved on a rejected login")
			}
			// Antes de validar o state, o code NÃO pode ser trocado por tokens.
			if strings.HasPrefix(tt.name, "state") || tt.name == "no cookies" || tt.name == "missing state" {
				if fa.gotCode != "" {
					t.Error("code was exchanged despite failing validation")
				}
			}
			if strings.Contains(rec.Body.String(), "spotify said no") ||
				strings.Contains(rec.Body.String(), "db down") {
				t.Errorf("internal error leaked: %s", rec.Body.String())
			}
		})
	}
}
