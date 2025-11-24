// internal/gameloop/engine.go
package game

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

var (
	ErrNotPlayersTurn  = errors.New("not this player's turn")
	ErrInvalidAction   = errors.New("invalid action for current state")
	ErrHandNotRunning  = errors.New("no active hand (phase is WAITING)")
	ErrAlreadyActed    = errors.New("player already folded or all-in")
	ErrNoSuchPlayer    = errors.New("player not at the table")
	ErrNoActivePlayers = errors.New("need at least 2 players with chips to start")
	ErrRaiseTooSmall   = errors.New("raise below minimum")
)

// ---------- Public engine types ----------

type Engine struct {
	Table      *Table
	DealerBtn  int // index into Table.Players
	SmallBlind int
	BigBlind   int
	MinRaise   int // dynamic; resets each street to BB (or last raise size)
	Pot        int

	toActIdx          int            // index into Table.Players
	roundBets         map[string]int // chips put in this betting round (street)
	totalContrib      map[string]int // chips contributed this hand (for side pots)
	highestThisStreet int            // amount to call on this street
	lastAggressorIdx  int            // index into Players; used to detect round close
	openAction        bool           // has anyone bet/raised this street
	evaluator         Evaluator      // hand ranking
}

// Evaluator offers a 7-card hand rank comparison. Higher is better.
type Evaluator interface {
	Rank7(hole [2]Card, board []Card) HandRank
}

// HandRank is a comparable numeric rank (bigger is stronger).
type HandRank uint64

// ---------- Construction ----------

func NewEngine(t *Table, sb, bb int) *Engine {
	return &Engine{
		Table:        t,
		SmallBlind:   sb,
		BigBlind:     bb,
		MinRaise:     bb,
		roundBets:    make(map[string]int),
		totalContrib: make(map[string]int),
		evaluator:    &SimpleEvaluator{}, // replace with a stronger one if desired
	}
}

func (e *Engine) SetEvaluator(ev Evaluator) {
	e.evaluator = ev
}

// ---------- Deck / dealing utilities ----------

var ranks = []string{"2", "3", "4", "5", "6", "7", "8", "9", "T", "J", "Q", "K", "A"}
var suits = []Suit{HEART, DIAMOND, CLUB, SPADE}

func newDeck() []Card {
	d := make([]Card, 0, 52)
	for _, s := range suits {
		for _, r := range ranks {
			d = append(d, Card{Rank: r, Suit: s})
		}
	}
	return d
}

func shuffle(deck []Card) {
	// Fisher–Yates with crypto/rand
	for i := len(deck) - 1; i > 0; i-- {
		nBig, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := int(nBig.Int64())
		deck[i], deck[j] = deck[j], deck[i]
	}
}

// pops top n cards from CardStack (top = end of slice)
func (e *Engine) draw(n int) []Card {
	if len(e.Table.CardStack) < n {
		return nil
	}
	top := e.Table.CardStack[len(e.Table.CardStack)-n:]
	e.Table.CardStack = e.Table.CardStack[:len(e.Table.CardStack)-n]
	return top
}

func (e *Engine) burn() { _ = e.draw(1) }

// ---------- Seating helpers ----------

func (e *Engine) nextIdx(i int) int {
	n := len(e.Table.Players)
	for step := 1; step <= n; step++ {
		j := (i + step) % n
		p := e.Table.Players[j]
		if p == nil {
			continue
		}
		if p.playerState != FOLDED && p.Chips >= 0 {
			return j
		}
	}
	return i
}

func (e *Engine) activeAndUnfolded() []*Player {
	out := []*Player{}
	for _, p := range e.Table.Players {
		if p == nil {
			continue
		}
		if p.playerState != FOLDED && (p.Chips > 0 || e.totalContrib[p.ID] > 0) {
			out = append(out, p)
		}
	}
	return out
}

func (e *Engine) stillContesting() []*Player {
	out := []*Player{}
	for _, p := range e.Table.Players {
		if p == nil {
			continue
		}
		if p.playerState != FOLDED && (p.playerState == INHAND || p.playerState == ALLIN) {
			out = append(out, p)
		}
	}
	return out
}

func (e *Engine) leftOf(idx int) int { return e.nextIdx(idx) }

// ---------- Hand lifecycle ----------

func (e *Engine) ToActIndex() int {
	if e.Table == nil {
		return -1
	}
	if e.toActIdx < 0 || e.toActIdx >= len(e.Table.Players) {
		return -1
	}
	return e.toActIdx
}

func (e *Engine) StartHand() error {
	// Need at least 2 players with chips
	count := 0
	for _, p := range e.Table.Players {
		if p != nil && p.Chips > 0 {
			count++
		}
	}
	if count < 2 {
		return ErrNoActivePlayers
	}

	// rotate dealer button
	if e.DealerBtn < 0 || e.DealerBtn >= len(e.Table.Players) {
		e.DealerBtn = 0
	} else {
		e.DealerBtn = e.leftOf(e.DealerBtn)
	}

	// reset table/engine
	e.Table.ResetHand()
	e.Pot = 0
	e.roundBets = map[string]int{}
	e.totalContrib = map[string]int{}
	e.highestThisStreet = 0
	e.openAction = false
	e.lastAggressorIdx = -1
	e.MinRaise = e.BigBlind

	// fresh deck
	e.Table.CardStack = newDeck()
	shuffle(e.Table.CardStack)

	// deal two cards to each player in seat order, starting left of dealer
	start := e.leftOf(e.DealerBtn)
	// two passes
	for pass := 0; pass < 2; pass++ {
		i := start
		for loop := 0; loop < len(e.Table.Players); loop++ {
			p := e.Table.Players[i]
			if p != nil && p.Chips > 0 {
				card := e.draw(1)
				if card == nil {
					panic("deck underflow")
				}
				p.Cards[pass] = card[0]
				p.playerState = INHAND
			}
			i = e.leftOf(i)
		}
	}

	// blinds
	sbIdx := e.leftOf(e.DealerBtn)
	bbIdx := e.leftOf(sbIdx)
	e.postBlind(sbIdx, e.SmallBlind)
	e.postBlind(bbIdx, e.BigBlind)

	// first to act: left of BB
	e.toActIdx = e.leftOf(bbIdx)

	return nil
}

func (e *Engine) postBlind(idx int, amount int) {
	p := e.Table.Players[idx]
	if p == nil || p.playerState == FOLDED {
		return
	}
	put := min(amount, p.Chips)
	p.Chips -= put
	e.roundBets[p.ID] += put
	e.totalContrib[p.ID] += put
	e.Pot += put
	if put < amount {
		p.playerState = ALLIN
	}
	if amount > e.highestThisStreet {
		e.highestThisStreet = amount
	}
}

// ---------- Acting ----------

type ActRequest struct {
	PlayerID string
	Action   Action
	Amount   int // for RAISE only: total bet increment over current to-call (i.e., raise size)
}

// Act applies a player's action. On street completion, advances phase automatically.
func (e *Engine) Act(req ActRequest) error {
	if e.Table.Phase == WAITING {
		return ErrHandNotRunning
	}
	pi := e.findPlayerIdx(req.PlayerID)
	if pi < 0 {
		return ErrNoSuchPlayer
	}
	if pi != e.toActIdx {
		return ErrNotPlayersTurn
	}
	p := e.Table.Players[pi]
	if p.playerState == FOLDED || p.playerState == ALLIN {
		return ErrAlreadyActed
	}

	toCall := e.highestThisStreet - e.roundBets[p.ID]
	switch req.Action {
	case FOLD:
		p.playerState = FOLDED
		// next player
		e.toActIdx = e.findNextToActAfterFold(pi)
	case CHECK:
		if toCall != 0 {
			return ErrInvalidAction
		}
		e.toActIdx = e.advanceOrClose(pi, false)
	case CALL:
		if toCall <= 0 {
			// treat as check
			e.toActIdx = e.advanceOrClose(pi, false)
			break
		}
		callAmt := min(toCall, p.Chips)
		p.Chips -= callAmt
		e.roundBets[p.ID] += callAmt
		e.totalContrib[p.ID] += callAmt
		e.Pot += callAmt
		if callAmt < toCall {
			p.playerState = ALLIN
		}
		e.toActIdx = e.advanceOrClose(pi, false)
	case RAISE:
		// No-limit raise: player must first call, then raise by >= MinRaise (unless all-in smaller, which is allowed but doesn't reset MinRaise)
		minRaise := e.MinRaise
		raiseSize := req.Amount
		if raiseSize < 0 {
			return ErrInvalidAction
		}
		totalNeeded := toCall + raiseSize
		if p.Chips < totalNeeded {
			// allow all-in raise smaller than min; it will NOT reopen action
			if p.Chips <= toCall {
				// it's effectively a CALL (all-in for call)
				callAmt := p.Chips
				p.Chips = 0
				e.roundBets[p.ID] += callAmt
				e.totalContrib[p.ID] += callAmt
				e.Pot += callAmt
				p.playerState = ALLIN
				e.toActIdx = e.advanceOrClose(pi, false)
				break
			}
			// partial raise: allowed but does not reset MinRaise or last aggressor
			callAmt := toCall
			extra := p.Chips - callAmt
			p.Chips = 0
			e.roundBets[p.ID] += callAmt + extra
			e.totalContrib[p.ID] += callAmt + extra
			e.Pot += callAmt + extra
			if e.roundBets[p.ID] > e.highestThisStreet {
				e.highestThisStreet = e.roundBets[p.ID]
			}
			p.playerState = ALLIN
			e.toActIdx = e.advanceOrClose(pi, false)
			break
		}
		// must be a legal raise
		if raiseSize < minRaise {
			return ErrRaiseTooSmall
		}

		commit := totalNeeded
		p.Chips -= commit
		e.roundBets[p.ID] += commit
		e.totalContrib[p.ID] += commit
		e.Pot += commit
		if e.roundBets[p.ID] > e.highestThisStreet {
			e.highestThisStreet = e.roundBets[p.ID]
		}

		// successful raise resets MinRaise to raiseSize and sets last aggressor
		e.MinRaise = raiseSize
		e.openAction = true
		e.lastAggressorIdx = pi

		e.toActIdx = e.nextIdx(pi)
	default:
		return ErrInvalidAction
	}

	// If betting round naturally closes, advance street (and maybe showdown)
	if e.bettingRoundComplete() {
		if err := e.advanceStreet(); err != nil {
			return err
		}
	}
	return nil
}

// determine next to act after a fold: if only one left, end immediately
func (e *Engine) findNextToActAfterFold(pi int) int {
	if len(e.contenders()) == 1 {
		// award pot immediately
		e.awardWhenOnlyOne()
		return pi
	}
	return e.advanceOrClose(pi, false)
}

func (e *Engine) contenders() []*Player {
	out := []*Player{}
	for _, p := range e.Table.Players {
		if p != nil && p.playerState != FOLDED {
			out = append(out, p)
		}
	}
	return out
}

// advanceOrClose advances turn pointer or triggers close if we just matched last aggressor
func (e *Engine) advanceOrClose(prevIdx int, forceClose bool) int {
	if forceClose {
		return prevIdx
	}
	nxt := e.nextIdx(prevIdx)
	return nxt
}

// bettingRoundComplete checks if everyone has acted such that no more action is possible:
//   - All remaining players are either folded or all-in, or
//   - Every non-all-in player has matched the highest bet (checks/calls) and there's no pending action.
func (e *Engine) bettingRoundComplete() bool {
	alive := 0
	allinOrFold := 0
	for _, p := range e.Table.Players {
		if p == nil {
			continue
		}
		if p.playerState == FOLDED {
			allinOrFold++
			continue
		}
		alive++
		if p.playerState == ALLIN {
			allinOrFold++
			continue
		}
	}
	// Everyone is folded or all-in ⇒ round ends
	if alive == allinOrFold {
		return true
	}
	// No open action and everyone matched (all checks)
	if !e.openAction {
		for _, p := range e.Table.Players {
			if p == nil || p.playerState != INHAND {
				continue
			}
			if e.roundBets[p.ID] != e.highestThisStreet {
				return false
			}
		}
		return true
	}
	// Open action exists: close when all non-all-in have matched
	for _, p := range e.Table.Players {
		if p == nil || p.playerState != INHAND {
			continue
		}
		if e.roundBets[p.ID] != e.highestThisStreet {
			return false
		}
	}
	return true
}

// ---------- Street transitions ----------

func (e *Engine) resetStreet() {
	e.roundBets = map[string]int{}
	e.highestThisStreet = 0
	e.openAction = false
	e.MinRaise = e.BigBlind
	e.lastAggressorIdx = -1
}

func (e *Engine) advanceStreet() error {
	// if only one player remains, award pot
	if len(e.contenders()) == 1 {
		e.awardWhenOnlyOne()
		return nil
	}

	switch e.Table.Phase {
	case PREFLOP:
		// flop: burn one, deal 3
		e.burn()
		e.Table.CardOpen = append(e.Table.CardOpen, e.draw(3)...)
		e.Table.Phase = FLOP
		e.resetStreet()
		// first to act: left of dealer
		e.toActIdx = e.leftOf(e.DealerBtn)
	case FLOP:
		// turn: burn, deal 1
		e.burn()
		e.Table.CardOpen = append(e.Table.CardOpen, e.draw(1)...)
		e.Table.Phase = TURN
		e.resetStreet()
		e.toActIdx = e.leftOf(e.DealerBtn)
	case TURN:
		// river: burn, deal 1
		e.burn()
		e.Table.CardOpen = append(e.Table.CardOpen, e.draw(1)...)
		e.Table.Phase = RIVER
		e.resetStreet()
		e.toActIdx = e.leftOf(e.DealerBtn)
	case RIVER:
		// showdown
		e.Table.Phase = SHOWDOWN
		e.showdown()
		e.endHand()
	case SHOWDOWN, WAITING:
		// nothing
	default:
		return fmt.Errorf("unknown phase: %s", e.Table.Phase)
	}
	return nil
}

func (e *Engine) awardWhenOnlyOne() {
	for _, p := range e.Table.Players {
		if p != nil && p.playerState != FOLDED {
			p.Chips += e.Pot
			e.Pot = 0
			break
		}
	}
	e.endHand()
}

func (e *Engine) endHand() {
	e.Table.Phase = WAITING
}

// ---------- Showdown & side pots ----------

func (e *Engine) showdown() {
	// collect eligible players
	type contestant struct {
		idx   int
		p     *Player
		rank  HandRank
		inPot int // total contribution
	}
	cont := []contestant{}
	for i, p := range e.Table.Players {
		if p == nil || p.playerState == FOLDED {
			continue
		}
		r := HandRank(0)
		if e.evaluator != nil {
			r = e.evaluator.Rank7(p.Cards, e.Table.CardOpen)
		}
		cont = append(cont, contestant{idx: i, p: p, rank: r, inPot: e.totalContrib[p.ID]})
	}

	// Build side pots from contribution tiers
	type potLayer struct {
		capAmount int
		amount    int
		players   []int // indexes of contestants eligible for this layer
	}
	if len(cont) == 0 {
		return
	}
	// unique caps
	capsMap := map[int]struct{}{}
	for _, c := range cont {
		capsMap[c.inPot] = struct{}{}
	}
	caps := []int{}
	for v := range capsMap {
		if v > 0 {
			caps = append(caps, v)
		}
	}
	if len(caps) == 0 {
		return
	}
	sort.Ints(caps)

	layers := []potLayer{}
	prev := 0
	for _, capAmt := range caps {
		layer := potLayer{capAmount: capAmt}
		// amount: sum over all players' min(contrib, capAmt) - prev
		total := 0
		for _, p := range e.Table.Players {
			if p == nil {
				continue
			}
			contrib := e.totalContrib[p.ID]
			if contrib <= 0 {
				continue
			}
			total += max(0, min(contrib, capAmt)-prev)
		}
		layer.amount = total
		// eligible players: those not folded with contrib >= capAmt
		for _, c := range cont {
			if c.inPot >= capAmt {
				layer.players = append(layer.players, c.idx)
			}
		}
		layers = append(layers, layer)
		prev = capAmt
	}

	// Award each layer to best hand among eligibles (split on ties)
	for _, L := range layers {
		if L.amount == 0 || len(L.players) == 0 {
			continue
		}
		// find top rank among eligibles
		best := HandRank(0)
		winners := []int{}
		for _, idx := range L.players {
			// find contestant rank
			var rank HandRank
			for _, c := range cont {
				if c.idx == idx {
					rank = c.rank
					break
				}
			}
			if rank > best {
				best = rank
				winners = []int{idx}
			} else if rank == best {
				winners = append(winners, idx)
			}
		}
		share := L.amount / len(winners)
		rem := L.amount % len(winners)
		for i, idx := range winners {
			e.Table.Players[idx].Chips += share
			if i < rem {
				e.Table.Players[idx].Chips++
			}
			e.Pot -= share
		}
		e.Pot -= rem
	}
	if e.Pot < 0 {
		e.Pot = 0
	}
}

// ---------- Query helpers ----------

func (e *Engine) findPlayerIdx(id string) int {
	for i, p := range e.Table.Players {
		if p != nil && p.ID == id {
			return i
		}
	}
	return -1
}

// ---------- Simple hand evaluator ----------

type SimpleEvaluator struct{}

const (
	cHigh = iota
	cPair
	cTwoPair
	cTrips
	cStraight
	cFlush
	cFullHouse
	cQuads
	cStraightFlush
)

var rankVal = map[string]int{
	"2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "8": 8, "9": 9,
	"10": 10, "J": 11, "Q": 12, "K": 13, "A": 14,
}

func (s *SimpleEvaluator) Rank7(hole [2]Card, board []Card) HandRank {
	all := make([]Card, 0, 7)
	all = append(all, hole[0], hole[1])
	all = append(all, board...)

	// build counts
	byRank := map[int]int{}
	bySuit := map[Suit][]int{} // store ranks per suit
	distinct := []int{}
	for _, c := range all {
		r := rankVal[c.Rank]
		byRank[r]++
		if _, ok := bySuit[c.Suit]; !ok {
			bySuit[c.Suit] = []int{}
		}
		bySuit[c.Suit] = append(bySuit[c.Suit], r)
	}
	for r := range byRank {
		distinct = append(distinct, r)
	}
	sort.Ints(distinct)

	// check flush (also helps straight flush)
	var flushRanks []int
	for _, arr := range bySuit {
		if len(arr) >= 5 {
			flushRanks = append([]int{}, arr...)
			sort.Ints(flushRanks)
			break
		}
	}

	// straight helper (on any ranks slice, sorted ascending, dedup)
	isStraight := func(vals []int) (bool, int) {
		if len(vals) < 5 {
			return false, 0
		}
		v := dedupAsc(vals)
		// wheel: A can be low (count as 1)
		hasA := contains(v, 14)
		if hasA {
			v = append([]int{1}, v...)
		}
		best := 0
		run := 1
		for i := 1; i < len(v); i++ {
			if v[i] == v[i-1]+1 {
				run++
				if run >= 5 {
					best = v[i]
				}
			} else if v[i] != v[i-1] {
				run = 1
			}
		}
		if best > 0 {
			return true, best
		}
		return false, 0
	}

	// Straight flush?
	if len(flushRanks) >= 5 {
		if ok, top := isStraight(flushRanks); ok {
			return makeRank(cStraightFlush, []int{top})
		}
	}

	// Quads / Trips / Pairs analysis
	type bucket struct{ rank, cnt int }
	buckets := []bucket{}
	for r, c := range byRank {
		buckets = append(buckets, bucket{r, c})
	}
	// sort by count desc, then rank desc
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].cnt != buckets[j].cnt {
			return buckets[i].cnt > buckets[j].cnt
		}
		return buckets[i].rank > buckets[j].rank
	})

	// Quads
	if buckets[0].cnt == 4 {
		quad := buckets[0].rank
		kicker := topKExcept(distinct, 1, quad)[0]
		return makeRank(cQuads, []int{quad, kicker})
	}

	// Full house (3 + 2)
	if buckets[0].cnt == 3 {
		trip := buckets[0].rank
		// look for the best pair among the rest (or another trips acts as pair)
		pair := 0
		for i := 1; i < len(buckets); i++ {
			if buckets[i].cnt >= 2 {
				pair = max(pair, buckets[i].rank)
			}
		}
		if pair > 0 {
			return makeRank(cFullHouse, []int{trip, pair})
		}
	}

	// Flush
	if len(flushRanks) >= 5 {
		sort.Sort(sort.Reverse(sort.IntSlice(flushRanks)))
		return makeRank(cFlush, flushRanks[:5])
	}

	// Straight
	if ok, top := isStraight(distinct); ok {
		return makeRank(cStraight, []int{top})
	}

	// Trips
	if buckets[0].cnt == 3 {
		trip := buckets[0].rank
		kicks := topKExcept(distinct, 2, trip)
		return makeRank(cTrips, append([]int{trip}, kicks...))
	}

	// Two Pair / One Pair
	if buckets[0].cnt == 2 {
		firstPair := buckets[0].rank
		secondPair := 0
		for i := 1; i < len(buckets); i++ {
			if buckets[i].cnt == 2 {
				secondPair = buckets[i].rank
				break
			}
		}
		if secondPair > 0 {
			hi, lo := firstPair, secondPair
			if lo > hi {
				hi, lo = lo, hi
			}
			kicker := topKExcept(distinct, 1, hi, lo)[0]
			return makeRank(cTwoPair, []int{hi, lo, kicker})
		}
		// One pair
		kicks := topKExcept(distinct, 3, firstPair)
		return makeRank(cPair, append([]int{firstPair}, kicks...))
	}

	// High card
	sort.Sort(sort.Reverse(sort.IntSlice(distinct)))
	fill := distinct
	if len(fill) > 5 {
		fill = fill[:5]
	}
	return makeRank(cHigh, fill)
}

func dedupAsc(vals []int) []int {
	if len(vals) == 0 {
		return vals
	}
	sort.Ints(vals)
	out := []int{vals[0]}
	for _, v := range vals[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

func contains(v []int, x int) bool {
	for _, a := range v {
		if a == x {
			return true
		}
	}
	return false
}

func topKExcept(vals []int, k int, except ...int) []int {
	ex := map[int]struct{}{}
	for _, e := range except {
		ex[e] = struct{}{}
	}
	cands := []int{}
	for _, v := range vals {
		if _, ok := ex[v]; !ok {
			cands = append(cands, v)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(cands)))
	if len(cands) > k {
		return cands[:k]
	}
	return cands
}

// pack category + kickers into a 64b rank. Higher is better.
// Layout: [cat:4][k1:6][k2:6][k3:6][k4:6][k5:6][pad]
func makeRank(cat int, ks []int) HandRank {
	k := make([]int, 5)
	copy(k, ks)
	var out uint64
	out |= (uint64(cat) & 0xF) << 60
	shift := 54
	for i := 0; i < 5; i++ {
		out |= (uint64(k[i]) & 0x3F) << shift
		shift -= 6
	}
	return HandRank(out)
}

// ---------- Utility ----------

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------- Debug helpers (optional) ----------

func (e *Engine) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Phase: %s  Pot: %d  ToActIdx: %d\n", e.Table.Phase, e.Pot, e.toActIdx)
	fmt.Fprintf(&b, "Board: %v\n", e.Table.CardOpen)
	for i, p := range e.Table.Players {
		if p == nil {
			continue
		}
		fmt.Fprintf(&b, "Seat %d: %s chips=%d state=%s betThisStreet=%d contrib=%d cards=%v\n",
			i, p.ID, p.Chips, p.playerState, e.roundBets[p.ID], e.totalContrib[p.ID], p.Cards)
	}
	return b.String()
}
