package connection

import "github.com/dtaing11/Texas-HoldEm-Infrastructure/game"

type StatePayload struct {
	Type  string       `json:"type"`
	State *PublicState `json:"state"`
}

type PublicState struct {
	Table    *game.Table `json:"table"`
	Pot      int         `json:"pot"`
	Phase    game.Phase  `json:"phase"`
	Board    []game.Card `json:"board"`
	ToCall   int         `json:"ToCall"`
	ToActIdx int         `json:"toActIdx"`
	Hand     int         `json:"hand"`
}

func (tb *tableBinding) buildPublicStateFor(c *Client) *PublicState {
	tableCopy := *tb.Table
	playersCopy := make([]*game.Player, len(tb.Table.Players))

	for i, p := range tb.Table.Players {
		if p == nil {
			continue
		}
		cp := *p
		if !c.isHost && cp.ID != c.playerID {
			cp.Cards = [2]game.Card{}
		}
		playersCopy[i] = &cp
	}

	tableCopy.Players = playersCopy

	return &PublicState{
		Table:    &tableCopy,
		Pot:      tb.Engine.Pot,
		Phase:    tb.Table.Phase,
		Board:    tb.Table.CardOpen,
		ToActIdx: tb.Engine.ToActIndex(),
		ToCall:   tb.Engine.MinRaise,
		Hand:     tb.handCounter,
	}
}
