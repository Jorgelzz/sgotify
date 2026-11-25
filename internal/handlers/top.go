// ORDEM DE CRIAÇÃO: 7º (Etapa 7 — mais ouvidos) · junto com spotify/top.go e service/top.go
//
// POR QUE EXISTE:
//
//	Expõe service.TopService.Week como JSON em GET /api/top-week.
//
// O QUE VAI AQUI (a implementar):
//   - Ler ?limit= : valor padrão 10, máximo 50. Valor inválido => 400.
//   - Chamar service.Week(ctx, limit).
//   - Responder {"tracks":[{"track":"...","artist":"...","image":"...","plays":12}, ...]}.
//   - Erros: mesma política do now-playing (401 sem token, 502 falha externa).
//
// Validação de entrada (limit) é papel do handler; a regra "o que é a
// semana" é do service.
package handlers
