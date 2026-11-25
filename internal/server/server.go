// ORDEM DE CRIAÇÃO: 4º (Etapa 4 — rotas e login) · junto com middleware.go e handlers/auth.go
//
// POR QUE EXISTE:
//
//	Hoje o main.go cria o mux e registra /health direto. Na Etapa 4 passamos
//	a ter várias rotas (/login, /callback, /api/...). Em vez de o main.go
//	crescer sem controle, este pacote é o ÚNICO lugar que sabe "qual URL vai
//	para qual handler". Aparece agora (e não antes) porque só agora existe
//	mais de uma rota.
//
// O QUE VAI AQUI (a implementar):
//   - func New(deps) http.Handler: cria o http.ServeMux, registra as rotas
//     no formato do Go 1.22+ ("GET /login", "GET /api/now-playing"...),
//     envolve o mux com os middlewares (CORS) e devolve o resultado.
//   - "deps" é uma struct com o que os handlers precisam (config, services).
//     Isso se chama injeção de dependências: nada de variável global.
//   - Mover o healthHandler que hoje está no main.go (para handlers/ ou
//     aqui — você decide).
//   - Esta API NÃO serve front-end: não existe rota "GET /".
//
// Rotas previstas: ver tabela na seção 6 do README.
package server
