"""Stand-in for jinxxy.com for end-to-end tests of the in-app page (links, login, downloads).
Only the addresses follow the live site (checked 2026-10-03: /market/browse, /market/<category>, ?max_price=0,
/<creator>/<product>, /login); the markup, the login cookie and the inventory are made up, since the live
pages could not be read without an account. Nothing in the program reads this markup."""
import http.server, socketserver, urllib.parse, sys, html, io, zipfile, tarfile

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 47993
B = f"http://127.0.0.1:{PORT}"
EXT = f"http://localhost:{PORT}"  # another origin: stands for a shop elsewhere (Payhip, Gumroad …)
PRODUCTS = [("aesu", "studded_shorts", "Studded Mini Shorts with a keychain", "$6.90 USD"), ("Karisauria", "varania", "Nekari Varania - Monitor Lizard VRChat avatar", "$50.00 USD"),
            ("OwlManny", "masterofhalloweenhat", "FREEBIE Master of Halloween: Top Hat", "$0.00 USD")]
LOG = []


def unitypackage(path):
    bio = io.BytesIO()
    with tarfile.open(fileobj=bio, mode="w:gz") as tf:
        for name, data in [("0123456789abcdef0123456789abcd01/pathname", path.encode()), ("0123456789abcdef0123456789abcd01/asset", b"%YAML 1.1")]:
            ti = tarfile.TarInfo(name); ti.size = len(data); tf.addfile(ti, io.BytesIO(data))
    return bio.getvalue()


def pack():
    bio = io.BytesIO()
    with zipfile.ZipFile(bio, "w") as z:
        z.writestr("Studded Shorts/StuddedShorts_Plum.unitypackage", unitypackage("Assets/Aesu/StuddedShorts/StuddedShorts_Plum.prefab"))
        z.writestr("Studded Shorts/readme.txt", "Studded Mini Shorts")
    return bio.getvalue()


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

    def page(self, title, body):
        who = "signed in" if "jx_session=ok" in (self.headers.get("Cookie") or "") else "signed out"
        nav = f'<nav><a href="/">Jinxxy</a> <a href="/market/browse">Everything</a> <a href="/login">Sign In</a> <a href="/my/inventory">My Purchases</a> <span id="who">{who}</span></nav>'
        self.send(200, f"<html><head><title>{html.escape(title)}</title></head><body>{nav}{body}</body></html>")

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query)
        host = self.headers.get("Host", "")
        LOG.append(host + self.path)
        if u.path == "/mock/log":
            return self.send(200, "\n".join(LOG), "text/plain")
        if host.startswith("localhost"):  # the shop elsewhere
            return self.send(200, "<html><head><title>Another shop</title></head><body><h1>Another shop</h1></body></html>")
        if u.path == "/login":  # a login cookie without an expiry date, as many sites set it
            return self.send(200, '<html><head><title>Sign In – Jinxxy</title></head><body><h1 id="in">Signed in</h1><a href="/">Home</a></body></html>',
                             headers={"Set-Cookie": "jx_session=ok; Path=/; HttpOnly"})
        if u.path == "/dl/StuddedShorts.zip":
            if "jx_session=ok" not in (self.headers.get("Cookie") or ""):
                return self.send(403, "sign in first", "text/plain")
            return self.send(200, pack(), "application/zip", {"Content-Disposition": 'attachment; filename="Studded Mini Shorts v1.2.zip"'})
        if u.path == "/my/inventory":
            if "jx_session=ok" not in (self.headers.get("Cookie") or ""):
                return self.page("Sign In – Jinxxy", '<h1>Sign in to see your purchases</h1>')
            return self.page("Inventory – Jinxxy", '<h1>Inventory</h1><div class="owned">Studded Mini Shorts with a keychain <a id="dl" href="/dl/StuddedShorts.zip?sig=secret-token">Download</a></div>')
        if u.path.startswith("/market/"):
            free = q.get("max_price", [""])[0] == "0"
            cards = "".join(f'<li class="p"><a class="own" href="/{c}/{s}" target="_blank">{html.escape(t)}</a> <span class="price">{pr}</span></li>'
                            for c, s, t, pr in PRODUCTS if not free or pr.startswith("$0.00"))
            cards += f'<li class="p"><a id="ext" href="{EXT}/b/QvEtC?ref=jinxxy" target="_blank">Molly [PC n Quest]</a> <span class="price">$15.00 USD</span></li>'
            return self.page("Marketplace for VR Avatars, Assets, and Worlds – Jinxxy", f'<h1 id="h">Browsing {html.escape(u.path.split("/")[-1])}{" free" if free else ""}</h1><ul>{cards}</ul>')
        parts = [p for p in u.path.split("/") if p]
        if len(parts) == 2:
            hit = [p for p in PRODUCTS if p[0] == parts[0] and p[1] == parts[1]]
            title, price = (hit[0][2], hit[0][3]) if hit else (parts[1], "$1.00 USD")
            return self.page(f"{title} by {parts[0]} on Jinxxy", f'<h1 id="title">{html.escape(title)}</h1><p class="price">{price}</p><button>Add to Cart</button>')
        return self.page("Jinxxy", '<h1 id="home">Jinxxy</h1><a href="/market/browse">Marketplace</a>')


socketserver.ThreadingTCPServer.allow_reuse_address = True
with socketserver.ThreadingTCPServer(("127.0.0.1", PORT), H) as srv:
    srv.serve_forever()
