// internal/connection/server.go
package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
)

// starting chip stack for new players
const (
	defaultChips = 1000
)

// ------------ Public Server ------------

// Server holds global config and all table hubs.
type Server struct {
	apiKey       string
	hostStartKey string // secret that allows a client to become "host"

	// tableID -> hub
	hubsMu sync.RWMutex
	hubs   map[string]*hub

	upgrader websocket.Upgrader

	// optional: logger
	log *log.Logger
}

// NewServer constructs a WebSocket server. apiKey is required for auth.
// Host start key is read from env START_KEY (default: "supersecret").
func NewServer(apiKey string) *Server {
	startKey := os.Getenv("START_KEY")
	if strings.TrimSpace(startKey) == "" {
		startKey = "supersecret"
	}

	s := &Server{
		apiKey:       apiKey,
		hostStartKey: startKey,
		hubs:         make(map[string]*hub),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// You can lock this down further with an origin check if needed.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		log: log.New(os.Stdout, "[ws] ", log.LstdFlags|log.Lmicroseconds),
	}
	return s
}

// RegisterTable wires a game.Table + Engine into a WebSocket hub under the given tableID.
// Call this once per table during process init (or dynamically when creating tables).
func (s *Server) RegisterTable(tableID string, t *game.Table, e *game.Engine) {
	s.hubsMu.Lock()
	defer s.hubsMu.Unlock()
	if _, ok := s.hubs[tableID]; ok {
		return
	}
	h := newHub(tableID, t, e, s.log)
	s.hubs[tableID] = h
	go h.run()
}

// ServeHTTP attaches routes for WS & health. Mount this on your mux.
func (s *Server) ServeHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ws", s.handleWS)
}

// ------------ Auth helpers ------------

func (s *Server) authorize(r *http.Request) (tableID string, err error) {
	// Accept credentials via query OR headers
	q := r.URL.Query()
	apiKey := firstNonEmpty(
		q.Get("apiKey"),
		r.Header.Get("X-API-Key"),
	)
	tableID = firstNonEmpty(
		q.Get("table"),
		r.Header.Get("X-Table-ID"),
	)

	if apiKey == "" || tableID == "" || apiKey != s.apiKey {
		return "", ErrUnauthorized
	}
	return tableID, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ------------ WS Handler ------------

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	tableID, err := s.authorize(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.hubsMu.RLock()
	h, ok := s.hubs[tableID]
	s.hubsMu.RUnlock()
	if !ok {
		http.Error(w, "table not found", http.StatusNotFound)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Printf("upgrade error: %v", err)
		return
	}

	// Identify player from query/header (required).
	playerID := firstNonEmpty(
		r.URL.Query().Get("player"),
		r.Header.Get("X-Player-ID"),
	)
	if playerID == "" {
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "missing player id"),
			time.Now().Add(2*time.Second))
		_ = conn.Close()
		return
	}

	// Host detection: needs correct startKey.
	isHost := false
	startKey := firstNonEmpty(
		r.URL.Query().Get("startKey"),
		r.Header.Get("X-Start-Key"),
	)
	if startKey != "" && startKey == s.hostStartKey {
		isHost = true
	}

	client := &client{
		playerID: playerID,
		h:        h,
		conn:     conn,
		send:     make(chan []byte, 64),
		log:      s.log,
		isHost:   isHost,
	}

	// register & spawn pumps
	h.register <- client
	go client.writePump()
	go client.readPump()
}

// =====================================================
// ================ Hub & Client =======================
// =====================================================

type hub struct {
	tableID string
	table   *game.Table
	engine  *game.Engine

	// live clients
	mu      sync.RWMutex
	clients map[*client]struct{}

	// channels
	register   chan *client
	unregister chan *client
	broadcast  chan []byte

	log *log.Logger
}

func newHub(tableID string, t *game.Table, e *game.Engine, logger *log.Logger) *hub {
	return &hub{
		tableID:    tableID,
		table:      t,
		engine:     e,
		clients:    make(map[*client]struct{}),
		register:   make(chan *client),
		unregister: make(chan *client),
		broadcast:  make(chan []byte, 128),
		log:        logger,
	}
}

func (h *hub) run() {
	ticker := time.NewTicker(25 * time.Second) // ping cadence safety
	defer ticker.Stop()

	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			// Ensure this player exists with chips on the table.
			h.ensurePlayer(c.playerID)
			h.mu.Unlock()

			h.log.Printf("client join: table=%s player=%s host=%v", h.tableID, c.playerID, c.isHost)
			h.pushStateTo(c)

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.mu.Unlock()
			h.log.Printf("client leave: table=%s player=%s", h.tableID, c.playerID)

		case msg := <-h.broadcast:
			h.mu.Lock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					// slow consumer: drop connection
					close(c.send)
					delete(h.clients, c)
				}
			}
			h.mu.Unlock()

		case <-ticker.C:
			// periodic state push (optional); can be removed if purely event-driven
			h.pushState()
		}
	}
}

// ensurePlayer seats a player on the table if they don't already exist.
func (h *hub) ensurePlayer(playerID string) *game.Player {
	for _, p := range h.table.Players {
		if p != nil && p.ID == playerID {
			return p
		}
	}
	p := &game.Player{
		ID:    playerID,
		Chips: defaultChips,
	}
	h.table.AddPlayer(p)
	h.log.Printf("new player seated: table=%s player=%s chips=%d",
		h.tableID, playerID, p.Chips)
	return p
}

func (h *hub) pushState() {
	payload := serverMessage{
		Type: "state",
		State: &statePayload{
			Table:    h.table,
			Pot:      h.engine.Pot,
			Phase:    string(h.table.Phase),
			Board:    h.table.CardOpen,
			ToActIdx: h.peekToActIdx(),
		},
	}
	h.broadcastJSON(payload)
}

func (h *hub) pushStateTo(c *client) {
	payload := serverMessage{
		Type: "state",
		State: &statePayload{
			Table:    h.table,
			Pot:      h.engine.Pot,
			Phase:    string(h.table.Phase),
			Board:    h.table.CardOpen,
			ToActIdx: h.peekToActIdx(),
		},
	}
	c.sendJSON(payload)
}

// peekToActIdx asks the engine whose turn it is.
// Requires game.Engine.ToActIndex() int to be implemented.
func (h *hub) peekToActIdx() int {
	if h.engine == nil {
		return -1
	}
	return h.engine.ToActIndex()
}

func (h *hub) broadcastJSON(v any) {
	b, _ := json.Marshal(v)
	h.broadcast <- b
}

func (h *hub) handleClientMessage(c *client, in clientMessage) {
	switch in.Type {
	case "join":

		// No-op: presence is already established. Could re-send state.
		h.pushStateTo(c)

	case "start_hand":
		// Only host can start a hand
		if !c.isHost {
			c.sendJSON(serverMessage{Type: "error", Error: "only host can start hand"})
			return
		}

		if err := h.engine.StartHand(); err != nil {
			c.sendJSON(serverMessage{Type: "error", Error: err.Error()})
			return
		}
		h.pushState()

	case "act":
		// validate fields
		if in.PlayerID == "" || in.Action == "" {
			c.sendJSON(serverMessage{Type: "error", Error: "missing playerId or action"})
			return
		}
		a := strings.ToUpper(in.Action)
		var act game.Action
		switch a {
		case "FOLD":
			act = game.FOLD
		case "CHECK":
			act = game.CHECK
		case "CALL":
			act = game.CALL
		case "RAISE":
			act = game.RAISE
		default:
			c.sendJSON(serverMessage{Type: "error", Error: "invalid action"})
			return
		}
		amt := 0
		if in.Amount != nil {
			amt = *in.Amount
		}
		err := h.engine.Act(game.ActRequest{
			PlayerID: in.PlayerID,
			Action:   act,
			Amount:   amt, // raise size (not final total)
		})
		if err != nil {
			c.sendJSON(serverMessage{Type: "error", Error: err.Error()})
			return
		}
		h.pushState()

	default:
		c.sendJSON(serverMessage{Type: "error", Error: "unknown message type"})
	}
}

// ------------ Client ------------

type client struct {
	playerID string
	isHost   bool
	h        *hub
	conn     *websocket.Conn
	send     chan []byte
	log      *log.Logger
}

func (c *client) readPump() {
	defer func() {
		c.h.unregister <- c
		_ = c.conn.Close()
	}()

	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			c.log.Printf("read error: %v", err)
			return
		}

		var msg clientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			c.sendJSON(serverMessage{Type: "error", Error: "bad json"})
			continue
		}
		c.h.handleClientMessage(c, msg)
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(20 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *client) sendJSON(v any) {
	b, _ := json.Marshal(v)
	select {
	case c.send <- b:
	default:
		// drop if buffer full
	}
}

// =====================================================
// ================= Message Schema ====================
// =====================================================

type clientMessage struct {
	Type     string `json:"type"` // "join" | "start_hand" | "act"
	PlayerID string `json:"playerId,omitempty"`
	Action   string `json:"action,omitempty"` // "FOLD"|"CHECK"|"CALL"|"RAISE"
	Amount   *int   `json:"amount,omitempty"` // raise size
}

type serverMessage struct {
	Type  string        `json:"type"`            // "state" | "error"
	Error string        `json:"error,omitempty"` //
	State *statePayload `json:"state,omitempty"`
}

type statePayload struct {
	Table    *game.Table `json:"table"`
	Pot      int         `json:"pot"`
	Phase    string      `json:"phase"`
	Board    []game.Card `json:"board"`
	ToActIdx int         `json:"toActIdx"` // -1 if hidden
}

// =====================================================
// ================ Bootstrapping ======================
// =====================================================

// Convenience helper to start an HTTP server that Cloud Run can use.
// (Not used in your simple main.go, but useful for Cloud Run.)
func StartHTTPServer(ctx context.Context, s *Server) error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("invalid PORT: %v", err)
	}

	mux := http.NewServeMux()
	s.ServeHTTP(mux)

	// Basic server
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("listening on :%s ...", port)
	return srv.ListenAndServe()
}
