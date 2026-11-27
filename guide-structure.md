# sgotify: guia técnico (fluxos e estrutura)

Este documento explica **como o sgotify funciona por dentro**: arquitetura, fluxos, rotas, configuração, estrutura de pastas,
dados, testes e limites. O **escopo** do projeto (o que é, para que serve, o que fica de fora) e como rodar estão no
[README.md](README.md).

Índice:
1. [A pegadinha que define a arquitetura](#1-a-pegadinha-que-define-a-arquitetura)
2. [Arquitetura em camadas](#2-arquitetura-em-camadas)
3. [A pasta `internal/`](#3-a-pasta-internal)
4. [Fluxo de login e identidade](#4-fluxo-de-login-e-identidade)
5. [Autenticação das rotas `/api/*` (sessão)](#5-autenticação-das-rotas-api-sessão)
6. [CORS](#6-cors)
7. [Rotas e formatos](#7-rotas-e-formatos)
8. [Configuração](#8-configuração)
9. [Estrutura de pastas e ordem de construção](#9-estrutura-de-pastas-e-ordem-de-construção)
10. [Modelo de dados e PostgreSQL](#10-modelo-de-dados-e-postgresql)
11. [Coletor](#11-coletor)
12. [Testes](#12-testes)
13. [Decisões e limites conhecidos](#13-decisões-e-limites-conhecidos)

---

## 1. A pegadinha que define a arquitetura

A API do Spotify **não tem** uma rota "o que ouvi em cada dia". O que existe:

| O que eu quero | Rota do Spotify | Limitação |
|---|---|---|
| Música atual | `GET /v1/me/player/currently-playing` | Só o instante atual. Devolve `204` (sem corpo) se nada toca. |
| Mais ouvidos | `GET /v1/me/top/tracks?time_range=...` | Só 3 janelas fixas: `short_term` (~4 semanas), `medium_term` (~6 meses), `long_term`. **Não existe "semana"**, nem dia. |
| Histórico | `GET /v1/me/player/recently-played?limit=50` | **Só as últimas 50 faixas.** Cada item tem `played_at`. |
| Quem sou eu | `GET /v1/me` | Devolve o `id` da conta dona do token (usado na identidade, seção 4). |

Consequências:

- Para o **calendário** precisamos de histórico por dia. Como o Spotify só guarda 50 faixas, **nós guardamos o histórico**: um
  *coletor* roda de tempos em tempos, chama `recently-played` e salva no nosso PostgreSQL.
- O calendário só tem dados **a partir do dia em que o coletor começou a rodar**.
- O **"top da semana"** é calculado a partir do **nosso histórico** (contando plays dos últimos 7 dias), não pela rota `top/tracks`.
  (`spotify/top.go` existe, mas nenhuma rota o usa.)

Duas fontes de dados: a **API ao vivo** (música atual) e o **nosso histórico** (semana + calendário).

---

## 2. Arquitetura em camadas

```
 Front-end (outro projeto)
        │  HTTP/JSON + cookie de sessão  (precisa de CORS, seção 6)
        ▼
 ┌───────────────────────────── Servidor Go ──────────────────────────────┐
 │                                                                        │
 │  server ──► handlers ──────► service ──────┬──► spotify (client)  ──► Spotify
 │  (rotas,    (HTTP ⇄ Go:      (regras de    │
 │  CORS,      lê request,      negócio)      └──► storage ◄── collector (goroutine)
 │  sessão)    escreve JSON)                          │
 │      │                                             └─ PostgreSQL (token + plays)
 │      └─ session (cookie assinado)                                       │
 └────────────────────────────────────────────────────────────────────────┘
```

Regra: **cada camada só conhece a de baixo**.

| Camada | Responsabilidade | NÃO deve |
|---|---|---|
| `server` | Registrar as rotas, CORS, log, exigir sessão em `/api/*` | ter regra de negócio |
| `handlers` | Ler a requisição (query, headers), chamar um service, escrever status + JSON | ter regra de negócio, falar com o Spotify |
| `service` | Regras: "top da semana = contar plays de 7 dias", "música do dia = mais plays, empate = mais recente" | saber o que é `http.Request` |
| `spotify` | Falar com a API do Spotify (URLs, token, formatos) | saber de calendário ou de HTTP do nosso servidor |
| `storage` | Guardar/ler plays e o token | saber de Spotify |
| `session` | Emitir e validar o cookie de sessão | saber de rotas ou de Spotify |
| `collector` | Em segundo plano, copiar o histórico do Spotify para o storage | — |

Por que separar `service` de `handlers`? Porque a regra do calendário não pode depender de HTTP: assim você a testa com `go test`,
sem servidor, sem internet e sem login.

**Duas exceções conscientes à regra das camadas:**

1. `storage/tokenstore.go` importa `spotify`, porque implementa a interface `spotify.TokenStore` e devolve `spotify.Token` /
   `spotify.ErrNoToken`. A interface fica declarada no `spotify` (do lado de quem **consome**) para evitar um ciclo de imports.
   O resto do `storage` (`Play`, `Store`, `playstore.go`) não conhece o Spotify.
2. `handlers/auth.go` chama o `spotify` direto, sem `service` no meio: o login é protocolo OAuth, não há regra de negócio.

**Interfaces do lado de quem consome.** Cada pacote declara só o que precisa dos outros (ex.: `handlers` define
`nowPlayingGetter`, `identityAPI`, `sessionIssuer`). Os tipos reais satisfazem as interfaces automaticamente, e os testes
passam falsos em memória.

Dois fluxos independentes:

- **Leitura** (front chama a API): `server` (sessão) → handler → service → (client Spotify **ou** storage) → JSON.
- **Coleta** (segundo plano): `collector` acorda a cada N min → `spotify.RecentlyPlayed` → `storage.Add` (ignorando duplicados).

---

## 3. A pasta `internal/`

Não é um arquivo nem algo obrigatório: é uma **regra do compilador Go**. Um pacote dentro de `X/internal/...` **só pode ser
importado por código que esteja dentro de `X`**.

Neste projeto o módulo é `github.com/jorgelzz/sgotify`. Então:

- `main.go` e qualquer pacote nosso podem importar `github.com/jorgelzz/sgotify/internal/spotify`. ✅
- Outro projeto que importe isso recebe erro de compilação (`use of internal package ... not allowed`). ❌

Para que serve: tudo ali dentro pode ser renomeado ou refatorado sem quebrar ninguém de fora. Para uma aplicação (não uma
biblioteca) é a convenção padrão: "isto é detalhe interno". O ponto de entrada fica na raiz (`main.go`).

---

## 4. Fluxo de login e identidade

O login do Spotify (OAuth 2.0 Authorization Code + **PKCE**) faz **duas coisas** ao mesmo tempo:

1. **Autorização:** o Spotify deixa o nosso servidor ler os seus dados (gera o token).
2. **Identidade:** provamos que quem logou é a **sua** conta e, só então, damos ao browser a sessão que libera `/api/*`.

```
Browser                    sgotify                          Spotify
  │  GET /login               │                                │
  │──────────────────────────►│ gera verifier + state          │
  │                           │ guarda os dois em cookies      │
  │◄──────────────────────────│ 302 → accounts.spotify.com     │
  │  aprova a permissão ─────────────────────────────────────►│
  │◄───────────────────────────────────────── 302 /callback?code=...&state=...
  │  GET /callback            │                                │
  │──────────────────────────►│ 1. confere o state (cookie)    │
  │                           │ 2. troca code+verifier ───────►│ → access + refresh token
  │                           │ 3. GET /v1/me (com o token) ──►│ → {"id": "..."}
  │                           │ 4. id == ALLOWED_SPOTIFY_USER_ID ?
  │                           │      não → 403, NADA é gravado
  │                           │      sim → 5. grava o token no Postgres
  │                           │            6. emite o cookie de sessão
  │◄──────────────────────────│ 302 → FRONTEND_URL
```

Detalhes importantes:

- **PKCE:** o servidor gera um `code_verifier` aleatório e envia ao Spotify só o `code_challenge` (SHA-256 em base64url). Por isso
  **não existe client secret**. O `state` (aleatório) protege contra CSRF.
- **`state` e `verifier` ficam em cookies** (`sgotify_state`, `sgotify_verifier`): `HttpOnly`, `SameSite=Lax`, 10 minutos,
  `Path=/callback`. Assim o servidor **não guarda estado em memória** (reiniciar no meio do login não quebra) e o `state` fica
  amarrado ao browser que iniciou o login. São apagados no primeiro uso. A comparação do `state` é em tempo constante.
- **A identidade vem antes do `Save`:** uma conta de fora **não pode sobrescrever** o token guardado (senão a API passaria a mostrar a
  conta dela). A tabela `spotify_tokens` tem uma linha só.
- **Um `id` vazio nunca passa:** `spotify.UserID` devolve erro se o `id` vier vazio, o `config` exige `ALLOWED_SPOTIFY_USER_ID`, e o
  handler ainda recusa se qualquer um dos dois for vazio (senão `"" == ""` liberaria qualquer um).
- **Contas recusadas:** o `id` visto vai para o **log**, não para a resposta. É assim que você descobre o seu id na primeira vez.
- **Scopes pedidos:** `user-read-currently-playing`, `user-read-recently-played`, `user-top-read`. O `id` do `/v1/me` parece
  não exigir scope. Se o Spotify responder `403` no seu primeiro login real, acrescente `user-read-private` em `spotify/auth.go`.
- **Token só no backend:** o front nunca o vê. Quando o access token expira (~1h), o client usa o refresh token para pegar outro
  **sem novo login**, sob um mutex (para handler e coletor não renovarem ao mesmo tempo). É isso que permite o coletor rodar sozinho.
- **Redirect URI:** o Spotify exige o IP de loopback `http://127.0.0.1:8080/callback`; **não aceita** `localhost`.

---

## 5. Autenticação das rotas `/api/*` (sessão)

Depois do login, o browser recebe um cookie de sessão. O middleware `RequireSession` (em `internal/server`) exige esse cookie em
`/api/*`; sem ele, responde `401` e **nem chama o handler**.

**O cookie** (`sgotify_session`) vale `<validade>.<assinatura>`:

- `validade`: instante de expiração, em segundos Unix;
- `assinatura`: **HMAC-SHA256** da validade, calculado com o `SESSION_SECRET`, em base64url.

Só quem conhece o segredo gera uma assinatura que bate. Logo, ninguém forja um cookie nem estica a validade de um verdadeiro
(mudar o número invalida a assinatura). **Não há tabela nem estado no servidor.**

Regras de validação (`session.verify`): (1) o formato tem que estar certo; (2) a assinatura é recalculada e comparada com
`hmac.Equal` (tempo constante, nunca `==`); (3) **só depois** a validade é lida e tem que estar no futuro. Qualquer entrada ruim
devolve "inválido", nunca pânico.

Atributos do cookie: `HttpOnly` (o JavaScript da página não o lê), `SameSite=Lax`, `Path=/`, validade de **7 dias**.

| Rota | Sessão |
|---|---|
| `/health`, `/login`, `/callback` | **não exigem** (`/login` e `/callback` são como se ganha a sessão) |
| `/api/now-playing`, `/api/top-week`, `/api/calendar` | **exigem** |
| Preflight `OPTIONS` | **não exige** (o browser o envia sem cookies) |

Os dois 401 que o front pode receber, ambos com `login_url`:

- `{"error":"authentication required","login_url":"/login"}`: **sem sessão** (middleware).
- `{"error":"not logged in to Spotify","login_url":"/login"}`: sessão ok, mas ainda **não há token do Spotify** guardado (service).

Em ambos os casos o front manda o usuário para `/login`.

Sobre o `SESSION_SECRET`: precisa ter **≥ 32 bytes** e ser aleatório (`openssl rand -base64 32`). Quem o conhecer forja sessões. Trocar
o valor invalida todas as sessões (é a única forma de "deslogar", já que não há `/logout`).

---

## 6. CORS

O front-end roda em **outra origem** (ex.: `http://127.0.0.1:5173` falando com `http://127.0.0.1:8080`), então o browser **bloqueia**
as respostas a não ser que o backend envie os headers de CORS. Fica em um *middleware* em `internal/server/middleware.go`, que
envolve o mux inteiro.

- `Access-Control-Allow-Origin` = a origem configurada (`CORS_ALLOWED_ORIGIN`). **Nunca `*`.**
- `Access-Control-Allow-Credentials: true`: necessário para o browser enviar o cookie de sessão. Só é aceito com origem **explícita**.
- `Vary: Origin`, e `Allow-Methods: GET, OPTIONS`, `Allow-Headers: Content-Type`.
- **Preflight** (`OPTIONS`): responde `204`, sem corpo, **sem chamar o handler**. O middleware envolve o mux porque o mux só registra
  `GET` (um `OPTIONS` cairia em `405`).
- O `401` também leva os headers de CORS: sem isso, o front não conseguiria **ler** o `401` para mandar o usuário ao `/login`.

No front, as chamadas precisam de `fetch(url, { credentials: "include" })`.

Atenção: `localhost` e `127.0.0.1` são origens (e sites) **diferentes** para o browser. Use o mesmo nome no front, no
`CORS_ALLOWED_ORIGIN` e no `FRONTEND_URL`.

Ordem das camadas no servidor, de fora para dentro: `Logger → CORS → mux → (sessão, só em /api/*) → handler`. O `Logger` registra
método, caminho, status e duração, e **nunca a query string** (no `/callback` ela traz o `code` do OAuth).

---

## 7. Rotas e formatos

Só existem estas 6 rotas. Não há `GET /`; qualquer outro método dá `405`.

| Rota | O que faz | Camadas |
|---|---|---|
| `GET /health` | Servidor vivo | handlers/health |
| `GET /login` | Inicia o OAuth | handlers/auth → spotify/auth |
| `GET /callback` | Conclui o login, confere a conta, grava o token, emite a sessão, redireciona ao front | handlers/auth → spotify (auth, me), storage, session |
| `GET /api/now-playing` | Música atual (ou `{"playing": false}`) | handler → service/nowplaying → spotify/player |
| `GET /api/top-week?limit=10` | Top músicas dos últimos 7 dias | handler → service/top → storage |
| `GET /api/calendar?month=2026-11` | Para cada dia do mês: música mais ouvida + capa | handler → service/calendar → storage |

**Formato das respostas** (as três `/api/*` exigem sessão):

| Rota | Sucesso | Erros |
|---|---|---|
| `/api/now-playing` | `{"playing":true,"track","artist","image","progress_ms","duration_ms"}`; nada tocando **ou pausado**: `{"playing":false}` | `401` sem sessão ou sem token do Spotify; `502` se o Spotify falhar |
| `/api/top-week?limit=10` | `{"tracks":[{"track","artist","image","plays"}]}`; `limit` padrão 10, máximo 50 | `400` se `limit` não for inteiro de 1 a 50; `500` se o banco falhar |
| `/api/calendar?month=2026-11` | `{"month":"2026-11","days":[{"date","track","artist","image","plays"}]}`; dias sem plays **não aparecem**; sem `month`, vale o mês atual | `400` se `month` não for `AAAA-MM`; `500` se o banco falhar |
| `/callback` | `302` para `FRONTEND_URL` | `400` (state/cookies/code inválidos, ou login cancelado), `403` (conta não permitida), `502`, `500`; texto simples, pois quem chama é o browser |

Os corpos de erro da API são `{"error":"..."}`; detalhes internos vão só para o log, nunca para a resposta. Listas vazias saem como
`[]`, nunca `null`.

**Regra do calendário:** por dia, conta plays por música; vence quem tem mais; empate → a tocada mais recentemente. O "dia" é
calculado no **fuso local** (`America/Sao_Paulo` por padrão), porque o `played_at` do Spotify vem em UTC e uma música das 22h cairia
no dia seguinte.

**Regra do top da semana:** os últimos 7 dias do nosso histórico, contando plays por música.

---

## 8. Configuração

Lida uma vez por `config.Load()`. As variáveis de ambiente **ganham** do `.env`; um `.env` ausente não é erro.

| Variável | Obrigatória | Padrão | O que é |
|---|---|---|---|
| `SPOTIFY_CLIENT_ID` | **sim** | | Client ID do app no Spotify (com PKCE não há secret) |
| `DB_URL` | **sim** | | Conexão do PostgreSQL |
| `ALLOWED_SPOTIFY_USER_ID` | **sim** | | A única conta Spotify autorizada |
| `SESSION_SECRET` | **sim** | | Chave do HMAC do cookie de sessão (≥ 32 bytes) |
| `SPOTIFY_REDIRECT_URI` | não | `http://127.0.0.1:8080/callback` | Idêntico ao cadastrado no dashboard |
| `PORT` | não | `8080` | Porta do servidor |
| `FRONTEND_URL` | não | `http://localhost:3000` | Para onde o `/callback` leva o browser |
| `CORS_ALLOWED_ORIGIN` | não | `http://localhost:3000` | Origem permitida pelo CORS |
| `TIMEZONE` | não | `America/Sao_Paulo` | Fuso que define "em que dia" uma música foi ouvida |
| `COLLECT_INTERVAL_MINUTES` | não | `20` | Intervalo do coletor (> 0) |

`TEST_DB_URL` **não** é lida do `.env`: é uma variável de ambiente só do `go test` (seção 12).

Notas: `.env.example` sugere `http://127.0.0.1:5173` para `FRONTEND_URL` e `CORS_ALLOWED_ORIGIN`; os padrões do código só valem se a
variável estiver ausente. O fuso vem embutido no binário (`time/tzdata`), porque o Windows não traz o banco de fusos.

---

## 9. Estrutura de pastas e ordem de construção

```
sgotify/
├── go.mod, go.sum
├── .gitignore                      (.env ignorado)
├── main.go                         [0] /health → [11] versão final (monta tudo, shutdown limpo)
├── README.md                       escopo e como rodar
├── guide-structure.md              este guia
├── .env.example                    modelo das variáveis de ambiente
│
└── internal/                       (código só deste módulo, ver seção 3)
    ├── config/
    │   ├── config.go               [2] lê .env/variáveis e valida
    │   └── config_test.go
    ├── spotify/                    (tudo que fala com o Spotify)
    │   ├── auth.go                 [3] PKCE, URL de login, troca de code, refresh
    │   ├── token.go                [3] struct do token, Expired, interface TokenStore
    │   ├── types.go                [5] Track, Artist, Album, Image (formato das respostas)
    │   ├── client.go               [5] http client: Bearer + renova token (get / getWithToken)
    │   ├── player.go               [6] currently-playing
    │   ├── top.go                  [7] top tracks do Spotify (sem rota)
    │   ├── history.go              [9] recently-played
    │   ├── me.go                   [12] GET /me: de quem é este token?
    │   └── *_test.go               auth, token, client, me
    ├── session/                    [12] cookie de sessão assinado (HMAC)
    │   ├── session.go
    │   └── session_test.go
    ├── server/
    │   ├── server.go               [4] monta as rotas (mux) e aplica a sessão em /api/*
    │   ├── middleware.go           [4] CORS, Logger, RequireSession
    │   └── server_test.go
    ├── handlers/                   (HTTP ⇄ Go)
    │   ├── json.go                 [4] writeJSON, erros padronizados (401/502/500)
    │   ├── health.go               [4] /health
    │   ├── auth.go                 [4] /login e /callback (+ identidade e sessão em [12])
    │   ├── nowplaying.go           [6] /api/now-playing
    │   ├── top.go                  [7] /api/top-week
    │   ├── calendar.go             [10] /api/calendar
    │   └── handlers_test.go
    ├── service/                    (regras de negócio)
    │   ├── nowplaying.go           [6]
    │   ├── top.go                  [7]
    │   ├── calendar.go             [10]
    │   └── service_test.go
    ├── storage/                    (tudo que fala com o PostgreSQL)
    │   ├── postgres.go             [4a] Open (conexão) + Migrate (cria tabelas)
    │   ├── tokenstore.go           [4a] implementa spotify.TokenStore
    │   ├── storage.go              [8] modelo Play + interface Store
    │   ├── playstore.go            [8] implementa Store
    │   └── storage_test.go         (integração; precisa de TEST_DB_URL)
    └── collector/
        ├── collector.go            [9] goroutine que coleta o histórico
        └── collector_test.go
```

Os testes ficam **ao lado do código** (`_test.go` no mesmo pacote), que é a convenção de Go: assim enxergam os detalhes internos e
andam junto com o arquivo que testam.

A ordem de construção (etapas 0 a 12) e o que cada uma ensina estão no roteiro de estudo do [README.md](README.md#4-roteiro-de-estudo).

---

## 10. Modelo de dados e PostgreSQL

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

- **Chave única:** `played_at + track_id` (a `PRIMARY KEY` da tabela `plays`). O coletor sempre revê itens já salvos (as 50 últimas
  se sobrepõem); a chave evita duplicar, com `INSERT ... ON CONFLICT DO NOTHING`.
- Nome, artista e capa ficam **junto** para o calendário não chamar o Spotify de novo.

Tudo persistente vive no PostgreSQL, em duas tabelas:

| Tabela | Conteúdo | Quem usa |
|---|---|---|
| `spotify_tokens` | **1 linha**: access token, refresh token, `expires_at` | `/callback` (grava), `spotify/client.go` (lê e renova) |
| `plays` | Uma linha por música ouvida (o `Play` acima) | `collector` (grava), `service/top` e `service/calendar` (leem) |

Decisões:

- **Driver:** `pgx` (`github.com/jackc/pgx/v5/stdlib`) usado através de `database/sql`, a interface da biblioteca padrão. É a
  única dependência externa do projeto.
- **Interfaces mantidas:** `spotify.TokenStore` e `storage.Store` são interfaces. Services e client não conhecem o banco, e os
  testes deles usam versões falsas em memória.
- **Calendário em Go, não em SQL:** o Postgres só guarda e filtra por período (`WHERE played_at >= $1 AND played_at < $2`). A regra
  "mais plays, empate pela mais recente, no fuso local" fica em `service/calendar`, onde é fácil de testar sem banco.
- **Criação das tabelas:** `Migrate` roda `CREATE TABLE IF NOT EXISTS` na inicialização. Simples e suficiente para duas tabelas.
- **Sem multiusuário:** o `CHECK (id = 1)` em `spotify_tokens` impede uma segunda linha.
- A mensagem de erro de conexão **não** inclui a `DB_URL` (ela tem a senha).

---

## 11. Coletor

`recently-played` devolve no máximo 50 faixas. Com músicas de ~3 min, 50 faixas ≈ 2h30. Coletando a cada **15–30 min** não perdemos
nada. O coletor usa o parâmetro `after` (ms Unix da última faixa salva) para pedir só o novo.

- Roda numa **goroutine** iniciada no `main`; coleta uma vez **ao iniciar** e depois a cada `COLLECT_INTERVAL_MINUTES` (`time.Ticker`).
- **Nunca derruba o programa:** uma falha (rede, Spotify instável, `429`, ainda sem login) vai só para o log, e a próxima rodada tenta
  de novo. Sem login, o log diz `collector: waiting for login`.
- É **idempotente**: rodar duas vezes sobre os mesmos itens não duplica nada (a chave primária cuida disso).
- Para quando o `context` é cancelado (`Ctrl+C`/`SIGTERM`); o `main` espera a coleta em andamento terminar antes de fechar o banco.
- O coletor usa o token do banco, **não** a sessão do browser: funciona sozinho depois do primeiro login.

Limite honesto: **o coletor só funciona com o servidor ligado.**

---

## 12. Testes

```bash
go test ./...
```

- **Unitários, com falsos:** `spotify` (servidor HTTP falso via `httptest`), `service`, `collector`, `handlers`, `server`, `session`
  e `config` rodam **sem internet, sem banco e sem login**.
- **Handlers:** `httptest.NewRecorder` grava a resposta em memória; os services entram como falsos.
- **Sessão:** o relógio é injetado (`m.now`) para testar vencimento e a fronteira exata da validade; o teste do servidor usa o
  `session.Manager` real e confere o formato do cookie contra uma assinatura HMAC feita à mão, de forma independente.
- **`storage` é de integração:** precisa de um PostgreSQL real e **apaga linhas**. Por isso:
  - usa um banco **separado** (`sgotify_test`), lido da variável de ambiente `TEST_DB_URL`;
  - `TEST_DB_URL` **não** é lida do `.env` (de propósito: o `.env` do dia a dia nunca aponta para um teste que apaga linhas);
  - se estiver vazia, os testes de banco são **pulados** (`t.Skip`) e `go test ./...` continua passando;
  - o banco real (`DB_URL`) nunca é usado em teste.

```bash
# bash
TEST_DB_URL='postgres://USUARIO:SENHA@localhost:5432/sgotify_test?sslmode=disable' go test ./...
```
```powershell
# PowerShell
$env:TEST_DB_URL = "postgres://USUARIO:SENHA@localhost:5432/sgotify_test?sslmode=disable"
go test ./...
```

**Como saber se um teste presta:** uma boa prática de estudo é a **mutação**: quebrar o código de propósito (ex.: apagar a checagem de
`id` vazio, ou deixar de proteger uma rota) e conferir que algum teste **falha**. Os testes de autenticação foram verificados assim.

Não há como automatizar o **login real** no Spotify (precisa do seu browser e do seu Client ID): ele só é exercitado manualmente, em
`/login`. A parte do `/callback` é testada com um Spotify falso.

---

## 13. Decisões e limites conhecidos

| Tema | Decisão / limite |
|---|---|
| Uma conta só | O app é pessoal: `ALLOWED_SPOTIFY_USER_ID` define o único dono; o token tem uma linha |
| Sem senha própria | A identidade é o login do Spotify; não há tela de usuário e senha |
| Sem `/logout` | Não existe rota para encerrar uma sessão. Ela expira em 7 dias; trocar o `SESSION_SECRET` desloga tudo |
| Cookie sem `Secure` | O servidor local fala HTTP. **Atrás de HTTPS, ligue `Secure`** (em `session` e no login). Com front e API em domínios **diferentes**, o cookie precisaria de `SameSite=None; Secure` |
| `/login` aberto | Qualquer um que alcance o servidor pode iniciar o fluxo, mas uma conta de fora é recusada (`403`) e **não** sobrescreve o token |
| Rede | Não exponha o servidor na internet sem HTTPS e sem revisar esta lista |
| Token em texto no banco | Aceitável para uso pessoal com banco local; criptografar é trabalho futuro |
| Coletor | Só coleta com o servidor ligado; o calendário só tem dados a partir da primeira coleta |
| Migrações | `CREATE TABLE IF NOT EXISTS`; uma ferramenta de migração só valeria se o schema começasse a mudar |
| Race detector | `go test -race` precisa de `cgo` (um compilador C); não roda em todas as máquinas |
| Scope do `/me` | Se o `/v1/me` responder `403`, acrescentar `user-read-private` aos scopes |
