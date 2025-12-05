// connection/server.go
package connection

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
	"github.com/gorilla/websocket"
)

// Server manages tables and WebSocket connections.
type Server struct {
	apiKey   string
	startKey string

	mu     sync.Mutex
	tables map[string]*tableBinding
}

// tableBinding ties a game.Table + Engine to connected clients.
type tableBinding struct {
	Table  *game.Table
	Engine *game.Engine

	mu          sync.Mutex
	clients     map[*Client]struct{}
	host        *Client
	handCounter int
}

// Client is a single WebSocket connection (either host or player).
type Client struct {
	conn     *websocket.Conn
	server   *Server
	binding  *tableBinding
	playerID string
	isHost   bool
	send     chan []byte
}

// Messages coming from clients.
type inboundMessage struct {
	Type   string      `json:"type"`             // "join", "act", "host_start", "host_reset"
	Player string      `json:"player,omitempty"` // (unused now)
	Action game.Action `json:"action,omitempty"` // CHECK/CALL/RAISE/FOLD
	Amount int         `json:"amount,omitempty"` // for RAISE
}

// Messages going to clients.
type StatePayload struct {
	Type  string       `json:"type"` // "state"
	State *PublicState `json:"state"`
}

// PublicState is per-client filtered view.
type PublicState struct {
	Table    *game.Table `json:"table"`
	Pot      int         `json:"pot"`
	Phase    game.Phase  `json:"phase"`
	Board    []game.Card `json:"board"`
	ToCall   int         `json:"ToCall"`
	ToActIdx int         `json:"toActIdx"`
	Hand     int         `json:"hand"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// NewServer takes a general API key (for all WS clients)
// and a special startKey that only the host knows.
func NewServer(apiKey, startKey string) *Server {
	return &Server{
		apiKey:   apiKey,
		startKey: startKey,
		tables:   make(map[string]*tableBinding),
	}
}

// RegisterTable wires a table + engine into this server under a given ID.
func (s *Server) RegisterTable(id string, t *game.Table, e *game.Engine) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tables[id] = &tableBinding{
		Table:   t,
		Engine:  e,
		clients: make(map[*Client]struct{}),
	}
}

// ServeHTTP registers the /ws endpoint on the provided mux.
func (s *Server) ServeHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/ws", s.handleWS)
}

// handleWS upgrades to WebSocket, authenticates, and creates a Client.
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

	// Determine if this client is the host (god mode) by startKey.
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

	// Register client with the table.
	binding.mu.Lock()
	binding.clients[client] = struct{}{}
	if isHost {
		log.Printf("[ws] host connected: table=%s hostID=%s", tableID, playerID)
		binding.host = client
	} else {
		// Seat only real players, never host.
		ensurePlayerSeated(binding.Table, playerID)
		log.Printf("[ws] player joined: table=%s player=%s", tableID, playerID)
	}

	// Decide whether we should auto-start a hand now
	shouldStart := binding.canStartHandLocked()
	if shouldStart {
		log.Printf("[HOST_LOGIC] Auto-starting hand #%d (on connect)", binding.handCounter)
		if err := binding.Engine.StartHand(); err != nil {
			log.Printf("[HOST_LOGIC] StartHand error: %v", err)
		}
	}
	// Broadcast state while we still hold the lock.
	binding.broadcastStateLocked()
	binding.mu.Unlock()

	go client.writeLoop()
	go client.readLoop()
}

// ensurePlayerSeated adds a player with default chips if not already present.
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

// canStartHandLocked returns true if:
// - host is connected
// - table is in WAITING phase
// - at least 2 seated players have chips
// NOTE: must be called with tb.mu held.
func (tb *tableBinding) canStartHandLocked() bool {
	if tb.host == nil {
		return false
	}
	if tb.Table == nil || tb.Engine == nil {
		return false
	}
	if tb.Table.Phase != game.WAITING {
		return false
	}
	// count players with chips
	count := 0
	for _, p := range tb.Table.Players {
		if p != nil && p.Chips > 0 {
			count++
		}
	}
	return count >= 2
}

// buildPublicStateFor returns a per-client filtered state:
// - Host: sees everything (all cards).
// - Player: sees only their own hole cards; others masked.
func (tb *tableBinding) buildPublicStateFor(c *Client) *PublicState {
	// Shallow copy table.
	tableCopy := *tb.Table

	// Deep-ish copy players slice so we can mask cards safely.
	playersCopy := make([]*game.Player, len(tb.Table.Players))
	for i, p := range tb.Table.Players {
		if p == nil {
			continue
		}
		cp := *p
		// Mask hole cards for other players if this client is not host.
		if !c.isHost && cp.ID != c.playerID {
			cp.Cards = [2]game.Card{}
		}
		playersCopy[i] = &cp
	}
	tableCopy.Players = playersCopy

	return &PublicState{
		Table:    &tableCopy,
		Pot:      tb.Engine.Pot,
		Phase:    tb.Table.Phase,
		Board:    tb.Table.CardOpen,
		ToActIdx: tb.Engine.ToActIndex(),
		ToCall:   tb.Engine.MinRaise,
		Hand:     tb.handCounter,
	}
}

// broadcastStateLocked assumes tb.mu is already held.
func (tb *tableBinding) broadcastStateLocked() {
	for c := range tb.clients {
		state := tb.buildPublicStateFor(c)
		payload := StatePayload{
			Type:  "state",
			State: state,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			log.Printf("[ws] marshal state error: %v", err)
			continue
		}
		select {
		case c.send <- data:
		default:
		}
	}
}

// broadcastState is a safe helper when you DON'T already hold tb.mu.
func (tb *tableBinding) broadcastState() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.broadcastStateLocked()
}

// readLoop processes JSON messages from this client.
func (c *Client) readLoop() {
	defer c.close()

	for {
		var msg inboundMessage
		if err := c.conn.ReadJSON(&msg); err != nil {
			log.Printf("[ws] read error (player=%s): %v", c.playerID, err)
			return
		}

		switch msg.Type {
		case "join":
			// Already handled at connect time.

		case "act":
			// Normal player acting for themselves.
			if c.isHost {
				// host should not send `act`
				continue
			}
			c.handlePlayerAct(msg)

		case "host_start":
			// kept for manual use
			if !c.isHost {
				log.Printf("[ws] non-host tried host_start (player=%s)", c.playerID)
				continue
			}
			c.handleHostStart()

		case "host_reset":
			// host can reset the whole game
			if !c.isHost {
				log.Printf("[ws] non-host tried host_reset (player=%s)", c.playerID)
				continue
			}
			c.handleHostReset()

		default:
			log.Printf("[ws] unknown message type: %s", msg.Type)
		}
	}
}

// writeLoop sends messages from c.send to the WebSocket.
func (c *Client) writeLoop() {
	defer c.close()

	for {
		data, ok := <-c.send
		if !ok {
			return
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[ws] write error: %v", err)
			return
		}
	}
}

// close unregisters the client and closes its connection.
func (c *Client) close() {
	// Make close idempotent by recovering panics from double-close on send.
	defer func() {
		recover()
	}()

	c.binding.mu.Lock()
	if _, ok := c.binding.clients[c]; ok {
		delete(c.binding.clients, c)
	}
	if c.binding.host == c {
		log.Printf("[ws] host disconnected: player=%s", c.playerID)
		c.binding.host = nil
	}
	c.binding.mu.Unlock()

	_ = c.conn.Close()
	close(c.send)
}

// handlePlayerAct handles a normal player's action (acting for themselves).
func (c *Client) handlePlayerAct(msg inboundMessage) {
	tb := c.binding

	tb.mu.Lock()
	defer tb.mu.Unlock()

	log.Printf("[ws] handlePlayerAct: player=%s action=%v amount=%d",
		c.playerID, msg.Action, msg.Amount)

	// SERVER-SIDE GATE:
	// If the engine says this player cannot act (not their turn, etc.),
	// ignore the request and just broadcast the current state.
	if !tb.Engine.CanPlayerAct(c.playerID) {
		log.Printf("[ws] ignoring act from %s: CanPlayerAct=false (phase=%s, toActIdx=%d)",
			c.playerID, tb.Table.Phase, tb.Engine.ToActIndex())
		tb.broadcastStateLocked()
		return
	}

	prevPhase := tb.Table.Phase

	// Try the requested action first
	err := tb.Engine.Act(game.ActRequest{
		PlayerID: c.playerID,
		Action:   msg.Action,
		Amount:   msg.Amount,
	})
	if err != nil {
		// With "illegal ⇒ fold" semantics, this should mostly be fatal stuff
		// like ErrHandNotRunning / ErrNoSuchPlayer.
		log.Printf("[ws] player act error: table=%s player=%s err=%v",
			tb.Table.ID, c.playerID, err)

		foldReq := game.ActRequest{
			PlayerID: c.playerID,
			Action:   game.FOLD, // adjust if your constant name differs
			Amount:   0,
		}

		if foldErr := tb.Engine.Act(foldReq); foldErr != nil {
			log.Printf("[ws] auto-fold failed: table=%s player=%s err=%v",
				tb.Table.ID, c.playerID, foldErr)
			// still broadcast whatever state we’re in
			tb.broadcastStateLocked()
			return
		}

		// Check if the forced fold ended the hand
		if prevPhase != game.WAITING && tb.Table.Phase == game.WAITING {
			log.Printf("[HOST_LOGIC] Hand #%d finished (via auto-fold).", tb.handCounter)
			tb.handCounter++

			if tb.canStartHandLocked() {
				log.Printf("[HOST_LOGIC] Auto-starting hand #%d (post-hand via auto-fold)", tb.handCounter)
				if err := tb.Engine.StartHand(); err != nil {
					log.Printf("[HOST_LOGIC] StartHand error (post-hand via auto-fold): %v", err)
				}
			}
		}

		tb.broadcastStateLocked()
		return
	}

	// Normal successful action path
	if prevPhase != game.WAITING && tb.Table.Phase == game.WAITING {
		log.Printf("[HOST_LOGIC] Hand #%d finished.", tb.handCounter)
		tb.handCounter++

		// Auto-start next hand if possible.
		if tb.canStartHandLocked() {
			log.Printf("[HOST_LOGIC] Auto-starting hand #%d (post-hand)", tb.handCounter)
			if err := tb.Engine.StartHand(); err != nil {
				log.Printf("[HOST_LOGIC] StartHand error (post-hand): %v", err)
			}
		}
	}

	tb.broadcastStateLocked()
}

// handleHostStart lets the host start a new hand (kept for safety/manual use).
func (c *Client) handleHostStart() {
	tb := c.binding

	tb.mu.Lock()
	defer tb.mu.Unlock()

	canStart := tb.canStartHandLocked()
	if !canStart {
		log.Printf("[HOST_LOGIC] host_start ignored: either no host, not WAITING, or <2 players")
		return
	}

	log.Printf("[HOST_LOGIC] Starting hand #%d (host_start)", tb.handCounter)
	if err := tb.Engine.StartHand(); err != nil {
		log.Printf("[HOST_LOGIC] StartHand error: %v", err)
		return
	}

	tb.broadcastStateLocked()
}

// handleHostReset lets the host reset the table:
// - All players back to 1000 chips
// - handCounter reset to 0
// - phase -> WAITING
// - then immediately start a fresh hand if possible
func (c *Client) handleHostReset() {
	tb := c.binding

	tb.mu.Lock()
	defer tb.mu.Unlock()

	log.Printf("[HOST_LOGIC] host_reset requested by host=%s", c.playerID)

	// Reset our logical hand counter
	tb.handCounter = 0

	// Reset engine + table stacks/state
	tb.Engine.ResetGame(1000)

	// Try to immediately start a new hand if conditions allow:
	// host present, phase=WAITING, at least 2 players with chips.
	if tb.canStartHandLocked() {
		log.Printf("[HOST_LOGIC] host_reset: starting fresh hand #%d", tb.handCounter)
		if err := tb.Engine.StartHand(); err != nil {
			log.Printf("[HOST_LOGIC] StartHand error after reset: %v", err)
		}
	}

	// Broadcast fresh state to everyone
	tb.broadcastStateLocked()
}
