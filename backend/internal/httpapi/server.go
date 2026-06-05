package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"foundry-tx-simulator/backend/internal/config"
	"foundry-tx-simulator/backend/internal/model"
	"foundry-tx-simulator/backend/internal/projectcache"
	"foundry-tx-simulator/backend/internal/simulation"
)

type Server struct {
	cfg              config.Config
	configPath       string
	chooseProjectDir func(context.Context) (string, error)
	projectCache     *projectcache.Cache
	simulator        *simulation.Service
}

func NewServer(cfg config.Config, configPath string) *Server {
	projectCachePath := cfg.ProjectCachePath
	if projectCachePath == "" && cfg.WorkDir != "" {
		projectCachePath = filepath.Join(cfg.WorkDir, "projects.json")
	}
	return &Server{
		cfg:              cfg,
		configPath:       configPath,
		chooseProjectDir: chooseProjectDirectory,
		projectCache:     projectcache.New(projectCachePath, projectcache.DefaultLimit),
		simulator:        simulation.NewService(cfg),
	}
}

func (s *Server) Close() {
	if s != nil && s.simulator != nil {
		s.simulator.Close()
	}
}

func (s *Server) Routes() http.Handler {
	router := chi.NewRouter()
	router.Use(debugHTTP)
	router.Use(localCORS)

	router.Get("/docs", s.handleSwaggerUI)
	router.Get("/docs/*", s.handleSwaggerUI)
	router.Get("/openapi.json", s.handleOpenAPI)
	router.Get("/health", s.handleHealth)
	router.Get("/chains", s.handleChains)
	router.Get("/projects", s.handleProjects)
	router.Get("/browse/project", s.handleBrowseProject)
	router.Get("/requests/{id}", s.handleRequestRecord)
	router.Post("/simulation", s.handleSimulation)
	router.Post("/tx", s.handleTx)
	router.Options("/*", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	return router
}

func localCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, model.HealthResponse{
		OK:                true,
		Chains:            len(s.cfg.RPCURLs),
		MaxConcurrentRuns: s.cfg.MaxConcurrent,
	})
}

func (s *Server) handleChains(w http.ResponseWriter, _ *http.Request) {
	chains := make([]string, 0, len(s.cfg.RPCURLs))
	for chain := range s.cfg.RPCURLs {
		chains = append(chains, chain)
	}
	sort.Strings(chains)
	writeJSON(w, http.StatusOK, model.ChainsResponse{
		Chains:       chains,
		ExplorerURLs: s.cfg.ExplorerURLs,
	})
}

func (s *Server) handleProjects(w http.ResponseWriter, _ *http.Request) {
	projects, err := s.projectCache.List()
	if err != nil {
		slog.Warn("list cached projects", "error", err)
		projects = []string{}
	}
	writeJSON(w, http.StatusOK, model.ProjectsResponse{Projects: projects})
}

func (s *Server) handleBrowseProject(w http.ResponseWriter, r *http.Request) {
	path, err := s.chooseProjectDir(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, model.ErrorResponse{Error: "choose project folder: " + err.Error()})
		return
	}
	s.rememberProjectPath(path)
	writeJSON(w, http.StatusOK, model.BrowseProjectResponse{Path: path})
}

func (s *Server) handleRequestRecord(w http.ResponseWriter, r *http.Request) {
	record, err := s.simulator.LoadRecord(chi.URLParam(r, "id"))
	if errors.Is(err, simulation.ErrInvalidRecordID) {
		writeJSON(w, http.StatusBadRequest, model.ErrorResponse{Error: err.Error()})
		return
	}
	if errors.Is(err, simulation.ErrRecordNotFound) {
		writeJSON(w, http.StatusNotFound, model.ErrorResponse{Error: err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.ErrorResponse{Error: "load request record: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleSimulation(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := r.Body.Close(); err != nil {
			slog.Warn("close request body", "error", err)
		}
	}()

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()

	var req model.SimulateRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.ErrorResponse{Error: "invalid JSON body: " + err.Error()})
		return
	}

	projectPath := strings.TrimSpace(req.ProjectPath)
	resp, status := s.simulator.Simulate(r.Context(), req)
	if status < http.StatusBadRequest {
		s.rememberProjectPath(projectPath)
	}
	writeJSON(w, status, resp)
}

func (s *Server) handleTx(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := r.Body.Close(); err != nil {
			slog.Warn("close request body", "error", err)
		}
	}()

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()

	var req model.TxRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.ErrorResponse{Error: "invalid JSON body: " + err.Error()})
		return
	}

	resp, status := s.simulator.ReplayTx(r.Context(), req)
	writeJSON(w, status, resp)
}

func (s *Server) rememberProjectPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if err := s.projectCache.Add(path); err != nil {
		slog.Warn("cache project path", "path", path, "error", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Warn("write response", "error", err)
	}
}
