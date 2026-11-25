// ORDEM DE CRIAÇÃO: 8º (Etapa 8 — nosso histórico) · junto com storage.go
//
// POR QUE EXISTE:
//
//	Implementação de Store com PostgreSQL. O banco resolve de graça o que
//	teríamos de escrever à mão: duplicados (chave primária), concorrência entre
//	o coletor e os handlers, e filtro por período. A tabela plays é criada em
//	postgres.go (Migrate).
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Verificação em tempo de compilação (veja tokenstore.go).
var _ Store = (*PlayStore)(nil)

// PlayStore guarda o histórico de plays no PostgreSQL.
type PlayStore struct {
	db *sql.DB
}

// NewPlayStore cria o PlayStore sobre um pool já aberto (storage.Open).
func NewPlayStore(db *sql.DB) *PlayStore {
	return &PlayStore{db: db}
}

// insertPlay grava um play. ON CONFLICT DO NOTHING faz o banco IGNORAR (sem
// erro) uma linha cuja chave primária (played_at, track_id) já existe. Essa
// cláusula é o que torna a coleta idempotente.
const insertPlay = `
	INSERT INTO plays (played_at, track_id, track_name, artist, album_image_url)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT DO NOTHING`

// Add grava os plays dentro de UMA transação: ou entram todos, ou nenhum. Se o
// meio do lote falhar, não ficamos com metade gravada.
func (s *PlayStore) Add(ctx context.Context, plays []Play) (int, error) {
	if len(plays) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("starting transaction: %w", err)
	}
	// Seguro chamar sempre: depois de um Commit bem-sucedido o Rollback não faz
	// nada; se saímos por um erro antes do Commit, ele desfaz tudo.
	defer tx.Rollback()

	// Prepara o comando UMA vez e o reaproveita para cada play do lote.
	stmt, err := tx.PrepareContext(ctx, insertPlay)
	if err != nil {
		return 0, fmt.Errorf("preparing insert: %w", err)
	}
	defer stmt.Close()

	added := 0
	for _, p := range plays {
		res, err := stmt.ExecContext(ctx, p.PlayedAt, p.TrackID, p.TrackName, p.Artist, p.AlbumImageURL)
		if err != nil {
			return 0, fmt.Errorf("inserting play %s at %s: %w", p.TrackID, p.PlayedAt, err)
		}
		// RowsAffected é 1 quando a linha entrou e 0 quando já existia: somar
		// dá exatamente "quantos eram novos".
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("reading affected rows: %w", err)
		}
		added += int(n)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing transaction: %w", err)
	}
	return added, nil
}

// Between devolve os plays com from <= played_at < to, em ordem cronológica.
// O fim é exclusivo (<) para um play exatamente à meia-noite não aparecer em
// dois dias. Usa o índice da chave primária, que começa por played_at.
func (s *PlayStore) Between(ctx context.Context, from, to time.Time) ([]Play, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT played_at, track_id, track_name, artist, album_image_url
		FROM plays
		WHERE played_at >= $1 AND played_at < $2
		ORDER BY played_at`, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying plays: %w", err)
	}
	defer rows.Close() // devolve a conexão ao pool

	var plays []Play
	for rows.Next() {
		var p Play
		if err := rows.Scan(&p.PlayedAt, &p.TrackID, &p.TrackName, &p.Artist, &p.AlbumImageURL); err != nil {
			return nil, fmt.Errorf("scanning play: %w", err)
		}
		// O banco devolve no fuso da sessão; padronizamos em UTC. Decidir o
		// "dia" no fuso local é papel do service/calendar.
		p.PlayedAt = p.PlayedAt.UTC()
		plays = append(plays, p)
	}
	// rows.Next() também devolve false quando a leitura FALHA no meio; só
	// rows.Err() distingue "acabaram as linhas" de "deu erro".
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading plays: %w", err)
	}
	return plays, nil
}

// LastPlayedAt devolve o played_at mais recente, ou o valor zero se a tabela
// estiver vazia. max() sobre uma tabela vazia devolve NULL, e por isso lemos
// em sql.NullTime, que sabe representar "sem valor".
func (s *PlayStore) LastPlayedAt(ctx context.Context) (time.Time, error) {
	var last sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT max(played_at) FROM plays`).Scan(&last); err != nil {
		return time.Time{}, fmt.Errorf("querying last play: %w", err)
	}
	if !last.Valid {
		return time.Time{}, nil
	}
	return last.Time.UTC(), nil
}
