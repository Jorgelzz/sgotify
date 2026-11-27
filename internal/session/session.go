// ORDEM DE CRIAÇÃO: passo 2 da autenticação das rotas (sessão em cookie assinado)
//
// POR QUE EXISTE:
//
//	Depois que o /callback confirmar que quem logou é a SUA conta Spotify, o
//	browser precisa "provar" isso nas próximas chamadas a /api/*. Fazemos isso
//	com um cookie de sessão. Este pacote emite esse cookie e confere se um
//	cookie recebido é legítimo.
//
// COMO FUNCIONA (sem tabela, sem estado no servidor):
//
//	O cookie vale   <validade>.<assinatura>
//	  validade:   instante de expiração, em segundos Unix (ex.: 1796000000)
//	  assinatura: HMAC-SHA256 da validade, calculado com o SESSION_SECRET,
//	              em base64url (base64.RawURLEncoding, como no auth.go)
//
//	Só quem conhece o segredo consegue gerar uma assinatura que bate. Então
//	ninguém forja um cookie, nem estica a validade de um cookie verdadeiro
//	(mudar o número invalida a assinatura).
//
// LIMITES CONSCIENTES (documentados no guide-structure.md, seção 13):
//   - não há como encerrar UMA sessão antes do prazo; a saída é trocar o
//     SESSION_SECRET (invalida todas). Não criamos rota /logout: o escopo
//     fixa as rotas do projeto.
//   - o cookie vai sem a flag Secure, porque o servidor local fala HTTP.
//     Atrás de HTTPS, ligue.
//
// IMPORTS que você vai precisar (adicione conforme usar):
//
//	crypto/hmac, crypto/sha256, encoding/base64, errors, fmt, net/http,
//	strconv, strings, time
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CookieName é o nome do cookie de sessão.
const CookieName = "sgotify_session"

// minSecretLen é o tamanho mínimo do segredo, em BYTES. Um segredo curto pode
// ser adivinhado por força bruta e permitiria forjar cookies.
const minSecretLen = 32

// Manager emite e valida cookies de sessão.
type Manager struct {
	secret []byte        // chave do HMAC (vem de SESSION_SECRET)
	ttl    time.Duration // quanto tempo uma sessão dura
	// now existe para os testes fixarem o relógio (como o calendarHandler faz).
	// Em produção é time.Now.
	now func() time.Time
}

// New cria um Manager.
//
// PASSO 2A:
//  1. Se len(secret) < minSecretLen, devolva erro. (Mensagem clara, SEM
//     imprimir o segredo.)
//  2. Se ttl <= 0, devolva erro (uma sessão que nunca vale não faz sentido).
//  3. Devolva &Manager{secret: []byte(secret), ttl: ttl, now: time.Now}.
func New(secret string, ttl time.Duration) (*Manager, error) {

	if len(secret) < minSecretLen {
		return nil, fmt.Errorf("secret length must be at least %d bytes", minSecretLen)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("ttl must be positive")
	}
	return &Manager{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

// sign devolve o valor do cookie para uma sessão que expira em "expires".
//
// PASSO 2B (função PURA: só strings, sem HTTP — por isso é fácil de testar):
//  1. payload := strconv.FormatInt(expires.Unix(), 10)
//  2. mac := hmac.New(sha256.New, m.secret); mac.Write([]byte(payload))
//  3. sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
//  4. devolva payload + "." + sig
//
// IMPORTANTE: a validade TEM que entrar no que é assinado (o payload). Se só
// o texto fixo fosse assinado, daria para trocar a data sem quebrar nada.
func (m *Manager) sign(expires time.Time) string {
	payload := strconv.FormatInt(expires.Unix(), 10)
	sig := base64.RawURLEncoding.EncodeToString(m.sum(payload))
	return payload + "." + sig
}

// verify diz se "value" é um cookie AUTÊNTICO e AINDA NÃO VENCIDO.
//
// PASSO 2C (também pura). Qualquer entrada ruim devolve false, NUNCA pânico:
//  1. strings.Cut(value, ".") -> payload e sig; se não houver ".", false.
//  2. Recalcule a assinatura esperada do payload (a mesma conta do sign).
//  3. Compare com hmac.Equal (tempo constante), NUNCA com ==. O == para no
//     primeiro byte diferente, e o tempo de resposta vaza informação.
//     Dica: decodifique o sig recebido com base64.RawURLEncoding.DecodeString
//     (erro de decodificação -> false) e compare os BYTES com hmac.Equal.
//  4. SÓ DEPOIS de a assinatura bater: strconv.ParseInt(payload, 10, 64)
//     (erro -> false) e confira que m.now() é ANTES da validade.
//     Cookie autêntico mas vencido é inválido.

func (m *Manager) sum(payload string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (m *Manager) verify(value string) bool {
	payload, sig, ok := strings.Cut(value, ".")
	if !ok || payload == "" || sig == "" {
		return false
	}

	got, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil {
		return false
	}
	if !hmac.Equal(got, m.sum(payload)) {
		return false
	}
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	return m.now().Before(time.Unix(exp, 0))
}

// Issue grava o cookie de sessão na resposta (chamado no /callback, depois de
// confirmar a conta).
//
// PASSO 2D:
//
//	expires := m.now().Add(m.ttl)
//	http.SetCookie(w, &http.Cookie{
//	    Name:     CookieName,
//	    Value:    m.sign(expires),
//	    Path:     "/",                       // vale para /api/*
//	    Expires:  expires,
//	    MaxAge:   int(m.ttl.Seconds()),
//	    HttpOnly: true,                      // o JavaScript da página não lê
//	    SameSite: http.SameSiteLaxMode,
//	    // Secure: ligar atrás de HTTPS (o servidor local fala HTTP).
//	})
func (m *Manager) Issue(w http.ResponseWriter) {
	expires := m.now().Add(m.ttl)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    m.sign(expires),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(m.ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Valid diz se a requisição traz uma sessão legítima (usado pelo middleware
// que protege /api/*).
//
// PASSO 2E:
//
//	c, err := r.Cookie(CookieName)
//	err != nil (sem cookie) -> false
//	senão -> m.verify(c.Value)
func (m *Manager) Valid(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	return m.verify(c.Value)
}
