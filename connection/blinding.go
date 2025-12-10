package connection

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
)

type tableBinding struct {
	Table  *game.Table
	Engine *game.Engine

	mu          sync.Mutex
	clients     map[*Client]struct{}
	host        *Client
	handCounter int
}

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

	count := 0
	for _, p := range tb.Table.Players {
		if p != nil && p.Chips > 0 {
			count++
		}
	}
	return count >= 2
}

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

func (tb *tableBinding) broadcastState() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.broadcastStateLocked()
}
