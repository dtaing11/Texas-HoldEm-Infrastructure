import json
import sys
import ssl
from websocket import WebSocketApp
from dotenv import load_dotenv
import os

# ---- env + URL ----

load_dotenv()
BASEURL = os.getenv("BASEURL")
API_KEY = os.getenv("APIKEY")
TABLE_ID = os.getenv("TABLEID")

if not BASEURL:
    raise RuntimeError("BASEURL environment variable is not set")

if not API_KEY:
    raise RuntimeError("API_KEY environment variable is not set")

if not TABLE_ID:
    raise RuntimeError("TABLE_ID environment variable is not set")

WS_URL_TEMPLATE = BASEURL + "/ss?apiKey={apiKey}&table={table}&player={player}"

player_id = sys.argv[1] if len(sys.argv) > 1 else "p1"
ws_url = WS_URL_TEMPLATE.format(apiKey=API_KEY, table=TABLE_ID, player=player_id)

# ---- websocket callbacks ----

def on_open(ws):
    print(f"[WS] Connected to {ws_url}")

def on_close(ws, code, reason):
    print(f"[WS] Closed: code={code}, reason={reason}")

def on_error(ws, error):
    print(f"[WS] Error: {error}")

def on_message(ws, message: str):
    # 🔥 RAW JSON FROM SERVER
    print("\n===== RAW WS MESSAGE =====")
    print(message)

    # optional: pretty-print parsed JSON if it is valid
    try:
        data = json.loads(message)
        print("----- Parsed JSON -----")
        print(json.dumps(data, indent=2))
    except json.JSONDecodeError:
        print("!! Not valid JSON, leaving as raw text")

# ---- run client ----

if __name__ == "__main__":
    ws = WebSocketApp(
        ws_url,
        on_open=on_open,
        on_message=on_message,
        on_error=on_error,
        on_close=on_close,
    )

    # if you're using wss://
    ws.run_forever(sslopt={"cert_reqs": ssl.CERT_NONE})
