// ORDEM DE CRIAÇÃO: 4º, parte "a" (Etapa 4a — banco) · depois de postgres.go
//
// POR QUE EXISTE:
//
//	Implementa spotify.TokenStore com SQL: guarda o token do usuário na tabela
//	spotify_tokens para sobreviver a reinícios. É o que permite o coletor
//	rodar por dias sem novo login. Vem antes dos handlers porque o /callback
//	precisa dele para salvar o token.
//
// OBSERVAÇÃO SOBRE AS CAMADAS: este é o único arquivo de storage que importa o
// pacote spotify (precisa do tipo spotify.Token). A direção é storage ->
// spotify, nunca o contrário; por isso a interface TokenStore fica declarada
// em spotify/token.go (veja o cabeçalho de lá).
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jorgelzz/sgotify/internal/spotify"
)

// Verificação em tempo de COMPILAÇÃO: se alguém mudar a assinatura de um
// método e este tipo deixar de satisfazer a interface, o erro aparece aqui, e
// não só quando o main.go tentar ligar as peças.
var _ spotify.TokenStore = (*TokenStore)(nil)

// TokenStore guarda o token do Spotify no PostgreSQL.
type TokenStore struct {
	db *sql.DB
}

// NewTokenStore cria o TokenStore sobre um pool já aberto (storage.Open).
func NewTokenStore(db *sql.DB) *TokenStore {
	return &TokenStore{db: db}
}

// upsertToken insere o token ou, se já existe a linha única (id = 1), a
// atualiza. Isso se chama UPSERT. EXCLUDED é o valor que tentamos inserir.
const upsertToken = `
	INSERT INTO spotify_tokens (id, access_token, refresh_token, expires_at, updated_at)
	VALUES (1, $1, $2, $3, now())
	ON CONFLICT (id) DO UPDATE SET
		access_token  = EXCLUDED.access_token,
		refresh_token = EXCLUDED.refresh_token,
		expires_at    = EXCLUDED.expires_at,
		updated_at    = now()`

// Save grava o token, substituindo o anterior.
//
// Os valores vão como PARÂMETROS ($1, $2, $3), nunca concatenados no texto do
// SQL: assim o banco os trata como dados e não como código (proteção contra
// SQL injection).
func (s *TokenStore) Save(ctx context.Context, t spotify.Token) error {
	_, err := s.db.ExecContext(ctx, upsertToken, t.AccessToken, t.RefreshToken, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("saving spotify token: %w", err)
	}
	return nil
}

// Load devolve o token guardado. Se a tabela estiver vazia (o usuário nunca fez
// login), devolve spotify.ErrNoToken, que o chamador reconhece com errors.Is.
func (s *TokenStore) Load(ctx context.Context) (spotify.Token, error) {
	var t spotify.Token
	err := s.db.QueryRowContext(ctx,
		`SELECT access_token, refresh_token, expires_at FROM spotify_tokens WHERE id = 1`,
	).Scan(&t.AccessToken, &t.RefreshToken, &t.ExpiresAt)

	// sql.ErrNoRows = "a consulta não achou linha nenhuma". Traduzimos para o
	// erro do domínio: quem chama (client, handler) não precisa saber de SQL.
	if errors.Is(err, sql.ErrNoRows) {
		return spotify.Token{}, spotify.ErrNoToken
	}
	if err != nil {
		return spotify.Token{}, fmt.Errorf("loading spotify token: %w", err)
	}

	// Normaliza para UTC: o fuso de exibição não importa para o token.
	t.ExpiresAt = t.ExpiresAt.UTC()
	return t, nil
}
