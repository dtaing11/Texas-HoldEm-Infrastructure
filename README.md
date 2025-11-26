# Texas Hold’em Engine

A real-time, host-authoritative Texas Hold’em Poker engine built with Go and WebSockets.
The engine manages dealing, betting rounds, blinds, pots, hand evaluation, and player state synchronization.

## Features

- Real-time WebSocket multiplayer
- Host-authoritative poker logic
- PREFLOP → FLOP → TURN → RIVER → SHOWDOWN phases
- Side pot handling and all-ins
- Deterministic card dealing and hand evaluation
- JSON WebSocket protocol
- Client-friendly for Python / JS / Flutter

## Architecture

/game
    engine.go        # Core poker logic
    model.go         # Player, Table, Deck, Actions
    handeval.go      # Hand strength evaluator

/ws
    hub.go           # WebSocket hub
    handlers.go      # Message routing

/cmd
    server.go        # Server entrypoint

## Game Flow

### 1. WAITING
Players connect and join.

### 2. PREFLOP
Blinds posted, 2 cards dealt, first betting.

### 3. FLOP
Burn one, deal 3 community cards.

### 4. TURN
Burn one, deal 4th community card.

### 5. RIVER
Burn one, deal 5th community card.

### 6. SHOWDOWN
Best hand(s) win the pot, new hand begins.

## WebSocket Protocol

### Connect

ws://localhost:8080/ws?apiKey=dev&table=table-1&player=p1

### Client → Server

Join:
{ "type": "join" }

Action:
{ "type": "action", "action": "call" }

Raise:
{
  "type": "action",
  "action": "raise",
  "amount": 120
}

### Server → Client

State:
{
  "type": "state",
  "hand": 2,
  "phase": "TURN",
  "pot": 540
}

Error:
{
  "type": "error",
  "message": "Not your turn"
}

## Python Player Client

import asyncio
import json
import websockets

API_KEY = "dev"
TABLE = "table-1"
PLAYER = "p1"

async def player():
    url = f"ws://localhost:8080/ws?apiKey={API_KEY}&table={TABLE}&player={PLAYER}"
    async with websockets.connect(url) as ws:
        await ws.send(json.dumps({ "type": "join" }))
        print(f"Joined as {PLAYER}")
        while True:
            msg = await ws.recv()
            print(msg)

asyncio.run(player())


## Running the Server

go build -o poker-server ./cmd
./poker-server
