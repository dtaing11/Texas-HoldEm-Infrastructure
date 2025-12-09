import json
import threading
import time
import sys
import ssl
import random

from websocket import WebSocketApp, WebSocketConnectionClosedException

# Same config style as host.py
WS_URL_TEMPLATE = (
    "ws://localhost:8080/ws"
    "?apiKey={apiKey}&table={table}&player={player}"
)

api_key = "dev"
table_id = "table-1"


class AutoPlayerClient:
    """
    Automatic player:
    - Connects as a normal player (no startKey).
    - On each state, if it's our turn and we haven't already acted
      for this (hand, phase, toActIdx), pick a random legal action.
    """

    def __init__(self, player_id: str):
        self.player_id = player_id
        self.ws: WebSocketApp | None = None

        # Remember the last state we acted on:
        # key = (hand_no, phase, toActIdx)
        self.last_act_key = None

    # ------------- WebSocket callbacks -------------

    def _on_open(self, ws: WebSocketApp):
        print(f"[BOT {self.player_id}] connected")
        join_msg = {"type": "join"}
        ws.send(json.dumps(join_msg))

    def _on_close(self, ws: WebSocketApp, code, msg):
        print(f"[BOT {self.player_id}] closed: {code} {msg!r}")

    def _on_error(self, ws: WebSocketApp, error):
        print(f"[BOT {self.player_id}] error:", error)

    def _on_message(self, ws: WebSocketApp, message: str):
        try:
            data = json.loads(message)
        except json.JSONDecodeError:
            print(f"[BOT {self.player_id}] non-JSON:", message)
            return

        if data.get("type") != "state":
            return

        state = data["state"]
        table = state["table"]
        phase = state["phase"]
        to_act_idx = state["toActIdx"]
        hand_no = state.get("hand")

        # Hand not running
        if phase in ("WAITING", "SHOWDOWN"):
            return

        players = table.get("players") or []
        my_idx = None
        my_stack = None

        for i, p in enumerate(players):
            if p is None:
                continue
            if p.get("id") == self.player_id:
                my_idx = i
                my_stack = p.get("chips", 0)
                break

        if my_idx is None:
            # not seated yet
            return

        # Not our turn
        if my_idx != to_act_idx:
            return

        if my_stack is None or my_stack <= 0:
            # busted, nothing to do
            return

        # ---- Only act once per (hand, phase, toActIdx) ----
        act_key = (hand_no, phase, to_act_idx)
        if act_key == self.last_act_key:
            # already acted in this state
            return
        self.last_act_key = act_key

        # Small jitter so bots don't all fire at same exact moment
        time.sleep(random.uniform(0.01, 0.1))

        # Decide an action (lowercase)
        action, amount = self._choose_action(state, my_stack)

        # Engine expects uppercase: FOLD/CALL/RAISE/CHECK
        action_str = action.upper()

        msg = {"type": "act", "action": action_str}
        if action_str == "RAISE":
            msg["amount"] = amount

        try:
            ws.send(json.dumps(msg))
            if action_str == "RAISE":
                print(f"[BOT {self.player_id}] acted: {action_str} (amt={amount})")
            else:
                print(f"[BOT {self.player_id}] acted: {action_str}")
        except WebSocketConnectionClosedException as e:
            print(f"[BOT {self.player_id}] send failed:", e)

    # ------------- Simple legal random strategy -------------

    def _choose_action(self, state: dict, my_stack: int):
        """
        Random strategy with only legal moves:

        - We NEVER send 'check' directly (CALL becomes a check if toCall == 0).
        - We only RAISE with amount >= MinRaise (sent as 'ToCall' in your state).
        - Distribution:
          * short stack: mostly CALL or FOLD
          * normal stack: mix of FOLD / CALL / RAISE
        """
        # In your server, PublicState.ToCall == Engine.MinRaise
        min_raise = state.get("ToCall", 0) or 0
        base = max(min_raise, 1)

        # If stack is tiny, be conservative
        if my_stack <= base:
            # 80% call, 20% fold
            if random.random() < 0.2:
                return "fold", 0
            return "call", 0

        # Normal stack
        r = random.random()
        if r < 0.2:
            # 20% fold
            return "fold", 0
        elif r < 0.75:
            # 55% call
            return "call", 0
        else:
            # 25% raise, but only if meaningful
            # raise between 1x and 4x "base" while <= ~1/3 of stack
            max_mult = min(4, max(1, my_stack // (3 * base)))
            if max_mult <= 0:
                # not enough chips to raise sensibly
                return "call", 0

            mult = random.randint(1, max_mult)
            raise_size = base * mult
            # ensure >= MinRaise
            raise_size = max(base, raise_size)
            return "raise", raise_size

    # ------------- Public API -------------

    def start(self):
        # Simple reconnect loop so bots come back if server restarts
        while True:
            url = WS_URL_TEMPLATE.format(
                apiKey=api_key,
                table=table_id,
                player=self.player_id,
            )

            self.ws = WebSocketApp(
                url,
                on_open=self._on_open,
                on_message=self._on_message,
                on_error=self._on_error,
                on_close=self._on_close,
            )

            sslopt = {"cert_reqs": ssl.CERT_NONE}
            try:
                self.ws.run_forever(sslopt=sslopt)
            except Exception as e:
                print(f"[BOT {self.player_id}] run_forever error:", e)

            print(f"[BOT {self.player_id}] disconnected, retrying in 2s...")
            time.sleep(2)


def main():
    # Number of bots from CLI, default 10
    if len(sys.argv) > 1:
        try:
            num_bots = int(sys.argv[1])
        except ValueError:
            print("Usage: python auto_players.py [num_bots]")
            return
    else:
        num_bots = 10

    print(f"[BOTS] starting {num_bots} auto players on table={table_id}")

    threads = []

    for i in range(num_bots):
        pid = f"bot-{i+1}"
        bot = AutoPlayerClient(pid)

        t = threading.Thread(target=bot.start, daemon=True)
        t.start()
        threads.append(t)

        # slight stagger so they don't all hit at once
        time.sleep(0.1)

    print("[BOTS] all bots started. Ctrl+C to stop.")

    try:
        while True:
            time.sleep(1.0)
    except KeyboardInterrupt:
        print("\n[BOTS] shutting down…")
        # daemon threads exit with process


if __name__ == "__main__":
    main()
