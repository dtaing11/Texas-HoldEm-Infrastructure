package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/connection"
	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
)

func main() {
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("API_KEY env var required")
	}

	// Build your table + engine
	table := game.NewTable("table-1")

	// Example players (you’ll probably add them via your own join flow)
	table.AddPlayer(&game.Player{ID: "p1", Chips: 1000})
	table.AddPlayer(&game.Player{ID: "p2", Chips: 1000})
	table.AddPlayer(&game.Player{ID: "p3", Chips: 1000})

	engine := game.NewEngine(table, 5, 10)

	// WS server
	srv := connection.NewServer(apiKey)
	srv.RegisterTable("table-1", table, engine)

	// Run
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := connection.StartHTTPServer(ctx, srv); err != nil {
		log.Fatal(err)
	}
}
