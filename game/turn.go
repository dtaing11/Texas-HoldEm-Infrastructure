package game

// normalizeToActIdx ensures toActIdx either points to a player who can act
// (INHAND with chips) or is set to -1 if nobody can act.
func (e *Engine) normalizeToActIdx() {
	if e.Table == nil {
		e.toActIdx = -1
		return
	}

	// No one can act when the hand isn't running.
	if e.Table.Phase == WAITING || e.Table.Phase == SHOWDOWN {
		e.toActIdx = -1
		return
	}

	// See if there is anyone who can actually act.
	hasActable := false
	for _, p := range e.Table.Players {
		if p != nil && p.playerState == INHAND && p.Chips > 0 {
			hasActable = true
			break
		}
	}
	if !hasActable {
		// Everyone is either all-in or folded ⇒ no one to act.
		e.toActIdx = -1
		return
	}

	// If toActIdx is out of range, pick the first actable player left of dealer.
	if e.toActIdx < 0 || e.toActIdx >= len(e.Table.Players) {
		e.toActIdx = e.nextIdx(e.DealerBtn)
		return
	}

	// If current toActIdx points at someone who cannot act, move to the next.
	p := e.Table.Players[e.toActIdx]
	if p == nil || p.playerState != INHAND || p.Chips <= 0 {
		e.toActIdx = e.nextIdx(e.toActIdx)
	}
}

// CanPlayerAct returns true if this player is the one to act and the hand is active.
// It also normalizes toActIdx so it never points at an ALLIN/FOLDED/busted seat.
func (e *Engine) CanPlayerAct(playerID string) bool {
	if e.Table == nil {
		return false
	}

	// Keep toActIdx sane before checking.
	e.normalizeToActIdx()

	if e.Table.Phase == WAITING || e.Table.Phase == SHOWDOWN {
		return false
	}

	// find seat
	idx := e.findPlayerIdx(playerID)
	if idx < 0 {
		return false
	}

	// If normalizeToActIdx found no actable players, toActIdx will be -1.
	if e.toActIdx < 0 || e.toActIdx >= len(e.Table.Players) {
		return false
	}

	// From the server's POV, the only rule we enforce is "is it your turn?"
	// The engine's Act() still handles folds/all-ins/etc.
	return idx == e.toActIdx
}

// nextIdx is for ACTION TURN ORDER within a hand.
// Only players currently INHAND (not folded, not all-in) will get a turn.
func (e *Engine) nextIdx(i int) int {
	n := len(e.Table.Players)
	if n == 0 {
		return -1
	}
	for step := 1; step <= n; step++ {
		j := (i + step) % n
		p := e.Table.Players[j]
		if p == nil {
			continue
		}
		if p.playerState == INHAND && p.Chips > 0 {
			return j
		}
	}
	return i
}

func (e *Engine) ToActIndex() int {
	if e.Table == nil {
		return -1
	}

	e.normalizeToActIdx()

	if e.toActIdx < 0 || e.toActIdx >= len(e.Table.Players) {
		return -1
	}
	return e.toActIdx
}

func (e *Engine) leftOf(idx int) int { return e.nextIdx(idx) }
