// ORDEM DE CRIAÇÃO: 3º (Etapa 3 — autenticação) · junto com auth.go
//
// POR QUE EXISTE:
//
//	O access token dura ~1h, mas o coletor precisa funcionar por dias sem eu
//	fazer login de novo. Então o token (principalmente o refresh_token)
//	precisa SOBREVIVER a reinícios do servidor => persistir no PostgreSQL.
//	Separado do auth.go porque "como obter token" (rede) e "onde guardar
//	token" (banco) são responsabilidades diferentes.
//
// O QUE VAI AQUI:
//   - Token, newToken e Expired      -> Etapa 3 (pronto)
//   - ErrNoToken e interface TokenStore -> Etapa 4a (só a DECLARAÇÃO; a
//     implementação com SQL fica em storage/tokenstore.go)
//
// POR QUE A INTERFACE FICA AQUI e não no pacote storage:
//
//	storage/tokenstore.go precisa do tipo spotify.Token, então storage IMPORTA
//	spotify. Se spotify importasse storage de volta, o Go recusaria (import
//	cycle). Declarando a interface do lado de quem a CONSOME (spotify), o
//	spotify não conhece o banco: quem liga os dois é o main.go.
//
// CUIDADO: acesso concorrente (handler + coletor) => o client.go (Etapa 5)
// precisa de um sync.Mutex para não renovar o token duas vezes ao mesmo
// tempo. O banco resolve a gravação concorrente, mas não a renovação dupla.
package spotify

import (
	"context"
	"errors"
	"time"
)

// Token é o par de credenciais do usuário.
//
// As tags `json:"..."` ficam por conveniência (logs, testes). No Postgres cada
// campo vira uma COLUNA da tabela spotify_tokens, e não um JSON.
//
// ExpiresAt é um HORÁRIO ABSOLUTO, não uma duração. O Spotify manda
// "expires_in: 3600" (segundos a partir de agora); se guardássemos só isso,
// depois não saberíamos mais quando o token expira.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// newToken monta um Token a partir do que o Spotify devolve.
//
// O que tratar:
//   - converter expiresIn (segundos) em ExpiresAt = agora + expiresIn segundos.
//     Dica: time.Now().Add(time.Duration(expiresIn) * time.Second).
//
// Quem usa: auth.go (requestToken). É uma função separada para ser testável
// sem rede.
//
// Teste: newToken("a", "r", 3600) deve ter ExpiresAt ~1h à frente de agora.
func newToken(accessToken, refreshToken string, expiresIn int) Token {
	return Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
	}
}

// Expired diz se o token já não deve ser usado.
//
// O que tratar:
//   - devolver true se faltam MENOS DE 30 SEGUNDOS para ExpiresAt (ou se já
//     passou). A folga evita usar um token que expira no meio da requisição.
//   - Dica: time.Now().Add(30 * time.Second).After(t.ExpiresAt)
//
// Teste (3 casos): expira em 1h -> false; expirou há 1 min -> true;
// expira em 10s -> true (dentro da folga).
func (t Token) Expired() bool {
	return time.Now().Add(30 * time.Second).After(t.ExpiresAt)
}

// ErrNoToken diz que ainda não há token guardado (o usuário nunca fez login).
//
// Quem implementa TokenStore devolve ESTE erro quando a tabela está vazia, e o
// chamador o reconhece com errors.Is(err, ErrNoToken): o handler responde 401
// apontando para /login. É o substituto do antigo "arquivo não existe".
// Distingue "nunca logou" (esperado) de "o banco caiu" (erro de verdade).
var ErrNoToken = errors.New("no spotify token stored")

// TokenStore é o que o pacote spotify precisa de um lugar para guardar o token.
// Só o contrato; a implementação com SQL está em storage/tokenstore.go.
//
//   - Save: grava o token, SUBSTITUINDO o anterior (só existe um usuário, logo
//     só existe uma linha).
//   - Load: devolve o token guardado, ou ErrNoToken se não houver nenhum.
//     Em qualquer outro erro, devolva Token{} e o erro embrulhado com %w.
//
// Usada por: handlers/auth.go (Save no /callback) e spotify/client.go (Load
// antes de cada chamada; Save depois de renovar com Auth.Refresh).
//
// Os testes do client.go (Etapa 5) usam um TokenStore FALSO em memória: não
// precisam de banco.
type TokenStore interface {
	Save(ctx context.Context, t Token) error
	Load(ctx context.Context) (Token, error)
}
