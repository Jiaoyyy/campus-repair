package main

import (
	"campus-repair/internal/app"
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	c, err := app.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	secret := os.Getenv("MCP_CALL_SECRET")
	if len(secret) < 32 {
		log.Fatal("MCP_CALL_SECRET must contain at least 32 characters")
	}
	db, err := sql.Open("mysql", c.DSN)
	if err != nil {
		log.Fatal("database configuration invalid")
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		log.Fatal("database unavailable during startup")
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", app.MCPHandler(db, secret))
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	addr := os.Getenv("MCP_ADDR")
	if addr == "" {
		addr = ":8081"
	}
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	stop, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()
	go func() {
		<-stop.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Print("campus repair MCP ready")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("MCP HTTP server failed")
	}
}
