# sgotify - SISTEMA PARA ESTUDOS

**API backend em Go** que conversa com o Spotify e expõe rotas JSON para um front-end (que fica em outro projeto):

1. **O que estou ouvindo agora.**
2. **Meus mais ouvidos da semana.**
3. **Calendário:** para cada dia do mês, a música mais ouvida naquele dia (com a capa).

> Este é um **projeto de estudo**: o objetivo é aprender Go construindo uma API de verdade, **entendendo cada peça**,
> e não entregar um produto. Por isso o código é cheio de comentários, cada arquivo começa explicando **por que existe**
> e **em que ordem foi criado**, e quase tudo tem teste.
>
> Aqui está o **escopo** (o que é e o que não é este projeto) e como rodar.
> O **como funciona por dentro** (arquitetura, fluxos, estrutura de pastas, decisões) está em
> **[guide-structure.md](guide-structure.md)**.

---

## 1. Por que este projeto existe

- **Aprender Go** em um caso real: `net/http`, interfaces, `database/sql`, goroutines, testes, tratamento de erros.
- **Aprender a integrar com uma API externa**: OAuth 2.0 com PKCE, renovação de token, limites da API do Spotify.
- **Aprender a organizar um backend em camadas** (`handlers → service → spotify/storage`) sem frameworks.

Regras de estudo que seguimos:

- Só **biblioteca padrão** do Go, com **uma** exceção: o driver `pgx` para falar com o PostgreSQL.
- Cada arquivo tem, no topo, a ordem de criação e o motivo de existir. Cada função tem comentário.
- Testes ficam ao lado do código (`_test.go`) e rodam **sem internet e sem login** (com falsos), exceto os de banco, que são opcionais.

---

## 2. Escopo

### Dentro do escopo

| O quê | Resumo |
|---|---|
| Música atual | Mostra o que está tocando agora (ou que nada toca) |
| Top da semana | Ranking dos últimos 7 dias, calculado no **nosso** histórico |
| Calendário | Para cada dia do mês, a música mais ouvida e a capa |
| Coletor | Copia o histórico do Spotify para o PostgreSQL, em segundo plano |
| Login com Spotify | Autoriza o app a ler os seus dados **e** é a identidade que libera a API |
| Proteção das rotas `/api/*` | Só a **sua** conta Spotify entra (sessão em cookie assinado) |
| CORS | Só a origem do seu front-end pode ler as respostas |

### Fora do escopo

- **Front-end** (outro projeto).
- **Vários usuários.** É um app pessoal: uma conta Spotify, uma linha de token.
- **Usuário e senha próprios.** A identidade vem do login do Spotify.
- **Encerrar uma sessão individualmente** (`/logout`). A sessão expira sozinha em 7 dias.
- **Deploy público e HTTPS.** O servidor foi pensado para rodar na sua máquina.
- **Controlar o player** (pausar, pular).
- **Ferramenta de migração de schema.** `CREATE TABLE IF NOT EXISTS` basta para 2 tabelas.
- **Criptografar o token no banco** (uso pessoal, banco local).

### Limitações que você precisa saber

- O Spotify **não** guarda "o que ouvi em cada dia", só as últimas 50 faixas. Por isso existe o coletor: o calendário só tem dados **a partir do dia em que o servidor começou a coletar**.
- **O coletor só funciona com o servidor ligado.** Se ficar desligado e você ouvir mais de 50 faixas, as mais antigas se perdem.

---

## 3. Rotas

Só existem estas 6 (não há `GET /`: a API não serve front-end).

| Rota | O que faz | Precisa de sessão? |
|---|---|---|
| `GET /health` | Servidor vivo | não |
| `GET /login` | Inicia o login com o Spotify | não (é como se consegue a sessão) |
| `GET /callback` | Conclui o login e leva de volta ao front-end | não (idem) |
| `GET /api/now-playing` | Música atual | **sim** |
| `GET /api/top-week?limit=10` | Top músicas dos últimos 7 dias | **sim** |
| `GET /api/calendar?month=2026-11` | Música mais ouvida de cada dia do mês | **sim** |

Sem sessão válida, as rotas `/api/*` respondem `401` com o caminho do login. Formatos de resposta, erros e regras
(como o "dia" é calculado, o que é empate) estão no [guide-structure.md](guide-structure.md#7-rotas-e-formatos).

---

## 4. Roteiro de estudo

O projeto foi construído em etapas. Cada uma tem **um conceito novo** e uma forma de saber se funcionou:

| Etapa | O que foi construído | O que se aprende |
|---|---|---|
| 0 | Servidor + `/health` | `net/http`, `ServeMux` |
| 1 | README | planejar antes de codar |
| 2 | `config` | variáveis de ambiente, segredos fora do código |
| 3 | `spotify/auth.go`, `token.go` | OAuth, PKCE, SHA-256, base64url |
| 4a | `storage` (banco + token) | `database/sql`, driver `pgx`, SQL, `ON CONFLICT` |
| 4b | `server`, `middleware`, `handlers/auth.go` | redirects, `state`, middleware, CORS |
| 5 | `spotify/client.go` | `http.Client`, refresh de token, mutex |
| 6 | `/api/now-playing` | decodificar JSON, tratar `204`, primeiro service |
| 7 | `/api/top-week` | query params, limitação do Spotify |
| 8 | `storage` (plays) | interfaces, transações, `ON CONFLICT DO NOTHING` |
| 9 | `history.go` + `collector` | goroutines, `time.Ticker`, idempotência |
| 10 | `/api/calendar` | agrupamento, fuso horário, testes |
| 11 | `main.go` final | injeção de dependências, shutdown limpo |
| 12 | Autenticação das rotas | HMAC, cookies, comparação em tempo constante, identidade via OAuth |

---

## 5. Como rodar

**Pré-requisitos:** Go, um PostgreSQL acessível, e um app criado no
[dashboard do Spotify](https://developer.spotify.com/dashboard).

**1. Spotify.** Crie um app, anote o **Client ID** e cadastre o *Redirect URI* exatamente como
`http://127.0.0.1:8080/callback` (o Spotify **não aceita** `localhost`, use `127.0.0.1`).

**2. Bancos.** Crie uma vez (no `psql` ou em uma ferramenta gráfica):

```sql
CREATE DATABASE sgotify;        -- app
CREATE DATABASE sgotify_test;   -- testes de integração (opcional)
```

**3. Configuração.** Copie o modelo e preencha. O `.env` já está no `.gitignore`.

```bash
cp .env.example .env
```

São **4 variáveis obrigatórias** (o programa não sobe sem elas):

| Variável | O que é |
|---|---|
| `SPOTIFY_CLIENT_ID` | Client ID do seu app no Spotify |
| `DB_URL` | Conexão do PostgreSQL (com a senha real) |
| `ALLOWED_SPOTIFY_USER_ID` | O id da **sua** conta Spotify: só ela entra |
| `SESSION_SECRET` | Segredo (≥ 32 caracteres) que assina a sessão. Gere com `openssl rand -base64 32` |

Para achar o seu id do Spotify: perfil → **Compartilhar → Copiar link**; o id é o trecho depois de `/user/`.
Atalho: deixe qualquer valor, faça o login e o servidor registra no log `login rejected: spotify user "<id>"` com o seu id.
A lista completa de variáveis está no [guide-structure.md](guide-structure.md#8-configuração).

**4. Subir e fazer o login uma vez.**

```bash
go run .
curl http://127.0.0.1:8080/health
# abra http://127.0.0.1:8080/login no browser e aprove no Spotify
```

Depois do login, o browser recebe o cookie de sessão e o front-end consome `/api/*`. O coletor faz a primeira coleta
na rodada seguinte (no máximo `COLLECT_INTERVAL_MINUTES` depois) e segue a cada intervalo. `Ctrl+C` encerra com calma.

**5. Testes.**

```bash
go test ./...
```

Os testes de banco (`storage`) são **de integração**: apagam linhas, então usam um banco **separado** e só rodam se a variável de
ambiente `TEST_DB_URL` existir (senão são pulados). Detalhes em [guide-structure.md](guide-structure.md#12-testes).

---

## 6. Segurança em uma olhada

- Os dados são **seus**, e o token do Spotify fica **só no backend**. O front-end nunca o vê.
- Só a conta `ALLOWED_SPOTIFY_USER_ID` recebe a sessão. Outra conta que tente o login é recusada e **não** sobrescreve o token.
- `SESSION_SECRET` e `DB_URL` são segredos: ficam no `.env` (ignorado pelo Git), nunca em arquivos versionados.
- O servidor local fala HTTP. **Não o exponha na internet** sem antes colocá-lo atrás de HTTPS e revisar o que está
  listado como limite em [guide-structure.md](guide-structure.md#13-decisões-e-limites-conhecidos).

---

## 7. Onde continuar

- **[guide-structure.md](guide-structure.md)**: arquitetura em camadas, fluxo de login e sessão, CORS, rotas e formatos,
  configuração, estrutura de pastas, modelo de dados, coletor, testes e limites conhecidos.
