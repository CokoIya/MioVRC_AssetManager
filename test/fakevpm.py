# A VPM world on one port for the UI tests: the built-in repositories' listings, their zips, the template files.
import json, io, zipfile, hashlib, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 47994
BASE = "http://127.0.0.1:%d" % PORT
zips = {}
def pkg(name, version, deps=None, top="", unity="2022.3"):
    bio = io.BytesIO()
    with zipfile.ZipFile(bio, "w") as zf:
        zf.writestr(top + "package.json", json.dumps({"name": name, "version": version, "displayName": name}))
        zf.writestr(top + "Editor/x.cs", "// " + name)
    b = bio.getvalue(); path = "/dl/%s-%s.zip" % (name, version); zips[path] = b
    v = {"name": name, "version": version, "url": BASE + path, "zipSHA256": hashlib.sha256(b).hexdigest(), "unity": unity, "displayName": name}
    if deps: v["vpmDependencies"] = deps
    return v
def listing(id, name, pkgs):
    return {"name": name, "id": id, "author": "fake", "url": BASE + "/" + id + ".json", "packages": {k: {"versions": {v["version"]: v for v in vs}} for k, vs in pkgs.items()}}
L = {
 "com.vrchat.repos.official": listing("com.vrchat.repos.official", "official", {
    "com.vrchat.base": [pkg("com.vrchat.base", "3.10.5")], "com.vrchat.avatars": [pkg("com.vrchat.avatars", "3.10.5", {"com.vrchat.base": "3.10.5"})],
    "com.vrchat.core.vpm-resolver": [pkg("com.vrchat.core.vpm-resolver", "0.1.29")]}),
 "com.vrchat.repos.curated": listing("com.vrchat.repos.curated", "curated", {
    "vrchat.blackstartx.gesture-manager": [pkg("vrchat.blackstartx.gesture-manager", "3.9.9", {"com.vrchat.avatars": ">=3.10.4 < 3.11.X"}, "GestureManager-3.9.9/")],
    "lyuma.av3emulator": [pkg("lyuma.av3emulator", "3.4.13", {"com.vrchat.avatars": "^3.1.0"})]}),
 "dev.nadena.vpm": listing("dev.nadena.vpm", "bd_", {
    "nadena.dev.ndmf": [pkg("nadena.dev.ndmf", "1.14.8")], "nadena.dev.modular-avatar": [pkg("nadena.dev.modular-avatar", "1.18.7", {"nadena.dev.ndmf": ">=1.14.7 <2.0.0-a", "com.vrchat.avatars": ">=3.7.4"})]}),
 # 3.0.0 is made for a later Unity than new projects use: 2.3.4 is the one that goes in
 "io.github.lilxyzw.vpm": listing("io.github.lilxyzw.vpm", "lil", {"jp.lilxyzw.liltoon": [pkg("jp.lilxyzw.liltoon", "2.3.4"), pkg("jp.lilxyzw.liltoon", "3.0.0", unity="6000.0")]}),
 "com.anatawa12.vpm": listing("com.anatawa12.vpm", "anatawa12", {"com.anatawa12.avatar-optimizer": [pkg("com.anatawa12.avatar-optimizer", "1.9.20", {"nadena.dev.ndmf": ">=1.8.0 <2.0.0", "com.vrchat.avatars": ">=3.7.0 <3.11.0"})]}),
 "com.vrcfury.vcc": listing("com.vrcfury.vcc", "VRCFury", {"com.vrcfury.vrcfury": [pkg("com.vrcfury.vrcfury", "1.1430.0")]}),
}
TPL = {"ProjectSettings.asset": "%YAML 1.1\n%TAG !u! tag:unity3d.com,2011:\n--- !u!129 &1\nPlayerSettings:\n  m_ObjectHideFlags: 0\n  serializedVersion: 26\n  productName: World\n  m_ActiveColorSpace: 1\n",
       "TagManager.asset": "%YAML 1.1\nTagManager:\n  tags: []\n", "AudioManager.asset": "%YAML 1.1\nAudioManager: {}\n"}
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        p = self.path.split("?")[0]
        if p.startswith("/dl/") and p in zips: return self.send(zips[p], "application/zip")
        if p.endswith(".json") and p[1:-5] in L: return self.send(json.dumps(L[p[1:-5]]).encode(), "application/json")
        if p.startswith("/template/") and p[len("/template/"):] in TPL: return self.send(TPL[p[len("/template/"):]].encode(), "text/plain")
        self.send_response(404); self.end_headers()
    def send(self, b, ct):
        self.send_response(200); self.send_header("Content-Type", ct); self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
print("fake VPM on", PORT, flush=True)
ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()
