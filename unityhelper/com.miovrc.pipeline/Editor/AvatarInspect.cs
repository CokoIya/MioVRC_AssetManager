// Read-only: what is in the open scenes, what an avatar wears, what its menu looks like, which prefabs a
// folder holds. Nothing here changes the scene or the assets.
using System;
using System.Collections;
using System.Collections.Generic;
using System.Reflection;
using UnityEditor;
using UnityEngine;
using UnityEngine.SceneManagement;

namespace MioVRCA.Pipeline
{
    internal static class AvatarInspect
    {
        public static object Overview(Dictionary<string, object> args)
        {
            var o = new Dictionary<string, object>();
            var scenes = new List<object>();
            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                Scene s = SceneManager.GetSceneAt(i);
                scenes.Add(new Dictionary<string, object> { { "name", s.name }, { "path", s.path }, { "dirty", s.isDirty }, { "loaded", s.isLoaded } });
            }
            o["scenes"] = scenes;
            o["modularAvatar"] = Refl.PackageVersion(Refl.MA + "ModularAvatarMenuItem");
            o["vrcSdk"] = Refl.DescriptorType != null;
            o["playing"] = EditorApplication.isPlayingOrWillChangePlaymode;
            string only = J.Str(args, "avatar");
            var list = new List<object>();
            foreach (Component c in Refl.Avatars())
            {
                if (only != "" && c.gameObject.name != only && Refl.ScenePath(c.transform) != only) continue;
                list.Add(Avatar(c));
            }
            o["avatars"] = list;
            return o;
        }

        static object Avatar(Component desc)
        {
            Transform root = desc.transform;
            var a = new Dictionary<string, object>();
            a["name"] = root.name;
            a["path"] = Refl.ScenePath(root);
            a["active"] = root.gameObject.activeInHierarchy;
            a["scene"] = root.gameObject.scene.name;
            a["prefab"] = Refl.PrefabSource(root.gameObject);
            FieldInfo view = desc.GetType().GetField("ViewPosition");
            if (view != null && view.GetValue(desc) is Vector3) a["eyeHeight"] = Math.Round(((Vector3)view.GetValue(desc)).y, 3);

            // the menu and the parameters the avatar descriptor points at
            object menu = Field(desc, "expressionsMenu");
            object pars = Field(desc, "expressionParameters");
            a["menuAsset"] = menu is UnityEngine.Object && (UnityEngine.Object)menu != null ? AssetDatabase.GetAssetPath((UnityEngine.Object)menu) : "";
            a["parametersAsset"] = pars is UnityEngine.Object && (UnityEngine.Object)pars != null ? AssetDatabase.GetAssetPath((UnityEngine.Object)pars) : "";
            if (pars is UnityEngine.Object && (UnityEngine.Object)pars != null)
            {
                MethodInfo cost = pars.GetType().GetMethod("CalcTotalCost", Type.EmptyTypes);
                if (cost != null) a["parameterBits"] = Convert.ToInt32(cost.Invoke(pars, null));
            }
            if (menu is UnityEngine.Object && (UnityEngine.Object)menu != null) a["descriptorMenu"] = VrcMenu(menu, 0);

            var kids = new List<object>();
            foreach (Transform c in root)
            {
                var k = new Dictionary<string, object>();
                k["name"] = c.name;
                k["active"] = c.gameObject.activeSelf;
                k["kind"] = Kind(c);
                var comps = ComponentNames(c.gameObject);
                if (comps.Count > 0) k["components"] = comps;
                int n = c.GetComponentsInChildren<Renderer>(true).Length;
                if (n > 0) k["renderers"] = n;
                string pf = Refl.PrefabSource(c.gameObject);
                if (pf != "") k["prefab"] = pf;
                kids.Add(k);
            }
            a["children"] = kids;
            a["maMenu"] = MaMenu(root);
            return a;
        }

        static object Field(object o, string name)
        {
            FieldInfo f = o.GetType().GetField(name);
            return f != null ? f.GetValue(o) : null;
        }

        // what a direct child of the avatar is
        static string Kind(Transform c)
        {
            Type merge = Refl.Find(Refl.MA + "ModularAvatarMergeArmature");
            Type item = Refl.Find(Refl.MA + "ModularAvatarMenuItem");
            Type installer = Refl.Find(Refl.MA + "ModularAvatarMenuInstaller");
            if (merge != null && c.GetComponentInChildren(merge, true) != null) return "outfit";
            Type proxy = Refl.Find(Refl.MA + "ModularAvatarBoneProxy");
            if (proxy != null && c.GetComponentInChildren(proxy, true) != null && c.GetComponentsInChildren<Renderer>(true).Length > 0) return "accessory";
            if ((item != null && c.GetComponent(item) != null) || (installer != null && c.GetComponent(installer) != null)) return "menu";
            if (c.GetComponent<SkinnedMeshRenderer>() != null) return "mesh";
            Animator an = c.parent != null ? c.parent.GetComponent<Animator>() : null;
            if (an != null && an.isHuman)
            {
                Transform hips = an.GetBoneTransform(HumanBodyBones.Hips);
                if (hips != null && hips.IsChildOf(c)) return "armature";
            }
            if (c.GetComponentsInChildren<Renderer>(true).Length > 0) return "object";
            return "other";
        }

        static List<object> ComponentNames(GameObject go)
        {
            var names = new List<object>();
            foreach (Component c in go.GetComponents<Component>())
            {
                if (c == null) { if (!names.Contains("(缺失脚本)")) names.Add("(缺失脚本)"); continue; }
                if (c is Transform) continue;
                string n = c.GetType().Name.Replace("ModularAvatar", "MA ");
                if (!names.Contains(n) && names.Count < 10) names.Add(n);
            }
            return names;
        }

        // ---------- menus ----------

        static object VrcMenu(object menu, int depth)
        {
            var o = new List<object>();
            var controls = Field(menu, "controls") as IList;
            if (controls == null) return o;
            foreach (object c in controls)
            {
                if (c == null) continue;
                var d = new Dictionary<string, object>();
                d["name"] = Convert.ToString(Field(c, "name"));
                d["type"] = Convert.ToString(Field(c, "type"));
                object par = Field(c, "parameter");
                string pn = par != null ? Convert.ToString(Field(par, "name")) : "";
                if (!string.IsNullOrEmpty(pn)) d["parameter"] = pn;
                object sub = Field(c, "subMenu");
                if (sub is UnityEngine.Object && (UnityEngine.Object)sub != null && depth < 2) d["children"] = VrcMenu(sub, depth + 1);
                o.Add(d);
            }
            return o;
        }

        // the Modular Avatar menu items under the avatar, as the tree they form
        static object MaMenu(Transform avatar)
        {
            var o = new List<object>();
            Type item = Refl.Find(Refl.MA + "ModularAvatarMenuItem");
            Type installer = Refl.Find(Refl.MA + "ModularAvatarMenuInstaller");
            if (item == null) return o;
            // roots: objects with a menu installer, and menu items whose parent is not part of a menu
            foreach (Transform t in avatar.GetComponentsInChildren<Transform>(true))
            {
                if (t == avatar) continue;
                bool isItem = t.GetComponent(item) != null;
                bool isInst = installer != null && t.GetComponent(installer) != null;
                if (!isItem && !isInst) continue;
                Transform p = t.parent;
                bool parentInMenu = p != null && p != avatar && (p.GetComponent(item) != null || (installer != null && p.GetComponent(installer) != null));
                if (parentInMenu) continue;
                o.Add(MaNode(avatar, t, item, 0));
            }
            return o;
        }

        static object MaNode(Transform avatar, Transform t, Type item, int depth)
        {
            var d = new Dictionary<string, object>();
            d["object"] = Refl.RelPath(avatar, t);
            Component mi = t.GetComponent(item);
            bool children = true;
            if (mi != null)
            {
                var so = new SerializedObject(mi);
                SerializedProperty label = so.FindProperty("label");
                d["label"] = label != null && label.stringValue != "" ? label.stringValue : t.name;
                string type = Refl.GetEnum(so, "Control.type");
                d["type"] = type;
                SerializedProperty pn = so.FindProperty("Control.parameter.name");
                if (pn != null && pn.stringValue != "") d["parameter"] = pn.stringValue;
                SerializedProperty pv = so.FindProperty("Control.value");
                if (pv != null && type != "SubMenu") d["value"] = Math.Round(pv.floatValue, 3);
                SerializedProperty def = so.FindProperty("isDefault");
                if (def != null && def.boolValue) d["default"] = true;
                SerializedProperty auto = so.FindProperty("automaticValue");
                if (auto != null && auto.boolValue) d["auto"] = true;
                children = type == "SubMenu" && Refl.GetEnum(so, "MenuSource") == "Children";
                var tg = Toggles(avatar, t);
                if (tg.Count > 0) d["toggles"] = tg;
            }
            else
            {
                d["label"] = t.name;
                d["type"] = "MenuInstaller";
            }
            if (children && depth < 6)
            {
                var kids = new List<object>();
                foreach (Transform c in t)
                    if (c.GetComponent(item) != null) kids.Add(MaNode(avatar, c, item, depth + 1));
                if (kids.Count > 0) d["children"] = kids;
            }
            return d;
        }

        // "Outfit=on", "Outfit/Jacket=off" for the object toggle on a menu item
        public static List<object> Toggles(Transform avatar, Transform t)
        {
            var o = new List<object>();
            Type toggle = Refl.Find(Refl.MA + "ModularAvatarObjectToggle");
            if (toggle == null) return o;
            foreach (Component c in t.GetComponents(toggle))
            {
                var so = new SerializedObject(c);
                SerializedProperty arr = so.FindProperty("m_objects");
                if (arr == null) continue;
                for (int i = 0; i < arr.arraySize && o.Count < 40; i++)
                {
                    SerializedProperty e = arr.GetArrayElementAtIndex(i);
                    SerializedProperty path = e.FindPropertyRelative("Object.referencePath");
                    SerializedProperty act = e.FindPropertyRelative("Active");
                    if (path == null || act == null) continue;
                    o.Add(path.stringValue + (act.boolValue ? "=on" : "=off"));
                }
            }
            return o;
        }

        // ---------- one object in detail ----------

        public static object ObjectDetail(Dictionary<string, object> args)
        {
            Component avatar = Refl.FindAvatar(J.Str(args, "avatar"));
            Transform t = Refl.FindUnder(avatar.transform, J.Str(args, "path"));
            return Describe(avatar.transform, t);
        }

        // the meshes under an object: what there is to switch on and off
        public static Dictionary<string, object> Describe(Transform avatar, Transform t)
        {
            var d = new Dictionary<string, object>();
            d["object"] = Refl.RelPath(avatar, t);
            d["active"] = t.gameObject.activeSelf;
            d["components"] = ComponentNames(t.gameObject);
            float baseY = avatar.position.y;
            var meshes = new List<object>();
            foreach (Renderer r in t.GetComponentsInChildren<Renderer>(true))
            {
                if (r is ParticleSystemRenderer || r is TrailRenderer || r is LineRenderer) continue;
                if (meshes.Count >= 120) { d["more"] = true; break; }
                var m = new Dictionary<string, object>();
                m["path"] = Refl.RelPath(avatar, r.transform);
                m["active"] = r.gameObject.activeInHierarchy && r.enabled;
                var mats = new List<object>();
                foreach (Material mat in r.sharedMaterials)
                    if (mat != null && mats.Count < 6 && !mats.Contains(mat.name)) mats.Add(mat.name);
                m["materials"] = mats;
                Bounds b = Refl.TightBounds(r);
                if (b.size.sqrMagnitude > 0)
                    m["height"] = new List<object> { Math.Round(b.min.y - baseY, 2), Math.Round(b.max.y - baseY, 2) };
                var smr = r as SkinnedMeshRenderer;
                if (smr != null && smr.sharedMesh != null)
                {
                    long idx = 0;
                    for (int s = 0; s < smr.sharedMesh.subMeshCount; s++) idx += smr.sharedMesh.GetIndexCount(s);
                    m["triangles"] = idx / 3;
                    if (smr.sharedMesh.blendShapeCount > 0) m["blendShapes"] = smr.sharedMesh.blendShapeCount;
                }
                meshes.Add(m);
            }
            d["meshes"] = meshes;
            return d;
        }

        // ---------- prefabs in folders ----------

        public static object Prefabs(Dictionary<string, object> args)
        {
            var folders = new List<string>();
            var skipped = new List<object>();
            foreach (string f in J.Strs(args, "folders"))
            {
                string p = f.Replace('\\', '/').TrimEnd('/');
                if (AssetDatabase.IsValidFolder(p)) folders.Add(p);
                else skipped.Add(p);
            }
            var o = new Dictionary<string, object>();
            if (skipped.Count > 0) o["notFound"] = skipped;
            var list = new List<object>();
            o["prefabs"] = list;
            if (folders.Count == 0) return o;
            int limit = (int)J.Num(args, "limit", 60);
            Type desc = Refl.DescriptorType;
            Type merge = Refl.Find(Refl.MA + "ModularAvatarMergeArmature");
            string[] guids = AssetDatabase.FindAssets("t:Prefab", folders.ToArray());
            o["total"] = guids.Length;
            foreach (string g in guids)
            {
                if (list.Count >= limit) break;
                string path = AssetDatabase.GUIDToAssetPath(g);
                var go = AssetDatabase.LoadAssetAtPath<GameObject>(path);
                if (go == null) continue;
                var d = new Dictionary<string, object>();
                d["path"] = path;
                d["name"] = go.name;
                bool isAvatar = desc != null && go.GetComponentInChildren(desc, true) != null;
                if (isAvatar) d["wholeAvatar"] = true; // the body comes with it: not something to dress with
                if (merge != null && go.GetComponentInChildren(merge, true) != null) d["modularAvatarReady"] = true;
                var names = new List<object>();
                Renderer[] rs = go.GetComponentsInChildren<Renderer>(true);
                foreach (Renderer r in rs) if (names.Count < 14) names.Add(r.name);
                d["renderers"] = rs.Length;
                d["meshNames"] = names;
                bool hips = false;
                foreach (Transform t in go.GetComponentsInChildren<Transform>(true))
                    if (t.name.IndexOf("hips", StringComparison.OrdinalIgnoreCase) >= 0) { hips = true; break; }
                d["hasArmature"] = hips;
                list.Add(d);
            }
            return o;
        }
    }
}
