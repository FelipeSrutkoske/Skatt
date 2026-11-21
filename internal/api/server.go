package api

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/ai"
	"github.com/FelipeSrutkoske/Skatt/internal/parser"
	"github.com/FelipeSrutkoske/Skatt/internal/receipt"
	"github.com/FelipeSrutkoske/Skatt/internal/store"
)

type Dependencies struct {
	Store              *store.Store
	Staging            *parser.StagingManager
	DataDir            string
	UploadsDir         string
	Mailer             receipt.Mailer
	AIExtractorFactory func(apiKey, model string) ai.Extractor
	WebFS              fs.FS
}

type Server struct {
	store              *store.Store
	staging            *parser.StagingManager
	dataDir            string
	uploadsDir         string
	mailer             receipt.Mailer
	aiExtractorFactory func(apiKey, model string) ai.Extractor
	webFS              fs.FS
	mux                *http.ServeMux
}

func NewServer(deps Dependencies) http.Handler {
	mailer := deps.Mailer
	if mailer == nil {
		mailer = receipt.NewSMTPMailer()
	}

	aiFactory := deps.AIExtractorFactory
	if aiFactory == nil {
		aiFactory = func(apiKey, model string) ai.Extractor {
			return ai.NewGeminiClient(apiKey, model, "")
		}
	}

	uploadsDir := deps.UploadsDir
	if uploadsDir == "" && deps.DataDir != "" {
		uploadsDir = filepath.Join(deps.DataDir, "uploads")
	}

	s := &Server{
		store:              deps.Store,
		staging:            deps.Staging,
		dataDir:            deps.DataDir,
		uploadsDir:         uploadsDir,
		mailer:             mailer,
		aiExtractorFactory: aiFactory,
		webFS:              deps.WebFS,
		mux:                http.NewServeMux(),
	}

	s.registerRoutes()
	return s.mux
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/dashboard", s.handleGetDashboard)

	s.mux.HandleFunc("GET /api/people", s.handleListPeople)
	s.mux.HandleFunc("POST /api/people", s.handleCreatePerson)
	s.mux.HandleFunc("PUT /api/people/{id}", s.handleUpdatePerson)
	s.mux.HandleFunc("DELETE /api/people/{id}", s.handleDeletePerson)
	s.mux.HandleFunc("GET /api/people/{id}/ledger", s.handleGetPersonLedger)

	s.mux.HandleFunc("POST /api/payments", s.handleCreatePayment)
	s.mux.HandleFunc("DELETE /api/payments/{id}", s.handleDeletePayment)

	s.mux.HandleFunc("POST /api/imports/preview", s.handlePreviewImport)
	s.mux.HandleFunc("POST /api/imports/preview/{stagingId}/unlock", s.handleUnlockImport)
	s.mux.HandleFunc("POST /api/imports/preview/{stagingId}/ai-extract", s.handleAIExtractImport)
	s.mux.HandleFunc("POST /api/imports/confirm", s.handleConfirmImport)

	s.mux.HandleFunc("GET /api/statements/{id}", s.handleGetStatement)
	s.mux.HandleFunc("DELETE /api/statements/{id}", s.handleDeleteStatement)
	s.mux.HandleFunc("GET /api/statements/{id}/file", s.handleDownloadStatementFile)
	s.mux.HandleFunc("POST /api/statements/{id}/apply-suggestions", s.handleApplySuggestions)
	s.mux.HandleFunc("POST /api/statements/{id}/assign-remaining", s.handleAssignRemaining)

	s.mux.HandleFunc("PUT /api/transactions/{id}/splits", s.handleSetTransactionSplits)
	s.mux.HandleFunc("PATCH /api/transactions/{id}", s.handleUpdateTransaction)

	s.mux.HandleFunc("GET /api/receipts/availability", s.handleGetReceiptAvailability)
	s.mux.HandleFunc("POST /api/receipts/preview", s.handlePreviewReceipt)
	s.mux.HandleFunc("POST /api/receipts/send", s.handleSendReceipt)
	s.mux.HandleFunc("GET /api/receipts", s.handleListReceipts)
	s.mux.HandleFunc("GET /api/receipts/{id}/html", s.handleGetReceiptHTML)

	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handleUpdateSettings)
	s.mux.HandleFunc("POST /api/settings/smtp/test", s.handleTestSMTP)

	s.mux.HandleFunc("GET /api/backup/export", s.handleExportBackup)
	s.mux.HandleFunc("POST /api/backup/restore", s.handleRestoreBackup)

	s.mux.HandleFunc("/api/", s.handleAPINotFound)
	s.mux.HandleFunc("/", s.handleSPA)
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "endpoint nao encontrado")
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		writeError(w, http.StatusNotFound, "endpoint nao encontrado")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "metodo nao permitido")
		return
	}
	if s.webFS == nil {
		http.NotFound(w, r)
		return
	}

	cleaned := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if cleaned != "" && cleaned != "." && !strings.HasPrefix(path.Base(cleaned), ".") {
		if info, err := fs.Stat(s.webFS, cleaned); err == nil && !info.IsDir() {
			http.FileServer(http.FS(s.webFS)).ServeHTTP(w, r)
			return
		}
	}

	f, err := s.webFS.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "erro ao carregar aplicacao", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (s *Server) resolveUploadPath(storedPath string) (string, bool) {
	trimmed := strings.TrimSpace(storedPath)
	if trimmed == "" || s.uploadsDir == "" {
		return "", false
	}
	cleaned := filepath.Clean(filepath.FromSlash(trimmed))
	if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	absBase, err := filepath.Abs(s.uploadsDir)
	if err != nil {
		return "", false
	}
	target := filepath.Join(absBase, cleaned)
	rel, err := filepath.Rel(absBase, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return target, true
}

func (s *Server) removeUploadedFile(storedPath string) {
	target, ok := s.resolveUploadPath(storedPath)
	if !ok {
		return
	}
	_ = os.Remove(target)
}
