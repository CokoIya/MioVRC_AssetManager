// The studio's on-screen interface (IMGUI in the Game view), in MioVRCA's colours: a rail of panel buttons on the
// left (姿势, 手势, 表情, 镜头, 灯光, 背景, 输出), the open panel next to it, the shutter and output summary at the bottom
// right, recent pictures at the top right, a status line at the bottom left, and the output frame over the view.
// Layout is in unscaled units; the widgets (UiKit) draw in raw pixels at Scale with fonts sized for it, so text stays
// sharp at any scale. The rects of the last Repaint are kept in raw pixels for IsOverUi.
using System;
using System.Collections.Generic;
using System.IO;
using UnityEngine;

namespace MioVRCA.Studio
{
    internal enum StudioPanel { None, Pose, Hands, Face, Camera, Light, Background, Output }

    internal sealed class StudioUi
    {
        public StudioPanel Panel = StudioPanel.Pose;
        public float UserScale = 1f; // 0.75..1.5, on top of the automatic scale

        // the rects the UI drew this frame, in raw GUI pixels (for IsOverUi)
        readonly List<Rect> drawn = new List<Rect>();

        // the automatic scale (from the view height) times UserScale
        public float Scale
        {
            get { return Mathf.Clamp(Screen.height / 1000f, 0.75f, 2f) * UserScale; }
        }

        // layout, in unscaled units
        const float Margin = 12f, BarH = 40f, Top = 62f, PanelW = 300f;
        const float X0 = 8f, Inner = 272f;     // the panel content: left edge and width inside its clip
        const float FaceRowH = 36f;
        const string FilterControl = "MioVRCA.Studio.FaceFilter";

        static StudioLights lightDefaults; // the default light settings (a plain object: Create is never called)

        float railW = 60f, railScale = -1f; // the rail fits its longest label
        string railLang;

        bool closed;        // Close was called from here: nothing more to draw for this studio
        bool switchAsked;   // 切换模型 was clicked: DrawOver binds the next avatar once the panels are done
        bool hiddenSeen;    // UiHidden as of the last pass: IsOverUi then only knows the pill
        Rect pillRaw;       // 「Tab 显示界面」 while the interface is hidden
        bool recording;     // this pass refills `drawn`
        float vw, vh;       // the view in unscaled units

        // the open panel
        float y;                 // layout cursor inside the panel content
        float contentH = 200f;   // content height measured by the last pass
        float availH;            // content height that fits without scrolling the panel
        float panelScroll;
        StudioPanel scrolledPanel;

        // expressions
        float faceScroll;
        bool filterFocused, filterDrawn;
        Rect filterScreen;
        SkinnedMeshRenderer visMesh, nameMesh;
        string[] visNames;
        string visFilter, meshName = "";
        List<int> visible = new List<int>();

        // the UI scale slider applies when it is let go (the layout would move under the mouse while dragging)
        float scaleDraft = -1f;

        // texts that depend on the language or on numbers, made again only when those change
        string labelsLang;
        readonly string[] timerNames = new string[PhotoCapture.Timers.Length];
        readonly string[] timerChips = new string[PhotoCapture.Timers.Length];
        readonly string[] aspectChips = new string[PhotoCapture.AspectNames.Length];
        int summaryKey = -1, pixelsKey = -1;
        string summary, pixels, photoDir;
        Shot nameShot;
        string shotName;
        string barLang, barName, summaryFor;
        float barScale = -1f, titleW, nameW, switchW, hideW, quitW, summaryScale = -1f, summaryW;

        // Under everything (Repaint): darkens the view outside the output frame, frame border, rule-of-thirds lines.
        public void DrawUnder(Studio s)
        {
            if (closed || s == null) return;
            hiddenSeen = s.UiHidden;
            if (Event.current.type != EventType.Repaint) return;
            Matrix4x4 oldMatrix = GUI.matrix;
            GUI.matrix = Matrix4x4.identity;
            float W = Screen.width, H = Screen.height;
            Rect f = s.FrameRect();
            bool light = s.UiHidden;
            float x0 = Mathf.Round(f.x), y0 = Mathf.Round(f.y), x1 = Mathf.Round(f.xMax), y1 = Mathf.Round(f.yMax);
            Color shade = UiKit.Alpha(UiKit.Bg, light ? 0.3f : 0.55f);
            if (y0 > 0f) Draw2D.Box(new Rect(0f, 0f, W, y0), shade, 0f);
            if (y1 < H) Draw2D.Box(new Rect(0f, y1, W, H - y1), shade, 0f);
            if (x0 > 0f) Draw2D.Box(new Rect(0f, y0, x0, y1 - y0), shade, 0f);
            if (x1 < W) Draw2D.Box(new Rect(x1, y0, W - x1, y1 - y0), shade, 0f);
            var fr = new Rect(x0, y0, x1 - x0, y1 - y0);
            if (s.View != null && s.View.Thirds && fr.width > 6f && fr.height > 6f)
            {
                var t = new Color(1f, 1f, 1f, light ? 0.1f : 0.18f);
                for (int i = 1; i <= 2; i++)
                {
                    float x = Mathf.Round(x0 + fr.width * i / 3f), yy = Mathf.Round(y0 + fr.height * i / 3f);
                    Draw2D.Box(new Rect(x, y0, 1f, fr.height), t, 0f);
                    Draw2D.Box(new Rect(x0, yy, fr.width, 1f), t, 0f);
                }
            }
            Draw2D.Frame(fr, new Color(1f, 1f, 1f, light ? 0.2f : 0.35f), 1f, 0f);
            GUI.matrix = oldMatrix;
        }

        // The panels and buttons, at Scale (GUI.matrix stays identity: the widgets scale themselves so the text is
        // rendered at its real size). Records their rects. When the studio has no avatar, shows s.Problem with
        // 「重新查找」 and 「退出摄影棚」 instead.
        public void DrawPanels(Studio s)
        {
            if (closed || s == null) return;
            Event e = Event.current;
            hiddenSeen = s.UiHidden;
            // the rects of the last Repaint stay for the mouse events in between
            recording = e.type == EventType.Repaint || drawn.Count == 0;
            if (recording) drawn.Clear();
            if (s.UiHidden)
            {
                DropFilterFocus();
                return;
            }

            Matrix4x4 oldMatrix = GUI.matrix;
            GUI.matrix = Matrix4x4.identity;
            float sc = Scale;
            UiKit.Begin(sc);
            UiKit.NoHover = s.Handles != null && s.Handles.Dragging;
            vw = Screen.width / sc;
            vh = Screen.height / sc;
            Labels();
            MeasureRail();
            if (Panel < StudioPanel.None || Panel > StudioPanel.Output) Panel = StudioPanel.None; // e.g. from old prefs

            // the filter field gives the keyboard back on Enter / Esc / Tab and on a click anywhere else
            // (rawType: Studio's shortcuts may already have used Escape)
            bool blur = false;
            if (filterFocused)
            {
                if (e.rawType == EventType.KeyDown && (e.keyCode == KeyCode.Return || e.keyCode == KeyCode.KeypadEnter ||
                    e.keyCode == KeyCode.Escape || e.keyCode == KeyCode.Tab))
                {
                    DropFilterFocus();
                    if (e.type == EventType.KeyDown) e.Use();
                }
                else if (e.rawType == EventType.MouseDown && !filterScreen.Contains(e.mousePosition)) blur = true;
            }
            filterDrawn = false;

            try
            {
                bool avatar = HasAvatar(s);
                TopBar(s, avatar);
                if (closed) return;
                // (asked again: the panels below must never reach a missing Pose, whatever a control above did)
                if (!avatar || !HasAvatar(s)) ProblemCard(s);
                else
                {
                    // controls whose number changes (panel content, thumbnails) come last, so the ids of the
                    // others never move
                    ShutterGroup(s);
                    Rail(s);
                    if (Panel != StudioPanel.None) PanelBox(s);
                    Recent(s);
                }
            }
            finally
            {
                UiKit.End();
                GUI.matrix = oldMatrix;
            }
            if (closed) return;
            if (blur || (filterFocused && !filterDrawn)) DropFilterFocus();
        }

        // Over everything: countdown number, flash, toast, status line (handle hint or the default keys).
        // While the interface is hidden: only the 「Tab 显示界面」 pill instead of the status line.
        public void DrawOver(Studio s)
        {
            if (closed || s == null) return;
            // 切换模型 binds here, after the panels of the pass that clicked it (they were drawn for the avatar in hand,
            // and a bind can leave none) and before anything below asks for the avatar again. DrawOver runs on every
            // pass, also with the interface hidden, so the click is never left waiting.
            if (switchAsked)
            {
                switchAsked = false;
                NextAvatar(s);
            }
            Event e = Event.current;
            hiddenSeen = s.UiHidden;
            Matrix4x4 oldMatrix = GUI.matrix;
            GUI.matrix = Matrix4x4.identity;
            float sc = Scale;
            UiKit.Begin(sc);
            UiKit.NoHover = false;
            vw = Screen.width / sc;
            vh = Screen.height / sc;
            try
            {
                bool repaint = e.type == EventType.Repaint;
                if (repaint && s.Photo != null && s.Photo.Flash > 0f)
                    Draw2D.Box(new Rect(0f, 0f, Screen.width, Screen.height), new Color(1f, 1f, 1f, Mathf.Clamp01(s.Photo.Flash) * 0.7f), 0f);
                if (repaint && s.Photo != null && s.Photo.Countdown > 0f) Countdown(s);
                if (repaint && s.ToastLeft > 0f && !string.IsNullOrEmpty(s.ToastText)) Toast(s);
                if (s.UiHidden)
                {
                    DropFilterFocus();
                    HiddenPill(s);
                }
                else
                {
                    pillRaw = new Rect();
                    if (repaint && HasAvatar(s)) StatusLine(s);
                }
            }
            finally
            {
                UiKit.End();
                GUI.matrix = oldMatrix;
            }
        }

        // the point (raw GUI pixels) is on a panel or button drawn this frame
        public bool IsOverUi(Vector2 p)
        {
            if (closed) return false;
            if (hiddenSeen) return pillRaw.Contains(p);
            foreach (Rect r in drawn)
                if (r.Contains(p)) return true;
            return false;
        }

        // ---- helpers ----

        static bool HasAvatar(Studio s)
        {
            return s.Rig != null && s.Rig.Alive && s.Pose != null;
        }

        void Rec(Rect u)
        {
            if (recording) drawn.Add(UiKit.Px(u));
        }

        void DropFilterFocus()
        {
            if (filterFocused && GUIUtility.keyboardControl != 0) GUIUtility.keyboardControl = 0;
            filterFocused = false;
        }

        void Labels()
        {
            if (labelsLang == L.Lang) return;
            labelsLang = L.Lang;
            for (int i = 0; i < PhotoCapture.Timers.Length; i++)
            {
                int t = PhotoCapture.Timers[i];
                timerNames[i] = t <= 0 ? L.T("关") : L.F("{0} 秒", t);
                timerChips[i] = t <= 0 ? L.T("定时：关") : L.F("定时：{0} 秒", t);
            }
            for (int i = 0; i < PhotoCapture.AspectNames.Length; i++)
                aspectChips[i] = L.F("比例：{0}", PhotoCapture.AspectNames[i]);
            summaryKey = -1;
            pixelsKey = -1;
        }

        void MeasureRail()
        {
            if (railLang == L.Lang && Mathf.Abs(railScale - UiKit.S) < 0.01f) return;
            railLang = L.Lang;
            railScale = UiKit.S;
            float w = 0f;
            for (int i = 1; i <= 7; i++) w = Mathf.Max(w, UiKit.Measure(PanelName((StudioPanel)i), UiKit.BodyCenter));
            railW = Mathf.Clamp(Mathf.Ceil(w) + 12f + 18f, 60f, 120f);
        }

        static string PanelName(StudioPanel p)
        {
            switch (p)
            {
                case StudioPanel.Pose: return L.T("姿势");
                case StudioPanel.Hands: return L.T("手势");
                case StudioPanel.Face: return L.T("表情");
                case StudioPanel.Camera: return L.T("镜头");
                case StudioPanel.Light: return L.T("灯光");
                case StudioPanel.Background: return L.T("背景");
                case StudioPanel.Output: return L.T("输出");
            }
            return "";
        }

        // "1080 × 1920 · PNG", made again only when the size or the backdrop changes
        string Summary(Studio s)
        {
            Vector2Int size = s.Photo.Size;
            bool transparent = s.Back != null && s.Back.Transparent;
            int key = ((size.x & 0x3fff) * 16384 + (size.y & 0x3fff)) * 2 + (transparent ? 1 : 0);
            if (key != summaryKey || summary == null)
            {
                summaryKey = key;
                summary = L.F("{0} × {1} · {2}", size.x, size.y, transparent ? L.T("透明 PNG") : "PNG");
            }
            return summary;
        }

        string Pixels(Studio s)
        {
            Vector2Int size = s.Photo.Size;
            int key = (size.x & 0x3fff) * 16384 + (size.y & 0x3fff);
            if (key != pixelsKey || pixels == null)
            {
                pixelsKey = key;
                pixels = L.F("照片 {0} × {1} 像素", size.x, size.y);
            }
            return pixels;
        }

        string PhotoDir
        {
            get { return photoDir ?? (photoDir = StudioHost.PhotoDir); }
        }

        static bool Same(Color a, Color b)
        {
            return Mathf.Abs(a.r - b.r) < 0.01f && Mathf.Abs(a.g - b.g) < 0.01f && Mathf.Abs(a.b - b.b) < 0.01f;
        }

        // shows a picture selected in its folder through the editor, or says where it is
        void Reveal(Studio s, string path)
        {
            if (string.IsNullOrEmpty(path)) return;
            if (!File.Exists(path))
            {
                s.Toast(L.T("这张照片已经不在原来的位置了"), 2.5f);
                return;
            }
            if (StudioHost.Reveal != null) StudioHost.Reveal(path);
            else s.Toast(L.F("照片在：{0}", path), 5f);
        }

        // ---- top bar ----

        void TopBar(Studio s, bool avatar)
        {
            const float y0 = Margin, h = BarH;
            string title = L.T("MioVRCA 摄影棚");
            string name = avatar ? s.Rig.Name : null;
            MeasureBar(name);
            float tw = titleW, nw = nameW;
            bool canSwitch = s.Candidates != null && s.Candidates.Count > 1;
            string sw = L.T("切换模型");
            float bw = canSwitch ? switchW : 0f;
            float w = 14f + 16f + tw + (nw > 0f ? 21f + nw : 0f) + (canSwitch ? 12f + bw + 6f : 14f);
            var bar = new Rect(Margin, y0, w, h);
            Rec(bar);
            UiKit.Box(bar, UiKit.PanelBg, 12f);
            float x = bar.x + 14f;
            UiKit.Disc(new Vector2(x + 4f, y0 + h / 2f), 4f, UiKit.Accent);
            x += 16f;
            UiKit.Label(new Rect(x, y0, tw, h), title, UiKit.Title);
            x += tw;
            if (nw > 0f)
            {
                UiKit.Box(new Rect(x + 10f, y0 + 12f, 1f / UiKit.S, h - 24f), UiKit.Line, 0f);
                x += 21f;
                UiKit.Label(new Rect(x, y0, nw, h), name, UiKit.Small, UiKit.Muted);
                x += nw;
            }
            if (canSwitch)
            {
                x += 12f;
                if (UiKit.Button(new Rect(x, y0 + 6f, bw, h - 12f), sw, ButtonKind.Normal, true, UiKit.SmallCenter)) switchAsked = true;
            }

            string hide = L.T("隐藏界面（Tab）"), quit = L.T("退出摄影棚");
            float hw = hideW, qw = quitW;
            float rw = 6f + hw + 6f + qw + 6f;
            var right = new Rect(vw - Margin - rw, y0, rw, h);
            Rec(right);
            UiKit.Box(right, UiKit.PanelBg, 12f);
            if (UiKit.Button(new Rect(right.x + 6f, y0 + 6f, hw, h - 12f), hide, ButtonKind.Ghost, true, UiKit.SmallCenter))
            {
                s.UiHidden = true;
                hiddenSeen = true;
            }
            if (UiKit.Button(new Rect(right.x + 12f + hw, y0 + 6f, qw, h - 12f), quit, ButtonKind.Normal, true, UiKit.SmallCenter))
            {
                DropFilterFocus();
                closed = true;
                s.Close(true);
            }
        }

        // the top bar's text widths, measured again only when the language, the scale or the avatar's name change
        void MeasureBar(string name)
        {
            if (barLang == L.Lang && Mathf.Abs(barScale - UiKit.S) < 0.001f && ReferenceEquals(barName, name)) return;
            barLang = L.Lang;
            barScale = UiKit.S;
            barName = name;
            titleW = UiKit.Measure(L.T("MioVRCA 摄影棚"), UiKit.Title) + 1f;
            nameW = string.IsNullOrEmpty(name) ? 0f : Mathf.Min(UiKit.Measure(name, UiKit.Small) + 1f, 240f);
            switchW = UiKit.Measure(L.T("切换模型"), UiKit.SmallCenter) + 22f;
            hideW = UiKit.Measure(L.T("隐藏界面（Tab）"), UiKit.SmallCenter) + 24f;
            quitW = UiKit.Measure(L.T("退出摄影棚"), UiKit.SmallCenter) + 24f;
        }

        // Binds the next candidate after the current avatar. One that cannot be used is skipped for the one after it
        // (BindAvatar keeps the avatar in hand and toasts why, so with none usable the user is told and nothing changes).
        void NextAvatar(Studio s)
        {
            List<Transform> c = s.Candidates;
            if (c == null) return;
            int n = c.Count, idx = -1;
            Transform cur = s.Rig != null ? s.Rig.Root : null;
            if (cur != null)
                for (int i = 0; i < n; i++)
                    if (c[i] == cur)
                    {
                        idx = i;
                        break;
                    }
            for (int k = 1; k <= n; k++)
            {
                Transform t = c[((idx + k) % n + n) % n];
                if (t == null || t == cur || !s.BindAvatar(t)) continue;
                faceScroll = 0f;
                if (s.Rig != null) s.Toast(L.F("已切换到 {0}", s.Rig.Name), 2f);
                return;
            }
            // A candidate sharing bones with the avatar in hand (one inside the other) has it given back before it is
            // tried, so a failure there leaves no avatar: take the one in hand again rather than an empty studio (its
            // pose starts over), and still say why the switch did not work.
            if (cur != null && !HasAvatar(s))
            {
                string why = s.Problem;
                if (s.BindAvatar(cur) && !string.IsNullOrEmpty(why)) s.Toast(why, 4f);
            }
        }

        // ---- no avatar ----

        void ProblemCard(Studio s)
        {
            bool searching = string.IsNullOrEmpty(s.Problem);
            string title = searching ? L.T("正在查找模型…") : L.T("没有可以拍摄的模型");
            string body = searching ? L.T("进入播放模式后，NDMF、VRCFury 等工具可能还在生成模型，请稍候。") : s.Problem;
            const float w = 400f, inner = w - 48f;
            float th = UiKit.MeasureHeight(title, UiKit.CardTitle, inner);
            float bh = UiKit.MeasureHeight(body, UiKit.BodyWrap, inner);
            float h = 24f + th + 10f + bh + 22f + 34f + 24f;
            var r = new Rect((vw - w) / 2f, Mathf.Max(Top, (vh - h) / 2f), w, h);
            Rec(r);
            UiKit.Box(r, UiKit.PanelBg, 12f);
            UiKit.Frame(r, UiKit.Line, 1f, 12f);
            float yy = r.y + 24f;
            UiKit.Box(new Rect(r.x, yy + 2f, 3f, th - 4f), searching ? UiKit.Warn : UiKit.Accent, 1.5f);
            UiKit.Label(new Rect(r.x + 24f, yy, inner, th + 2f), title, UiKit.CardTitle);
            yy += th + 10f;
            UiKit.Label(new Rect(r.x + 24f, yy, inner, bh + 2f), body, UiKit.BodyWrap, UiKit.Muted);
            yy += bh + 22f;
            float bw = (inner - 10f) / 2f;
            if (UiKit.Button(new Rect(r.x + 24f, yy, bw, 34f), L.T("重新查找"), ButtonKind.Primary, true)) s.FindAvatar();
            if (UiKit.Button(new Rect(r.x + 34f + bw, yy, bw, 34f), L.T("退出摄影棚"), ButtonKind.Normal, true))
            {
                DropFilterFocus();
                closed = true;
                s.Close(true);
            }
        }

        // ---- rail and panel ----

        void Rail(Studio s)
        {
            var r = new Rect(Margin, Top, railW, 6f + 7 * 44f + 2f);
            Rec(r);
            UiKit.Box(r, UiKit.PanelBg, 12f);
            for (int i = 0; i < 7; i++)
            {
                var p = (StudioPanel)(i + 1);
                var b = new Rect(r.x + 6f, r.y + 6f + i * 44f, railW - 12f, 40f);
                if (UiKit.Button(b, PanelName(p), Panel == p ? ButtonKind.On : ButtonKind.Ghost, true))
                {
                    Panel = Panel == p ? StudioPanel.None : p;
                    s.PrefsChanged();
                }
            }
        }

        void PanelBox(Studio s)
        {
            if (scrolledPanel != Panel)
            {
                scrolledPanel = Panel;
                panelScroll = 0f;
            }
            float maxH = Mathf.Max(180f, vh - Top - 56f);
            float full = contentH + 16f; // the content with its padding inside the clip
            var pr = new Rect(Margin + railW + 8f, Top, PanelW, Mathf.Min(full + 12f, maxH));
            Rec(pr);
            UiKit.Box(pr, UiKit.PanelBg, 12f);
            int barId = UiKit.NewScrollId(); // before the content, whose controls come and go
            var view = new Rect(pr.x + 6f, pr.y + 6f, pr.width - 12f, pr.height - 12f);
            float maxScroll = Mathf.Max(0f, full - view.height);
            panelScroll = Mathf.Clamp(panelScroll, 0f, maxScroll);
            availH = maxH - 12f - 16f;

            UiKit.BeginClip(view);
            y = 8f - panelScroll;
            float start = y;
            Title(s, PanelName(Panel));
            switch (Panel)
            {
                case StudioPanel.Pose: PosePanel(s); break;
                case StudioPanel.Hands: HandsPanel(s); break;
                case StudioPanel.Face: FacePanel(s); break;
                case StudioPanel.Camera: CameraPanel(s); break;
                case StudioPanel.Light: LightPanel(s); break;
                case StudioPanel.Background: BackgroundPanel(s); break;
                case StudioPanel.Output: OutputPanel(s); break;
            }
            contentH = Mathf.Max(40f, y - start - 6f);
            UiKit.EndClip();

            if (maxScroll > 0f)
            {
                panelScroll = UiKit.ScrollBar(barId, new Rect(pr.xMax - 5f, pr.y + 12f, 3f, pr.height - 24f), panelScroll, full, view.height);
                panelScroll = UiKit.Wheel(pr, panelScroll, maxScroll);
            }
        }

        void Title(Studio s, string text)
        {
            UiKit.Label(new Rect(X0, y, Inner - 30f, 26f), text, UiKit.PanelTitle);
            if (UiKit.Button(new Rect(X0 + Inner - 26f, y + 1f, 26f, 24f), "×", ButtonKind.Ghost, true))
            {
                Panel = StudioPanel.None;
                s.PrefsChanged();
            }
            y += 30f;
        }

        void Header(string text)
        {
            y += 6f;
            UiKit.Label(new Rect(X0, y, Inner, 16f), text, UiKit.Caption);
            y += 20f;
        }

        void Note(string text)
        {
            float h = UiKit.MeasureHeight(text, UiKit.CaptionWrap, Inner);
            UiKit.Label(new Rect(X0, y, Inner, h + 2f), text, UiKit.CaptionWrap);
            y += h + 8f;
        }

        Rect Row(float h)
        {
            var r = new Rect(X0, y, Inner, h);
            y += h + 6f;
            return r;
        }

        bool SwitchRow(string label, bool v, bool enabled)
        {
            return UiKit.Switch(Row(30f), label, v, enabled);
        }

        float SliderRow(string label, float v, float min, float max, float def, float step, Fmt fmt, bool enabled)
        {
            return UiKit.Slider(Row(38f), label, v, min, max, def, step, fmt, enabled);
        }

        // ---- 姿势 ----

        void PosePanel(Studio s)
        {
            PoseModel pose = s.Pose;
            Rect row = Row(32f);
            if (UiKit.Button(UiKit.Cell(row, 0, 2, 8f), L.T("撤销"), ButtonKind.Normal, pose.CanUndo)) pose.Undo();
            if (UiKit.Button(UiKit.Cell(row, 1, 2, 8f), L.T("重做"), ButtonKind.Normal, pose.CanRedo)) pose.Redo();

            Header(L.T("骨骼"));
            PoseHandles h = s.Handles;
            bool b = SwitchRow(L.T("显示骨骼（H）"), h.Visible, true);
            if (b != h.Visible)
            {
                h.Visible = b;
                s.PrefsChanged();
            }
            b = SwitchRow(L.T("细节"), h.Detail, h.Visible);
            if (b != h.Detail)
            {
                h.Detail = b;
                s.PrefsChanged();
            }
            b = SwitchRow(L.T("手指"), h.Fingers, h.Visible);
            if (b != h.Fingers)
            {
                h.Fingers = b;
                s.PrefsChanged();
            }
            b = SwitchRow(L.T("移动身体时固定双脚"), h.PinFeet, h.Visible);
            if (b != h.PinFeet)
            {
                h.PinFeet = b;
                s.PrefsChanged();
            }
            float v = SliderRow(L.T("拖动灵敏度"), h.Sensitivity, 0.25f, 3f, 1f, 0.05f, Fmt.Times, h.Visible);
            if (v != h.Sensitivity)
            {
                h.Sensitivity = v;
                s.PrefsChanged();
            }

            Header(L.T("姿势槽"));
            PoseSlots slots = s.Slots;
            row = Row(34f);
            for (int i = 0; i < PoseSlots.Count; i++)
            {
                bool has = slots != null && slots.Has(i);
                int c = UiKit.Slot(UiKit.Cell(row, i, PoseSlots.Count, 6f), UiKit.N(i + 1), has, slots != null);
                if (c == 0)
                {
                    if (has && slots.ApplyTo(i, pose)) s.Toast(L.F("已套用姿势槽 {0}", i + 1), 1.5f);
                    else s.Toast(L.T("这个姿势槽是空的：右键点击可以保存当前姿势"), 2.5f);
                }
                else if (c == 1)
                {
                    slots.Store(i, pose);
                    s.Toast(L.F("当前姿势已保存到姿势槽 {0}", i + 1), 2f);
                }
            }
            Note(L.T("左键套用 · 右键保存"));
            row = Row(32f);
            if (UiKit.Button(UiKit.Cell(row, 0, 2, 8f), L.T("自然站立"), ButtonKind.Normal, true))
            {
                pose.BeginEdit();
                pose.StandNaturally();
            }
            if (UiKit.Button(UiKit.Cell(row, 1, 2, 8f), L.T("初始姿势"), ButtonKind.Normal, true))
            {
                pose.BeginEdit();
                pose.ResetAll();
            }

            Header(L.T("视线"));
            LookAt look = s.Look;
            b = SwitchRow(L.T("视线跟随"), look.Enabled, true);
            if (b != look.Enabled)
            {
                look.Enabled = b;
                s.PrefsChanged();
            }
            row = Row(30f);
            UiKit.SegmentBack(row);
            if (UiKit.Segment(row, 0, 2, L.T("看镜头"), look.FollowCamera, look.Enabled) && !look.FollowCamera)
            {
                look.FollowCamera = true;
                s.PrefsChanged();
            }
            if (UiKit.Segment(row, 1, 2, L.T("看目标"), !look.FollowCamera, look.Enabled) && look.FollowCamera)
            {
                look.FollowCamera = false;
                // the target appears straight ahead of the face as it is now
                look.Target = look.DefaultTarget(s.Rig);
                s.PrefsChanged();
            }
            if (look.Enabled && !look.FollowCamera) Note(L.T("拖动画面里的白色圆圈来移动视线目标"));
            v = SliderRow(L.T("眼睛"), look.Eyes, 0f, 1f, 1f, 0.01f, Fmt.Percent, look.Enabled);
            if (v != look.Eyes)
            {
                look.Eyes = v;
                s.PrefsChanged();
            }
            v = SliderRow(L.T("头部"), look.Head, 0f, 1f, 0.3f, 0.01f, Fmt.Percent, look.Enabled);
            if (v != look.Head)
            {
                look.Head = v;
                s.PrefsChanged();
            }
        }

        // ---- 手势 ----

        void HandsPanel(Studio s)
        {
            Rect row = Row(32f);
            UiKit.SegmentBack(row);
            HandSide side = s.HandSide;
            HandSide picked = side;
            if (UiKit.Segment(row, 0, 3, L.T("左手"), side == HandSide.Left, true)) picked = HandSide.Left;
            if (UiKit.Segment(row, 1, 3, L.T("右手"), side == HandSide.Right, true)) picked = HandSide.Right;
            if (UiKit.Segment(row, 2, 3, L.T("双手"), side == HandSide.Both, true)) picked = HandSide.Both;
            if (picked != side)
            {
                s.HandSide = picked;
                s.PrefsChanged();
            }
            float v = SliderRow(L.T("力度"), s.HandStrength, 0f, 1f, 1f, 0.01f, Fmt.Percent, true);
            if (v != s.HandStrength)
            {
                s.HandStrength = v;
                s.PrefsChanged();
            }

            Header(L.T("预设"));
            string[] names = HandPresets.Names;
            for (int i = 0; i < names.Length; i += 2)
            {
                row = Row(34f);
                for (int k = 0; k < 2 && i + k < names.Length; k++)
                {
                    string label = L.T(names[i + k]); // the names are literals in HandPresets.cs
                    if (UiKit.Button(UiKit.Cell(row, k, 2, 8f), label, ButtonKind.Normal, true))
                    {
                        HandPresets.Apply(s.Pose, i + k, s.HandSide, s.HandStrength);
                        s.Toast(L.F("已套用手势：{0}", label), 1.5f);
                    }
                }
            }
            Note(L.T("套用后还可以拖动手指微调 · Ctrl+Z 撤销"));
        }

        // ---- 表情 ----

        void FacePanel(Studio s)
        {
            FaceControl f = s.Face;
            int n = f != null && f.Meshes != null ? f.Meshes.Count : 0;
            if (n == 0)
            {
                Note(L.T("这个模型没有带形态键的网格，没有可以调整的表情。"));
                return;
            }

            SkinnedMeshRenderer mesh = f.Mesh;
            if (n > 1)
            {
                Rect row = Row(32f);
                int idx = mesh != null ? f.Meshes.IndexOf(mesh) : -1;
                if (UiKit.Button(new Rect(row.x, row.y, 32f, row.height), "‹", ButtonKind.Normal, true)) UseMesh(f, idx, -1);
                if (UiKit.Button(new Rect(row.x + 38f, row.y, row.width - 76f, row.height), mesh != null ? MeshName(mesh) : L.T("选择网格"),
                    ButtonKind.Normal, true, UiKit.SmallCenter)) UseMesh(f, idx, 1);
                if (UiKit.Button(new Rect(row.xMax - 32f, row.y, 32f, row.height), "›", ButtonKind.Normal, true)) UseMesh(f, idx, 1);
            }
            else if (mesh != null)
            {
                UiKit.Label(Row(18f), MeshName(mesh), UiKit.SmallMuted);
            }
            if (mesh == null || f.Names == null || f.Names.Length == 0)
            {
                Note(L.T("这个网格没有形态键。"));
                return;
            }

            Rect fr = Row(32f);
            Rect screen;
            string filter = UiKit.TextField(fr, f.Filter, L.T("筛选形态键…"), FilterControl, out filterFocused, out screen);
            filterScreen = screen;
            filterDrawn = true;
            if (filter != (f.Filter ?? ""))
            {
                f.Filter = filter;
                faceScroll = 0f;
            }

            string resetText = L.T("全部复原");
            float resetW = Mathf.Max(72f, UiKit.Measure(resetText, UiKit.SmallCenter) + 20f);
            Rect sr = Row(32f);
            var slots = new Rect(sr.x, sr.y, sr.width - resetW - 8f, sr.height);
            for (int i = 0; i < FaceControl.SlotCount; i++)
            {
                bool has = f.HasSlot(i);
                int c = UiKit.Slot(UiKit.Cell(slots, i, FaceControl.SlotCount, 6f), UiKit.N(i + 1), has, true);
                if (c == 0)
                {
                    if (has)
                    {
                        f.ApplySlot(i);
                        s.Toast(L.F("已套用表情槽 {0}", i + 1), 1.5f);
                    }
                    else s.Toast(L.T("这个表情槽是空的：右键点击可以保存当前表情"), 2.5f);
                }
                else if (c == 1)
                {
                    f.StoreSlot(i);
                    s.Toast(L.F("当前表情已保存到表情槽 {0}", i + 1), 2f);
                }
            }
            if (UiKit.Button(new Rect(sr.xMax - resetW, sr.y, resetW, sr.height), resetText, ButtonKind.Normal, true, UiKit.SmallCenter)) f.ResetAll();
            Note(L.T("表情槽：左键套用 · 右键保存 · 双击滑条归零"));

            List<int> vis = Visible(f);
            if (vis.Count == 0)
            {
                Note(L.T("没有名字匹配的形态键。"));
                return;
            }
            float total = vis.Count * FaceRowH;
            float used = y + panelScroll - 8f;
            float listH = Mathf.Min(total, Mathf.Max(150f, availH - used - 4f));
            Rect lr = Row(listH);
            int barId = UiKit.NewScrollId(); // before the rows, whose number changes with the scroll
            float maxScroll = Mathf.Max(0f, total - listH);
            faceScroll = Mathf.Clamp(faceScroll, 0f, maxScroll);

            UiKit.BeginClip(lr);
            float rowW = lr.width - (maxScroll > 0f ? 10f : 0f);
            int first = Mathf.Max(0, Mathf.FloorToInt(faceScroll / FaceRowH));
            int last = Mathf.Min(vis.Count - 1, Mathf.FloorToInt((faceScroll + listH) / FaceRowH));
            string[] names = f.Names;
            for (int i = first; i <= last; i++)
            {
                int k = vis[i];
                if (k < 0 || k >= names.Length) continue;
                var rr = new Rect(0f, i * FaceRowH - faceScroll, rowW, FaceRowH);
                float w = f.Get(k);
                float nw = UiKit.Slider(rr, names[k], w, 0f, 100f, 0f, 1f, Fmt.Int, true,
                    w > 0.05f ? UiKit.Small : UiKit.SmallMuted, UiKit.Accent);
                if (nw != w) f.Set(k, nw);
            }
            UiKit.EndClip();

            if (maxScroll > 0f)
            {
                faceScroll = UiKit.ScrollBar(barId, new Rect(lr.xMax - 3f, lr.y, 3f, lr.height), faceScroll, total, listH);
                faceScroll = UiKit.Wheel(lr, faceScroll, maxScroll);
            }
        }

        void UseMesh(FaceControl f, int idx, int dir)
        {
            int n = f.Meshes.Count;
            for (int k = 1; k <= n; k++)
            {
                SkinnedMeshRenderer m = f.Meshes[((idx + dir * k) % n + n) % n];
                if (m == null) continue;
                f.Use(m);
                faceScroll = 0f;
                return;
            }
        }

        // Object.name makes a new string every time: kept for the mesh shown
        string MeshName(SkinnedMeshRenderer m)
        {
            if (!ReferenceEquals(m, nameMesh))
            {
                nameMesh = m;
                meshName = m != null ? m.name : "";
            }
            return meshName;
        }

        // FaceControl.Visible() makes a new list: asked again only when the mesh, its names or the filter change
        List<int> Visible(FaceControl f)
        {
            string filter = f.Filter ?? "";
            if (!ReferenceEquals(f.Mesh, visMesh) || !ReferenceEquals(f.Names, visNames) || filter != visFilter)
            {
                visMesh = f.Mesh;
                visNames = f.Names;
                visFilter = filter;
                visible = f.Visible() ?? new List<int>();
            }
            return visible;
        }

        // ---- 镜头 ----

        void CameraPanel(Studio s)
        {
            StudioCamera v = s.View;
            Header(L.T("取景"));
            Rect row = Row(32f);
            if (UiKit.Button(UiKit.Cell(row, 0, 3, 6f), L.T("全身"), ButtonKind.Normal, true)) v.FrameAvatar(s.Rig, Framing.Full, s.Photo.Aspect);
            if (UiKit.Button(UiKit.Cell(row, 1, 3, 6f), L.T("上半身"), ButtonKind.Normal, true)) v.FrameAvatar(s.Rig, Framing.Upper, s.Photo.Aspect);
            if (UiKit.Button(UiKit.Cell(row, 2, 3, 6f), L.T("脸"), ButtonKind.Normal, true)) v.FrameAvatar(s.Rig, Framing.Face, s.Photo.Aspect);

            Header(L.T("焦距"));
            float[] lenses = StudioCamera.Lenses;
            row = Row(30f);
            for (int i = 0; i < lenses.Length; i++)
            {
                bool on = Mathf.Abs(v.FocalLength - lenses[i]) < 0.5f;
                if (UiKit.Button(UiKit.Cell(row, i, lenses.Length, 6f), UiKit.Format(Fmt.Millimetres, lenses[i]),
                    on ? ButtonKind.On : ButtonKind.Normal, true, UiKit.SmallCenter)) SetFocal(s, lenses[i]);
            }
            float fl = SliderRow(L.T("微调"), v.FocalLength, 14f, 200f, 50f, 1f, Fmt.Millimetres, true);
            if (fl != v.FocalLength) SetFocal(s, fl);
            float o = SliderRow(L.T("正交"), v.Ortho, 0f, 1f, 0f, 0.01f, Fmt.Percent, true);
            if (o != v.Ortho)
            {
                v.Ortho = o;
                s.PrefsChanged();
            }

            Header(L.T("视角"));
            bool b = SwitchRow(L.T("跟随骨骼"), v.Follow, true);
            if (b != v.Follow)
            {
                // from where the pivot is now, so the picture does not jump
                if (b) v.SetFollow(s.Rig, v.FollowBone);
                v.Follow = b;
                s.PrefsChanged();
            }
            b = SwitchRow(L.T("三分线"), v.Thirds, true);
            if (b != v.Thirds)
            {
                v.Thirds = b;
                s.PrefsChanged();
            }
            float sp = SliderRow(L.T("旋转灵敏度"), v.OrbitSpeed, 0.25f, 3f, 1f, 0.05f, Fmt.Times, true);
            if (sp != v.OrbitSpeed)
            {
                v.OrbitSpeed = sp;
                s.PrefsChanged();
            }
            if (UiKit.Button(Row(32f), L.T("重置视角（F）"), ButtonKind.Normal, true)) v.FrameAvatar(s.Rig, Framing.Full, s.Photo.Aspect);
            Note(L.T("右键拖动旋转 · 中键拖动平移 · 滚轮推拉 · 换焦距时人物大小不变"));
        }

        // A new focal length keeps the avatar the same size in the picture: the camera moves back or closer, so the
        // lens changes the perspective rather than the framing (the wheel is for coming closer).
        void SetFocal(Studio s, float focal)
        {
            StudioCamera v = s.View;
            float old = v.FocalLength;
            v.FocalLength = focal;
            if (old > 0.5f) v.Distance = Mathf.Clamp(v.Distance * focal / old, 0.15f, 30f);
            s.PrefsChanged();
        }

        // ---- 灯光 ----

        void LightPanel(Studio s)
        {
            StudioLights l = s.Lights;
            StudioLights d = lightDefaults ?? (lightDefaults = new StudioLights());
            Header(L.T("主光"));
            float shownYaw = Mathf.DeltaAngle(0f, l.Yaw);
            float v = SliderRow(L.T("水平角度"), shownYaw, -180f, 180f, d.Yaw, 1f, Fmt.Degrees, true);
            if (v != shownYaw)
            {
                l.Yaw = v;
                s.PrefsChanged();
            }
            v = SliderRow(L.T("高度角度"), l.Pitch, -10f, 89f, d.Pitch, 1f, Fmt.Degrees, true);
            if (v != l.Pitch)
            {
                l.Pitch = v;
                s.PrefsChanged();
            }
            bool b = SwitchRow(L.T("跟随镜头"), l.FollowCamera, true);
            if (b != l.FollowCamera)
            {
                l.FollowCamera = b;
                s.PrefsChanged();
            }
            b = SwitchRow(L.T("在画面上拖动"), l.DragMode, true);
            if (b != l.DragMode) l.DragMode = b;
            if (l.DragMode) Note(L.T("左键在画面空白处拖动，转动主光"));

            Rect row = Row(32f);
            Color[] sw = StudioLights.Swatches;
            for (int i = 0; i < sw.Length; i++)
            {
                Rect cell = UiKit.Cell(row, i, sw.Length, 0f);
                if (UiKit.Swatch(cell.center, 12f, sw[i], Same(l.Tint, sw[i])))
                {
                    l.Tint = sw[i];
                    s.PrefsChanged();
                }
            }
            v = SliderRow(L.T("强度"), l.Intensity, 0f, 3f, d.Intensity, 0.05f, Fmt.Decimal, true);
            if (v != l.Intensity)
            {
                l.Intensity = v;
                s.PrefsChanged();
            }
            // the track is filled with the colour of the temperature
            Color kc = StudioLights.KelvinColor(l.Kelvin); // the colour the key light really gets
            kc.a = 1f;
            v = UiKit.Slider(Row(38f), L.T("色温"), l.Kelvin, 2500f, 10000f, d.Kelvin, 50f, Fmt.Kelvin, true, UiKit.SmallMuted, kc);
            if (v != l.Kelvin)
            {
                l.Kelvin = v;
                s.PrefsChanged();
            }

            Header(L.T("阴影与轮廓光"));
            b = SwitchRow(L.T("阴影"), l.Shadows, true);
            if (b != l.Shadows)
            {
                l.Shadows = b;
                s.PrefsChanged();
            }
            v = SliderRow(L.T("阴影强度"), l.ShadowStrength, 0f, 1f, d.ShadowStrength, 0.01f, Fmt.Percent, l.Shadows);
            if (v != l.ShadowStrength)
            {
                l.ShadowStrength = v;
                s.PrefsChanged();
            }
            b = SwitchRow(L.T("轮廓光"), l.Rim, true);
            if (b != l.Rim)
            {
                l.Rim = b;
                s.PrefsChanged();
            }
            v = SliderRow(L.T("轮廓光强度"), l.RimIntensity, 0f, 3f, d.RimIntensity, 0.05f, Fmt.Decimal, l.Rim);
            if (v != l.RimIntensity)
            {
                l.RimIntensity = v;
                s.PrefsChanged();
            }

            Header(L.T("环境"));
            v = SliderRow(L.T("环境光"), l.Ambient, 0f, 1.5f, d.Ambient, 0.01f, Fmt.Percent, true);
            if (v != l.Ambient)
            {
                l.Ambient = v;
                s.PrefsChanged();
            }
            b = SwitchRow(L.T("只用摄影棚灯光"), l.OnlyStudioLights, true);
            if (b != l.OnlyStudioLights)
            {
                l.OnlyStudioLights = b;
                s.PrefsChanged();
            }
            if (UiKit.Button(Row(32f), L.T("恢复默认灯光"), ButtonKind.Normal, true))
            {
                l.Yaw = d.Yaw;
                l.Pitch = d.Pitch;
                l.FollowCamera = d.FollowCamera;
                l.Intensity = d.Intensity;
                l.Kelvin = d.Kelvin;
                l.Tint = d.Tint;
                l.Shadows = d.Shadows;
                l.ShadowStrength = d.ShadowStrength;
                l.Rim = d.Rim;
                l.RimIntensity = d.RimIntensity;
                l.Ambient = d.Ambient;
                l.OnlyStudioLights = d.OnlyStudioLights;
                s.PrefsChanged();
            }
        }

        // ---- 背景 ----

        void BackgroundPanel(Studio s)
        {
            Backdrop b = s.Back;
            Rect row = Row(32f);
            UiKit.SegmentBack(row);
            BackdropMode mode = b.Mode, picked = mode;
            if (UiKit.Segment(row, 0, 4, L.T("场景"), mode == BackdropMode.Scene, true)) picked = BackdropMode.Scene;
            if (UiKit.Segment(row, 1, 4, L.T("纯色"), mode == BackdropMode.Solid, true)) picked = BackdropMode.Solid;
            if (UiKit.Segment(row, 2, 4, L.T("渐变"), mode == BackdropMode.Gradient, true)) picked = BackdropMode.Gradient;
            if (UiKit.Segment(row, 3, 4, L.T("透明"), mode == BackdropMode.Transparent, true)) picked = BackdropMode.Transparent;
            if (picked != mode)
            {
                b.Mode = picked;
                s.PrefsChanged();
            }

            switch (b.Mode)
            {
                case BackdropMode.Solid:
                {
                    Header(L.T("颜色"));
                    row = Row(34f);
                    Color[] sw = Backdrop.SolidSwatches;
                    for (int i = 0; i < sw.Length; i++)
                    {
                        Rect cell = UiKit.Cell(row, i, sw.Length, 0f);
                        if (UiKit.Swatch(cell.center, 13f, sw[i], Same(b.Solid, sw[i])))
                        {
                            b.Solid = sw[i];
                            s.PrefsChanged();
                        }
                    }
                    break;
                }
                case BackdropMode.Gradient:
                {
                    Header(L.T("预设"));
                    row = Row(40f);
                    int n = Mathf.Min(Backdrop.GradientNames.Length, Mathf.Min(Backdrop.GradientTop.Length, Backdrop.GradientBottom.Length));
                    for (int i = 0; i < n; i++)
                    {
                        Rect cell = UiKit.Cell(row, i, n, 10f);
                        if (UiKit.GradientTile(cell, Backdrop.GradientTop[i], Backdrop.GradientBottom[i], b.Gradient == i))
                        {
                            b.Gradient = i;
                            s.PrefsChanged();
                        }
                    }
                    Rect names = Row(16f);
                    for (int i = 0; i < n; i++) // the names are literals in Backdrop.cs
                        UiKit.Label(UiKit.Cell(names, i, n, 10f), L.T(Backdrop.GradientNames[i]), UiKit.CaptionCenter,
                            b.Gradient == i ? UiKit.Text : UiKit.Muted);
                    break;
                }
                case BackdropMode.Transparent:
                    Note(L.T("照片保存为透明背景的 PNG；画面里用灰色代表透明的部分。"));
                    break;
                default:
                    Note(L.T("保留场景原本的样子作为背景，场景里的物体也会拍进照片。"));
                    break;
            }
        }

        // ---- 输出 ----

        void OutputPanel(Studio s)
        {
            PhotoCapture p = s.Photo;
            Header(L.T("比例"));
            Rect row = Row(30f);
            UiKit.SegmentBack(row);
            string[] an = PhotoCapture.AspectNames;
            for (int i = 0; i < an.Length; i++)
                if (UiKit.Segment(row, i, an.Length, an[i], p.AspectIndex == i, true) && p.AspectIndex != i)
                {
                    p.AspectIndex = i;
                    s.PrefsChanged();
                }

            Header(L.T("尺寸（短边）"));
            row = Row(30f);
            UiKit.SegmentBack(row);
            int[] sizes = PhotoCapture.Sizes;
            for (int i = 0; i < sizes.Length; i++)
                if (UiKit.Segment(row, i, sizes.Length, UiKit.Format(Fmt.Int, sizes[i]), p.SizeIndex == i, true) && p.SizeIndex != i)
                {
                    p.SizeIndex = i;
                    s.PrefsChanged();
                }
            Note(Pixels(s));

            Header(L.T("定时"));
            row = Row(30f);
            UiKit.SegmentBack(row);
            for (int i = 0; i < timerNames.Length; i++)
                if (UiKit.Segment(row, i, timerNames.Length, timerNames[i], p.TimerIndex == i, true) && p.TimerIndex != i)
                {
                    p.TimerIndex = i;
                    s.PrefsChanged();
                }
            bool b = SwitchRow(L.T("超采样（更细腻，稍慢）"), p.Supersample, true);
            if (b != p.Supersample)
            {
                p.Supersample = b;
                s.PrefsChanged();
            }

            Header(L.T("照片"));
            if (UiKit.Button(Row(32f), L.T("打开照片文件夹"), ButtonKind.Normal, true)) OpenPhotoDir(s);
            Note(PhotoDir);

            Header(L.T("界面"));
            float shown = scaleDraft >= 0f ? scaleDraft : UserScale;
            float v = SliderRow(L.T("界面大小"), shown, 0.75f, 1.5f, 1f, 0.05f, Fmt.Percent, true);
            if (GUIUtility.hotControl == UiKit.LastId) scaleDraft = v;
            else if (scaleDraft >= 0f || v != UserScale)
            {
                UserScale = Mathf.Clamp(scaleDraft >= 0f ? scaleDraft : v, 0.75f, 1.5f);
                scaleDraft = -1f;
                s.PrefsChanged();
            }
        }

        void OpenPhotoDir(Studio s)
        {
            string dir = PhotoDir;
            try
            {
                Directory.CreateDirectory(dir);
            }
            catch (Exception ex)
            {
                s.Toast(L.F("无法创建照片文件夹：{0}", ex.Message), 4f);
                return;
            }
            // the folder itself, with the pictures in it (Reveal would show its parent, with the folder selected)
            try
            {
                Action<string> open = StudioHost.OpenFolder;
                if (open != null) open(dir);
                else Application.OpenURL(new Uri(dir).AbsoluteUri);
            }
            catch (Exception ex)
            {
                // an exception would end the whole interface pass: say where the folder is instead
                Debug.LogWarning("[MioVRCA] 没能打开照片文件夹：" + ex.Message);
                s.Toast(L.F("照片文件夹：{0}", dir), 5f);
            }
        }

        // ---- shutter, chips, summary ----

        void ShutterGroup(Studio s)
        {
            PhotoCapture p = s.Photo;
            const float d = 68f, cw = 112f, ch = 30f;
            var sh = new Rect(vw - Margin - 12f - d, vh - Margin - 12f - d, d, d);

            // timer and aspect chips left of the shutter: left click = next, right click = previous
            var timer = new Rect(sh.x - 12f - cw, sh.y + 2f, cw, ch);
            var aspect = new Rect(timer.x, sh.yMax - 2f - ch, cw, ch);
            int nt = PhotoCapture.Timers.Length, na = PhotoCapture.AspectNames.Length;
            int ti = Mathf.Clamp(p.TimerIndex, 0, nt - 1), ai = Mathf.Clamp(p.AspectIndex, 0, na - 1);
            Rec(timer);
            Rec(aspect);
            int c = UiKit.ButtonAny(timer, timerChips[ti], ButtonKind.Floating, true, true, UiKit.SmallCenter);
            if (c >= 0)
            {
                p.TimerIndex = (ti + (c == 0 ? 1 : nt - 1)) % nt;
                s.PrefsChanged();
            }
            c = UiKit.ButtonAny(aspect, aspectChips[ai], ButtonKind.Floating, true, true, UiKit.SmallCenter);
            if (c >= 0)
            {
                p.AspectIndex = (ai + (c == 0 ? 1 : na - 1)) % na;
                s.PrefsChanged();
            }

            // what the picture will be, above
            string sum = Summary(s);
            if (!ReferenceEquals(sum, summaryFor) || Mathf.Abs(summaryScale - UiKit.S) > 0.001f)
            {
                summaryFor = sum;
                summaryScale = UiKit.S;
                summaryW = UiKit.Measure(sum, UiKit.SmallCenter) + 26f;
            }
            float sw = summaryW;
            var sr = new Rect(sh.xMax - sw, sh.y - 10f - 28f, sw, 28f);
            Rec(sr);
            UiKit.Box(sr, UiKit.PanelBg, 14f);
            UiKit.Label(sr, sum, UiKit.SmallCenter, UiKit.Muted);

            // the shutter (pressed again while counting down = cancel)
            Rec(sh);
            int id = GUIUtility.GetControlID(FocusType.Passive);
            Rect raw = UiKit.Px(sh);
            if (UiKit.Click(id, raw, true, false) == 0)
            {
                if (p.Countdown > 0f)
                {
                    p.Cancel();
                    s.Toast(L.T("已取消倒计时"), 1.5f);
                }
                else s.RequestShot();
            }
            if (Event.current.type == EventType.Repaint)
            {
                bool hover = UiKit.Hover(raw, id), down = GUIUtility.hotControl == id;
                Vector2 c0 = sh.center;
                UiKit.Disc(c0, d / 2f + 3f, UiKit.Alpha(UiKit.Bg, 0.35f));
                UiKit.Disc(c0, d / 2f, Color.white);
                UiKit.Disc(c0, d / 2f - 5f, down ? UiKit.AccentDown : (hover ? UiKit.AccentHover : UiKit.AccentFill));
                if (p.Countdown > 0f) UiKit.Label(sh, UiKit.N(Mathf.CeilToInt(p.Countdown)), UiKit.ShutterNumber);
                else UiKit.Ring(c0, d / 2f - 14f, 1.5f, UiKit.Alpha(Color.white, 0.4f));
            }
        }

        // ---- recent pictures ----

        void Recent(Studio s)
        {
            List<Shot> list = s.Photo.Recent;
            if (list == null || list.Count == 0) return;
            const float h = 72f;
            float left = Margin + railW + 8f + (Panel != StudioPanel.None ? PanelW + 8f : 0f) + 8f;
            float x = vw - Margin;
            bool repaint = Event.current.type == EventType.Repaint;
            Shot hovered = null;
            var hoveredRect = new Rect();
            for (int i = 0; i < list.Count; i++)
            {
                Shot shot = list[i];
                if (shot == null) continue;
                Texture2D t = shot.Thumb;
                float w = h;
                if (t != null && t.height > 0) w = Mathf.Clamp(h * t.width / t.height, h * 0.45f, h * 2f);
                if (x - w - 3f < left) break;
                x -= w + 3f;
                var r = new Rect(x, Top + 3f, w, h);
                x -= 3f + 8f;
                Rect outer = UiKit.Inset(r, -3f);
                Rec(outer);
                int id = GUIUtility.GetControlID(FocusType.Passive);
                Rect raw = UiKit.Px(outer);
                if (UiKit.Click(id, raw, true, false) == 0) Reveal(s, shot.Path);
                if (!repaint) continue;
                UiKit.Box(outer, UiKit.PanelBg, 10f);
                if (t != null) GUI.DrawTexture(UiKit.Px(r), t, ScaleMode.ScaleAndCrop, true, 0f, Color.white, 0f, 7f * UiKit.S);
                else UiKit.Box(r, UiKit.Raised, 7f);
                if (UiKit.Hover(raw, id))
                {
                    UiKit.Frame(outer, UiKit.Accent, 2f, 10f);
                    hovered = shot;
                    hoveredRect = outer;
                }
            }
            if (hovered == null) return;

            // the file name under the hovered picture
            if (!ReferenceEquals(hovered, nameShot))
            {
                nameShot = hovered;
                shotName = string.IsNullOrEmpty(hovered.Path) ? "" : Path.GetFileName(hovered.Path);
            }
            string hint = L.T("点击在文件夹中显示");
            float tw = Mathf.Max(UiKit.Measure(shotName, UiKit.Small), UiKit.Measure(hint, UiKit.Caption)) + 24f;
            var tip = new Rect(Mathf.Max(left, hoveredRect.xMax - tw), hoveredRect.yMax + 6f, tw, 40f);
            UiKit.Box(tip, UiKit.PanelBg, 8f);
            UiKit.Label(new Rect(tip.x + 12f, tip.y + 4f, tw - 24f, 18f), shotName, UiKit.Small);
            UiKit.Label(new Rect(tip.x + 12f, tip.y + 21f, tw - 24f, 15f), hint, UiKit.Caption);
        }

        // ---- over everything ----

        void Countdown(Studio s)
        {
            float cd = s.Photo.Countdown;
            int n = Mathf.CeilToInt(cd);
            float a = Mathf.Clamp01(0.35f + 0.65f * (cd - (n - 1))); // each second starts bright and fades
            Vector2 c = s.FrameRect().center / UiKit.S;
            UiKit.Disc(c, 86f, UiKit.Alpha(UiKit.Bg, 0.4f));
            var r = new Rect(c.x - 150f, c.y - 90f, 300f, 180f);
            UiKit.Label(new Rect(r.x, r.y + 3f, r.width, r.height), UiKit.N(n), UiKit.Huge, new Color(0f, 0f, 0f, 0.35f * a));
            UiKit.Label(r, UiKit.N(n), UiKit.Huge, new Color(1f, 1f, 1f, a));
            UiKit.Label(new Rect(c.x - 150f, c.y + 94f, 300f, 18f), L.T("Esc 取消"), UiKit.CaptionCenter, UiKit.Text);
        }

        void Toast(Studio s)
        {
            string text = s.ToastText;
            float a = Mathf.Clamp01(s.ToastLeft / 0.3f); // fades out in the last 0.3 s
            float maxW = Mathf.Min(vw - 48f, 560f);
            float tw = Mathf.Min(UiKit.Measure(text, UiKit.ToastText) + 2f, maxW - 36f);
            float th = Mathf.Max(18f, UiKit.MeasureHeight(text, UiKit.ToastText, tw));
            var r = new Rect((vw - tw - 36f) / 2f, Top, tw + 36f, th + 18f);
            float radius = Mathf.Min(r.height / 2f, 18f);
            UiKit.Box(r, UiKit.Alpha(UiKit.Card, 0.97f * a), radius);
            UiKit.Frame(r, UiKit.Alpha(UiKit.Line, a), 1f, radius);
            UiKit.Label(new Rect(r.x + 18f, r.y + 9f, tw, th), text, UiKit.ToastText, UiKit.Alpha(UiKit.Text, a));
        }

        void StatusLine(Studio s)
        {
            string hint = s.Handles != null ? s.Handles.Hint() : null;
            bool strong = !string.IsNullOrEmpty(hint);
            if (!strong)
                hint = s.Lights != null && s.Lights.DragMode
                    ? L.T("左键拖动关节摆姿势 · 左键拖动空白处转动主光 · 右键拖动旋转视角 · 中键平移 · 滚轮缩放 · Space 拍照")
                    : L.T("左键拖动关节摆姿势 · 右键拖动旋转视角 · 中键平移 · 滚轮缩放 · Space 拍照 · Tab 隐藏界面 · Ctrl+Z 撤销");
            // the room left of the shutter group
            float maxW = Mathf.Max(160f, vw - Margin - 24f - (Margin + 12f + 68f + 12f + 112f) - 16f);
            float tw = UiKit.Measure(hint, UiKit.Small) + 1f, th = 16f;
            GUIStyle st = UiKit.Small;
            if (tw > maxW)
            {
                tw = maxW;
                st = UiKit.SmallWrap;
                th = UiKit.MeasureHeight(hint, st, tw);
            }
            var r = new Rect(Margin, vh - Margin - th - 14f, tw + 24f, th + 14f);
            UiKit.Box(r, UiKit.Alpha(UiKit.PanelBg, 0.85f), Mathf.Min(r.height / 2f, 12f));
            UiKit.Label(new Rect(r.x + 12f, r.y + 7f, tw, th), hint, st, strong ? UiKit.Text : UiKit.Muted);
        }

        // the only thing left while the interface is hidden; clicking it shows the interface again
        void HiddenPill(Studio s)
        {
            string t = L.T("Tab 显示界面");
            float w = UiKit.Measure(t, UiKit.SmallCenter) + 24f;
            var r = new Rect(Margin, vh - Margin - 28f, w, 28f);
            Rect raw = UiKit.Px(r);
            pillRaw = raw;
            int id = GUIUtility.GetControlID(FocusType.Passive);
            if (UiKit.Click(id, raw, true, false) == 0)
            {
                s.UiHidden = false;
                hiddenSeen = false;
                return;
            }
            if (Event.current.type != EventType.Repaint) return;
            bool hover = UiKit.Hover(raw, id);
            UiKit.Box(r, UiKit.Alpha(UiKit.PanelBg, hover ? 1f : 0.6f), 14f);
            UiKit.Label(r, t, UiKit.SmallCenter, hover ? UiKit.Text : UiKit.Muted);
        }
    }
}
