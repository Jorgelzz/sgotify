# sgotify - SISTEMA PARA ESTUDOS

**API backend em Go** que conversa com o Spotify e expõe rotas JSON para um front-end (que ficará em outro projeto):

1. **O que estou ouvindo agora.**
2. **Meus mais ouvidos da semana.**
3. **Calendário:** para cada dia do mês, a música mais ouvida naquele dia (com a capa).

> Este README é o mapa do projeto. A ideia é **construir passo a passo, entendendo cada peça**.
> Cada arquivo já existe com um comentário no topo dizendo **em que ordem** deve ser implementado e **por que** existe.
> Só Go, só biblioteca padrão no começo. Sem front-end aqui.

---

## 1. A pegadinha que define toda a arquitetura

A API do Spotify **não tem** uma rota "o que ouvi em cada dia". O que existe:

| O que eu quero | Rota do Spotify | Limitação |
|---|---|---|
| Música atual | `GET /v1/me/player/currently-playing` | Só o instante atual. Devolve `204` (sem corpo) se nada toca. |
| Mais ouvidos | `GET /v1/me/top/tracks?time_range=...` | Só 3 janelas fixas: `short_term` (~4 semanas), `medium_term` (~6 meses), `long_term`. **Não existe "semana"**, nem dia. |
| Histórico | `GET /v1/me/player/recently-played?limit=50` | **Só as últimas 50 faixas.** Cada item tem `played_at`. |

Consequências:

- Para o **calendário** precisamos de histórico por dia. Como o Spotify só guarda 50 faixas, **nós guardamos o histórico**: um *coletor* roda de tempos em tempos, chama `recently-played` e salva no nosso PostgreSQL.
- O calendário só tem dados **a partir do dia em que o coletor começar a rodar**.
- O **"top da semana"** é calculado a partir do **nosso histórico** (contando plays dos últimos 7 dias), não pela rota `top/tracks`.

Duas fontes de dados: a **API ao vivo** (música atual) e o **nosso histórico** (semana + calendário).

---

## 2. Arquitetura em camadas

```
 Front-end (outro projeto)
        │  HTTP/JSON  (precisa de CORS, ver seção 5)
        ▼
 ┌───────────────────────────── Servidor Go ──────────────────────────────┐
 │                                                                        │
 │  server ──► handlers ──────► service ──────┬──► spotify (client)  ──► Spotify
 │  (rotas,    (HTTP ⇄ Go:      (regras de    │
 │  CORS)      lê request,      negócio)      └──► storage ◄── collector (goroutine)
 │             escreve JSON)                          │
 │                                                    └─ PostgreSQL (token + plays)
 └────────────────────────────────────────────────────────────────────────┘
```

Quem faz o quê (regra: **cada camada só conhece a de baixo**):

| Camada | Responsabilidade | NÃO deve |
|---|---|---|
| `handlers` | Ler a requisição (query, headers), chamar um service, escrever status + JSON | ter regra de negócio, falar com o Spotify |
| `service` | Regras: "top da semana = contar plays de 7 dias", "música do dia = mais plays, empate = mais recente" | saber o que é `http.Request` |
| `spotify` | Falar com a API do Spotify (URLs, token, formatos) | saber de calendário ou de HTTP do nosso servidor |
| `storage` | Guardar/ler plays | saber de Spotify |
| `collector` | Em segundo plano, copiar o histórico do Spotify para o storage | — |

**Única exceção consciente à regra "storage não conhece Spotify":** `storage/tokenstore.go` importa o pacote `spotify`, porque implementa a interface `spotify.TokenStore` e devolve `spotify.Token` / `spotify.ErrNoToken`. A interface fica declarada no `spotify` (lado de quem consome) para evitar um ciclo de imports. O resto do `storage` (`Play`, `Store`, `playstore.go`) não conhece o Spotify. Da mesma forma, `handlers/auth.go` chama `spotify/auth` direto, sem `service` no meio: o login é só protocolo OAuth, não há regra de negócio.

Por que separar `service` de `handlers`? Porque a regra do calendário não pode depender de HTTP: assim você a testa com `go test`, sem servidor, sem internet e sem login.

Dois fluxos independentes:

- **Leitura** (front chama a API): handler → service → (client Spotify **ou** storage) → JSON.
- **Coleta** (segundo plano): `collector` acorda a cada N min → `spotify.RecentlyPlayed` → `storage.Add` (ignorando duplicados).

---

## 3. O que é a pasta `internal/`

Não é um arquivo nem algo obrigatório: é uma **regra do compilador Go**. Um pacote dentro de `X/internal/...` **só pode ser importado por código que esteja dentro de `X`**.

Neste projeto o módulo é `github.com/jorgelzz/sgotify`. Então:

- `main.go` e qualquer pacote nosso podem importar `github.com/jorgelzz/sgotify/internal/spotify`. ✅
- Outro projeto que importe isso recebe erro de compilação (`use of internal package ... not allowed`). ❌

Para que serve: tudo ali dentro pode ser renomeado ou refatorado sem quebrar ninguém de fora. Para uma aplicação (não uma biblioteca) é a convenção padrão: "isto é detalhe interno".

Layout comum que vamos seguir: código da aplicação em `internal/`, ponto de entrada na raiz (`main.go`). Projetos maiores costumam mover o entry point para `cmd/<nome>/main.go` (útil quando há vários executáveis); aqui não precisa.

---

## 4. Autenticação (OAuth 2.0 Authorization Code + PKCE)

Os dados são **do meu usuário**, então preciso da permissão dele.

1. Abro `GET /login` no nosso servidor.
2. O servidor gera `code_verifier` aleatório, calcula o `code_challenge` (SHA-256) e um `state` (anti-CSRF), guarda `state` e `verifier` em dois cookies (`HttpOnly`, `SameSite=Lax`, 10 min, só no caminho `/callback`: o servidor não guarda estado em memória) e **redireciona** para `https://accounts.spotify.com/authorize?...` com os *scopes*.
3. Aprovo na tela do Spotify.
4. O Spotify redireciona para `GET /callback?code=...&state=...`.
5. O servidor confere o `state`, troca `code` + `code_verifier` por **access token** (~1h) e **refresh token** em `POST https://accounts.spotify.com/api/token`.
6. Salva na tabela `spotify_tokens` do PostgreSQL (uma única linha: só existe um usuário). Quando o access token expira, usa o refresh token para pegar outro **sem novo login**. É isso que permite o coletor rodar sozinho.
7. O `/callback` termina redirecionando o browser para o front-end (`FRONTEND_URL`).

Detalhe importante: o **token fica só no backend**. O front nunca o vê; ele só chama nossas rotas `/api/*`. Como é uso pessoal (um usuário), o backend usa o mesmo token para qualquer requisição, então **as rotas `/api/*` não têm autenticação própria**. Por isso, não exponha este servidor na internet sem antes protegê-lo.

**Scopes:**

| Scope | Para quê |
|---|---|
| `user-read-currently-playing` | música atual |
| `user-read-recently-played` | histórico (coletor) |
| `user-top-read` | `top/tracks` (opcional: `spotify/top.go` existe, mas **nenhuma rota** o usa; o top da semana vem do nosso histórico) |

**Dashboard do Spotify** (https://developer.spotify.com/dashboard):

1. Criar um app → anotar o **Client ID**.
2. Em *Redirect URIs* cadastrar exatamente `http://127.0.0.1:8080/callback`. O Spotify **não aceita mais `localhost`**; use o IP de loopback `127.0.0.1`.
3. Copiar `.env.example` para `.env` e preencher. O `.env` já está no `.gitignore`.

---

## 5. Front-end separado ⇒ CORS

Como o front rodará em outra origem (ex.: `http://127.0.0.1:5173` falando com `http://127.0.0.1:8080`), o browser **bloqueia** as respostas a não ser que o backend envie os headers de CORS (`Access-Control-Allow-Origin` etc.) e responda ao preflight `OPTIONS`.

- Fica em um *middleware* em `internal/server/middleware.go` (função que "envolve" o mux e mexe em toda requisição).
- A origem permitida vem do `.env` (`CORS_ALLOWED_ORIGIN`). **Não** use `*` por padrão.
- O preflight (`OPTIONS`) responde `204` sem chamar o handler. O middleware envolve o mux inteiro porque o mux só registra `GET`.
- Atenção: `localhost` e `127.0.0.1` são origens **diferentes** para o browser.

---

## 6. Rotas do **nosso** servidor

| Rota | O que faz | Camadas |
|---|---|---|
| `GET /health` | Servidor vivo | handlers/health |
| `GET /login` | Inicia o OAuth | handlers/auth → spotify/auth |
| `GET /callback` | Recebe o `code`, troca por tokens, salva, redireciona ao front | handlers/auth → spotify/auth, token |
| `GET /api/now-playing` | Música atual (ou `{"playing": false}`) | handler → service/nowplaying → spotify/player |
| `GET /api/top-week?limit=10` | Top músicas dos últimos 7 dias | handler → service/top → storage |
| `GET /api/calendar?month=2026-11` | Para cada dia do mês: música mais ouvida + capa | handler → service/calendar → storage |

Só existem estas 6 rotas (não há `GET /`: esta API não serve front-end).

**Formato das respostas:**

| Rota | Sucesso | Erros |
|---|---|---|
| `/api/now-playing` | `{"playing":true,"track","artist","image","progress_ms","duration_ms"}`; nada tocando ou pausado: `{"playing":false}` | `401` com `login_url` se nunca fez login; `502` se o Spotify falhar |
| `/api/top-week?limit=10` | `{"tracks":[{"track","artist","image","plays"}]}`; `limit` padrão 10, máximo 50 | `400` se `limit` não for inteiro de 1 a 50; `500` se o banco falhar |
| `/api/calendar?month=2026-11` | `{"month":"2026-11","days":[{"date","track","artist","image","plays"}]}`; dias sem plays **não aparecem**; sem `month` vale o mês atual | `400` se `month` não for `AAAA-MM`; `500` se o banco falhar |
| `/callback` | `302` para `FRONTEND_URL` | `400` (state/cookies/code inválidos, ou login cancelado), `502`, `500`; texto simples, pois quem chama é o browser |

Os corpos de erro da API são `{"error":"..."}`; detalhes internos vão só para o log.

**Regra do calendário:** por dia, conta plays por música; vence quem tem mais; empate → a tocada mais recentemente. O "dia" é calculado no **fuso local** (`America/Sao_Paulo`), porque o `played_at` do Spotify vem em UTC e uma música das 22h cairia no dia seguinte.

---

## 7. Estrutura de pastas e **ordem de construção**

```
sgotify/
├── go.mod                          [0] já existia
├── .gitignore                      [0] já existia (a linha data/ ficou sem uso: agora é Postgres)
├── main.go                         [0] /health → [11] versão final (monta tudo, shutdown limpo)
├── README.md                       [1] este arquivo
├── .env.example                    [2] modelo das variáveis de ambiente
│
└── internal/                       (código só deste módulo — ver seção 3)
    ├── config/
    │   └── config.go               [2] lê .env/variáveis
    ├── spotify/                    (tudo que fala com o Spotify)
    │   ├── auth.go                 [3] PKCE, URL de login, troca de code, refresh
    │   ├── token.go                [3] struct do token, Expired, interface TokenStore [4a]
    │   ├── types.go                [5] Track, Artist, Album, Image (formato das respostas)
    │   ├── client.go               [5] http client: Bearer + renova token
    │   ├── player.go               [6] currently-playing
    │   ├── top.go                  [7] top tracks do Spotify (sem rota: o top da semana vem do histórico)
    │   └── history.go              [9] recently-played
    ├── server/
    │   ├── server.go               [4] monta as rotas (mux)
    │   └── middleware.go           [4] CORS + log de requisições (sem a query string)
    ├── handlers/                   (HTTP ⇄ Go)
    │   ├── json.go                 [4] writeJSON, erros padronizados (401/502/500)
    │   ├── health.go               [4] /health
    │   ├── auth.go                 [4] /login e /callback
    │   ├── nowplaying.go           [6] /api/now-playing
    │   ├── top.go                  [7] /api/top-week
    │   └── calendar.go             [10] /api/calendar
    ├── service/                    (regras de negócio)
    │   ├── nowplaying.go           [6]
    │   ├── top.go                  [7]
    │   └── calendar.go             [10]
    ├── storage/                    (tudo que fala com o PostgreSQL)
    │   ├── postgres.go             [4a] Open (conexão) + Migrate (cria tabelas)
    │   ├── tokenstore.go           [4a] implementa spotify.TokenStore (tabela spotify_tokens)
    │   ├── storage.go              [8] modelo Play + interface Store
    │   └── playstore.go            [8] implementa Store (tabela plays)
    └── collector/
        └── collector.go            [9] goroutine que coleta o histórico
```

Tabelas no PostgreSQL: `spotify_tokens` (1 linha) e `plays` (1 linha por vez que ouvi uma música). O SQL de cada uma está nos comentários de `postgres.go` e `playstore.go`.

| Etapa | O que construir | Como saber que funcionou | O que você aprende |
|---|---|---|---|
| 0 | Servidor + `/health` | `curl 127.0.0.1:8080/health` | `net/http`, `ServeMux` |
| 1 | README | — | planejar antes de codar |
| 2 | `config` + `.env.example` | imprimir o Client ID lido | variáveis de ambiente, segredos fora do código |
| 3 | `spotify/auth.go`, `token.go` | funções geram URL de login válida | OAuth, PKCE, SHA-256, base64url |
| 4a | `storage/postgres.go` + `tokenstore.go` + `spotify.TokenStore` | Save/Load do token contra o banco (teste de integração) | `database/sql`, driver `pgx`, SQL, `ON CONFLICT`, `ErrNoToken` |
| 4b | `server` + `middleware` + `handlers/auth.go` | `/login` → Spotify → `/callback` → linha criada em `spotify_tokens` | redirects, query params, `state`, middleware, CORS |
| 5 | `spotify/client.go` | chamar o Spotify com token válido (renovando se expirou) | `http.Client`, headers, refresh, mutex |
| 6 | `player.go` + service + handler | `/api/now-playing` mostra a música | decodificar JSON, tratar `204`, 1º service |
| 7 | `top.go` + service + handler | `/api/top-week` devolve a lista | query params; ver a limitação do Spotify |
| 8 | `storage/storage.go` + `playstore.go` | gravar e ler plays na tabela `plays`, sem duplicar | interfaces, transações, índices, `ON CONFLICT DO NOTHING` |
| 9 | `history.go` + `collector` | deixar rodando e ver a tabela `plays` crescer sem duplicar | goroutines, `time.Ticker`, idempotência |
| 10 | `service/calendar` + handler | `/api/calendar` devolve a música de cada dia | agrupamento, fuso horário, testes |
| 11 | `main.go` final | `go run .` sobe servidor + coletor | composição / injeção de dependências, shutdown limpo |

---

## 8. Modelo de dados

Um registro por vez que ouvi uma música (`storage.Play`):

```json
{
  "played_at": "2026-11-24T15:03:11Z",
  "track_id": "4uLU6hMCjMI75M1A2tKUQC",
  "track_name": "Never Gonna Give You Up",
  "artist": "Rick Astley",
  "album_image_url": "https://i.scdn.co/image/..."
}
```

- **Chave única:** `played_at + track_id` (a `PRIMARY KEY` da tabela `plays`). O coletor sempre revê itens já salvos (as 50 últimas se sobrepõem); a chave evita duplicar, com `INSERT ... ON CONFLICT DO NOTHING`.
- Nome, artista e capa ficam **junto** para o calendário não chamar o Spotify de novo.

### Armazenamento: PostgreSQL

Tudo persistente vive no Postgres, em duas tabelas:

| Tabela | Conteúdo | Quem usa |
|---|---|---|
| `spotify_tokens` | **1 linha**: access token, refresh token, `expires_at` | `/callback` (grava), `spotify/client.go` (lê e renova) |
| `plays` | Uma linha por música ouvida (o `Play` acima) | `collector` (grava), `service/top` e `service/calendar` (leem) |

Decisões:

- **Driver:** `pgx` (`github.com/jackc/pgx/v5/stdlib`) usado através de `database/sql`, a interface da biblioteca padrão. É a primeira dependência externa do projeto.
- **Interfaces mantidas:** `spotify.TokenStore` e `storage.Store` continuam sendo interfaces. Os services e o client não conhecem o banco, e os testes deles usam versões falsas em memória (sem Postgres).
- **Calendário em Go, não em SQL:** o Postgres só guarda e filtra por período (`WHERE played_at >= $1 AND played_at < $2`). A regra "mais plays, empate pela mais recente, no fuso local" fica em `service/calendar`, onde é fácil de testar sem banco.
- **Criação das tabelas:** `Migrate` roda `CREATE TABLE IF NOT EXISTS` na inicialização. Simples e suficiente para duas tabelas; uma ferramenta de migração (ex.: golang-migrate) só valeria se o schema começasse a mudar.
- **Sem usuário:** o `CHECK (id = 1)` em `spotify_tokens` impede uma segunda linha; o sistema tem um usuário só.

### Testes com banco

Testes de `storage` são de **integração**: precisam de um Postgres de verdade e **apagam linhas**. Por isso:

- usam um banco **separado** (`sgotify_test`), lido da variável de ambiente `TEST_DB_URL`;
- `TEST_DB_URL` é uma variável de **ambiente do `go test`**: ela **não** é lida do `.env` (de propósito: assim o `.env` do dia a dia nunca aponta testes que apagam linhas para o banco errado);
- se `TEST_DB_URL` estiver vazia, são **pulados** (`t.Skip`) e `go test ./...` continua passando;
- o banco real (`DB_URL`) nunca é usado em teste.

```powershell
# PowerShell
$env:TEST_DB_URL = "postgres://USUARIO:SENHA@localhost:5432/sgotify_test?sslmode=disable"
go test ./...
```

```bash
# bash
TEST_DB_URL='postgres://USUARIO:SENHA@localhost:5432/sgotify_test?sslmode=disable' go test ./...
```

---

## 9. Coletor: frequência

`recently-played` devolve no máximo 50 faixas. Com músicas de ~3 min, 50 faixas ≈ 2h30. Coletando a cada **15–30 min** não perdemos nada. O coletor usa o parâmetro `after` (ms Unix da última faixa salva) para pedir só o novo.

Limite honesto: **o coletor só funciona com o servidor ligado.** Se ficar desligado e você ouvir mais de 50 faixas nesse intervalo, as mais antigas se perdem.

---

## 10. Como rodar

**Estado:** todas as etapas (0 a 11) estão implementadas e cobertas por testes. O que **não** dá para automatizar é o login real no Spotify (precisa do seu browser e do seu Client ID): ele só é exercitado manualmente, em `/login`.

**Pré-requisito:** um PostgreSQL rodando e acessível pela `DB_URL`. Crie os bancos uma vez (no `psql` ou em uma ferramenta gráfica):

```sql
CREATE DATABASE sgotify;        -- app
CREATE DATABASE sgotify_test;   -- testes de integração
```

```bash
cp .env.example .env     # preencher SPOTIFY_CLIENT_ID e DB_URL (com a senha real)
go run .
curl http://127.0.0.1:8080/health
# abrir http://127.0.0.1:8080/login uma vez; depois o front consome /api/*
```

Sem login feito, `/api/now-playing` responde `401` e o coletor só registra no log "waiting for login". Depois do login, a primeira coleta acontece na próxima rodada (no máximo `COLLECT_INTERVAL_MINUTES` depois) e segue a cada intervalo. `Ctrl+C` encerra com calma (para o coletor, termina as requisições em andamento e fecha o banco).

---

## 11. Fora do escopo

- Front-end (outro projeto).
- Múltiplos usuários (hoje é um só: eu).
- Autenticação própria nas rotas `/api/*` e deploy público.
- Controlar o player (pausar, pular).
- Ferramenta de migração de schema (por enquanto `CREATE TABLE IF NOT EXISTS` basta).
- Criptografar o token no banco (uso pessoal, banco local).
