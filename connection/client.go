package connection

import (
	"log"

	"github.com/dtaing11/Texas-HoldEm-Infrastructure/game"
	"github.com/gorilla/websocket"
)

type Client struct {
	conn     *websocket.Conn
	server   *Server
	binding  *tableBinding
	playerID string
	isHost   bool
	send     chan []byte
}

type inboundMessage struct {
	Type   string      `json:"type"`
	Player string      `json:"player,omitempty"`
	Action game.Action `json:"action,omitempty"`
	Amount int         `json:"amount,omitempty"`
}

func (c *Client) readLoop() {
	defer c.close()

	for {
		var msg inboundMessage
		if err := c.conn.ReadJSON(&msg); err != nil {
			log.Printf("[ws] read error (player=%s): %v", c.playerID, err)
			return
		}

		switch msg.Type {
		case "act":
			if !c.isHost {
				c.handlePlayerAct(msg)
			}

		case "host_start":
			if c.isHost {
				c.handleHostStart()
			}

		case "host_reset":
			if c.isHost {
				c.handleHostReset()
			}

		default:
			log.Printf("[ws] unknown message type: %s", msg.Type)
		}
	}
}

func (c *Client) writeLoop() {
	defer c.close()

	for data := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[ws] write error: %v", err)
			return
		}
	}
}

func (c *Client) close() {
	defer func() { recover() }()

	c.binding.mu.Lock()
	delete(c.binding.clients, c)
	if c.binding.host == c {
		c.binding.host = nil
	}
	c.binding.mu.Unlock()

	_ = c.conn.Close()
	close(c.send)
}

func (c *Client) handlePlayerAct(msg inboundMessage) {
	tb := c.binding

	tb.mu.Lock()
	defer tb.mu.Unlock()

	log.Printf("[ws] handlePlayerAct: player=%s action=%v amount=%d",
		c.playerID, msg.Action, msg.Amount)

	if !tb.Engine.CanPlayerAct(c.playerID) {
		tb.broadcastStateLocked()
		return
	}

	prev := tb.Table.Phase
	err := tb.Engine.Act(game.ActRequest{
		PlayerID: c.playerID,
		Action:   msg.Action,
		Amount:   msg.Amount,
	})

	if err != nil {
		tb.broadcastStateLocked()
		return
	}

	if prev != game.WAITING && tb.Table.Phase == game.WAITING {
		tb.handCounter++
		if tb.canStartHandLocked() {
			if err := tb.Engine.StartHand(); err != nil {
				log.Printf("[HOST_LOGIC] StartHand error: %v", err)
			}
		}
	}

	tb.broadcastStateLocked()
}
