// ORDEM DE CRIAÇÃO: 10º (Etapa 10 — calendário) · junto com service/calendar.go
//
// POR QUE EXISTE:
//
//	Expõe service.CalendarService.Month como JSON em
//	GET /api/calendar?month=2026-11.
//
// O QUE VAI AQUI (a implementar):
//   - Ler e validar ?month=YYYY-MM (padrão: mês atual no fuso configurado).
//     Mês inválido => 400 com mensagem clara. (time.Parse("2006-01", s))
//   - Chamar service.Month(ctx, ano, mês).
//   - Responder: {"month":"2026-11","days":[{"date":"2026-11-24","track":"...",
//     "artist":"...","image":"https://...","plays":7}, ...]}
//   - Dias sem nenhum play NÃO aparecem na lista; quem desenha o calendário
//     (o front) preenche os dias vazios.
package handlers
