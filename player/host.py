import json
import threading
import time
import sys
from websocket import WebSocketApp, WebSocketConnectionClosedException

WS_URL_TEMPLATE = "ws://localhost:8080/ws?apiKey={apiKey}&table={table}&player={player}&startKey={startKey}"



def safe_card_str(c: dict) -> str:
    """Safely render a card like 'Ah' or '' if unknown."""
    if not isinstance(c, dict):
        return ""
    rank = c.get("rank", "") or ""
    suit = c.get("suit", "") or ""
    suit_char = suit[0] if isinstance(suit, str) and len(suit) > 0 else ""
    return f"{rank}{suit_char}"


def format_board(board):
    if not board:
        return "[]"
    return "[" + " ".join(safe_card_str(c) for c in board) + "]"


def on_message(ws, message):
    try:
        data = json.loads(message)
    except json.JSONDecodeError:
        print("[HOST] non-JSON message:", message)
        return

    if data.get("type") != "state":
        print("[HOST] message:", data)
        return

    state = data["state"]
    table = state["table"]
    pot = state["pot"]
    phase = state["phase"]
    board = state["board"]
    to_act_idx = state["toActIdx"]
    hand_no = state.get("hand", 0)

    players = table.get("players") or []
    pretty_players = []
    for i, p in enumerate(players):
        if p is None:
            continue
        pid = p.get("id")
        chips = p.get("chips")
        cards = p.get("cards") or []
        cards_str = ""
        if cards:
            cards_str = "  " + " ".join(safe_card_str(c) for c in cards)
        pretty_players.append(f"seat {i}: {pid}:{chips}{cards_str}")

    print("\n=== HOST STATE (hand #{}) ===".format(hand_no))
    print("Phase:", phase, " Pot:", pot)
    print("Board:", format_board(board))
    print("Players:")
    for s in pretty_players:
        print("  ", s)
    print("ToActIdx:", to_act_idx)


def on_error(ws, error):
    print("[HOST] error:", error)


def on_close(ws, close_status_code, close_msg):
    print("[HOST] connection closed:", close_status_code, close_msg)


def on_open(ws):
    print(f"[HOST] Connected as {ws.player_id} (god mode)")
    join_msg = {"type": "join"}
    ws.send(json.dumps(join_msg))


def run_ws(ws):
    ws.run_forever()


def main():
    host_id = "host-1"

    url = WS_URL_TEMPLATE.format(
        apiKey=api_key, table=table_id, player=host_id, startKey=start_key
    )
    print(f"[HOST] Connecting to {url}")

    ws = WebSocketApp(
        url,
        on_message=on_message,
        on_error=on_error,
        on_close=on_close,
        on_open=on_open,
    )
    ws.player_id = host_id

    t = threading.Thread(target=run_ws, args=(ws,), daemon=True)
    t.start()

    # give WebSocket a moment to connect & receive first state
    time.sleep(1.0)

    print("Host commands:")
    print("  s  -> start next hand")
    print("  q  -> quit host")

    try:
        while True:
            cmd = input("[HOST] command: ").strip()
            if cmd == "":
                continue

            if cmd.lower() == "q":
                print("[HOST] quitting…")
                break

            if not ws.sock or not ws.sock.connected:
                print("[HOST] error: Connection to remote host was lost.")
                break

            if cmd.lower() == "s":
                msg = {"type": "host_start"}
                try:
                    ws.send(json.dumps(msg))
                except WebSocketConnectionClosedException as e:
                    print("[HOST] send failed:", e)
                    break
            else:
                print("[HOST] unknown command. Use:")
                print("  s")
                print("  q")

    except KeyboardInterrupt:
        print("\n[HOST] interrupted by user")

    finally:
        try:
            ws.close()
        except Exception:
            pass
        time.sleep(0.5)
        print("[HOST] connection closed")


if __name__ == "__main__":
    main()
