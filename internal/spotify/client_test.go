package spotify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTokenStore é um TokenStore em MEMÓRIA: o client não precisa de banco para
// ser testado, e é por isso que TokenStore é uma interface.
type fakeTokenStore struct {
	mu    sync.Mutex
	tok   Token
	has   bool
	saves int // quantas vezes Save foi chamado
}

func (f *fakeTokenStore) Save(_ context.Context, t Token) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tok, f.has = t, true
	f.saves++
	return nil
}

func (f *fakeTokenStore) Load(_ context.Context) (Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.has {
		return Token{}, ErrNoToken
	}
	return f.tok, nil
}

// newTestClient monta um Client que fala com um "Spotify de mentira":
//   - apiHandler responde as rotas da API (baseURL aponta para ele);
//   - o endpoint de TOKEN (renovação) também é falso: devolve "renovado" e
//     incrementa refreshes, para o teste contar quantas renovações ocorreram.
//
// Reaproveita o newTestAuth do auth_test.go.
func newTestClient(t *testing.T, store TokenStore, apiHandler http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()

	var refreshes atomic.Int32
	auth := newTestAuth(t, func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"renovado","expires_in":3600}`))
	})

	api := httptest.NewServer(apiHandler)
	t.Cleanup(api.Close)

	client := NewClient(auth, store)
	client.baseURL = api.URL
	return client, &refreshes
}

// validToken é um token que ainda tem 1h de vida.
func validToken() Token {
	return Token{AccessToken: "valido", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
}

func TestClientSendsBearerToken(t *testing.T) {
	store := &fakeTokenStore{tok: validToken(), has: true}
	client, refreshes := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer valido" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer valido")
		}
		w.Write([]byte(`{"items":[]}`))
	})

	if _, err := client.TopTracks(context.Background(), "short_term", 5); err != nil {
		t.Fatalf("TopTracks() error = %v", err)
	}
	if refreshes.Load() != 0 {
		t.Errorf("refreshes = %d, want 0 (token was still valid)", refreshes.Load())
	}
}

// Token expirado: o client renova ANTES de chamar a API, grava o novo e usa o
// novo token na chamada.
func TestClientRefreshesExpiredToken(t *testing.T) {
	store := &fakeTokenStore{
		tok: Token{AccessToken: "velho", RefreshToken: "refresh-antigo", ExpiresAt: time.Now().Add(-time.Minute)},
		has: true,
	}
	client, refreshes := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer renovado" {
			t.Errorf("Authorization = %q, want the REFRESHED token", got)
		}
		w.Write([]byte(`{"items":[]}`))
	})

	if _, err := client.TopTracks(context.Background(), "short_term", 5); err != nil {
		t.Fatalf("TopTracks() error = %v", err)
	}
	if refreshes.Load() != 1 {
		t.Errorf("refreshes = %d, want 1", refreshes.Load())
	}
	saved, _ := store.Load(context.Background())
	if saved.AccessToken != "renovado" {
		t.Errorf("saved AccessToken = %q, want renovado (the new token must be persisted)", saved.AccessToken)
	}
	// O servidor falso de token não devolve refresh_token: o Refresh deve ter
	// MANTIDO o antigo, senão perderíamos o acesso na próxima renovação.
	if saved.RefreshToken != "refresh-antigo" {
		t.Errorf("saved RefreshToken = %q, want refresh-antigo", saved.RefreshToken)
	}
}

// Várias chamadas simultâneas com o token expirado devem renová-lo UMA vez só:
// é exatamente para isso que existe o mutex do Client. (Rode com -race.)
func TestClientRefreshesOnlyOnceConcurrently(t *testing.T) {
	store := &fakeTokenStore{
		tok: Token{AccessToken: "velho", RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Minute)},
		has: true,
	}
	client, refreshes := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[]}`))
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.TopTracks(context.Background(), "short_term", 5); err != nil {
				t.Errorf("TopTracks() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if refreshes.Load() != 1 {
		t.Errorf("refreshes = %d, want exactly 1", refreshes.Load())
	}
}

// Sem token (nunca fez login): ErrNoToken chega intacto e NENHUMA chamada vai
// para a API.
func TestClientNoToken(t *testing.T) {
	var apiCalls atomic.Int32
	client, _ := newTestClient(t, &fakeTokenStore{}, func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
	})

	_, err := client.CurrentlyPlaying(context.Background())
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("error = %v, want ErrNoToken", err)
	}
	if apiCalls.Load() != 0 {
		t.Errorf("api calls = %d, want 0", apiCalls.Load())
	}
}

// 429 vira *APIError com o Retry-After lido do header.
func TestClientRateLimit(t *testing.T) {
	store := &fakeTokenStore{tok: validToken(), has: true}
	client, _ := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`rate limited`))
	})

	_, err := client.RecentlyPlayed(context.Background(), time.Time{})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusTooManyRequests {
		t.Errorf("Status = %d, want 429", apiErr.Status)
	}
	if apiErr.RetryAfter != 7*time.Second {
		t.Errorf("RetryAfter = %v, want 7s", apiErr.RetryAfter)
	}
	if !strings.Contains(apiErr.Error(), "rate limited") {
		t.Errorf("Error() = %q, want it to include the response body", apiErr.Error())
	}
}

// ------------------------------------------------------------- CurrentlyPlaying

const trackJSON = `{"id":"t1","name":"Song","duration_ms":200000,
	"artists":[{"name":"A"},{"name":"B"}],
	"album":{"images":[{"url":"big"},{"url":"mid"},{"url":"small"}]}}`

func TestCurrentlyPlaying(t *testing.T) {
	store := &fakeTokenStore{tok: validToken(), has: true}
	client, _ := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/player/currently-playing" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"is_playing":true,"progress_ms":1234,"currently_playing_type":"track","item":` + trackJSON + `}`))
	})

	np, err := client.CurrentlyPlaying(context.Background())
	if err != nil || np == nil {
		t.Fatalf("CurrentlyPlaying() = %v, %v", np, err)
	}
	if !np.IsPlaying || np.ProgressMS != 1234 || np.Track.Name != "Song" || np.Track.ID != "t1" {
		t.Errorf("got %+v", np)
	}
}

// Os 3 casos de "não há música para mostrar" devolvem (nil, nil), sem erro.
func TestCurrentlyPlayingNothing(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"nothing playing (204)", http.StatusNoContent, ""},
		{"item is null (ad)", http.StatusOK, `{"is_playing":true,"currently_playing_type":"ad","item":null}`},
		{"podcast episode", http.StatusOK, `{"is_playing":true,"currently_playing_type":"episode","item":{"id":"e1","name":"Ep"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeTokenStore{tok: validToken(), has: true}
			client, _ := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})

			np, err := client.CurrentlyPlaying(context.Background())
			if err != nil || np != nil {
				t.Errorf("CurrentlyPlaying() = %v, %v, want nil, nil", np, err)
			}
		})
	}
}

// -------------------------------------------------------------------- History

func TestRecentlyPlayed(t *testing.T) {
	store := &fakeTokenStore{tok: validToken(), has: true}
	after := time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)

	client, _ := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("limit"); got != "50" {
			t.Errorf("limit = %q, want 50", got)
		}
		// O Spotify quer MILISSEGUNDOS, não segundos.
		// 2026-11-24T15:00:00Z = 1795532400 s (conferido com `date -u -d`).
		if got, want := q.Get("after"), "1795532400000"; got != want {
			t.Errorf("after = %q, want %q", got, want)
		}
		w.Write([]byte(`{"items":[{"played_at":"2026-11-24T15:03:11.123Z","track":` + trackJSON + `}]}`))
	})

	items, err := client.RecentlyPlayed(context.Background(), after)
	if err != nil || len(items) != 1 {
		t.Fatalf("RecentlyPlayed() = %v, %v", items, err)
	}
	want := time.Date(2026, 11, 24, 15, 3, 11, 123_000_000, time.UTC)
	if !items[0].PlayedAt.Equal(want) {
		t.Errorf("PlayedAt = %v, want %v", items[0].PlayedAt, want)
	}
	if items[0].Track.ID != "t1" {
		t.Errorf("Track.ID = %q, want t1", items[0].Track.ID)
	}
}

// Sem "after" (primeira coleta), o parâmetro NÃO deve ser enviado.
func TestRecentlyPlayedWithoutAfter(t *testing.T) {
	store := &fakeTokenStore{tok: validToken(), has: true}
	client, _ := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("after") {
			t.Errorf("after was sent for the zero time: %q", r.URL.RawQuery)
		}
		w.Write([]byte(`{"items":[]}`))
	})

	if _, err := client.RecentlyPlayed(context.Background(), time.Time{}); err != nil {
		t.Fatalf("RecentlyPlayed() error = %v", err)
	}
}

// ------------------------------------------------------------------------ Top

func TestTopTracks(t *testing.T) {
	store := &fakeTokenStore{tok: validToken(), has: true}
	client, _ := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("time_range") != "medium_term" || q.Get("limit") != "3" {
			t.Errorf("query = %q, want time_range=medium_term&limit=3", r.URL.RawQuery)
		}
		w.Write([]byte(`{"items":[` + trackJSON + `]}`))
	})

	tracks, err := client.TopTracks(context.Background(), "medium_term", 3)
	if err != nil || len(tracks) != 1 || tracks[0].Name != "Song" {
		t.Fatalf("TopTracks() = %v, %v", tracks, err)
	}
}

// time_range inválido é recusado ANTES de qualquer chamada de rede.
func TestTopTracksInvalidRange(t *testing.T) {
	var apiCalls atomic.Int32
	store := &fakeTokenStore{tok: validToken(), has: true}
	client, _ := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
	})

	if _, err := client.TopTracks(context.Background(), "this_week", 5); err == nil {
		t.Fatal("TopTracks() error = nil, want an error for an invalid time_range")
	}
	if apiCalls.Load() != 0 {
		t.Errorf("api calls = %d, want 0", apiCalls.Load())
	}
}

// -------------------------------------------------------------- Track helpers

func TestTrackHelpers(t *testing.T) {
	tr := Track{
		Artists: []Artist{{Name: "A"}, {Name: "B"}},
		Album:   Album{Images: []Image{{URL: "big"}, {URL: "mid"}, {URL: "small"}}},
	}
	if got := tr.ArtistNames(); got != "A, B" {
		t.Errorf("ArtistNames() = %q, want %q", got, "A, B")
	}
	// A imagem do MEIO (a de ~300px): leve o bastante para um ícone.
	if got := tr.ImageURL(); got != "mid" {
		t.Errorf("ImageURL() = %q, want mid", got)
	}
	if got := (Track{}).ImageURL(); got != "" {
		t.Errorf("ImageURL() without images = %q, want empty", got)
	}
}
