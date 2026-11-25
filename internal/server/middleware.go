// ORDEM DE CRIAÇÃO: 4º (Etapa 4 — rotas e login) · junto com server.go
//
// POR QUE EXISTE:
//
//	O front-end roda em OUTRA origem (outro projeto/porta). O browser só deixa
//	o JS dele ler nossas respostas se enviarmos headers de CORS. Isso vale para
//	TODAS as rotas, então não se faz dentro de cada handler: faz-se UMA vez,
//	num middleware — uma função que recebe um http.Handler e devolve outro
//	http.Handler "envolvendo" o original.
//
// O QUE VAI AQUI (a implementar):
//   - func CORS(allowedOrigin string) func(http.Handler) http.Handler
//     1. define Access-Control-Allow-Origin = allowedOrigin (nunca "*" por padrão);
//     2. define Allow-Methods (GET, OPTIONS) e Allow-Headers (Content-Type);
//     3. se o método for OPTIONS (preflight), responde 204 e NÃO chama o
//     handler seguinte.
//   - (opcional) func Logger(next http.Handler) http.Handler: loga método,
//     caminho, status e duração. Ótimo para enxergar o que o front está chamando.
//
// CONCEITO PARA ENTENDER: o padrão func(http.Handler) http.Handler é a base
// de todos os middlewares em Go.
package server
