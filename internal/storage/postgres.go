// ORDEM DE CRIAÇÃO: 4º, parte "a" (Etapa 4a — banco) · antes de tokenstore.go
//
// POR QUE EXISTE:
//
//	Token e histórico vivem no PostgreSQL. Este arquivo é o ÚNICO lugar que
//	sabe abrir a conexão e criar as tabelas; os outros arquivos do pacote
//	(tokenstore.go, playstore.go) só recebem um *sql.DB já pronto. Vem na
//	Etapa 4 (e não na 8) porque o /callback do login já precisa gravar o token.
//
// O QUE FAZ:
//   - Open:    abre o pool de conexões e confirma que o banco responde.
//   - Migrate: cria as tabelas (idempotente: pode rodar a cada inicialização).
//
// DRIVER: pgx, usado através de database/sql (a interface da biblioteca
// padrão). O import com "_" abaixo só REGISTRA o driver com o nome "pgx".
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// Importado só pelo efeito colateral: registra o driver "pgx" em
	// database/sql. É por isso que sql.Open("pgx", ...) funciona abaixo.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// schema são os comandos que criam as tabelas. Todos usam IF NOT EXISTS, então
// rodar de novo não quebra nada nem apaga dados (por isso não precisamos de uma
// ferramenta de migração enquanto o schema for este).
//
// Cada comando é um item separado (e não um texto único com vários ";") para
// que um erro aponte exatamente QUAL comando falhou.
var schema = []string{
	// Token do usuário. O CHECK (id = 1) garante UMA linha só: o sistema tem um
	// único usuário, então "salvar o token" é sempre um upsert nessa linha.
	// timestamptz guarda um INSTANTE (com fuso), sem ambiguidade.
	`CREATE TABLE IF NOT EXISTS spotify_tokens (
		id            integer     PRIMARY KEY DEFAULT 1 CHECK (id = 1),
		access_token  text        NOT NULL,
		refresh_token text        NOT NULL,
		expires_at    timestamptz NOT NULL,
		updated_at    timestamptz NOT NULL DEFAULT now()
	)`,

	// Histórico: uma linha por vez que ouvi uma música.
	// A chave primária (played_at, track_id) é a regra anti-duplicado: o
	// coletor sempre revê as últimas faixas, e o banco recusa as repetidas
	// (INSERT ... ON CONFLICT DO NOTHING). O índice dessa chave começa por
	// played_at, então as consultas por período usam esse índice: não
	// precisamos criar outro.
	`CREATE TABLE IF NOT EXISTS plays (
		played_at       timestamptz NOT NULL,
		track_id        text        NOT NULL,
		track_name      text        NOT NULL,
		artist          text        NOT NULL,
		album_image_url text        NOT NULL DEFAULT '',
		PRIMARY KEY (played_at, track_id)
	)`,
}

// Open abre o pool de conexões com o PostgreSQL e confirma que ele responde.
//
// Detalhe importante: sql.Open NÃO conecta, só prepara o pool. Um erro de
// senha, host ou porta só aparece no PingContext, e por isso ele é obrigatório
// aqui: queremos falhar na inicialização do programa, e não na primeira
// requisição.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		// Não incluímos o dsn na mensagem: ele contém a senha.
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Limites do pool. Uso pessoal: poucas conexões bastam. Reciclar conexões
	// velhas evita surpresas se o banco as derrubar por inatividade.
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	// Timeout próprio: sem ele, um host inalcançável deixaria o programa
	// esperando indefinidamente na inicialização.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close() // não deixa o pool aberto se a conexão falhou
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	return db, nil
}

// Migrate cria as tabelas que ainda não existem. É idempotente: rodar duas
// vezes é seguro, e é o que o main.go faz a cada inicialização.
func Migrate(ctx context.Context, db *sql.DB) error {
	for i, stmt := range schema {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("running schema statement %d: %w", i+1, err)
		}
	}
	return nil
}
