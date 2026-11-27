// ORDEM DE CRIAÇÃO: 0º (já existia) · reescrito na Etapa 11 (montagem final)
//
// POR QUE EXISTE:
//
//	Ponto de entrada do programa. Fica PEQUENO de propósito: só "monta as
//	peças e liga". Toda a lógica mora em internal/.
//
// ORDEM DA MONTAGEM (de baixo para cima nas camadas):
//
//	config -> Postgres (Open + Migrate) -> stores -> cliente Spotify ->
//	services -> sessão -> handlers -> server; e o coletor numa goroutine ao lado.
//
// ENCERRAMENTO LIMPO: Ctrl+C (ou SIGTERM) cancela um context; o coletor para,
// o servidor termina as requisições em andamento e o banco é fechado.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jorgelzz/sgotify/internal/collector"
	"github.com/jorgelzz/sgotify/internal/config"
	"github.com/jorgelzz/sgotify/internal/handlers"
	"github.com/jorgelzz/sgotify/internal/server"
	"github.com/jorgelzz/sgotify/internal/service"
	"github.com/jorgelzz/sgotify/internal/session"
	"github.com/jorgelzz/sgotify/internal/spotify"
	"github.com/jorgelzz/sgotify/internal/storage"
)

// shutdownTimeout é quanto tempo damos às requisições em andamento para
// terminarem depois do Ctrl+C.
const shutdownTimeout = 10 * time.Second

// sessionTTL é quanto tempo o login vale no browser. Passado esse prazo, o
// front recebe 401 e manda o usuário de volta ao /login (um clique, porque o
// Spotify já está logado).
const sessionTTL = 7 * 24 * time.Hour

func main() {
	// run existe para o main poder devolver um erro e sair com código 1.
	// (log.Fatal dentro do main pularia os "defer", como o db.Close.)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// ctx é cancelado quando o usuário aperta Ctrl+C ou o sistema manda SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Banco: abre o pool e cria as tabelas que faltarem (idempotente) ---
	db, err := storage.Open(ctx, cfg.DBURL)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()
	if err := storage.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}
	tokens := storage.NewTokenStore(db)
	plays := storage.NewPlayStore(db)

	// --- Spotify e services ---
	auth := spotify.NewAuth(cfg.ClientID, cfg.RedirectURI)
	client := spotify.NewClient(auth, tokens)

	nowPlaying := service.NewNowPlayingService(client)
	top := service.NewTopService(plays)
	calendar := service.NewCalendarService(plays, cfg.Timezone)

	// --- Sessão: o cookie assinado que libera /api/* depois do login ---
	// session.New recusa um SESSION_SECRET curto (< 32 bytes): falha já aqui,
	// na inicialização, e a mensagem não repete o segredo.
	sessions, err := session.New(cfg.SessionSecret, sessionTTL)
	if err != nil {
		return fmt.Errorf("SESSION_SECRET: %w", err)
	}

	// --- HTTP ---
	handler := server.New(server.Routes{
		// O client faz o papel de "quem é o dono deste token?" (UserID) no
		// /callback; só a conta ALLOWED_SPOTIFY_USER_ID ganha a sessão.
		Auth:       handlers.NewAuthHandler(auth, client, tokens, sessions, cfg.AllowedSpotifyUserID, cfg.FrontendURL),
		Sessions:   sessions,
		NowPlaying: handlers.NowPlaying(nowPlaying),
		TopWeek:    handlers.TopWeek(top),
		Calendar:   handlers.Calendar(calendar, cfg.Timezone),
	}, cfg.CORSAllowedOrigin)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: handler,
		// Sem isso, um cliente lento poderia manter a conexão aberta para sempre.
		ReadHeaderTimeout: 5 * time.Second,
	}

	// --- Coletor: copia o histórico do Spotify para o Postgres em segundo plano ---
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		collector.New(client, plays, cfg.CollectInterval).Run(ctx)
	}()

	// --- Shutdown: quando ctx for cancelado, desliga o servidor com calma ---
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		log.Println("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("sgotify listening on :%s (login: http://127.0.0.1:%s/login)", cfg.Port, cfg.Port)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil // desligamento pedido por nós: não é erro
	}

	// Se o servidor caiu por outro motivo (porta ocupada, por exemplo), o ctx
	// ainda está ativo: cancelamos para parar o coletor e a goroutine de shutdown.
	stop()
	<-shutdownDone
	wg.Wait() // espera o coletor terminar a coleta em andamento antes de fechar o banco
	return err
}
