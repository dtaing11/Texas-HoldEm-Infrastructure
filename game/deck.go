package game

import (
	"crypto/rand"
	"math/big"
)

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

// pops top n cards from CardStack (top = end of slice)
func (e *Engine) draw(n int) []Card {
	if len(e.Table.CardStack) < n {
		return nil
	}
	top := e.Table.CardStack[len(e.Table.CardStack)-n:]
	e.Table.CardStack = e.Table.CardStack[:len(e.Table.CardStack)-n]
	return top
}

func shuffle(deck []Card) {
	// Fisher–Yates with crypto/rand
	for i := len(deck) - 1; i > 0; i-- {
		nBig, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := int(nBig.Int64())
		deck[i], deck[j] = deck[j], deck[i]
	}
}

func (e *Engine) burn() { _ = e.draw(1) }
