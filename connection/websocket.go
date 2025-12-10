package connection

import (
	"log"
	"net/http"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	apiKey := q.Get("apiKey")

	if apiKey != s.apiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tableID := q.Get("table")
	playerID := q.Get("player")
	if tableID == "" || playerID == "" {
		http.Error(w, "missing table or player", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	binding, ok := s.tables[tableID]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "unknown table", http.StatusNotFound)
		return
	}

	isHost := false
	if sk := q.Get("startKey"); sk != "" && sk == s.startKey {
		isHost = true
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	client := &Client{
		conn:     conn,
		server:   s,
		binding:  binding,
		playerID: playerID,
		isHost:   isHost,
		send:     make(chan []byte, 32),
	}

	binding.mu.Lock()

	binding.clients[client] = struct{}{}

	if isHost {
		log.Printf("[ws] host connected: table=%s hostID=%s", tableID, playerID)
		binding.host = client
	} else {
		ensurePlayerSeated(binding.Table, playerID)
		log.Printf("[ws] player joined: table=%s player=%s", tableID, playerID)
	}

	shouldStart := binding.canStartHandLocked()
	if shouldStart {
		log.Printf("[HOST_LOGIC] Auto-starting hand #%d (on connect)", binding.handCounter)
		if err := binding.Engine.StartHand(); err != nil {
			log.Printf("[HOST_LOGIC] StartHand error: %v", err)
		}
	}

	binding.broadcastStateLocked()
	binding.mu.Unlock()

	go client.writeLoop()
	go client.readLoop()
}

func ensurePlayerSeated(t *game.Table, id string) *game.Player {
	for _, p := range t.Players {
		if p != nil && p.ID == id {
			return p
		}
	}
	p := &game.Player{
		ID:    id,
		Chips: 1000,
	}
	t.AddPlayer(p)
	return p
}
