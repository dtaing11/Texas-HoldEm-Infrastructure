package main

import (
	"log"
	"net/http"
	"os"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/connection"
	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
)

func main() {
	// General API key
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		apiKey = "dev"
	}

	// Host-only start key ("god key")
	startKey := os.Getenv("START_KEY")
	if startKey == "" {
		startKey = "supersecret"
	}

	// Create table + engine
	t := game.NewTable("table-1")
	e := game.NewEngine(t, 5, 10)

	// NEW: pass BOTH keys to NewServer
	s := connection.NewServer(apiKey, startKey)

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
