package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/grantdeshazer/build-server/internal/config"
	"github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/server"
)

//go:embed templates static
var embeddedFS embed.FS

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	basePath := flag.String("base-path", "", "URL path prefix when served behind a reverse proxy (e.g. /build-server)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// CLI flag takes precedence; fall back to config value.
	// Normalize: strip trailing slash so "/build-server/" → "/build-server".
	bp := strings.TrimRight(*basePath, "/")
	if bp == "" {
		bp = strings.TrimRight(cfg.Server.BasePath, "/")
	}

	database, err := db.Open(cfg.Server.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	fmt.Printf("connected to db at: %v", cfg.Server.DBPath)
	defer database.Close()

	if err := db.SyncRepos(database, cfg.Repositories); err != nil {
		log.Fatalf("sync repos: %v", err)
	}

	var fsys fs.FS = embeddedFS
	srv, err := server.New(cfg, database, fsys, bp)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	addr := srv.Addr()
	fmt.Printf("build-server listening on http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
