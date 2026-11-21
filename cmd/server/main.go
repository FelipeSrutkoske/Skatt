package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/api"
	"github.com/FelipeSrutkoske/Skatt/internal/db"
	"github.com/FelipeSrutkoske/Skatt/internal/parser"
	"github.com/FelipeSrutkoske/Skatt/internal/receipt"
	"github.com/FelipeSrutkoske/Skatt/internal/store"
	"github.com/FelipeSrutkoske/Skatt/internal/web"
)

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "./data"
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("falha ao criar diretorio de dados: %v", err)
	}

	dbPath := filepath.Join(dataDir, "finance.db")
	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("falha ao abrir banco de dados: %v", err)
	}

	st := store.New(conn)
	defer func() {
		if active := st.DB(); active != nil {
			_ = active.Close()
		}
	}()

	uploadsDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		log.Fatalf("falha ao criar diretorio de uploads: %v", err)
	}

	stagingDir := filepath.Join(dataDir, "staging")
	staging := parser.NewStagingManager(stagingDir)
	if err := staging.CleanupExpired(24 * time.Hour); err != nil {
		log.Printf("aviso ao limpar staging expirado: %v", err)
	}

	handler := api.NewServer(api.Dependencies{
		Store:      st,
		Staging:    staging,
		DataDir:    dataDir,
		UploadsDir: uploadsDir,
		Mailer:     receipt.NewSMTPMailer(),
		WebFS:      web.DistFS,
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("servidor iniciado em http://0.0.0.0:%s (DATA_DIR=%s)", port, dataDir)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("erro no servidor HTTP: %v", err)
		}
	case <-ctx.Done():
		log.Printf("encerrando servidor...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("erro no encerramento gracioso: %v", err)
		}
	}
}
