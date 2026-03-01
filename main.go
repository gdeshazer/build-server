package main

import (
	"embed"
	"flag"
	"io/fs"
	"net/http"
	"strings"

	"github.com/grantdeshazer/build-server/internal/config"
	"github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/logger"
	"github.com/grantdeshazer/build-server/internal/server"
)

//go:embed templates static
var embeddedFS embed.FS

func main() {
	// Initialize logger first so all subsequent logging uses our structured logger
	logger.Init(logger.INFO)
	logger.Info("build-server starting up")

	configPath := flag.String("config", "config.yaml", "path to config file")
	basePath := flag.String("base-path", "", "URL path prefix when served behind a reverse proxy (e.g. /build-server)")
	flag.Parse()

	logger.Info("Loading configuration from: %s", *configPath)
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("Failed to load configuration: %v", err)
	}
	logger.Info("Configuration loaded successfully")

	// CLI flag takes precedence; fall back to config value.
	// Normalize: strip trailing slash so "/build-server/" → "/build-server".
	bp := strings.TrimRight(*basePath, "/")
	if bp == "" {
		bp = strings.TrimRight(cfg.Server.BasePath, "/")
	}
	logger.Info("Server base path: %s", bp)

	logger.Info("Opening database at: %s", cfg.Server.DBPath)
	database, err := db.Open(cfg.Server.DBPath)
	if err != nil {
		logger.Fatal("Failed to open database: %v", err)
	}
	logger.Info("Database connection established")
	defer database.Close()

	logger.Info("Synchronizing repositories from configuration")
	if err := db.SyncRepos(database, cfg.Repositories); err != nil {
		logger.Fatal("Failed to synchronize repositories: %v", err)
	}
	logger.Info("Repository synchronization completed")

	var fsys fs.FS = embeddedFS
	logger.Info("Initializing HTTP server")
	srv, err := server.New(cfg, database, fsys, bp)
	if err != nil {
		logger.Fatal("Failed to initialize server: %v", err)
	}
	logger.Info("HTTP server initialized successfully")

	addr := srv.Addr()
	logger.Info("build-server listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		logger.Fatal("Server failed: %v", err)
	}
}
