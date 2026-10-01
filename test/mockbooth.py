"""Mock of accounts.booth.pm library/orders pages for end-to-end tests (structure modelled on
the current Booth markup used by BoothDownloader / booth-library-manager)."""
import http.server, socketserver, urllib.parse, io, sys
from PIL import Image

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 47990

ITEMS = {
    # id: (name, shop, shopsub, [files])
    "9000001": ("オリジナル3Dモデル「輝夜 -Kaguya-」", "Kaguya Studio", "kaguyastudio", ["Kaguya_v1.06.zip", "Kaguya_PSD.zip"]),
    "7770415": ("プラム -Plum- / オリジナル3Dモデル", "あまとうさぎ", "komado", ["Plum_v1.0.1.zip"]),
    "9000003": ("【8アバター対応】Moon Dress", "Luna Works", "lunaworks", ["MoonDress_v1.2.zip", "Texture.zip"]),
    "9000004": ("SexyGLOW Skin for liltoon", "Miu真昼", "miuthepotato", ["SexyGLOW_v2.unitypackage"]),
    "9000005": ("【無料】ふわふわ尻尾", "Tail Shop", "tailshop", ["fluffy_tail.zip", "Texture.zip"]),
}
GIFTS = {"9000006": ("誕生日のお祝いリボン", "Gift Shop", "giftshop", ["ribbon_gift.zip"])}
PAGES = [["9000001", "7770415", "9000003"], ["9000004", "9000005"]]
ORDERS = [  # id, date, items (None = list page does not show items)
    ("51000001", "2026/09/20", ["9000001"]),
    ("51000002", "2025/12/02", None),
    ("51000003", "2025/03/08", ["9000004", "9000005"]),
]
ORDER_DETAIL = {"51000002": ["7770415", "9000003"]}


def card(iid, it, n0):
    name, shop, sub, files = it
    rows = "".join(f'''
      <div class="mt-16 desktop:flex desktop:justify-between desktop:items-center">
        <div class="flex items-center gap-8"><i class="icon-file"></i><div class="text-14 text-text-default break-all">{f}</div></div>
        <div class="js-download-button flex" data-href="https://booth.pm/downloadables/{n0 + k}" data-test="download-button"><span>ダウンロード</span></div>
      </div>''' for k, f in enumerate(files))
    return f'''
  <div class="bg-white desktop:rounded-8 p-16 mb-16">
    <div class="flex gap-16">
      <a href="https://booth.pm/ja/items/{iid}" target="_blank"><img class="l-library-item-thumbnail" src="http://127.0.0.1:{PORT}/thumb/{iid}.jpg" alt="{name}"></a>
      <div class="min-w-0">
        <a href="https://booth.pm/ja/items/{iid}" target="_blank"><div class="text-text-default font-bold text-16 mb-8 break-all">{name}</div></a>
        <a href="https://{sub}.booth.pm/" target="_blank"><div class="text-14 text-text-gray600 break-all">{shop}</div></a>
      </div>
    </div>
    <div class="mt-16">{rows}</div>
    <div class="w-full js-download-button" data-test="other-downloads-button"><span>その他のダウンロード</span></div>
  </div>'''


def page_shell(body, nav=""):
    return f'''<!doctype html><html><head><title>ライブラリ - BOOTH</title></head><body>
<header><a href="https://booth.pm/ja">BOOTH</a><a href="https://booth.pm/ja/items/1111111">おすすめ</a></header>
<main><div class="w-full">{body}</div>{nav}</main><footer><a href="https://booth.pm/ja/items/2222222">footer item</a></footer></body></html>'''


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

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query)
        page = int(q.get("page", ["1"])[0])
        logged = "session=ok" in (self.headers.get("Cookie") or "")
        if u.path.startswith("/thumb/"):
            im = Image.new("RGB", (300, 300), (200, 80 + int(u.path[-6:-4]) % 100, 120))
            bio = io.BytesIO(); im.save(bio, "JPEG")
            return self.send(200, bio.getvalue(), "image/jpeg")
        if u.path == "/users/sign_in":
            return self.send(200, '''<html><body><h1>ログイン</h1><p>logging in…</p><script>
              setTimeout(() => { document.cookie = "session=ok; path=/"; location.href = "/library"; }, 2500);</script></body></html>''')
        if not logged:
            return self.send(302, "", headers={"Location": "/users/sign_in"})
        if u.path == "/library":
            if page > len(PAGES):
                return self.send(200, page_shell("<p>購入した商品はありません</p>"))
            body = "".join(card(i, ITEMS[i], int(i) * 10) for i in PAGES[page - 1])
            nav = '<nav class="pager">' + "".join(f'<a class="nav-item" href="/library?page={p}">{p}</a>' for p in range(1, len(PAGES) + 1)) + \
                  f'<a class="nav-item last-page" href="/library?page={len(PAGES)}">»</a></nav>'
            return self.send(200, page_shell(body, nav))
        if u.path == "/library/gifts":
            body = "".join(card(i, it, int(i) * 10) for i, it in GIFTS.items()) if page == 1 else ""
            return self.send(200, page_shell(body))
        if u.path == "/orders":
            if page > 1:
                return self.send(200, page_shell("<p>なし</p>"))
            blocks = ""
            for oid, date, items in ORDERS:
                inner = "".join(f'<a href="https://booth.pm/ja/items/{i}"><div class="font-bold">{ITEMS[i][0]}</div></a>' for i in (items or []))
                blocks += f'''<div class="sheet"><a href="https://accounts.booth.pm/orders/{oid}"><div class="u-tpg-caption1">注文番号 {oid}</div>
                  <div class="text-14">注文日時 {date} 12:34</div></a>{inner}</div>'''
            return self.send(200, page_shell(blocks))
        if u.path.startswith("/orders/"):
            oid = u.path.split("/")[2]
            items = ORDER_DETAIL.get(oid) or next((o[2] for o in ORDERS if o[0] == oid), [])
            body = f"<div>注文番号 {oid}</div>" + "".join(card(i, ITEMS[i], int(i) * 10) for i in items)
            return self.send(200, page_shell(body))
        return self.send(404, "not found")


socketserver.ThreadingTCPServer.allow_reuse_address = True
with socketserver.ThreadingTCPServer(("127.0.0.1", PORT), H) as srv:
    srv.serve_forever()
