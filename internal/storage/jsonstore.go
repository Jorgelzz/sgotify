// ORDEM DE CRIAÇÃO: 8º (Etapa 8 — nosso histórico) · junto com storage.go
//
// POR QUE EXISTE:
//
//	É a primeira implementação concreta de Store: guarda os plays em
//	data/plays.json. Escolhida por ser a mais simples (só biblioteca padrão,
//	e você abre o arquivo no editor para ver o que está acontecendo).
//
// O QUE VAI AQUI (a implementar):
//   - struct JSONStore { path string; mu sync.Mutex; plays []Play; seen map[string]struct{} }
//   - NewJSONStore(path): carrega o arquivo se existir; senão começa vazio.
//   - Add: ignora o que já está em "seen" (chave PlayedAt+TrackID), ordena
//     por PlayedAt, grava o arquivo inteiro.
//   - Between / LastPlayedAt: leitura em memória.
//
// ARMADILHAS:
//   - O Mutex é obrigatório: o coletor escreve enquanto um handler lê.
//   - Escrita atômica: grave em "plays.json.tmp" e depois os.Rename — se o
//     programa morrer no meio, você não fica com um JSON pela metade.
//
// LIMITE CONHECIDO: reescrever o arquivo todo a cada Add não escala para
// milhões de plays, mas para uso pessoal (alguns milhares/ano) é irrelevante.
package storage
