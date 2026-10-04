#!/usr/bin/env python3
"""Which Chinese texts of the interface the dictionaries (web/i18n-en.js, web/i18n-ja.js) do not cover.

    python3 test/i18n_harvest.py            what is not covered, per language; exit 1 when a text of the
                                            page's own templates (web/*.js, index.html) is among them
    python3 test/i18n_harvest.py --dump ui  every text found (ui, go or cs), with where it is
    python3 test/i18n_harvest.py --all      also list what is covered only as a piece of a longer entry

It reads the sources the way the page meets them: a template literal is cut at its tags and its ${…}, a Go
format string at its verbs, and each piece of text is looked up as web/i18n.js would look it up (an exact
entry, a pattern, or its parts: sentences, 「label：rest」, clauses, lists). A piece that is only a fragment of
a sentence put together at run time ("已复制提取码 " + pwd) counts as covered when an entry holds it ("piece").
That is weaker than a run of the page (test/i18n_crawl.py), which is what finds a sentence no entry matches.

The program's and the plugin's messages are listed too (Go, C#). Not every Chinese literal there reaches the
player: log lines, what is said to the AI model, words looked for in names. Those are left out by where they
stand (LOG, MODEL, LOGIC below) or, one by one, in NOT_SHOWN.
"""
import itertools, json, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
HAN = re.compile(r'[\u3400-\u9fff]')
CJK = re.compile(r'[\u3000-\u303f\u3400-\u9fff\uff00-\uffef]')
SLOT = '\x01'  # where a value is put in at run time

# ---------- the sources ----------

def js_literals(src):
    """(line, text) of every string literal; a template's ${…} becomes SLOT and what is inside is read too"""
    out, n = [], len(src)

    def esc(c, i):
        if c == 'n': return '\n', i + 1
        if c == 't': return '\t', i + 1
        if c == 'u' and src[i + 1] != '{': return chr(int(src[i + 1:i + 5], 16)), i + 5
        if c == 'x': return chr(int(src[i + 1:i + 3], 16)), i + 3
        if c == '\n': return '', i + 1
        return c, i + 1

    def code(i, stop):
        """reads code from i until the unmatched `stop` ('}' or None); returns where it ends"""
        depth, prev = 0, '('
        while i < n:
            c = src[i]
            if c in ' \t\r\n': i += 1; continue
            if src.startswith('//', i): i = src.find('\n', i); i = n if i < 0 else i; continue
            if src.startswith('/*', i): i = src.find('*/', i) + 2; continue
            if c in '"\'':
                line, j, buf = src.count('\n', 0, i) + 1, i + 1, []
                while src[j] != c:
                    if src[j] == '\\': ch, j = esc(src[j + 1], j + 1); buf.append(ch)
                    else: buf.append(src[j]); j += 1
                out.append((line, ''.join(buf))); i = j + 1; prev = '"'; continue
            if c == '`':
                line, j, buf = src.count('\n', 0, i) + 1, i + 1, []
                while src[j] != '`':
                    if src[j] == '\\': ch, j = esc(src[j + 1], j + 1); buf.append(ch)
                    elif src.startswith('${', j): buf.append(SLOT); j = code(j + 2, '}') + 1
                    else: buf.append(src[j]); j += 1
                out.append((line, ''.join(buf))); i = j + 1; prev = '"'; continue
            if c == '/' and (prev in '(,=:[!&|?{};+-*%<>~^' or prev == 'kw'):  # a regular expression, not a division
                j, cls = i + 1, False
                while src[j] != '/' or cls:
                    if src[j] == '\\': j += 1
                    elif src[j] == '[': cls = True
                    elif src[j] == ']': cls = False
                    j += 1
                i = j + 1
                while i < n and src[i].isalpha(): i += 1
                prev = '"'; continue
            if c == '{': depth += 1
            elif c == '}':
                if depth == 0 and stop == '}': return i
                depth -= 1
            if c.isalnum() or c in '_$':
                j = i
                while j < n and (src[j].isalnum() or src[j] in '_$'): j += 1
                prev = 'kw' if src[i:j] in ('return', 'typeof', 'case', 'in', 'of', 'do', 'else') else 'a'
                i = j; continue
            prev = c; i += 1
        return n

    code(0, None)
    return out

TAG = re.compile(r'<[^<>]*>')
ATTR = re.compile(r'\b(?:title|placeholder|aria-label|alt)=\\?"([^"]*?)\\?"')
DATA = re.compile(r'\bdata-[\w-]+=\\?"[^"]*?\\?"')
ENT = {'&gt;': '>', '&lt;': '<', '&amp;': '&', '&quot;': '"', '&#39;': "'"}

def html_chunks(text):
    """the texts a piece of markup shows: what stands between its tags, and the attributes people read"""
    text = DATA.sub('', text)
    out = [m.group(1) for m in ATTR.finditer(text)]
    if '<' in text or ATTR.search(text): out += TAG.split(ATTR.sub('', text))
    else: out.append(text)
    res = []
    for c in out:
        for k, v in ENT.items(): c = c.replace(k, v)
        for line in c.split('\n'):
            line = re.sub(r'\s+', ' ', line).strip()
            if HAN.search(line): res.append(line)
    return res

def go_literals(src):
    """(line, text, calls, key, func): calls = the calls the literal stands in, innermost last; key = the field
    or map key it is the value of; func = the function (or top-level name) it is in"""
    out, n, i = [], len(src), 0
    stack, word, key, func, depth = [], '', '', '', 0
    while i < n:
        c = src[i]
        if src.startswith('//', i): i = src.find('\n', i); i = n if i < 0 else i; continue
        if src.startswith('/*', i): i = src.find('*/', i) + 2; continue
        if c == '\n' and depth == 0:
            m = re.match(r'\n(?:func (?:\([^)]*\) )?(\w+)|(?:const|var) (\w+)|\t(\w+)\s+=)', src[i:i + 200])
            if m: func = m.group(1) or m.group(2) or m.group(3) or func
        if c == '"' or c == '`':
            line, j, buf = src.count('\n', 0, i) + 1, i + 1, []
            while src[j] != c:
                if c == '"' and src[j] == '\\':
                    e = src[j + 1]
                    if e == 'n': buf.append('\n'); j += 2
                    elif e == 't': buf.append('\t'); j += 2
                    elif e == 'u': buf.append(chr(int(src[j + 2:j + 6], 16))); j += 6
                    else: buf.append(e); j += 2
                else: buf.append(src[j]); j += 1
            # what the literal is given to: a field ("Label: …"), a map key ("err": …)
            before = src[max(0, i - 60):i]
            m = re.search(r'(?:"(\w+)"|(\w+))\s*:\s*$', before)
            out.append((line, ''.join(buf), [s for s in stack if s], (m.group(1) or m.group(2)) if m else '', func))
            i = j + 1; word = ''; continue
        if c == "'":
            j = i + 1
            while src[j] != "'": j += 2 if src[j] == '\\' else 1
            i = j + 1; continue
        if c.isalnum() or c in '_.':
            j = i
            while j < n and (src[j].isalnum() or src[j] in '_.'): j += 1
            word = src[i:j]; i = j; continue
        if c == '(': stack.append(word)
        elif c == ')' and stack: stack.pop()
        elif c == '{': depth += 1
        elif c == '}': depth -= 1
        if c not in ' \t': word = ''
        i += 1
    return out

def cs_literals(src):
    """(line, text, call): C# strings; an interpolated string's {…} becomes SLOT"""
    out, n, i = [], len(src), 0
    stack, word = [], ''
    while i < n:
        c = src[i]
        if src.startswith('//', i): i = src.find('\n', i); i = n if i < 0 else i; continue
        if src.startswith('/*', i): i = src.find('*/', i) + 2; continue
        m = re.match(r'(\$@|@\$|\$|@)?"', src[i:i + 3])
        if m:
            pre = m.group(1) or ''
            line, j, buf = src.count('\n', 0, i) + 1, i + len(m.group(0)), []
            while True:
                ch = src[j]
                if '@' in pre and ch == '"':
                    if src[j + 1] == '"': buf.append('"'); j += 2; continue
                    break
                if '@' not in pre and ch == '"': break
                if '@' not in pre and ch == '\\':
                    e = src[j + 1]
                    if e == 'n': buf.append('\n'); j += 2
                    elif e == 'u': buf.append(chr(int(src[j + 2:j + 6], 16))); j += 6
                    else: buf.append(e); j += 2
                    continue
                if '$' in pre and ch == '{':
                    if src[j + 1] == '{': buf.append('{'); j += 2; continue
                    d, j = 1, j + 1
                    while d:
                        if src[j] == '{': d += 1
                        elif src[j] == '}': d -= 1
                        elif src[j] == '"':  # a string inside the hole: read it as its own literal below
                            k = j + 1
                            while src[k] != '"': k += 2 if src[k] == '\\' else 1
                            if HAN.search(src[j + 1:k]): out.append((line, src[j + 1:k], [s for s in stack if s]))
                            j = k
                        j += 1
                    buf.append(SLOT); continue
                if '$' in pre and ch == '}' and src[j + 1] == '}': buf.append('}'); j += 2; continue
                buf.append(ch); j += 1
            out.append((line, ''.join(buf), [s for s in stack if s]))
            i = j + 1; word = ''; continue
        if c == "'":
            j = i + 1
            while src[j] != "'": j += 2 if src[j] == '\\' else 1
            i = j + 1; continue
        if c.isalnum() or c in '_.':
            j = i
            while j < n and (src[j].isalnum() or src[j] in '_.'): j += 1
            word = src[i:j]; i = j; continue
        if c == '(' or c == '[': stack.append(word)
        elif c in ')]' and stack: stack.pop()
        if c not in ' \t': word = ''
        i += 1
    return out

VERB = re.compile(r'%[-+# 0]*\d*(?:\.\d+)?[sdvqfgxXtTwcU]')
CSFMT = re.compile(r'\{\d+(?::[^}]*)?\}')

# Go: literals that never reach the page
LOG = {'core.Logf', 'Logf', 'log.Printf', 'log.Println', 'log.Fatalf', 'flag.Bool', 'flag.Int', 'flag.String', 'flag.StringVar', 'panic',
       'fmt.Fprintf', 'fmt.Fprintln', 'fmt.Println', 'fmt.Printf'}
LOGIC = {'strings.Contains', 'strings.HasPrefix', 'strings.HasSuffix', 'strings.Index', 'strings.EqualFold', 'strings.TrimPrefix',
         'strings.TrimSuffix', 'strings.Split', 'strings.CutPrefix', 'strings.CutSuffix', 'strings.ReplaceAll', 'strings.NewReplacer',
         'regexp.MustCompile', 'strings.ContainsAny', 'strings.Trim', 'strings.TrimLeft', 'strings.TrimRight', 'strings.Count', 'strings.Cut',
         'strings.SplitN', 'strings.LastIndex', 'strings.ContainsRune', 'strings.TrimFunc', 'strings.IndexAny', 'score'}
# … and what is said to the AI model (the prompt, the tools' descriptions, what a tool call answers with)
MODEL_CALLS = {'prop', 'strList', 'obj'}
MODEL_KEYS = {'Desc', 'description'}
MODEL_FUNCS = set()   # filled in below, file by file
NOT_SHOWN = set()     # (file, how the literal starts) of single literals that are words looked for, not words shown
def not_shown(rel, text):
    t = text.replace(SLOT, '{}')
    return any(f == rel and (p == '*' or t.startswith(p)) for f, p in NOT_SHOWN)

def load_rules():
    """the lists above continue in test/i18n_harvest.skip: one rule a line, with the reason after a #"""
    p = os.path.join(ROOT, 'test', 'i18n_harvest.skip')
    if not os.path.exists(p): return
    for line in open(p, encoding='utf-8'):
        line = line.split(' #')[0].rstrip('\n')
        if not line.strip() or line.startswith('#'): continue
        kind, _, rest = line.partition(' ')
        if kind == 'func': MODEL_FUNCS.add(tuple(rest.strip().split(' ', 1)))       # func <file> <name>
        elif kind == 'text': f, _, t = rest.partition(' '); NOT_SHOWN.add((f, t.replace('\\n', '\n')))  # text <file> <how the literal starts>
        elif kind == 'file': NOT_SHOWN.add((rest.strip(), '*'))                      # file <file>

def harvest():
    ui, go, cs = [], [], []
    web = os.path.join(ROOT, 'web')
    for f in sorted(os.listdir(web)):
        p = os.path.join(web, f)
        if f.endswith('.js') and not f.startswith('i18n'):
            for line, text in js_literals(open(p, encoding='utf-8').read()):
                if not_shown('web/' + f, text): continue
                for c in html_chunks(text): ui.append(('web/%s:%d' % (f, line), c))
        elif f.endswith('.html'):
            src = open(p, encoding='utf-8').read()
            src = re.sub(r'<script[^>]*>.*?</script>|<style.*?</style>', '', src, flags=re.S)
            for i, line in enumerate(src.split('\n'), 1):
                for c in html_chunks(line): ui.append(('web/%s:%d' % (f, i), c))
    for base in ('internal', 'cmd'):
        for d, _, files in sorted(os.walk(os.path.join(ROOT, base))):
            for f in sorted(files):
                if not f.endswith('.go') or f.endswith('_test.go') or f == 'doc.go': continue
                rel = os.path.relpath(os.path.join(d, f), ROOT).replace(os.sep, '/')
                if not_shown(rel, '\x00') or '/testkit/' in rel or '/unitytest/' in rel: continue
                for line, text, calls, key, func in go_literals(open(os.path.join(d, f), encoding='utf-8').read()):
                    if not HAN.search(text): continue
                    if any(c in LOG or c in LOGIC or c in MODEL_CALLS or c.endswith('.Logf') for c in calls): continue
                    if key in MODEL_KEYS or (rel, func) in MODEL_FUNCS or not_shown(rel, text): continue
                    text = VERB.sub(SLOT, text).replace('%%', '%')
                    for c in html_chunks(text): go.append(('%s:%d' % (rel, line), c))
    for d, _, files in sorted(os.walk(os.path.join(ROOT, 'unityhelper'))):
        for f in sorted(files):
            if not f.endswith('.cs'): continue
            rel = os.path.relpath(os.path.join(d, f), ROOT).replace(os.sep, '/')
            for line, text, calls in cs_literals(open(os.path.join(d, f), encoding='utf-8').read()):
                if not HAN.search(text) or not_shown(rel, text): continue
                if any(c.startswith('Debug.Log') or c in ('Contains', 'StartsWith', 'EndsWith', 'IndexOf', 'MenuItem', 'Regex', 'Regex.IsMatch') or c.endswith(('.Contains', '.StartsWith', '.EndsWith', '.IndexOf')) for c in calls): continue
                text = CSFMT.sub(SLOT, text)
                for c in html_chunks(text): cs.append(('%s:%d' % (rel, line), c))
    return ui, go, cs

# ---------- the dictionaries, read as web/i18n.js reads them ----------

def read_dict(lang):
    p = os.path.join(ROOT, 'web', 'i18n-%s.js' % lang)
    if not os.path.exists(p): return None
    src = open(p, encoding='utf-8').read()
    i = src.index('I18N.add(')
    i = src.index('{', i)
    # the object is JSON but for comments and commas before a closing bracket
    out, j, n = [], i, len(src)
    while j < n:
        c = src[j]
        if c == '"':
            k = j + 1
            while src[k] != '"': k += 2 if src[k] == '\\' else 1
            out.append(src[j:k + 1]); j = k + 1; continue
        if src.startswith('//', j): j = src.find('\n', j); continue
        out.append(c); j += 1
    text = ''.join(out)
    text = text[:text.rindex('}') + 1]
    text = re.sub(r',(\s*[}\]])', r'\1', text)
    return json.loads(text)

def norm(s): return re.sub(' ?\u3000 ?', '\u3000', re.sub(r'[^\S\u3000]+', ' ', s)).strip()

SLOTS = {'': r'([\s\S]+?)', '#': r'(-?\d+(?:[.,]\d+)*)', '@': r'(\d{1,2}月\d{1,2}日(?: \d\d:\d\d)?|从未)'}
TIME = re.compile(r'^\d{1,2}月\d{1,2}日(?: \d\d:\d\d)?$')

class Dict:
    def __init__(self, d):
        self.exact, self.pats, self.lits = {}, [], []
        for k, v in d.get('exact', {}).items():
            k = norm(k)
            if len(k) > 1 and k.endswith('。'): k = k[:-1]
            self.exact[k] = v
        for k, v in d.get('patterns', []):
            if k.startswith('re:'):
                self.pats.append(re.compile(k[3:])); self.lits.append(k); continue
            k = norm(k)
            if k.endswith('。'): k = k[:-1]
            src = ''
            for n, part in enumerate(re.split(r'(\{[#@]?\w+\})', k)):
                if n % 2: src += SLOTS[part[1] if part[1] in '#@' else '']
                else: src += re.escape(part); self.lits.append(part)
            self.pats.append(re.compile('^' + src + '$'))
        self.hay = '\x00'.join(list(self.exact) + self.lits)

    def pattern(self, k):
        if SLOT not in k: return any(p.match(k) for p in self.pats)
        n = k.count(SLOT)
        samples = ['7', '甲乙', '1月1日 00:00']
        combos = itertools.product(samples, repeat=n) if n <= 4 else [[s] * n for s in samples]
        for combo in combos:
            it = iter(combo); s = re.sub(SLOT, lambda m: next(it), k)
            if any(p.match(s) for p in self.pats): return True
        return False

    def known(self, k, depth=0):
        """'exact', 'pattern', 'parts' or '' — every Chinese part of k has an entry"""
        k = norm(k)
        if not HAN.search(k.replace(SLOT, '')): return 'exact'
        if len(k) > 1 and k.endswith('。'): k = k[:-1]
        flat = k.replace('\u3000', ' ')  # as an entry would write it
        if k in self.exact or flat in self.exact: return 'exact'
        if TIME.match(flat): return 'exact'
        every = lambda parts: all(self.known(x, depth + 1) for x in parts if x.strip())
        for sep in ('。', '\u3000', ' · '):  # what divides a text before anything else
            if sep in k and depth <= 5 and every(k.split(sep)): return 'parts'
        if self.pattern(flat): return 'pattern'
        if depth > 5 or '。' in k: return ''
        m = re.match(r'^([^：；，。]{1,24})：([\s\S]*)$', k)
        if m and self.known(m.group(1), depth + 1): return 'parts' if self.known(m.group(2), depth + 1) else ''
        for sep in ('；', '\u3000', ' · ', ' + ', '、'):
            if sep in k: return 'parts' if every(k.split(sep)) else ''
        m = re.match(r'^([\s\S]*?)（([^（）]*)）$', k)
        if m and self.known(m.group(1), depth + 1) and self.known(m.group(2), depth + 1): return 'parts'
        if '，' in k and every(k.split('，')): return 'parts'
        return ''

    def piece(self, k):
        """every run of Chinese between the slots stands in some entry"""
        for part in re.split('[%s。；]' % SLOT, k):
            part = part.strip(' 　，。：；、（）「」“”…,.:;()')
            if part.startswith('的') and part not in self.hay: part = part[1:]
            if HAN.search(part) and part not in self.hay: return False
        return True

    def cover(self, k):
        return self.known(k) or ('piece' if self.piece(norm(k)) else '')

def main():
    args = sys.argv[1:]
    load_rules()
    ui, go, cs = harvest()
    if '--dump' in args:
        what = args[args.index('--dump') + 1]
        seen = set()
        for where, text in {'ui': ui, 'go': go, 'cs': cs}[what]:
            if text in seen: continue
            seen.add(text); print('%s\t%s' % (where, text.replace(SLOT, '{}')))
        return 0
    bad = False
    for lang in ('en', 'ja'):
        d = read_dict(lang)
        if d is None: print('%s: web/i18n-%s.js is missing' % (lang, lang)); bad = True; continue
        D = Dict(d)
        print('== %s: %d exact entries, %d patterns' % (lang, len(D.exact), len(D.pats)))
        for name, rows in (('UI templates', ui), ('Go messages', go), ('C# messages', cs)):
            count, missing, pieces, seen = {'exact': 0, 'pattern': 0, 'parts': 0, 'piece': 0, '': 0}, [], [], set()
            for where, text in rows:
                if text in seen: continue
                seen.add(text)
                how = D.cover(text); count[how] += 1
                if not how: missing.append((where, text))
                elif how == 'piece': pieces.append((where, text))
            print('%-13s %4d texts: %d exact, %d pattern, %d in parts, %d as a piece of an entry, %d not covered'
                  % (name, len(seen), count['exact'], count['pattern'], count['parts'], count['piece'], count['']))
            for where, text in missing: print('   %s  %s' % (where, text.replace(SLOT, '{}')))
            if '--all' in args:
                for where, text in pieces: print('   (piece) %s  %s' % (where, text.replace(SLOT, '{}')))
            if name == 'UI templates' and missing: bad = True
        if lang == 'ja':  # a Japanese text that is itself a Chinese entry would be translated twice if it met the layer again
            twice = [v for v in D.exact.values() if v in D.exact and D.exact[v] != v]
            if twice: print('   written the same as a Chinese entry with another meaning: ' + '、'.join(sorted(set(twice))))
    return 1 if bad else 0

if __name__ == '__main__':
    sys.exit(main())
