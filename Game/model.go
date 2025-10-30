// internal/gameloop/model.go
package game

// ---------- Phases ----------
type Phase string

const (
	Waiting  Phase = "WAITING"
	Preflop  Phase = "PREFLOP"
	Flop     Phase = "FLOP"
	Turn     Phase = "TURN"
	River    Phase = "RIVER"
	Showdown Phase = "SHOWDOWN"
)

// ---------- Cards ----------
type Suit rune

const (
	Spade Suit = '♠'
	Heart Suit = '♥'
	Diam  Suit = '♦'
	Club  Suit = '♣'
)

type Card struct {
	Rank string // "A","K","Q","J","10","9"...,"2","?"
	Suit Suit
}

// ---------- Players ----------
type Player struct {
	ID     string
	Stack  int  // remaining chips
	In     bool // seated
	Folded bool
}

// ---------- Table ----------
type TableState struct {
	ID         string
	Phase      Phase
	Pot        int
	Board      []Card
	Order      []string // seat order (player IDs)
	ToActIdx   int      // index into Order (active only)
	Players    map[string]*Player
	Hands      map[string][2]Card // dealt hole cards
	MaxPlayers int                // cap per table (e.g., 100)

	StartStack int            // initial stack per join (e.g., 5000)
	CurrentBet int            // highest committed amount this round
	Bets       map[string]int // per-player committed this round
	MinPlayers int            // optional: gate START if < MinPlayers
}

// ---------- Actions ----------
type KindAction string

const (
	JOIN  KindAction = "JOIN"
	LEAVE KindAction = "LEAVE"
	FOLD  KindAction = "FOLD"
	CHECK KindAction = "CHECK"
	CALL  KindAction = "CALL"
	RAISE KindAction = "RAISE"
	START KindAction = "START"
)

type Action struct {
	PlayerID string
	Kind     KindAction
	Amount   int // for RAISE
}
