// ORDEM DE CRIAÇÃO: 3º (Etapa 3 — autenticação) · junto com auth.go
//
// POR QUE EXISTE:
//
//	O access token dura ~1h, mas o coletor precisa funcionar por dias sem eu
//	fazer login de novo. Então o token (principalmente o refresh_token)
//	precisa SOBREVIVER a reinícios do servidor => persistir em disco.
//	Separado do auth.go porque "como obter token" (rede) e "onde guardar
//	token" (disco) são responsabilidades diferentes.
//
// O QUE VAI AQUI:
//   - Token, newToken e Expired  -> Etapa 3 (agora)
//   - Save / Load em disco       -> Etapa 4 (quando o handler precisar)
//
// CUIDADO: acesso concorrente (handler + coletor) => proteger com sync.Mutex
// quando chegar na Etapa 5.
package spotify

import "time"

// Token é o par de credenciais do usuário.
//
// As tags `json:"..."` definem o nome de cada campo quando a struct for
// convertida para JSON (ao salvar em disco, na Etapa 4).
//
// ExpiresAt é um HORÁRIO ABSOLUTO, não uma duração. O Spotify manda
// "expires_in: 3600" (segundos a partir de agora); se guardássemos só isso,
// depois não saberíamos mais quando o token expira.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// newToken monta um Token a partir do que o Spotify devolve.
//
// O que tratar:
//   - converter expiresIn (segundos) em ExpiresAt = agora + expiresIn segundos.
//     Dica: time.Now().Add(time.Duration(expiresIn) * time.Second).
//
// Quem usa: auth.go (requestToken). É uma função separada para ser testável
// sem rede.
//
// Teste: newToken("a", "r", 3600) deve ter ExpiresAt ~1h à frente de agora.
func newToken(accessToken, refreshToken string, expiresIn int) Token {
	return Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
	}
}

// Expired diz se o token já não deve ser usado.
//
// O que tratar:
//   - devolver true se faltam MENOS DE 30 SEGUNDOS para ExpiresAt (ou se já
//     passou). A folga evita usar um token que expira no meio da requisição.
//   - Dica: time.Now().Add(30 * time.Second).After(t.ExpiresAt)
//
// Teste (3 casos): expira em 1h -> false; expirou há 1 min -> true;
// expira em 10s -> true (dentro da folga).
func (t Token) Expired() bool {
	return time.Now().Add(30 * time.Second).After(t.ExpiresAt)
}

// Etapa 4 (NÃO escreva agora): Save(path string, t Token) error e
// Load(path string) (Token, error) em data/token.json.
//   - Save: json.MarshalIndent + os.WriteFile com permissão 0o600 (credencial).
//     Crie a pasta antes com os.MkdirAll(filepath.Dir(path), 0o700).
//   - Load: se o arquivo não existir, devolva um erro que o chamador consiga
//     identificar (errors.Is(err, os.ErrNotExist)) => "usuário ainda não fez login".
