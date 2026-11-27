// ORDEM DE CRIAÇÃO: 2º (Etapa 2 — configuração) · junto com .env.example
//
// POR QUE EXISTE:
//
//	Todo o resto do programa precisa de valores externos (Client ID, porta,
//	fuso, intervalo do coletor, origem do front). Em vez de cada pacote chamar
//	os.Getenv por conta própria, este pacote lê TUDO uma vez, valida e entrega
//	uma struct. Por isso vem logo no início: é a única coisa da qual os outros
//	pacotes dependem e que não depende de nada.
//
// O QUE FAZ:
//   - struct Config com: ClientID, RedirectURI, Port, FrontendURL,
//     CORSAllowedOrigin, Timezone (*time.Location), CollectInterval
//     (time.Duration), DBURL (variável DB_URL: obrigatória, como o Client ID;
//     substitui o antigo DataDir, já que token e histórico agora ficam no
//     PostgreSQL e não em arquivos).
//   - func Load() (Config, error): lê o arquivo .env (se existir) e as
//     variáveis de ambiente; devolve erro claro se faltar SPOTIFY_CLIENT_ID.
//
// OBRIGATÓRIAS (o programa não sobe sem elas): SPOTIFY_CLIENT_ID, DB_URL,
// ALLOWED_SPOTIFY_USER_ID e SESSION_SECRET.
//
// DECISÃO: não usar biblioteca para ler .env no começo — são ~15 linhas com
// bufio.Scanner e dá para entender o que acontece.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	// Embute o banco de fusos horários (IANA) dentro do binário. O Windows não
	// traz esse banco, e sem ele time.LoadLocation("America/Sao_Paulo") falharia
	// em qualquer máquina que não tenha o Go instalado. Custa ~450 KB.
	_ "time/tzdata"
)

type Config struct {
	ClientID          string
	RedirectURI       string
	Port              string
	FrontendURL       string
	CORSAllowedOrigin string
	Timezone          *time.Location
	CollectInterval   time.Duration
	// DBURL é a string de conexão do PostgreSQL (variável DB_URL). Guarda o
	// token do Spotify e o histórico de plays. Substituiu o antigo DataDir.
	DBURL string
	// AllowedSpotifyUserID é o id da ÚNICA conta Spotify que pode usar a API
	// (variável ALLOWED_SPOTIFY_USER_ID). O /callback compara com ele o dono do
	// token: outra conta é recusada e não sobrescreve o token guardado.
	AllowedSpotifyUserID string
	// SessionSecret é a chave que assina o cookie de sessão (SESSION_SECRET).
	// O tamanho mínimo é validado em session.New, que é quem usa a chave.
	SessionSecret string
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}

	return scanner.Err()
}

func getEnv(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

func Load() (Config, error) {

	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}

	cfg := Config{
		ClientID:          getEnv("SPOTIFY_CLIENT_ID", ""),
		RedirectURI:       getEnv("SPOTIFY_REDIRECT_URI", "http://127.0.0.1:8080/callback"),
		Port:              getEnv("PORT", "8080"),
		FrontendURL:       getEnv("FRONTEND_URL", "http://localhost:3000"),
		CORSAllowedOrigin: getEnv("CORS_ALLOWED_ORIGIN", "http://localhost:3000"),
		DBURL:             getEnv("DB_URL", ""),

		AllowedSpotifyUserID: getEnv("ALLOWED_SPOTIFY_USER_ID", ""),
		SessionSecret:        getEnv("SESSION_SECRET", ""),
	}

	if cfg.ClientID == "" {
		return Config{}, errors.New("SPOTIFY_CLIENT_ID is required")
	}
	// Sem banco não há onde guardar o token nem o histórico: falhar já na
	// inicialização, com mensagem clara, é melhor que cair no meio do login.
	if cfg.DBURL == "" {
		return Config{}, errors.New("DB_URL is required (see .env.example)")
	}

	// Sem estas duas a autenticação das rotas não existe: falhar aqui, na
	// inicialização, é melhor que subir um servidor aberto "por engano". Um
	// ALLOWED_SPOTIFY_USER_ID vazio, em especial, nunca pode virar "qualquer
	// um entra".
	if cfg.AllowedSpotifyUserID == "" {
		return Config{}, errors.New("ALLOWED_SPOTIFY_USER_ID is required (see .env.example)")
	}
	if cfg.SessionSecret == "" {
		return Config{}, errors.New("SESSION_SECRET is required (see .env.example)")
	}

	// O .env só tem texto: "20" vira 20 minutos (time.Duration).
	minutes, err := strconv.Atoi(getEnv("COLLECT_INTERVAL_MINUTES", "20"))
	if err != nil {
		return Config{}, fmt.Errorf("COLLECT_INTERVAL_MINUTES is invalid: %w", err)
	}
	// time.NewTicker (usado pelo coletor) entra em pânico com intervalo <= 0,
	// então recusamos aqui, na inicialização, com uma mensagem clara.
	if minutes <= 0 {
		return Config{}, fmt.Errorf("COLLECT_INTERVAL_MINUTES must be greater than 0, got %d", minutes)
	}
	cfg.CollectInterval = time.Duration(minutes) * time.Minute

	// "America/Sao_Paulo" vira *time.Location, usado para decidir em que DIA
	// uma música foi ouvida (o played_at do Spotify vem em UTC).
	loc, err := time.LoadLocation(getEnv("TIMEZONE", "America/Sao_Paulo"))
	if err != nil {
		return Config{}, fmt.Errorf("TIMEZONE is invalid: %w", err)
	}
	cfg.Timezone = loc

	return cfg, nil
}
