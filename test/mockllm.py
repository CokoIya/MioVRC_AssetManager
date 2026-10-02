# An OpenAI-compatible and Anthropic-compatible chat service for the UI tests (port 47992).
import json, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
KEY = "sk-good-key-1234"
P = "Assets/KDress/Kaguya/Kaguya prefab/"
MENU = {"items": [
 {"kind": "outfit", "path": ["衣服", "恶魔水手服"], "label": "嫉妒猫", "objects": ["Envy cat"], "default": True},
 {"kind": "outfit", "path": ["衣服", "恶魔水手服"], "label": "融化恶魔", "objects": ["Envy cat"], "materialsFrom": P + "Melty Devil .prefab"},
 {"kind": "part", "path": ["衣服", "恶魔水手服"], "label": "外套", "objects": ["Envy cat/UV4_Cardigan"]},
 {"kind": "part", "path": ["衣服", "恶魔水手服"], "label": "鞋子", "objects": ["Envy cat/NOPE"]}]}
FIXED = json.loads(json.dumps(MENU).replace("NOPE", "UV2_Sandal"))
def plan(n_results, last_user):
    if "删" in last_user:
        return [("我找一下删除用的操作。", [("unity_find_skills", {"intent": "delete asset"})]),
                ("", [("unity_skill", {"name": "asset_delete", "args": {"assetPath": "Assets/Old"}})]),
                ("处理完了。", [])][min(n_results, 2)]
    if "穿到头像上" not in last_user and "装到头像上" not in last_user:
        return ("好的，已经记下：" + last_user[:30], [])
    steps = [
     ("先看看头像现在的样子。", [("inspect_avatar", {})]),
     ("", [("list_prefabs", {"folders": ["Assets/KDress"]})]),
     ("这是给 Kaguya 的版本，三个配色是同一件衣服。穿上「Envy cat」，其余做成换色。", [("dress", {"prefab": P + "Envy cat.prefab"}), ("inspect_object", {"path": "Envy cat"})]),
     ("", [("build_menu", MENU)]),
     ("路径写错了一处，改正后重试。", [("build_menu", FIXED)]),
     ("做好了：\n· 穿上了「恶魔水手服」（嫉妒猫配色，默认穿着），融化恶魔做成了换色项。\n· 菜单在 主菜单 > 衣服 > 恶魔水手服：嫉妒猫、融化恶魔、外套、鞋子。\n\n去 Unity 的 Play 模式里用 Gesture Manager 点一遍；满意就按 Ctrl+S 保存，不满意按 Ctrl+Z。", []),
    ]
    done = [0, 1, 2, 4, 5, 6]  # tool results seen so far → which step is next
    return steps[done.index(n_results)] if n_results in done else steps[-1]
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, obj):
        b = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(code); self.send_header('Content-Type', 'application/json'); self.send_header('Content-Length', str(len(b))); self.end_headers(); self.wfile.write(b)
    def authed(self):
        return KEY in (self.headers.get('Authorization') or '') or self.headers.get('x-api-key') == KEY
    def do_GET(self):
        if not self.authed(): return self.send(401, {"error": {"message": "Incorrect API key provided"}})
        if self.path.startswith('/v1/models'): return self.send(200, {"data": [{"id": "mio-large"}, {"id": "mio-small"}, {"id": "mio-embed"}]})
        self.send(404, {"error": {"message": "not found"}})
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length') or 0)) or b'{}')
        if not self.authed(): return self.send(401, {"error": {"message": "Incorrect API key provided"}})
        if body.get("model") not in ("mio-large", "mio-small"): return self.send(404, {"error": {"message": "The model `%s` does not exist" % body.get("model")}})
        time.sleep(0.5)
        msgs = body.get("messages", [])
        if self.path == '/v1/messages':
            n, user = 0, ""
            for m in msgs:
                for bl in (m["content"] if isinstance(m["content"], list) else [{"type": "text", "text": m["content"]}]):
                    if bl.get("type") == "tool_result": n += 1
                    if m["role"] == "user" and bl.get("type") == "text": user, n = bl["text"], 0
            text, calls = plan(n, user)
            blocks = ([{"type": "text", "text": text}] if text else []) + [{"type": "tool_use", "id": "tu_%d_%d" % (n, i), "name": c[0], "input": c[1]} for i, c in enumerate(calls)]
            return self.send(200, {"role": "assistant", "content": blocks, "stop_reason": "tool_use" if calls else "end_turn"})
        if self.path == '/v1/chat/completions':
            n, user = 0, ""
            for m in msgs:
                if m["role"] == "tool": n += 1
                if m["role"] == "user": user, n = m["content"], 0
            text, calls = plan(n, user)
            msg = {"role": "assistant", "content": text}
            if calls: msg["tool_calls"] = [{"id": "call_%d_%d" % (n, i), "type": "function", "function": {"name": c[0], "arguments": json.dumps(c[1], ensure_ascii=False)}} for i, c in enumerate(calls)]
            return self.send(200, {"choices": [{"message": msg, "finish_reason": "tool_calls" if calls else "stop"}]})
        self.send(404, {"error": {"message": "not found"}})
ThreadingHTTPServer(('127.0.0.1', 47992), H).serve_forever()
