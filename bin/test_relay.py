#!/usr/bin/env python3
"""IMAgent Relay E2E Test"""
import asyncio, json
import websockets

RELAY = "ws://8.153.192.3:8099/mcp"

async def rpc(ws, method, params=None, msg_id=1):
    req = {"jsonrpc": "2.0", "id": msg_id, "method": method}
    if params:
        req["params"] = params
    await ws.send(json.dumps(req))
    resp = await asyncio.wait_for(ws.recv(), timeout=10)
    return json.loads(resp)

async def test():
    print(f"📡 Connecting to {RELAY}...")
    ws = await websockets.connect(RELAY)
    print("✅ Connected")

    # 1. Initialize
    r = await rpc(ws, "initialize")
    assert "imagent-relay" in str(r), f"Init failed: {r}"
    print(f"✅ Initialize: {r['result']['serverInfo']['name']} v{r['result']['serverInfo']['version']}")

    # 2. List tools
    r = await rpc(ws, "tools/list")
    tools = [t["name"] for t in r["result"]["tools"]]
    print(f"✅ Tools: {', '.join(tools)}")

    # 3. Connect & get code
    r = await rpc(ws, "tools/call", {"name": "voice_connect", "arguments": {}})
    content = r["result"]["content"][0]["text"]
    print(f"✅ {content}")

    # Extract code
    import re
    match = re.search(r"Pairing code: (\w+)", content)
    code = match.group(1) if match else None
    print(f"   Pairing code: {code}")

    # 4. Check status
    r = await rpc(ws, "tools/call", {"name": "voice_status", "arguments": {}})
    status = r["result"]["content"][0]["text"]
    print(f"✅ Status: {status}")

    # 5. Send chat
    r = await rpc(ws, "tools/call", {"name": "voice_chat", "arguments": {"content": "Hello from E2E test!"}})
    print(f"✅ Chat sent: {r['result']['content'][0]['text']}")

    # 6. Send speak
    r = await rpc(ws, "tools/call", {"name": "voice_speak", "arguments": {"text": "测试语音消息"}})
    print(f"✅ Speak sent: {r['result']['content'][0]['text']}")

    # 7. Reset
    r = await rpc(ws, "tools/call", {"name": "voice_reset", "arguments": {}})
    print(f"✅ Reset: {r['result']['content'][0]['text']}")

    await ws.close()
    print("\n🎉 All tests passed!")

asyncio.run(test())
