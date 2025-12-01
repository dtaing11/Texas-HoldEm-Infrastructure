# player2.py
import json
import threading
import time
from websocket import WebSocketApp

WS_URL = "ws://localhost:8080/ws"
API_KEY = "dev"
TABLE_ID = "table-1"
PLAYER_ID = "p2"   # only difference from player1.py

last_state = None
state_lock = threading.Lock()
running = True


def card_str(card):
    if not card:
        return "??"
    rank = card.get("rank", "")
    suit = card.get("suit", "")
    s_map = {
        "HEART": "♥",
        "DIAMOND": "♦",
        "CLUB": "♣",
        "SPADE": "♠",
    }
    return f"{rank}{s_map.get(suit, '?')}"


def on_message(ws, message):
    global last_state
    data = json.loads(message)
    if data.get("type") != "state":
        print(f"[{PLAYER_ID}] ←", data)
        return

    state = data["state"]
    with state_lock:
        last_state = state

    phase = state["phase"]
    pot = state["pot"]
    board = [card_str(c) for c in state.get("board", [])]
    hand_no = state.get("hand", 0)
    min_raise = state.get("minRaise")
    sb = state.get("smallBlind")
    bb = state.get("bigBlind")

    print(f"\n[{PLAYER_ID} STATE]")
    print(f"  Hand #{hand_no} Phase={phase} Pot={pot}")
    print(f"  Blinds: SB={sb} BB={bb} MinRaise={min_raise}")
    print(f"  Board: {' '.join(board) if board else '(empty)'}")

    players = state["table"]["players"]
    my_idx = None
    for i, p in enumerate(players):
        if p is None:
            continue
        pid = p["id"]
        chips = p["chips"]
        if pid == PLAYER_ID:
            my_idx = i
            my_cards = [card_str(c) for c in p.get("cards", [])]
            print(f"  You: {pid} chips={chips} cards={' '.join(my_cards)}")
        else:
            print(f"  Opp: {pid} chips={chips}")

    to_act = state["toActIdx"]
    if phase != "WAITING" and my_idx is not None and to_act == my_idx:
        print(f"\n[{PLAYER_ID}] 👉 It is your turn!")
        print("  Options:")
        print("    c  -> CHECK/CALL")
        print("    r  -> RAISE")
        print("    f  -> FOLD")


def on_error(ws, error):
    print(f"[{PLAYER_ID}] error:", error)


def on_close(ws, close_status_code, close_msg):
    global running
    print(f"[{PLAYER_ID}] connection closed")
    running = False


def on_open(ws):
    print(f"[{PLAYER_ID}] ✅ Connected")
    join_msg = {"type": "join"}
    ws.send(json.dumps(join_msg))


def input_thread(ws):
    global running
    while running:
        time.sleep(0.2)
        with state_lock:
            state = last_state

        if state is None:
            continue

        phase = state["phase"]
        if phase == "WAITING":
            continue

        players = state["table"]["players"]
        my_idx = None
        my_player = None
        for i, p in enumerate(players):
            if p is None:
                continue
            if p["id"] == PLAYER_ID:
                my_idx = i
                my_player = p
                break

        if my_idx is None:
            continue

        if state["toActIdx"] != my_idx:
            continue  # not our turn

        try:
            chips = my_player["chips"]
        except Exception:
            chips = None

        min_raise = state.get("minRaise", 0)

        print(f"\n[{PLAYER_ID}] Your turn. You have {chips} chips.")
        print("  Enter action: (c=check/call, r=raise, f=fold)")
        action_in = input(f"[{PLAYER_ID}] > ").strip().lower()

        if action_in == "f":
            msg = {"type": "act", "action": "FOLD"}
            ws.send(json.dumps(msg))
        elif action_in == "c":
            msg = {"type": "act", "action": "CALL"}
            ws.send(json.dumps(msg))
        elif action_in == "r":
            while True:
                val = input(f"[{PLAYER_ID}] Raise size (>= {min_raise}, or 0 to cancel): ").strip()
                if not val:
                    continue
                try:
                    amt = int(val)
                except ValueError:
                    print("  Not a number.")
                    continue
                if amt == 0:
                    print("  Raise cancelled.")
                    break
                if amt < min_raise:
                    print(f"  Raise must be >= {min_raise}")
                    continue
                msg = {
                    "type": "act",
                    "action": "RAISE",
                    "amount": amt,
                }
                ws.send(json.dumps(msg))
                break
        else:
            print("  Unknown action, defaulting to CALL.")
            msg = {"type": "act", "action": "CALL"}
            ws.send(json.dumps(msg))


def main():
    params = f"?apiKey={API_KEY}&table={TABLE_ID}&player={PLAYER_ID}"
    url = WS_URL + params

    ws = WebSocketApp(
        url,
        on_open=on_open,
        on_message=on_message,
        on_error=on_error,
        on_close=on_close,
    )

    t = threading.Thread(target=input_thread, args=(ws,), daemon=True)
    t.start()

    ws.run_forever()
    time.sleep(0.2)


if __name__ == "__main__":
    main()
