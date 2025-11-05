// internal/gameloop/model.go
package game

// ---------- Phases ----------
type Phase string

const (
	WAITING  Phase = "WAITING"
	PREFLOP  Phase = "PREFLOP"
	FLOP     Phase = "FLOP"
	TURN     Phase = "TURN"
	RIVER    Phase = "RIVER"
	SHOWDOWN Phase = "SHOWDOWN"
)

// ---------- Actions ----------
type Action string

const (
	CHECK Action = "CHECK"
	CALL  Action = "CALL"
	RAISE Action = "RAISE"
	FOLD  Action = "FOLD"
)

// ---------- Card / Suits ----------
type Suit string

const (
	HEART   Suit = "HEART"
	DIAMOND Suit = "DIAMOND"
	CLUB    Suit = "CLUB"
	SPADE   Suit = "SPADE"
)

type Card struct {
	Rank string `json:"rank"`
	Suit Suit   `json:"suit"`
}

// ---------- Player ----------
type PlayerState string

const (
	INHAND PlayerState = "INHAND"
	FOLDED PlayerState = "FOLDED"
	ALLIN  PlayerState = "ALLIN"
)

type Player struct {
	ID          string  `json:"id"`
	Chips       int     `json:"chips"`
	Action      Action  `json:"action"`
	Cards       [2]Card `json:"cards"`
	playerState PlayerState
}

// ---------- Table ----------
type Table struct {
	ID        string    `json:"id"`
	Players   []*Player `json:"players"` // pointers for in-place updates
	Phase     Phase     `json:"phase"`
	CardStack []Card    `json:"cardStack"` // face-down deck
	CardOpen  []Card    `json:"cardOpen"`  // community cards
}

// ---------- Helpers (optional but handy) ----------
func NewTable(id string) *Table {
	return &Table{
		ID:        id,
		Players:   make([]*Player, 0),
		Phase:     WAITING,
		CardStack: make([]Card, 0),
		CardOpen:  make([]Card, 0, 5),
	}
}

func (t *Table) AddPlayer(p *Player) {
	t.Players = append(t.Players, p)
}

func (t *Table) ResetHand() {
	t.Phase = PREFLOP
	t.CardOpen = t.CardOpen[:0]
	// keep CardStack management in your dealer/shuffler code
	for _, p := range t.Players {
		p.Action = ""
		p.Cards = [2]Card{}
	}
}
