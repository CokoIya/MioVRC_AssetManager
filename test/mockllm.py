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
def plan(n_results, last_user, last_tool=""):
    if "删" in last_user:
        return [("我找一下删除用的操作。", [("unity_find_skills", {"intent": "delete asset"})]),
                ("", [("unity_skill", {"name": "asset_delete", "args": {"assetPath": "Assets/Old"}})]),
                ("处理完了。", [])][min(n_results, 2)]
    if "穿到头像上" not in last_user and "装配到模型上" not in last_user:
        return ("好的，已经记下：" + last_user[:30], [])
    steps = [
     ("先看看头像现在的样子。", [("inspect_avatar", {})]),
     ("", [("list_prefabs", {"folders": ["Assets/KDress"]})]),
     ("这是给 Kaguya 的版本，三个配色是同一件衣服。穿上「Envy cat」，其余做成换色。", [("dress", {"prefab": P + "Envy cat.prefab"}), ("inspect_object", {"path": "Envy cat"})]),
     ("", [("build_menu", MENU)]),
     ("路径写错了一处，改正后重试。", [("build_menu", FIXED)]),
     ("装好了，拍两张照确认一下。", [("look", {"views": ["front", "back"], "question": "水手服有没有穿模或错位"})]),
     ("做好了：\n· 穿上了「恶魔水手服」（嫉妒猫配色，默认穿着），融化恶魔做成了换色项。\n· 菜单在 主菜单 > 衣服 > 恶魔水手服：嫉妒猫、融化恶魔、外套、鞋子。\n\n去 Unity 的 Play 模式里用 Gesture Manager 点一遍；满意就按 Ctrl+S 保存，不满意按 Ctrl+Z。", []),
    ]
    done = [0, 1, 2, 4, 5, 6, 7]  # tool results seen so far → which step is next
    text, calls = steps[done.index(n_results)] if n_results in done else steps[-1]
    if not calls:
        if "视觉模型（" in last_tool: text += "\n\n截图我自己看不了，视觉模型的描述是：整体正常，左肩有一小块身体穿出来——建议你放大截图确认一下。"
        elif "截图附在后面" in last_tool: text += "\n\n我看了正面和背面的截图：水手服位置正常，没有洋红色；左肩有一小块身体穿出来。"
        elif "没有看到画面" in last_tool: text += "\n\n这次我看不了图：截图在上面的记录里，请你自己看一眼。想让我也能检查画面，可以在「AI 服务」的「识图」里设置一个视觉模型。"
    return text, calls
def read_digits(raw):
    import base64, io, re
    from PIL import Image
    m = re.search(r'base64,([A-Za-z0-9+/=]+)', raw)
    im = Image.open(io.BytesIO(base64.b64decode(m.group(1)))).convert("L")
    FONT = [[0x0E,0x11,0x13,0x15,0x19,0x11,0x0E],[0x04,0x0C,0x04,0x04,0x04,0x04,0x0E],[0x0E,0x11,0x01,0x02,0x04,0x08,0x1F],[0x1F,0x02,0x04,0x02,0x01,0x11,0x0E],[0x02,0x06,0x0A,0x12,0x1F,0x02,0x02],
            [0x1F,0x10,0x1E,0x01,0x01,0x11,0x0E],[0x06,0x08,0x10,0x1E,0x11,0x11,0x0E],[0x1F,0x01,0x02,0x04,0x08,0x08,0x08],[0x0E,0x11,0x11,0x0E,0x11,0x11,0x0E],[0x0E,0x11,0x11,0x0F,0x01,0x02,0x0C]]
    out = ""
    for i in range(3):
        rows = [sum((0x10 >> x) for x in range(5) if im.getpixel((22 + i * 84 + x * 14 + 7, 22 + y * 14 + 7)) < 128) for y in range(7)]
        out += str(FONT.index(rows)) if rows in FONT else "?"
    return out
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
        raw = json.dumps(body, ensure_ascii=False)
        pics = raw.count('"image_url"') // 2 + raw.count('"media_type"')
        if body.get("model") == "mio-vision":
            if not pics: return self.send(200, {"choices": [{"message": {"role": "assistant", "content": "看不到"}, "finish_reason": "stop"}]})
            if "三位数" in raw:
                return self.send(200, {"choices": [{"message": {"role": "assistant", "content": read_digits(raw)}, "finish_reason": "stop"}]})
            time.sleep(0.8)
            return self.send(200, {"choices": [{"message": {"role": "assistant", "content": "- 整体：完整的人形，A 字站姿。\n- 衣服：水手服在身上，左肩有一小块身体穿出来。\n- 材质：没有洋红色。"}, "finish_reason": "stop"}]})
        if pics: return self.send(400, {"error": {"message": "Failed to deserialize the JSON body into the target type: messages[1]: unknown variant `image_url`, expected `text`"}})
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
            n, user, tool = 0, "", ""
            for m in msgs:
                if m["role"] == "tool": n += 1; tool = m["content"]
                if m["role"] == "user" and isinstance(m["content"], str): user, n = m["content"], 0
            text, calls = plan(n, user, tool)
            msg = {"role": "assistant", "content": text}
            if calls: msg["tool_calls"] = [{"id": "call_%d_%d" % (n, i), "type": "function", "function": {"name": c[0], "arguments": json.dumps(c[1], ensure_ascii=False)}} for i, c in enumerate(calls)]
            return self.send(200, {"choices": [{"message": msg, "finish_reason": "tool_calls" if calls else "stop"}]})
        self.send(404, {"error": {"message": "not found"}})
ThreadingHTTPServer(('127.0.0.1', 47992), H).serve_forever()
