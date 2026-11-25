// ORDEM DE CRIAÇÃO: 10º (Etapa 10 — calendário) · junto com handlers/calendar.go
//
// POR QUE EXISTE:
//
//	É o "cérebro" do produto: transforma uma lista de plays em "qual foi a
//	música do dia, para cada dia do mês". Fica por último entre as peças de
//	backend porque precisa do storage (Etapa 8) já alimentado pelo coletor
//	(Etapa 9).
//
// O QUE VAI AQUI (a implementar):
//   - struct DayTop { Date string "2026-11-24"; TrackID, TrackName, Artist,
//     ImageURL string; Plays int }
//   - struct CalendarService { store storage.Store; loc *time.Location }
//   - func (s *CalendarService) Month(ctx, year int, month time.Month) ([]DayTop, error):
//     1. início e fim do mês NO FUSO LOCAL (s.loc);
//     2. store.Between(inicio, fim);
//     3. chamar a função PURA abaixo.
//   - func topPerDay(plays []storage.Play, loc *time.Location) []DayTop  <- função
//     pura, sem I/O (a parte que você mais vai testar):
//     a. converte PlayedAt (UTC) para o fuso local ANTES de pegar a data;
//     b. agrupa por dia -> por track_id -> conta;
//     c. vencedor do dia: mais plays; empate => tocada mais recentemente;
//     d. devolve ordenado por data.
//
// ARMADILHA PRINCIPAL: fuso horário. Uma música ouvida às 22h em São Paulo é
// 01h do dia SEGUINTE em UTC. Se agrupar em UTC, o calendário erra. Escreva
// um teste (calendar_test.go) exatamente para esse caso.
package service
