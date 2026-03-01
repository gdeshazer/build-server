package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/grantdeshazer/build-server/internal/config"
	"github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/server"
)

//go:embed templates static
var embeddedFS embed.FS

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	database, err := db.Open(cfg.Server.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.SyncRepos(database, cfg.Repositories); err != nil {
		log.Fatalf("sync repos: %v", err)
	}

	var fsys fs.FS = embeddedFS
	srv, err := server.New(cfg, database, fsys)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	addr := srv.Addr()
	fmt.Printf("build-server listening on http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
