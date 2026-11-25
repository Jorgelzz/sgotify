// ORDEM DE CRIAÇÃO: 5º (Etapa 5 — cliente HTTP do Spotify)
//
// POR QUE EXISTE:
//
//	Toda chamada à API (player, top, history) precisa de: URL base
//	https://api.spotify.com/v1, header "Authorization: Bearer <token>",
//	token ainda válido, e tratamento de erros (401, 429...). Em vez de
//	repetir isso em 3 arquivos, existe UM cliente. Vem depois do login
//	(Etapa 4) porque precisa de um token real para ser testado.
//
// COMO FUNCIONA:
//   - accessToken: lê o token do TokenStore; se expirou, renova com
//     Auth.Refresh e grava o novo, tudo sob um mutex.
//   - get: faz o GET autenticado e decodifica o JSON.
//
// Os arquivos player.go, top.go e history.go são métodos deste Client.
package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// apiBaseURL é o endereço base da Web API do Spotify.
const apiBaseURL = "https://api.spotify.com/v1"

// APIError é um erro devolvido PELO Spotify (status diferente de 200/204).
//
// É um tipo próprio (e não só um texto) para quem chama poder inspecioná-lo
// com errors.As, por exemplo para ler o RetryAfter de um 429.
type APIError struct {
	Status     int           // status HTTP (401, 429, 500...)
	Body       string        // corpo da resposta: o Spotify explica o motivo ali
	RetryAfter time.Duration // só no 429: quanto esperar (header Retry-After)
}

func (e *APIError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("spotify api: status %d (retry after %s): %s", e.Status, e.RetryAfter, e.Body)
	}
	return fmt.Sprintf("spotify api: status %d: %s", e.Status, e.Body)
}

// Client faz chamadas autenticadas à API do Spotify.
type Client struct {
	http    *http.Client
	auth    *Auth      // usado só para renovar o token
	tokens  TokenStore // de onde lemos e onde gravamos o token
	baseURL string     // campo (e não só a constante) para os testes apontarem a um servidor falso

	// mu serializa a leitura/renovação do token. Sem ele, o handler e o coletor
	// poderiam ver o token expirado ao mesmo tempo e renová-lo DUAS vezes (e
	// como o Spotify pode girar o refresh token, a segunda renovação poderia
	// usar um refresh token já invalidado).
	mu sync.Mutex
}

// NewClient cria um Client. Recebe as dependências por parâmetro (nada de
// variável global), então nos testes passamos um TokenStore falso.
func NewClient(auth *Auth, tokens TokenStore) *Client {
	return &Client{
		// Timeout obrigatório: sem ele, um Spotify travado trava o servidor.
		http:    &http.Client{Timeout: 10 * time.Second},
		auth:    auth,
		tokens:  tokens,
		baseURL: apiBaseURL,
	}
}

// accessToken devolve um access token VÁLIDO, renovando se preciso.
//
// Erros: devolve ErrNoToken (sem embrulhar texto novo por cima, o chamador usa
// errors.Is) se o usuário nunca fez login.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tok, err := c.tokens.Load(ctx)
	if err != nil {
		return "", err
	}
	if !tok.Expired() {
		return tok.AccessToken, nil
	}

	// Expirado (ou a menos de 30s de expirar): renova sem pedir novo login.
	fresh, err := c.auth.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refreshing spotify token: %w", err)
	}
	if err := c.tokens.Save(ctx, fresh); err != nil {
		return "", fmt.Errorf("saving refreshed spotify token: %w", err)
	}
	return fresh.AccessToken, nil
}

// get faz um GET autenticado em baseURL+path e decodifica o JSON em out.
//
// O primeiro resultado diz se HOUVE conteúdo:
//   - true:  200 OK, e "out" foi preenchido;
//   - false: 204 No Content (não é erro: ex.: "nada tocando agora"); "out"
//     fica intacto.
//
// Qualquer outro status vira *APIError.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (bool, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return false, err
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("calling spotify: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return false, nil // sem corpo: não tente decodificar JSON
	case http.StatusOK:
		// segue para decodificar
	default:
		// Limita a leitura: não queremos carregar uma resposta gigante num erro.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		apiErr := &APIError{Status: resp.StatusCode, Body: string(body)}
		// 429 = rate limit. O header Retry-After diz quantos SEGUNDOS esperar.
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			apiErr.RetryAfter = time.Duration(secs) * time.Second
		}
		return false, apiErr
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return false, fmt.Errorf("decoding spotify response: %w", err)
	}
	return true, nil
}
