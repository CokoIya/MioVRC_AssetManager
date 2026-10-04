// The bone handles drawn over the avatar: dots on the joints joined by lines, coloured by side and by finger. Drag a
// hand or foot to move it (two-bone IK), an elbow or knee to swing the bend, the hips to move the body (Shift turns
// it; with PinFeet the feet stay where they are), anything else to turn it; fingers turn too, slower. Shift turns a
// hand or foot instead of moving it, Alt twists a bone around itself, a double click puts the bone back as it was
// when the studio started, Escape during a drag puts the pose back as it was before the drag.
using System.Collections.Generic;
using UnityEngine;

namespace MioVRCA.Studio
{
    internal enum HandleKind
    {
        Body,     // the hips: move in the screen plane (Shift: turn the whole body)
        Rotate,   // turn the bone (trackball around the camera's axes)
        Effector, // hand or foot: move in the screen plane with IK on Upper -> Mid -> Bone
        Pole,     // elbow or knee: swing the bend, the hand or foot stays where it is
        Finger,   // a finger joint: turn, finer
        Look,     // the gaze target (not a bone)
    }

    internal sealed class BoneHandle
    {
        internal static readonly HumanBodyBones[] NoBones = new HumanBodyBones[0];

        public HumanBodyBones Bone;
        public HandleKind Kind;
        public HumanBodyBones Upper = HumanBodyBones.LastBone, Mid = HumanBodyBones.LastBone; // IK chain for Effector / Pole
        public Color Color;
        public float Radius;   // GUI pixels at UI scale 1
        public bool Detail;    // shown only with 细节 on
        public string Label;   // 中文 (through L.T when shown)

        // filled in by PoseHandles.Bind
        internal int Index;                                    // place in PoseHandles.All
        internal HumanBodyBones End = HumanBodyBones.LastBone; // the hand or foot of an Effector / Pole chain
        internal HumanBodyBones[] Changes = NoBones;           // the bones a drag on it edits (for PoseModel.Capture)
    }

    internal sealed class PoseHandles
    {
        public bool Visible = true;
        public bool Detail;            // spine segments, shoulders, upper arms / legs, toes
        public bool Fingers = true;
        public bool PinFeet = true;    // moving or turning the hips keeps the feet where they are
        public float Sensitivity = 1f; // drag speed multiplier, 0.25..3
        public float Scale = 1f;       // the UI scale, so the dots grow with it

        public readonly List<BoneHandle> All = new List<BoneHandle>();

        public BoneHandle Hover { get; private set; }
        public BoneHandle Active { get; private set; }

        public bool Dragging
        {
            get { return Active != null; }
        }

        const int Count = (int)HumanBodyBones.LastBone;
        const int None = -1;
        const float TurnSpeed = 0.4f;   // degrees per pixel at sensitivity 1
        const float FingerSpeed = 0.5f; // fingers turn at half that

        static readonly Color CentreColor = Rgb(0xeceef6), LeftColor = Rgb(0x4c8dff), RightColor = Rgb(0xff5a6a);
        static readonly Color[] FingerColors = { Rgb(0xffb547), Rgb(0x3fd0a6), Rgb(0x4cc9ff), Rgb(0x9b7bff), Rgb(0xff7ac8) };
        static readonly Color LookColor = Color.white;
        static readonly Color Outline = Rgb(0x14151f, 0.6f), PoleCore = Rgb(0x14151f, 0.75f);
        static readonly Color ActiveRing = Rgb(0xff5a6a), HoverRing = new Color(1f, 1f, 1f, 0.95f);
        static readonly Color GazeLine = new Color(1f, 1f, 1f, 0.35f);
        static readonly string[] FingerLabels = { "拇指", "食指", "中指", "无名指", "小指" }; // for Label only, Name() shows them

        // the skeleton lines (bone indices) and what Alt twists towards, made by Bind
        int[] segFrom = new int[0], segTo = new int[0];
        Color[] segColor = new Color[0];
        bool[] segFinger = new bool[0];
        int[] used = new int[0]; // bones on a line or with a handle: the ones projected
        readonly int[] parentOf = new int[Count];
        readonly int[] twistTo = new int[Count];
        readonly Color[] boneColor = new Color[Count];
        readonly List<int> build = new List<int>();
        Vector3 headUp = Vector3.up; // the avatar's up in the head's own space, at bind

        // screen positions, reused by every Draw / hit test (no allocations per OnGUI call)
        readonly Vector2[] boneGui = new Vector2[Count];
        readonly bool[] boneFront = new bool[Count];
        Vector2[] handleGui = new Vector2[0];
        bool[] handleShown = new bool[0];
        Vector2 lookGui;
        bool lookFront;

        // the drag in progress
        PoseState dragStart;
        bool edited, shiftWas;
        Matrix4x4 camToWorld, invProj; // the camera as it was when the drag began: rays and plane stay in that frame
        Rect camPixels;
        Quaternion camRot;
        Vector3 planePoint, planeNormal, grabOffset;
        Vector3 pole;      // Effector: the side the elbow / knee bends to
        Vector3 endStart;  // Pole: where the hand / foot stays
        Vector3 lookStart;
        readonly bool[] footPinned = new bool[2];
        readonly Vector3[] footPos = new Vector3[2];
        readonly Quaternion[] footRot = new Quaternion[2];
        readonly Vector3[] legPole = new Vector3[2]; // in the hips' space, so the knees follow the body

        // the status line, made again only when the handle or the language changes
        BoneHandle hintFor;
        bool hintActive;
        string hintLang, hint;

        float UiScale
        {
            get { return Scale > 0.1f ? Scale : 1f; }
        }

        // builds the handle list for the bones this avatar has
        public void Bind(AvatarRig rig)
        {
            EndDrag();
            Hover = null;
            All.Clear();
            hintFor = null;
            hint = null;
            for (int i = 0; i < Count; i++)
            {
                parentOf[i] = None;
                twistTo[i] = None;
                boneColor[i] = CentreColor;
                boneFront[i] = false;
            }
            headUp = Vector3.up;
            if (rig != null)
            {
                AddBody(rig);
                AddFingers(rig);
                Add(rig, HumanBodyBones.LastBone, HandleKind.Look, LookColor, 8f, false, "视线目标");
                Transform head = rig[HumanBodyBones.Head];
                if (head != null) headUp = head.InverseTransformDirection(rig.Root != null ? rig.Root.up : Vector3.up);
            }
            BuildSkeleton(rig);
            handleGui = new Vector2[All.Count];
            handleShown = new bool[All.Count];
        }

        void AddBody(AvatarRig rig)
        {
            bool chest = rig[HumanBodyBones.Chest] != null; // without a chest the spine is the main torso handle
            Add(rig, HumanBodyBones.Hips, HandleKind.Body, CentreColor, 9f, false, "腰");
            Add(rig, HumanBodyBones.Spine, HandleKind.Rotate, CentreColor, 7f, chest, "脊柱");
            Add(rig, HumanBodyBones.Chest, HandleKind.Rotate, CentreColor, 7f, false, "胸");
            Add(rig, HumanBodyBones.UpperChest, HandleKind.Rotate, CentreColor, 6f, true, "上胸");
            Add(rig, HumanBodyBones.Neck, HandleKind.Rotate, CentreColor, 6f, true, "脖子");
            Add(rig, HumanBodyBones.Head, HandleKind.Rotate, CentreColor, 8f, false, "头");
            for (int side = 0; side < 2; side++)
            {
                bool left = side == 0;
                Color c = left ? LeftColor : RightColor;
                HumanBodyBones upperArm = left ? HumanBodyBones.LeftUpperArm : HumanBodyBones.RightUpperArm;
                HumanBodyBones lowerArm = left ? HumanBodyBones.LeftLowerArm : HumanBodyBones.RightLowerArm;
                HumanBodyBones hand = left ? HumanBodyBones.LeftHand : HumanBodyBones.RightHand;
                HumanBodyBones upperLeg = left ? HumanBodyBones.LeftUpperLeg : HumanBodyBones.RightUpperLeg;
                HumanBodyBones lowerLeg = left ? HumanBodyBones.LeftLowerLeg : HumanBodyBones.RightLowerLeg;
                HumanBodyBones foot = left ? HumanBodyBones.LeftFoot : HumanBodyBones.RightFoot;
                Add(rig, left ? HumanBodyBones.LeftShoulder : HumanBodyBones.RightShoulder, HandleKind.Rotate, c, 6f, true, left ? "左肩" : "右肩");
                Add(rig, upperArm, HandleKind.Rotate, c, 6f, true, left ? "左上臂" : "右上臂");
                Chain(rig, Add(rig, lowerArm, HandleKind.Pole, c, 7f, false, left ? "左手肘" : "右手肘"), upperArm, lowerArm, hand);
                Chain(rig, Add(rig, hand, HandleKind.Effector, c, 9f, false, left ? "左手" : "右手"), upperArm, lowerArm, hand);
                Add(rig, upperLeg, HandleKind.Rotate, c, 6.5f, true, left ? "左大腿" : "右大腿");
                Chain(rig, Add(rig, lowerLeg, HandleKind.Pole, c, 7f, false, left ? "左膝" : "右膝"), upperLeg, lowerLeg, foot);
                Chain(rig, Add(rig, foot, HandleKind.Effector, c, 9f, false, left ? "左脚" : "右脚"), upperLeg, lowerLeg, foot);
                Add(rig, left ? HumanBodyBones.LeftToes : HumanBodyBones.RightToes, HandleKind.Rotate, c, 5.5f, true, left ? "左脚尖" : "右脚尖");
            }
        }

        void AddFingers(AvatarRig rig)
        {
            for (int i = 0; i < 30; i++)
            {
                var b = (HumanBodyBones)((int)HumanBodyBones.LeftThumbProximal + i);
                int f = i % 15 / 3;
                string label = string.Format("{0}{1}第 {2} 节", i < 15 ? "左手" : "右手", FingerLabels[f], i % 3 + 1);
                Add(rig, b, HandleKind.Finger, FingerColors[f], 4.5f, false, label);
            }
        }

        // a handle for this bone when the avatar has it, else null
        BoneHandle Add(AvatarRig rig, HumanBodyBones bone, HandleKind kind, Color color, float radius, bool detail, string label)
        {
            if (kind != HandleKind.Look && rig[bone] == null) return null;
            var h = new BoneHandle
            {
                Bone = bone, Kind = kind, Color = color, Radius = radius, Detail = detail, Label = label, Index = All.Count,
            };
            if (kind == HandleKind.Body)
            {
                // the hips, and the legs PinFeet bends
                var list = new List<HumanBodyBones>();
                foreach (HumanBodyBones b in new[]
                {
                    HumanBodyBones.Hips, HumanBodyBones.LeftUpperLeg, HumanBodyBones.LeftLowerLeg, HumanBodyBones.LeftFoot,
                    HumanBodyBones.RightUpperLeg, HumanBodyBones.RightLowerLeg, HumanBodyBones.RightFoot,
                })
                    if (rig[b] != null) list.Add(b);
                h.Changes = list.ToArray();
            }
            else if (kind != HandleKind.Look) h.Changes = new[] { bone };
            if (kind != HandleKind.Look) boneColor[(int)bone] = color;
            All.Add(h);
            return h;
        }

        // Effector / Pole: the IK chain; without all three bones the handle only turns its bone
        static void Chain(AvatarRig rig, BoneHandle h, HumanBodyBones upper, HumanBodyBones mid, HumanBodyBones end)
        {
            if (h == null) return;
            if (rig[upper] == null || rig[mid] == null || rig[end] == null)
            {
                h.Kind = HandleKind.Rotate;
                return;
            }
            h.Upper = upper;
            h.Mid = mid;
            h.End = end;
            h.Changes = new[] { upper, mid, end };
        }

        void BuildSkeleton(AvatarRig rig)
        {
            build.Clear();
            if (rig != null)
            {
                Link(rig, HumanBodyBones.Hips, HumanBodyBones.Spine, HumanBodyBones.Chest, HumanBodyBones.UpperChest, HumanBodyBones.Neck, HumanBodyBones.Head);
                HumanBodyBones armRoot = rig[HumanBodyBones.UpperChest] != null ? HumanBodyBones.UpperChest
                    : rig[HumanBodyBones.Chest] != null ? HumanBodyBones.Chest : HumanBodyBones.Spine;
                for (int side = 0; side < 2; side++)
                {
                    bool left = side == 0;
                    HumanBodyBones hand = left ? HumanBodyBones.LeftHand : HumanBodyBones.RightHand;
                    Link(rig, armRoot, left ? HumanBodyBones.LeftShoulder : HumanBodyBones.RightShoulder,
                        left ? HumanBodyBones.LeftUpperArm : HumanBodyBones.RightUpperArm,
                        left ? HumanBodyBones.LeftLowerArm : HumanBodyBones.RightLowerArm, hand);
                    Link(rig, HumanBodyBones.Hips, left ? HumanBodyBones.LeftUpperLeg : HumanBodyBones.RightUpperLeg,
                        left ? HumanBodyBones.LeftLowerLeg : HumanBodyBones.RightLowerLeg,
                        left ? HumanBodyBones.LeftFoot : HumanBodyBones.RightFoot,
                        left ? HumanBodyBones.LeftToes : HumanBodyBones.RightToes);
                    // the hand twists around the line to the middle finger, not the thumb
                    HumanBodyBones middle = left ? HumanBodyBones.LeftMiddleProximal : HumanBodyBones.RightMiddleProximal;
                    if (rig[hand] != null && rig[middle] != null) twistTo[(int)hand] = (int)middle;
                    int first = (int)(left ? HumanBodyBones.LeftThumbProximal : HumanBodyBones.RightThumbProximal);
                    for (int f = 0; f < 5; f++)
                        Link(rig, hand, (HumanBodyBones)(first + f * 3), (HumanBodyBones)(first + f * 3 + 1), (HumanBodyBones)(first + f * 3 + 2));
                }
            }

            int n = build.Count / 2;
            segFrom = new int[n];
            segTo = new int[n];
            segColor = new Color[n];
            segFinger = new bool[n];
            var seen = new bool[Count];
            for (int i = 0; i < n; i++)
            {
                segFrom[i] = build[2 * i];
                segTo[i] = build[2 * i + 1];
                Color c = boneColor[segTo[i]]; // the child's side / finger
                c.a = 0.45f;
                segColor[i] = c;
                segFinger[i] = PoseModel.IsFinger((HumanBodyBones)segTo[i]);
                seen[segFrom[i]] = true;
                seen[segTo[i]] = true;
            }
            foreach (BoneHandle h in All)
                if (h.Kind != HandleKind.Look) seen[(int)h.Bone] = true;
            var list = new List<int>();
            for (int i = 0; i < Count; i++)
                if (seen[i]) list.Add(i);
            used = list.ToArray();
            build.Clear();
        }

        // lines between the bones of a chain the avatar has, skipping the ones it lacks
        void Link(AvatarRig rig, params HumanBodyBones[] chain)
        {
            int prev = None;
            foreach (HumanBodyBones b in chain)
            {
                if (rig[b] == null) continue;
                int i = (int)b;
                if (prev != None && prev != i)
                {
                    build.Add(prev);
                    build.Add(i);
                    if (parentOf[i] == None) parentOf[i] = prev;
                    if (twistTo[prev] == None) twistTo[prev] = i;
                }
                prev = i;
            }
        }

        // Draws lines and dots (EventType.Repaint only, raw GUI pixels, GUI.matrix = identity). Handles behind the
        // camera are skipped; the hovered and the dragged one are highlighted. The look target is drawn when
        // look.Enabled && !look.FollowCamera.
        public void Draw(Camera cam, AvatarRig rig, LookAt look)
        {
            Event e = Event.current;
            if (e == null || e.type != EventType.Repaint) return;
            if (!Visible || cam == null || rig == null || All.Count == 0) return;
            Project(cam, rig, look);
            float s = UiScale;
            Matrix4x4 keep = GUI.matrix;
            GUI.matrix = Matrix4x4.identity;
            try
            {
                DrawLines(look, s);
                // list order: body first, fingers last (they stay visible and clickable); hovered / dragged on top
                for (int k = 0; k < All.Count; k++)
                {
                    BoneHandle h = All[k];
                    if (!handleShown[k] || h == Hover || h == Active) continue;
                    DrawDot(h, handleGui[k], s, false, false);
                }
                if (Hover != Active) DrawOnTop(Hover, s, true, false);
                DrawOnTop(Active, s, false, true);
            }
            finally
            {
                GUI.matrix = keep;
            }
        }

        void DrawLines(LookAt look, float s)
        {
            float w = 2f * s;
            Draw2D.BeginLines();
            try
            {
                for (int i = 0; i < segFrom.Length; i++)
                {
                    if (segFinger[i] && !Fingers) continue;
                    int a = segFrom[i], b = segTo[i];
                    if (!boneFront[a] || !boneFront[b]) continue;
                    Draw2D.Line(boneGui[a], boneGui[b], w, segColor[i]);
                }
                // the gaze: from the head to its target
                int head = (int)HumanBodyBones.Head;
                if (LookShown(look) && lookFront && boneFront[head]) Draw2D.Line(boneGui[head], lookGui, 1.5f * s, GazeLine);
            }
            finally
            {
                Draw2D.EndLines();
            }
        }

        void DrawOnTop(BoneHandle h, float s, bool hovered, bool active)
        {
            if (h == null) return;
            int k = h.Index;
            if (k < 0 || k >= All.Count || All[k] != h || !handleShown[k]) return;
            DrawDot(h, handleGui[k], s, hovered, active);
        }

        static void DrawDot(BoneHandle h, Vector2 p, float s, bool hovered, bool active)
        {
            float r = h.Radius * s * (hovered || active ? 1.25f : 1f);
            float ow = 1.5f * s;
            Color fill = h.Color;
            fill.a = 0.85f;
            float outer = r + ow;
            Draw2D.Ring(p, r + ow, ow, Outline); // dark edge: readable on light and dark backgrounds
            if (h.Kind == HandleKind.Look)
            {
                // a ring with a dot: a point in space, not a joint
                float rw = 2.5f * s;
                Draw2D.Ring(p, r, rw, fill);
                Draw2D.Ring(p, r - rw, ow, Outline);
                Draw2D.Disc(p, 1.8f * s, fill);
            }
            else
            {
                Draw2D.Disc(p, r, fill);
                if (h.Kind == HandleKind.Pole)
                {
                    Draw2D.Disc(p, r * 0.4f, PoleCore); // hollow middle: it swings the bend, it does not turn
                }
                else if (h.Kind == HandleKind.Effector)
                {
                    Draw2D.Ring(p, r + ow + 3f * s, 1.5f * s, fill); // a second ring: it moves, IK follows
                    outer = r + ow + 3f * s;
                }
            }
            if (active) Draw2D.Ring(p, outer + 3f * s, 2.5f * s, ActiveRing);
            else if (hovered) Draw2D.Ring(p, outer + 2.5f * s, 2f * s, HoverRing);
        }

        // Mouse handling for the handles, with e.mousePosition in raw GUI pixels. Returns true when it used the event
        // (then the caller does not orbit the camera). Left button only; MouseDown on a handle starts a drag
        // (model.BeginEdit()), MouseDrag edits the bones and then model.Capture(...) the changed bones, MouseUp ends.
        // Double click resets. Escape while dragging puts the pose back as it was when the drag began.
        public bool HandleEvent(Event e, Camera cam, PoseModel model, LookAt look)
        {
            if (e == null) return false;
            AvatarRig rig = model != null ? model.Rig : null;
            bool ready = cam != null && rig != null && rig.Alive;
            Vector2 mouse, delta;
            switch (e.type)
            {
                case EventType.MouseDown:
                {
                    if (e.button != 0) return false;
                    if (Active != null) EndDrag(); // the last drag's MouseUp went somewhere else
                    if (!ready || !Visible) return false;
                    RawMouse(e, out mouse, out delta);
                    BoneHandle h = HitTest(mouse, cam, rig, look);
                    if (h == null) return false;
                    if (e.clickCount == 2)
                    {
                        ResetHandle(h, model, look);
                        Hover = h;
                    }
                    else BeginDrag(h, e, mouse, cam, model, look);
                    e.Use();
                    return true;
                }
                case EventType.MouseDrag:
                {
                    if (Active == null) return false;
                    if (!ready || (Active.Kind == HandleKind.Look && look == null))
                        EndDrag(); // the avatar went away: stop, but do not hand the gesture to the camera
                    else
                    {
                        RawMouse(e, out mouse, out delta);
                        DragTo(e, mouse, delta, model, look);
                    }
                    e.Use();
                    return true;
                }
                case EventType.MouseUp:
                {
                    if (Active == null || e.button != 0) return false;
                    EndDrag();
                    e.Use();
                    return true;
                }
                case EventType.KeyDown:
                {
                    if (Active == null || e.keyCode != KeyCode.Escape) return false;
                    CancelDrag(model, look);
                    e.Use();
                    return true;
                }
            }
            return false;
        }

        // updates Hover for a mouse position (raw GUI pixels); null when nothing is near
        public void UpdateHover(Vector2 mouse, Camera cam, AvatarRig rig, LookAt look)
        {
            if (!Visible)
            {
                EndDrag(); // hidden mid-drag (H): nothing would ever end it
                Hover = null;
                return;
            }
            // a MouseMove comes only with no button held, so a drag still running lost its MouseUp
            Event e = Event.current;
            if (Active != null && e != null && e.type == EventType.MouseMove) EndDrag();
            Hover = Active != null ? null : HitTest(mouse, cam, rig, look);
        }

        // a line for the status bar about the hovered / dragged handle, or null
        public string Hint()
        {
            BoneHandle h = Active != null ? Active : Hover;
            if (h == null) return null;
            bool act = h == Active;
            if (h == hintFor && act == hintActive && hintLang == L.Lang) return hint;
            string name = Name(h), text;
            switch (h.Kind)
            {
                case HandleKind.Body: text = L.F("{0} · 拖动：移动身体 · Shift：转身 · 双击：复原", name); break;
                case HandleKind.Effector: text = L.F("{0} · 拖动：移动（IK） · Shift：旋转 · 双击：复原", name); break;
                case HandleKind.Pole: text = L.F("{0} · 拖动：改变弯曲方向 · 双击：复原", name); break;
                case HandleKind.Finger: text = L.F("{0} · 拖动：弯曲 · Alt：扭转 · 双击：复原", name); break;
                case HandleKind.Look: text = L.F("{0} · 拖动：移动视线目标 · 双击：回到正前方", name); break;
                default: text = L.F("{0} · 拖动：旋转 · Alt：扭转 · 双击：复原", name); break;
            }
            if (act) text = L.F("{0} · Esc：取消", text);
            hintFor = h;
            hintActive = act;
            hintLang = L.Lang;
            hint = text;
            return text;
        }

        // the handle's name in the UI language
        static string Name(BoneHandle h)
        {
            if (h.Kind == HandleKind.Look) return L.T("视线目标");
            if (PoseModel.IsFinger(h.Bone))
            {
                int i = (int)h.Bone - (int)HumanBodyBones.LeftThumbProximal;
                string side = i < 15 ? L.T("左手") : L.T("右手"), finger;
                switch (i % 15 / 3)
                {
                    case 0: finger = L.T("拇指"); break;
                    case 1: finger = L.T("食指"); break;
                    case 2: finger = L.T("中指"); break;
                    case 3: finger = L.T("无名指"); break;
                    default: finger = L.T("小指"); break;
                }
                return L.F("{0}{1}第 {2} 节", side, finger, i % 3 + 1);
            }
            switch (h.Bone)
            {
                case HumanBodyBones.Hips: return L.T("腰");
                case HumanBodyBones.Spine: return L.T("脊柱");
                case HumanBodyBones.Chest: return L.T("胸");
                case HumanBodyBones.UpperChest: return L.T("上胸");
                case HumanBodyBones.Neck: return L.T("脖子");
                case HumanBodyBones.Head: return L.T("头");
                case HumanBodyBones.LeftShoulder: return L.T("左肩");
                case HumanBodyBones.RightShoulder: return L.T("右肩");
                case HumanBodyBones.LeftUpperArm: return L.T("左上臂");
                case HumanBodyBones.RightUpperArm: return L.T("右上臂");
                case HumanBodyBones.LeftLowerArm: return L.T("左手肘");
                case HumanBodyBones.RightLowerArm: return L.T("右手肘");
                case HumanBodyBones.LeftHand: return L.T("左手");
                case HumanBodyBones.RightHand: return L.T("右手");
                case HumanBodyBones.LeftUpperLeg: return L.T("左大腿");
                case HumanBodyBones.RightUpperLeg: return L.T("右大腿");
                case HumanBodyBones.LeftLowerLeg: return L.T("左膝");
                case HumanBodyBones.RightLowerLeg: return L.T("右膝");
                case HumanBodyBones.LeftFoot: return L.T("左脚");
                case HumanBodyBones.RightFoot: return L.T("右脚");
                case HumanBodyBones.LeftToes: return L.T("左脚尖");
                case HumanBodyBones.RightToes: return L.T("右脚尖");
                default: return h.Label;
            }
        }

        // ---- projection and hit test ----

        bool Shows(BoneHandle h)
        {
            if (h.Kind == HandleKind.Finger) return Fingers;
            return !h.Detail || Detail;
        }

        static bool LookShown(LookAt look)
        {
            return look != null && look.Enabled && !look.FollowCamera;
        }

        // screen positions of the bones and the handles (raw GUI pixels, y down) and which handles are shown now
        void Project(Camera cam, AvatarRig rig, LookAt look)
        {
            if (handleGui.Length != All.Count) // All was changed from outside Bind
            {
                handleGui = new Vector2[All.Count];
                handleShown = new bool[All.Count];
                for (int k = 0; k < All.Count; k++) All[k].Index = k;
            }
            float sh = Screen.height;
            for (int u = 0; u < used.Length; u++)
            {
                int i = used[u];
                Transform t = rig.Bones[i];
                if (t == null)
                {
                    boneFront[i] = false;
                    continue;
                }
                Vector3 sp = cam.WorldToScreenPoint(t.position);
                boneFront[i] = sp.z > 0f;
                boneGui[i] = new Vector2(sp.x, sh - sp.y);
            }
            bool lookShown = LookShown(look);
            lookFront = false;
            if (lookShown)
            {
                Vector3 sp = cam.WorldToScreenPoint(look.Target);
                lookFront = sp.z > 0f;
                lookGui = new Vector2(sp.x, sh - sp.y);
            }
            for (int k = 0; k < All.Count; k++)
            {
                BoneHandle h = All[k];
                if (h.Kind == HandleKind.Look)
                {
                    handleShown[k] = lookShown && lookFront;
                    handleGui[k] = lookGui;
                    continue;
                }
                int i = (int)h.Bone;
                bool ok = i >= 0 && i < Count && boneFront[i];
                handleShown[k] = ok && Shows(h);
                handleGui[k] = ok ? boneGui[i] : Vector2.zero;
            }
        }

        // the shown handle the mouse is on: the smallest distance relative to its reach wins
        BoneHandle HitTest(Vector2 mouse, Camera cam, AvatarRig rig, LookAt look)
        {
            if (!Visible || cam == null || rig == null || All.Count == 0) return null;
            Project(cam, rig, look);
            float s = UiScale;
            BoneHandle best = null;
            float bestScore = float.MaxValue;
            for (int k = 0; k < All.Count; k++)
            {
                if (!handleShown[k]) continue;
                BoneHandle h = All[k];
                float reach = h.Radius * s + 6f * s;
                float d = Vector2.Distance(mouse, handleGui[k]);
                if (d > reach) continue;
                float score = d / reach;
                if (score < bestScore)
                {
                    bestScore = score;
                    best = h;
                }
            }
            return best;
        }

        // the mouse in raw GUI pixels whatever GUI.matrix is now (the studio calls with identity already)
        static void RawMouse(Event e, out Vector2 mouse, out Vector2 delta)
        {
            Matrix4x4 keep = GUI.matrix;
            bool scaled = !keep.isIdentity;
            if (scaled) GUI.matrix = Matrix4x4.identity;
            mouse = e.mousePosition;
            delta = e.delta;
            if (scaled) GUI.matrix = keep;
        }

        // ---- dragging ----

        void BeginDrag(BoneHandle h, Event e, Vector2 mouse, Camera cam, PoseModel model, LookAt look)
        {
            AvatarRig rig = model.Rig;
            Active = h;
            Hover = null;
            edited = false;
            dragStart = h.Kind == HandleKind.Look ? null : model.Snapshot();
            lookStart = look != null ? look.Target : Vector3.zero;
            // the camera now: when it follows the hips, moving them must not move the plane under the mouse
            camToWorld = cam.cameraToWorldMatrix;
            invProj = cam.projectionMatrix.inverse;
            camPixels = cam.pixelRect;
            camRot = cam.transform.rotation;
            planeNormal = -cam.transform.forward;
            shiftWas = e.shift;
            Grab(mouse, rig, look);

            if (h.Kind == HandleKind.Body) PinFeetHere(rig); // the feet stay where they are now for the whole drag
            else if (h.Kind == HandleKind.Effector)
            {
                Transform up = rig[h.Upper], mid = rig[h.Mid], end = rig[h.Bone];
                bool arm = h.Bone == HumanBodyBones.LeftHand || h.Bone == HumanBodyBones.RightHand;
                Vector3 fallback = arm ? -rig.Forward * 0.6f + Vector3.down * 0.4f : rig.Forward; // elbows back, knees forward
                pole = up != null && mid != null && end != null ? Ik.PolePoint(up, mid, end, fallback) : planePoint;
            }
            else if (h.Kind == HandleKind.Pole)
            {
                Transform end = rig[h.End];
                endStart = end != null ? end.position : planePoint;
            }
        }

        // the drag plane through the handle as it is now, and where on it the mouse grabbed
        void Grab(Vector2 mouse, AvatarRig rig, LookAt look)
        {
            Vector3 p = Vector3.zero;
            if (Active != null && Active.Kind == HandleKind.Look)
            {
                if (look != null) p = look.Target;
            }
            else if (Active != null && rig != null)
            {
                Transform t = rig[Active.Bone];
                if (t != null) p = t.position;
            }
            planePoint = p;
            Vector3 m;
            grabOffset = MousePoint(mouse, out m) ? p - m : Vector3.zero;
        }

        // where the mouse ray (through the camera as it was at the drag start) meets the drag plane
        bool MousePoint(Vector2 mouse, out Vector3 point)
        {
            point = planePoint;
            if (camPixels.width < 1f || camPixels.height < 1f) return false;
            float nx = (mouse.x - camPixels.x) / camPixels.width * 2f - 1f;
            float ny = (Screen.height - mouse.y - camPixels.y) / camPixels.height * 2f - 1f;
            Vector3 a = invProj.MultiplyPoint(new Vector3(nx, ny, -1f)), b = invProj.MultiplyPoint(new Vector3(nx, ny, 0f));
            Vector3 origin = camToWorld.MultiplyPoint3x4(a), dir = camToWorld.MultiplyVector(b - a);
            float len = dir.magnitude, denom = Vector3.Dot(dir, planeNormal);
            if (len < 1e-12f || Mathf.Abs(denom) < len * 1e-4f) return false;
            point = origin + dir * (Vector3.Dot(planePoint - origin, planeNormal) / denom);
            return true;
        }

        void DragTo(Event e, Vector2 mouse, Vector2 delta, PoseModel model, LookAt look)
        {
            BoneHandle h = Active;
            AvatarRig rig = model.Rig;
            Vector3 p;
            if (h.Kind == HandleKind.Look)
            {
                if (MousePoint(mouse, out p)) look.Target = p + grabOffset;
                return;
            }
            if (!edited)
            {
                model.BeginEdit(); // only once something moves: a plain click leaves no undo step
                edited = true;
            }
            model.Apply(); // the bones as the pose has them, without the gaze LookAt adds every frame
            if (e.shift != shiftWas)
            {
                // moving <-> turning: carry on from where the handle is now, no jump
                shiftWas = e.shift;
                Grab(mouse, rig, look);
            }
            float k = TurnSpeed * Mathf.Clamp(Sensitivity, 0.05f, 10f);
            switch (h.Kind)
            {
                case HandleKind.Body:
                    MoveBody(rig, e, mouse, delta, k);
                    break;
                case HandleKind.Rotate:
                    Turn(rig, h.Bone, delta, e.alt, k);
                    break;
                case HandleKind.Finger:
                    Turn(rig, h.Bone, delta, e.alt, k * FingerSpeed);
                    break;
                case HandleKind.Effector:
                    if (e.shift) Turn(rig, h.Bone, delta, e.alt, k);
                    else if (MousePoint(mouse, out p)) Ik.TwoBone(rig[h.Upper], rig[h.Mid], rig[h.Bone], p + grabOffset, pole, true);
                    break;
                case HandleKind.Pole:
                    if (MousePoint(mouse, out p)) Ik.TwoBone(rig[h.Upper], rig[h.Mid], rig[h.End], endStart, p + grabOffset, true);
                    break;
            }
            if (h.Changes.Length > 0) model.Capture(h.Changes);
        }

        void MoveBody(AvatarRig rig, Event e, Vector2 mouse, Vector2 delta, float k)
        {
            Transform hips = rig[HumanBodyBones.Hips];
            if (hips == null) return;
            Vector3 p;
            if (e.shift) Turn(rig, HumanBodyBones.Hips, delta, e.alt, k);
            else if (MousePoint(mouse, out p)) hips.position = p + grabOffset;
            if (PinFeet) SolveLegs(rig);
        }

        // PinFeet: keeps where the feet are now, and the side each knee bends to (in the hips' space, so the knees
        // turn with the body)
        void PinFeetHere(AvatarRig rig)
        {
            Transform hips = rig[HumanBodyBones.Hips];
            for (int side = 0; side < 2; side++)
            {
                Transform ul = rig[side == 0 ? HumanBodyBones.LeftUpperLeg : HumanBodyBones.RightUpperLeg];
                Transform ll = rig[side == 0 ? HumanBodyBones.LeftLowerLeg : HumanBodyBones.RightLowerLeg];
                Transform ft = rig[side == 0 ? HumanBodyBones.LeftFoot : HumanBodyBones.RightFoot];
                footPinned[side] = hips != null && ul != null && ll != null && ft != null;
                if (!footPinned[side]) continue;
                footPos[side] = ft.position;
                footRot[side] = ft.rotation;
                legPole[side] = hips.InverseTransformPoint(Ik.PolePoint(ul, ll, ft, rig.Forward));
            }
        }

        // PinFeet: the legs bent again so the feet are back where PinFeetHere found them, after the hips moved
        void SolveLegs(AvatarRig rig)
        {
            Transform hips = rig[HumanBodyBones.Hips];
            if (hips == null) return;
            for (int side = 0; side < 2; side++)
            {
                if (!footPinned[side]) continue;
                Transform ft = rig[side == 0 ? HumanBodyBones.LeftFoot : HumanBodyBones.RightFoot];
                if (ft == null) continue;
                Ik.TwoBone(rig[side == 0 ? HumanBodyBones.LeftUpperLeg : HumanBodyBones.RightUpperLeg],
                    rig[side == 0 ? HumanBodyBones.LeftLowerLeg : HumanBodyBones.RightLowerLeg], ft, footPos[side],
                    hips.TransformPoint(legPole[side]), true);
                ft.rotation = footRot[side];
            }
        }

        // Turns a bone by this frame's mouse movement: a trackball where the side facing the camera follows the
        // mouse, or with Alt a twist around the bone's own axis (grabbing the side facing the camera again).
        void Turn(AvatarRig rig, HumanBodyBones b, Vector2 d, bool alt, float k)
        {
            Transform t = rig[b];
            if (t == null) return;
            Vector3 up = camRot * Vector3.up, right = camRot * Vector3.right, toCam = camRot * Vector3.back;
            Quaternion q;
            if (alt)
            {
                Vector3 axis = TwistAxis(rig, b, t);
                if (axis.sqrMagnitude < 1e-12f) return;
                axis.Normalize();
                // how the near side moves on screen for a positive turn; a bone pointing at the camera: its top
                Vector3 m = Vector3.Cross(axis, toCam);
                var ms = new Vector2(Vector3.Dot(m, right), -Vector3.Dot(m, up));
                if (ms.sqrMagnitude < 0.09f)
                {
                    m = Vector3.Cross(axis, up);
                    ms = new Vector2(Vector3.Dot(m, right), -Vector3.Dot(m, up));
                }
                if (ms.sqrMagnitude < 1e-6f) return;
                q = Quaternion.AngleAxis(Vector2.Dot(d, ms.normalized) * k, axis);
            }
            else q = Quaternion.AngleAxis(-d.x * k, up) * Quaternion.AngleAxis(-d.y * k, right);
            t.rotation = q * t.rotation;
        }

        // the bone's own axis: towards its next bone (hand: middle finger), the head's up, else on from its parent
        Vector3 TwistAxis(AvatarRig rig, HumanBodyBones b, Transform t)
        {
            int i = (int)b;
            Transform next = twistTo[i] != None ? rig.Bones[twistTo[i]] : null;
            if (next != null) return next.position - t.position;
            if (b == HumanBodyBones.Head) return t.TransformDirection(headUp);
            Transform parent = parentOf[i] != None ? rig.Bones[parentOf[i]] : null;
            if (parent != null) return t.position - parent.position;
            return t.up;
        }

        // Escape: the pose (or the gaze target) as it was when the drag began
        void CancelDrag(PoseModel model, LookAt look)
        {
            if (Active != null)
            {
                if (Active.Kind == HandleKind.Look)
                {
                    if (look != null) look.Target = lookStart;
                }
                else if (edited && model != null)
                {
                    // the drag's undo step goes again and the redo steps it dropped come back; should the history
                    // have changed meanwhile, only the pose is put back
                    if (!model.CancelEdit() && dragStart != null) model.Restore(dragStart);
                }
            }
            EndDrag();
        }

        void EndDrag()
        {
            Active = null;
            edited = false;
            dragStart = null;
        }

        // Double click: the bone (hand / foot, elbow / knee: the whole limb) back to the pose at bind. The hips bring
        // the legs along, which hang from them: with PinFeet the legs are bent again so the feet stay where they are
        // (as a drag of the hips would), else the legs go back to the pose at bind too.
        void ResetHandle(BoneHandle h, PoseModel model, LookAt look)
        {
            if (h.Kind == HandleKind.Look)
            {
                if (look != null && model.Rig != null) look.Target = look.DefaultTarget(model.Rig);
                return;
            }
            model.BeginEdit();
            if (h.Kind == HandleKind.Body && PinFeet && model.Rig != null)
            {
                model.Apply(); // the bones as the pose has them, without the gaze, before the feet are measured
                PinFeetHere(model.Rig);
                model.ResetBone(HumanBodyBones.Hips);
                SolveLegs(model.Rig);
                model.Capture(h.Changes);
            }
            else
                foreach (HumanBodyBones b in h.Changes)
                    model.ResetBone(b);
        }

        static Color Rgb(int hex, float a = 1f)
        {
            return new Color(((hex >> 16) & 255) / 255f, ((hex >> 8) & 255) / 255f, (hex & 255) / 255f, a);
        }
    }
}
