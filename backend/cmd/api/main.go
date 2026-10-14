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
	db, err := sql.Open("mysql", c.DSN)
	if err != nil {
		log.Fatal("database configuration invalid")
	}
	defer db.Close()
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(6)
	db.SetConnMaxLifetime(5 * time.Minute)
	var ready bool
	for i := 0; i < 60; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		log.Fatal("database unavailable during startup")
	}
	if err = app.Initialize(db, c); err != nil {
		log.Fatal("database initialization failed")
	}
	server := &http.Server{Addr: c.Addr, Handler: app.NewServer(db, c.Secret).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stop, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()
	go func() {
		<-stop.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Print("campus repair API ready")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("HTTP server failed")
	}
}
