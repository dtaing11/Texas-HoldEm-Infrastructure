import json
import threading
import time
import sys
import ssl
import select
from threading import Event, Lock

from dotenv import load_dotenv
from websocket import WebSocketApp, WebSocketConnectionClosedException
import matplotlib.pyplot as plt

# If you're using .env, uncomment:
##load_dotenv()

WS_URL_TEMPLATE = (
    "wss://texasholdem-871757115753.northamerica-northeast1.run.app"
    "/ws?apiKey={apiKey}&table={table}&player={player}&startKey={startKey}"
)

# WS_URL_TEMPLATE = (
#     "ws://localhost:8080/ws"
#     "?apiKey={apiKey}&table={table}&player={player}&startKey={startKey}"
# )

api_key = "dev"
table_id = "table-1"
start_key = "supersecret"

# --------- Shared state for plotting ---------
chip_history = {}  # {player_id: {"hands": [...], "chips": [...]} }
history_lock = Lock()
last_logged_hand = -1  # to only record once per hand


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


def update_chip_history(players, hand_no, phase):
    """
    Record each player's chip stack once per hand.

    We still store history, but for the bar chart we mainly use latest chips.
    """
    global last_logged_hand

    # If no hand number, or we're between hands, skip
    if hand_no is None or phase == "WAITING":
        return

    with history_lock:
        # Only log once per distinct hand number
        if hand_no == last_logged_hand:
            return
        last_logged_hand = hand_no

        for p in players:
            if p is None:
                continue
            pid = p.get("id")
            chips = p.get("chips", 0)
            if pid is None:
                continue

            series = chip_history.setdefault(pid, {"hands": [], "chips": []})
            series["hands"].append(hand_no)
            series["chips"].append(chips)


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
    hand_no = state.get("hand")  # may be None

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

    print("\n=== HOST STATE (hand #{}) ===".format(hand_no if hand_no is not None else "?"))
    print("Phase:", phase, " Pot:", pot)
    print("Board:", format_board(board))
    print("Players:")
    for s in pretty_players:
        print("  ", s)
    print("ToActIdx:", to_act_idx)

    # ---- Update chip history for plotting ----
    update_chip_history(players, hand_no, phase)


def on_error(ws, error):
    print("[HOST] error:", error)


def on_close(ws, close_status_code, close_msg):
    print("[HOST] connection closed:", close_status_code, close_msg)


def on_open(ws):
    print(f"[HOST] Connected as {ws.player_id} (god mode)")
    join_msg = {"type": "join"}
    ws.send(json.dumps(join_msg))


def run_ws(ws, stop_event: Event):
    # DEV ONLY: disable certificate verification for wss
    sslopt = {"cert_reqs": ssl.CERT_NONE}
    ws.run_forever(sslopt=sslopt)
    stop_event.set()  # if WS loop exits, signal shutdown


def main():
    host_id = "host-1"

    url = WS_URL_TEMPLATE.format(
        apiKey=api_key,
        table=table_id,
        player=host_id,
        startKey=start_key,
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

    stop_event = Event()

    # WebSocket in its own thread
    t_ws = threading.Thread(target=run_ws, args=(ws, stop_event), daemon=True)
    t_ws.start()

    # --- Matplotlib setup on MAIN THREAD ---
    plt.ion()
    fig, ax = plt.subplots()

    manager = getattr(fig, "canvas", None)
    manager = getattr(manager, "manager", None)
    if manager is not None:
        try:
            manager.set_window_title("Chip Stacks – Live Bar Chart")
        except Exception:
            pass

    print("Host commands:")
    print("  s  -> start next hand")
    print("  r  -> reset game (everyone 1000, hand 0, disconnect players)")
    print("  q  -> quit host")

    try:
        # Main loop: handle CLI + update plot
        while not stop_event.is_set():
            # 1) Non-blocking stdin check using select
            rlist, _, _ = select.select([sys.stdin], [], [], 0.1)
            if sys.stdin in rlist:
                cmd = sys.stdin.readline()
                if cmd == "":
                    # EOF on stdin
                    print("[HOST] stdin closed, exiting…")
                    stop_event.set()
                    break

                cmd = cmd.strip()
                if cmd == "":
                    pass
                elif cmd.lower() == "q":
                    print("[HOST] quitting…")
                    stop_event.set()
                    break
                elif not ws.sock or not ws.sock.connected:
                    print("[HOST] error: Connection to remote host was lost.")
                    stop_event.set()
                    break
                elif cmd.lower() == "s":
                    msg = {"type": "host_start"}
                    try:
                        ws.send(json.dumps(msg))
                    except WebSocketConnectionClosedException as e:
                        print("[HOST] send failed:", e)
                        stop_event.set()
                        break
                elif cmd.lower() == "r":
                    msg = {"type": "host_reset"}
                    try:
                        ws.send(json.dumps(msg))
                    except WebSocketConnectionClosedException as e:
                        print("[HOST] send failed:", e)
                        stop_event.set()
                        break
                else:
                    print("[HOST] unknown command. Use:")
                    print("  s")
                    print("  r")
                    print("  q")

            # 2) Update plot – BAR GRAPH of current chips
            with history_lock:
                ax.clear()

                # Only keep active players (last chips > 0)
                active = {}
                for pid, series in chip_history.items():
                    chips = series["chips"]
                    if chips and chips[-1] > 0:
                        active[pid] = chips[-1]  # keep only latest chip value

                if active:
                    names = list(active.keys())
                    values = [active[pid] for pid in names]
                    x_positions = range(len(names))

                    ax.bar(x_positions, values)
                    ax.set_xticks(list(x_positions))
                    ax.set_xticklabels(names, rotation=45, ha="right")

                    ax.set_xlabel("Player")
                    ax.set_ylabel("Chips")
                    ax.set_title("Current chip stacks (active players)")

                    # Zoom y-axis around current values
                    ymin = 0
                    ymax = max(values)
                    padding = max(10, int(0.1 * ymax)) if ymax > 0 else 10
                    ax.set_ylim(ymin, ymax + padding)
                else:
                    ax.set_title("Waiting for active players…")

            fig.canvas.draw()
            fig.canvas.flush_events()
            plt.pause(0.05)

    except KeyboardInterrupt:
        print("\n[HOST] interrupted by user")
        stop_event.set()
    finally:
        stop_event.set()
        try:
            ws.close()
        except Exception:
            pass
        try:
            plt.ioff()
            plt.close(fig)
        except Exception:
            pass
        time.sleep(0.2)
        print("[HOST] shutdown complete")


if __name__ == "__main__":
    main()
