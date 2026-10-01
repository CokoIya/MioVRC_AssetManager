"""Mock of Baidu share pages, Booth search/item JSON and Bing translator for end-to-end tests.
Request/response shapes copied from the live services (checked 2026-10-01)."""
import http.server, socketserver, urllib.parse, json, io, sys, html
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
    "1NoPwdShare": ("", None, [
        {"name": "【8アバター対応】Moon Dress.zip", "size": 52000000},
        {"name": "MoonDress_Texture.zip", "size": 120000000},
    ]),
}
HITS = {
    "moon dress": [("9100001", "【8アバター対応】Moon Dress", "lunaworks", "Luna Works", "3D衣装", 1500),
                   ("9100002", "Moonlight Ribbon", "ribbonshop", "Ribbon", "3D装飾品", 300)],
    "neru": [("9100010", "ネル ヘア cloth set【Plum対応】", "neru", "neru shop", "3D衣装", 800)],
}


def tree_items(nodes, base):
    out = []
    for n in nodes:
        path = base + "/" + n["name"]
        out.append({"server_filename": n["name"], "path": path, "isdir": 1 if n.get("dir") else 0, "size": n.get("size", 0), "fs_id": abs(hash(path)) % 10**15})
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
        if u.path.startswith("/thumb/"):
            im = Image.new("RGB", (300, 300), (60, 120, 200)); bio = io.BytesIO(); im.save(bio, "JPEG")
            return self.send(200, bio.getvalue(), "image/jpeg")
        if u.path == "/translator":
            return self.send(200, '<html><script>var _G={IG:"ABCDEF0123"};params_AbusePreventionHelper = [1790000000,"TOK",3600000];</script><div id="rich_tta" data-iid="translator.5023"></div></html>')
        if u.path.startswith("/s/"):
            surl = u.path[3:]
            if surl not in SHARES:
                return self.send(200, "<html>啊哦，你来晚了，分享的文件已经被删除了</html>")
            pwd, title, tree = SHARES[surl]
            if pwd and self.cookies().get("BDCLND") != "RSK%2F" + surl:
                return self.send(302, "", headers={"Location": "/share/init?surl=" + surl[1:], "Set-Cookie": "BAIDUID=abc; path=/"})
            data = {"share_uk": "3967098798", "shareid": 46386138315, "file_list": tree_items(tree, "/sharelink1-" + surl)}
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
            for v in HITS.values():
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
