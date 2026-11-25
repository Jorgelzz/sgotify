package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jorgelzz/sgotify/internal/spotify"
	"github.com/jorgelzz/sgotify/internal/storage"
)

// -------------------------------------------------------------------- fakes
// Os services dependem de INTERFACES, então os testes usam versões falsas em
// memória: rápidos, sem rede e sem banco.

type fakePlayer struct {
	np  *spotify.NowPlaying
	err error
}

func (f fakePlayer) CurrentlyPlaying(context.Context) (*spotify.NowPlaying, error) {
	return f.np, f.err
}

// fakeStore guarda os plays em memória e registra os limites pedidos ao
// Between, para o teste conferir qual janela o service consultou.
type fakeStore struct {
	plays    []storage.Play
	err      error
	gotFrom  time.Time
	gotTo    time.Time
	between  int // quantas vezes Between foi chamado
	lastPlay time.Time
}

func (f *fakeStore) Add(_ context.Context, plays []storage.Play) (int, error) {
	f.plays = append(f.plays, plays...)
	return len(plays), nil
}

func (f *fakeStore) Between(_ context.Context, from, to time.Time) ([]storage.Play, error) {
	f.between++
	f.gotFrom, f.gotTo = from, to
	if f.err != nil {
		return nil, f.err
	}
	var out []storage.Play
	for _, p := range f.plays { // from inclusivo, to exclusivo: igual ao banco
		if !p.PlayedAt.Before(from) && p.PlayedAt.Before(to) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeStore) LastPlayedAt(context.Context) (time.Time, error) { return f.lastPlay, nil }

// play monta um Play de teste.
func play(at time.Time, id string) storage.Play {
	return storage.Play{PlayedAt: at, TrackID: id, TrackName: "name-" + id, Artist: "artist-" + id, AlbumImageURL: "img-" + id}
}

// ------------------------------------------------------------- NowPlaying

func TestNowPlayingNothing(t *testing.T) {
	svc := NewNowPlayingService(fakePlayer{}) // (nil, nil): nada tocando

	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != (NowPlaying{}) {
		t.Errorf("Get() = %+v, want the zero NowPlaying (Playing false)", got)
	}
}

func TestNowPlayingMapsTrack(t *testing.T) {
	svc := NewNowPlayingService(fakePlayer{np: &spotify.NowPlaying{
		IsPlaying:  true,
		ProgressMS: 1500,
		Track: spotify.Track{
			Name: "Song", DurationMS: 200000,
			Artists: []spotify.Artist{{Name: "A"}, {Name: "B"}},
			Album:   spotify.Album{Images: []spotify.Image{{URL: "big"}, {URL: "mid"}, {URL: "small"}}},
		},
	}})

	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	want := NowPlaying{Playing: true, Track: "Song", Artist: "A, B", Image: "mid", ProgressMS: 1500, DurationMS: 200000}
	if got != want {
		t.Errorf("Get() = %+v, want %+v", got, want)
	}
}

// Pausada: Playing false, mas a faixa continua informada.
func TestNowPlayingPaused(t *testing.T) {
	svc := NewNowPlayingService(fakePlayer{np: &spotify.NowPlaying{IsPlaying: false, Track: spotify.Track{Name: "Song"}}})

	got, _ := svc.Get(context.Background())
	if got.Playing || got.Track != "Song" {
		t.Errorf("Get() = %+v, want Playing=false and Track=Song", got)
	}
}

// O erro original é repassado SEM ser trocado: o handler precisa reconhecê-lo
// (errors.Is) para escolher o status HTTP.
func TestNowPlayingPropagatesError(t *testing.T) {
	svc := NewNowPlayingService(fakePlayer{err: spotify.ErrNoToken})

	_, err := svc.Get(context.Background())
	if !errors.Is(err, spotify.ErrNoToken) {
		t.Errorf("Get() error = %v, want spotify.ErrNoToken", err)
	}
}

// ------------------------------------------------------------------- Top

func TestTopTracksRanking(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	plays := []storage.Play{
		play(t0, "a"),
		play(t0.Add(1*time.Hour), "b"),
		play(t0.Add(2*time.Hour), "b"),
		play(t0.Add(3*time.Hour), "c"),
		play(t0.Add(4*time.Hour), "b"),
		play(t0.Add(5*time.Hour), "a"), // a: 2 plays, última às 17h; c: 1 play
	}

	got := topTracks(plays, 10)

	var ids []string
	for _, tc := range got {
		ids = append(ids, tc.TrackID)
	}
	want := []string{"b", "a", "c"} // b=3, a=2, c=1
	if len(ids) != 3 || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("order = %v, want %v", ids, want)
	}
	if got[0].Plays != 3 || got[1].Plays != 2 || got[2].Plays != 1 {
		t.Errorf("plays = %d,%d,%d, want 3,2,1", got[0].Plays, got[1].Plays, got[2].Plays)
	}
	if got[0].Name != "name-b" || got[0].Artist != "artist-b" || got[0].ImageURL != "img-b" {
		t.Errorf("display data = %+v", got[0])
	}
}

// Empate em plays: quem foi tocada por ÚLTIMO vem primeiro.
func TestTopTracksTieBreak(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	plays := []storage.Play{
		play(t0, "old"),
		play(t0.Add(time.Hour), "new"),
	}

	got := topTracks(plays, 10)
	if got[0].TrackID != "new" {
		t.Errorf("first = %q, want new (the tie goes to the most recent)", got[0].TrackID)
	}
}

func TestTopTracksLimit(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	plays := []storage.Play{play(t0, "a"), play(t0.Add(time.Minute), "b"), play(t0.Add(2*time.Minute), "c")}

	if got := topTracks(plays, 2); len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

// Sem plays: lista VAZIA (não nil), para o JSON sair [] e não null.
func TestTopTracksEmptyIsNotNil(t *testing.T) {
	if got := topTracks(nil, 10); got == nil || len(got) != 0 {
		t.Errorf("topTracks(nil) = %#v, want an empty non-nil slice", got)
	}
}

// A janela da semana: de (agora - 7 dias) até agora, com o relógio injetado.
func TestTopWeekWindow(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{plays: []storage.Play{
		play(now.AddDate(0, 0, -8), "too-old"),
		play(now.AddDate(0, 0, -6), "inside"),
	}}
	svc := &TopService{store: store, now: func() time.Time { return now }}

	got, err := svc.Week(context.Background(), 10)
	if err != nil {
		t.Fatalf("Week() error = %v", err)
	}
	if !store.gotFrom.Equal(now.AddDate(0, 0, -7)) || !store.gotTo.Equal(now) {
		t.Errorf("window = [%v, %v), want [%v, %v)", store.gotFrom, store.gotTo, now.AddDate(0, 0, -7), now)
	}
	if len(got) != 1 || got[0].TrackID != "inside" {
		t.Errorf("Week() = %+v, want only the track inside the window", got)
	}
}

func TestTopWeekPropagatesStoreError(t *testing.T) {
	boom := errors.New("db down")
	svc := NewTopService(&fakeStore{err: boom})

	if _, err := svc.Week(context.Background(), 10); !errors.Is(err, boom) {
		t.Errorf("Week() error = %v, want the store error", err)
	}
}

// -------------------------------------------------------------- Calendar

func saoPaulo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err) // em produção o config embute o tzdata
	}
	return loc
}

// O caso que justifica a função: uma música ouvida às 22h30 em São Paulo
// (UTC-3) é 01h30 do dia SEGUINTE em UTC. Tem que contar no dia 24, e não no 25.
func TestTopPerDayUsesLocalTimezone(t *testing.T) {
	loc := saoPaulo(t)
	at := time.Date(2026, 11, 25, 1, 30, 0, 0, time.UTC) // = 24/nov 22:30 em São Paulo

	got := topPerDay([]storage.Play{play(at, "a")}, loc)

	if len(got) != 1 || got[0].Date != "2026-11-24" {
		t.Fatalf("topPerDay() = %+v, want one entry on 2026-11-24 (local day)", got)
	}
}

func TestTopPerDayWinnerAndOrder(t *testing.T) {
	loc := saoPaulo(t)
	d1 := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)
	plays := []storage.Play{
		play(d1, "x"),
		play(d1.Add(time.Hour), "y"),
		play(d1.Add(2*time.Hour), "y"), // dia 10: y (2 plays) vence x (1)
		play(d2, "z"),                  // dia 2: só z
	}

	got := topPerDay(plays, loc)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 days", len(got))
	}
	// Cronológico: dia 2 antes do dia 10 (e "02" < "10" como texto).
	if got[0].Date != "2026-11-02" || got[0].TrackID != "z" {
		t.Errorf("first day = %+v, want 2026-11-02 / z", got[0])
	}
	if got[1].Date != "2026-11-10" || got[1].TrackID != "y" || got[1].Plays != 2 {
		t.Errorf("second day = %+v, want 2026-11-10 / y with 2 plays", got[1])
	}
}

// Empate no dia: vence a música tocada por último.
func TestTopPerDayTieGoesToMostRecent(t *testing.T) {
	loc := saoPaulo(t)
	t0 := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)

	got := topPerDay([]storage.Play{play(t0, "morning"), play(t0.Add(time.Hour), "evening")}, loc)

	if len(got) != 1 || got[0].TrackID != "evening" {
		t.Errorf("topPerDay() = %+v, want evening to win the tie", got)
	}
}

// Dia sem plays NÃO aparece; sem plays no mês, a lista é vazia (não nil).
func TestTopPerDayEmpty(t *testing.T) {
	got := topPerDay(nil, time.UTC)
	if got == nil || len(got) != 0 {
		t.Errorf("topPerDay(nil) = %#v, want an empty non-nil slice", got)
	}
}

// Month deve consultar o storage com os limites do mês NO FUSO LOCAL.
func TestCalendarMonthBoundsAreLocal(t *testing.T) {
	loc := saoPaulo(t)
	store := &fakeStore{}
	svc := NewCalendarService(store, loc)

	if _, err := svc.Month(context.Background(), 2026, time.November); err != nil {
		t.Fatalf("Month() error = %v", err)
	}

	wantFrom := time.Date(2026, 11, 1, 0, 0, 0, 0, loc) // 03:00 UTC
	wantTo := time.Date(2026, 12, 1, 0, 0, 0, 0, loc)
	if !store.gotFrom.Equal(wantFrom) || !store.gotTo.Equal(wantTo) {
		t.Errorf("Between(%v, %v), want (%v, %v)", store.gotFrom, store.gotTo, wantFrom, wantTo)
	}
}

// Um play das 22h do dia 30/nov (local) = 01h UTC de 1º/dez: pertence a
// NOVEMBRO, e não a dezembro.
func TestCalendarMonthKeepsLateNightInTheRightMonth(t *testing.T) {
	loc := saoPaulo(t)
	store := &fakeStore{plays: []storage.Play{
		play(time.Date(2026, 12, 1, 1, 0, 0, 0, time.UTC), "late"), // 30/nov 22:00 local
	}}
	svc := NewCalendarService(store, loc)

	nov, _ := svc.Month(context.Background(), 2026, time.November)
	dec, _ := svc.Month(context.Background(), 2026, time.December)

	if len(nov) != 1 || nov[0].Date != "2026-11-30" {
		t.Errorf("november = %+v, want the play on 2026-11-30", nov)
	}
	if len(dec) != 0 {
		t.Errorf("december = %+v, want empty", dec)
	}
}

func TestCalendarPropagatesStoreError(t *testing.T) {
	boom := errors.New("db down")
	svc := NewCalendarService(&fakeStore{err: boom}, time.UTC)

	if _, err := svc.Month(context.Background(), 2026, time.November); !errors.Is(err, boom) {
		t.Errorf("Month() error = %v, want the store error", err)
	}
}
