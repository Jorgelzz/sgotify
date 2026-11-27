// ORDEM DE CRIAÇÃO: passo 1 da autenticação das rotas (identidade via Spotify)
//
// POR QUE EXISTE:
//
//	O login do Spotify já nos dá um token, mas não diz QUEM logou. Para só
//	deixar a SUA conta entrar, o /callback precisa perguntar ao Spotify "de
//	quem é este token?" e comparar a resposta com ALLOWED_SPOTIFY_USER_ID.
//	Esta função faz essa pergunta.
//
// Rota do Spotify: GET /me   (https://api.spotify.com/v1/me)
// Resposta: {"id": "seu_id", "display_name": "...", ...}
// Scope: o "id" parece vir sem scope nenhum. Se o Spotify responder 403 no seu
// primeiro login real, acrescente "user-read-private" aos scopes em auth.go.
package spotify

import (
	"context"
	"errors"
)

// PASSO 1A (antes de escrever esta função) — em client.go:
//
//	O c.get chama c.accessToken(ctx), que LÊ O TOKEN DO BANCO. No /callback o
//	token acabou de chegar e ainda NÃO foi gravado (só gravamos depois de
//	confirmar a conta). Então divida o get em dois:
//
//	  getWithToken(ctx, token, path, query, out) (bool, error)
//	      recebe o token PRONTO e faz o resto: monta a requisição, header
//	      Authorization, trata 204/erros (*APIError) e decodifica o JSON.
//
//	  get(ctx, path, query, out) (bool, error)
//	      só faz: token, err := c.accessToken(ctx) -> chama getWithToken.
//
//	O comportamento do get NÃO muda: rode `go test ./internal/spotify/` e os 28
//	testes que já existem têm que continuar passando.
//
// PASSO 1B — esta função:
//
//	1. Declare uma struct pequena só com o campo que interessa, ex.:
//	   type meResponse struct { ID string `json:"id"` }
//	   (o player.go faz o mesmo com currentlyPlayingResponse).
//
//	2. Chame c.getWithToken(ctx, accessToken, "/me", nil, &resp).
//	   Note: o token vem do PARÂMETRO, não do banco.
//
//	3. Trate os resultados:
//	   - err != nil: devolva "", err. Não embrulhe com texto novo por cima do
//	     *APIError, para quem chama poder usar errors.As (ex.: ver o Status 403).
//	   - found == false (204): o /me nunca deveria responder isso -> devolva
//	     um erro (ex.: errors.New("spotify returned no profile")).
//	   - resp.ID == "": devolva ERRO. Importante para a segurança: se devolvesse
//	     "" sem erro e o ALLOWED_SPOTIFY_USER_ID também estivesse vazio, a
//	     comparação passaria e qualquer pessoa entraria.
//
//	4. Tudo certo: devolva resp.ID, nil.
//
// TESTES (em client_test.go, usando newTestClient e fakeTokenStore):
//
//	a. Sucesso: o servidor falso confere o caminho "/me" e o header
//	   "Authorization: Bearer abc" e responde {"id":"user123"}. Esperado: "user123".
//	b. Token vem do PARÂMETRO: use um fakeTokenStore VAZIO. Se passar, a função
//	   não consultou o banco.
//	c. Status 403: o erro é *APIError com Status == 403 (errors.As).
//	d. Corpo {}: erro por id vazio.
//	e. JSON inválido: erro.

// UserID devolve o id da conta Spotify dona do accessToken.
//
// Recebe o token por parâmetro (e não lê do banco) porque é chamada ANTES de
// o token ser gravado: só gravamos se o id for o permitido.

func (c *Client) UserID(ctx context.Context, accessToken string) (string, error) {
	type meResponse struct {
		ID string `json:"id"`
	}

	var resp meResponse

	found, err := c.getWithToken(ctx, accessToken, "/me", nil, &resp)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("spotify returned no profile")
	}
	if resp.ID == "" {
		return "", errors.New("spotify returned empty user id")
	}
	return resp.ID, nil
}
