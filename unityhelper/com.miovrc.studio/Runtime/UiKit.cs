// Hand-drawn widgets for the studio's interface, in MioVRCA's colours: buttons, switches, sliders, segmented rows,
// colour swatches, gradient tiles, a text field and a thin scroll bar. Layout is in unscaled units (one unit = one
// pixel at UI scale 1); every widget turns its rect into raw GUI pixels itself (Px) and the fonts are sized for the
// scale, so text is rendered at its real size and stays sharp (scaling through GUI.matrix would stretch the glyphs).
// Clicks and drags go through IMGUI control ids and GUIUtility.hotControl; the mouse wheel never changes a value.
using System.Collections.Generic;
using System.Globalization;
using UnityEngine;

namespace MioVRCA.Studio
{
    internal enum Fmt { Int, Percent, Degrees, Kelvin, Millimetres, Times, Decimal }

    internal enum ButtonKind
    {
        Normal,   // raised control
        On,       // selected: accent-soft fill, accent border
        Primary,  // accent fill behind white text
        Ghost,    // no fill until hovered, muted text
        Floating, // over the view: panel fill with a thin border
    }

    internal static class UiKit
    {
        // MioVRCA palette
        public static readonly Color Bg = Rgb(0x14151f, 1f);
        public static readonly Color PanelBg = Rgb(0x191b27, 0.94f);
        public static readonly Color Card = Rgb(0x1f2231, 1f);
        public static readonly Color Raised = Rgb(0x282b3d, 1f);
        public static readonly Color RaisedHover = Rgb(0x33374d, 1f);
        public static readonly Color RaisedDown = Rgb(0x222536, 1f);
        public static readonly Color Line = Rgb(0x2e3247, 1f);
        public static readonly Color Text = Rgb(0xeceef6, 1f);
        public static readonly Color Muted = Rgb(0x9a9eb5, 1f);
        public static readonly Color Faint = Rgb(0x888ca5, 1f);
        public static readonly Color Disabled = Rgb(0x888ca5, 0.5f);
        public static readonly Color Accent = Rgb(0xff5a6a, 1f);
        public static readonly Color AccentFill = Rgb(0xd6364a, 1f);
        public static readonly Color AccentHover = Rgb(0xe4475a, 1f);
        public static readonly Color AccentDown = Rgb(0xbd2e40, 1f);
        public static readonly Color AccentSoft = Rgb(0xff5a6a, 0.14f);
        public static readonly Color AccentSoftHover = Rgb(0xff5a6a, 0.22f);
        public static readonly Color Ok = Rgb(0x3fd0a6, 1f);
        public static readonly Color Blue = Rgb(0x4c8dff, 1f);
        public static readonly Color Warn = Rgb(0xffb547, 1f);
        public static readonly Color Wash = new Color(1f, 1f, 1f, 0.05f); // hover on a transparent control

        const int ButtonHint = 0x4d690001, SliderHint = 0x4d690002, ScrollHint = 0x4d690003;

        // every style with its size at scale 1, so the sizes can follow the scale
        static readonly List<GUIStyle> styles = new List<GUIStyle>();
        static readonly List<int> baseSizes = new List<int>();
        static float fontScale = -1f;

        public static readonly GUIStyle Body = Make(13, Text, TextAnchor.MiddleLeft, FontStyle.Normal, false);
        public static readonly GUIStyle BodyCenter = Make(13, Text, TextAnchor.MiddleCenter, FontStyle.Normal, false);
        public static readonly GUIStyle BodyWrap = Make(13, Text, TextAnchor.UpperLeft, FontStyle.Normal, true);
        public static readonly GUIStyle Small = Make(12, Text, TextAnchor.MiddleLeft, FontStyle.Normal, false);
        public static readonly GUIStyle SmallCenter = Make(12, Text, TextAnchor.MiddleCenter, FontStyle.Normal, false);
        public static readonly GUIStyle SmallRight = Make(12, Text, TextAnchor.MiddleRight, FontStyle.Normal, false);
        public static readonly GUIStyle SmallMuted = Make(12, Muted, TextAnchor.MiddleLeft, FontStyle.Normal, false);
        public static readonly GUIStyle SmallWrap = Make(12, Muted, TextAnchor.UpperLeft, FontStyle.Normal, true);
        public static readonly GUIStyle Caption = Make(11, Muted, TextAnchor.MiddleLeft, FontStyle.Normal, false);
        public static readonly GUIStyle CaptionCenter = Make(11, Muted, TextAnchor.MiddleCenter, FontStyle.Normal, false);
        public static readonly GUIStyle CaptionWrap = Make(11, Muted, TextAnchor.UpperLeft, FontStyle.Normal, true);
        public static readonly GUIStyle Title = Make(13, Text, TextAnchor.MiddleLeft, FontStyle.Bold, false);
        public static readonly GUIStyle PanelTitle = Make(15, Text, TextAnchor.MiddleLeft, FontStyle.Bold, false);
        public static readonly GUIStyle CardTitle = Make(15, Text, TextAnchor.UpperLeft, FontStyle.Bold, true);
        public static readonly GUIStyle ToastText = Make(13, Text, TextAnchor.MiddleCenter, FontStyle.Normal, true);
        public static readonly GUIStyle ShutterNumber = Make(26, Text, TextAnchor.MiddleCenter, FontStyle.Bold, false);
        public static readonly GUIStyle Huge = Make(120, Text, TextAnchor.MiddleCenter, FontStyle.Bold, false);
        public static readonly GUIStyle Field = MakeField();

        public static float S = 1f; // the UI scale of this pass

        // the clip of the current group (raw pixels, in the group's own coordinates) and the group's corner on screen
        static readonly Rect Everything = new Rect(-100000f, -100000f, 200000f, 200000f);
        static Rect clip = Everything;
        static Vector2 origin;
        static readonly Rect[] clipStack = new Rect[8];
        static readonly Vector2[] originStack = new Vector2[8];
        static int depth;

        // the control that holds the mouse because of us (to let go of it if its id ever goes missing)
        public static int Claimed;
        static int pressedButton;
        static float grab;

        // no hover effects (the view is being dragged under the interface)
        public static bool NoHover;

        // the id of the last slider, so a caller can tell whether it is being dragged
        public static int LastId;

        static readonly GUIContent tmp = new GUIContent();
        static readonly string[] numbers = new string[200];
        static readonly Dictionary<int, string>[] formatted = new Dictionary<int, string>[7];

        public static Color Rgb(int hex, float a)
        {
            return new Color(((hex >> 16) & 255) / 255f, ((hex >> 8) & 255) / 255f, (hex & 255) / 255f, a);
        }

        public static Color Alpha(Color c, float a)
        {
            c.a *= a;
            return c;
        }

        static GUIStyle Make(int size, Color c, TextAnchor anchor, FontStyle fs, bool wrap)
        {
            var st = new GUIStyle();
            st.normal.textColor = c;
            st.alignment = anchor;
            st.fontStyle = fs;
            st.wordWrap = wrap;
            st.clipping = TextClipping.Clip;
            st.richText = false;
            st.fontSize = size;
            styles.Add(st);
            baseSizes.Add(size);
            return st;
        }

        static GUIStyle MakeField()
        {
            GUIStyle st = Make(13, Text, TextAnchor.MiddleLeft, FontStyle.Normal, false);
            st.hover.textColor = Text;
            st.active.textColor = Text;
            st.focused.textColor = Text;
            return st;
        }

        // Starts a pass (DrawPanels / DrawOver): the scale, the font sizes for it, no open groups.
        public static void Begin(float scale)
        {
            S = Mathf.Max(0.3f, scale);
            if (Mathf.Abs(S - fontScale) > 0.0001f)
            {
                fontScale = S;
                for (int i = 0; i < styles.Count; i++) styles[i].fontSize = Mathf.Max(8, Mathf.RoundToInt(baseSizes[i] * S));
                Field.padding = new RectOffset(Mathf.RoundToInt(10f * S), Mathf.RoundToInt(30f * S), 0, 0);
            }
            depth = 0;
            clip = Everything;
            origin = Vector2.zero;
        }

        // Ends a pass: closes groups left open (after an exception in a panel) and lets go of a lost mouse capture
        // (a MouseUp nobody of ours took, or a MouseMove, which only comes with no button held).
        public static void End()
        {
            while (depth > 0) EndClip();
            Event e = Event.current;
            if (e != null && (e.rawType == EventType.MouseUp || e.rawType == EventType.MouseMove) && Claimed != 0) ReleaseClaim();
        }

        // Lets go of the mouse if one of our controls still holds it. For a press whose MouseUp never comes (it went
        // to another window after the Game view lost focus, and the Game view sends no MouseMove either): the control
        // would keep the mouse and every other control would stay dead. Call it from OnGUI.
        public static void ReleaseClaim()
        {
            if (Claimed != 0 && GUIUtility.hotControl == Claimed) GUIUtility.hotControl = 0;
            Claimed = 0;
        }

        public static bool Repaint
        {
            get { return Event.current.type == EventType.Repaint; }
        }

        // unscaled -> raw GUI pixels (in the current group), on whole pixels
        public static Rect Px(Rect u)
        {
            float x = Mathf.Round(u.x * S), y = Mathf.Round(u.y * S);
            return new Rect(x, y, Mathf.Round(u.xMax * S) - x, Mathf.Round(u.yMax * S) - y);
        }

        // a raw rect of the current group in screen pixels
        public static Rect ToScreen(Rect raw)
        {
            return new Rect(raw.x + origin.x, raw.y + origin.y, raw.width, raw.height);
        }

        public static Rect Inset(Rect u, float d)
        {
            return new Rect(u.x + d, u.y + d, u.width - 2f * d, u.height - 2f * d);
        }

        // the i-th of n cells of a row, with gaps between them
        public static Rect Cell(Rect row, int i, int n, float gap)
        {
            float w = (row.width - gap * (n - 1)) / n;
            return new Rect(row.x + i * (w + gap), row.y, w, row.height);
        }

        public static string N(int i)
        {
            if (i < 0 || i >= numbers.Length) return i.ToString(CultureInfo.InvariantCulture);
            return numbers[i] ?? (numbers[i] = i.ToString(CultureInfo.InvariantCulture));
        }

        // a value as text, cached by its rounded value (no new string for a value seen before)
        public static string Format(Fmt f, float v)
        {
            int key;
            switch (f)
            {
                case Fmt.Percent:
                case Fmt.Times:
                case Fmt.Decimal:
                    key = Mathf.RoundToInt(v * 100f);
                    break;
                case Fmt.Kelvin:
                    key = Mathf.RoundToInt(v / 50f) * 50;
                    break;
                default:
                    key = Mathf.RoundToInt(v);
                    break;
            }
            Dictionary<int, string> d = formatted[(int)f];
            if (d == null) formatted[(int)f] = d = new Dictionary<int, string>();
            string s;
            if (d.TryGetValue(key, out s)) return s;
            CultureInfo inv = CultureInfo.InvariantCulture;
            switch (f)
            {
                case Fmt.Percent: s = key.ToString(inv) + "%"; break;
                case Fmt.Degrees: s = key.ToString(inv) + "°"; break;
                case Fmt.Kelvin: s = key.ToString(inv) + " K"; break;
                case Fmt.Millimetres: s = key.ToString(inv) + " mm"; break;
                case Fmt.Times: s = (key / 100f).ToString("0.00", inv) + "×"; break;
                case Fmt.Decimal: s = (key / 100f).ToString("0.00", inv); break;
                default: s = key.ToString(inv); break;
            }
            if (d.Count < 4096) d[key] = s;
            return s;
        }

        // ---- measuring and drawing ----

        public static float Measure(string text, GUIStyle st)
        {
            if (string.IsNullOrEmpty(text)) return 0f;
            tmp.text = text;
            return st.CalcSize(tmp).x / S;
        }

        public static float MeasureHeight(string text, GUIStyle st, float width)
        {
            if (string.IsNullOrEmpty(text)) return 0f;
            tmp.text = text;
            return st.CalcHeight(tmp, width * S) / S;
        }

        public static void Label(Rect u, string text, GUIStyle st)
        {
            if (Event.current.type != EventType.Repaint || string.IsNullOrEmpty(text)) return;
            tmp.text = text;
            st.Draw(Px(u), tmp, false, false, false, false);
        }

        public static void Label(Rect u, string text, GUIStyle st, Color c)
        {
            if (Event.current.type != EventType.Repaint || string.IsNullOrEmpty(text)) return;
            Color old = st.normal.textColor;
            st.normal.textColor = c;
            tmp.text = text;
            st.Draw(Px(u), tmp, false, false, false, false);
            st.normal.textColor = old;
        }

        public static void Box(Rect u, Color c, float radius)
        {
            if (Event.current.type != EventType.Repaint || u.width <= 0f || u.height <= 0f) return;
            Draw2D.Box(Px(u), c, radius * S);
        }

        // width in raw pixels (borders stay thin at any scale)
        public static void Frame(Rect u, Color c, float width, float radius)
        {
            if (Event.current.type != EventType.Repaint || u.width <= 0f || u.height <= 0f) return;
            Draw2D.Frame(Px(u), c, width, radius * S);
        }

        public static void Disc(Vector2 center, float radius, Color c)
        {
            if (Event.current.type != EventType.Repaint) return;
            Draw2D.Disc(center * S, radius * S, c);
        }

        public static void Ring(Vector2 center, float radius, float width, Color c)
        {
            if (Event.current.type != EventType.Repaint) return;
            Draw2D.Ring(center * S, radius * S, width, c);
        }

        // ---- groups ----

        // A clipped group: what is drawn inside is cut at its edges, rects inside are relative to its corner, and
        // the mouse only reaches the widgets inside where the group is visible.
        public static void BeginClip(Rect u)
        {
            Rect raw = Px(u);
            if (depth < clipStack.Length)
            {
                clipStack[depth] = clip;
                originStack[depth] = origin;
            }
            depth++;
            float x0 = Mathf.Max(clip.x, raw.x), y0 = Mathf.Max(clip.y, raw.y);
            float x1 = Mathf.Min(clip.xMax, raw.xMax), y1 = Mathf.Min(clip.yMax, raw.yMax);
            clip = new Rect(x0 - raw.x, y0 - raw.y, Mathf.Max(0f, x1 - x0), Mathf.Max(0f, y1 - y0));
            origin += raw.position;
            GUI.BeginGroup(raw);
        }

        public static void EndClip()
        {
            if (depth <= 0) return;
            GUI.EndGroup();
            depth--;
            if (depth < clipStack.Length)
            {
                clip = clipStack[depth];
                origin = originStack[depth];
            }
            else
            {
                clip = Everything;
                origin = Vector2.zero;
            }
        }

        // ---- input ----

        public static bool MouseIn(Rect raw)
        {
            Vector2 m = Event.current.mousePosition;
            return raw.Contains(m) && clip.Contains(m);
        }

        public static bool Hover(Rect raw, int id)
        {
            int hot = GUIUtility.hotControl;
            return !NoHover && (hot == 0 || hot == id) && MouseIn(raw);
        }

        static void Grab(int id)
        {
            GUIUtility.hotControl = id;
            Claimed = id;
        }

        static void Release()
        {
            GUIUtility.hotControl = 0;
            Claimed = 0;
        }

        // A press and a release on the rect: 0 = left click, 1 = right click (only when right), -1 = none.
        public static int Click(int id, Rect raw, bool enabled, bool right)
        {
            Event e = Event.current;
            switch (e.GetTypeForControl(id))
            {
                case EventType.MouseDown:
                    if (enabled && (e.button == 0 || (right && e.button == 1)) && MouseIn(raw))
                    {
                        Grab(id);
                        pressedButton = e.button;
                        e.Use();
                    }
                    break;
                case EventType.MouseDrag:
                    if (GUIUtility.hotControl == id) e.Use();
                    break;
                case EventType.MouseUp:
                    if (GUIUtility.hotControl == id)
                    {
                        Release();
                        e.Use();
                        if (enabled && e.button == pressedButton && MouseIn(raw)) return pressedButton;
                    }
                    break;
            }
            return -1;
        }

        // the mouse wheel over the rect scrolls (and is used); returns the new scroll
        public static float Wheel(Rect u, float scroll, float maxScroll)
        {
            Event e = Event.current;
            if (e.type == EventType.ScrollWheel && maxScroll > 0f && MouseIn(Px(u)))
            {
                scroll = Mathf.Clamp(scroll + e.delta.y * 14f, 0f, maxScroll);
                e.Use();
            }
            return scroll;
        }

        // ---- widgets ----

        public static bool Button(Rect u, string text, ButtonKind kind, bool enabled)
        {
            return ButtonAny(u, text, kind, enabled, false, BodyCenter) == 0;
        }

        public static bool Button(Rect u, string text, ButtonKind kind, bool enabled, GUIStyle st)
        {
            return ButtonAny(u, text, kind, enabled, false, st) == 0;
        }

        // the mouse button that clicked it (right clicks only when right), or -1
        public static int ButtonAny(Rect u, string text, ButtonKind kind, bool enabled, bool right, GUIStyle st)
        {
            int id = GUIUtility.GetControlID(ButtonHint, FocusType.Passive);
            Rect raw = Px(u);
            int click = Click(id, raw, enabled, right);
            if (Event.current.type == EventType.Repaint)
            {
                bool hover = enabled && Hover(raw, id);
                bool down = enabled && GUIUtility.hotControl == id;
                Color bg, fg = Text, border = Color.clear;
                float radius = Mathf.Min(8f, u.height / 2f);
                switch (kind)
                {
                    case ButtonKind.On:
                        bg = hover ? AccentSoftHover : AccentSoft;
                        border = Accent;
                        break;
                    case ButtonKind.Primary:
                        bg = down ? AccentDown : (hover ? AccentHover : AccentFill);
                        break;
                    case ButtonKind.Ghost:
                        bg = down ? RaisedDown : (hover ? Raised : Color.clear);
                        fg = hover ? Text : Muted;
                        break;
                    case ButtonKind.Floating:
                        bg = down ? RaisedDown : (hover ? RaisedHover : PanelBg);
                        border = Line;
                        break;
                    default:
                        bg = down ? RaisedDown : (hover ? RaisedHover : Raised);
                        break;
                }
                if (!enabled)
                {
                    bg = Alpha(bg, 0.5f);
                    border = Alpha(border, 0.5f);
                    fg = Disabled;
                }
                if (bg.a > 0f) Draw2D.Box(raw, bg, radius * S);
                if (border.a > 0f) Draw2D.Frame(raw, border, 1f, radius * S);
                Label(u, text, st, fg);
            }
            return click;
        }

        // A slot (pose or expression): filled ones are raised with a dot, empty ones only outlined. Left click =
        // 0, right click = 1.
        public static int Slot(Rect u, string text, bool filled, bool enabled)
        {
            int id = GUIUtility.GetControlID(ButtonHint, FocusType.Passive);
            Rect raw = Px(u);
            int click = Click(id, raw, enabled, true);
            if (Event.current.type == EventType.Repaint)
            {
                bool hover = enabled && Hover(raw, id);
                bool down = enabled && GUIUtility.hotControl == id;
                float a = enabled ? 1f : 0.5f;
                if (filled)
                {
                    Draw2D.Box(raw, Alpha(down ? RaisedDown : (hover ? RaisedHover : Raised), a), 8f * S);
                    Disc(new Vector2(u.xMax - 7f, u.y + 7f), 2.5f, Alpha(Accent, a));
                }
                else
                {
                    if (hover) Draw2D.Box(raw, Wash, 8f * S);
                    Draw2D.Frame(raw, Alpha(Line, a * 1.6f), 1f, 8f * S);
                    Draw2D.Frame(raw, Alpha(Color.white, a * 0.08f), 1f, 8f * S);
                }
                Label(u, text, BodyCenter, !enabled ? Disabled : (filled || hover ? Text : Faint));
            }
            return click;
        }

        // the background of a segmented row; the cells go on it with Segment
        public static void SegmentBack(Rect row)
        {
            Box(row, Raised, 8f);
        }

        public static bool Segment(Rect row, int i, int n, string text, bool on, bool enabled)
        {
            Rect u = Cell(new Rect(row.x + 2f, row.y + 2f, row.width - 4f, row.height - 4f), i, n, 2f);
            int id = GUIUtility.GetControlID(ButtonHint, FocusType.Passive);
            Rect raw = Px(u);
            int click = Click(id, raw, enabled, false);
            if (Event.current.type == EventType.Repaint)
            {
                bool hover = enabled && Hover(raw, id);
                if (on)
                {
                    Draw2D.Box(raw, Alpha(hover ? AccentSoftHover : AccentSoft, enabled ? 1f : 0.5f), 6f * S);
                    Draw2D.Frame(raw, Alpha(Accent, enabled ? 1f : 0.4f), 1f, 6f * S);
                }
                else if (hover) Draw2D.Box(raw, Wash, 6f * S);
                Label(u, text, SmallCenter, !enabled ? Disabled : (on || hover ? Text : Muted));
            }
            return click == 0;
        }

        // a switch row: the label on the left, the switch on the right; clicking anywhere on the row flips it
        public static bool Switch(Rect u, string label, bool on, bool enabled)
        {
            int id = GUIUtility.GetControlID(ButtonHint, FocusType.Passive);
            Rect hit = new Rect(u.x - 6f, u.y, u.width + 12f, u.height);
            Rect raw = Px(hit);
            if (Click(id, raw, enabled, false) == 0) on = !on;
            if (Event.current.type == EventType.Repaint)
            {
                bool hover = enabled && Hover(raw, id);
                if (hover) Draw2D.Box(raw, Wash, 8f * S);
                Label(new Rect(u.x, u.y, u.width - 44f, u.height), label, Body, enabled ? Text : Disabled);
                Rect track = new Rect(u.xMax - 34f, u.y + (u.height - 18f) / 2f, 34f, 18f);
                float a = enabled ? 1f : 0.45f;
                Box(track, Alpha(on ? AccentFill : Rgb(0x3a3e55, 1f), a), 9f);
                float kx = on ? track.xMax - 9f : track.x + 9f;
                Disc(new Vector2(kx, track.center.y), 7f, Alpha(on ? Color.white : Text, a));
            }
            return on;
        }

        public static float Slider(Rect u, string label, float v, float min, float max, float def, float step, Fmt fmt, bool enabled)
        {
            return Slider(u, label, v, min, max, def, step, fmt, enabled, SmallMuted, Accent);
        }

        // A slider row: the label and the value on top, the track below. Click or drag on the track to set it,
        // double click for def. The mouse wheel is left alone (it scrolls the panel instead).
        public static float Slider(Rect u, string label, float v, float min, float max, float def, float step, Fmt fmt,
            bool enabled, GUIStyle labelStyle, Color fill)
        {
            int id = GUIUtility.GetControlID(SliderHint, FocusType.Passive);
            LastId = id;
            Event e = Event.current;
            float cy = u.yMax - 9f;
            Rect track = new Rect(u.x + 7f, cy - 2f, u.width - 14f, 4f);
            Rect hit = Px(new Rect(u.x, u.yMax - 20f, u.width, 20f));
            Rect trackRaw = Px(track);
            switch (e.GetTypeForControl(id))
            {
                case EventType.MouseDown:
                    if (enabled && e.button == 0 && MouseIn(hit))
                    {
                        Grab(id);
                        e.Use();
                        v = e.clickCount == 2 ? def : FromMouse(e.mousePosition.x, trackRaw, min, max, step);
                    }
                    break;
                case EventType.MouseDrag:
                    if (GUIUtility.hotControl == id)
                    {
                        v = FromMouse(e.mousePosition.x, trackRaw, min, max, step);
                        e.Use();
                    }
                    break;
                case EventType.MouseUp:
                    if (GUIUtility.hotControl == id)
                    {
                        Release();
                        e.Use();
                    }
                    break;
                case EventType.Repaint:
                {
                    bool hot = GUIUtility.hotControl == id;
                    bool hover = enabled && (hot || Hover(hit, id));
                    float valueW = 64f;
                    Label(new Rect(u.x, u.y, u.width - valueW, u.height - 18f), label, labelStyle, enabled ? labelStyle.normal.textColor : Disabled);
                    Label(new Rect(u.xMax - valueW, u.y, valueW, u.height - 18f), Format(fmt, v), SmallRight, enabled ? Text : Disabled);
                    float t = max > min ? Mathf.Clamp01((v - min) / (max - min)) : 0f;
                    float zero = min < 0f && max > 0f ? (0f - min) / (max - min) : 0f;
                    Box(track, Rgb(0x3a3e55, enabled ? 1f : 0.5f), 2f);
                    float a = Mathf.Min(t, zero), b = Mathf.Max(t, zero);
                    if (b > a) Box(new Rect(track.x + track.width * a, track.y, track.width * (b - a), track.height), enabled ? fill : Disabled, 2f);
                    var knob = new Vector2(track.x + track.width * t, cy);
                    if (enabled)
                    {
                        if (hot) Disc(knob, 11f, AccentSoftHover);
                        Disc(knob, hover ? 8f : 7f, Color.white);
                        Ring(knob, hover ? 8f : 7f, 1f, Alpha(Bg, 0.35f));
                    }
                    else Disc(knob, 6f, Faint);
                    break;
                }
            }
            return v;
        }

        static float FromMouse(float mx, Rect track, float min, float max, float step)
        {
            float t = track.width > 0f ? Mathf.Clamp01((mx - track.x) / track.width) : 0f;
            float v = Mathf.Lerp(min, max, t);
            if (step > 0f) v = Mathf.Clamp(min + Mathf.Round((v - min) / step) * step, min, max);
            return v;
        }

        // a colour disc; the chosen one has an accent ring
        public static bool Swatch(Vector2 center, float radius, Color c, bool on)
        {
            int id = GUIUtility.GetControlID(ButtonHint, FocusType.Passive);
            Rect raw = Px(new Rect(center.x - radius - 3f, center.y - radius - 3f, radius * 2f + 6f, radius * 2f + 6f));
            int click = Click(id, raw, true, false);
            if (Event.current.type == EventType.Repaint)
            {
                bool hover = Hover(raw, id);
                if (on) Ring(center, radius + 4f, 2f, Accent);
                else if (hover) Ring(center, radius + 3f, 1.5f, Alpha(Color.white, 0.35f));
                c.a = 1f;
                Disc(center, radius, c);
                Ring(center, radius, 1f, Alpha(Color.white, 0.16f));
            }
            return click == 0;
        }

        // a tile with a vertical gradient (backdrop presets)
        public static bool GradientTile(Rect u, Color top, Color bottom, bool on)
        {
            int id = GUIUtility.GetControlID(ButtonHint, FocusType.Passive);
            Rect raw = Px(u);
            int click = Click(id, raw, true, false);
            if (Event.current.type == EventType.Repaint)
            {
                bool hover = Hover(raw, id);
                top.a = 1f;
                bottom.a = 1f;
                float r = 8f * S;
                // solid caps with the round corners, the blend in the straight part between them
                Draw2D.Box(raw, top, r);
                var bottomCap = new Rect(raw.x, raw.yMax - r, raw.width, r);
                GUI.DrawTexture(bottomCap, Draw2D.White, ScaleMode.StretchToFill, true, 0f, bottom, Vector4.zero, new Vector4(0f, 0f, r, r));
                float y0 = raw.y + r, y1 = raw.yMax - r;
                const int Strips = 8;
                float sh = (y1 - y0) / Strips;
                for (int i = 0; i < Strips && sh > 0f; i++)
                {
                    Color c = Color.Lerp(top, bottom, (i + 0.5f) / Strips);
                    GUI.DrawTexture(new Rect(raw.x, y0 + i * sh, raw.width, sh + 0.5f), Draw2D.White, ScaleMode.StretchToFill, true, 0f, c, 0f, 0f);
                }
                if (on) Frame(Inset(u, -3f), Accent, 2f, 10f);
                else if (hover) Frame(Inset(u, -2f), Alpha(Color.white, 0.35f), 1.5f, 9f);
                Frame(u, Alpha(Color.white, 0.12f), 1f, 8f);
            }
            return click == 0;
        }

        // A one-line text field with a placeholder and a clear button. focused: it has the keyboard; screenRect:
        // where it is on the screen (raw pixels), so a click elsewhere can take the keyboard away.
        public static string TextField(Rect u, string text, string placeholder, string name, out bool focused, out Rect screenRect)
        {
            if (text == null) text = "";
            Rect raw = Px(u);
            screenRect = ToScreen(raw);
            Box(u, Card, 8f);
            Frame(u, Line, 1f, 8f);
            // The clear button comes first so it gets the click before the field does. Its id is taken even while
            // it is hidden: otherwise the field's own id would move when the first letter is typed and the field
            // would lose the keyboard.
            int clearId = GUIUtility.GetControlID(ButtonHint, FocusType.Passive);
            var clearU = new Rect(u.xMax - 28f, u.y + 4f, 24f, u.height - 8f);
            Rect clearRaw = Px(clearU);
            bool clear = Click(clearId, clearRaw, text.Length > 0, false) == 0;
            if (text.Length > 0 && Event.current.type == EventType.Repaint)
            {
                bool hover = Hover(clearRaw, clearId);
                if (hover) Draw2D.Box(clearRaw, Raised, 6f * S);
                Label(clearU, "×", BodyCenter, hover ? Text : Muted);
            }
            GUI.SetNextControlName(name);
            string result = GUI.TextField(raw, text, Field);
            // (GetNameOfFocusedControl makes a string: only asked when something has the keyboard)
            focused = GUIUtility.keyboardControl != 0 && GUI.GetNameOfFocusedControl() == name;
            if (clear)
            {
                result = "";
                // a field that has the keyboard keeps its own copy of the text: let go of it so it shows ""
                if (focused) GUIUtility.keyboardControl = 0;
                focused = false;
            }
            if (Event.current.type == EventType.Repaint)
            {
                if (focused) Frame(u, Accent, 1f, 8f);
                if (result.Length == 0 && !focused) Label(new Rect(u.x + 10f, u.y, u.width - 40f, u.height), placeholder, Small, Faint);
            }
            return result;
        }

        // A thin scroll bar (u: its track) whose thumb can be dragged; returns the new scroll. Take id before the
        // scrolled content, so the content can change without moving the bar's id.
        public static float ScrollBar(int id, Rect u, float scroll, float content, float view)
        {
            float maxScroll = content - view;
            if (maxScroll <= 0f || u.height <= 0f) return 0f;
            scroll = Mathf.Clamp(scroll, 0f, maxScroll);
            float thumbH = Mathf.Min(u.height, Mathf.Max(24f, u.height * view / content));
            float travel = u.height - thumbH;
            var thumb = new Rect(u.x, u.y + (travel > 0f ? travel * scroll / maxScroll : 0f), u.width, thumbH);
            Rect hit = Px(new Rect(u.x - 5f, u.y, u.width + 10f, u.height));
            Event e = Event.current;
            switch (e.GetTypeForControl(id))
            {
                case EventType.MouseDown:
                    if (e.button == 0 && MouseIn(hit))
                    {
                        Grab(id);
                        Rect tr = Px(thumb);
                        grab = e.mousePosition.y >= tr.y && e.mousePosition.y <= tr.yMax ? e.mousePosition.y - tr.y : tr.height / 2f;
                        if (travel > 0f) scroll = Mathf.Clamp((e.mousePosition.y - grab) / S - u.y, 0f, travel) / travel * maxScroll;
                        e.Use();
                    }
                    break;
                case EventType.MouseDrag:
                    if (GUIUtility.hotControl == id)
                    {
                        if (travel > 0f) scroll = Mathf.Clamp((e.mousePosition.y - grab) / S - u.y, 0f, travel) / travel * maxScroll;
                        e.Use();
                    }
                    break;
                case EventType.MouseUp:
                    if (GUIUtility.hotControl == id)
                    {
                        Release();
                        e.Use();
                    }
                    break;
                case EventType.Repaint:
                {
                    bool hot = GUIUtility.hotControl == id;
                    Box(thumb, Alpha(Color.white, hot ? 0.4f : (Hover(hit, id) ? 0.3f : 0.16f)), u.width / 2f);
                    break;
                }
            }
            return scroll;
        }

        public static int NewScrollId()
        {
            return GUIUtility.GetControlID(ScrollHint, FocusType.Passive);
        }
    }
}
