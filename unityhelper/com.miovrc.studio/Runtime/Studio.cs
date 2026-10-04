// The studio itself: one hidden, unsaved object with this component, made when the studio opens in Play mode. It
// owns the parts (avatar, pose, handles, gaze, camera, lights, backdrop, capture, face, UI), runs them in order every
// frame, and routes the mouse and keyboard: UI first, then the bone handles, then the camera (or the light).
// It cleans up when it is closed, when it is destroyed, and when Play mode ends (OnApplicationQuit: hidden unsaved
// objects outlive Play mode, so nothing may be left to a delayed Destroy). Before scripts are reloaded in Play mode
// the launcher closes it at once (CloseNow) and opens it again afterwards.
// While it is open it keeps UserSettings/MioVRCA/studio/running fresh (StudioHost.RunningFile), so MioVRCA does not
// remove the package under it.
using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using UnityEngine;

namespace MioVRCA.Studio
{
    [DefaultExecutionOrder(-5000)]
    [AddComponentMenu("")]
    public sealed class Studio : MonoBehaviour
    {
        public static Studio Current { get; private set; }

        // Opens the studio (in Play mode only); when it is already open, only switches the language.
        // lang: "zh", "en", "ja", or "" for the last one used.
        public static Studio Open(string lang)
        {
            if (lang == null) lang = "";
            if (!Application.isPlaying)
            {
                Debug.LogWarning("[MioVRCA] 摄影棚只能在 Play 模式中打开");
                return null;
            }
            if (Current != null)
            {
                if (lang != "")
                {
                    L.Use(lang);
                    Current.PrefsChanged();
                }
                return Current;
            }
            // what a studio lost to a script reload (or a failed close) left behind would fight the new one: a camera
            // drawing over it, a light lighting the avatar twice
            SweepLeftovers(null);
            return Create(lang);
        }

        static Studio Create(string lang)
        {
            openLang = lang; // Awake runs inside AddComponent
            try
            {
                var go = new GameObject(LeftoverPrefix) { hideFlags = HideFlags.HideAndDontSave };
                return go.AddComponent<Studio>();
            }
            finally
            {
                openLang = "";
            }
        }

        // Closes the studio and puts the scene back (lights, cameras, hidden renderers, Animator, ambient).
        // exitPlayMode: also leave Play mode (through StudioHost.ExitPlayMode).
        public void Close(bool exitPlayMode)
        {
            Cleanup(false, false);
            if (exitPlayMode)
            {
                Action exit = StudioHost.ExitPlayMode;
                if (exit != null) exit();
            }
        }

        // Closes the studio and destroys everything it made right now, not at the end of the frame: before scripts
        // are reloaded (a delayed Destroy would not run before the reload, and the reloaded studio could not reach
        // what is left).
        public void CloseNow()
        {
            Cleanup(true, false);
        }

        // the language the studio shows: "zh", "en" or "ja"
        public string Language
        {
            get { return L.Lang; }
        }

        // Everything below is runtime-only. [NonSerialized]: a script reload in Play mode keeps the private fields of
        // a MonoBehaviour that it can serialise, and a studio coming back with ready = true but none of its parts
        // would run on in a broken state instead of starting over (Recover).
        [NonSerialized] internal StudioPrefs Prefs;
        [NonSerialized] internal AvatarRig Rig;              // null until an avatar is bound
        [NonSerialized] internal PoseModel Pose;
        [NonSerialized] internal PoseSlots Slots;
        [NonSerialized] internal PoseHandles Handles = new PoseHandles();
        [NonSerialized] internal LookAt Look = new LookAt();
        [NonSerialized] internal StudioCamera View = new StudioCamera();
        [NonSerialized] internal StudioLights Lights = new StudioLights();
        [NonSerialized] internal Backdrop Back = new Backdrop();
        [NonSerialized] internal PhotoCapture Photo = new PhotoCapture();
        [NonSerialized] internal FaceControl Face = new FaceControl();
        [NonSerialized] internal StudioUi Ui = new StudioUi();

        [NonSerialized] internal List<Transform> Candidates = new List<Transform>(); // avatars to choose from
        [NonSerialized] internal string Problem;     // why there is no avatar (中文 through L.T), shown by the UI with a retry button
        [NonSerialized] internal bool UiHidden;      // Tab
        [NonSerialized] internal HandSide HandSide = HandSide.Both;
        [NonSerialized] internal float HandStrength = 1f;
        [NonSerialized] internal string ToastText;
        [NonSerialized] internal float ToastLeft;

        // still looking for an avatar (Problem stays empty meanwhile)
        internal bool Searching
        {
            get { return searching; }
        }

        enum DragKind { None, Handle, Orbit, Pan, Light }

        const float SearchEvery = 0.5f, SearchFor = 10f; // NDMF / VRCFury may still be building the avatar
        const float PrefsDelay = 1f;
        const float BeatEvery = 5f;  // the running file is touched this often (seconds)
        const int StaleFrames = 3;   // frames a drag's button must read as up before the drag is taken as lost
        const int AnimatorFightsMax = 10;
        const string LeftoverPrefix = "MioVRCA Studio"; // the name of everything the studio makes (AvatarRig skips it)
        static readonly Vector2 Nowhere = new Vector2(-100000f, -100000f);

        static string openLang = "";
        static bool noLegacyInput;   // the project uses only the new Input System: the Input class throws
        static bool beatWarned;

        // ready: Awake finished (false after a script reload in Play mode that the launcher could not close it before:
        // the component is kept, none of what it held). closed: cleaned up, nothing may run any more.
        [NonSerialized] bool ready, closed;
        [NonSerialized] bool searching;
        [NonSerialized] float nextSearch, searchUntil;
        [NonSerialized] bool prefsDirty;
        [NonSerialized] float prefsDue;
        [NonSerialized] float nextBeat;
        [NonSerialized] DragKind drag;
        [NonSerialized] int dragButton;
        [NonSerialized] Vector2 lastMouse;
        [NonSerialized] int staleFrames;  // Update: frames in a row the drag's button (or any, for the UI) read as up
        [NonSerialized] bool releaseUi;   // the next OnGUI lets go of the mouse a UI control still holds (UiKit.Claimed)
        [NonSerialized] int heldKeys;     // Space / Tab / F / H act once per press, not on key repeat (one bit each, KeyBit)
        [NonSerialized] int failed;       // parts whose exception LateUpdate already logged (one bit each): no flood every frame
        [NonSerialized] int animatorFights;

        // Binds this avatar: pose model, slots, handles, gaze, face, and frames it. The avatar in hand is given back
        // only once the new one works; when it does not, the current one stays and the reason is shown as a toast
        // (as the problem card only when there is no avatar at all). True when the avatar is bound (or already was).
        internal bool BindAvatar(Transform root)
        {
            if (root != null && Rig != null && Rig.Alive && Rig.Root == root) return true; // already this one
            string err;
            if (TryBind(root, out err)) return true;
            if (HasAvatar())
            {
                Toast(err, 4f);
                return false;
            }
            Problem = err;
            searching = false;
            return false;
        }

        // looks for avatars again and binds the first one (the retry button, and when the avatar disappears)
        internal void FindAvatar()
        {
            searching = true;
            searchUntil = Time.unscaledTime + SearchFor;
            Problem = null;
            Search();
        }

        internal void Toast(string text, float seconds)
        {
            ToastText = text;
            ToastLeft = seconds;
        }

        // the shutter button / Space
        internal void RequestShot()
        {
            if (closed || !ready) return;
            Photo.Request();
        }

        // settings changed: saved a moment later (not on every slider step)
        internal void PrefsChanged()
        {
            prefsDirty = true;
            prefsDue = Time.unscaledTime + PrefsDelay;
        }

        // the output frame in raw GUI pixels for the current view size
        internal Rect FrameRect()
        {
            return StudioCamera.FrameRect(Photo.Aspect, new Vector2(Screen.width, Screen.height));
        }

        // ---- life ----

        void Awake()
        {
            if (Current != null && Current != this)
            {
                // only Open makes the studio; a second one would fight over the camera
                closed = true;
                Destroy(this);
                return;
            }
            Current = this;
            Prefs = StudioPrefs.Load();
            L.Use(openLang != "" ? openLang : Prefs.lang);
            if (openLang != "" && openLang != Prefs.lang) PrefsChanged();
            Safe(() => Prefs.ApplyTo(this));
            Safe(View.Create);
            Safe(Lights.Create);
            Safe(() => Back.Create(View));
            ready = true;
            // Ctrl+Z / Ctrl+Y would otherwise run Unity's own Edit/Undo and Edit/Redo instead of reaching the studio
            Action<bool> capture = StudioHost.CaptureShortcuts;
            if (capture != null) Safe(() => capture(true));
            Beat();
            FindAvatar();
        }

        void Update()
        {
            if (closed) return;
            if (!ready)
            {
                Recover();
                return;
            }
            float now = Time.unscaledTime;
            if (ToastLeft > 0f) ToastLeft = Mathf.Max(0f, ToastLeft - Time.unscaledDeltaTime);
            if (prefsDirty && now >= prefsDue)
            {
                prefsDirty = false;
                SavePrefs();
            }
            if (now >= nextBeat) Beat();
            CheckLostMouseUp();

            if (Rig != null)
            {
                if (!Rig.Alive || !Rig.Root.gameObject.activeInHierarchy)
                {
                    // destroyed or replaced (NDMF / VRCFury rebuild avatars, a scene was loaded): look again
                    ReleaseRig();
                    FindAvatar();
                }
                else KeepAnimatorOff();
            }
            else if (searching && now >= nextSearch) Search();
        }

        void LateUpdate()
        {
            if (closed || !ready) return;
            AvatarRig rig = HasAvatar() ? Rig : null;
            if (rig != null)
            {
                try
                {
                    Pose.Apply();
                    // the camera position of the last frame: close enough for the gaze
                    if (View.Cam != null) Look.Apply(rig, View.Cam.transform.position);
                }
                catch (Exception e) { Report(0, e); }
            }
            // A handle drag moves the bones along rays from the camera as it was at the press: a view following the
            // hips (or the chest / head) would slide along with them, and the dragged handle would run away from the
            // cursor. The view holds still during the drag; StudioCamera takes the follow offset again once Follow is
            // back on, so it does not jump afterwards either.
            bool follow = View.Follow;
            if (drag == DragKind.Handle && Handles.Dragging) View.Follow = false;
            try { View.Apply(rig); }
            catch (Exception e) { Report(1, e); }
            finally { View.Follow = follow; }
            try { Lights.Apply(View, rig); } catch (Exception e) { Report(2, e); }
            try { Back.Apply(View, rig); } catch (Exception e) { Report(3, e); }
            bool due = false;
            try { due = Photo.Tick(Time.unscaledDeltaTime); } catch (Exception e) { Report(4, e); }
            if (due) Shoot();
        }

        void OnGUI()
        {
            if (closed || !ready) return;
            Event e = Event.current;
            GUI.matrix = Matrix4x4.identity; // mousePosition in raw GUI pixels
            Handles.Scale = Ui.Scale;
            if (releaseUi)
            {
                // a UI control lost its MouseUp (focus went elsewhere, see CheckLostMouseUp): it lets go of the mouse
                releaseUi = false;
                if (UiKit.Claimed != 0 && GUIUtility.hotControl == UiKit.Claimed) GUIUtility.hotControl = 0;
                UiKit.Claimed = 0;
            }

            // (1) the keyboard, before the UI so the shortcuts win (a focused text field keeps its keys)
            if (e.type == EventType.KeyDown)
            {
                Keys(e);
                // Tab also comes as a character ('\t'; Shift+Tab: 25), mostly as an event of its own (keyCode None).
                // Left unused, IMGUI moves the keyboard focus with it, into the expression filter (the only keyboard
                // control), which would then swallow Space and the other shortcuts. The studio never wants that; the
                // UI still sees the key half (rawType) to leave the filter.
                if ((e.character == '\t' || e.character == (char)25) && e.type == EventType.KeyDown) e.Use();
            }
            else if (e.type == EventType.KeyUp) heldKeys &= ~KeyBit(e.keyCode);

            // (2) the darkened view and the output frame, (3) the bone handles
            if (e.type == EventType.Repaint)
            {
                Ui.DrawUnder(this);
                if (HasAvatar() && Handles.Visible && !UiHidden && View.Cam != null)
                {
                    GUI.matrix = Matrix4x4.identity;
                    Handles.Draw(View.Cam, Rig, Look);
                }
            }

            // (4) the panels; 「退出摄影棚」 closes the studio from in here
            if (!UiHidden) Ui.DrawPanels(this);
            if (closed) return;

            // (5) the viewport: handles, camera, light
            GUI.matrix = Matrix4x4.identity;
            if (e.type != EventType.Used) Mouse(e);

            // (6) countdown, flash, toast, status line
            Ui.DrawOver(this);
        }

        // Play mode ends: hidden unsaved objects outlive it and a delayed Destroy may never run, so all of it goes now.
        void OnApplicationQuit()
        {
            Cleanup(true, false);
        }

        // In the editor this is the Game view gaining or losing focus.
        void OnApplicationFocus(bool focus)
        {
            if (focus) return;
            heldKeys = 0; // the key ups go elsewhere now
            if (closed || !ready) return;
            // and so do the mouse ups: a drag (or a UI control holding the mouse) would otherwise stay on
            if (drag != DragKind.None) EndDrag(null);
            staleFrames = 0;
            releaseUi = true;
        }

        void OnDestroy()
        {
            Cleanup(false, true);
        }

        // ---- avatar ----

        bool HasAvatar()
        {
            return Rig != null && Rig.Alive && Pose != null;
        }

        // one look for avatars: binds the first one that works, unless the one in hand is still there
        void Search()
        {
            nextSearch = Time.unscaledTime + SearchEvery;
            Candidates.Clear();
            try
            {
                foreach (Transform t in AvatarRig.FindCandidates())
                    if (t != null) Candidates.Add(t);
            }
            catch (Exception e) { LogError(e); }

            if (Rig != null && Rig.Alive && Rig.Root.gameObject.activeInHierarchy)
            {
                searching = false;
                Problem = null;
                return;
            }
            string error = null;
            for (int i = 0; i < Candidates.Count; i++)
            {
                string err;
                if (TryBind(Candidates[i], out err)) return;
                if (error == null) error = err;
            }
            // the UI shows "searching" while Problem is empty
            if (Time.unscaledTime < searchUntil) return;
            searching = false;
            Problem = error ?? L.T("场景里没有找到模型（带 VRC Avatar Descriptor 或 Humanoid 的物体）");
        }

        // Takes the avatar at root. The new one is bound first and the one in hand released only after that worked,
        // so a failure leaves the current avatar, its pose and its undo steps as they were.
        bool TryBind(Transform root, out string error)
        {
            error = null;
            if (root == null)
            {
                error = L.T("场景里没有找到模型（带 VRC Avatar Descriptor 或 Humanoid 的物体）");
                return false;
            }
            // Except for an avatar that shares bones or the Animator with the one in hand (the same object, or one
            // inside the other): its rig would record the studio's pose and the Animator the studio switched off as
            // the scene's own, and hand those back at the end. The one in hand is given back first then.
            if (Rig != null && (!Rig.Alive || Rig.Root == null || root == Rig.Root || root.IsChildOf(Rig.Root) ||
                Rig.Root.IsChildOf(root)))
                ReleaseRig();

            AvatarRig rig = null;
            try { rig = AvatarRig.Bind(root, out error); }
            catch (Exception e)
            {
                LogError(e);
                rig = null;
                error = L.F("没能使用这个模型：{0}", e.Message);
            }
            if (rig == null)
            {
                if (string.IsNullOrEmpty(error)) error = L.T("没能使用这个模型");
                return false;
            }

            PoseModel pose = null;
            PoseSlots slots;
            try
            {
                pose = new PoseModel(rig);
                // an avatar standing in its import T-pose starts relaxed (no undo step: there is nothing before it)
                if (pose.LooksLikeTPose()) pose.StandNaturally();
                pose.Apply(); // the framing and the gaze target below see the pose the studio shows
                slots = PoseSlots.Load();
                StopHandleDrag(); // a drag on the avatar in hand ends on its own pose
                Handles.Bind(rig);
            }
            catch (Exception e)
            {
                LogError(e);
                // the handles go back to the avatar in hand (if any), the new one is given back
                try { Handles.Bind(HasAvatar() ? Rig : null); } catch (Exception e2) { LogError(e2); }
                if (pose != null)
                {
                    try { pose.Dispose(); } catch (Exception e2) { LogError(e2); }
                }
                try { rig.Release(); } catch (Exception e2) { LogError(e2); }
                error = L.F("没能使用这个模型：{0}", e.Message);
                return false;
            }

            // the new avatar works: only now the old one goes back (its bones, its face, its Animator)
            ReleaseRig();
            Rig = rig;
            Pose = pose;
            Slots = slots;
            // gaze, face and framing are extras: the avatar can be posed and shot without them
            try
            {
                Look.Bind(rig);
                Look.Target = Look.DefaultTarget(rig);
            }
            catch (Exception e) { LogError(e); }
            try { Face.Bind(rig); } catch (Exception e) { LogError(e); }
            try { View.FrameAvatar(rig, Framing.Full, Photo.Aspect); } catch (Exception e) { LogError(e); }
            Problem = null;
            searching = false;
            failed = 0;
            animatorFights = 0;
            return true;
        }

        // gives the avatar back (its face, its Animator); a handle drag on it ends
        void ReleaseRig()
        {
            StopHandleDrag();
            try { Face.Release(); } catch (Exception e) { LogError(e); }
            if (Rig != null)
            {
                try { Rig.Release(); } catch (Exception e) { LogError(e); }
            }
            if (Pose != null)
            {
                try { Pose.Dispose(); } catch (Exception e) { LogError(e); }
            }
            Rig = null;
            Pose = null;
        }

        // ends a handle drag on the avatar in hand (the edit so far stays, as on a MouseUp)
        void StopHandleDrag()
        {
            if (drag == DragKind.Handle) drag = DragKind.None;
            if (!Handles.Dragging) return;
            // the handles let go only on a MouseUp
            try { Handles.HandleEvent(new Event { type = EventType.MouseUp, button = 0 }, View.Cam, Pose, Look); }
            catch (Exception e) { LogError(e); }
        }

        // Gesture Manager, Av3Emulator and the like may switch the Animator back on after the studio took the avatar;
        // it would then overwrite the face. Switched off again, but only a few times: no fight every frame.
        void KeepAnimatorOff()
        {
            Animator a = Rig.Animator;
            if (animatorFights >= AnimatorFightsMax || !a.enabled) return;
            a.enabled = false;
            if (++animatorFights == AnimatorFightsMax)
                Debug.LogWarning("[MioVRCA] 有其他工具反复打开模型的 Animator（例如 Gesture Manager），摄影棚不再关闭它，表情可能会被动画覆盖");
        }

        // ---- the shot ----

        void Shoot()
        {
            string err = null, path = null;
            try
            {
                // the picture is what is inside the frame drawn over the view
                float frameScale = Screen.height > 0 ? FrameRect().height / Screen.height : 1f;
                path = Photo.Shoot(View, Back, frameScale, out err);
            }
            catch (Exception e)
            {
                LogError(e);
                path = null;
                err = L.F("拍照时出错：{0}", e.Message);
            }
            if (!string.IsNullOrEmpty(path))
            {
                Toast(L.T("已保存照片"), 2.5f);
                Debug.Log("[MioVRCA] 摄影棚照片已保存：" + path);
            }
            else Toast(string.IsNullOrEmpty(err) ? L.T("照片没有保存成功") : err, 4f);
        }

        // ---- keyboard ----

        void Keys(Event e)
        {
            KeyCode k = e.keyCode;
            if (k == KeyCode.None) return; // the character half of a key press

            if (k == KeyCode.Escape)
            {
                if (GUIUtility.keyboardControl != 0)
                {
                    // out of the text field; the event stays for the UI, which tracks its own focus
                    GUIUtility.keyboardControl = 0;
                    return;
                }
                if (drag == DragKind.Handle)
                {
                    // the handles put the pose back as it was before the drag
                    if (HasAvatar() && View.Cam != null && Handles.Dragging) Handles.HandleEvent(e, View.Cam, Pose, Look);
                    EndDrag(e);
                }
                else if (drag != DragKind.None) drag = DragKind.None;
                else if (Photo.Countdown > 0f)
                {
                    Photo.Cancel();
                    Toast(L.T("已取消倒计时"), 1.5f);
                }
                else return;
                if (e.type != EventType.Used) e.Use();
                return;
            }
            if (GUIUtility.keyboardControl != 0 || drag != DragKind.None) return;

            if (e.control || e.command)
            {
                if (k == KeyCode.Z)
                {
                    if (e.shift) Redo();
                    else Undo();
                    e.Use();
                }
                else if (k == KeyCode.Y)
                {
                    Redo();
                    e.Use();
                }
                return;
            }
            if (k == KeyCode.Z && !e.alt)
            {
                // Z / Shift+Z as well, so undo always has a key: Unity's own Edit/Undo still takes Ctrl+Z when its
                // shortcuts could not be kept out of the Game view (StudioHost.CaptureShortcuts), when the user
                // switched them back on there, or from the macOS menu bar
                if (e.shift) Redo();
                else Undo();
                e.Use();
                return;
            }
            int bit = KeyBit(k);
            if (e.alt || bit == 0) return;
            e.Use();
            if ((heldKeys & bit) != 0) return; // key repeat: holding Space must not take a burst of pictures
            heldKeys |= bit;
            switch (k)
            {
                case KeyCode.Space:
                    RequestShot();
                    break;
                case KeyCode.Tab:
                    UiHidden = !UiHidden; // the UI shows how to get it back
                    break;
                case KeyCode.F:
                    if (HasAvatar()) View.FrameAvatar(Rig, Framing.Full, Photo.Aspect);
                    break;
                case KeyCode.H:
                    Handles.Visible = !Handles.Visible;
                    Toast(Handles.Visible ? L.T("已显示骨骼控制点") : L.T("已隐藏骨骼控制点"), 1.5f);
                    PrefsChanged();
                    break;
            }
        }

        static int KeyBit(KeyCode k)
        {
            switch (k)
            {
                case KeyCode.Space: return 1;
                case KeyCode.Tab: return 2;
                case KeyCode.F: return 4;
                case KeyCode.H: return 8;
                default: return 0;
            }
        }

        void Undo()
        {
            if (!HasAvatar()) return;
            if (!Pose.Undo()) Toast(L.T("没有可以撤销的操作"), 1.5f);
        }

        void Redo()
        {
            if (!HasAvatar()) return;
            if (!Pose.Redo()) Toast(L.T("没有可以重做的操作"), 1.5f);
        }

        // ---- mouse ----

        // The mouse over the view (GUI.matrix = identity): left on a handle drags it, else orbits (or turns the light
        // in light drag mode); right orbits, middle or Alt + right pans, the wheel dollies. A drag keeps its kind until
        // its button goes up, also over the UI.
        void Mouse(Event e)
        {
            bool avatar = HasAvatar() && View.Cam != null;
            bool handlesOn = avatar && Handles.Visible && !UiHidden;
            Vector2 mouse = e.mousePosition;
            bool overUi = Ui.IsOverUi(mouse);
            switch (e.type)
            {
                case EventType.MouseMove:
                case EventType.Repaint:
                {
                    // a MouseMove comes only with no button held: a drag still running lost its MouseUp
                    if (e.type == EventType.MouseMove && drag != DragKind.None) EndDrag(e);
                    if (drag != DragKind.None || !avatar) break;
                    if (handlesOn && !overUi) Handles.UpdateHover(mouse, View.Cam, Rig, Look);
                    else if (Handles.Hover != null) Handles.UpdateHover(Nowhere, View.Cam, Rig, Look);
                    break;
                }
                case EventType.MouseDown:
                {
                    if (drag != DragKind.None)
                    {
                        if (e.button != dragButton && MouseHeld(dragButton) != 0)
                        {
                            e.Use(); // another button during a drag
                            break;
                        }
                        // the same button again, or the drag's button is up: its MouseUp went somewhere else
                        EndDrag(e);
                    }
                    if (overUi) break; // the UI's
                    GUIUtility.keyboardControl = 0; // a click on the view leaves the text field
                    DragKind kind = DragKind.None;
                    if (e.button == 0)
                    {
                        if (handlesOn && Handles.HandleEvent(e, View.Cam, Pose, Look)) kind = DragKind.Handle;
                        else kind = Lights.DragMode ? DragKind.Light : DragKind.Orbit;
                    }
                    else if (e.button == 1) kind = e.alt ? DragKind.Pan : DragKind.Orbit;
                    else if (e.button == 2) kind = DragKind.Pan;
                    if (kind == DragKind.None) break;
                    drag = kind;
                    dragButton = e.button;
                    lastMouse = mouse;
                    if (e.type != EventType.Used) e.Use();
                    break;
                }
                case EventType.MouseDrag:
                {
                    if (drag == DragKind.None) break;
                    // from the last position seen, so a step the UI happened to take is not lost
                    Vector2 d = mouse - lastMouse;
                    lastMouse = mouse;
                    switch (drag)
                    {
                        case DragKind.Handle:
                            if (avatar) Handles.HandleEvent(e, View.Cam, Pose, Look);
                            break;
                        case DragKind.Orbit:
                            View.Orbit(d);
                            break;
                        case DragKind.Pan:
                            View.Pan(d);
                            break;
                        case DragKind.Light:
                            Lights.Drag(d);
                            PrefsChanged();
                            break;
                    }
                    if (e.type != EventType.Used) e.Use();
                    break;
                }
                case EventType.MouseUp:
                {
                    if (drag == DragKind.None) break;
                    if (e.button == dragButton)
                    {
                        if (drag == DragKind.Handle && avatar) Handles.HandleEvent(e, View.Cam, Pose, Look);
                        drag = DragKind.None;
                    }
                    if (e.type != EventType.Used) e.Use();
                    break;
                }
                case EventType.ScrollWheel:
                {
                    if (overUi) break;
                    View.Dolly(e.delta.y);
                    e.Use();
                    break;
                }
            }
        }

        // ends the drag; a handle drag gets the MouseUp it missed, so the handles let go too (e: the event that
        // tells, or null outside OnGUI)
        void EndDrag(Event e)
        {
            if (drag == DragKind.Handle && Handles.Dragging && HasAvatar() && View.Cam != null)
            {
                var up = e != null ? new Event(e) { type = EventType.MouseUp, button = 0 }
                    : new Event { type = EventType.MouseUp, button = 0 };
                try { Handles.HandleEvent(up, View.Cam, Pose, Look); }
                catch (Exception ex) { LogError(ex); }
            }
            drag = DragKind.None;
        }

        // A drag (or a UI control holding the mouse) whose button is up lost its MouseUp, e.g. focus went elsewhere
        // mid-drag; the Game view sends no MouseMove that would tell. Until it ends, the shortcuts are off and clicks
        // with the other buttons are swallowed. The legacy Input state says which buttons are held: it must read "up"
        // for a few frames in a row, as it need not agree with the GUI events on the very same frame.
        void CheckLostMouseUp()
        {
            bool stale = false;
            if (drag != DragKind.None) stale = MouseHeld(dragButton) == 0;
            else if (UiKit.Claimed != 0) stale = MouseHeld(0) == 0 && MouseHeld(1) == 0 && MouseHeld(2) == 0;
            if (!stale)
            {
                staleFrames = 0;
                return;
            }
            if (++staleFrames < StaleFrames) return;
            staleFrames = 0;
            if (drag != DragKind.None) EndDrag(null);
            else releaseUi = true;
        }

        // 1: the mouse button is held, 0: it is up, -1: unknown (a project with only the new Input System, where the
        // Input class throws; then the studio relies on the GUI events alone, as before)
        static int MouseHeld(int button)
        {
            if (noLegacyInput) return -1;
            try { return Input.GetMouseButton(button) ? 1 : 0; }
            catch (InvalidOperationException)
            {
                noLegacyInput = true;
                return -1;
            }
        }

        // ---- closing ----

        void SavePrefs()
        {
            // only a studio whose parts were set up from the file: one that never got there (left over from a script
            // reload) would write its defaults over the user's settings
            if (!ready) return;
            if (Prefs == null) Prefs = new StudioPrefs();
            try { Prefs.ReadFrom(this); } catch (Exception e) { LogError(e); }
            Prefs.Save();
        }

        // Keeps StudioHost.RunningFile fresh (its time, and the unix seconds in it): MioVRCA does not remove the
        // package while the studio is open. Written in place, so the file never disappears between two beats.
        void Beat()
        {
            nextBeat = Time.unscaledTime + BeatEvery;
            try
            {
                Directory.CreateDirectory(StudioHost.DataDir);
                File.WriteAllText(StudioHost.RunningFile,
                    DateTimeOffset.UtcNow.ToUnixTimeSeconds().ToString(CultureInfo.InvariantCulture));
            }
            catch (Exception e)
            {
                if (!beatWarned) Debug.LogWarning("[MioVRCA] 摄影棚的运行标记写不进去：" + e.Message);
                beatWarned = true;
            }
        }

        // For the editor side: Update does not run while Play mode is paused, so the launcher beats from the editor
        // loop too, and the running file stays fresh as long as the studio is open.
        public void Heartbeat()
        {
            if (ready && !closed) Beat();
        }

        static void DeleteRunningFile()
        {
            try { File.Delete(StudioHost.RunningFile); }
            catch (Exception) { } // stale after a few seconds anyway
        }

        // Puts everything back, once. now: destroy at once (Play mode is ending, scripts are about to be reloaded, or
        // edit mode where Destroy is not allowed). destroying: called from OnDestroy, the object goes anyway.
        void Cleanup(bool now, bool destroying)
        {
            if (closed) return;
            closed = true;
            if (ReferenceEquals(Current, this)) Current = null;
            now |= !Application.isPlaying;
            if (ready)
            {
                Camera cam = View.Cam;
                Light key = Lights.Key, rim = Lights.RimLight;
                drag = DragKind.None;
                SavePrefs();
                Safe(Photo.Clear);
                Safe(Back.Destroy);
                Safe(Lights.Destroy);
                Safe(View.Destroy);
                ReleaseRig();
                Safe(Draw2D.Release);
                Action<bool> capture = StudioHost.CaptureShortcuts;
                if (capture != null) Safe(() => capture(false));
                DeleteRunningFile();
                if (now)
                {
                    // whatever a part left to a delayed Destroy: a camera left behind would keep drawing over the
                    // Game view, a light keep lighting the scene
                    DestroyNow(cam);
                    DestroyNow(key);
                    DestroyNow(rim);
                }
            }
            // a component left over from before a script reload: its parts are unknown. Not from OnDestroy (that is
            // a sweep destroying it), and never while another studio is open (the sweep would take its parts).
            else if (!destroying && Current == null) SweepLeftovers(gameObject);
            if (destroying) return;
            if (now) DestroyImmediate(gameObject);
            else Destroy(gameObject);
        }

        void DestroyNow(Component c)
        {
            if (c != null && c.gameObject != gameObject) DestroyImmediate(c.gameObject);
        }

        // After scripts are recompiled in Play mode without the launcher closing the studio first (normally it does,
        // see StudioLauncher), the component is still here but everything it held is gone: remove what is left of the
        // old studio and open a fresh one. Scene objects the old one had switched off come back when Play mode ends.
        void Recover()
        {
            closed = true;
            Debug.LogWarning("[MioVRCA] 脚本重新编译后，摄影棚重新打开");
            if (Current != null)
            {
                Destroy(gameObject); // a new studio is already open (its Open swept the rest)
                return;
            }
            SweepLeftovers(gameObject);
            Destroy(gameObject);
            Create("");
        }

        // Destroys the hidden, unsaved "MioVRCA Studio ..." things other than keep: the camera, lights and backdrop of
        // a lost studio with their meshes and materials, thumbnails and render textures. Only while no studio is open.
        static void SweepLeftovers(GameObject keep)
        {
            foreach (GameObject go in Resources.FindObjectsOfTypeAll<GameObject>())
                if (go != keep && Leftover(go)) DestroyLeftover(go);
            // what the parts made besides objects (after the objects: none of them still uses these then)
            foreach (Mesh m in Resources.FindObjectsOfTypeAll<Mesh>())
                if (Leftover(m)) DestroyLeftover(m);
            foreach (Material m in Resources.FindObjectsOfTypeAll<Material>())
                if (Leftover(m)) DestroyLeftover(m);
            foreach (Texture t in Resources.FindObjectsOfTypeAll<Texture>()) // Texture2D and RenderTexture
                if (Leftover(t)) DestroyLeftover(t);
        }

        static bool Leftover(UnityEngine.Object o)
        {
            return o != null && (o.hideFlags & HideFlags.DontSaveInEditor) != 0 &&
                   o.name.StartsWith(LeftoverPrefix, StringComparison.Ordinal);
        }

        static void DestroyLeftover(UnityEngine.Object o)
        {
            try
            {
                if (o != null) DestroyImmediate(o);
            }
            catch (Exception e) { LogError(e); } // e.g. an asset of that name: Unity refuses, it stays
        }

        // ---- helpers ----

        static void Safe(Action a)
        {
            try { a(); }
            catch (Exception e) { LogError(e); }
        }

        void Report(int part, Exception e)
        {
            if ((failed & (1 << part)) != 0) return;
            failed |= 1 << part;
            LogError(e);
        }

        static void LogError(Exception e)
        {
            Debug.LogWarning("[MioVRCA] 摄影棚出错：" + e);
        }
    }
}
