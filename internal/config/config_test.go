package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// keys são todas as variáveis que o Load lê.
var keys = []string{
	"SPOTIFY_CLIENT_ID", "SPOTIFY_REDIRECT_URI", "PORT", "FRONTEND_URL",
	"CORS_ALLOWED_ORIGIN", "TIMEZONE", "COLLECT_INTERVAL_MINUTES", "DB_URL",
	"ALLOWED_SPOTIFY_USER_ID", "SESSION_SECRET",
}

// requiredEnv são as variáveis OBRIGATÓRIAS e um valor válido para cada uma.
var requiredEnv = map[string]string{
	"SPOTIFY_CLIENT_ID":       "abc",
	"DB_URL":                  "postgres://user:pass@localhost:5432/db?sslmode=disable",
	"ALLOWED_SPOTIFY_USER_ID": "me",
	"SESSION_SECRET":          "0123456789abcdef0123456789abcdef",
}

// validEnv define todas as variáveis OBRIGATÓRIAS, para os testes que não
// estão testando a obrigatoriedade delas.
func validEnv(t *testing.T) {
	t.Helper()
	for k, v := range requiredEnv {
		t.Setenv(k, v)
	}
}

// isolate deixa o teste independente da sua máquina:
//   - zera as variáveis (t.Setenv("X", "") faz o getEnv usar o valor padrão e
//     restaura o valor original quando o teste termina);
//   - entra numa pasta vazia, para o Load não ler o seu .env de verdade.
func isolate(t *testing.T) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
}

func TestLoadDefaults(t *testing.T) {
	isolate(t)
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.CollectInterval != 20*time.Minute {
		t.Errorf("CollectInterval = %v, want 20m", cfg.CollectInterval)
	}
	if cfg.Timezone.String() != "America/Sao_Paulo" {
		t.Errorf("Timezone = %v, want America/Sao_Paulo", cfg.Timezone)
	}
}

// Cada variável obrigatória, SOZINHA ausente, faz o Load falhar, e o erro cita
// o nome dela (para quem configura saber o que falta).
func TestLoadMissingRequired(t *testing.T) {
	for missing := range requiredEnv {
		t.Run(missing, func(t *testing.T) {
			isolate(t)
			for k, v := range requiredEnv {
				if k != missing {
					t.Setenv(k, v)
				}
			}

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error when %s is missing", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error %q does not name %s", err, missing)
			}
		})
	}
}

// Auth: os valores chegam à struct.
func TestLoadAuthSettings(t *testing.T) {
	isolate(t)
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AllowedSpotifyUserID != "me" {
		t.Errorf("AllowedSpotifyUserID = %q, want me", cfg.AllowedSpotifyUserID)
	}
	if cfg.SessionSecret != requiredEnv["SESSION_SECRET"] {
		t.Error("SessionSecret was not loaded")
	}
}

func TestLoadCollectInterval(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr bool
	}{
		{"valid", "5", 5 * time.Minute, false},
		{"not a number", "abc", 0, true},
		{"zero", "0", 0, true},
		{"negative", "-5", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			validEnv(t)
			t.Setenv("COLLECT_INTERVAL_MINUTES", tt.value)

			cfg, err := Load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && cfg.CollectInterval != tt.want {
				t.Errorf("CollectInterval = %v, want %v", cfg.CollectInterval, tt.want)
			}
		})
	}
}

func TestLoadInvalidTimezone(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("TIMEZONE", "Mars/Olympus_Mons")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid TIMEZONE")
	}
}

func TestDotEnv(t *testing.T) {
	isolate(t)
	// As chaves precisam estar realmente AUSENTES (não vazias) para o .env
	// poder defini-las. Unsetenv depois do t.Setenv: ao fim do teste, o
	// t.Setenv restaura o estado original.
	os.Unsetenv("SPOTIFY_CLIENT_ID")
	os.Unsetenv("PORT")
	os.Unsetenv("DB_URL")
	os.Unsetenv("ALLOWED_SPOTIFY_USER_ID")
	os.Unsetenv("SESSION_SECRET")
	t.Setenv("PORT", "2222") // variável real: deve ganhar do .env

	content := "# comentário\n\nSPOTIFY_CLIENT_ID = fromfile\nPORT=1111\nDB_URL=postgres://file@localhost/db\n" +
		"ALLOWED_SPOTIFY_USER_ID=fromfile-user\nSESSION_SECRET=0123456789abcdef0123456789abcdef\n"
	if err := os.WriteFile(filepath.Join(".", ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Unsetenv("SPOTIFY_CLIENT_ID")
		os.Unsetenv("DB_URL")
		os.Unsetenv("ALLOWED_SPOTIFY_USER_ID")
		os.Unsetenv("SESSION_SECRET")
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientID != "fromfile" {
		t.Errorf("ClientID = %q, want fromfile (read from .env, spaces trimmed)", cfg.ClientID)
	}
	if cfg.DBURL != "postgres://file@localhost/db" {
		t.Errorf("DBURL = %q, want the value from .env", cfg.DBURL)
	}
	if cfg.Port != "2222" {
		t.Errorf("Port = %q, want 2222 (real env var must beat .env)", cfg.Port)
	}
	if cfg.AllowedSpotifyUserID != "fromfile-user" {
		t.Errorf("AllowedSpotifyUserID = %q, want the value from .env", cfg.AllowedSpotifyUserID)
	}
}

func TestLoadWithoutDotEnvFile(t *testing.T) {
	isolate(t)
	validEnv(t)

	// isolate já entrou numa pasta sem .env: não pode dar erro.
	if _, err := Load(); err != nil {
		t.Fatalf("missing .env must not be an error, got: %v", err)
	}
}
