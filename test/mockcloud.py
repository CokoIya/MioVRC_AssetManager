"""Mock of Google Drive (share pages, the uc download endpoint and its confirm page, the Drive API) and of
Dropbox (dl=1 links) for end-to-end tests. Shapes follow the live services' documented behaviour and the
pages gdown reads (checked 2026-10-04). Run the program with
VRCLIB_GDRIVE_BASE=http://127.0.0.1:47993 VRCLIB_GDRIVE_API=http://127.0.0.1:47993 VRCLIB_DROPBOX_BASE=http://127.0.0.1:47993
(VRCLIB_GDRIVE_KEY=anything makes it use the API instead of the pages)."""
import http.server, socketserver, urllib.parse, io, sys, zipfile, json, re, html, time

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 47993
B = f"http://127.0.0.1:{PORT}"


def zipped(files):
    bio = io.BytesIO()
    with zipfile.ZipFile(bio, "w", zipfile.ZIP_STORED) as z:
        for n, b in files:
            z.writestr(n, b)
    return bio.getvalue()


# Drive: id -> node. A folder has kids; a file has data (big: shown the "can't scan for viruses" page first)
DRIVE = {
    "1FolderKaguyaOutfits000000000": {"name": "Kaguya Outfits", "kids": ["1FileMoonDress00000000000000", "1FolderTextures0000000000000", "1FileReadme00000000000000000", "1DocNotes00000000000000000000"]},
    "1FileMoonDress00000000000000": {"name": "MoonDress_Kaguya.zip", "data": zipped([("MoonDress_Kaguya/MoonDress.unitypackage", b"U" * 300000), ("MoonDress_Kaguya/readme.txt", b"hi")]), "big": True},
    "1FolderTextures0000000000000": {"name": "Textures", "kids": ["1FileTexA0000000000000000000", "1FileTexB0000000000000000000"]},
    "1FileTexA0000000000000000000": {"name": "MoonDress_Texture_A.png", "data": b"\x89PNG" + b"A" * 40000},
    "1FileTexB0000000000000000000": {"name": "MoonDress_Texture_B.png", "data": b"\x89PNG" + b"B" * 40000},
    "1FileReadme00000000000000000": {"name": "README.txt", "data": b"MoonDress for Kaguya\n"},
    "1DocNotes00000000000000000000": {"name": "Notes", "doc": True},
    "1FileSingleOutfit00000000000": {"name": "AONAMI_kaguya.zip", "data": zipped([("AONAMI_kaguya/AONAMI.unitypackage", b"A" * 120000)])},
    "1FileThrottled00000000000000": {"name": "Busy.zip", "data": b"PK" * 10, "quota": True},
    "1FileSlow0000000000000000000": {"name": "Slow_Pack.zip", "data": zipped([("Slow_Pack/big.unitypackage", b"S" * 6000000)]), "slow": True},
}
DROPBOX = {  # link id -> (name, data)
    "db1dressabc1234": ("Dress_Kaguya.zip", zipped([("Dress_Kaguya/Dress.unitypackage", b"D" * 200000)])),
    "dbfolderxyz7890": ("Hair Pack.zip", zipped([("Hair Pack/Twintail.unitypackage", b"T" * 50000), ("Hair Pack/Bob.unitypackage", b"B" * 50000)])),
}
STATE = {"gone": set(), "quota": set()}


class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def send(self, code, body, ctype="text/html; charset=utf-8", headers=None):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(body)

    def serve_file(self, name, data, slow=False):
        rg = self.headers.get("Range")
        headers = {"Content-Disposition": "attachment; filename=\"%s\"; filename*=UTF-8''%s" % (name, urllib.parse.quote(name)),
                   "ETag": '"%s"' % name, "Accept-Ranges": "bytes"}
        if rg:
            m = re.match(r"bytes=(\d+)-(\d*)", rg)
            a = int(m.group(1)); z = int(m.group(2)) if m.group(2) else len(data) - 1
            if a >= len(data):
                return self.send(416, b"", "text/plain")
            headers["Content-Range"] = f"bytes {a}-{z}/{len(data)}"
            if slow:
                return self.trickle(206, data[a:z + 1], headers)
            return self.send(206, data[a:z + 1], "application/octet-stream", headers)
        if slow:
            return self.trickle(200, data, headers)
        self.send(200, data, "application/octet-stream", headers)

    def trickle(self, code, body, headers):
        """a big file on a slow line: 64 KB every tenth of a second"""
        self.send_response(code)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(len(body)))
        for k, v in headers.items():
            self.send_header(k, v)
        self.end_headers()
        try:
            for i in range(0, len(body), 65536):
                self.wfile.write(body[i:i + 65536]); self.wfile.flush(); time.sleep(0.1)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query)
        get = lambda k: (q.get(k) or [""])[0]
        # ---- Drive: pages ----
        if u.path == "/embeddedfolderview":
            n = DRIVE.get(get("id"))
            if not n or "kids" not in n or get("id") in STATE["gone"]:
                return self.send(404, b"<html><body>Not found</body></html>")
            out = ['<!DOCTYPE html><html><head><title>%s - Google Drive</title></head><body><div id="folder-view"><div class="flip-entries">' % html.escape(n["name"])]
            for k in n["kids"]:
                kn = DRIVE[k]
                href = ("https://drive.google.com/drive/folders/%s?usp=drive_web" % k if "kids" in kn
                        else "https://docs.google.com/document/d/%s/edit?usp=drive_web" % k if kn.get("doc")
                        else "https://drive.google.com/file/d/%s/view?usp=drive_web" % k)
                out.append('<div class="flip-entry" id="entry-%s"><a href="%s" target="_blank"><div class="flip-entry-thumb"><img src="x"></div><div class="flip-entry-info"><div class="flip-entry-title">%s</div><div class="flip-entry-last-modified"><div>Oct 1, 2026</div></div></div></a></div>' % (k, href, html.escape(kn["name"])))
            out.append("</div></div></body></html>")
            return self.send(200, "".join(out).encode())
        if u.path == "/uc":
            fid = get("id"); n = DRIVE.get(fid)
            if not n or "data" not in n or fid in STATE["gone"]:
                return self.send(404, b"<html><body>Not found</body></html>")
            if n.get("quota") or fid in STATE["quota"]:
                return self.send(200, b'<html><body><div class="uc-main"><p class="uc-error-caption">Sorry, you can\'t view or download this file at this time.</p><p class="uc-error-subcaption">Too many users have viewed or downloaded this file recently. Please try accessing the file again later. If the file you are trying to access is particularly large or is shared with many people, it may take up to 24 hours to be able to view or download the file. If you still can\'t access a file after 24 hours, contact your domain administrator.</p></div></body></html>')
            if n.get("big"):
                page = ('<!DOCTYPE html><html><head><title>Google Drive - Virus scan warning</title></head><body><div class="uc-main">'
                        '<p class="uc-warning-caption">Google Drive can\'t scan this file for viruses.</p>'
                        '<p class="uc-warning-subcaption"><span class="uc-name-size"><a href="/open?id=%s">%s</a> (%dK)</span> is too large for Google to scan for viruses. Would you still like to download this file?</p>'
                        '<form id="download-form" action="%s/download" method="get"><input type="submit" id="uc-download-link" class="goog-inline-block jfk-button jfk-button-action" value="Download anyway"/>'
                        '<input type="hidden" name="id" value="%s"><input type="hidden" name="export" value="download"><input type="hidden" name="confirm" value="t"><input type="hidden" name="uuid" value="5e0c1a2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b"></form></div></body></html>'
                        % (fid, html.escape(n["name"]), len(n["data"]) // 1024, B, fid))
                return self.send(200, page.encode())
            return self.serve_file(n["name"], n["data"], n.get("slow"))
        if u.path == "/download":  # drive.usercontent.google.com/download: the confirm form's target
            fid = get("id"); n = DRIVE.get(fid)
            if not n or get("confirm") != "t" or not get("uuid"):
                return self.send(400, b"bad request", "text/plain")
            return self.serve_file(n["name"], n["data"])
        if u.path.startswith("/file/d/"):
            fid = u.path.split("/")[3]; n = DRIVE.get(fid, {"name": ""})
            return self.send(200, ('<html><head><meta property="og:title" content="%s"><title>%s - Google Drive</title></head></html>' % (html.escape(n["name"]), html.escape(n["name"]))).encode())
        # ---- Drive: the API (with a key) ----
        if u.path.startswith("/drive/v3/files"):
            if not get("key"):
                return self.send(400, json.dumps({"error": {"code": 400, "message": "API key not valid. Please pass a valid API key.", "errors": [{"reason": "badRequest"}]}}).encode(), "application/json")
            meta = lambda k: ({"id": k, "name": DRIVE[k]["name"], "mimeType": "application/vnd.google-apps.folder"} if "kids" in DRIVE[k]
                              else {"id": k, "name": DRIVE[k]["name"], "mimeType": "application/vnd.google-apps.document"} if DRIVE[k].get("doc")
                              else {"id": k, "name": DRIVE[k]["name"], "mimeType": "application/zip", "size": str(len(DRIVE[k]["data"]))})
            if u.path == "/drive/v3/files":
                m = re.match(r"'([^']+)' in parents", get("q") or "")
                fid = m.group(1) if m else ""
                if fid not in DRIVE or "kids" not in DRIVE[fid]:
                    return self.send(404, json.dumps({"error": {"code": 404, "message": "File not found", "errors": [{"reason": "notFound"}]}}).encode(), "application/json")
                return self.send(200, json.dumps({"files": [meta(k) for k in DRIVE[fid]["kids"]]}).encode(), "application/json")
            fid = u.path.split("/")[4]
            if fid not in DRIVE or fid in STATE["gone"]:
                return self.send(404, json.dumps({"error": {"code": 404, "message": "File not found: " + fid, "errors": [{"reason": "notFound"}]}}).encode(), "application/json")
            if get("alt") != "media":
                return self.send(200, json.dumps(meta(fid)).encode(), "application/json")
            n = DRIVE[fid]
            if n.get("quota") or fid in STATE["quota"]:
                return self.send(403, json.dumps({"error": {"code": 403, "message": "The download quota for this file has been exceeded.", "errors": [{"reason": "downloadQuotaExceeded"}]}}).encode(), "application/json")
            return self.serve_file(n["name"], n["data"])
        # ---- Dropbox ----
        m = re.match(r"^/(s|scl/fi|scl/fo|sh)/([A-Za-z0-9]+)", u.path)
        if m:
            name, data = DROPBOX.get(m.group(2), (None, None))
            if name is None or m.group(2) in STATE["gone"]:
                return self.send(404, b"<html><body>Error (404)</body></html>")
            if get("dl") != "1":
                return self.send(200, b"<html><body>preview page</body></html>")
            return self.serve_file(name, data)
        # ---- test control: /mock/gone?id=…, /mock/quota?id=…, /mock/reset ----
        if u.path == "/mock/gone":
            STATE["gone"].add(get("id")); return self.send(200, b"ok", "text/plain")
        if u.path == "/mock/quota":
            STATE["quota"].add(get("id")); return self.send(200, b"ok", "text/plain")
        if u.path == "/mock/reset":
            STATE["gone"].clear(); STATE["quota"].clear(); return self.send(200, b"ok", "text/plain")
        self.send(404, b"not found", "text/plain")


class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


if __name__ == "__main__":
    print("mock cloud on", B, flush=True)
    Server(("127.0.0.1", PORT), H).serve_forever()
