package connection

import (
	"net/http"
	"sync"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
)

type Server struct {
	apiKey   string
	startKey string

	mu     sync.Mutex
	tables map[string]*tableBinding
}

func NewServer(apiKey, startKey string) *Server {
	return &Server{
		apiKey:   apiKey,
		startKey: startKey,
		tables:   make(map[string]*tableBinding),
	}
}

func (s *Server) RegisterTable(id string, t *game.Table, e *game.Engine) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tables[id] = &tableBinding{
		Table:   t,
		Engine:  e,
		clients: make(map[*Client]struct{}),
	}
}

func (s *Server) ServeHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/ws", s.handleWS)
}
