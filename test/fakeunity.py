# Plays the Unity side of the AI assistant for the UI tests: heartbeat + answers in <project>/UserSettings/MioVRCA/bridge.
import json, os, sys, time, glob
proj = sys.argv[1]
d = os.path.join(proj, 'UserSettings', 'MioVRCA', 'bridge')
os.makedirs(d, exist_ok=True)
MESH = ["UV1_Choker", "UV1_Devil horn", "UV1_Devil tail", "UV1_Devil wing", "UV1_Sailor", "UV2_Loose socks", "UV2_Sandal", "UV2_Skirt", "UV4_Cardigan"]
def prefabs(folders=None):
    out = []
    for col in ["Envy cat", "Heart Catcher ", "Melty Devil "]:
        out.append({"path": "Assets/KDress/Kaguya/Kaguya prefab/%s.prefab" % col, "name": col, "modularAvatarReady": True, "renderers": 9, "meshNames": MESH, "hasArmature": True})
    out.append({"path": "Assets/KDress/Kaguya/Kaguya_full.prefab", "name": "Kaguya_full", "wholeAvatar": True, "renderers": 20, "meshNames": ["Body"], "hasArmature": True})
    out.append({"path": "Assets/IKUSIA/kaguya/kaguya.prefab", "name": "kaguya", "wholeAvatar": True, "renderers": 12, "meshNames": ["Body", "Hair"], "hasArmature": True})
    out.append({"path": "Assets/_头发/樱发/Khaki.prefab", "name": "Khaki", "modularAvatarReady": True, "renderers": 3, "meshNames": ["Hair", "hairpin1", "hairpin2"], "hasArmature": True})
    out.append({"path": "Assets/_道具/Bat/bat.prefab", "name": "bat", "renderers": 1, "meshNames": ["Bat"], "hasArmature": False})
    out.append({"path": "Assets/_配饰/Glasses/glasses.prefab", "name": "glasses", "renderers": 1, "meshNames": ["Glasses"], "hasArmature": False})
    if folders:
        low = [f.lower().rstrip('/') for f in folders]
        out = [x for x in out if any(x["path"].lower() == f or x["path"].lower().startswith(f + '/') for f in low)]
    return out
BRIDGE = os.environ.get("FAKE_BRIDGE", "1.2.1")
def doll(path, view, dressed):
    # a paper doll, so the pictures in the log look like something
    from PIL import Image, ImageDraw
    im = Image.new("RGB", (420, 768), (51, 54, 64)); d = ImageDraw.Draw(im)
    skin, hair = (244, 214, 196), (120, 88, 150) if view != "back" else (96, 70, 122)
    d.ellipse((150, 60, 270, 190), fill=skin); d.pieslice((140, 46, 280, 200), 180, 360, fill=hair)
    if view == "back": d.ellipse((146, 56, 274, 250), fill=hair)
    d.rounded_rectangle((165, 190, 255, 430), 30, fill=skin)
    d.rectangle((120, 205, 165, 225), fill=skin); d.rectangle((255, 205, 300, 225), fill=skin)
    d.rectangle((172, 420, 205, 720), fill=skin); d.rectangle((215, 420, 248, 720), fill=skin)
    if dressed:
        d.polygon([(160, 200), (260, 200), (290, 470), (130, 470)], fill=(40, 44, 78)); d.rectangle((160, 200, 260, 230), fill=(230, 230, 240))
        d.rectangle((168, 690, 208, 730), fill=(20, 20, 30)); d.rectangle((212, 690, 252, 730), fill=(20, 20, 30))
        if view != "back": d.polygon([(196, 228), (224, 228), (210, 262)], fill=(196, 48, 80))
    im.save(path, quality=88)
state = {"prefabOf": {}, "dressed": [], "menu": [], "log": [], "placed": os.environ.get("FAKE_EMPTY") != "1"}
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class SK(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def out(self, o):
        b = json.dumps(o).encode(); self.send_response(200); self.send_header('Content-Type', 'application/json'); self.send_header('Content-Length', str(len(b))); self.end_headers(); self.wfile.write(b)
    def do_GET(self):
        if self.path.startswith('/skills/recommend'):
            return self.out({"results": [{"name": "asset_delete", "description": "Delete an asset", "category": "Asset", "schema": {"parameters": [{"name": "assetPath", "type": "string", "required": True}], "riskLevel": "high", "readOnly": False}}]})
        if self.path.startswith('/skills'):
            return self.out({"skills": [{"name": "asset_delete", "operation": ["Delete"], "readOnly": False, "mutatesAssets": True, "riskLevel": "medium"},
                                        {"name": "gameobject_set_active", "operation": ["Modify"], "readOnly": False, "mutatesScene": True, "riskLevel": "low"}]})
        self.out({"status": "ok"})
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get('Content-Length') or 0)).decode()
        open(os.path.join(proj, 'skills_calls.log'), 'a').write(self.path + ' ' + body + '\n')
        if 'screenshot' in self.path:
            import base64, io
            from PIL import Image
            im = Image.new("RGB", (1280, 720), (70, 74, 90)); b = io.BytesIO(); im.save(b, "PNG")
            return self.out({"status": "success", "result": {"path": "Assets/Screenshots/shot.png", "imageWidth": 1280, "imageHeight": 720, "imageBytes": len(b.getvalue()), "imageBase64": base64.b64encode(b.getvalue()).decode()}})
        self.out({"status": "success", "result": {"deleted": json.loads(body or '{}').get("assetPath")}})
threading.Thread(target=lambda: ThreadingHTTPServer(('127.0.0.1', 47993), SK).serve_forever(), daemon=True).start()
def answer(cmd, a):
    state["log"].append(cmd)
    if cmd == "refresh": time.sleep(0.4); return {"refreshed": True}, ""
    if cmd == "inspect":
        if not state["placed"]: return {"modularAvatar": "1.18.3", "vrcSdk": True, "avatars": []}, ""
        kids = [{"name": "Body", "kind": "mesh", "active": True}, {"name": "Hair", "kind": "mesh", "active": True, "prefab": "Assets/IKUSIA/kaguya/kaguya.prefab"}] + [{"name": n, "kind": "outfit", "active": True, "prefab": state["prefabOf"].get(n, "")} for n in state["dressed"]]
        return {"modularAvatar": "1.18.3", "vrcSdk": True, "avatars": [{"name": "Kaguya_Test", "path": "Kaguya_Test", "active": True, "prefab": "Assets/IKUSIA/kaguya/kaguya.prefab", "children": kids, "maMenu": state["menu"]}]}, ""
    if cmd == "place_avatar":
        time.sleep(0.5); state["placed"] = True
        return {"scene": "Assets/fakeproj/fakeproj.unity", "avatar": "Kaguya_Test", "prefab": a["prefab"]}, ""
    if cmd == "prefabs":
        if any('Nope' in f for f in a.get("folders", [])): return {"prefabs": [], "notFound": a["folders"], "total": 0}, ""
        got = prefabs(a.get("folders"))
        return {"prefabs": got, "total": len(got)}, ""
    if cmd == "dress":
        time.sleep(0.5)
        name = os.path.basename(a["prefab"]).replace(".prefab", "").strip()
        if "full" in name: return None, "「Kaguya_full」是包含素体的完整模型，不是单独的衣服，请改用仅含衣服的 prefab"
        ex = name in state["dressed"]
        if not ex: state["dressed"].append(name)
        state.setdefault("prefabOf", {})[name] = a["prefab"]
        meshes = next((x["meshNames"] for x in prefabs() if x["path"] == a["prefab"]), MESH)
        return {"setUp": True, "existing": ex, "warnings": [], "outfit": {"object": name, "meshes": [{"path": name + "/" + m, "active": True, "height": [0, 1]} for m in meshes]}}, ""
    if cmd == "inspect_object":
        return {"object": a["path"], "meshes": [{"path": a["path"] + "/" + m, "active": True, "materials": ["mat"], "height": [0, 1], "triangles": 1000} for m in MESH]}, ""
    if cmd == "build_menu":
        time.sleep(0.6)
        for it in a.get("items", []):
            for o in it.get("objects", []):
                if "NOPE" in o: return None, "模型下未找到「%s」" % o
        open(os.path.join(proj, 'last_menu.json'), 'w', encoding='utf-8').write(json.dumps(a, ensure_ascii=False))
        return {"created": ["Avatar Menu"] + ["Avatar Menu/" + "/".join(it["path"] + [it["label"]]) for it in a["items"]], "warnings": ["「衣服」中有 9 项，超过每页 8 项的上限，游戏中将自动出现「More」翻页，可再分一层"] if len(a["items"]) > 12 else [], "icons": len(a["items"]), "root": a.get("root") or "Avatar Menu", "parameter": a.get("parameter") or "Clothtoggle", "menu": {}}, ""
    if cmd == "undo": return {"undone": "MioVRCA 生成菜单"}, ""
    if cmd == "snapshot" and BRIDGE >= "1.2.0":
        time.sleep(0.4)
        sd = os.path.join(proj, 'UserSettings', 'MioVRCA', 'shots'); os.makedirs(sd, exist_ok=True)
        shots = []
        for i, v in enumerate(a.get("views") or ["front"]):
            f = os.path.join(sd, 'f%d_%d_%s.jpg' % (len(state["log"]), i, v)); doll(f, v, state["dressed"])
            shots.append({"view": v, "file": f.replace(os.sep, '/'), "width": 420, "height": 768})
        return {"avatar": "Kaguya_Test", "shots": shots, "visible": ["Body", "Hair"] + state["dressed"], "hidden": a.get("hide") or [], "playing": False,
                "note": "编辑模式里的画面：是场景里现在显示着的东西，菜单开关的效果没有算进去"}, ""
    return None, "不认识的操作：" + cmd
while True:
    open(os.path.join(d, 'alive.tmp'), 'w').write(json.dumps({"bridge": BRIDGE, "pid": 1, "ma": "1.18.3", "sdk": True, "skills": {"installed": True, "running": True, "port": 47993, "version": "2.8.4", "mode": "Auto"}}))
    os.replace(os.path.join(d, 'alive.tmp'), os.path.join(d, 'alive.json'))
    for r in sorted(glob.glob(os.path.join(d, 'req_*.json'))):
        run = os.path.join(d, 'run_' + os.path.basename(r)[4:])
        try: os.replace(r, run); r = run; q = json.load(open(r, encoding='utf-8'))
        except Exception: continue
        res, err = answer(q["cmd"], q.get("args") or {})
        out = {"id": q["id"], "cmd": q["cmd"], "ok": not err, "result": res}
        if err: out["error"] = err
        open(os.path.join(d, 'res_%s.tmp' % q["id"]), 'w', encoding='utf-8').write(json.dumps(out, ensure_ascii=False))
        os.replace(os.path.join(d, 'res_%s.tmp' % q["id"]), os.path.join(d, 'res_%s.json' % q["id"]))
        os.remove(r)
    time.sleep(0.1)
