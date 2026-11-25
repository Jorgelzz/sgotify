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
// O QUE VAI AQUI (a implementar):
//   - struct Client { http *http.Client; tokenPath string; mu sync.Mutex; ... }
//   - NewClient(config) *Client
//   - método interno get(ctx, path, query, &out) error:
//     1. garante token válido (se Expired() => Refresh() e salva em disco);
//     2. monta o request com o header Bearer;
//     3. trata status: 204 (sem conteúdo, não é erro), 401 (token ruim),
//     429 (rate limit: ler o header "Retry-After"), outros => erro com o
//     corpo da resposta para facilitar debug;
//     4. decodifica o JSON em "out".
//   - Use context.Context e timeout no http.Client — nunca http.DefaultClient
//     sem timeout.
//
// Os arquivos player.go, top.go e history.go são métodos deste Client.
package spotify
