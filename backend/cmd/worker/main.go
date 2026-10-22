package main

import (
	"campus-repair/internal/app"
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"os"
	"os/signal"
	"syscall"
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
	db.SetMaxOpenConns(5)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app.RunIndexer(ctx, db)
}
