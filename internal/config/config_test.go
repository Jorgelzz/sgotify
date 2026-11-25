package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// keys são todas as variáveis que o Load lê.
var keys = []string{
	"SPOTIFY_CLIENT_ID", "SPOTIFY_REDIRECT_URI", "PORT", "FRONTEND_URL",
	"CORS_ALLOWED_ORIGIN", "TIMEZONE", "COLLECT_INTERVAL_MINUTES", "DATA_DIR",
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
	t.Setenv("SPOTIFY_CLIENT_ID", "abc")

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

func TestLoadMissingClientID(t *testing.T) {
	isolate(t)

	if _, err := Load(); err == nil {
		t.Fatal("expected error when SPOTIFY_CLIENT_ID is missing")
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
			t.Setenv("SPOTIFY_CLIENT_ID", "abc")
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
	t.Setenv("SPOTIFY_CLIENT_ID", "abc")
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
	os.Unsetenv("DATA_DIR")
	t.Setenv("PORT", "2222") // variável real: deve ganhar do .env

	content := "# comentário\n\nSPOTIFY_CLIENT_ID = fromfile\nPORT=1111\nDATA_DIR=meus-dados\n"
	if err := os.WriteFile(filepath.Join(".", ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Unsetenv("SPOTIFY_CLIENT_ID")
		os.Unsetenv("DATA_DIR")
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientID != "fromfile" {
		t.Errorf("ClientID = %q, want fromfile (read from .env, spaces trimmed)", cfg.ClientID)
	}
	if cfg.DataDir != "meus-dados" {
		t.Errorf("DataDir = %q, want meus-dados", cfg.DataDir)
	}
	if cfg.Port != "2222" {
		t.Errorf("Port = %q, want 2222 (real env var must beat .env)", cfg.Port)
	}
}

func TestLoadWithoutDotEnvFile(t *testing.T) {
	isolate(t)
	t.Setenv("SPOTIFY_CLIENT_ID", "abc")

	// isolate já entrou numa pasta sem .env: não pode dar erro.
	if _, err := Load(); err != nil {
		t.Fatalf("missing .env must not be an error, got: %v", err)
	}
}
