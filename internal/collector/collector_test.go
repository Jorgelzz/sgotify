package collector

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jorgelzz/sgotify/internal/spotify"
	"github.com/jorgelzz/sgotify/internal/storage"
)

// ----------------------------------------------------------------- fakes

type fakeAPI struct {
	items    []spotify.PlayedItem
	err      error
	calls    atomic.Int32
	mu       sync.Mutex
	gotAfter time.Time
}

func (f *fakeAPI) RecentlyPlayed(_ context.Context, after time.Time) ([]spotify.PlayedItem, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.gotAfter = after
	f.mu.Unlock()
	return f.items, f.err
}

// fakeStore se comporta como o banco real: ignora duplicados (mesma chave
// played_at + track_id) e conta só os novos.
type fakeStore struct {
	mu   sync.Mutex
	seen map[string]storage.Play
	last time.Time
	err  error
}

func newFakeStore() *fakeStore { return &fakeStore{seen: map[string]storage.Play{}} }

func (f *fakeStore) Add(_ context.Context, plays []storage.Play) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	added := 0
	for _, p := range plays {
		key := p.PlayedAt.String() + "|" + p.TrackID
		if _, dup := f.seen[key]; !dup {
			f.seen[key] = p
			added++
		}
	}
	return added, nil
}

func (f *fakeStore) Between(context.Context, time.Time, time.Time) ([]storage.Play, error) {
	return nil, nil
}

func (f *fakeStore) LastPlayedAt(context.Context) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last, f.err
}

func (f *fakeStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func item(at time.Time, id string) spotify.PlayedItem {
	return spotify.PlayedItem{PlayedAt: at, Track: spotify.Track{
		ID: id, Name: "name-" + id,
		Artists: []spotify.Artist{{Name: "A"}, {Name: "B"}},
		Album:   spotify.Album{Images: []spotify.Image{{URL: "big"}, {URL: "mid"}, {URL: "small"}}},
	}}
}

// ----------------------------------------------------------------- Collect

func TestCollectConvertsAndStores(t *testing.T) {
	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	api := &fakeAPI{items: []spotify.PlayedItem{item(t0, "a"), item(t0.Add(time.Minute), "b")}}
	store := newFakeStore()

	added, err := New(api, store, time.Minute).Collect(context.Background())
	if err != nil || added != 2 {
		t.Fatalf("Collect() = %d, %v, want 2, nil", added, err)
	}

	got := store.seen[t0.String()+"|a"]
	want := storage.Play{PlayedAt: t0, TrackID: "a", TrackName: "name-a", Artist: "A, B", AlbumImageURL: "mid"}
	if got != want {
		t.Errorf("stored play = %+v, want %+v", got, want)
	}
}

// A coleta usa o último play guardado como "after": pede só o que é novo.
func TestCollectPassesLastPlayedAtAsAfter(t *testing.T) {
	last := time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)
	api := &fakeAPI{}
	store := newFakeStore()
	store.last = last

	if _, err := New(api, store, time.Minute).Collect(context.Background()); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !api.gotAfter.Equal(last) {
		t.Errorf("after = %v, want %v", api.gotAfter, last)
	}
}

// Idempotente: coletar de novo o MESMO lote não duplica nada.
func TestCollectIsIdempotent(t *testing.T) {
	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	api := &fakeAPI{items: []spotify.PlayedItem{item(t0, "a")}}
	store := newFakeStore()
	c := New(api, store, time.Minute)

	first, _ := c.Collect(context.Background())
	second, err := c.Collect(context.Background())

	if first != 1 || second != 0 || err != nil {
		t.Errorf("Collect() twice = %d then %d (err %v), want 1 then 0", first, second, err)
	}
	if store.count() != 1 {
		t.Errorf("stored = %d, want 1", store.count())
	}
}

// Faixa sem ID (arquivo local) é ignorada.
func TestCollectSkipsTracksWithoutID(t *testing.T) {
	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	api := &fakeAPI{items: []spotify.PlayedItem{item(t0, ""), item(t0.Add(time.Minute), "ok")}}
	store := newFakeStore()

	added, _ := New(api, store, time.Minute).Collect(context.Background())
	if added != 1 || store.count() != 1 {
		t.Errorf("added = %d, stored = %d, want 1 and 1 (local file skipped)", added, store.count())
	}
}

func TestCollectPropagatesErrors(t *testing.T) {
	t.Run("no token", func(t *testing.T) {
		_, err := New(&fakeAPI{err: spotify.ErrNoToken}, newFakeStore(), time.Minute).Collect(context.Background())
		if !errors.Is(err, spotify.ErrNoToken) {
			t.Errorf("error = %v, want ErrNoToken", err)
		}
	})
	t.Run("store failure", func(t *testing.T) {
		boom := errors.New("db down")
		store := newFakeStore()
		store.err = boom
		_, err := New(&fakeAPI{}, store, time.Minute).Collect(context.Background())
		if !errors.Is(err, boom) {
			t.Errorf("error = %v, want the store error", err)
		}
	})
}

// ---------------------------------------------------------------------- Run

// Run coleta IMEDIATAMENTE (sem esperar o primeiro intervalo) e para quando o
// contexto é cancelado.
func TestRunCollectsImmediatelyAndStopsOnCancel(t *testing.T) {
	api := &fakeAPI{}
	c := New(api, newFakeStore(), time.Hour) // intervalo enorme: só a coleta inicial acontece

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()

	waitFor(t, func() bool { return api.calls.Load() >= 1 }, "the initial collection")
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after the context was cancelled")
	}
	if api.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (the interval is 1h)", api.calls.Load())
	}
}

// Com intervalo curto, Run continua coletando; e um erro NÃO o derruba.
func TestRunKeepsGoingAfterErrors(t *testing.T) {
	api := &fakeAPI{err: errors.New("spotify is down")}
	c := New(api, newFakeStore(), 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()

	waitFor(t, func() bool { return api.calls.Load() >= 3 }, "at least 3 collections despite errors")
	cancel()
	<-done
}

// waitFor espera uma condição por até 2s, sem dormir um tempo fixo: o teste só
// demora o necessário e não fica instável.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
