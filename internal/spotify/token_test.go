package spotify

import (
	"testing"
	"time"
)

// TestTokenExpired usa uma tabela: cada linha é um caso, e o loop roda todos.
// O caso de 31s é o que prova que a folga é de 30s exatos (nem mais, nem menos).
func TestTokenExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresIn time.Duration // a partir de agora; negativo = já expirou
		want      bool
	}{
		{"expires in 1h", time.Hour, false},
		{"expired 1 minute ago", -time.Minute, true},
		{"expires in 10s (inside the 30s margin)", 10 * time.Second, true},
		{"expires in 31s (outside the margin)", 31 * time.Second, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := Token{ExpiresAt: time.Now().Add(tt.expiresIn)}

			if got := tok.Expired(); got != tt.want {
				t.Errorf("Expired() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Um Token zerado (usuário que nunca fez login) conta como "precisa renovar".
func TestTokenZeroValueIsExpired(t *testing.T) {
	if !(Token{}).Expired() {
		t.Error("zero Token must be expired")
	}
}

func TestNewToken(t *testing.T) {
	// time.Now() muda a cada chamada, então comparar ExpiresAt com "==" nunca
	// bate. Marcamos o relógio antes e depois e exigimos cair no intervalo.
	before := time.Now()
	tok := newToken("access", "refresh", 3600)
	after := time.Now()

	if tok.AccessToken != "access" {
		t.Errorf("AccessToken = %q, want access", tok.AccessToken)
	}
	if tok.RefreshToken != "refresh" {
		t.Errorf("RefreshToken = %q, want refresh", tok.RefreshToken)
	}

	earliest := before.Add(time.Hour)
	latest := after.Add(time.Hour)
	if tok.ExpiresAt.Before(earliest) || tok.ExpiresAt.After(latest) {
		t.Errorf("ExpiresAt = %v, want between %v and %v", tok.ExpiresAt, earliest, latest)
	}
}
