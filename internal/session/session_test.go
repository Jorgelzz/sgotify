// Testes do pacote session. Os testes estão como esqueleto: cada um começa com
// t.Skip("TODO"). Apague o Skip ao escrever o teste.
//
// DICAS GERAIS:
//   - Estamos no MESMO pacote (package session), então o teste enxerga
//     m.now, m.sign, m.verify e m.secret (campos e funções privadas).
//   - Para fixar o relógio: m.now = func() time.Time { return fixo }.
//   - Para ler o cookie que Issue gravou:
//     rec := httptest.NewRecorder(); m.Issue(rec)
//     cookies := rec.Result().Cookies()   // []*http.Cookie
//   - Para simular a requisição com o cookie:
//     req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
//     req.AddCookie(cookies[0])
package session

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// secretOK é um segredo com 32+ bytes, para os testes.
const secretOK = "0123456789abcdef0123456789abcdef"

// Teste 1 — ida e volta: um valor assinado agora é válido agora.
//
//	m, err := New(secretOK, time.Hour) ; relógio fixo em T
//	v := m.sign(T.Add(time.Hour))      ; m.verify(v) == true
func TestSignVerifyRoundTrip(t *testing.T) {
	m, err := New(secretOK, time.Hour)
	if err != nil {
		t.Fatalf("New() = %v, expected nil", err)
	}
	v := m.sign(time.Now().UTC().Add(time.Hour))
	if !m.verify(v) {
		t.Errorf("verify(%q) = false, expected true", v)
	}
}

// Teste 2 — vencido: com o relógio ADIANTADO além da validade, verify == false.
//
//	valor assinado com validade T+1h; mude m.now para T+2h; verify == false.
//	Confira também a FRONTEIRA: exatamente na validade já está vencido
//	(m.now() deve ser ANTES da validade).
func TestVerifyExpired(t *testing.T) {
	m, err := New(secretOK, time.Hour)
	if err != nil {
		t.Fatalf("New() = %v, expected nil", err)
	}
	fixed := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	expires := fixed.Add(1 * time.Hour)
	v := m.sign(expires)

	// Fronteira exata: exatamente na validade já deve estar vencido.
	m.now = func() time.Time { return expires }
	if m.verify(v) {
		t.Errorf("verify(%q) = true, expected false", v)
	}
	// Além da validade
	m.now = func() time.Time { return expires.Add(2 * time.Hour) }
	if m.verify(v) {
		t.Errorf("verify(%q) = true, expected false", v)
	}
}

// Teste 3 (o mais importante) — validade adulterada: troque o número e
// mantenha a assinatura. verify == false.
//
//	v := m.sign(T.Add(time.Hour))      // "1796003600.AAAA..."
//	payload, sig, _ := strings.Cut(v, ".")
//	monte um valor com OUTRA validade (mais longa) + a MESMA sig.
//	verify == false. Se isto passar, qualquer um estica a própria sessão.
func TestVerifyTamperedExpiry(t *testing.T) {
	m, err := New(secretOK, time.Hour)
	if err != nil {
		t.Fatalf("New() =%v, expected nil", err)
	}
	fixed := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	v := m.sign(fixed.Add(time.Hour))

	_, sig, _ := strings.Cut(v, ".")

	m.now = func() time.Time { return fixed }

	extendedPayload := strconv.FormatInt(fixed.Add(24*time.Hour).Unix(), 10)
	if m.verify(extendedPayload + "." + sig) {
		t.Fatalf("verify() accepted tampered expiry, expected false")
	}
}

// Teste 4 — assinatura adulterada, ou feita com OUTRO segredo: false.
//
//	a) troque o último caractere da assinatura;
//	b) assine com m2 (outro segredo) e verifique com m.
func TestVerifyTamperedSignature(t *testing.T) {
	m, err := New(secretOK, time.Hour)
	if err != nil {
		t.Fatalf("New() = %v, expected nil", err)
	}
	fixed := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }

	v := m.sign(fixed.Add(time.Hour))

	payload, sig, _ := strings.Cut(v, ".")

	// a) troque o último caractere da assinatura;
	replacement := "A"
	if sig[len(sig)-1] == 'A' {
		replacement = "Z"
	}
	tampered := payload + "." + sig[:len(sig)-1] + replacement
	if m.verify(tampered) {
		t.Errorf("verify(%q) = true, expected false", tampered)
	}

	// b) assine com m2 (outro segredo) e verifique com m.
	otherSecret := "fedcba9876543210fedcba9876543210"
	m2, err := New(otherSecret, time.Hour)
	if err != nil {
		t.Fatalf("New() = %v, expected nil", err)
	}
	v2 := m2.sign(fixed.Add(time.Hour))
	if m.verify(v2) {
		t.Errorf("verify() accepted cookie signed with another secret")
	}

}

// Teste 5 — lixo: nunca pânico, sempre false. Use TABELA.
//
//	"", "abc", "123.", ".abc", "x.y", "123.não-é-base64!!", "99999999999999999999.AAAA"
func TestVerifyGarbage(t *testing.T) {
	m, err := New(secretOK, time.Hour)
	if err != nil {
		t.Fatalf("New() = %v, expected nil", err)
	}

	garbage := []string{"", "abc", "123.", ".abc", "x.y", "123.não-é-base64!!", "99999999999999999999.AAAA"}

	for _, v := range garbage {
		if m.verify(v) {
			t.Errorf("verify(%q) = true, expected false", v)
		}
	}
}

// Teste 6 — Valid sem cookie na requisição: false.
func TestValidWithoutCookie(t *testing.T) {
	m, err := New(secretOK, time.Hour)
	if err != nil {
		t.Fatalf("New() = %v, expected nil", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "/api/x", nil)
	if m.Valid(req) {
		t.Errorf("Valid() = true, expected false")
	}
}

// Teste 7 — Issue + Valid juntos, pelo caminho HTTP real:
//
//	m.Issue(rec) -> pegue o cookie de rec.Result().Cookies() -> req.AddCookie
//	-> m.Valid(req) == true.
//
// E os ATRIBUTOS do cookie emitido: Name == CookieName, HttpOnly,
// SameSite == http.SameSiteLaxMode, Path == "/", MaxAge > 0.
func TestIssueThenValid(t *testing.T) {
	m, err := New(secretOK, time.Hour)
	if err != nil {
		t.Fatalf("New() = %v, expected nil", err)
	}

	rec := httptest.NewRecorder()
	m.Issue(rec)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Errorf("Expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != CookieName {
		t.Fatalf("Expected cookie name %q, got %q", CookieName, cookie.Name)
	}

	if !cookie.HttpOnly {
		t.Errorf("Expected cookie to be HttpOnly")
	}

	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("Expected cookie SameSite %v, got %v", http.SameSiteLaxMode, cookie.SameSite)
	}

	if cookie.Path != "/" {
		t.Errorf("Expected cookie path %q, got %q", "/", cookie.Path)
	}

	if cookie.MaxAge <= 0 {
		t.Errorf("Expected cookie MaxAge > 0, got %d", cookie.MaxAge)
	}

	req, _ := http.NewRequest(http.MethodGet, "/api/x", nil)
	req.AddCookie(cookie)

	if !m.Valid(req) {
		t.Errorf("Valid() = false, expected true")
	}
}

// Teste 8 — New recusa configuração fraca. Use TABELA:
//
//	segredo curto (31 bytes)    -> erro
//	segredo vazio               -> erro
//	ttl zero                    -> erro
//	ttl negativo                -> erro
//	segredo de 32 bytes + ttl 1h -> sem erro
func TestNewRejectsWeakConfig(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		ttl     time.Duration
		wantErr bool
	}{
		{"segredo curto (31 bytes)", strings.Repeat("a", 31), time.Hour, true},
		{"segredo vazio", "", time.Hour, true},
		{"ttl zero", secretOK, 0, true},
		{"ttl negativo", secretOK, -time.Minute, true},
		{"segredo de 32 bytes + ttl 1h", secretOK, time.Hour, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := New(tt.secret, tt.ttl)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New() err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr && m != nil {
				t.Errorf("expected nil manager on error")
			}
			if !tt.wantErr && m == nil {
				t.Errorf("expected non-nil manager")
			}
			if err != nil && tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
				t.Errorf("error message leaked the secret")
			}
		})
	}
}
