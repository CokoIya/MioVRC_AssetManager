// Access to Modular Avatar, the VRChat SDK and UnitySkills by name, so this package compiles in any project
// and reports what is missing instead of failing to compile.
using System;
using System.Collections.Generic;
using System.Reflection;
using UnityEditor;
using UnityEngine;
using UnityEngine.SceneManagement;

namespace MioVRCA.Pipeline
{
    internal class PipelineException : Exception
    {
        public PipelineException(string message) : base(message) { }
    }

    internal static class Refl
    {
        public const string MA = "nadena.dev.modular_avatar.core.";

        static readonly Dictionary<string, Type> cache = new Dictionary<string, Type>();

        public static Type Find(string fullName)
        {
            Type t;
            if (cache.TryGetValue(fullName, out t)) return t;
            foreach (Assembly asm in AppDomain.CurrentDomain.GetAssemblies())
            {
                t = asm.GetType(fullName, false);
                if (t != null) break;
            }
            cache[fullName] = t;
            return t;
        }

        public static Type Need(string fullName, string what)
        {
            Type t = Find(fullName);
            if (t == null) throw new PipelineException("工程里没有装 " + what + "，先用 VCC / ALCOM 装上");
            return t;
        }

        public static Type DescriptorType { get { return Find("VRC.SDKBase.VRC_AvatarDescriptor"); } }
        public static Type MAType(string shortName) { return Need(MA + shortName, "Modular Avatar"); }
        public static bool HasMA { get { return Find(MA + "ModularAvatarMenuItem") != null; } }

        public static string PackageVersion(string assemblyTypeName)
        {
            Type t = Find(assemblyTypeName);
            if (t == null) return "";
            var info = UnityEditor.PackageManager.PackageInfo.FindForAssembly(t.Assembly);
            return info != null ? info.version : "";
        }

        // ---------- scene ----------

        public static IEnumerable<GameObject> SceneRoots()
        {
            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                Scene s = SceneManager.GetSceneAt(i);
                if (!s.isLoaded) continue;
                foreach (GameObject go in s.GetRootGameObjects()) yield return go;
            }
        }

        // every avatar in the open scenes (inactive ones too), in hierarchy order
        public static List<Component> Avatars()
        {
            var o = new List<Component>();
            Type t = DescriptorType;
            if (t == null) return o;
            foreach (GameObject root in SceneRoots())
                foreach (Component c in root.GetComponentsInChildren(t, true))
                    o.Add(c);
            return o;
        }

        // "Avatar/Child/Sub" from the scene root; names are what the player sees in the Hierarchy
        public static string ScenePath(Transform t)
        {
            string p = t.name;
            while (t.parent != null) { t = t.parent; p = t.name + "/" + p; }
            return p;
        }

        public static string RelPath(Transform root, Transform t)
        {
            if (t == root) return "";
            string p = t.name;
            Transform c = t.parent;
            while (c != null && c != root) { p = c.name + "/" + p; c = c.parent; }
            if (c == null) throw new PipelineException("「" + t.name + "」不在「" + root.name + "」下面");
            return p;
        }

        // the avatar the request names (by scene path or name); with none named, the only active one
        public static Component FindAvatar(string name)
        {
            List<Component> all = Avatars();
            if (all.Count == 0)
            {
                if (DescriptorType == null) throw new PipelineException("工程里没有装 VRChat SDK（Avatars）");
                throw new PipelineException("打开的场景里没有头像（带 VRC Avatar Descriptor 的物体）。先在 Unity 里打开放着头像的场景");
            }
            if (!string.IsNullOrEmpty(name))
            {
                foreach (Component c in all) if (ScenePath(c.transform) == name) return c;
                foreach (Component c in all) if (c.gameObject.name == name) return c;
                throw new PipelineException("场景里找不到头像「" + name + "」");
            }
            var active = all.FindAll(c => c.gameObject.activeInHierarchy);
            if (active.Count == 1) return active[0];
            if (active.Count == 0 && all.Count == 1) return all[0];
            var names = new List<string>();
            foreach (Component c in active.Count > 0 ? active : all) names.Add(c.gameObject.name);
            throw new PipelineException("场景里有 " + names.Count + " 个头像（" + string.Join("、", names.ToArray()) + "），要说明用哪一个");
        }

        // an object under the avatar, by its path below the avatar ("Outfit/Jacket") or, when that is unique, its name
        public static Transform FindUnder(Transform avatar, string path)
        {
            if (string.IsNullOrEmpty(path)) throw new PipelineException("没有写物体的路径");
            string p = path.Replace('\\', '/').Trim('/');
            if (p.StartsWith(avatar.name + "/")) { Transform viaRoot = avatar.Find(p.Substring(avatar.name.Length + 1)); if (viaRoot != null) return viaRoot; }
            Transform t = avatar.Find(p);
            if (t != null) return t;
            Transform hit = null;
            int n = 0;
            foreach (Transform c in avatar.GetComponentsInChildren<Transform>(true))
            {
                if (c != avatar && c.name == p) { hit = c; n++; }
            }
            if (n == 1) return hit;
            if (n > 1) throw new PipelineException("头像下有 " + n + " 个叫「" + p + "」的物体，要写完整路径");
            throw new PipelineException("头像下找不到「" + path + "」");
        }

        public static string PrefabSource(GameObject go)
        {
            if (!PrefabUtility.IsPartOfPrefabInstance(go)) return "";
            GameObject root = PrefabUtility.GetNearestPrefabInstanceRoot(go);
            if (root != go) return "";
            return PrefabUtility.GetPrefabAssetPathOfNearestInstanceRoot(go) ?? "";
        }

        // The box a renderer really fills right now, in the world. A skinned mesh's own bounds are whatever its
        // author (or MA Mesh Settings) wrote, often one big box for the whole outfit, so its posed vertices are measured.
        public static Bounds TightBounds(Renderer r)
        {
            var smr = r as SkinnedMeshRenderer;
            if (smr == null || smr.sharedMesh == null) return r.bounds;
            Mesh baked = null;
            try
            {
                baked = new Mesh { hideFlags = HideFlags.HideAndDontSave };
                smr.BakeMesh(baked);
                Vector3[] vs = baked.vertices;
                if (vs.Length == 0) return r.bounds;
                // baked vertices carry the scale already: only position and rotation are left to apply
                Matrix4x4 m = Matrix4x4.TRS(smr.transform.position, smr.transform.rotation, Vector3.one);
                int step = Math.Max(1, vs.Length / 6000);
                Bounds b = new Bounds(m.MultiplyPoint3x4(vs[0]), Vector3.zero);
                for (int i = step; i < vs.Length; i += step) b.Encapsulate(m.MultiplyPoint3x4(vs[i]));
                return b;
            }
            catch (Exception)
            {
                return r.bounds;
            }
            finally
            {
                if (baked != null) UnityEngine.Object.DestroyImmediate(baked);
            }
        }

        // ---------- serialized fields ----------

        public static SerializedProperty Prop(SerializedObject so, string path)
        {
            SerializedProperty p = so.FindProperty(path);
            if (p == null)
                throw new PipelineException("这个版本的 " + so.targetObject.GetType().Name + " 没有「" + path + "」字段，Modular Avatar 可能太旧（需要 1.12 以上）");
            return p;
        }

        public static void SetEnum(SerializedObject so, string path, string name)
        {
            SerializedProperty p = Prop(so, path);
            int i = Array.IndexOf(p.enumNames, name);
            if (i < 0) throw new PipelineException(path + " 没有「" + name + "」这个选项");
            p.enumValueIndex = i;
        }

        public static string GetEnum(SerializedObject so, string path)
        {
            SerializedProperty p = so.FindProperty(path);
            if (p == null || p.enumValueIndex < 0 || p.enumValueIndex >= p.enumNames.Length) return "";
            return p.enumNames[p.enumValueIndex];
        }
    }
}
