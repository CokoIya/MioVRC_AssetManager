// The avatar the studio works on: its root, its humanoid bones, how tall it is, and its face meshes. While the studio
// holds it, the Animator is switched off so nothing overwrites the pose (PhysBones keep running, they are separate).
// Release puts the bones back as they were at bind and gives the Animator back.
using System;
using System.Collections.Generic;
using System.Reflection;
using UnityEngine;
using UnityEngine.SceneManagement;

namespace MioVRCA.Studio
{
    internal sealed class AvatarRig
    {
        public Transform Root;
        public Animator Animator;
        public Component Descriptor; // VRC_AvatarDescriptor, found by reflection; null without the VRChat SDK
        public string Name;
        public readonly Transform[] Bones = new Transform[(int)HumanBodyBones.LastBone];
        public Transform LeftEye, RightEye; // humanoid eyes, else the descriptor's eye look bones
        public float Height = 1.5f; // world units, feet to the top of the head

        bool animatorWasEnabled;
        bool released;

        // the pose at bind, put back by Release
        readonly Quaternion[] bindLocal = new Quaternion[(int)HumanBodyBones.LastBone];
        Vector3 bindHipsPosition;

        // Eyes that are not humanoid bones (only the descriptor knows them): the pose never writes them, so LookAt
        // turns them from their rest rotation every frame, and Release puts them back.
        internal bool LeftEyeExtra, RightEyeExtra;
        internal Quaternion LeftEyeRest, RightEyeRest;

        // What LookAt turned after the last PoseModel.Apply: local rotation before and after, by bone. PoseModel.Capture
        // uses it so an edit read back from the transforms does not keep the gaze in the pose.
        internal readonly bool[] GazeSet = new bool[(int)HumanBodyBones.LastBone];
        internal readonly Quaternion[] GazeBefore = new Quaternion[(int)HumanBodyBones.LastBone];
        internal readonly Quaternion[] GazeAfter = new Quaternion[(int)HumanBodyBones.LastBone];

        public Transform this[HumanBodyBones b]
        {
            get { return b >= 0 && b < HumanBodyBones.LastBone ? Bones[(int)b] : null; }
        }

        public bool Alive
        {
            get { return Root != null && Animator != null && Bones[(int)HumanBodyBones.Hips] != null; }
        }

        // the avatar's front, flattened onto the ground
        public Vector3 Forward
        {
            get
            {
                if (Root == null) return Vector3.forward;
                Vector3 f = Root.forward;
                f.y = 0f;
                return f.sqrMagnitude < 1e-4f ? Vector3.forward : f.normalized;
            }
        }

        // Avatars in the open scenes, active ones only, in hierarchy order: objects with a VRC_AvatarDescriptor, or
        // (without the VRChat SDK) humanoid Animators.
        public static List<Transform> FindCandidates()
        {
            var list = new List<Transform>();
            Type desc = FindType("VRC.SDKBase.VRC_AvatarDescriptor");
            if (desc != null)
            {
                foreach (GameObject go in ActiveRoots())
                    foreach (Component c in go.GetComponentsInChildren(desc, false))
                        if (c != null && Usable(c.gameObject)) list.Add(c.transform);
            }
            if (list.Count > 0) return list;

            // no SDK (or no descriptor): humanoid Animators, the outermost one of each avatar
            foreach (GameObject go in ActiveRoots())
                foreach (Animator a in go.GetComponentsInChildren<Animator>(false))
                {
                    if (a == null || !IsHumanoid(a) || !Usable(a.gameObject)) continue;
                    bool inner = false;
                    foreach (Transform t in list)
                        if (a.transform.IsChildOf(t)) { inner = true; break; }
                    if (!inner) list.Add(a.transform);
                }
            return list;
        }

        // Takes the avatar: finds its humanoid bones and eyes, measures it and switches its Animator off.
        // Returns null with a reason (中文, through L.T) when it cannot be used, e.g. not humanoid.
        public static AvatarRig Bind(Transform root, out string error)
        {
            error = null;
            if (root == null)
            {
                error = L.T("模型已经不在场景里了");
                return null;
            }
            Animator anim = root.GetComponent<Animator>();
            if (anim == null || !IsHumanoid(anim))
            {
                anim = null;
                foreach (Animator a in root.GetComponentsInChildren<Animator>(false))
                    if (a != null && IsHumanoid(a)) { anim = a; break; }
            }
            if (anim == null)
            {
                error = L.T("该模型不是 Humanoid，无法摆姿势");
                return null;
            }

            var rig = new AvatarRig { Root = root, Animator = anim, Name = root.name };
            Type desc = FindType("VRC.SDKBase.VRC_AvatarDescriptor");
            if (desc != null) rig.Descriptor = root.GetComponent(desc);

            // bones first, while the Animator is still as the scene had it
            for (int i = 0; i < rig.Bones.Length; i++)
                rig.Bones[i] = anim.GetBoneTransform((HumanBodyBones)i);
            Transform hips = rig.Bones[(int)HumanBodyBones.Hips];
            if (hips == null)
            {
                error = L.T("找不到模型的 Hips 骨骼，无法摆姿势");
                return null;
            }

            rig.LeftEye = rig.Bones[(int)HumanBodyBones.LeftEye];
            rig.RightEye = rig.Bones[(int)HumanBodyBones.RightEye];
            if (rig.LeftEye == null || rig.RightEye == null)
            {
                Transform l, r;
                rig.DescriptorEyes(out l, out r);
                if (rig.LeftEye == null && l != null) { rig.LeftEye = l; rig.LeftEyeExtra = true; rig.LeftEyeRest = l.localRotation; }
                if (rig.RightEye == null && r != null) { rig.RightEye = r; rig.RightEyeExtra = true; rig.RightEyeRest = r.localRotation; }
            }

            rig.Height = rig.Measure();

            for (int i = 0; i < rig.Bones.Length; i++)
                rig.bindLocal[i] = rig.Bones[i] != null ? rig.Bones[i].localRotation : Quaternion.identity;
            rig.bindHipsPosition = hips.localPosition;

            rig.animatorWasEnabled = anim.enabled;
            anim.enabled = false;
            return rig;
        }

        // Gives the Animator back (when the studio closes or another avatar is chosen). The bones go back to the
        // pose they had at bind first, so an avatar whose Animator was off is left as it was found.
        public void Release()
        {
            if (released) return;
            released = true;
            for (int i = 0; i < Bones.Length; i++)
            {
                Transform t = Bones[i];
                if (t != null) t.localRotation = bindLocal[i];
            }
            Transform hips = Bones[(int)HumanBodyBones.Hips];
            if (hips != null) hips.localPosition = bindHipsPosition;
            if (LeftEyeExtra && LeftEye != null) LeftEye.localRotation = LeftEyeRest;
            if (RightEyeExtra && RightEye != null) RightEye.localRotation = RightEyeRest;
            if (Animator != null) Animator.enabled = animatorWasEnabled;
        }

        // About the top of the head, in world space.
        public Vector3 HeadTop()
        {
            Transform head = this[HumanBodyBones.Head];
            if (head == null) return Root != null ? Root.position + Vector3.up * Height : Vector3.zero;
            Vector3 h = head.position;
            float size = 0.07f * Height;
            Transform neck = this[HumanBodyBones.Neck];
            if (neck != null) size = Mathf.Max(size, 0.6f * Vector3.Distance(h, neck.position));
            // the head bone sits at the bottom of the skull: the eyes, when known, tell how big the head is (capped, in
            // case an eye is mapped to something far away)
            Vector3 eyes;
            if (EyeCenter(out eyes)) size = Mathf.Max(size, Mathf.Min(1.8f * Vector3.Distance(h, eyes), 0.35f * Height));
            return h + Vector3.up * size;
        }

        // Every SkinnedMeshRenderer under the avatar that has blend shapes, the face mesh first.
        public List<SkinnedMeshRenderer> BlendShapeMeshes()
        {
            var list = new List<SkinnedMeshRenderer>();
            if (Root == null) return list;
            SkinnedMeshRenderer face = FaceMesh();
            if (face != null) list.Add(face);
            foreach (SkinnedMeshRenderer smr in Root.GetComponentsInChildren<SkinnedMeshRenderer>(false))
                if (smr != face && HasShapes(smr)) list.Add(smr);
            return list;
        }

        // The descriptor's viseme mesh, else the mesh most likely to be the face.
        public SkinnedMeshRenderer FaceMesh()
        {
            if (Root == null) return null;
            SkinnedMeshRenderer viseme = DescriptorField("VisemeSkinnedMesh") as SkinnedMeshRenderer;
            if (viseme != null && HasShapes(viseme) && viseme.transform.IsChildOf(Root)) return viseme;

            // a mesh named like the face / body / head with the most shapes, else the one with the most shapes
            SkinnedMeshRenderer best = null, named = null;
            int bestCount = 0, namedCount = 0;
            foreach (SkinnedMeshRenderer smr in Root.GetComponentsInChildren<SkinnedMeshRenderer>(false))
            {
                if (!HasShapes(smr)) continue;
                int n = smr.sharedMesh.blendShapeCount;
                if (n > bestCount) { best = smr; bestCount = n; }
                if (n > namedCount && LooksLikeFace(smr.name)) { named = smr; namedCount = n; }
            }
            return named != null ? named : best;
        }

        public static Type FindType(string fullName)
        {
            foreach (var asm in AppDomain.CurrentDomain.GetAssemblies())
            {
                Type t = asm.GetType(fullName, false);
                if (t != null) return t;
            }
            return null;
        }

        // the middle of the two eyes (or the one there is)
        internal bool EyeCenter(out Vector3 p)
        {
            bool l = LeftEye != null, r = RightEye != null;
            if (l && r) p = (LeftEye.position + RightEye.position) * 0.5f;
            else if (l) p = LeftEye.position;
            else if (r) p = RightEye.position;
            else p = Vector3.zero;
            return l || r;
        }

        static bool IsHumanoid(Animator a)
        {
            return a.avatar != null && a.avatar.isValid && a.avatar.isHuman && a.isHuman;
        }

        static bool HasShapes(SkinnedMeshRenderer smr)
        {
            return smr != null && smr.gameObject.activeInHierarchy && smr.sharedMesh != null && smr.sharedMesh.blendShapeCount > 0;
        }

        static bool LooksLikeFace(string name)
        {
            string n = (name ?? "").Trim().ToLowerInvariant();
            return n.StartsWith("body", StringComparison.Ordinal) || n.Contains("face") || n.Contains("head") || n.Contains("kao") || n.Contains("顔");
        }

        // the root objects of the loaded scenes that are active, in hierarchy order
        static List<GameObject> ActiveRoots()
        {
            var roots = new List<GameObject>();
            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                Scene scene = SceneManager.GetSceneAt(i);
                if (!scene.isLoaded) continue;
                foreach (GameObject go in scene.GetRootGameObjects())
                    if (go != null && go.activeInHierarchy) roots.Add(go);
            }
            return roots;
        }

        // not ours, not hidden, not an emulator's copy of the avatar
        static bool Usable(GameObject go)
        {
            if (go == null || !go.activeInHierarchy || (go.hideFlags & HideFlags.HideInHierarchy) != 0) return false;
            string n = go.name;
            return !n.StartsWith("MioVRCA Studio", StringComparison.Ordinal) &&
                   !n.EndsWith("(MirrorReflection)", StringComparison.Ordinal) && !n.EndsWith("(ShadowClone)", StringComparison.Ordinal);
        }

        object DescriptorField(string field)
        {
            if (Descriptor == null) return null;
            try
            {
                FieldInfo f = Descriptor.GetType().GetField(field, BindingFlags.Public | BindingFlags.Instance);
                return f != null ? f.GetValue(Descriptor) : null;
            }
            catch (Exception)
            {
                return null;
            }
        }

        // customEyeLookSettings.leftEye / rightEye (VRChat SDK 3), tolerant of older or missing SDKs
        void DescriptorEyes(out Transform left, out Transform right)
        {
            left = right = null;
            object s = DescriptorField("customEyeLookSettings");
            if (s == null) return;
            try
            {
                Type t = s.GetType();
                FieldInfo lf = t.GetField("leftEye", BindingFlags.Public | BindingFlags.Instance);
                FieldInfo rf = t.GetField("rightEye", BindingFlags.Public | BindingFlags.Instance);
                left = lf != null ? lf.GetValue(s) as Transform : null;
                right = rf != null ? rf.GetValue(s) as Transform : null;
            }
            catch (Exception)
            {
                left = right = null;
            }
            // only eyes of this avatar
            if (left != null && !left.IsChildOf(Root)) left = null;
            if (right != null && !right.IsChildOf(Root)) right = null;
        }

        // feet to the top of the head: from the descriptor's view position (eye height) when it is set, else the bones
        float Measure()
        {
            object v = DescriptorField("ViewPosition");
            if (v is Vector3)
            {
                float eye = ((Vector3)v).y;
                if (eye > 0.05f) return Mathf.Max(0.2f, eye * Mathf.Abs(Root.lossyScale.y) / 0.92f);
            }
            float low = Root.position.y;
            foreach (HumanBodyBones b in Feet)
            {
                Transform t = this[b];
                if (t != null) low = Mathf.Min(low, t.position.y);
            }
            Transform head = this[HumanBodyBones.Head];
            if (head == null) return Height;
            // HeadTop scales with Height: measure to the head bone first, then to the top
            Height = Mathf.Max(0.2f, head.position.y - low);
            return Mathf.Max(0.2f, HeadTop().y - low);
        }

        static readonly HumanBodyBones[] Feet =
        {
            HumanBodyBones.LeftFoot, HumanBodyBones.RightFoot, HumanBodyBones.LeftToes, HumanBodyBones.RightToes,
        };
    }
}
