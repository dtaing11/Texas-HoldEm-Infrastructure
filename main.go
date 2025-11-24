// main.go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/connection"
	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
)

func main() {
	// General auth key for all WS clients
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		apiKey = "dev" // local testing
	}

	// Special key that is allowed to start a game (host key)
	startKey := os.Getenv("START_KEY")
	if startKey == "" {
		startKey = "host-dev" // local testing
	}

	// One empty table; players will be added dynamically as they join.
	t := game.NewTable("table-1")
	e := game.NewEngine(t, 5, 10) // 5/10 blinds

	// Create WS server with general API key + host-only start key
	s := connection.NewServer(apiKey)
	s.RegisterTable("table-1", t, e)

	mux := http.NewServeMux()
	s.ServeHTTP(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("listening on :%s ...", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
