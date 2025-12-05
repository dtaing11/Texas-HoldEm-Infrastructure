import json
import sys
import threading
import time
from websocket import WebSocketApp, WebSocketConnectionClosedException
import ssl  # 👈 make sure this is imported
from dotenv import load_dotenv
import os
load_dotenv()
BASEURL = os.getenv("BASEURL")
API_KEY = os.getenv("APIKEY")
TABLE_ID = os.getenv("TABLEID")
WS_URL_TEMPLATE = BASEURL + "/ws?apiKey={apiKey}&table={table}&player={player}"



def safe_card_str(c: dict) -> str:
    rank = c.get("rank", "") or ""
    suit = c.get("suit", "") or ""
    suit_char = suit[0] if isinstance(suit, str) and len(suit) > 0 else ""
    return f"{rank}{suit_char}"


class PlayerClient:
    def __init__(self, player_id: str):
        self.player_id = player_id
        self.ws: WebSocketApp | None = None
        self.my_seat = None
        self.to_act_idx = -1
        self.phase = "WAITING"
        self.lock = threading.Lock()

    # ---- websocket callbacks ----
    def on_open(self, ws):
        print(f"[{self.player_id}] Connected")
        ws.send(json.dumps({"type": "join"}))

    def on_error(self, ws, error):
        print(f"[{self.player_id}] error:", error)

    def on_close(self, ws, status_code, msg):
        print(f"[{self.player_id}] connection closed:", status_code, msg)

    def on_message(self, ws, message: str):
        try:
            data = json.loads(message)
            print(data)
        except json.JSONDecodeError:
            print(f"[{self.player_id}] non-JSON:", message)
            return

        if data.get("type") != "state":
            print(f"[{self.player_id}] msg:", data)
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
        my_seat = None
        for i, p in enumerate(players):
            if p is None:
                continue
            pid = p.get("id")
            chips = p.get("chips")
            cards = p.get("cards") or []
            cards_str = ""
            if cards:
                cards_str = " " + " ".join(safe_card_str(c) for c in cards)
            if pid == self.player_id:
                my_seat = i
            pretty_players.append(f"   seat {i}: {pid}:{chips}{cards_str}")

        with self.lock:
            self.my_seat = my_seat
            self.to_act_idx = to_act_idx
            self.phase = phase

        print(f"\n=== STATE for {self.player_id} (hand #{hand_no}) ===")
        print(f"Phase: {phase}  Pot: {pot}")
        if board:
            print("Board:", "[" + " ".join(safe_card_str(c) for c in board) + "]")
        else:
            print("Board: []")
        print("Players:")
        for line in pretty_players:
            print(line)
        print(f"ToActIdx: {to_act_idx}   (your seat: {my_seat})")

        # show commands always, to make debugging easier
        print(f"[{self.player_id}] Commands when it's your turn:")
        print("  c         -> check/call")
        print("  f         -> fold")
        print("  r <amt>   -> raise by <amt> (raise size, not total bet)")
        print("  q         -> quit client")
        # hint when it's really your turn
        if my_seat is not None and to_act_idx == my_seat and phase not in ("WAITING", "SHOWDOWN"):
            print(f"[{self.player_id}] 💡 It's your turn!")

    # ---- send action ----
    def send_action(self, action: str, amount: int = 0):
        if not self.ws or not self.ws.sock or not self.ws.sock.connected:
            print(f"[{self.player_id}] cannot send, not connected")
            return
        msg = {
            "type": "act",
            "action": action,
            "amount": amount,
        }
        try:
            self.ws.send(json.dumps(msg))
        except WebSocketConnectionClosedException as e:
            print(f"[{self.player_id}] send failed:", e)

    # ---- threads ----
    def run_ws(self):
        url =  WS_URL_TEMPLATE.format(
            apiKey=API_KEY,
            table=TABLE_ID,
            player=self.player_id
        )
        print(f"[{self.player_id}] Connecting to {url}")

        self.ws = WebSocketApp(
            url,
            on_open=self.on_open,
            on_close=self.on_close,
            on_error=self.on_error,
            on_message=self.on_message,
        )

        # DEV ONLY: disable certificate verification so wss connect works
        sslopt = {"cert_reqs": ssl.CERT_NONE}

        self.ws.run_forever(sslopt=sslopt)

    def run_input_loop(self):
        # simple input loop; you can type even if it's not your turn, we gate it
        while True:
            try:
                cmd = input(f"[{self.player_id}] your move (c/f/r <amt>/q): ").strip()
            except EOFError:
                break

            if cmd == "":
                continue
            if cmd.lower() == "q":
                print(f"[{self.player_id}] quitting client…")
                if self.ws:
                    self.ws.close()
                break

            with self.lock:
                my_seat = self.my_seat
                to_act_idx = self.to_act_idx
                phase = self.phase

            if phase in ("WAITING", "SHOWDOWN") or my_seat is None or to_act_idx != my_seat:
                print(f"[{self.player_id}] Not your turn right now.")
                continue

            parts = cmd.split()
            code = parts[0].lower()
            amt = 0
            if len(parts) >= 2:
                try:
                    amt = int(parts[1])
                except ValueError:
                    print(f"[{self.player_id}] invalid amount, using 0")
                    amt = 0

            if code == "c":
                self.send_action("CALL", 0)
            elif code == "f":
                self.send_action("FOLD", 0)
            elif code == "r":
                self.send_action("RAISE", amt)
            else:
                print(f"[{self.player_id}] unknown command, use c/f/r or q")

        print(f"[{self.player_id}] client closed")


def main():
    if len(sys.argv) < 2:
        print("Usage: python3 player_cli.py <playerId>")
        sys.exit(1)

    player_id = sys.argv[1]
    client = PlayerClient(player_id)


    t_ws = threading.Thread(target=client.run_ws, daemon=True)
    t_ws.start()

    # give ws a moment to connect
    time.sleep(1.0)

    client.run_input_loop()


if __name__ == "__main__":
    main()
