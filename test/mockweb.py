"""Mock of Baidu share pages, Booth search/item JSON and Bing translator for end-to-end tests.
Request/response shapes copied from the live services (checked 2026-10-01)."""
import http.server, socketserver, urllib.parse, json, io, sys, html, os, hashlib, zipfile, tarfile, gzip, threading
from PIL import Image

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 47991
B = f"http://127.0.0.1:{PORT}"

SHARES = {  # surl -> (pwd, title, tree)
    "1AbCdEfGhIjK": ("x7k2", None, [
        {"name": "Kaguya_v1.06", "dir": True, "kids": [
            {"name": "Kaguya_v1.06.unitypackage", "size": 300623143},
            {"name": "Texture", "dir": True, "kids": [{"name": f"tex_{i}.png", "size": 1000 + i} for i in range(3)]},
            {"name": "README.txt", "size": 1200}]},
    ]),
    "1AonamiShare": ("t9ya", None, [
        {"name": "8099091", "dir": True, "kids": [{"name": n, "size": sz} for n, sz in [
            ("AONAMI_chocolat_plum.zip", 6200000), ("AONAMI_manuka.zip", 25300000), ("AONAMI_materials.zip", 246000000),
            ("AONAMI_milfy_eku.zip", 6500000), ("AONAMI_PSD.zip", 383000000), ("AONAMI_rurune.zip", 25100000),
            ("AONAMI_shinano.zip", 25100000), ("AONAMI_sio.zip", 25100000)]]},
    ]),
    "1KaguyaHeji": ("kg88", None, [  # a collection: one folder per category, several products in each
        {"name": "辉夜合集-咸鱼@邀月浮白", "dir": True, "kids": [
            {"name": "8562330 辉夜 Kaguya模型", "dir": True, "kids": [{"name": "Kaguya_v1.06.unitypackage", "size": 300623143}, {"name": "Kaguya_PSD.zip", "size": 210000000}]},
            {"name": "头发", "dir": True, "kids": [
                {"name": "Twintail_hair", "dir": True, "kids": [{"name": "twintail.unitypackage", "size": 8200000}, {"name": "readme.txt", "size": 900}]},
                {"name": "Bob_hair_v2.zip", "size": 6100000}]},
            {"name": "妆容", "dir": True, "kids": [{"name": "Glitter makeup.zip", "size": 3100000}, {"name": "Natural makeup.zip", "size": 2800000}]},
            {"name": "衣服", "dir": True, "kids": [
                {"name": "AONAMI", "dir": True, "kids": [{"name": "AONAMI_kaguya.zip", "size": 25300000}, {"name": "AONAMI_PSD.zip", "size": 383000000}]},
                {"name": "Moon Dress", "dir": True, "kids": [{"name": "MoonDress_Kaguya.zip", "size": 52000000}, {"name": "Texture.zip", "size": 120000000}]}]},
            {"name": "饰品", "dir": True, "kids": [{"name": "Ribbon.unitypackage", "size": 1200000}]},
            {"name": "闲鱼@邀月浮白.jpg", "size": 230000}]},
    ]),
    "1NoPwdShare": ("", None, [
        {"name": "【8アバター対応】Moon Dress.zip", "size": 52000000},
        {"name": "MoonDress_Texture.zip", "size": 120000000},
    ]),
}


def unitypackage(files):
    """A .unitypackage: gzip tar of <guid>/{pathname,asset,asset.meta}."""
    bio = io.BytesIO()
    with tarfile.open(fileobj=bio, mode="w:gz") as tf:
        for i, (path, body) in enumerate(files):
            guid = hashlib.md5(path.encode()).hexdigest()
            for name, data in (("pathname", path.encode()), ("asset.meta", f"fileFormatVersion: 2\nguid: {guid}\n".encode()),
                               ("asset", body if isinstance(body, bytes) else None)):
                if data is None:
                    continue
                ti = tarfile.TarInfo(f"{guid}/{name}"); ti.size = len(data); tf.addfile(ti, io.BytesIO(data))
    return bio.getvalue()


def zipped(files):
    bio = io.BytesIO()
    with zipfile.ZipFile(bio, "w", zipfile.ZIP_STORED) as z:
        for n, b in files:
            z.writestr(n, b)
    return bio.getvalue()


# a share to download for real: the dress for two base bodies, its PSD pack, a readme
PSD_ZIP = zipped([("Moon_PSD/body.psd", b"8BPS" + b"p" * 50000)])
CONTENT = {
    "MoonDress_Plum.unitypackage": unitypackage([("Assets/MoonDress", None), ("Assets/MoonDress/Plum/MoonDress_Plum.prefab", b"prefab plum"),
                                                 ("Assets/MoonDress/Tex/dress.png", b"\x89PNG" + b"d" * 120000)]),
    "MoonDress_Kaguya.unitypackage": unitypackage([("Assets/MoonDress/Kaguya/MoonDress_Kaguya.prefab", b"prefab kaguya")]),
}
CONTENT["MoonDress_v1.zip"] = zipped([("MoonDress_v1/MoonDress_Plum.unitypackage", CONTENT["MoonDress_Plum.unitypackage"]),
                                      ("MoonDress_v1/MoonDress_Kaguya.unitypackage", CONTENT["MoonDress_Kaguya.unitypackage"]),
                                      ("MoonDress_v1/MoonDress_PSD.zip", PSD_ZIP)])
CONTENT["readme.txt"] = "Moon Dress 使用说明".encode()
SHARES["1DlTestShare"] = ("dl12", None, [
    {"name": "Moon Dress 月光裙", "dir": True, "kids": [
        {"name": "MoonDress_v1.zip", "size": len(CONTENT["MoonDress_v1.zip"])},
        {"name": "readme.txt", "size": len(CONTENT["readme.txt"])}]}])
SHARES["1LimitShare"] = ("", None, [  # more files than a free account may save at once (with limit=2)
    {"name": "Hair Pack", "dir": True, "kids": [{"name": "a.txt", "size": 10},
        {"name": "sub", "dir": True, "kids": [{"name": "b.txt", "size": 20}, {"name": "c.txt", "size": 30}]}]}])
# a share with several downloads: the player takes only some of it
CONTENT["Dress_A.zip"] = zipped([("Dress_A/Dress_A_Plum.unitypackage", unitypackage([("Assets/DressA/DressA_Plum.prefab", b"prefab a")]))])
CONTENT["Dress_B.zip"] = zipped([("Dress_B/Dress_B_Plum.unitypackage", unitypackage([("Assets/DressB/DressB_Plum.prefab", b"prefab b")]))])
SHARES["1PickShare"] = ("", None, [
    {"name": "Kisekae Set", "dir": True, "kids": [
        {"name": "Dress_A.zip", "size": len(CONTENT["Dress_A.zip"])},
        {"name": "Dress_B.zip", "size": len(CONTENT["Dress_B.zip"])},
        {"name": "PSD", "dir": True, "kids": [{"name": "a.psd", "size": 4000}, {"name": "b.psd", "size": 5000}]},
        {"name": "readme_set.txt", "size": 300}]}])
DISK = {"/": {"dir": True}}  # the logged-in player's netdisk: path -> {dir, size, src}
BD = {"login": False, "limit": 1000, "cut": set(), "log": []}
BDUSS = "mockbduss"


def fsid(p):
    return int(hashlib.md5(p.encode()).hexdigest()[:12], 16)


def disk_add(path, node):
    parent = path.rsplit("/", 1)[0] or "/"
    if parent not in DISK:
        disk_add(parent, {"dir": True})
    DISK[path] = node


def count_files(nodes):
    return sum(count_files(n.get("kids", [])) if n.get("dir") else 1 for n in nodes)


def copy_into(node, dest):
    p = dest + "/" + node["name"]
    while p in DISK:  # ondup=newcopy
        p += "(1)"
    if node.get("dir"):
        disk_add(p, {"dir": True})
        for k in node.get("kids", []):
            copy_into(k, p)
    else:
        disk_add(p, {"dir": False, "size": node.get("size", 0), "name": node["name"]})
    return p


def share_node_by_fsid(fid):
    for surl, (pwd, title, tree) in SHARES.items():
        def walk(nodes, base):
            for n in nodes:
                p = base + "/" + n["name"]
                if fsid(p) == fid:
                    return n
                if n.get("dir"):
                    r = walk(n.get("kids", []), p)
                    if r:
                        return r
        r = walk(tree, "/sharelink1-" + surl)
        if r:
            return r


def file_bytes(node):
    if node["name"] in CONTENT and len(CONTENT[node["name"]]) == node.get("size"):
        return CONTENT[node["name"]]
    return hashlib.sha256(node["name"].encode()).digest() * (node["size"] // 32 + 1)


HITS = {
    "moon dress": [("9100001", "【8アバター対応】Moon Dress", "lunaworks", "Luna Works", "3D衣装", 1500),
                   ("9100002", "Moonlight Ribbon", "ribbonshop", "Ribbon", "3D装飾品", 300)],
    "aonami": [("8099091", "【7アバター対応】AONAMI -アオナミ- セーラー服", "aonami", "AONAMI shop", "3D衣装", 2500)],
    "kaguya": [("8562330", "オリジナル3Dモデル『カグヤ』", "kaguya", "Kaguya Lab", "3Dキャラクター", 5800)],
    "neru": [("9100010", "ネル ヘア cloth set【Plum対応】", "neru", "neru shop", "3D衣装", 800)],
}


SYN = {}  # items made up while browsing
BROWSE_LOG = []


def tree_items(nodes, base):
    out = []
    for n in nodes:
        path = base + "/" + n["name"]
        out.append({"server_filename": n["name"], "path": path, "isdir": 1 if n.get("dir") else 0, "size": n.get("size", 0), "fs_id": fsid(path)})
    return out


def find(nodes, base, target):
    for n in nodes:
        p = base + "/" + n["name"]
        if p == target:
            return n.get("kids", []), p
        if n.get("dir"):
            r = find(n.get("kids", []), p, target)
            if r:
                return r
    return None


def card(id_, name, sub, shop, cat, price):
    return f'''<li class="item-card l-card " data-product-brand="{sub}" data-product-category="209" data-product-id="{id_}" data-product-name="{html.escape(name[:20])}..." data-product-price="{price}"><div class="item-card__wrap" id="item_{id_}"><div class="item-card__thumbnail js-thumbnail"><div class="item-card__thumbnail-images"><a class="js-thumbnail-image item-card__thumbnail-image" data-original="{B}/thumb/{id_}.jpg" href="https://booth.pm/ja/items/{id_}"></a></div></div>
<div class="item-card__summary"><div class="item-card__category"><a class="item-card__category-anchor nav-reverse" href="#">{cat}</a></div><div class="item-card__title"><a target="_self" class="item-card__title-anchor--multiline nav" href="https://booth.pm/ja/items/{id_}">{html.escape(name)}</a></div>
<div class="item-card__shop-info"><a class="item-card__shop-name-anchor nav" href="https://{sub}.booth.pm/"><div class="flex items-center"><img alt="{html.escape(shop)}" class="user-avatar at-item-footer" src="x"></div></a></div></div></div></li>'''


class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def send(self, code, body, ctype="text/html; charset=utf-8", headers=None):
        b = body if isinstance(body, bytes) else body.encode()
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(b)))
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(b)

    def cookies(self):
        c = {}
        for part in (self.headers.get("Cookie") or "").split(";"):
            if "=" in part:
                k, v = part.strip().split("=", 1)
                c[k] = v
        return c

    def do_POST(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query)
        body = self.rfile.read(int(self.headers.get("Content-Length") or 0)).decode()
        form = urllib.parse.parse_qs(body)
        if u.path == "/share/verify":
            surl = "1" + q["surl"][0]
            pwd = SHARES.get(surl, ("",))[0]
            if form.get("pwd", [""])[0] != pwd:
                return self.send(200, json.dumps({"errno": -9, "err_msg": ""}), "application/json")
            return self.send(200, "\n\n" + json.dumps({"errno": 0, "randsk": "RSK%2F" + surl}), "application/json",
                             {"Set-Cookie": "BDCLND=RSK%2F" + surl + "; path=/"})
        logged = self.cookies().get("BDUSS") == BDUSS
        if u.path in ("/api/create", "/share/transfer"):
            BD["log"].append(u.path + " " + json.dumps(form, ensure_ascii=False))
            if not logged or q.get("bdstoken", [""])[0] != "tok123":
                return self.send(200, json.dumps({"errno": -6}), "application/json")
        if u.path == "/api/create":
            p = form["path"][0]
            if p in DISK:
                return self.send(200, json.dumps({"errno": -8}), "application/json")
            disk_add(p, {"dir": True})
            return self.send(200, json.dumps({"errno": 0, "path": p, "isdir": 1}), "application/json")
        if u.path == "/share/transfer":
            ids = json.loads(form["fsidlist"][0]); dest = form["path"][0]
            nodes = [share_node_by_fsid(i) for i in ids]
            if None in nodes or dest not in DISK:
                return self.send(200, json.dumps({"errno": 2}), "application/json")
            n = count_files(nodes)
            if n > BD["limit"]:  # the account's file limit for one save
                return self.send(200, json.dumps({"errno": 12, "target_file_nums": n, "target_file_nums_limit": BD["limit"], "info": []}), "application/json")
            lst = [{"from_fs_id": i, "to": copy_into(nd, dest)} for i, nd in zip(ids, nodes)]
            return self.send(200, json.dumps({"errno": 0, "task_id": 0, "extra": {"list": lst}}), "application/json")
        if u.path == "/mock/bd":  # test switches: limit=N, cut=name (the first download of it breaks off)
            if "limit" in q:
                BD["limit"] = int(q["limit"][0])
            if "cut" in q:
                BD["cut"].add(q["cut"][0])
            if "reset" in q:
                DISK.clear(); DISK["/"] = {"dir": True}; BD["log"].clear()
            return self.send(200, json.dumps({"ok": True, "disk": sorted(DISK), "log": BD["log"]}, ensure_ascii=False), "application/json")
        if u.path == "/mock/add":  # a share changes on the author's side: one more file in a folder
            surl, d, n, sz = q["surl"][0], q["dir"][0], q["name"][0], int(q.get("size", ["1000"])[0])
            r = find(SHARES[surl][2], "", d)
            r[0].append({"name": n, "size": sz})
            return self.send(200, '{"ok":true}', "application/json")
        if u.path.startswith("/feedback"):  # FormSubmit's ajax endpoint: first message asks for activation
            with open(os.environ.get("MOCK_FB_LOG", "/dev/null"), "a") as f:
                f.write(json.dumps({"headers": dict(self.headers), "body": json.loads(body or "{}")}, ensure_ascii=False) + "\n")
            H.fb = getattr(H, "fb", 0) + 1
            if H.fb == 1:
                return self.send(200, json.dumps({"success": "false", "message": "This form needs Activation. We've sent you an email containing an 'Activate Form' link. Just click it and your form will be actived!"}), "application/json")
            return self.send(200, json.dumps({"success": "true", "message": "The form was submitted successfully."}), "application/json")
        if u.path == "/ttranslatev3":
            text = form.get("text", [""])[0]
            if form.get("token", [""])[0] != "TOK":
                return self.send(200, '{"statusCode":205}', "application/json")
            out = "\n".join("中文·" + line for line in text.split("\n"))
            return self.send(200, json.dumps([{"detectedLanguage": {"language": "ja"}, "translations": [{"text": out, "to": "zh-Hans"}]}]), "application/json")
        self.send(404, "")

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query)
        if u.path == "/repos/CokoIya/vrclib/releases/latest":  # renamed repository: GitHub answers 301 to the repo id
            return self.send(301, '{"message":"Moved Permanently"}', "application/json", {"Location": f"{B}/repositories/987654/releases/latest"})
        if u.path in ("/repos/CokoIya/MioVRC_AssetManager/releases/latest", "/repositories/987654/releases/latest"):  # GitHub API (shape of the live endpoint)
            rel = os.environ.get("MOCK_REL", "")
            assets = []
            for n in sorted(os.listdir(rel)) if rel and os.path.isdir(rel) else []:
                data = open(os.path.join(rel, n), "rb").read()
                assets.append({"name": n, "size": len(data), "browser_download_url": f"{B}/dl/{urllib.parse.quote(n)}",
                               "digest": "sha256:" + hashlib.sha256(data).hexdigest(), "content_type": "application/zip"})
            if not assets:
                return self.send(404, '{"message":"Not Found"}', "application/json")
            tag = os.environ.get("MOCK_TAG", "v9.9.9")
            return self.send(200, json.dumps({"tag_name": tag, "name": "MioVRCA " + tag[1:], "draft": False, "prerelease": False,
                "published_at": "2026-10-05T03:00:00Z", "html_url": "https://github.com/CokoIya/MioVRC_AssetManager/releases/tag/" + tag,
                "body": "## 更新内容\n- 新功能：**测试**\n- 修复：`library.json` 测试\n\n**下载**：[发布页](https://example.com)", "assets": assets}), "application/json")
        if u.path == "/mock/browselog":
            return self.send(200, json.dumps(BROWSE_LOG, ensure_ascii=False), "application/json")
        if u.path.startswith("/dl/"):
            n = urllib.parse.unquote(u.path[4:])
            return self.send(200, open(os.path.join(os.environ.get("MOCK_REL", ""), n), "rb").read(), "application/octet-stream")
        if u.path.startswith("/thumb/"):
            im = Image.new("RGB", (300, 300), (60, 120, 200)); bio = io.BytesIO(); im.save(bio, "JPEG")
            return self.send(200, bio.getvalue(), "image/jpeg")
        if u.path == "/translator":
            return self.send(200, '<html><script>var _G={IG:"ABCDEF0123"};params_AbusePreventionHelper = [1790000000,"TOK",3600000];</script><div id="rich_tta" data-iid="translator.5023"></div></html>')
        logged = self.cookies().get("BDUSS") == BDUSS
        if u.path == "/mock/bdlogin":  # Baidu's login page: logs in by itself after a moment (the player typing)
            return self.send(200, '<html><head><title>百度帐号登录</title></head><body><h1>登录百度帐号</h1><script>setTimeout(()=>location.href="/mock/bdlogin/do",1500)</script></body></html>')
        if u.path == "/mock/bdlogin/do":  # passport sets BDUSS, the netdisk page its own STOKEN
            self.send_response(302)
            self.send_header("Location", "/disk/main")
            self.send_header("Set-Cookie", f"BDUSS={BDUSS}; path=/; Max-Age=86400")
            self.send_header("Set-Cookie", "STOKEN=mockstoken; path=/; Max-Age=86400; HttpOnly")
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if u.path in ("/disk/main", "/disk/home"):
            return self.send(200, f'<html><head><title>百度网盘</title></head><body><h1>{"我的文件" if logged else "请登录"}</h1></body></html>')
        if u.path == "/api/gettemplatevariable":
            fields = json.loads(q.get("fields", ["[]"])[0])
            if "is_svip" in fields:
                return self.send(200, json.dumps({"errno": 0, "result": {"is_vip": 0, "is_svip": 0}}), "application/json")
            if not logged:
                return self.send(200, json.dumps({"errno": 0, "result": {"loginstate": 0}}), "application/json")
            return self.send(200, json.dumps({"errno": 0, "result": {"bdstoken": "tok123", "username": "mio测试", "loginstate": 1}}, ensure_ascii=False), "application/json")
        if u.path == "/api/list":
            d = q["dir"][0]
            if not logged:
                return self.send(200, json.dumps({"errno": -6}), "application/json")
            if d not in DISK:
                return self.send(200, json.dumps({"errno": -9, "list": []}), "application/json")
            start, num = int(q.get("start", ["0"])[0]), int(q.get("num", ["100"])[0])
            kids = sorted(p for p in DISK if p != d and p.rsplit("/", 1)[0] == (d if d != "/" else ""))
            lst = [{"path": p, "server_filename": p.rsplit("/", 1)[1], "isdir": 1 if DISK[p]["dir"] else 0, "size": DISK[p].get("size", 0), "fs_id": fsid(p)} for p in kids]
            return self.send(200, json.dumps({"errno": 0, "list": lst[start:start + num]}, ensure_ascii=False), "application/json")
        if u.path in ("/rest/2.0/pcs/file", "/cdn/pcs"):
            p = q.get("path", [""])[0]
            if u.path == "/rest/2.0/pcs/file":  # the PCS answers with a redirect to a file server
                if self.headers.get("User-Agent") != "pan.baidu.com":
                    return self.send(403, '{"error_code":31326,"error_msg":"user is not authorized, hitcode:119"}', "application/json")
                if not logged:
                    return self.send(403, '{"error_code":31045,"error_msg":"user not exists"}', "application/json")
                return self.send(302, "", headers={"Location": "/cdn/pcs?path=" + urllib.parse.quote(p)})
            node = DISK.get(p)
            if not node or node["dir"]:
                return self.send(404, "{}", "application/json")
            data = file_bytes(node)[:node["size"]]
            rng = self.headers.get("Range", "")
            a, z = 0, len(data) - 1
            if rng.startswith("bytes="):
                x, y = rng[6:].split("-")
                a, z = int(x), int(y) if y else len(data) - 1
            chunk = data[a:z + 1]
            BD["log"].append(f"GET {p} {a}-{z}")
            self.send_response(206)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Content-Range", f"bytes {a}-{z}/{len(data)}")
            self.send_header("Content-Length", str(len(chunk)))
            self.end_headers()
            name = p.rsplit("/", 1)[1]
            if name in BD["cut"]:  # the line drops halfway through
                BD["cut"].discard(name)
                self.wfile.write(chunk[:len(chunk) // 2]); self.wfile.flush()
                self.close_connection = True
                return
            self.wfile.write(chunk)
            return
        if u.path.startswith("/s/"):
            surl = u.path[3:]
            if surl not in SHARES:
                return self.send(200, "<html>啊哦，你来晚了，分享的文件已经被删除了</html>")
            pwd, title, tree = SHARES[surl]
            if pwd and self.cookies().get("BDCLND") != "RSK%2F" + surl:
                return self.send(302, "", headers={"Location": "/share/init?surl=" + surl[1:], "Set-Cookie": "BAIDUID=abc; path=/"})
            data = {"share_uk": "3967098798", "shareid": 46386138315, "file_list": tree_items(tree, "/sharelink1-" + surl)}
            if logged:  # the page a logged-in visitor gets: the older layout
                data.update({"bdstoken": "tok123", "loginstate": 1})
                return self.send(200, f'<html><script>locals.mset({json.dumps(data, ensure_ascii=False)});</script></html>')
            return self.send(200, f'<html><script id="locals-data" type="application/json">\n {json.dumps(data, ensure_ascii=False)}\n</script></html>',
                             headers={"Set-Cookie": "BAIDUID=abc; path=/"})
        if u.path == "/share/init":
            return self.send(200, "<html>请输入提取码</html>")
        if u.path == "/share/list":
            d = q.get("dir", [""])[0]
            for surl, (pwd, title, tree) in SHARES.items():
                r = find(tree, "/sharelink1-" + surl, d)
                if r:
                    if pwd and self.cookies().get("BDCLND") != "RSK%2F" + surl:
                        return self.send(200, json.dumps({"errno": -9}), "application/json")
                    kids, p = r
                    return self.send(200, json.dumps({"errno": 0, "list": tree_items(kids, p)}), "application/json")
            return self.send(200, json.dumps({"errno": 2}), "application/json")
        if u.path.startswith("/xy"):  # a stand-in for www.goofish.com: home, search, a chat with a share
            sub = u.path[3:] or "/"
            kw = q.get("q", [""])[0]
            if sub.startswith("/search"):
                body = f"<h1>搜索：{html.escape(kw)}</h1>" + "".join(f'<div class="item"><a href="/xy/item?id={i}" target="_blank">{html.escape(kw)} 宝贝 {i}</a> ¥{9 + i}</div>' for i in range(1, 6))
            elif sub.startswith("/im"):
                body = '<h1>消息</h1><div class="msg" id="share">链接: https://pan.baidu.com/s/1XyShareTest 提取码: xy12 复制这段内容打开百度网盘</div>'
            elif sub.startswith("/item"):
                body = f'<h1>宝贝 {html.escape(q.get("id", [""])[0])}</h1><button onclick="window.open(\'/xy/im\')">聊一聊</button>'
            elif sub.startswith("/login"):  # like the real one: a login cookie without an expiry date
                return self.send(200, "<html><head><title>闲鱼 登录</title></head><body><h1>登录好了</h1></body></html>",
                                 headers={"Set-Cookie": "cookie2=xy-session-1; path=/; HttpOnly"})
            else:
                who = "已登录" if self.cookies().get("cookie2") == "xy-session-1" else "未登录"
                body = f'<h1>闲鱼首页</h1><p id="who">{who}</p><a href="/xy/search?q=test">搜索 test</a>'
                sub = "首页 " + who
            return self.send(200, f"<html><head><title>闲鱼 {html.escape(sub)}</title></head><body>{body}</body></html>")
        if u.path.startswith("/ja/browse/") and "sort" in q:  # browsing from the Booth view: 60 cards a page
            cat = urllib.parse.unquote(u.path.split("/ja/browse/")[1])
            words = q.get("q", [""])[0]
            page = int(q.get("page", ["1"])[0])
            hits = []
            if page == 1:  # things the player has: bought (9000003), bought and in the library (7770415), in the library (8099091)
                hits += [("9000003", "【8アバター対応】Moon Dress", "lunaworks", "Luna Works", "3D衣装", 1500),
                         ("7770415", "プラム -Plum- / オリジナル3Dモデル", "komado", "あまとうさぎ", "3Dキャラクター", 5500),
                         ("8099091", "【7アバター対応】AONAMI -アオナミ- セーラー服", "aonami", "AONAMI shop", "3D衣装", 2500)]
                for h in hits:
                    SYN[h[0]] = h
            n = 60 if page == 1 else 20
            for i in range(len(hits), n):
                iid = str(9500000 + (abs(hash((cat, words))) % 4000) * 25 + (page - 1) * 60 + i)
                name = f"{cat} {words} No.{(page - 1) * 60 + i + 1}".strip()
                SYN[iid] = (iid, name, "shop" + str(i % 7), "Shop " + str(i % 7), cat, 300 + i * 50)
                hits.append(SYN[iid])
            BROWSE_LOG.append(u.path + "?" + u.query)
            return self.send(200, "<html><ul>" + "".join(card(*h) for h in hits) + "</ul></html>")
        if u.path.startswith("/ja/browse/") or u.path.startswith("/ja/search/"):
            term = (q.get("q", [""])[0] if "q" in q else urllib.parse.unquote(u.path.split("/ja/search/")[-1])).lower()
            hits = []
            for k, v in HITS.items():
                if k in term or term in k:
                    hits += v
            return self.send(200, "<html><ul>" + "".join(card(*h) for h in hits) + "</ul></html>")
        if u.path.startswith("/ja/items/"):
            id_ = u.path.split("/")[-1]
            is_json = id_.endswith(".json")
            id_ = id_[:-5] if is_json else id_
            for v in list(HITS.values()) + [[x] for x in SYN.values()]:
                for h in v:
                    if h[0] != id_:
                        continue
                    if not is_json:  # item page: og tags + titled description sections (live layout)
                        return self.send(200, f'''<html><head><meta property="og:title" content="{html.escape(h[1])} - {html.escape(h[3])} - BOOTH"><meta property="og:image" content="{B}/thumb/{id_}.jpg"></head><body>
<div class="js-market-item-detail-description description"><p class="autolink">この商品は8アバターに対応したドレスです。</p></div>
<section class="shop__text"><h2 class="break-words font-bold">商品説明</h2><p class="break-words js-autolink whitespace-pre-line">8アバター対応のドレスです。\n&lt;注意&gt; 改変前提です。</p></section><section class="shop__text"><h2 class="break-words font-bold">利用規約</h2><p class="break-words">VN3ライセンス<br>再配布禁止</p></section><section class="shop__text"><h2 class="x">画像のみ</h2><img src="x"></section></body></html>''')
                    # live shape (checked 2026-10-01): id is a string, tags are objects
                    return self.send(200, json.dumps({"id": id_, "name": h[1], "price": f"¥ {h[5]}", "url": f"https://{h[2]}.booth.pm/items/{id_}",
                        "description": "この商品は8アバターに対応したドレスです。\n\n■対応アバター\nPlum / Chocolat\n■内容\nunitypackage",
                        "tags": [{"name": "VRChat", "url": "https://booth.pm/ja/items?tags%5B%5D=VRChat"}, {"name": "衣装", "url": "x"}],
                        "category": {"id": 209, "name": h[4], "parent": {"name": "3Dモデル", "url": "x"}, "url": "x"},
                        "shop": {"uuid": "u", "name": h[3], "subdomain": h[2], "thumbnail_url": "x", "url": f"https://{h[2]}.booth.pm/"},
                        "images": [{"caption": None, "original": f"{B}/thumb/{id_}.jpg", "resized": f"{B}/thumb/{id_}.jpg"}]}), "application/json")
            return self.send(404, "{}")
        self.send(404, "")


socketserver.ThreadingTCPServer.allow_reuse_address = True
with socketserver.ThreadingTCPServer(("127.0.0.1", PORT), H) as srv:
    srv.serve_forever()
