// ORDEM DE CRIAÇÃO: 9º (Etapa 9 — coleta do histórico) · junto com spotify/history.go
//
// POR QUE EXISTE:
//
//	É a peça que resolve a limitação central do projeto: roda em segundo
//	plano, de tempos em tempos, e copia o histórico do Spotify para o nosso
//	storage ANTES de ele ser esquecido (só 50 faixas ficam disponíveis).
//	Sem ele, o calendário seria impossível. É o primeiro código do projeto
//	que NÃO é disparado por uma requisição HTTP.
//
// O QUE VAI AQUI (a implementar):
//   - struct Collector { client; store; interval time.Duration }
//   - func (c *Collector) Run(ctx context.Context): loop com time.Ticker.
//     A cada tick: Collect(). Para quando ctx for cancelado (shutdown limpo).
//     Rode Collect() uma vez logo na partida (não espere o 1º intervalo).
//   - func (c *Collector) Collect(ctx) error:
//     1. after := store.LastPlayedAt();
//     2. items := client.RecentlyPlayed(ctx, after);
//     3. converter cada item em storage.Play;
//     4. store.Add(plays) e logar quantos eram novos.
//   - Se der erro (rede, 429, sem token), LOGAR e tentar no próximo tick —
//     nunca derrubar o programa.
//
// CONCEITOS: goroutine, context.Context, time.Ticker, idempotência (rodar 2x
// o mesmo Collect não pode duplicar nada — quem garante é o Store.Add).
package collector
