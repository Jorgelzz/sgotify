package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jorgelzz/sgotify/internal/spotify"
)

// testDB abre o banco de TESTE e o deixa limpo. Estes testes são de
// integração: precisam de um PostgreSQL de verdade e APAGAM as tabelas.
//
// Por isso:
//   - a URL vem de TEST_DB_URL (variável de ambiente, NÃO é lida do .env) e deve
//     apontar para um banco separado (ex.: sgotify_test), nunca o real;
//   - se a variável não existir, o teste é PULADO (t.Skip) e "go test ./..."
//     continua passando em qualquer máquina.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	url := os.Getenv("TEST_DB_URL")
	if url == "" {
		t.Skip("TEST_DB_URL not set: skipping integration test")
	}

	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	// Começa cada teste do zero: um teste não pode depender do anterior.
	if _, err := db.ExecContext(ctx, `TRUNCATE spotify_tokens, plays`); err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
	return db
}

// Não precisa de banco: uma porta onde nada escuta tem que dar ERRO, e não
// travar. Prova que o Ping do Open está funcionando (sql.Open sozinho não
// conecta, então sem o Ping este erro só apareceria na primeira consulta).
func TestOpenUnreachable(t *testing.T) {
	_, err := Open(context.Background(), "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("Open() error = nil, want a connection error")
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := testDB(t) // testDB já roda Migrate uma vez
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second Migrate() error = %v, want nil", err)
	}
}

// ---------------------------------------------------------------- TokenStore

func TestTokenStoreLoadEmpty(t *testing.T) {
	store := NewTokenStore(testDB(t))

	_, err := store.Load(context.Background())
	if !errors.Is(err, spotify.ErrNoToken) {
		t.Fatalf("Load() error = %v, want spotify.ErrNoToken", err)
	}
}

func TestTokenStoreRoundTrip(t *testing.T) {
	store := NewTokenStore(testDB(t))
	ctx := context.Background()

	want := spotify.Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC),
	}
	if err := store.Save(ctx, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
	// time.Time se compara com Equal, nunca com ==.
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
}

// Salvar de novo SUBSTITUI o token (é o que acontece a cada renovação) e nunca
// cria uma segunda linha.
func TestTokenStoreSaveReplaces(t *testing.T) {
	db := testDB(t)
	store := NewTokenStore(db)
	ctx := context.Background()
	exp := time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)

	for _, access := range []string{"first", "second"} {
		if err := store.Save(ctx, spotify.Token{AccessToken: access, RefreshToken: "r", ExpiresAt: exp}); err != nil {
			t.Fatalf("Save(%q) error = %v", access, err)
		}
	}

	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM spotify_tokens`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("rows = %d, want 1", rows)
	}
	got, _ := store.Load(ctx)
	if got.AccessToken != "second" {
		t.Errorf("AccessToken = %q, want second (the last one saved)", got.AccessToken)
	}
}

// ----------------------------------------------------------------- PlayStore

// play monta um Play de teste; só PlayedAt e TrackID importam para a chave.
func play(playedAt time.Time, trackID string) Play {
	return Play{PlayedAt: playedAt, TrackID: trackID, TrackName: "name-" + trackID, Artist: "artist", AlbumImageURL: "http://img/" + trackID}
}

func TestPlayStoreAddIgnoresDuplicates(t *testing.T) {
	store := NewPlayStore(testDB(t))
	ctx := context.Background()
	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	added, err := store.Add(ctx, []Play{play(t0, "a"), play(t0.Add(time.Minute), "b")})
	if err != nil || added != 2 {
		t.Fatalf("first Add() = %d, %v, want 2, nil", added, err)
	}

	// O coletor reenvia o que já viu: nada novo.
	added, err = store.Add(ctx, []Play{play(t0, "a"), play(t0.Add(time.Minute), "b")})
	if err != nil || added != 0 {
		t.Errorf("repeated Add() = %d, %v, want 0, nil", added, err)
	}

	// Lote misto: um repetido e um novo => só o novo conta.
	added, err = store.Add(ctx, []Play{play(t0, "a"), play(t0.Add(2*time.Minute), "c")})
	if err != nil || added != 1 {
		t.Errorf("mixed Add() = %d, %v, want 1, nil", added, err)
	}
}

// A MESMA música tocada em horários diferentes são 2 plays (não é duplicata).
func TestPlayStoreSameTrackTwice(t *testing.T) {
	store := NewPlayStore(testDB(t))
	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	added, err := store.Add(context.Background(), []Play{play(t0, "a"), play(t0.Add(5*time.Minute), "a")})
	if err != nil || added != 2 {
		t.Fatalf("Add() = %d, %v, want 2, nil", added, err)
	}
}

func TestPlayStoreAddEmpty(t *testing.T) {
	store := NewPlayStore(testDB(t))

	added, err := store.Add(context.Background(), nil)
	if err != nil || added != 0 {
		t.Errorf("Add(nil) = %d, %v, want 0, nil", added, err)
	}
}

// from é INCLUSIVO e to é EXCLUSIVO; o resultado vem em ordem cronológica.
func TestPlayStoreBetweenBounds(t *testing.T) {
	store := NewPlayStore(testDB(t))
	ctx := context.Background()
	from := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	_, err := store.Add(ctx, []Play{
		play(to, "at-end"),                          // == to: FORA
		play(from.Add(-time.Second), "before"),      // antes: FORA
		play(from.Add(10*time.Hour), "inside-late"), // dentro
		play(from, "at-start"),                      // == from: DENTRO
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Between(ctx, from, to)
	if err != nil {
		t.Fatalf("Between() error = %v", err)
	}
	var ids []string
	for _, p := range got {
		ids = append(ids, p.TrackID)
	}
	want := []string{"at-start", "inside-late"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Errorf("Between() track ids = %v, want %v", ids, want)
	}
}

func TestPlayStoreBetweenEmpty(t *testing.T) {
	store := NewPlayStore(testDB(t))

	got, err := store.Between(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err != nil || len(got) != 0 {
		t.Errorf("Between() = %v, %v, want empty, nil", got, err)
	}
}

func TestPlayStoreLastPlayedAt(t *testing.T) {
	store := NewPlayStore(testDB(t))
	ctx := context.Background()

	// Tabela vazia: valor zero e SEM erro (max() devolve NULL).
	last, err := store.LastPlayedAt(ctx)
	if err != nil || !last.IsZero() {
		t.Fatalf("empty LastPlayedAt() = %v, %v, want zero, nil", last, err)
	}

	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	if _, err := store.Add(ctx, []Play{play(t0, "a"), play(t0.Add(time.Hour), "b")}); err != nil {
		t.Fatal(err)
	}
	last, err = store.LastPlayedAt(ctx)
	if err != nil || !last.Equal(t0.Add(time.Hour)) {
		t.Errorf("LastPlayedAt() = %v, %v, want %v", last, err, t0.Add(time.Hour))
	}
}
