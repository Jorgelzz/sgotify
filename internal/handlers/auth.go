// ORDEM DE CRIAÇÃO: 4º (Etapa 4 — rotas e login) · junto com server/server.go
//
// POR QUE EXISTE:
//
//	É a ponta HTTP do login: o browser fala com ESTES handlers, e eles usam
//	spotify/auth.go para o trabalho de verdade. Handlers são "tradutores":
//	leem a requisição, chamam a lógica, escrevem a resposta — sem lógica de
//	negócio própria. Criado logo após auth.go para podermos testar o login
//	de ponta a ponta antes de construir qualquer outra coisa.
//	(Exceção consciente: o login chama spotify/auth direto, sem service,
//	porque não há regra de negócio — é só protocolo OAuth.)
//
// O QUE VAI AQUI (a implementar):
//   - Login (GET /login):
//     1. gera verifier + state aleatórios;
//     2. guarda os dois (cookie de curta duração ou map em memória);
//     3. http.Redirect para spotify.AuthURL(...).
//   - Callback (GET /callback):
//     1. confere se o "state" da query é o mesmo que guardamos (anti-CSRF);
//     2. se vier "error" na query (usuário negou), responder com mensagem;
//     3. spotify.ExchangeCode(code, verifier);
//     4. salvar o Token em disco;
//     5. redirecionar para FrontendURL (o front é outro projeto).
//   - Também escreva um helper writeJSON(w, status, v) aqui ou num arquivo
//     json.go — todos os handlers seguintes vão usar.
//
// TESTE DE FIM DA ETAPA: abrir /login, aprovar, e ver data/token.json criado.
package handlers
