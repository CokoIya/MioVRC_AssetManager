// The pose the studio keeps for the avatar: a local rotation for every humanoid bone and the hips position. It is
// written to the bones every frame (the Animator is off), edits read the bones back, and undo / redo keep copies.
using System;
using System.Collections.Generic;
using UnityEngine;

namespace MioVRCA.Studio
{
    internal sealed class PoseState
    {
        public Vector3 HipsLocalPosition;
        public readonly Quaternion[] Local = new Quaternion[(int)HumanBodyBones.LastBone];

        public PoseState Clone()
        {
            var c = new PoseState { HipsLocalPosition = HipsLocalPosition };
            Array.Copy(Local, c.Local, Local.Length);
            return c;
        }

        public void CopyTo(PoseState o)
        {
            o.HipsLocalPosition = HipsLocalPosition;
            Array.Copy(Local, o.Local, Local.Length);
        }
    }

    internal sealed class PoseModel
    {
        public const int UndoLimit = 100;

        public readonly AvatarRig Rig;

        readonly PoseState current = new PoseState();
        readonly bool[] mapped = new bool[(int)HumanBodyBones.LastBone];
        readonly List<PoseState> undo = new List<PoseState>(), redo = new List<PoseState>();

        // what the last BeginEdit did to the history, so CancelEdit can take it back: the snapshot it pushed (null
        // once the history changed again), the oldest undo step the limit pushed out, and the redo steps it dropped
        PoseState editPushed, editDroppedUndo;
        readonly List<PoseState> editDroppedRedo = new List<PoseState>();

        HumanPoseHandler handler;
        HumanPose humanPose;
        bool handlerFailed, disposed;
        bool initialTPose; // the arms were straight out at bind: their rotations then are the straight elbows and wrists

        public PoseModel(AvatarRig rig)
        {
            Rig = rig;
            if (rig != null)
                for (int i = 0; i < mapped.Length; i++)
                    mapped[i] = rig.Bones[i] != null;
            CaptureAll();
            Initial = current.Clone();
            initialTPose = LooksLikeTPose();
        }

        // the pose when the studio took the avatar
        public PoseState Initial { get; private set; }

        public bool CanUndo
        {
            get { return undo.Count > 0; }
        }

        public bool CanRedo
        {
            get { return redo.Count > 0; }
        }

        // the avatar is still there
        internal bool Ready
        {
            get { return Rig != null && Rig.Alive; }
        }

        // the bone was found on the avatar (only those are kept and written)
        internal bool IsMapped(HumanBodyBones b)
        {
            return b >= 0 && b < HumanBodyBones.LastBone && mapped[(int)b];
        }

        // muscles can be read and set (HumanPoseHandler works for this avatar)
        internal bool HasHumanPose
        {
            get { return Handler() != null; }
        }

        // reads every bone from the transforms
        public void CaptureAll()
        {
            for (int i = 0; i < mapped.Length; i++)
            {
                if (mapped[i]) Read(i);
                else current.Local[i] = Quaternion.identity;
            }
        }

        // reads these bones from the transforms (after they were edited)
        public void Capture(params HumanBodyBones[] bones)
        {
            if (bones == null) return;
            foreach (HumanBodyBones b in bones)
                if (IsMapped(b)) Read((int)b);
        }

        // writes the pose to the bones
        public void Apply()
        {
            if (Rig == null) return;
            Transform[] bones = Rig.Bones;
            for (int i = 0; i < bones.Length; i++)
            {
                if (!mapped[i]) continue;
                Transform t = bones[i];
                if (t != null) t.localRotation = current.Local[i];
            }
            Transform hips = bones[(int)HumanBodyBones.Hips];
            if (mapped[(int)HumanBodyBones.Hips] && hips != null) hips.localPosition = current.HipsLocalPosition;
            // the transforms hold the pose alone again, without the gaze LookAt adds on top
            Array.Clear(Rig.GazeSet, 0, Rig.GazeSet.Length);
        }

        public PoseState Snapshot()
        {
            return current.Clone();
        }

        // puts a snapshot back and applies it
        public void Restore(PoseState s)
        {
            if (s == null) return;
            s.CopyTo(current);
            Apply();
        }

        // Call once before every edit (a drag, a preset, a reset): keeps the current pose for undo and drops redo.
        public void BeginEdit()
        {
            editPushed = Snapshot();
            editDroppedUndo = Push(undo, editPushed);
            editDroppedRedo.Clear();
            editDroppedRedo.AddRange(redo);
            redo.Clear();
        }

        // Takes the last BeginEdit back as if the edit never happened (a drag cancelled with Escape): the pose it
        // kept is put back, its undo step goes and the redo steps it dropped return. Returns false and does nothing
        // when the history has changed since (an undo, a redo or another edit).
        public bool CancelEdit()
        {
            PoseState s = editPushed;
            if (s == null || undo.Count == 0 || !ReferenceEquals(undo[undo.Count - 1], s)) return false;
            undo.RemoveAt(undo.Count - 1);
            if (editDroppedUndo != null) undo.Insert(0, editDroppedUndo);
            redo.Clear();
            redo.AddRange(editDroppedRedo);
            ForgetEdit();
            Restore(s);
            return true;
        }

        public bool Undo()
        {
            if (undo.Count == 0) return false;
            ForgetEdit();
            Push(redo, Snapshot());
            Restore(Pop(undo));
            return true;
        }

        public bool Redo()
        {
            if (redo.Count == 0) return false;
            ForgetEdit();
            Push(undo, Snapshot());
            Restore(Pop(redo));
            return true;
        }

        // ResetBone and ResetAll do not call BeginEdit: the caller does, so a reset is one undo step with whatever
        // else it belongs to (a double click resets a whole chain).
        public void ResetBone(HumanBodyBones b)
        {
            if (!IsMapped(b) || Initial == null) return;
            int i = (int)b;
            current.Local[i] = Initial.Local[i];
            if (b == HumanBodyBones.Hips) current.HipsLocalPosition = Initial.HipsLocalPosition;
            Apply();
        }

        public void ResetAll()
        {
            Restore(Initial);
        }

        // A relaxed standing pose for any humanoid: the arms are turned geometrically to hang down beside the body
        // with a little bend at the elbow, the wrists straight, and the hands get the relaxed preset (finger muscles).
        // Legs, spine and head stay as they are. Does not call BeginEdit (the caller does when it is an edit).
        public void StandNaturally()
        {
            if (!Ready) return;
            Apply(); // the bones hold exactly the pose (no gaze) before they are measured

            // the body's own side and front, from the shoulders (else the hips, else the root): the body may have been
            // turned away from the root with the hips handle
            Vector3 right = Side(HumanBodyBones.LeftUpperArm, HumanBodyBones.RightUpperArm);
            if (right == Vector3.zero) right = Side(HumanBodyBones.LeftUpperLeg, HumanBodyBones.RightUpperLeg);
            if (right == Vector3.zero) right = Vector3.Cross(Vector3.up, Rig.Forward);
            Vector3 fwd = Vector3.Cross(right, Vector3.up);

            RelaxArm(true, right, fwd);
            RelaxArm(false, right, fwd);
            CaptureAll();
            HandPresets.Set(this, 0, HandSide.Both, 0.8f);
            CaptureAll();
        }

        // the arms stand out sideways (a T-pose as imported): the studio then starts with StandNaturally
        public bool LooksLikeTPose()
        {
            return ArmLevel(HumanBodyBones.LeftUpperArm, HumanBodyBones.LeftLowerArm) &&
                   ArmLevel(HumanBodyBones.RightUpperArm, HumanBodyBones.RightLowerArm);
        }

        // the pose as Unity humanoid muscles (HumanPoseHandler), for presets that work on any avatar
        public float[] GetMuscles(out Vector3 bodyPosition, out Quaternion bodyRotation)
        {
            bodyPosition = Vector3.zero;
            bodyRotation = Quaternion.identity;
            var result = new float[HumanTrait.MuscleCount];
            HumanPoseHandler h = Handler();
            if (h == null) return result;
            try
            {
                Apply(); // read the pose itself, not the gaze on top of it
                h.GetHumanPose(ref humanPose);
                if (humanPose.muscles != null)
                    Array.Copy(humanPose.muscles, result, Mathf.Min(result.Length, humanPose.muscles.Length));
                bodyPosition = humanPose.bodyPosition;
                bodyRotation = humanPose.bodyRotation;
            }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 读取 Humanoid 肌肉失败：" + e.Message);
            }
            return result;
        }

        // Sets muscles through HumanPoseHandler, but only the bones `only` accepts change (all bones when null): the
        // others, and the hips position unless setBody, are put back as they were. The model is updated.
        // bodyPosition / bodyRotation (used with setBody): relative to the root, as SetHumanPose takes them (BodyToRoot).
        public void SetMuscles(float[] muscles, Vector3 bodyPosition, Quaternion bodyRotation, bool setBody, Predicate<HumanBodyBones> only)
        {
            if (muscles == null || !Ready) return;
            HumanPoseHandler h = Handler();
            if (h == null) return;
            PoseState before = current.Clone();
            bool done = false;
            try
            {
                Apply();
                h.GetHumanPose(ref humanPose);
                float[] m = humanPose.muscles;
                int n = Mathf.Min(m.Length, muscles.Length);
                for (int i = 0; i < n; i++) m[i] = muscles[i];
                if (setBody)
                {
                    humanPose.bodyPosition = bodyPosition;
                    humanPose.bodyRotation = bodyRotation;
                }
                else BodyToRoot(ref humanPose.bodyPosition, ref humanPose.bodyRotation); // the body stays as it is
                h.SetHumanPose(ref humanPose);

                // the round trip through muscles is not exact: bones that should not change get their rotation back
                Transform[] bones = Rig.Bones;
                for (int i = 0; i < bones.Length; i++)
                    if (mapped[i] && bones[i] != null && only != null && !only((HumanBodyBones)i))
                        bones[i].localRotation = before.Local[i];
                Transform hips = bones[(int)HumanBodyBones.Hips];
                if (!setBody && hips != null) hips.localPosition = before.HipsLocalPosition;
                CaptureAll();
                done = true;
            }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 设置 Humanoid 肌肉失败：" + e.Message);
            }
            finally
            {
                // never leave the avatar half changed
                if (!done) Restore(before);
            }
        }

        // GetHumanPose gives the body (its centre and facing) in world space, the position divided by the human scale,
        // but SetHumanPose takes it relative to the root, scaled the same way (Unity's docs for the two; a plain get
        // and set moves an avatar that is not at the origin). This turns the first into the second. A pose slot keeps
        // the body so, and another avatar then gets it at its own place and facing. Returns false, and changes
        // nothing, when the root or scale is unusable.
        internal bool BodyToRoot(ref Vector3 position, ref Quaternion rotation)
        {
            Transform root;
            float scale;
            if (!BodyFrame(out root, out scale)) return false;
            position = root.InverseTransformPoint(position * scale) / scale;
            rotation = Normalized(Quaternion.Inverse(root.rotation) * rotation);
            return true;
        }

        // e.g. "LeftHand" -> the bone's place in HumanTrait.BoneName and its muscles; finger bones are LeftThumbProximal..RightLittleDistal
        public static bool IsFinger(HumanBodyBones b)
        {
            return b >= HumanBodyBones.LeftThumbProximal && b <= HumanBodyBones.RightLittleDistal;
        }

        public void Dispose()
        {
            disposed = true;
            if (handler != null)
            {
                handler.Dispose();
                handler = null;
            }
        }

        // the bone's local rotation as the pose has it: what LookAt added since the last Apply is taken out again
        // (an edit made on top of the gaze keeps the edit, not the gaze)
        void Read(int i)
        {
            Transform t = Rig != null ? Rig.Bones[i] : null;
            if (t == null) return;
            Quaternion q = t.localRotation;
            if (Rig.GazeSet[i]) q = q * Quaternion.Inverse(Rig.GazeAfter[i]) * Rig.GazeBefore[i];
            current.Local[i] = Normalized(q);
            if (i == (int)HumanBodyBones.Hips) current.HipsLocalPosition = t.localPosition;
        }

        // the transform the HumanPoseHandler is made with (see Handler) and the avatar's human scale
        bool BodyFrame(out Transform root, out float scale)
        {
            root = Ready ? Rig.Animator.transform : null;
            scale = root != null ? Rig.Animator.humanScale : 0f;
            return root != null && scale > 1e-4f && !float.IsNaN(scale) && !float.IsInfinity(scale);
        }

        void ForgetEdit()
        {
            editPushed = null;
            editDroppedUndo = null;
            editDroppedRedo.Clear();
        }

        HumanPoseHandler Handler()
        {
            if (disposed || handlerFailed || !Ready) return null; // never use a handler on a destroyed avatar
            if (handler != null) return handler;
            Avatar av = Rig.Animator.avatar;
            if (av == null || !av.isValid || !av.isHuman) return null;
            try
            {
                handler = new HumanPoseHandler(av, Rig.Animator.transform);
            }
            catch (Exception e)
            {
                handlerFailed = true;
                Debug.LogWarning("[MioVRCA] 无法读取 Humanoid 姿势：" + e.Message);
            }
            return handler;
        }

        // the unit vector from the left bone to the right one, flat on the ground; zero when there is none
        Vector3 Side(HumanBodyBones left, HumanBodyBones right)
        {
            Transform l = Rig[left], r = Rig[right];
            if (l == null || r == null) return Vector3.zero;
            Vector3 d = r.position - l.position;
            d.y = 0f;
            return d.sqrMagnitude < 1e-8f ? Vector3.zero : d.normalized;
        }

        // Turns one arm to hang about 72 degrees below the horizontal, a little out to the side and 8 degrees forward,
        // with the elbow bent 12 degrees forward and the wrist in line with the forearm. right / fwd: the body's axes.
        void RelaxArm(bool left, Vector3 right, Vector3 fwd)
        {
            HumanBodyBones ub = left ? HumanBodyBones.LeftUpperArm : HumanBodyBones.RightUpperArm;
            HumanBodyBones lb = left ? HumanBodyBones.LeftLowerArm : HumanBodyBones.RightLowerArm;
            HumanBodyBones hb = left ? HumanBodyBones.LeftHand : HumanBodyBones.RightHand;
            Transform upper = Rig[ub], lower = Rig[lb], hand = Rig[hb];
            if (upper == null || lower == null) return;
            Vector3 outward = left ? -right : right;

            if (initialTPose)
            {
                // start from the arm as it was at bind: elbow and wrist straight, and dropping it from there turns
                // the palm towards the thigh
                upper.localRotation = Initial.Local[(int)ub];
                lower.localRotation = Initial.Local[(int)lb];
                if (hand != null) hand.localRotation = Initial.Local[(int)hb];
            }
            else if (hand != null)
            {
                // straighten the elbow, so the result does not depend on how the arm was bent
                Vector3 fore = hand.position - lower.position, arm = lower.position - upper.position;
                if (fore.sqrMagnitude > 1e-10f && arm.sqrMagnitude > 1e-10f)
                    lower.rotation = Quaternion.FromToRotation(fore, arm) * lower.rotation;
            }

            const float down = 72f * Mathf.Deg2Rad, ahead = 8f * Mathf.Deg2Rad;
            Vector3 want = (Vector3.down * Mathf.Sin(down) + outward * Mathf.Cos(down)) * Mathf.Cos(ahead) + fwd * Mathf.Sin(ahead);
            Vector3 cur = lower.position - upper.position;
            if (cur.sqrMagnitude > 1e-10f) upper.rotation = Quaternion.FromToRotation(cur, want) * upper.rotation;
            if (hand == null) return;

            // the elbow bends forward around the side axis (the hand turns with the forearm)
            Vector3 forearm = hand.position - lower.position;
            Vector3 axis = Vector3.Cross(forearm, fwd); // turning about Cross(a, b) moves a towards b
            if (axis.sqrMagnitude < 1e-10f) axis = -right;
            lower.rotation = Quaternion.AngleAxis(12f, axis.normalized) * lower.rotation;
            if (initialTPose) return; // the wrist kept its straight rotation from bind

            // the wrist straight: hand to knuckles in line with the forearm, the hand's twist kept
            Transform k = Rig[left ? HumanBodyBones.LeftMiddleProximal : HumanBodyBones.RightMiddleProximal];
            if (k == null) k = Rig[left ? HumanBodyBones.LeftIndexProximal : HumanBodyBones.RightIndexProximal];
            if (k == null) return;
            Vector3 h = k.position - hand.position, f = hand.position - lower.position;
            if (h.sqrMagnitude > 1e-10f && f.sqrMagnitude > 1e-10f)
                hand.rotation = Quaternion.FromToRotation(h, f) * hand.rotation;
        }

        // the upper arm is within 25 degrees of the horizontal
        bool ArmLevel(HumanBodyBones upperBone, HumanBodyBones lowerBone)
        {
            if (Rig == null) return false;
            Transform upper = Rig[upperBone], lower = Rig[lowerBone];
            if (upper == null || lower == null) return false;
            Vector3 d = lower.position - upper.position;
            if (d.sqrMagnitude < 1e-10f) return false;
            return Mathf.Abs(Mathf.Asin(Mathf.Clamp(d.normalized.y, -1f, 1f))) * Mathf.Rad2Deg <= 25f;
        }

        // returns the oldest entry when the limit pushed it out, else null
        static PoseState Push(List<PoseState> stack, PoseState s)
        {
            stack.Add(s);
            if (stack.Count <= UndoLimit) return null;
            PoseState dropped = stack[0];
            stack.RemoveAt(0);
            return dropped;
        }

        static PoseState Pop(List<PoseState> stack)
        {
            PoseState s = stack[stack.Count - 1];
            stack.RemoveAt(stack.Count - 1);
            return s;
        }

        internal static Quaternion Normalized(Quaternion q)
        {
            float n = Mathf.Sqrt(q.x * q.x + q.y * q.y + q.z * q.z + q.w * q.w);
            if (n < 1e-6f || float.IsNaN(n) || float.IsInfinity(n)) return Quaternion.identity;
            return new Quaternion(q.x / n, q.y / n, q.z / n, q.w / n);
        }
    }

    // Five pose slots per project, in UserSettings/MioVRCA/studio/poses.json. A slot keeps the muscles (to use on any
    // avatar) and, for the avatar it was saved from, the exact local rotations.
    internal sealed class PoseSlots
    {
        public const int Count = 5;

        [Serializable]
        sealed class Slot
        {
            public bool used;
            public string avatar = "";            // Animator.avatar name + "/" + root name
            public float[] muscles = new float[0];
            public Vector3 bodyPosition;          // the HumanPose body, relative to the root (PoseModel.BodyToRoot)
            public Quaternion bodyRotation = Quaternion.identity;
            public bool bodyInRoot;               // false in files from version 1: the body was kept in world space
            public Quaternion[] local = new Quaternion[0]; // by HumanBodyBones
            public bool[] mapped = new bool[0];
            public Vector3 hips;                  // hips local position
        }

        [Serializable]
        sealed class FileData
        {
            public int version = 2;
            public Slot[] slots = new Slot[0];
        }

        readonly Slot[] slots = new Slot[Count];

        static string FilePath
        {
            get { return System.IO.Path.Combine(StudioHost.DataDir, "poses.json"); }
        }

        public static PoseSlots Load()
        {
            var ps = new PoseSlots();
            string text = StudioHost.ReadText(FilePath);
            if (string.IsNullOrEmpty(text)) return ps;
            try
            {
                FileData d = JsonUtility.FromJson<FileData>(text);
                if (d != null && d.slots != null)
                    for (int i = 0; i < Count && i < d.slots.Length; i++)
                        ps.slots[i] = d.slots[i];
            }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 姿势存档读不出来，当作空的：" + e.Message);
            }
            return ps;
        }

        public void Save()
        {
            try
            {
                var d = new FileData { slots = new Slot[Count] };
                for (int i = 0; i < Count; i++) d.slots[i] = slots[i] ?? new Slot();
                StudioHost.WriteText(FilePath, JsonUtility.ToJson(d, true));
            }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 姿势存档没保存成：" + e.Message);
            }
        }

        public bool Has(int i)
        {
            Slot s = Get(i);
            return s != null && s.used && (HasMuscles(s) || HasLocal(s));
        }

        public void Store(int i, PoseModel m)
        {
            if (i < 0 || i >= Count || m == null || !m.Ready) return;
            var s = new Slot { used = true, avatar = Signature(m.Rig) };
            if (m.HasHumanPose)
            {
                s.muscles = m.GetMuscles(out s.bodyPosition, out s.bodyRotation);
                s.bodyInRoot = m.BodyToRoot(ref s.bodyPosition, ref s.bodyRotation);
            }
            PoseState p = m.Snapshot();
            int n = (int)HumanBodyBones.LastBone;
            s.local = new Quaternion[n];
            s.mapped = new bool[n];
            for (int b = 0; b < n; b++)
            {
                s.mapped[b] = m.IsMapped((HumanBodyBones)b);
                s.local[b] = s.mapped[b] ? p.Local[b] : Quaternion.identity;
            }
            s.hips = p.HipsLocalPosition;
            slots[i] = s;
            Save();
        }

        // returns false when the slot is empty
        public bool ApplyTo(int i, PoseModel m)
        {
            if (!Has(i) || m == null || !m.Ready) return false;
            Slot s = slots[i];
            // the same avatar gets its exact rotations back; any other one gets the muscles
            bool exact = HasLocal(s) && s.avatar == Signature(m.Rig);
            if (!exact && (!HasMuscles(s) || !m.HasHumanPose)) return false;
            m.BeginEdit();
            if (exact)
            {
                PoseState p = m.Snapshot();
                for (int b = 0; b < p.Local.Length; b++)
                    if (s.mapped[b] && m.IsMapped((HumanBodyBones)b)) p.Local[b] = PoseModel.Normalized(s.local[b]);
                if (s.mapped[(int)HumanBodyBones.Hips] && IsFinite(s.hips)) p.HipsLocalPosition = s.hips;
                m.Restore(p);
            }
            else
            {
                var muscles = (float[])s.muscles.Clone();
                // The body is kept relative to the root, as SetHumanPose takes it. A body kept in world space (an
                // older file) is where the other avatar stood in the scene: this one then keeps its own body place
                // and facing and takes only the muscles.
                m.SetMuscles(muscles, s.bodyPosition, PoseModel.Normalized(s.bodyRotation), s.bodyInRoot, null);
            }
            return true;
        }

        Slot Get(int i)
        {
            return i >= 0 && i < Count ? slots[i] : null;
        }

        static string Signature(AvatarRig rig)
        {
            if (rig == null) return "";
            string av = rig.Animator != null && rig.Animator.avatar != null ? rig.Animator.avatar.name : "";
            string root = rig.Root != null ? rig.Root.name : "";
            return av + "/" + root;
        }

        static bool HasMuscles(Slot s)
        {
            if (s.muscles == null || s.muscles.Length != HumanTrait.MuscleCount || !IsFinite(s.bodyPosition)) return false;
            foreach (float f in s.muscles)
                if (float.IsNaN(f) || float.IsInfinity(f)) return false;
            return true;
        }

        static bool HasLocal(Slot s)
        {
            int n = (int)HumanBodyBones.LastBone;
            return s.local != null && s.mapped != null && s.local.Length == n && s.mapped.Length == n;
        }

        static bool IsFinite(Vector3 v)
        {
            return !float.IsNaN(v.x) && !float.IsNaN(v.y) && !float.IsNaN(v.z) &&
                   !float.IsInfinity(v.x) && !float.IsInfinity(v.y) && !float.IsInfinity(v.z);
        }
    }
}
