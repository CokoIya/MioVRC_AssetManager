#!/usr/bin/env python3
"""Runs the page in English and in Japanese through every view, dialog and drawer it can reach, with the
fakes of test/ playing Unity, the AI service and the shops, and lists the Chinese that is left on screen.

    python3 test/i18n_crawl.py --data <a data folder to copy> [part ...] [--bin <built program>] [--out <folder for screenshots>]

    parts (all when none is named):
      crawl    en and ja: every view and dialog, a pipeline run without and with the AI, a check-up; what is
               still Chinese and is not a name, a path or a note is listed (for ja: what no entry matched)
      sweep    every text of the page's templates (test/i18n_harvest.py) through the real lookup of web/i18n.js
      checks   Chinese is untouched; typing is not disturbed; the first-run default; the choice survives a restart
      header   the header fits at 720 px on every view, with and without the update button
      perf     5,000 cards drawn with the layer off and on

--data is a data folder of the program with a scanned library in it (library.json …); without it the crawl
starts from an empty one, which leaves the library's cards and their details out. Needs playwright (chromium).
Exit code 1 when a check fails or interface text is left untranslated.
"""
import asyncio, itertools, json, os, re, shutil, socket, subprocess, sys, tempfile, time, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.environ.get('I18N_ROOT') or (os.path.dirname(HERE) if os.path.exists(os.path.join(HERE, 'fakeunity.py')) else '/tmp/wt_i18n')
sys.path.insert(0, os.path.join(ROOT, 'test'))
import i18n_harvest as harvest

ARGS = sys.argv[1:]
def opt(name, default=''):
    return ARGS[ARGS.index(name) + 1] if name in ARGS else default
PARTS = [a for i, a in enumerate(ARGS) if not a.startswith('--') and (i == 0 or ARGS[i - 1] not in ('--data', '--bin', '--out'))]
def want(p): return not PARTS or p in PARTS
RUN = tempfile.mkdtemp(prefix='i18n_crawl_')
OUT = opt('--out', os.path.join(RUN, 'shots'))
DATA_TPL = opt('--data')
BIN = opt('--bin')
PORT, BOOTH, WEB, LLM, SKILLS, VPM, JX = 47855, 47880, 47881, 47882, 47883, 47884, 47885
URL = 'http://127.0.0.1:%d/' % PORT
HAN = re.compile(r'[\u3400-\u9fff]')
OKS, BAD, PROCS = [], [], []

def check(name, cond, detail=''):
    (OKS if cond else BAD).append(name)
    print(('PASS ' if cond else 'FAIL ') + name + (('  ' + str(detail)[:600]) if detail != '' and not cond else ''), flush=True)

# ---------- the program and the fakes ----------
FAKE_FILES = ['Assets/KDress/Kaguya/Kaguya prefab/Envy cat.prefab', 'Assets/KDress/Kaguya/Kaguya prefab/Heart Catcher .prefab', 'Assets/KDress/Kaguya/Kaguya prefab/Melty Devil .prefab',
              'Assets/KDress/Kaguya/Kaguya_full.prefab', 'Assets/IKUSIA/kaguya/kaguya.prefab', 'Assets/_头发/樱发/Khaki.prefab', 'Assets/_道具/Bat/bat.prefab', 'Assets/_配饰/Glasses/glasses.prefab',
              'Assets/nHaruka/Light/LightControl.prefab', 'Assets/Panda Shop/SPS/Prefab/for Kaguya/SPS for Kaguya.prefab', 'Assets/Panda Shop/SPS/Prefab/for Plum/SPS for Plum.prefab',
              'Assets/Triturbo/Kaguya_FT/Prefabs/Eye Tracking.prefab', 'Assets/Triturbo/Kaguya_FT/Prefabs/Mouth Tracking.prefab', 'Assets/Triturbo/Kaguya_FT/Editor/FaceTrackingEditor.cs',
              'Assets/Skins/Tan/Kaguya_Tan.prefab', 'Assets/Broken/gimmick.prefab', 'Assets/Miu/SexyGlow/Materials/Body_SexyGlow.mat', 'Assets/Miu/SexyGlow/Tex/skin_N.png', 'Assets/Faces/Pack/smile.anim']

def fake_project(path):
    for f in FAKE_FILES:
        os.makedirs(os.path.dirname(os.path.join(path, f)), exist_ok=True); open(os.path.join(path, f), 'w').close()
    os.makedirs(os.path.join(path, 'ProjectSettings'), exist_ok=True); os.makedirs(os.path.join(path, 'Packages'), exist_ok=True); os.makedirs(os.path.join(path, 'Temp'), exist_ok=True)
    open(os.path.join(path, 'ProjectSettings', 'ProjectVersion.txt'), 'w').write('m_EditorVersion: 2022.3.22f1\n')
    open(os.path.join(path, 'Temp', 'UnityLockfile'), 'w').close()  # "open in Unity"

def data_dir(name, lang=None, setup=True, project=None):
    """a data folder for one run: the one given with --data, in that language, with the fake project in it"""
    d = os.path.join(RUN, name)
    shutil.rmtree(d, ignore_errors=True)
    if DATA_TPL and setup: shutil.copytree(DATA_TPL, d, symlinks=True)
    else: os.makedirs(d)
    lib = os.path.join(d, 'library.json')
    if setup:
        st = json.load(open(lib, encoding='utf-8')) if os.path.exists(lib) else {'version': 1, 'settings': {'roots': [os.path.join(RUN, 'assets')], 'setupDone': True}}
        os.makedirs(os.path.join(RUN, 'assets'), exist_ok=True)
        s = st.setdefault('settings', {})
        if lang is not None: s['lang'] = lang
        if project: s['projectRoots'] = (s.get('projectRoots') or []) + [project]
        json.dump(st, open(lib, 'w', encoding='utf-8'), ensure_ascii=False)
        bak = lib + '.bak'
        if os.path.exists(bak): os.remove(bak)
    return d

def free(port):
    s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try: s.bind(('127.0.0.1', port)); return True
    except OSError: return False
    finally: s.close()

def spawn(cmd, log, env=None):
    p = subprocess.Popen(cmd, env=dict(os.environ, **(env or {})), stdout=open(os.path.join(RUN, log), 'w'), stderr=subprocess.STDOUT, cwd=ROOT)
    PROCS.append(p); return p

def start_fakes():
    for port in (BOOTH, WEB, LLM, SKILLS, VPM, JX):
        if not free(port): sys.exit('port %d is taken' % port)
    t = os.path.join(ROOT, 'test')
    spawn([sys.executable, os.path.join(t, 'mockbooth.py'), str(BOOTH)], 'booth.log')
    spawn([sys.executable, os.path.join(t, 'mockweb.py'), str(WEB)], 'web.log', {'MOCK_JX': 'http://127.0.0.1:%d' % JX})
    spawn([sys.executable, os.path.join(t, 'mockllm.py')], 'llm.log', {'MOCK_PORT': str(LLM)})
    spawn([sys.executable, os.path.join(t, 'fakevpm.py'), str(VPM)], 'vpm.log')
    spawn([sys.executable, os.path.join(t, 'mockjinxxy.py'), str(JX)], 'jx.log')

def start_app(data, extra=None):
    for _ in range(50):  # (the one before may still be closing)
        if free(PORT): break
        time.sleep(0.2)
    else: sys.exit('port %d is taken' % PORT)
    w = 'http://127.0.0.1:%d' % WEB
    env = {'VRCLIB_HEADLESS': '1', 'VRCLIB_BOOTH_BASE': 'http://127.0.0.1:%d' % BOOTH, 'VRCLIB_BOOTH_DL': 'http://127.0.0.1:%d' % BOOTH, 'VRCLIB_PAN_BASE': w, 'VRCLIB_BOOTH_WEB': w,
           'VRCLIB_BING_BASE': w, 'VRCLIB_XY_BASE': w + '/xy', 'VRCLIB_GUMROAD_BASE': w + '/gum', 'VRCLIB_PCS_BASE': w, 'VRCLIB_BAIDU_LOGIN': w + '/mock/bdlogin',
           'VRCLIB_GITHUB_API': w, 'VRCLIB_GITHUB_SITE': w, 'VRCLIB_VPM_BASE': 'http://127.0.0.1:%d' % VPM, 'VRCLIB_JINXXY_BASE': 'http://127.0.0.1:%d' % JX,
           'VRCLIB_PICK': os.path.join(RUN, 'picked'), 'VRCLIB_WISH_FAST': '1'}
    env.update(extra or {})
    os.makedirs(os.path.join(RUN, 'picked'), exist_ok=True)
    app = spawn([BIN, '-no-window', '-no-booth', '-port', str(PORT), '-data', data], 'app.log', env)
    for _ in range(100):
        try: urllib.request.urlopen(URL + 'api/ping', timeout=1).read(); return app
        except Exception: time.sleep(0.2)
    sys.exit('the program did not start: see ' + os.path.join(RUN, 'app.log'))

def stop(p):
    if p.poll() is None:
        p.terminate()
        try: p.wait(8)
        except Exception: p.kill()
    if p in PROCS: PROCS.remove(p)

def stop_all():
    for p in list(PROCS): stop(p)

async def boot(b, errs, w=1280, h=860, locale=None, debug=True, close=True):
    ctx = await b.new_context(viewport={'width': w, 'height': h}, **({'locale': locale} if locale else {}))
    pg = await ctx.new_page()
    if debug: await pg.add_init_script("try { localStorage.setItem('vrclib.i18n.debug', '1'); } catch (e) {}")
    pg.on('pageerror', lambda e: errs.append('pageerror: ' + str(e)))
    pg.on('console', lambda m: errs.append('console: ' + m.text) if m.type == 'error' and 'Failed to load resource' not in m.text else None)
    pg.on('dialog', lambda d: (DIALOGS.append(d.message), asyncio.ensure_future(d.accept())))
    await pg.goto(URL)
    await pg.wait_for_function("typeof S !== 'undefined' && S.data && !S.data.busy", timeout=120000)
    await pg.wait_for_timeout(900)
    if close: await pg.evaluate("closeModal(true)")
    return pg
DIALOGS = []

# ---------- what is left in Chinese ----------
# every text node and translated attribute outside the containers i18n.js leaves alone
LEFT = """(() => { const HAN = /[\\u3400-\\u9fff]/, out = [];
  const where = el => { const p = []; for (let e = el; e && e.nodeType === 1 && p.length < 4; e = e.parentNode) p.unshift(e.id ? '#' + e.id : e.tagName.toLowerCase() + (typeof e.className === 'string' && e.className ? '.' + e.className.trim().split(/\\s+/)[0] : '')); return p.join('>'); };
  const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
  for (let n = w.nextNode(); n; n = w.nextNode()) {
    if (n.nodeType === 3) { if (HAN.test(n.nodeValue) && !I18N.skips(n)) out.push([n.nodeValue.replace(/\\s+/g, ' ').trim(), where(n.parentNode)]); continue; }
    if (I18N.skips(n)) continue;
    for (const a of ['title', 'placeholder', 'aria-label', 'alt']) { const v = n.getAttribute(a); if (v && HAN.test(v)) out.push([v, where(n) + '@' + a]); }
  }
  return out; })()"""
# names, paths, notes and other things that are the player's or a shop's: not interface text
DATA = """(() => { const out = new Set(), HAN = /[\\u3400-\\u9fff]/;
  const add = (v, skip) => { if (typeof v === 'string') { if (HAN.test(v)) out.add(v); } else if (Array.isArray(v)) v.forEach(x => add(x, skip)); else if (v && typeof v === 'object') for (const k in v) if (!skip || !skip.includes(k)) add(v[k], skip); };
  const d = S.data || {};
  add(d.assets, ['category', 'autoCategory', 'styles', 'autoStyles', 'shareErr', 'err', 'boothNews', 'status', 'kind']);
  add(d.settings); add(d.projects); add(d.changelog); add(d.whatsNew); add((d.update || {}).notes); add([d.dataDir, d.dlDir, (d.baidu || {}).name, (d.gumroad || {}).name]);
  add(d.overrides && Object.keys(d.overrides)); add(S.projs, ['checkup']); add(S.shop.items, ['category']); add((S.shop.detail || {}).item, ['category']);
  add((d.downloads || []).map(j => [j.name, j.path])); add((d.panJobs || []).map(j => [j.title, j.file, j.dir, j.saved, j.project]));
  if (typeof W !== 'undefined') add(W.items, ['change', 'state']); if (typeof RCP !== 'undefined') add(RCP.list); if (typeof RCP !== 'undefined' && RCP.pv) add([RCP.pv.avatar, RCP.pv.recipe, (RCP.pv.rows || []).map(r => [r.name, r.resolved, r.otherBase])]);
  if (typeof RCP !== 'undefined' && RCP.imp) add([RCP.imp.recipe.name, RCP.imp.recipe.note, (RCP.imp.needs || []).map(n => [n.name, n.version])]);
  add((PIPE.assets || []).map(a => [a.name, a.folder])); add(PIPE.extra.map(a => [a.name, a.folder]));
  add(((AI.sess || {}).steps || []).filter(s => s.kind === 'user' && !/^将所选素材|^应用装配方案/.test(s.text)).map(s => s.text));
  add((window.__data || [])); add(['天川澪']); for (const k in (typeof CK !== 'undefined' ? CK : {}).recs || {}) { const r = (CK.recs[k].record || {}); add(r.avatar); for (const it of ((r.result || {}).items || [])) add((it.details || []).map(l => [l.t, l.f])); }
  if (S.setup) add([S.setup.roots.map(c => c.path), S.setup.projects.map(c => c.path)]);
  return [...out]; })()"""

# Chinese that is meant to stay
KEPT = {'链接: https://pan.baidu.com/s/1xxxx 提取码: abcd': 'the hint of the share box: a sample of the text Baidu Netdisk gives, which is Chinese'}

class Crawl:
    def __init__(self, lang, pg):
        self.lang, self.pg, self.left, self.step, self.data, self.broke, self.kept = lang, pg, {}, '', set(), [], set()
    async def snap(self, name, shot=True, wait=350):
        pg = self.pg
        self.step = name
        await pg.wait_for_timeout(wait)
        self.data.update(await pg.evaluate(DATA))
        data = sorted(self.data, key=len, reverse=True)
        def ui(text):
            for d in data: text = text.replace(d, '')
            return HAN.search(text) is not None
        rows = []
        if self.lang == 'en': rows = [(t, w) for t, w in await pg.evaluate(LEFT)]
        rows += [(t, 'no entry') for t in await pg.evaluate("[...(I18N.misses || new Map()).keys()]")]
        for t, w in rows:
            if t in KEPT: self.kept.add(t)
            elif ui(t) and t not in self.left: self.left[t] = (name, w)
        if shot:
            os.makedirs(os.path.join(OUT, self.lang), exist_ok=True)
            await pg.screenshot(path=os.path.join(OUT, self.lang, re.sub(r'[^\w.-]+', '_', name) + '.png'))
    async def js(self, code, name=None, wait=450, shot=True):
        try: await self.pg.evaluate(code)
        except Exception as e: self.broke.append((name or code[:60], str(e).split('\n')[0][:300]))  # (the crawl's own slip: reported, and the crawl goes on)
        if name: await self.snap(name, shot=shot, wait=wait)

FAKE_JOBS = """(() => {
  const a = S.data.assets.find(x => x.packages && !x.virtual && !x.panOnly) || S.data.assets[0];
  S.data.projects = S.data.projects.length ? S.data.projects : [{name: 'P', path: '/p'}];
  const proj = S.data.projects[0].path;
  window.__jobs = [
    {key: a.key, stage: 'unpack', msg: '正在解压 a.zip', done: 1, total: 3, project: proj},
    {key: a.key, stage: 'choose', project: proj, choices: [{name: 'Kaguya', path: '/x/a', bases: ['Kaguya'], size: 1e6, pick: true}, {name: 'Plum', path: '/x/b', bases: ['Plum'], size: 2e6}]},
    {key: a.key, stage: 'done', project: proj, imported: ['a.unitypackage', 'b.unitypackage'], files: 312, tops: ['Assets/KDress', 'Assets/Miu', 'Assets/A', 'Assets/B', 'Assets/C'], kept: 4, keptPkgs: ['com.vrchat.avatars'], removed: 2, failed: ['c.rar：压缩包有密码']},
    {key: a.key, stage: 'failed', project: proj, err: '未找到 unitypackage（有压缩包解压失败：c.rar：本机未检测到解压软件（7-Zip、Bandizip、WinRAR 等），无法解压 rar、7z、分卷和带密码的压缩包）', failed: ['c.rar']},
  ];
  return a.key; })()"""

async def crawl(b, lang):
    errs = []
    proj = os.path.join(RUN, 'fakeproj_' + lang); shutil.rmtree(proj, ignore_errors=True); fake_project(proj)
    data = data_dir('data_' + lang, lang, project=proj)
    app = start_app(data)
    fu = spawn([sys.executable, os.path.join(ROOT, 'test', 'fakeunity.py'), proj], 'fakeunity_%s.log' % lang, {'FAKE_SKILLS_PORT': str(SKILLS), 'FAKE_BITS': '120', 'FAKE_BONES': '40'})
    pg = await boot(b, errs)
    c = Crawl(lang, pg)
    js, snap = c.js, c.snap
    P = json.dumps(proj)
    check(f'{lang}: the page is in that language', await pg.evaluate("[I18N.lang, I18N.on, document.documentElement.lang]") == [lang, True, lang])

    # ---- the library ----
    await snap('lib')
    await js("S.cat = '衣服'; renderSide(); renderGrid()", 'lib outfits')
    await js("S.fold.cat = true; S.fold.cloth = true; S.fold.bases = true; renderSide()", 'lib side folded')
    await js("S.fold.cat = S.fold.cloth = S.fold.bases = false; S.cat = '全部'; S.q = 'zzzz-nothing'; renderSide(); renderGrid()", 'lib no match')
    await js("S.q = ''; S.purchase = 'missing'; renderSide(); renderGrid()", 'lib not downloaded')
    await js("S.purchase = 'bought'; renderSide(); renderGrid()", 'lib purchased', shot=False)
    await js("S.purchase = 'all'; S.share = 'panonly'; renderSide(); renderGrid()", 'lib netdisk only', shot=False)
    await js("S.share = 'all'; S.usage = 'used'; renderSide(); renderGrid()", 'lib in use', shot=False)
    await js("S.usage = 'all'; S.recent = 'new'; renderSide(); renderGrid()", 'lib new', shot=False)
    await js("S.recent = ''; renderSide(); renderGrid(); document.querySelector('#btnBulk') && document.querySelector('#btnBulk').click()", 'lib bulk')
    await js("document.querySelector('#bulkAll') && document.querySelector('#bulkAll').click()", 'lib bulk all')
    await js("S.bulk = null; renderGrid(); S.data.warnings = ['未找到素材文件夹：D:/gone', '无法读取：D:/x', '未扫描链接文件夹（符号链接或目录联接）：D:/l；如需收录，请将其实际位置添加为素材文件夹', '素材库文件已损坏，已从备份恢复（备份时间 2026-10-01 10:00）；该时间之后的改动未能保留', '素材库保存失败，最近的改动尚未写入磁盘（disk full）']; renderGrid()", 'lib warnings')
    await js("""S.data.warnings = []; const t = (name, label, msg, running, extra) => Object.assign({name, label, msg, running, done: 3, total: 9, ended: Date.now() / 1000}, extra || {});
      S.data.tasks = [t('scan', '扫描文件夹', '正在扫描 D:/assets', true), t('usage', '统计工程使用情况', '正在索引工程 Kaguya', true), t('booth', '获取 Booth 信息', 'Booth 暂时限制了访问（HTTP 429），已暂停获取，稍后将自动继续', true),
        t('download', '下载已购文件', '正在解压 a.zip', true), t('pandl', '下载网盘分享', 'Dress：正在解压 a.zip', true), t('import', '导入到 Unity 工程', '正在导入 a.unitypackage', true), t('update', '软件更新', '正在下载 1.2 MB / 30 MB', true),
        t('purchase', '同步 Booth 已购', '正在获取已购列表，第 2 页（40 件）', true), t('gumroad', '同步 Gumroad 已购', '正在获取 Gumroad 已购，第 1 页（3 件）', true), t('pan', '获取网盘分享', '正在获取网盘分享', true),
        t('trans', '翻译名称', '正在翻译…', true), t('match', '匹配 Booth 封面', '', true), t('newproject', '创建基础工程', '正在安装 Modular Avatar 1.12.5', true)];
      S.data.busy = true; renderStatus()""", 'status running')
    await js("""S.data.busy = false; S.data.dlNeedLogin = true; S.data.panJobs = [{key: 'x', stage: 'login'}];
      S.data.tasks = [{name: 'booth', msg: '连接超时，可能需要代理'}, {name: 'download', msg: '完成：已下载 3 个文件', ended: Date.now() / 1000}, {name: 'purchase', msg: '同步失败：未登录，同步已取消', ended: Date.now() / 1000},
        {name: 'gumroad', msg: '完成：已同步 5 件 Gumroad 已购', ended: Date.now() / 1000}, {name: 'pandl', msg: '完成：已下载 2 个网盘分享', ended: Date.now() / 1000}, {name: 'import', msg: '已导入 2 个 unitypackage，共 30 个文件，目标工程：Kaguya', ended: Date.now() / 1000}];
      S.data.dlNeedLogin = false; renderStatus(); S.data.dlNeedLogin = true; renderStatus(); S.data.updateNewer = true; S.data.update = {version: '9.9.9', notes: '- 说明', zip: {size: 3e7}, published: '2026-10-01T00:00:00Z'}; S.updShown = '9.9.9'; renderStatus()""", 'status idle')
    await js("S.data.dlNeedLogin = false; S.data.panJobs = []; S.data.updateNewer = false; NET.fails = 5; netSync()", 'offline')
    await js("NET.fails = 0; netSync(); load()", None)
    await pg.wait_for_timeout(800)

    # ---- an asset's details, one of every kind the library has ----
    keys = await pg.evaluate("""(() => { const as = S.data.assets, pick = f => (as.find(f) || {}).key; return [
      pick(a => !a.virtual && !a.panOnly && a.packages && !a.group), pick(a => a.virtual), pick(a => a.panOnly && a.pan && (a.pan.files || []).length), pick(a => a.group && !a.virtual),
      pick(a => a.purchase && !a.virtual && a.purchase.source !== 'gumroad'), pick(a => a.purchase && a.purchase.source === 'gumroad'), pick(a => a.boothSrc === 'auto'), pick(a => !a.boothId && !a.virtual && !a.panOnly),
      pick(a => a.panNews || a.boothNews || a.newerOnBooth || a.shareErr), pick(a => a.psdCount), pick(a => a.panParent), pick(a => a.canSplit || a.splitInto), pick(a => (a.usage || []).length), pick(a => a.user && a.user.hidden),
      ].filter(Boolean); })()""")
    for i, k in enumerate(dict.fromkeys(keys)):
        await js(f"renderDrawer({json.dumps(k)})", f'asset {i}', wait=700)
        await js("document.querySelector('#drawer .dbody').scrollTop = 1e5", f'asset {i} end', shot=(i < 4), wait=150)
    first = await pg.evaluate(FAKE_JOBS)
    for i in range(4):
        await js(f"S.data.importJob = window.__jobs[{i}]; renderDrawer({json.dumps(first)})", f'import job {i}')
    await js(f"""S.data.importJob = null; const a = findAsset({json.dumps(first)});
      Object.assign(a, {{newerOnBooth: '2.0', localVer: '1.0', newerDl: '1', boothNews: '商品名称已更改，价格 ¥1,000 → ¥800，商品说明已更改，商品图片已更改', boothNewsAt: 1790000000, panNews: {{at: 1790000000, added: ['/a/b.zip'], removed: ['/a/c.zip']}}, shareErr: '分享已失效或被取消'}});
      a.booth = Object.assign({{}}, a.booth, {{err: '连接超时，可能需要代理'}}); a._card = null; GRID = null; renderGrid(); renderDrawer(a.key)""", 'asset news')
    pan = await pg.evaluate("(S.data.assets.find(a => a.panOnly && a.user.shareUrl && a.pan && (a.pan.files || []).length) || {}).key || ''")
    if pan:
        K = json.dumps(pan)
        for i, j in enumerate(["{stage: 'queued'}", "{stage: 'save', msg: '正在转存到网盘（剩余 3 项）'}", "{stage: 'download', done: 5e6, total: 2e7, files: 1, fileN: 4, speed: 1e6, file: 'a.zip', slow: true, paths: ['/a'], project: '/p/Kaguya'}",
                               "{stage: 'unpack', msg: '正在解压 a.zip'}", "{stage: 'done', msg: '已下载所选的 2 项，共 5 个文件（1.2 GB）；已跳过此前下载过的 3 个文件；部分压缩包解压失败', dir: 'D:/x', failed: ['a.rar：压缩包有密码'], saved: '/MioVRCA/x'}",
                               "{stage: 'failed', err: '百度要求输入验证码，请点击「在软件中打开分享」手动输入后重新下载'}", "{stage: 'failed', err: '转存到网盘失败（百度网盘错误 -7）'}", "{stage: 'login'}"]):
            await js(f"S.data.baidu = {{loggedIn: {str(i != 7).lower()}, name: 'mio', vip: {i % 3}}}; S.data.panJobs = [Object.assign({{key: {K}}}, {j})]; GRID = null; renderGrid(); renderDrawer({K}); renderStatus()", f'pan job {i}', shot=(i in (2, 4, 5)))
        await js(f"S.data.panJobs = []; S.panSel = new Set(); renderDrawer({K}); const b = document.querySelector('#drawer .psel'); if (b) b.click()", 'pan pick')
        await js(f"S.data.baidu = {{loggedIn: false}}; S.panSel = new Set(); renderDrawer({K})", 'pan signed out')
    virt = await pg.evaluate("(S.data.assets.find(a => a.virtual && a.purchase && (a.purchase.dls || []).length) || {}).key || ''")
    if virt:
        await js(f"""const a = findAsset({json.dumps(virt)}); S.data.downloads = a.purchase.dls.slice(0, 4).map((id, i) => ({{id, item: a.boothId, name: 'f' + i, status: ['queued', 'running', 'unpacking', 'failed'][i % 4], done: 5, total: 10, err: i % 4 === 3 ? '下载失败（HTTP 403）' : ''}}));
          GRID = null; S.purchase = 'missing'; renderSide(); renderGrid(); renderDrawer(a.key)""", 'download jobs')
        await js("S.data.downloads = []; S.purchase = 'all'; renderSide(); renderGrid()", None)
    await js("closeDrawer(); showBig(['/x.png', '/y.png'], 0, true)", 'lightbox', shot=False)
    await js("document.querySelector('#lightbox').classList.remove('on')", None)

    # ---- dialogs ----
    await js("openSettings()", 'settings')
    await js("document.querySelector('#modal .mbody').scrollTop = 1e5", 'settings end')
    await js("""S.data.overrides = {'D:/a': 'split', 'D:/b': 'ignore', 'D:/c': 'asset'}; S.data.baidu = {loggedIn: true, name: 'mio', vip: 2}; S.data.gumroad = {loggedIn: true, name: 'mio', count: 3, lastSync: 1790000000};
      S.data.purchaseSync = 1790000000; S.data.purchaseCount = 12; S.data.canShortcut = true; openSettings(); renderSide()""", 'settings signed in')
    await js("closeModal(); openPanAdd('')", 'pan add')
    await js("closeModal(); openFeedback()", 'feedback', wait=700)
    await js("S.fb.kind = '功能建议'; S.fb.err = '发送失败（HTTP 500）'; renderFeedback()", 'feedback failed')
    await js("S.fb.sent = true; S.fb.note = '已提交，但作者邮箱尚未开通接收，可能无法送达。'; renderFeedback()", 'feedback note')
    await js("S.fb.note = ''; renderFeedback()", 'feedback sent', shot=False)
    upd = "const m = document.querySelector('#modal'); m.dataset.kind = 'update'; "
    await js("closeModal(); " + upd + "S.upd = {checking: true}; renderUpdateModal(); showModal()", 'update checking', shot=False)
    await js(upd + "S.upd = {err: '无法连接 GitHub（连接超时，可能需要代理）'}; renderUpdateModal()", 'update error')
    await js(upd + "S.upd = {info: {version: '9.9.9', notes: '## 新功能\\n- **多语言**：English、日本語', zip: {size: 3e7}, published: '2026-10-01T00:00:00Z'}, newer: true}; renderUpdateModal()", 'update newer')
    await js(upd + "S.upd = {info: {version: '9.9.9', setup: {size: 3e7}}, newer: true}; S.data.tasks = [{name: 'update', msg: '更新失败：下载的文件校验未通过，可能已被篡改，已停止更新'}]; renderUpdateModal()", 'update failed')
    await js(upd + "S.upd = {info: {version: '9.9.9'}, newer: true}; S.data.tasks = []; renderUpdateModal()", 'update no package', shot=False)
    await js(upd + "S.upd = {info: {version: '9.9.9', zip: {size: 3e7}}}; S.data.tasks = [{name: 'update', running: true, msg: '正在下载 1.2 MB / 30 MB', done: 1, total: 30}]; renderUpdateModal()", 'update running', shot=False)
    await js(upd + "S.data.tasks = []; S.updRestart = true; renderUpdateModal()", 'update restarting', shot=False)
    await js(upd + "S.updRestart = false; S.upd = {info: {version: S.data.version}}; renderUpdateModal()", 'update latest')
    await js("closeModal(); openWhatsNew(S.data.version, (S.data.changelog || []).slice(0, 2))", 'whats new')
    await js("closeModal(); S.setup = {roots: [{path: 'D:/BaiduNetdiskDownload', note: '百度网盘下载', checked: true}, {path: 'D:/Downloads', note: '下载文件夹'}], projects: [{path: 'D:/VRC', note: '3 个工程', checked: true}, {path: 'D:/VRC/K', note: 'Unity 工程'}]}; renderSetup()", 'first run')
    await js("S.setup = {roots: [], projects: []}; renderSetup()", 'first run empty', shot=False)
    await js("S.setup = null; document.querySelector('#modal').dataset.kind = ''; closeModal(true)", None)

    # ---- Booth, the wish list, the other shops ----
    await js("setView('shop')", 'shop', wait=2500)
    ids = await pg.evaluate("[...document.querySelectorAll('.shopcard')].map(c => c.dataset.shop)")
    for i in ids[2:5]:
        await pg.evaluate(f"wishAdd(wishSeed({json.dumps(i)}))"); await pg.wait_for_timeout(600)
    await snap('shop wished')
    for n, i in enumerate(ids[:6]):
        await js(f"openShopItem({json.dumps(i)})", f'shop item {n}', wait=1200, shot=(n < 2))
    await js("closeDrawer(); wishOpen()", 'wish list', wait=900)
    await js("""for (const [i, x] of W.items.entries()) Object.assign(x, [{change: {kind: 'drop', old: '¥1,000', new: '¥800', at: 1790000000, var: 'Full'}, diffDir: -1, diff: '¥200', low: '¥700', hist: [{at: 1789000000, price: '¥1,000'}, {at: 1790000000, price: '¥800', state: 'off'}], vars: [{name: '', price: '¥800', off: true, hist: [{at: 1, price: '¥1'}, {at: 2, price: '¥2'}]}], note: '搭配 Kaguya', from: true, checked: 1790000000},
        {change: {kind: 'gone', at: 1790000000}, state: 'gone', err: 'Booth 返回错误（HTTP 500）', first: true}, {change: {kind: 'free', old: '¥500', new: '¥0', at: 1790000000}, priceNum: 0, bought: false, state: 'off'}][i % 3]);
      W.paused = true; renderWish(); document.querySelectorAll('.wlhist').forEach(d => d.open = true)""", 'wish list changes')
    await js("W.paused = false; W.watch = false; W.busy = true; renderWish()", 'wish list not watching', shot=False)
    await js("W.watch = true; W.busy = false; const k = W.items[0].key; wishNoteOpen(k)", 'wish note')
    await js("closeModal(); const all = W.items; W.items = []; renderWish(); W.items = all", 'wish list empty')
    await js("wishClose(); S.shop.items = []; S.shop.err = 'Booth 返回错误（HTTP 503）'; renderShopGrid()", 'shop error', shot=False)
    await js("S.shop.err = ''; S.shop.loading = true; renderShopGrid(); S.shop.loading = false; S.shop.q = 'x'; renderShopGrid()", 'shop empty', shot=False)
    await js("S.shop.q = ''; shopSearch(true); setView('xianyu'); if (window.jxGo) jxGo('xianyu')", 'xianyu', wait=900)
    for mode, st in (('native', "{open: false}"), ('window', "{open: true, url: 'https://www.goofish.com/', title: '', loading: true}"), ('native', "{open: true, url: 'https://www.goofish.com/', title: 'x', back: true}")):
        await js(f"S.data.paneMode = '{mode}'; S.web.st = {st}; document.querySelector('#webhost').dataset.k = ''; renderXySide(); renderWeb()", f'xianyu pane {mode} {st[:12]}', shot=False)
    await js("S.data.paneMode = 'native'; if (window.jxGo) { JX.site = 'jinxxy'; JX.files = [{id: '1', name: 'a.zip', status: 'running', done: 1, total: 4}, {id: '2', name: 'b.zip', status: 'done', path: 'D:/x'}, {id: '3', name: 'c.zip', status: 'failed', err: '下载已中断'}, {id: '4', name: '', status: 'saving'}]; S.web.st = {open: true, url: JX.base, files: JX.files}; document.querySelector('#webhost').dataset.k = ''; renderAll(); }", 'jinxxy')
    await js("S.data.paneMode = ''; if (window.jxGo) { document.querySelector('#webhost').dataset.k = ''; renderAll(); }", 'jinxxy no browser', shot=False)
    await js("if (typeof JX !== 'undefined') JX.site = 'xianyu'; S.data.paneMode = 'native'; S.web.open.lib = true; S.web.loaded = 'pan'; S.web.st = {open: true, url: 'https://pan.baidu.com/'}; S.view = 'lib'; S.data.baidu = {waiting: true}; renderAll()", 'netdisk page', shot=False)
    await js("S.web.loaded = 'gumroad'; S.data.gumroad = {waiting: true}; renderAll(); S.data.gumroad = {loggedIn: true, name: ''}; S.web.back = {view: 'lib', key: 'x'}; renderWebBar()", 'gumroad page', shot=False)
    await js("S.web.open.lib = false; S.web.back = null; S.data.paneMode = ''; S.view = 'shop'; S.web.open.shop = true; S.web.st = {open: true, url: 'https://booth.pm'}; S.data.paneMode = 'native'; renderAll()", 'booth page', shot=False)
    await js("S.web.open.shop = false; S.data.paneMode = ''; load()", None, wait=900)

    # ---- projects, the check-up ----
    await pg.evaluate(f"api('/api/ai/setup', {{project: {P}, on: true}})")
    await js("setView('proj')", 'projects', wait=1500)
    await js(f"ckOpenProj({P})", 'project details', wait=1500)
    await js(f"ckRun({P})", 'project checked', wait=3500)
    await js("document.querySelectorAll('#drawer details').forEach(d => d.open = true); document.querySelector('#drawer .dbody').scrollTop = 1e5", 'project checked end')
    open(os.path.join(proj, 'fake_ctl.json'), 'w').write(json.dumps({'pink': 2, 'missing': 1, 'bigTex': 3, 'menuOver': 11, 'fury': 1, 'bits': 250, 'bones': 260}))
    await js(f"ckRun({P})", 'project check fails', wait=3500)
    await js("document.querySelectorAll('#drawer details').forEach(d => d.open = true)", 'project check fails open')
    open(os.path.join(proj, 'fake_ctl.json'), 'w').write(json.dumps({'noCalc': 1, 'pink': 0, 'missing': 0, 'menuOver': 0, 'fury': 0, 'bits': 120, 'bones': 40}))
    await js(f"ckRun({P})", 'project check no calculator', wait=3500, shot=False)
    open(os.path.join(proj, 'fake_ctl.json'), 'w').write(json.dumps({'noCalc': 0}))
    await js("closeDrawer(); S.projs = []; renderProjSide(); renderProjGrid()", 'projects none', shot=False)
    await js("S.projs = null; loadProjects()", None, wait=900)

    # ---- the pipeline: without AI, then with it ----
    await pg.evaluate("setView('pipe')"); await pg.wait_for_timeout(600)
    await pg.evaluate(f"pipeSelect(pipeProject({P}))")
    await pg.wait_for_function(f"AI.proj && AI.proj.path === {P} && PIPE.assets !== null && AI.sess && AI.sess.kit && AI.sess.kit.alive", timeout=40000)
    await snap('pipeline', wait=1500)
    await js("document.querySelector('[data-ai=\"send\"]') && (document.querySelector('#ai_text').value = '', 0); pipeAction({dataset: {pipe: 'add'}}); document.querySelector('#pipe_add').value = 'x/y'; pipeAction({dataset: {pipe: 'add'}})", 'pipeline bad path', shot=False)
    await js("document.querySelector('#pipe_add').value = 'Assets/Mine/Thing'; pipeAction({dataset: {pipe: 'add'}})", 'pipeline added row', shot=False)
    await js("PIPE.extra = []; for (const a of pipeRows()) PIPE.pick[a.folder] = false; pipeAssetsDraw(); pipeStart()", 'pipeline nothing picked', shot=False)
    await js("for (const a of pipeRows()) PIPE.pick[a.folder] = true; pipeAssetsDraw(); aiRenderLive(); document.querySelector('#pipe_noai').checked = true; PIPE.noAI = true; pipeStart()", None)
    await run_done(pg); await snap('pipeline run without AI', wait=2500)
    await js("document.querySelector('#stn5').scrollIntoView(); document.querySelector('#ai_log').scrollTop = 0", 'pipeline log top')
    await js("document.querySelector('#ai_log').scrollTop = 1e5", 'pipeline log end')
    await js("document.querySelector('#ck_panel').scrollIntoView()", 'pipeline check-up', wait=2500)
    await js("document.querySelectorAll('#ck_panel details').forEach(d => d.open = true)", 'pipeline check-up open', shot=False)
    menu = json.load(open(os.path.join(proj, 'last_menu.json'), encoding='utf-8')) if os.path.exists(os.path.join(proj, 'last_menu.json')) else {}
    c.menu = menu
    await js("pipeStart()", None); await run_done(pg); await snap('pipeline second run')
    # a preset of it
    await js("recipeShow('save')", 'preset save', wait=600)
    await pg.fill('#rcp_name', 'Daily + 樱发'); await pg.fill('#rcp_note', '备注')
    await js("window.__data = ['Daily + 樱发', '备注']; document.querySelector('#modal [data-recipe=\"savego\"]').click()", 'preset list', wait=1500)
    rid = await pg.evaluate("(RCP.list[0] || {}).id || ''")
    if rid:
        await js(f"recipePreview({json.dumps(rid)})", 'preset preview', wait=2500)
        await js(f"recipeAction({{dataset: {{recipe: 'rename', id: {json.dumps(rid)}}}}})", 'preset rename', shot=False)
        await js(f"RCP.imp = {{text: '', recipe: {{name: 'x', note: 'n', base: {{name: 'Kaguya'}}, assets: [1], menus: [{{items: [1, 2]}}], created: 1790000000}}, needs: [{{name: 'Dress', kind: '衣服', version: 'Kaguya', libKey: 'k', link: 'https://booth.pm/ja/items/1'}}, {{name: 'Hair', kind: '头发'}}]}}; recipeShow('import')", 'preset import')
        await js(f"RCP.imp = null; recipeShow('list'); recipePreview({json.dumps(rid)})", None, wait=2500)
        await js("const b = document.querySelector('#modal [data-recipe=\"applygo\"]'); if (b && !b.disabled) b.click(); else closeModal()", None)
        await run_done(pg); await snap('preset applied', wait=2000)
        await js(f"recipeAction({{dataset: {{recipe: 'export', id: {json.dumps(rid)}}}}})", 'preset exported', wait=900, shot=False)
    await js("const l = RCP.list; RCP.list = []; recipeShow('list'); RCP.list = l", 'preset list empty', shot=False)
    await js("closeModal(); aiAction({dataset: {ai: 'undo'}})", 'pipeline undo', wait=2500)
    await js("covBatchStart()", 'covers batch', wait=3500)
    await js("CK.batch = {project: AI.proj.path, running: true, done: 2, total: 5, now: 'Envy cat'}; covDrawProg()", 'covers running', shot=False)
    await js("CK.batch = {project: AI.proj.path, ended: Date.now() / 1000, made: 3, failed: 1, err: 'Unity 未能渲染该 prefab', last: '该 prefab 中没有可显示的网格'}; covDrawProg()", 'covers done', shot=False)
    # the AI
    await js("AI.back = AI.proj.path; openAICfg()", 'AI service', wait=900)
    for prov in await pg.evaluate("AI.cfg.providers.map(p => p.id)"):
        await js(f"aiAction({{dataset: {{aiprov: {json.dumps(prov)}}}}})", f'AI service {prov}', shot=False)
    for mode in ('main', 'other', 'off', ''):
        await js(f"aiCfgKeep(); AI.cfgForm.vision.mode = '{mode}'; openAICfg(true)", f'AI vision {mode or "auto"}', shot=(mode == 'other'))
    r = await pg.evaluate(f"api('/api/ai/save', {{provider: 'openai', baseUrl: 'http://127.0.0.1:{LLM}', model: 'mio-large', key: 'sk-good-key-1234', vision: {{mode: '', wire: 'openai', baseUrl: '', model: ''}}}})")
    check(f'{lang}: the AI service is saved', r.get('ok'), r)
    await js("AI.cfgForm = null; openAICfg()", None, wait=900)
    await js("window.__data = (window.__data || []).concat(['好的，已经记下：只回复两个字：收到']); document.querySelector('#modal [data-ai=\"test\"]').click()", 'AI service tested', wait=2500)  # (what the fake model answers)
    await js("document.querySelector('#modal [data-ai=\"models\"]').click()", 'AI service models', wait=1800, shot=False)
    await js("const b = document.querySelector('#modal [data-ai=\"vtest\"]'); if (b) b.click()", 'AI vision tested', wait=2500, shot=False)
    await js(f"api('/api/ai/test', {{provider: 'openai', baseUrl: 'http://127.0.0.1:{LLM}', model: 'mio-large', key: 'wrong'}}).then(r => {{ const o = document.querySelector('#ai_test'); o.className = 'aitest err'; o.textContent = r.err || r.note; }})", 'AI service wrong key', wait=1500, shot=False)
    await js("aiCfgDone(); openAIPick()", 'AI pick project', shot=False)
    await js("closeModal(); aiLoadCfg().then(aiRenderLive)", None, wait=900)
    await js("aiAction({dataset: {ai: 'reset'}})", None, wait=1200)
    await js("document.querySelector('#pipe_noai').checked = false; PIPE.noAI = false; pipeStart()", None)
    await run_done(pg, 120000)
    said = "window.__data = (window.__data || []).concat(AI.sess.steps.filter(s => s.kind === 'say').map(s => s.text.split(/\\n+/).map(l => [l, l.replace(/^\\s*[-·*]\\s*/, '')])).flat(2))"  # the model's own words
    await pg.evaluate(said)
    await snap('pipeline run with AI', wait=1500)
    c.said = await pg.evaluate("AI.sess.steps.filter(s => s.kind === 'say').map(s => s.text).join('\\n')")
    await pg.fill('#ai_text', 'check for missing materials'); await js("aiSend()", None)
    await run_done(pg, 120000)
    await pg.evaluate(said)
    await snap('pipeline chat', wait=1200)
    # steps of every kind, as the program writes them
    await js("""AI.sess = Object.assign({}, AI.sess, {busy: true, changes: 2, steps: [
      {kind: 'tool', tool: 'inspect_avatar', text: '查看模型和现有菜单', ok: true, out: '模型「Kaguya」，含 12 个子物体'}, {kind: 'tool', text: '查看「Envy cat」中的网格', ok: true, out: '9 个网格'},
      {kind: 'tool', text: '查找 prefab：Assets/KDress、Assets/Hair', ok: true, out: '3 个 prefab；未找到文件夹 Assets/Hair'}, {kind: 'tool', text: '装配「Envy cat」', ok: true, out: '「Envy cat」已装配，9 个网格。注意：模型下已有另一个名为「Dress」的物体，「Dress (1)」保持原名；Modular Avatar 无法将该素材的骨架对应到模型，可能不是为该素体制作，或骨架结构特殊，目前仅放置在模型下，不会跟随模型运动'},
      {kind: 'tool', text: '放置「LightControl」', ok: true, out: '「LightControl」已放置。注意：「LightControl」不含 Modular Avatar 或 VRCFury 的安装组件，仅放置在模型下，是否生效取决于素材自身的说明'},
      {kind: 'tool', text: '生成菜单（12 项）', ok: true, out: '新建 12 项，图标 9 张。注意：尚未指定默认衣服（Clothtoggle），进入游戏时将保持场景中当前的显示状态，可将常用的一件设为默认（default）。注意：同步参数预计至少 300 / 256 位（已计入 Modular Avatar 在构建时生成的参数；模型上有 VRCFury 组件，其参数无法预估，未计入），超出上限将导致上传失败，可减少部件开关的数量（如已安装 VRCFury，或可压缩至上限内）'},
      {kind: 'tool', text: '生成菜单「Avatar Menu」（3 项）', ok: false, out: '「Jacket」已是子菜单，无法改为开关，请更换名称'}, {kind: 'tool', text: '将素体「kaguya」放入新场景', ok: true, out: '新场景 Assets/Scenes/kaguya.unity，模型「kaguya」'},
      {kind: 'tool', text: '截图检查「Envy cat」', ok: true, out: '已截图 2 张（正面、背面）'}, {kind: 'tool', text: '截图检查模型', ok: true, out: '截图 2 张，由视觉模型（mio-vision）代为描述'}, {kind: 'tool', text: '撤销上一步', ok: true, out: '已撤销：MioVRCA 装配 Envy cat'},
      {kind: 'tool', text: '上传前体检', ok: true, out: '2 项需处理，1 项建议优化'}, {kind: 'tool', text: '查找 Unity 操作：delete object', ok: true}, {kind: 'tool', text: 'Unity 操作 gameobject_delete', ok: false, out: '未获允许，未执行'},
      {kind: 'tool', text: '服务繁忙，正在重试（1/3）', ok: true, out: '已恢复'}, {kind: 'tool', text: '刷新 Unity 资源以加载新导入的文件', ok: false, out: '已中断'},
      {kind: 'ask', text: 'AI 请求在 Unity 中执行「gameobject_delete」：{"name":"x"}\\n会删除内容；会修改工程中的文件，无法通过 Ctrl+Z 撤销；UnitySkills 标注的风险等级为 high。', busy: true},
      {kind: 'ask', text: 'AI 请求在 Unity 中执行「editor_play」\\n会进入或退出 Play 模式；会触发 Unity 重新编译。', out: '已允许'},
      {kind: 'error', text: '操作中途中断。已完成的改动仍保留在场景中（尚未保存）：已装配 2 件（Envy cat、Khaki），菜单已生成或更新，另有 1 项通过 UnitySkills 进行的改动。如需回退，每点击一次「撤销上一步」或在 Unity 中按一次 Ctrl+Z 撤销一步。\\n中断原因：DeepSeek：API Key 不正确或已失效，或无权使用该模型（HTTP 401）。服务返回：invalid key'},
      {kind: 'error', text: 'AI 已连续执行 40 步仍未完成，已暂停：请检查 Unity 中的结果，再告知 AI 后续操作'}, {kind: 'error', text: '当前 AI 模型不接受图片，截图未发送给 AI（截图仍显示在上方记录中）。如需 AI 检查画面，请在「AI 服务」的「识图」中配置视觉模型。'},
      {kind: 'error', text: 'Unity 未响应，可能正在编译、导入，或有对话框等待操作。请切换到 Unity 处理后重试'}]}); clearTimeout(AI.timer); aiRenderLive()""", 'pipeline steps', wait=600)
    await js("""AI.sess = {busy: false, steps: [], kit: {pipeline: false, running: false, editor: 'x'}}; AI.cfg = Object.assign({}, AI.cfg, {ready: false}); clearTimeout(AI.timer); aiRenderLive(); renderPipeSide()""", 'pipeline nothing installed')
    await js("""AI.sess = {busy: false, steps: [], kit: {pipeline: true, pipelineOld: true, alive: true, skills: 'own', skillsVer: '2.8.4', skillsOn: true, port: 8090, mode: 'Auto', running: true, hint: 'Unity 处于 Play 模式，请先退出 Play 模式，再由 AI 修改场景'}};
      AI.cfg = Object.assign({}, AI.cfg, {ready: true, looks: 'other', eyeModel: 'mio-vision', eyeOwn: true}); aiRenderLive(); PIPE.tool = {unity: false, unityWant: '2022.3.22f1', alcom: false, alcomSeen: true, vcc: false, hubInstallLink: 'unityhub://x', plugins: [], bases: []}; renderPipeSide()""", 'pipeline old plugin', shot=False)
    for looks in ('main', 'unknown', 'none', 'off'):
        await js(f"AI.cfg = Object.assign({{}}, AI.cfg, {{looks: '{looks}', eyeOwn: false}}); AI.sess.kit = {{pipeline: true, alive: true, skills: 'ours', skillsOn: false}}; aiRenderLive()", f'pipeline looks {looks}', shot=False)
    await js("PIPE.tool = null; AI.proj = null; document.querySelector('#grid')._pipe = null; renderPipeSide(); renderPipeMain()", 'pipeline no project', shot=False)
    await js("pipeEnter()", None, wait=1500)

    # ---- a new starter project ----
    await js("openNewProject()", 'new project', wait=1500)
    stages = ['prepare', 'repos', 'packages', 'settings', 'base', 'kit']
    for i, stg in enumerate(stages):
        await js(f"S.data.newProject = {{stage: '{stg}', msg: {json.dumps(['正在准备', '正在读取仓库 VRChat 官方', '正在安装 Modular Avatar 1.12.5', '正在写入工程设置', '正在导入素体', '正在安装 Unity 插件并打开 Unity'][i])}, done: {i}, total: 6, name: 'K', path: 'D:/K'}}; renderNP()", f'new project {stg}', shot=(i == 2))
    await js("S.data.importJob = window.__jobs[1]; S.data.newProject = {stage: 'base', msg: '正在导入素体', name: 'K', path: 'D:/K'}; renderNP()", 'new project choose', shot=False)
    await js("""S.data.importJob = null; S.data.newProject = {stage: 'done', name: 'K', path: 'D:/K', packages: ['com.vrchat.avatars 3.7.0', 'nadena.dev.modular-avatar 1.12.5'],
      notes: ['工程设置来自 VRChat 官方 Avatar 模板', '已加入 ALCOM / VCC 的工程列表', '未安装 lilToon 1.8：仓库中未找到 jp.lilxyzw.liltoon。可稍后在 ALCOM / VCC 中安装', '仓库信息使用本机缓存：VRChat 官方', '部分插件仓库无法读取：VRChat 精选（连接超时，可能需要代理）', 'Unity 插件：Unity 启动失败：exec failed', '本机未安装 Unity 2022.3.22f1，请通过 Unity Hub 安装该版本后再打开工程', 'VRChat 官方模板不可用，已使用最小工程设置（线性颜色空间等），其余设置将在 VRChat SDK 打开时补齐']}; renderNP()""", 'new project done')
    await js("S.data.newProject = {stage: 'failed', name: 'K', path: 'D:/K', err: '无法读取任何插件仓库：VRChat 官方（连接超时，可能需要代理）。请检查网络（如需代理，可在设置中填写），或先在 ALCOM / VCC 中刷新仓库', notes: ['工程设置来自 VCC 的 Avatar 模板']}; renderNP()", 'new project failed')
    await js("S.data.newProject = null; PIPE.npForm = null; closeModal()", None)

    # ---- notices and questions, as the page words them ----
    await js("""(async () => { for (const m of ['已保存，已同步到其余 2 个版本', '新增 3 个素材（衣服、头发），2 个有更新', '2 个素材有更新', '已复制提取码 x7k2', '已加入下载队列：a.zip、b.zip', '操作失败：程序返回错误 500（/api/x）', '已将 3 个移至「衣服」', '已添加「可爱」', '已隐藏 2 个，可在左侧勾选「显示已隐藏」查看',
      '愿望单商品降价：Dress　¥1,000 → ¥800', '已导出：x.json（点击打开所在文件夹）', '体检完成：2 项需处理', 'Gumroad 已登录：mio，正在同步已购', '百度网盘已登录：mio', '已撤销：上一步', '创建失败：仅支持 Windows', '正在使用 Unity 2022.3.22f1 打开 Kaguya', '已更新到 9.9.9']) { toast(m); await new Promise(r => setTimeout(r, 30)); } })()""", 'toasts', shot=False)
    for q in ["`为「K」开启封面？\\n\\n将在该工程的 Packages 中安装封面插件，在打开工程或保存场景时自动截取模型正面图作为封面。\\n插件仅在 Unity 编辑器中运行，不修改场景，也不会随模型上传。`", "'排除这 3 个素材所在的文件夹？后续扫描将跳过这些文件夹，文件不会被删除，可在设置的「手动调整」中恢复。'",
              "'将「D:/a」拆分为多个素材？' + '可在设置的「手动调整」中恢复。'", "'排除「D:/a」？后续扫描将跳过该文件夹，文件不会被删除。' + '可在设置的「手动调整」中恢复。'", "'下载这 3 件已购商品？文件将保存到 D:/dl。'", "'从愿望单移除该商品？其价格记录将一并删除。\\n\\nDress'",
              "`为「K」安装 Unity 插件？\\n\\n将在该工程的 Packages 中安装两个编辑器插件：\\n· MioVRCA 改模流水线：放置素体、装配素材、生成菜单和图标\\n· UnitySkills 2.8.4（开源，MIT）：为 AI 提供更多 Unity 操作能力\\n\\n插件仅在 Unity 编辑器中运行，不会随模型上传。安装后将打开 Unity，首次编译约需一至两分钟。\\n可随时在此处点击「移除」。`",
              "`从「K」移除 Unity 插件？\\n\\n将移除由本软件安装的插件（工程自带的 UnitySkills 不受影响）。已装配的素材和已生成的菜单将保留，它们仅依赖 Modular Avatar。`", "'删除方案「x」？已装配到工程中的内容不受影响。'"]:
        await pg.evaluate(f"confirm({q})")
    c.dialogs = list(DIALOGS); del DIALOGS[:]
    for m in c.dialogs:
        for line in m.split('\n'):
            t = line
            for d in sorted(c.data, key=len, reverse=True): t = t.replace(d, '')
            if lang == 'en' and HAN.search(t) and line not in c.left: c.left[line] = ('confirm()', 'dialog')
    await snap('end', shot=False)
    c.errs = errs
    await pg.context.close()
    stop(fu); stop(app)
    return c

async def run_done(pg, timeout=90000):
    try: await pg.wait_for_function("AI.sess && AI.sess.busy", timeout=8000)
    except Exception: pass
    await pg.wait_for_function("AI.sess && !AI.sess.busy && (AI.sess.steps || []).length > 0", timeout=timeout)
    await pg.wait_for_timeout(1500)

async def part_crawl(b):
    start_fakes()
    for lang in ('en', 'ja'):
        c = await crawl(b, lang)
        print(f'\n== {lang}: Chinese left in the interface: {len(c.left)}')
        for t, (step, where) in sorted(c.left.items(), key=lambda kv: kv[1]):
            print(f'   [{step}] {where}  {t[:200]}')
        for t in sorted(c.kept): print(f'   kept in Chinese: {t}  ({KEPT[t]})')
        check(f'{lang}: nothing of the interface is left in Chinese', not c.left, len(c.left))
        check(f'{lang}: no page errors', not c.errs, c.errs[:5])
        check(f'{lang}: every step of the crawl ran', not c.broke, c.broke)
        check(f'{lang}: confirm() asks in that language', len(c.dialogs) > 8 and all(not HAN.search(m) if lang == 'en' else re.search(r'[\u3040-\u30ff]', m) for m in c.dialogs), c.dialogs[:2])
        labels = [it.get('label', '') for it in (getattr(c, 'menu', {}) or {}).get('items', [])] if isinstance(getattr(c, 'menu', None), dict) else []
        want_strip = {'en': 'Take all off', 'ja': '全部脱ぐ'}[lang]
        print(f'   menu written into the avatar: {labels[:14]}')
        check(f'{lang}: the menu written into the avatar is in that language', want_strip in labels and '一键脱光' not in labels, labels)
        said = getattr(c, 'said', '')
        check(f'{lang}: the AI was told to answer in that language', bool(said), said[:80])
    stop_all()

# ---------- the page's own texts through the real lookup ----------
async def part_sweep(b):
    harvest.load_rules()
    ui, go, cs = harvest.harvest()
    values = ['7', 'Abc', '10月3日 22:31']  # what a text's slots are filled with: a text counts when some filling translates
    for lang in ('en', 'ja'):
        data = data_dir('sweep_' + lang, lang)
        app = start_app(data)
        errs = []
        pg = await boot(b, errs)
        D = harvest.Dict(harvest.read_dict(lang))
        for name, rows in (('UI templates', ui), ('Go messages', go), ('C# messages', cs)):
            texts = sorted({t for _, t in rows})
            whole = [t for t in texts if D.known(t) and D.exact.get(harvest.norm(t)) != '']  # (a piece of a sentence has no translation of its own)
            fills = []
            for t in whole:
                n = t.count(harvest.SLOT)
                combos = itertools.product(values, repeat=n) if 0 < n <= 3 else [[v] * n for v in (values if n else values[:1])]
                for combo in combos:
                    it = iter(combo); fills.append((t, re.sub(harvest.SLOT, lambda m: next(it), t)))
            out = await pg.evaluate("ts => ts.map(t => { I18N.misses.clear(); const o = I18N.t(t); return I18N.misses.size ? t : o === t ? '=' : o; })", [f for _, f in fills])
            out = [f if (o == '=' and lang == 'en') else o for (_, f), o in zip(fills, out)]  # '=': known, and written the same (Japanese)
            same = lambda s: D.exact.get(harvest.norm(s).rstrip('。')) == harvest.norm(s).rstrip('。')  # an entry that keeps the text: written alike in Japanese, a sample of a Chinese share text
            good = {t for (t, f), o in zip(fills, out) if same(f) or (o != f and not (lang == 'en' and HAN.search(o.replace('天川澪', ''))))}
            joined = ['」，已装配', '「\x01」\x01，\x01 个网格']  # halves the program joins: what they make is in the crawl's steps
            bad = [t for t in whole if t not in good and t.strip() not in joined]
            print(f'== {lang} {name}: {len(whole)} of {len(texts)} texts are whole sentences; {len(bad)} not translated by the page')
            for t in bad[:60]: print('   ' + t.replace(harvest.SLOT, '{}')[:160])
            check(f'{lang}: {name} translate in the page as the harvest says', not bad, len(bad))
        await pg.context.close(); stop(app)

# ---------- checks ----------
VIEWS = ['lib', 'shop', 'proj', 'pipe', 'xianyu']
async def part_checks(b):
    start_fakes()
    # Chinese: the layer does nothing
    data = data_dir('data_zh', '')
    app = start_app(data)
    errs, reqs = [], []
    pg = await boot(b, errs)
    pg.on('request', lambda r: reqs.append(r.url))
    await pg.reload(); await pg.wait_for_function("typeof S !== 'undefined' && S.data && !S.data.busy"); await pg.wait_for_timeout(800)
    st = await pg.evaluate("({on: I18N.on, lang: I18N.lang, html: document.documentElement.lang, native: /\\[native code\\]/.test(Function.prototype.toString.call(window.confirm)), misses: I18N.misses, t: I18N.t('设置')})")
    check('zh: the layer is not running', st == {'on': False, 'lang': '', 'html': 'zh-CN', 'native': True, 'misses': None, 't': '设置'}, st)
    check('zh: no dictionary and no extra style sheet is asked for', not [u for u in reqs if 'i18n-' in u or 'i18n.css' in u], [u for u in reqs if 'i18n' in u])
    async def dump(page):
        out = {}
        for v in VIEWS:
            await page.evaluate(f"setView('{v}')"); await page.wait_for_timeout({'shop': 1300, 'pipe': 2500}.get(v, 600))  # (the pipeline page fills in as its answers come)
            out[v] = await page.evaluate("[document.querySelector('header.top').outerHTML, document.querySelector('#side').innerHTML, document.querySelector('#resultbar').innerHTML, document.querySelector('#grid').innerHTML, document.querySelector('#status').innerHTML].join('\\n')")
        await page.evaluate("setView('lib'); openSettings()"); await page.wait_for_timeout(300)
        out['langbox'] = await page.evaluate("(() => { const r = document.querySelector('#modal .mbody').firstElementChild; return r && r.querySelector('#i18n_lang') ? r.textContent.replace(/\\s+/g, ' ') : ''; })()")
        out['settings'] = await page.evaluate("(() => { const m = document.querySelector('#modal').cloneNode(true), s = m.querySelector('#i18n_lang'); if (s) s.closest('.row').remove(); return m.innerHTML; })()")
        await page.evaluate("closeModal()")
        return out
    with_layer = await dump(pg)
    ctx2 = await b.new_context(viewport={'width': 1280, 'height': 860})
    pg2 = await ctx2.new_page()
    await pg2.route('**/i18n.js', lambda r: r.fulfill(status=200, content_type='text/javascript', body=''))
    await pg2.goto(URL); await pg2.wait_for_function("typeof S !== 'undefined' && S.data && !S.data.busy"); await pg2.wait_for_timeout(900); await pg2.evaluate("closeModal(true)")
    without = await dump(pg2)
    norm = lambda s: re.sub(r'上次扫描 [^<]*|rev=\d+|\bt=\d+', '', s)
    def differ(a, b):
        a, b = norm(a), norm(b)
        i = next((i for i, (x, y) in enumerate(zip(a, b)) if x != y), min(len(a), len(b)))
        return '' if a == b else 'at %d: …%s… / …%s…' % (i, a[max(0, i - 60):i + 80], b[max(0, i - 60):i + 80])
    for v in VIEWS + ['settings']:
        d = differ(with_layer[v], without[v])
        check(f'zh: {v} is byte for byte what it is without the layer' + (' (the language box apart)' if v == 'settings' else ''), not d, d)
    box = with_layer['langbox']
    check('zh: the language box is the first thing in the settings, named in three languages', all(x in box for x in ('语言 / Language / 言語', '简体中文', 'English', '日本語')) and without['langbox'] == '', box)
    check('zh: no page errors', not errs, errs[:5])
    await ctx2.close()

    # the choice: made in the settings, kept over a restart
    await pg.evaluate("openSettings()"); await pg.wait_for_timeout(300)
    await pg.select_option('#i18n_lang', 'en')
    await pg.wait_for_function("typeof I18N !== 'undefined' && I18N.lang === 'en' && I18N.on && typeof S !== 'undefined' && S.data", timeout=20000); await pg.wait_for_timeout(900)
    check('choosing English reloads the page in English', await pg.inner_text('#vLib') == 'Library' and await pg.get_attribute('#btnSettings', 'title') == 'Settings', await pg.inner_text('#vLib'))
    saved = json.load(open(os.path.join(data, 'library.json'), encoding='utf-8'))['settings']
    check('… and it is in the settings file, with the rest of the settings as they were', saved.get('lang') == 'en' and saved.get('setupDone') is True and len(saved.get('roots') or []) > 0, saved.get('lang'))
    await pg.context.close(); stop(app)
    app = start_app(data)
    pg = await boot(b, errs)
    check('after a restart it is still English', await pg.evaluate("I18N.lang") == 'en' and await pg.inner_text('#vProj') == 'Projects')

    # typing while the page is refreshed under it
    proj = os.path.join(RUN, 'fakeproj_type'); shutil.rmtree(proj, ignore_errors=True); fake_project(proj)
    r = await pg.evaluate(f"api('/api/import/project', {{path: {json.dumps(proj)}}})")
    await pg.evaluate("load()"); await pg.wait_for_timeout(600)
    await pg.evaluate("setView('pipe')"); await pg.wait_for_timeout(700)
    await pg.evaluate(f"pipeSelect(pipeProject({json.dumps(proj)}))"); await pg.wait_for_selector('#ai_text', timeout=20000); await pg.wait_for_timeout(1200)
    text = '把鞋子的开关改名为“凉鞋” and 设置'
    await pg.click('#ai_text'); await pg.keyboard.type(text, delay=25)
    await pg.keyboard.press('Home'); [await pg.keyboard.press('ArrowRight') for _ in range(3)]
    await pg.evaluate("load(); aiPoll(); renderAll()"); await pg.wait_for_timeout(3600)  # polls, state refreshes
    await pg.keyboard.type('XY', delay=40)
    v = await pg.evaluate("({v: document.querySelector('#ai_text').value, at: document.querySelector('#ai_text').selectionStart, focus: document.activeElement.id, ph: document.querySelector('#ai_text').placeholder})")
    check('typing in the pipeline box is not disturbed: text, caret and focus stay', v['v'] == text[:3] + 'XY' + text[3:] and v['at'] == 5 and v['focus'] == 'ai_text', v)
    check('… its placeholder is translated, its value is not', not HAN.search(v['ph']) and '设置' in v['v'], v['ph'][:60])
    await pg.fill('#pipe_hier', '主菜单 > 衣服 > {素材}'); await pg.fill('#pipe_add', 'Assets/店铺/衣服'); await pg.wait_for_timeout(3200)
    v = await pg.evaluate("[document.querySelector('#pipe_hier').value, document.querySelector('#pipe_add').value]")
    check('… nor the layout box and the path box', v == ['主菜单 > 衣服 > {素材}', 'Assets/店铺/衣服'], v)
    await pg.evaluate("PIPE.kind = {}; pipeAssetsDraw()")
    sel = await pg.evaluate("(() => { const s = document.querySelector('#pipe_assets select[data-pkind]'); return s ? {values: [...s.options].map(o => o.value), texts: [...s.options].map(o => o.text), value: s.value} : null; })()")
    check('a kind box shows English and still answers in Chinese', sel is not None and sel['values'][:3] == ['素体', '衣服', '头发'] and sel['texts'][:3] == ['Bases', 'Outfits', 'Hair'] and HAN.search(sel['value']) is not None, sel)
    hi = await pg.evaluate("aiLoadCfg().then(c => [c.hierarchy, c.defaultHierarchy])")
    check('the default menu layout is written in English', hi[1] == 'Main menu > {category} > {asset} > {toggles}', hi)
    await pg.context.close(); stop(app)

    # a fresh install: by the system's language, on the first-run screen
    for locale, wantl, word in (('ja-JP', 'ja', 'フォルダーを選択'), ('en-US', 'en', 'Choose folders'), ('zh-CN', '', '选择文件夹'), ('zh-TW', '', '选择文件夹'), ('fr-FR', 'en', 'Choose folders')):
        d = data_dir('fresh', setup=False)
        app = start_app(d, {'VRCLIB_DETECT_ROOTS': os.path.join(RUN, 'assets'), 'VRCLIB_DETECT_PROJECTS': ''})
        pg = await boot(b, errs, locale=locale, close=False)
        await pg.wait_for_selector('#modal.on #i18n_lang', timeout=20000); await pg.wait_for_timeout(500)
        got = await pg.evaluate("({lang: I18N.lang, fresh: I18N.fresh, head: document.querySelector('#modal .mhead').textContent, sel: document.querySelector('#i18n_lang').value, given: window.LANG})")
        check(f'fresh install, system in {locale}: the first-run screen is in {wantl or "zh"}', got == {'lang': wantl, 'fresh': True, 'head': word, 'sel': wantl, 'given': '?'}, got)
        if locale == 'ja-JP':
            await pg.screenshot(path=os.path.join(OUT, 'first_run_ja.png'))
            os.makedirs(os.path.join(RUN, 'assets', 'Dress'), exist_ok=True)
            await pg.evaluate(f"setupAdd('roots', {json.dumps(os.path.join(RUN, 'assets'))}, true); finishSetup()"); await pg.wait_for_timeout(1500)
            s = json.load(open(os.path.join(d, 'library.json'), encoding='utf-8'))['settings']
            check('… finishing the first-run screen saves that language', s.get('lang') == 'ja' and s.get('setupDone') is True, s.get('lang'))
            await pg.reload(); await pg.wait_for_function("typeof S !== 'undefined' && S.data"); await pg.wait_for_timeout(600)
            check('… and the page starts in it from then on', await pg.evaluate("[window.LANG, I18N.lang, I18N.fresh]") == ['ja', 'ja', False])
        if locale == 'en-US':  # picked by hand on the first-run screen
            await pg.select_option('#i18n_lang', '')
            await pg.wait_for_function("typeof I18N !== 'undefined' && I18N.lang === '' && document.querySelector('#modal.on .mhead')", timeout=20000); await pg.wait_for_timeout(600)
            got = await pg.evaluate("[document.querySelector('#modal .mhead').textContent, document.querySelector('#i18n_lang').value, I18N.on]")
            check('… choosing 简体中文 there turns the screen Chinese', got == ['选择文件夹', '', False], got)
        await pg.context.close(); stop(app)
    # an install from before: stays Chinese whatever the system's language
    d = data_dir('old', None)
    app = start_app(d)
    pg = await boot(b, errs, locale='en-US')
    check('an existing install stays Chinese on an English system', await pg.evaluate("[window.LANG, I18N.lang, I18N.on]") == ['', '', False] and await pg.inner_text('#vLib') == '素材库')
    await pg.context.close(); stop(app)
    check('no page errors in the checks', not errs, errs[:5])
    stop_all()

# ---------- the header at 720 px ----------
FIT = """(() => { const top = document.querySelector('header.top'); let right = 0; const clipped = [];
  for (const el of top.querySelectorAll('button, .search, select')) { const r = el.getBoundingClientRect(); if (!r.width) continue; right = Math.max(right, r.right);
    if (r.right > innerWidth + 0.5 || r.left < -0.5) clipped.push((el.id || el.title || el.innerText || el.className).trim().slice(0, 12)); }
  const wrapped = [...top.querySelectorAll('.views button, .btn span')].filter(e => e.getBoundingClientRect().height > 30).map(e => e.textContent);
  return {upd: document.querySelector('#btnUpdate').getBoundingClientRect().width > 0, right: Math.round(right), scroll: top.scrollWidth - top.clientWidth, search: Math.round(document.querySelector('.search').getBoundingClientRect().width), clipped, wrapped, h: Math.round(top.getBoundingClientRect().height)}; })()"""
WIDTHS = sorted(set(range(720, 1461, 20)) | {1401, 1301, 1281, 1181, 1101, 1061, 1041, 1001, 981, 961, 941, 901, 861, 841, 821, 801, 781, 761, 741}, reverse=True)  # every step of the style sheets, from both sides
async def part_header(b):
    for lang in ('', 'en', 'ja'):
        data = data_dir('hdr_' + (lang or 'zh'), lang)
        app = start_app(data)
        errs = []
        pg = await boot(b, errs)
        await pg.evaluate("clearTimeout(pollTimer); poll = () => {}; load = async () => {}")  # (the page's own refresh would take the update button away again)
        print(f'== header, {lang or "zh"}' + ('' if lang else ' (as it is without the layer: for comparison, not checked)'))
        for upd in (False, True):
            for v in VIEWS + ['wish', 'jinxxy']:
                await pg.evaluate({'wish': "setView('shop'); wishOpen()", 'jinxxy': "window.jxGo && jxGo('jinxxy')"}.get(v, f"if (typeof JX !== 'undefined') JX.site = 'xianyu'; if (typeof W !== 'undefined') W.on = false; setView('{v}'); renderAll()")); await pg.wait_for_timeout(500)
                line, least = [], (9999, 0)
                for w in WIDTHS:
                    await pg.set_viewport_size({'width': w, 'height': 760})
                    # (again each time: the page's own refresh puts the state back)
                    await pg.evaluate("S.data.updateNewer = %s; S.data.update = { version: '1.7.6' }; S.data.settings.skipVersion = ''; S.updShown = '1.7.6'; renderStatus()" % ('true' if upd else 'false'))
                    await pg.wait_for_timeout(120)
                    r = await pg.evaluate(FIT)
                    ok = not r['clipped'] and r['scroll'] <= 0 and not r['wrapped'] and r['upd'] == upd
                    if lang and (not ok or w == 720): check(f'header fits: {lang or "zh"} {v} {w}{" +update" if upd else ""}', ok, r)
                    if w in (1440, 1180, 1000, 860, 800, 760, 720): line.append(f"{w}:{r['right']}/{r['search']}")
                    least = min(least, (r['search'], w))
                    if w == 720 and lang: os.makedirs(os.path.join(OUT, lang), exist_ok=True); await pg.locator('header.top').screenshot(path=os.path.join(OUT, lang, f'header_{v}_720{"_upd" if upd else ""}.png'))
                print(f"   {v}{' +update' if upd else ''}: width:right edge/search box  " + ' '.join(line) + f'   narrowest search box {least[0]} px (at {least[1]})')
        await pg.context.close(); stop(app)

# ---------- 5,000 cards ----------
PERF = """async n => { const base = S.data.assets.filter(a => !a.virtual && !a.splitInto), big = [];
  for (let i = 0; big.length < n; i++) { const a = base[i % base.length]; big.push(Object.assign({}, a, {key: a.key + '#' + i, group: '', _card: null, _q: undefined, _hay: undefined})); }
  S.data.assets = big; S.byKey = new Map(big.map(a => [a.key, a])); S.groups = new Map();
  const frame = () => new Promise(r => requestAnimationFrame(() => setTimeout(r, 0)));
  const times = [], scripts = [], layer = [];
  for (let k = 0; k < 8; k++) { gridPut(''); for (const a of big) a._card = null; await frame();
    const t0 = performance.now(); renderSide(); renderGrid(); const t1 = performance.now();
    await null; const tl = performance.now(); // (the observer's turn comes before this line goes on: its time by itself)
    await frame(); const t2 = performance.now();
    if (k) { times.push(t2 - t0); scripts.push(t1 - t0); layer.push(tl - t1); } }
  const med = a => a.slice().sort((x, y) => x - y)[a.length >> 1];
  return {cards: document.querySelectorAll('#grid .card').length, nodes: document.querySelectorAll('#grid *').length, toPaint: Math.round(med(times)), script: Math.round(med(scripts)), layer: Math.round(med(layer)), cats: document.querySelectorAll('#grid .cat').length,
    han: [...document.querySelectorAll('#grid .cat, #grid .act')].filter(e => /^\\s*(衣服|头发|配饰|道具|材质|面捕|插件|动作|音效|字体|其他|导入|详情)/.test(e.textContent) || /^(打开|导入|在 Booth|详情)/.test(e.title || '')).length}; }"""  # (the cards' own words, as neither language keeps them)
async def part_perf(b):
    res = {}
    for lang in ('', 'en', 'ja'):
        data = data_dir('perf_' + (lang or 'zh'), lang)
        app = start_app(data)
        pg = await boot(b, [], debug=False)
        await pg.evaluate("clearTimeout(pollTimer); poll = () => {}; load = async () => {}")
        res[lang] = await pg.evaluate(PERF, 5000)
        await pg.context.close(); stop(app)
    z = res['']
    print('== 5,000 cards: script time of renderSide + renderGrid, and time until the frame after it is drawn (median of 7)')
    for lang in ('', 'en', 'ja'):
        r = res[lang]
        print(f"   {lang or 'zh (layer off)':15s} {r['cards']} cards, {r['nodes']} elements: script {r['script']} ms, the layer's turn {r['layer']} ms, to paint {r['toPaint']} ms" + ('' if not lang else f"  ({r['toPaint'] - z['toPaint']:+d} ms, {100 * (r['toPaint'] - z['toPaint']) // max(1, z['toPaint'])}%)   cards' labels and buttons left in Chinese: {r['han']}"))
    check('perf: 5,000 cards are drawn', all(r['cards'] == 5000 for r in res.values()), {k: r['cards'] for k, r in res.items()})
    check('perf: every card is translated when the frame is painted', res['en']['han'] == 0 and res['ja']['han'] == 0 and res['']['han'] > 5000, {k: r['han'] for k, r in res.items()})
    check('perf: the layer adds less than half again to a full redraw', all(res[l]['toPaint'] < z['toPaint'] * 1.5 + 50 for l in ('en', 'ja')), {k: r['toPaint'] for k, r in res.items()})

async def main():
    global BIN
    from playwright.async_api import async_playwright
    if not BIN:
        BIN = os.path.join(RUN, 'miovrca')
        subprocess.check_call(['go', 'build', '-o', BIN, './cmd/miovrca'], cwd=ROOT)
    os.makedirs(OUT, exist_ok=True)
    print('run folder:', RUN, ' screenshots:', OUT, flush=True)
    try:
        async with async_playwright() as p:
            b = await p.chromium.launch()
            if want('crawl'): await part_crawl(b)
            if want('sweep'): await part_sweep(b)
            if want('checks'): await part_checks(b)
            if want('header'): await part_header(b)
            if want('perf'): await part_perf(b)
            await b.close()
    finally:
        stop_all()
    print(f'\n{len(OKS)} passed, {len(BAD)} failed'); [print('  FAILED:', x) for x in BAD]
    return 1 if BAD else 0

if __name__ == '__main__':
    sys.exit(asyncio.run(main()))
