// Expressions: the blend shapes of the face mesh as sliders (with a filter), and five expression slots per project
// in UserSettings/MioVRCA/studio/faces.json (by blend shape name, so a slot works on any mesh that has the names).
// The weights every mesh had when the studio took the avatar are kept: 重置 puts them back, and so does Release
// (closing the studio, or another avatar).
using System;
using System.Collections.Generic;
using UnityEngine;

namespace MioVRCA.Studio
{
    internal sealed class FaceControl
    {
        public const int SlotCount = 5;

        public List<SkinnedMeshRenderer> Meshes = new List<SkinnedMeshRenderer>();
        public string Filter = "";

        public SkinnedMeshRenderer Mesh { get; private set; }

        // blend shape names of Mesh, by index
        public string[] Names = new string[0];

        // faces.json: per slot the names and weights of the shapes that were not 0
        [Serializable]
        internal sealed class Slot
        {
            public bool used;
            public string[] names = new string[0];
            public float[] weights = new float[0];
        }

        [Serializable]
        internal sealed class SlotFile
        {
            public Slot[] slots = new Slot[0];
        }

        // the weights of each mesh used, as they were when the studio took the avatar
        readonly Dictionary<SkinnedMeshRenderer, float[]> initial = new Dictionary<SkinnedMeshRenderer, float[]>();
        int count; // blend shapes of Mesh when it was chosen (guards the index even if Names is replaced)
        Slot[] slots = new Slot[SlotCount];
        static readonly string[] NoNames = new string[0];

        // Visible() is asked for on every GUI event: the list is rebuilt only when the mesh or the filter changed
        readonly List<int> visible = new List<int>();
        int version, visibleVersion = -1;
        string visibleFilter;
        string[] visibleNames;

        static string SlotPath
        {
            get { return System.IO.Path.Combine(StudioHost.DataDir, "faces.json"); }
        }

        public void Bind(AvatarRig rig)
        {
            initial.Clear();
            Meshes.Clear();
            SkinnedMeshRenderer face = null;
            if (rig != null)
            {
                List<SkinnedMeshRenderer> all = rig.BlendShapeMeshes();
                if (all != null)
                    foreach (SkinnedMeshRenderer m in all)
                        if (m != null && !Meshes.Contains(m)) Meshes.Add(m);
                face = rig.FaceMesh();
            }
            // the face mesh, unless it has nothing to move: then the first mesh that has
            if (!HasShapes(face)) face = null;
            if (face == null && Meshes.Count > 0) face = Meshes[0];
            if (face != null && !Meshes.Contains(face)) Meshes.Insert(0, face);
            Use(face);
            LoadSlots();
        }

        public void Use(SkinnedMeshRenderer smr)
        {
            if (smr == null) smr = null; // not a destroyed one (Unity's fake null)
            Mesh = smr;
            Names = NoNames;
            count = 0;
            version++;
            if (smr == null) return;
            UnityEngine.Mesh mesh = smr.sharedMesh;
            if (mesh == null) return;
            int n = mesh.blendShapeCount;
            var names = new string[n];
            for (int i = 0; i < n; i++) names[i] = mesh.GetBlendShapeName(i);
            Names = names;
            count = n;
            float[] w;
            if (!initial.TryGetValue(smr, out w) || w.Length != n)
            {
                w = new float[n];
                for (int i = 0; i < n; i++) w[i] = smr.GetBlendShapeWeight(i);
                initial[smr] = w;
            }
        }

        public float Get(int i)
        {
            if (i < 0 || i >= count || Mesh == null) return 0f;
            return Mesh.GetBlendShapeWeight(i);
        }

        public void Set(int i, float weight)
        {
            if (i < 0 || i >= count || Mesh == null || float.IsNaN(weight)) return;
            Mesh.SetBlendShapeWeight(i, Mathf.Clamp(weight, 0f, 100f));
        }

        // every blend shape of Mesh back to the value it had when the studio took the avatar
        public void ResetAll()
        {
            float[] w;
            if (Mesh == null || !initial.TryGetValue(Mesh, out w)) return;
            int n = Mathf.Min(count, w.Length);
            for (int i = 0; i < n; i++) Mesh.SetBlendShapeWeight(i, w[i]);
        }

        // Indices of Names that match Filter (case-insensitive substring), all when Filter is empty. The same list
        // object every time (do not change it); rebuilt only when Filter or Mesh changed.
        public List<int> Visible()
        {
            string f = Filter ?? "";
            if (visibleVersion == version && ReferenceEquals(visibleNames, Names) && string.Equals(f, visibleFilter, StringComparison.Ordinal))
                return visible;
            visibleVersion = version;
            visibleNames = Names;
            visibleFilter = f;
            visible.Clear();
            string q = f.Trim();
            string[] names = Names ?? NoNames;
            for (int i = 0; i < names.Length; i++)
                if (q.Length == 0 || (names[i] != null && names[i].IndexOf(q, StringComparison.OrdinalIgnoreCase) >= 0))
                    visible.Add(i);
            return visible;
        }

        public bool HasSlot(int i)
        {
            return slots != null && i >= 0 && i < slots.Length && slots[i] != null && slots[i].used;
        }

        public void StoreSlot(int i)
        {
            if (i < 0 || i >= SlotCount || Mesh == null) return;
            var names = new List<string>();
            var weights = new List<float>();
            for (int k = 0; k < count && k < Names.Length; k++)
            {
                float w = Mesh.GetBlendShapeWeight(k);
                if (w <= 0.001f || string.IsNullOrEmpty(Names[k])) continue;
                names.Add(Names[k]);
                weights.Add(w);
            }
            if (slots == null || slots.Length != SlotCount) slots = new Slot[SlotCount];
            slots[i] = new Slot { used = true, names = names.ToArray(), weights = weights.ToArray() };
            SaveSlots();
        }

        public void ApplySlot(int i)
        {
            if (!HasSlot(i) || Mesh == null) return;
            Slot s = slots[i];
            var map = new Dictionary<string, float>();
            if (s.names != null && s.weights != null)
                for (int k = 0; k < s.names.Length && k < s.weights.Length; k++)
                    if (!string.IsNullOrEmpty(s.names[k])) map[s.names[k]] = s.weights[k];
            for (int k = 0; k < count && k < Names.Length; k++)
            {
                float w;
                Set(k, Names[k] != null && map.TryGetValue(Names[k], out w) ? w : 0f);
            }
        }

        // Puts back every blend shape the studio changed on any mesh and forgets the avatar.
        internal void Release()
        {
            foreach (KeyValuePair<SkinnedMeshRenderer, float[]> kv in initial)
            {
                SkinnedMeshRenderer smr = kv.Key;
                if (smr == null) continue;
                UnityEngine.Mesh mesh = smr.sharedMesh;
                if (mesh == null) continue;
                int n = Mathf.Min(kv.Value.Length, mesh.blendShapeCount);
                for (int i = 0; i < n; i++)
                    if (smr.GetBlendShapeWeight(i) != kv.Value[i]) smr.SetBlendShapeWeight(i, kv.Value[i]);
            }
            initial.Clear();
            Meshes.Clear();
            Use(null);
        }

        static bool HasShapes(SkinnedMeshRenderer smr)
        {
            return smr != null && smr.sharedMesh != null && smr.sharedMesh.blendShapeCount > 0;
        }

        // the slots from faces.json; empty ones when there is no file or it cannot be read (never throws)
        void LoadSlots()
        {
            slots = new Slot[SlotCount];
            try
            {
                string text = StudioHost.ReadText(SlotPath);
                if (string.IsNullOrEmpty(text)) return;
                SlotFile f = JsonUtility.FromJson<SlotFile>(text);
                if (f == null || f.slots == null) return;
                for (int i = 0; i < SlotCount && i < f.slots.Length; i++) slots[i] = f.slots[i];
            }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 表情槽位读取失败：" + e.Message);
                slots = new Slot[SlotCount];
            }
        }

        void SaveSlots()
        {
            try
            {
                var f = new SlotFile { slots = new Slot[SlotCount] };
                for (int i = 0; i < SlotCount; i++) f.slots[i] = slots[i] ?? new Slot();
                StudioHost.WriteText(SlotPath, JsonUtility.ToJson(f, true));
            }
            catch (Exception e) { Debug.LogWarning("[MioVRCA] 表情槽位没能保存：" + e.Message); }
        }
    }
}
