# Plays the Unity side of the AI assistant for the UI tests: heartbeat + answers in <project>/UserSettings/MioVRCA/bridge.
# The scene remembers: what was dressed is on the avatar (and "existing" the next time), a name that is taken gives
# the next object another one, the menus that were built show in maMenu, and undo only takes back steps of its own.
# A test steers it with <project>/fake_ctl.json, read (and removed) before the next request:
#   {"compiling": 1}         the next request is answered 「正在编译」 once (the program asks again)
#   {"bits": 250}            synced parameter bits of the avatar's asset; menu items add to it as the plugin counts them
#   {"bones": 240}           PhysBones on the avatar before anything is dressed (each outfit adds 12)
#   {"dressWarn": "..."}     a warning that comes with every dress
#   {"foreignUndo": "Move"}  the player did something in Unity: it lies on top of the undo history
#   {"playing": 1} / {"busy": 1}  the heartbeat says Play mode / compiling (0 takes it back)
#   {"pink": 2, "missing": 1, "bigTex": 3}  what the check-up finds: pink materials, missing scripts, 4096 px textures
#   {"menuOver": 11}         a page of the descriptor's menu with that many controls
#   {"fury": 1}              VRCFury components on the avatar (the parameter count is a lower bound)
#   {"noCalc": 1}            the SDK's performance calculator is not there: counts without ranks
#   {"shotFail": "bat"}      prefab_shot answers a reason instead of a picture for prefabs with that in their name
#   {"oldPlugin": 1}         the plugin does not know checkup, prefab_shot, fix and fix_revert (one from before 1.7.6)
#   {"lights": 2}            Light components on the avatar (the check-up's 灯光 figure)
#   {"pkgTex": 1}            that many of the 4096 px textures are a package's: the texture fix leaves them (fixable = bigTex - pkgTex)
#   {"compileFailed": 1}     scripts do not compile: the missing-scripts fix is refused
#   {"lostTex": 1}           a texture of the revert record was moved since: fix_revert says so and keeps the record
#   {"fixBreaks": 1}         the texture fix breaks off after the first reimport: an error, with the record left in the project
# fix: textures (Max Size down: bigTex becomes pkgTex, a revert record is written to UserSettings/MioVRCA/fixes),
# lights (lights becomes 0), missing (missing becomes 0); fix_revert puts bigTex back from the record.
#   {"ft": true}             the avatar has face tracking on it
# Plugin prefabs (place): a lighting control with a menu of its own, an interaction gimmick per base body (VRCFury),
# face tracking that comes with an installer, a skin as a whole-avatar prefab, a prefab whose scripts are missing.
# FAKE_BITS / FAKE_BONES / FAKE_COMPILING set the same at the start; FAKE_SKILLS_PORT moves the UnitySkills port.
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
    out.append({"path": "Assets/nHaruka/Light/LightControl.prefab", "name": "LightControl", "renderers": 0, "meshNames": [], "hasArmature": False, "selfInstalling": True, "menuInstallers": 1, "scripts": ["MA MenuInstaller", "MA MergeAnimator", "MA Parameters"]})
    for b in ["Kaguya", "Plum"]:
        out.append({"path": "Assets/Panda Shop/SPS/Prefab/for %s/SPS for %s.prefab" % (b, b), "name": "SPS for " + b, "renderers": 1, "meshNames": ["SPS"], "hasArmature": False, "selfInstalling": True, "vrcFury": True, "scripts": ["VRCFury"]})
    for n in ["Eye Tracking", "Mouth Tracking"]:
        out.append({"path": "Assets/Triturbo/Kaguya_FT/Prefabs/%s.prefab" % n, "name": n, "renderers": 0, "meshNames": [], "hasArmature": False, "selfInstalling": True, "faceTracking": True, "scripts": ["MA MergeAnimator", "MA Parameters"]})
    out.append({"path": "Assets/Skins/Tan/Kaguya_Tan.prefab", "name": "Kaguya_Tan", "wholeAvatar": True, "renderers": 3, "meshNames": ["Body", "Hair"], "hasArmature": True})
    out.append({"path": "Assets/Broken/gimmick.prefab", "name": "gimmick", "renderers": 0, "meshNames": [], "hasArmature": False, "missingScripts": 2})
    if folders:
        low = [f.lower().rstrip('/') for f in folders]
        out = [x for x in out if any(x["path"].lower() == f or x["path"].lower().startswith(f + '/') for f in low)]
    return out
BRIDGE = os.environ.get("FAKE_BRIDGE", "1.4.0")
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
state = {"prefabOf": {}, "dressed": [], "menu": [], "log": [], "placed": os.environ.get("FAKE_EMPTY") != "1", "plans": [], "undo": []}
ctl = {"compiling": int(os.environ.get("FAKE_COMPILING", "0")), "bits": int(os.environ.get("FAKE_BITS", "0")), "bones": int(os.environ.get("FAKE_BONES", "0")), "dressWarn": "", "ft": False}
ctl.update({"playing": 0, "busy": 0, "pink": 0, "missing": 0, "bigTex": 1, "menuOver": 0, "fury": 0, "noCalc": 0, "shotFail": "", "oldPlugin": int(os.environ.get("FAKE_OLD_PLUGIN", "0")), "lights": 0,
            "pkgTex": 0, "compileFailed": 0, "lostTex": 0, "fixBreaks": 0})  # the check-up, its fixes and the covers
SKILLS_PORT = int(os.environ.get("FAKE_SKILLS_PORT", "47993"))
def steer():
    f = os.path.join(proj, 'fake_ctl.json')
    try: new = json.load(open(f, encoding='utf-8')); os.remove(f)
    except Exception: return
    if "foreignUndo" in new: state["undo"].append(new.pop("foreignUndo"))
    ctl.update(new)
def plan_key(items):
    # a plan without what a second run says differently (nothing is the default any more; the avatar has its "take everything off")
    return json.dumps([{k: v for k, v in it.items() if k != "default"} for it in items if it.get("kind") != "strip"], ensure_ascii=False, sort_keys=True)
def ma_menu():
    # the menus built so far, as inspect shows them: one root, the items flat below it
    kids, n = [], 0
    for a in state["plans"]:
        for it in a.get("items", []):
            n += 1
            node = {"object": "Avatar Menu/" + "/".join(it.get("path", []) + [it["label"]]), "label": it["label"], "type": "Toggle", "value": n,
                    "toggles": [o + ("=on" if it["kind"] in ("outfit", "toggle") else "=off") for o in it.get("objects", [])]}
            if it["kind"] == "install":
                node = {"object": node["object"], "label": it["label"], "type": "InstallTarget", "installer": (it.get("objects") or [""])[0]}
            elif it["kind"] == "skin":
                node["parameter"], node["toggles"] = it.get("parameter") or "Skin_Choose", []
                if it.get("default"): node["default"] = True
            elif it["kind"] == "outfit":
                node["parameter"] = it.get("parameter") or a.get("parameter") or "Clothtoggle"
                if it.get("default"): node["default"] = True
            else: node["auto"] = True
            if it["kind"] == "strip": node["strip"] = it.get("parameter") or a.get("parameter") or "Clothtoggle"
            if not any(k["object"] == node["object"] for k in kids): kids.append(node)
    return [{"object": "Avatar Menu", "label": "Avatar Menu", "type": "MenuInstaller", "children": kids}] if kids else []
def bits():
    # as the plugin estimates them: the asset's, one per switch with a parameter of its own, one or eight per shared parameter
    shared, own = {}, 0
    for k in (ma_menu() or [{}])[0].get("children", []):
        if k.get("auto"): own += 1
        elif k.get("parameter"): shared[k["parameter"]] = shared.get(k["parameter"], 0) + 1
    return ctl["bits"] + own + sum(1 if n == 1 else 8 for n in shared.values())
def bones(): return ctl["bones"] + 12 * len(state["dressed"])
# ---------- the check-up, as Checkup.cs answers it ----------
TIERS = {"triangles": [32000, 70000, 70000, 70000], "skinnedMeshes": [1, 2, 8, 16], "meshes": [4, 8, 16, 24], "materials": [4, 8, 16, 32], "bones": [75, 150, 256, 400],
         "physBoneComponents": [4, 8, 16, 32], "physBoneTransforms": [16, 64, 128, 256], "physBoneColliders": [4, 8, 16, 32], "physBoneChecks": [32, 128, 256, 512],
         "contacts": [8, 16, 24, 32], "constraints": [100, 250, 300, 350], "constraintDepth": [20, 50, 80, 100], "animators": [1, 4, 16, 32], "lights": [0, 0, 0, 1],
         "particleSystems": [0, 4, 8, 16], "particles": [0, 300, 1000, 2500], "audioSources": [1, 4, 8, 8], "textureMemory": [40, 75, 110, 150]}
RANKS = ["Excellent", "Good", "Medium", "Poor"]
def rank(fid, v):
    for name, top in zip(RANKS, TIERS.get(fid, [])):
        if v <= top: return name
    return "VeryPoor" if fid in TIERS else ""
def checkup(a):
    n = len(state["dressed"])
    items = []
    def item(id, group, label, level, value=None, **kw):
        it = {"id": id, "group": group, "label": label, "level": level}
        if value is not None: it["value"] = value
        it.update({k: v for k, v in kw.items() if v not in (None, "", [], False)})
        items.append(it); return it
    if not state["placed"]:
        item("avatar", "limit", "模型", "fail", text="未找到", advice="请先在 Unity 中打开包含模型的场景。模型的根物体上需要有 VRC Avatar Descriptor 组件。")
        return {"prebuild": True, "target": "pc", "avatar": "", "items": items, "ms": 3}
    b, pb = bits(), bones()
    fury = bool(ctl["fury"])
    item("params", "limit", "同步参数", "fail" if b > 256 else "warn" if fury and b > 230 else "ok", b, limit=256, unit="位", atLeast=fury,
         details=[{"t": "参数资产中已有", "v": ctl["bits"], "u": "位"}] + ([{"t": "Modular Avatar 在构建时生成（估算）", "v": b - ctl["bits"], "u": "位"}] if b > ctl["bits"] else []) + ([{"t": "VRCFury 在构建时生成", "n": "无法预估"}] if fury else []),
         advice="同步参数超过 256 位，上传会被拒绝。请减少部件开关的数量（每个开关占 1 位，一组互斥的衣服占 8 位），或使用 VRCFury 的参数压缩功能。" if b > 256 else
                "模型上有 VRCFury 组件，它的参数在构建时才生成，实际位数只会更多。当前已接近 256 位的上限，上传失败时请减少部件开关，或使用 VRCFury 的参数压缩功能。" if fury and b > 230 else "")
    calc = not ctl["noCalc"]
    item("physbones", "limit", "PhysBone 组件", "fail" if pb > 256 else "ok", pb, limit=256,
         details=([{"t": "Hair", "v": ctl["bones"], "u": "个"}] if ctl["bones"] else []) + [{"t": d, "v": 12, "u": "个"} for d in state["dressed"][:7]],
         rating=rank("physBoneComponents", pb) if calc else "", ratingQuest="VeryPoor" if calc and pb > 8 else "", tiers=TIERS["physBoneComponents"] if calc else None,
         advice="PhysBone 组件超过 256 个，上传会被拒绝。隐藏的衣服同样计入：请移除不常用的衣服，或让同款不同配色的衣服共用一份物理（只装配一件，其余配色改为换色开关）。" if pb > 256 else "")
    ms = int(ctl["missing"])
    item("missing", "limit", "丢失的脚本", "fail" if ms else "ok", ms, details=[{"t": "Armature/Hips/Tail_%d" % i, "v": 1, "u": "个"} for i in range(min(ms, 20))],
         advice="这些物体上有脚本丢失的组件，说明该脚本所属的插件没有安装，带着它上传会失败或功能缺失。请在 ALCOM 或 VCC 中为工程安装对应的插件（常见的是 Modular Avatar、VRCFury）；确认不再需要时，可在 Unity 的 Inspector 中移除该组件。" if ms else "")
    mo = int(ctl["menuOver"])
    item("menu", "limit", "菜单单页的控件", "fail" if mo > 8 else "ok", mo or 6, limit=8, details=[{"t": "主菜单/衣服", "v": mo, "u": "个"}] if mo > 8 else [],
         advice="菜单的一页最多放 8 个控件，超出时无法上传。请在 Unity 中打开对应的菜单资产，把多出的控件移入子菜单。" if mo > 8 else "")
    pk = int(ctl["pink"])
    item("materials", "look", "粉色或缺失的材质", "fail" if pk else "ok", pk,
         details=[{"t": (state["dressed"] or ["Body"])[0] + "/UV%d_Skirt" % i, "n": "着色器未安装" if i % 2 == 0 else "材质槽为空", "m": "Skirt_%d" % i if i % 2 == 0 else "", "f": "lilToon" if i % 2 == 0 else ""} for i in range(min(pk, 20))],
         advice="这些材质在游戏中会显示为粉色。多数情况是着色器插件没有安装：请在 ALCOM 或 VCC 中为工程安装对应的着色器（如 lilToon、Poiyomi），待 Unity 编译完成后重新体检。材质槽为空时，请在 Unity 中为该网格重新指定材质。" if pk else "")
    for it in items[-1:]:
        for d in it.get("details", []):
            for k in [k for k, v in d.items() if v == ""]: del d[k]
    item("nomesh", "look", "没有网格的蒙皮网格", "ok", 0)
    big = int(ctl["bigTex"])
    tex = [("Body_Main", 4096, "DXT5", 21.3), ("Hair_Main", 4096, "BC7", 21.3), ("Face_Main", 2048, "BC7", 5.3), ("Dress_Normal", 2048, "BC5", 5.3), ("Dress_Main", 2048, "DXT1", 2.7)]
    tex = [(nm, 4096 if i < big else min(sz, 2048), fmt, mb if i < big or sz < 4096 else 5.3) for i, (nm, sz, fmt, mb) in enumerate(tex)]
    item("textures", "look", "4096 像素以上的贴图", "warn" if big else "ok", big, unit="张", total=23 + 6 * n, detailsTitle="占用显存最多的贴图",
         details=[{"t": nm, "n": "%d × %d　%s　%.1f MB" % (sz, sz, fmt, mb), "p": "Assets/IKUSIA/kaguya/tex/%s.png" % nm, **({"w": True} if sz >= 4096 else {})} for nm, sz, fmt, mb in tex],
         advice="分辨率达到 4096 像素的贴图占用显存较多，容易使模型的贴图显存评级变差。可在 Unity 中选中该贴图，将导入设置中的 Max Size 改为 2048。" if big else "")
    items[-1]["fixable"] = max(0, big - int(ctl["pkgTex"]))  # what the texture fix can lower
    figs = [("triangles", "三角面", 68000 + 14000 * n, None), ("skinnedMeshes", "蒙皮网格", 3 + 9 * n, None), ("meshes", "普通网格", 2, None), ("materials", "材质槽", 9 + 7 * n, None),
            ("bones", "骨骼", 180 + 96 * n, None), ("physBoneTransforms", "PhysBone 影响的骨骼", 6 * pb, None), ("physBoneColliders", "PhysBone 碰撞体", 6 + 2 * n, None),
            ("physBoneChecks", "PhysBone 碰撞检测", 20 * pb, None), ("contacts", "Contact 组件", 14, None), ("constraints", "约束", 4, None), ("constraintDepth", "约束深度", 2, None),
            ("animators", "Animator", 1, None), ("lights", "灯光", int(ctl["lights"]), None), ("particleSystems", "粒子系统", 0, None), ("particles", "粒子总数", 0, None),
            ("audioSources", "音源", 0, None), ("textureMemory", "贴图显存", round(62.5 + 18.4 * n + 16 * big, 1), "MB")]
    advice = {"lights": "实时灯光对性能影响很大，建议移除模型上的灯光。",
              "triangles": "三角面数超出 Poor 档的上限。可移除不常用的衣服和配饰，或安装 Avatar Optimizer（AAO），在构建时自动移除被遮挡的网格。",
              "skinnedMeshes": "蒙皮网格偏多。安装 Avatar Optimizer（AAO）并在模型上添加 Trace And Optimize 组件后，构建时会自动合并网格。",
              "materials": "材质槽偏多。安装 Avatar Optimizer（AAO）并在模型上添加 Trace And Optimize 组件后，构建时会自动合并相同的材质槽。",
              "bones": "骨骼偏多。每件衣服都带着一套骨骼：可移除不常用的衣服，或安装 Avatar Optimizer（AAO），在构建时自动移除无用的骨骼。"}
    worst = max(RANKS.index(rank("physBoneComponents", pb)) if rank("physBoneComponents", pb) in RANKS else 4, 0) if calc else 0
    for fid, label, v, unit in figs:
        r = rank(fid, v) if calc else ""
        if r: worst = max(worst, RANKS.index(r) if r in RANKS else 4)
        item("fig." + fid, "figure", label, "warn" if r == "VeryPoor" else "ok" if calc else "info", v, unit=unit, rating=r, ratingQuest="VeryPoor" if calc and v > (TIERS.get(fid) or [0])[0] else ("Excellent" if calc else ""),
             tiers=TIERS.get(fid) if calc else None, advice=advice.get(fid, "该项超出 Poor 档的上限，模型的整体评级因此为 Very Poor，部分玩家的性能设置会默认不显示这类模型。") if r == "VeryPoor" else "")
    item("fig.bounds", "figure", "包围盒", "ok" if calc else "info", text="0.82 × 1.46 × 0.5", unit="米", rating="Excellent" if calc else "", ratingQuest="Excellent" if calc else "")
    out = {"prebuild": True, "target": "pc", "avatar": "Kaguya_Test", "path": "Kaguya_Test", "sdkCalc": calc, "items": items, "ms": 180,
           "tools": {"modularAvatar": "1.18.3", "avatarOptimizer": False, "vrcfury": fury}, "ranks": {"pc": (RANKS + ["VeryPoor"])[worst], "quest": "VeryPoor"} if calc else {}}
    return out
# ---------- the one-click fixes, as Fixes.cs answers them ----------
def fix(a):
    kind = a.get("kind")
    if kind == "textures":
        mx = int(a.get("max") or 2048)
        if mx not in (1024, 2048): return None, "Max Size 只能降到 2048 或 1024"
        big = int(ctl["bigTex"]); pkg = min(big, int(ctl["pkgTex"])); names = ["Body_Main", "Hair_Main"][:big - pkg]
        if not names and not a.get("all") and not a.get("paths"): return None, "未指定要处理的贴图（paths），或 all: true 处理模型用到的全部 4096 像素贴图"
        rec = ""
        if names:
            fd = os.path.join(proj, 'UserSettings', 'MioVRCA', 'fixes'); os.makedirs(fd, exist_ok=True)
            rec = "UserSettings/MioVRCA/fixes/%s_%03d.json" % (time.strftime("%Y%m%d_%H%M%S"), int(time.time() * 1000) % 1000)
            json.dump({"kind": "textures", "at": int(time.time()), "max": mx, "avatar": "Kaguya_Test", "bigTex": big, "textures": [{"path": "Assets/IKUSIA/kaguya/tex/%s.png" % n, "max": 4096, "platforms": {"Standalone": {"overridden": n == "Body_Main", "max": 4096}}} for n in names]}, open(os.path.join(proj, rec), 'w'))
            ctl["bigTex"] = pkg
            if ctl["fixBreaks"]:
                ctl["fixBreaks"] = 0
                return None, "重新导入「Assets/IKUSIA/kaguya/tex/%s.png」失败：Import failed。此前已改动的贴图可在体检面板中点击「恢复」改回原设置" % names[-1]
        out = {"kind": "textures", "max": mx, "changed": len(names), "items": [{"path": "Assets/IKUSIA/kaguya/tex/%s.png" % n, "name": n, "from": 4096, "to": mx, **({"platforms": ["Standalone"]} if n == "Body_Main" else {})} for n in names],
               "skipped": ([{"t": "Assets/IKUSIA/kaguya/tex/Face_Main.png", "n": "Max Size 已不高于 %d" % mx}] if a.get("paths") else []) + [{"t": "Packages/com.example.shader/tex/Matcap_%d.png" % i, "n": "不在 Assets 文件夹内"} for i in range(pkg)]}
        if rec: out.update({"record": rec, "memoryBefore": round(21.3 * len(names), 1), "memoryAfter": round(5.3 * len(names), 1)})
        return out, ""
    if kind == "lights":
        n = int(ctl["lights"]); ctl["lights"] = 0
        if n: state["undo"].append("MioVRCA 移除灯光"); state["lightsWas"] = n
        return {"kind": "lights", "changed": n, "items": [{"t": ["Head/Lamp", "Hand_R/Torch", "Body/Glow"][i % 3], "n": "已关闭" if a.get("remove") is False else "已移除"} for i in range(n)], "undo": "MioVRCA 移除灯光" if n else ""}, ""
    if kind == "missing":
        if ctl["compileFailed"]: return None, "工程中有脚本编译错误，未执行移除：此时显示为丢失的脚本可能只是暂时无法加载（例如 Modular Avatar、VRCFury 在 SDK 更新后编译失败），移除后其设置无法找回。请先解决 Unity Console 中的编译错误，待编译完成后重新体检"
        n = int(ctl["missing"]); ctl["missing"] = 0
        if n: state["undo"].append("MioVRCA 移除丢失的脚本")
        return {"kind": "missing", "changed": n, "items": [{"t": "Armature/Hips/Tail_%d" % i, "v": 1, "u": "个"} for i in range(n)], "undo": "MioVRCA 移除丢失的脚本" if n else ""}, ""
    return None, "没有「%s」这项一键处理" % kind
def fix_revert(a):
    rec = (a.get("record") or "").replace("\\", "/")
    f = os.path.join(proj, rec)
    if not rec.startswith("UserSettings/MioVRCA/fixes/") or ".." in rec: return None, "无效的恢复记录：" + rec
    if not os.path.exists(f): return None, "恢复记录已不存在，无法恢复：" + rec
    d = json.load(open(f)); lost = [t["path"] for t in d["textures"]][:int(ctl["lostTex"])]
    if not lost: os.remove(f)  # (a texture that is no longer at its path: the record stays for another go)
    ctl["bigTex"] = int(d.get("bigTex") or 0) - len(lost)
    return {"kind": "textures", "reverted": len(d["textures"]) - len(lost), "items": [{"path": t["path"], "to": t["max"]} for t in d["textures"] if t["path"] not in lost], "missing": lost}, ""
def prefab_shot(a):
    # a picture per prefab under Temp, as PrefabShot.cs leaves them; a prefab that cannot be drawn gets a reason
    from PIL import Image, ImageDraw
    sd = os.path.join(proj, 'Temp', 'MioVRCA', 'prefabshots'); os.makedirs(sd, exist_ok=True)
    size = int(a.get("size") or 512); shots = []
    for i, p in enumerate(a.get("prefabs") or []):
        name = os.path.basename(p).replace(".prefab", "")
        if ctl["shotFail"] and ctl["shotFail"].lower() in name.lower():
            shots.append({"prefab": p, "error": "该 prefab 的材质缺少着色器（显示为粉色），请先安装对应的着色器"}); continue
        im = Image.new("RGB", (size, size), (40, 43, 61)); d = ImageDraw.Draw(im); u = size / 512.0
        hue = sum(map(ord, name)) % 5
        col = [(196, 72, 104), (88, 132, 220), (120, 190, 150), (214, 170, 96), (150, 110, 200)][hue]
        dark = tuple(int(c * 0.62) for c in col)
        d.polygon([(206 * u, 96 * u), (306 * u, 96 * u), (356 * u, 400 * u), (156 * u, 400 * u)], fill=col)
        d.polygon([(206 * u, 96 * u), (256 * u, 150 * u), (306 * u, 96 * u)], fill=(232, 232, 240))
        d.rectangle((176 * u, 300 * u, 336 * u, 318 * u), fill=dark)
        d.polygon([(206 * u, 96 * u), (150 * u, 210 * u), (182 * u, 222 * u), (220 * u, 130 * u)], fill=dark)
        d.polygon([(306 * u, 96 * u), (362 * u, 210 * u), (330 * u, 222 * u), (292 * u, 130 * u)], fill=dark)
        f = os.path.join(sd, 'f%d_%d_%s.png' % (len(state["log"]), i, "".join(c if c.isalnum() else "_" for c in name)))
        im.save(f)
        shots.append({"prefab": p, "file": f.replace(os.sep, '/'), "width": size, "height": size})
    return {"shots": shots, "size": size}
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
        if 'set_active' in self.path: state["undo"].append("Skill: " + self.path.rsplit('/', 1)[-1])
        self.out({"status": "success", "result": {"deleted": json.loads(body or '{}').get("assetPath")}})
threading.Thread(target=lambda: ThreadingHTTPServer(('127.0.0.1', SKILLS_PORT), SK).serve_forever(), daemon=True).start()
def answer(cmd, a):
    state["log"].append(cmd)
    steer()
    if cmd == "ping": return {"bridge": BRIDGE}, ""
    if ctl["compiling"] > 0: ctl["compiling"] -= 1; return None, "Unity 正在编译或导入，请稍后重试"
    if cmd == "refresh": time.sleep(0.4); return {"refreshed": True}, ""
    if cmd in ("checkup", "prefab_shot") and not ctl["oldPlugin"]:
        if ctl["playing"]: return None, "Unity 处于 Play 模式，请先退出 Play 模式再" + ("体检" if cmd == "checkup" else "生成封面")
        time.sleep(0.5)
        return (checkup(a) if cmd == "checkup" else prefab_shot(a)), ""
    if cmd in ("fix", "fix_revert") and not ctl["oldPlugin"]:
        if ctl["playing"]: return None, "Unity 处于 Play 模式，请先退出 Play 模式再修改场景"
        time.sleep(0.6)
        return fix(a) if cmd == "fix" else fix_revert(a)
    if cmd == "inspect":
        if not state["placed"]: return {"modularAvatar": "1.18.3", "vrcSdk": True, "avatars": []}, ""
        kids = [{"name": "Body", "kind": "mesh", "active": True}, {"name": "Hair", "kind": "mesh", "active": True, "prefab": "Assets/IKUSIA/kaguya/kaguya.prefab"}] + [{"name": n, "kind": "outfit", "active": True, "prefab": state["prefabOf"].get(n, "")} for n in state["dressed"]]
        av = {"name": "Kaguya_Test", "path": "Kaguya_Test", "active": True, "prefab": "Assets/IKUSIA/kaguya/kaguya.prefab", "parameterBits": bits(), "parameterBitsAsset": ctl["bits"],
              "physBones": bones(), "maMenu": ma_menu(), "children": kids}
        if ctl["ft"]: av["faceTracking"] = True
        return {"modularAvatar": "1.18.3", "vrcSdk": True, "avatars": [av]}, ""
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
        warns = [ctl["dressWarn"]] if ctl["dressWarn"] else []
        was = next((n for n, p in state["prefabOf"].items() if p == a["prefab"] and n in state["dressed"]), "")
        ex = was != ""
        if ex: name = was
        elif name in state["dressed"]:
            # another prefab's object has the name: this one is named after the folder that sets it apart
            free = name + "_" + os.path.basename(os.path.dirname(a["prefab"]))
            warns.append("模型下已有名为「%s」的物体（不是同一个 prefab），新装配的物体已命名为「%s」，菜单项请使用该名称。素材自带的动画若按原名查找物体，将不会生效" % (name, free))
            name = free
        if not ex: state["dressed"].append(name); state["undo"].append("MioVRCA 装配 " + name)
        state["prefabOf"][name] = a["prefab"]
        if bones() > 256: warns.append("模型上共有 %d 个 PhysBone（本素材占 12 个），超过 256 个的上限，上传时将校验失败" % bones())
        meshes = next((x["meshNames"] for x in prefabs() if x["path"] == a["prefab"]), MESH)
        return {"setUp": True, "existing": ex, "changed": not ex, "name": name, "active": a.get("active", True) if not ex else True, "physBones": bones(), "warnings": warns,
                "outfit": {"object": name, "meshes": [{"path": name + "/" + m, "active": True, "height": [0, 1]} for m in meshes]}}, ""
    if cmd == "place":
        time.sleep(0.4)
        info = next((x for x in prefabs() if x["path"] == a["prefab"]), None)
        if info is None: return None, "工程中未找到该 prefab：" + a["prefab"]
        if info.get("wholeAvatar"): return None, "「%s」是包含素体的完整模型，不能作为插件放置到模型下" % info["name"]
        if info.get("missingScripts"): return None, "「%s」中有 %d 个缺失的脚本：它依赖的插件尚未安装到工程中，未放置。请先按素材说明安装所需的插件（常见的有 Modular Avatar、VRCFury 或素材自带的框架包）" % (info["name"], info["missingScripts"])
        name = info["name"]
        ex = state["prefabOf"].get(name) == a["prefab"] and name in state["dressed"]
        if not ex and name in state["dressed"]: return None, "模型下已有名为「%s」的物体（不是该 prefab 的实例）。插件的动画按物体路径查找，不能改名放置：请先在 Unity 中处理同名物体后重试" % name
        if not ex: state["dressed"].append(name); state["undo"].append("MioVRCA 放置 " + name)
        state["prefabOf"][name] = a["prefab"]
        if info.get("faceTracking"): ctl["ft"] = True
        return {"existing": ex, "changed": not ex, "name": name, "object": name, "active": True, "scripts": info.get("scripts", []), "selfInstalling": bool(info.get("selfInstalling")), "vrcFury": bool(info.get("vrcFury")),
                "faceTracking": bool(info.get("faceTracking")), "menuInstallers": [name] if info.get("menuInstallers") else [], "physBones": bones(), "parameterBits": bits(), "parameterBitsAdded": 0, "warnings": []}, ""
    if cmd == "inspect_object":
        return {"object": a["path"], "meshes": [{"path": a["path"] + "/" + m, "active": True, "materials": ["mat"], "height": [0, 1], "triangles": 1000} for m in MESH]}, ""
    if cmd == "build_menu":
        time.sleep(0.6)
        for it in a.get("items", []):
            for o in it.get("objects", []):
                if "NOPE" in o: return None, "模型下未找到「%s」" % o
        open(os.path.join(proj, 'last_menu.json'), 'w', encoding='utf-8').write(json.dumps(a, ensure_ascii=False))
        before = bits()
        same = any(plan_key(p.get("items", [])) == plan_key(a.get("items", [])) for p in state["plans"])  # a plan carried out before changes nothing
        state["plans"].append(a)
        if not same: state["undo"].append("MioVRCA 生成菜单")
        warns = ["「衣服」中有 9 项，超过每页 8 项的上限，游戏中将自动出现「More」翻页，可再分一层"] if len(a["items"]) > 12 else []
        if bits() > 256: warns.append("同步参数预计 %d / 256 位（已计入 Modular Avatar 在构建时生成的参数），超出上限将导致上传失败，可减少部件开关的数量（如已安装 VRCFury，或可压缩至上限内）" % bits())
        return {"created": [] if same else ["Avatar Menu"] + ["Avatar Menu/" + "/".join(it["path"] + [it["label"]]) for it in a["items"]], "warnings": warns, "icons": 0 if same else len(a["items"]), "changed": not same,
                "parameterBits": bits(), "parameterBitsBefore": before, "parameterBitsAdded": bits() - before, "root": a.get("root") or "Avatar Menu", "parameter": a.get("parameter") or "Clothtoggle", "menu": {}}, ""
    if cmd == "undo":
        # only a step of the plugin's own (or, when the program says so, one of UnitySkills') is taken back
        top = state["undo"][-1] if state["undo"] else ""
        if not top: return None, "Unity 的撤销记录中没有可撤销的 MioVRCA 操作，未执行撤销"
        if not (top.startswith("MioVRCA") or (a.get("skills") and top.startswith("Skill: "))):
            return None, "Unity 撤销记录中最近的一步是「%s」，不是 MioVRCA 的操作。为避免撤销您自己的改动，未执行撤销。如需继续回退，请在 Unity 中按 Ctrl+Z" % top
        state["undo"].pop()
        for verb in ("MioVRCA 装配 ", "MioVRCA 放置 "):
            if top.startswith(verb) and top[len(verb):] in state["dressed"]: state["dressed"].remove(top[len(verb):])
        if top == "MioVRCA 生成菜单" and state["plans"]: state["plans"].pop()
        if top == "MioVRCA 移除灯光": ctl["lights"] = state.get("lightsWas", 0)
        return {"undone": top}, ""
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
    steer()  # the heartbeat says Play mode / compiling as soon as the test asks for it
    open(os.path.join(d, 'alive.tmp'), 'w').write(json.dumps({"bridge": BRIDGE, "pid": 1, "ma": "1.18.3", "sdk": True, "playing": bool(ctl["playing"]), "compiling": bool(ctl["busy"]), "skills": {"installed": True, "running": True, "port": SKILLS_PORT, "version": "2.8.4", "mode": "Auto"}}))
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
