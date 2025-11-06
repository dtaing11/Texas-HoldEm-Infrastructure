// main.go
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
		apiKey = "dev" // local testing
	}

	// Build one table & engine so /ws has something to serve.
	t := game.NewTable("table-1")
	t.AddPlayer(&game.Player{ID: "p1", Chips: 1000})
	t.AddPlayer(&game.Player{ID: "p2", Chips: 1000})
	t.AddPlayer(&game.Player{ID: "p3", Chips: 1000})
	e := game.NewEngine(t, 5, 10)

	// Create WS server (registers /healthz and /ws)
	s := connection.NewServer(apiKey)
	s.RegisterTable("table-1", t, e)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := connection.StartHTTPServer(ctx, s); err != nil {
		log.Fatal(err)
	}
}
