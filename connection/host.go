package connection

import "log"

func (c *Client) handleHostStart() {
	tb := c.binding

	tb.mu.Lock()
	defer tb.mu.Unlock()

	if !tb.canStartHandLocked() {
		return
	}

	if err := tb.Engine.StartHand(); err != nil {
		log.Printf("[HOST_LOGIC] StartHand error: %v", err)
		return
	}

	tb.broadcastStateLocked()
}

func (c *Client) handleHostReset() {
	tb := c.binding

	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.handCounter = 0
	tb.Engine.ResetGame(1000)

	if tb.canStartHandLocked() {
		if err := tb.Engine.StartHand(); err != nil {
			log.Printf("[HOST_LOGIC] start after reset error: %v", err)
		}
	}

	tb.broadcastStateLocked()
}
