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

            // what a plan is decided by comes first and the long lists last: an answer that has to be cut short
            // loses the end
            ParamBudget bits = Bits(desc);
            a["parameterBits"] = bits.total;
            if (bits.asset >= 0) a["parameterBitsAsset"] = bits.asset;
            if (bits.fury) a["parameterBitsAtLeast"] = true; // VRCFury adds parameters nobody can count before the build
            int bones = PhysBones(root);
            if (bones >= 0) a["physBones"] = bones;
            if (FaceTracking(root)) a["faceTracking"] = true;
            // the menu and the parameters the avatar descriptor points at
            object menu = Field(desc, "expressionsMenu");
            object pars = Field(desc, "expressionParameters");
            a["menuAsset"] = menu is UnityEngine.Object && (UnityEngine.Object)menu != null ? AssetDatabase.GetAssetPath((UnityEngine.Object)menu) : "";
            a["parametersAsset"] = pars is UnityEngine.Object && (UnityEngine.Object)pars != null ? AssetDatabase.GetAssetPath((UnityEngine.Object)pars) : "";
            a["maMenu"] = MaMenu(root);
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
                int pb = PhysBones(c);
                if (pb > 0) k["physBones"] = pb;
                string pf = Refl.PrefabSource(c.gameObject);
                if (pf != "") k["prefab"] = pf;
                kids.Add(k);
            }
            a["children"] = kids;
            return a;
        }

        // VRChat allows 256 PhysBone components on an avatar and counts those on hidden objects too:
        // every colour of an outfit that is worn as a model of its own adds its bones again. What sits under
        // an EditorOnly object is taken out before the upload and does not count.
        public const int PhysBoneLimit = 256;

        public static int PhysBones(Transform under)
        {
            Type t = Refl.Find("VRC.SDK3.Dynamics.PhysBone.Components.VRCPhysBone");
            if (t == null) return -1;
            int n = 0;
            foreach (Component c in under.GetComponentsInChildren(t, true))
                if (c != null && !Refl.EditorOnly(c.transform)) n++;
            return n;
        }

        // ---------- what a plugin prefab is ----------

        // Face tracking under an object: a component of a face-tracking package (its type's name says so), or MA
        // Parameters declaring what VRCFaceTracking sends ("FT/v2/…", the older "v2/…").
        public static bool FaceTracking(Transform under)
        {
            foreach (Component c in under.GetComponentsInChildren<Component>(true))
            {
                if (c == null) continue;
                string n = c.GetType().FullName ?? "";
                if (n.IndexOf("FaceTracking", StringComparison.OrdinalIgnoreCase) >= 0) return true;
            }
            Type mp = Refl.Find(Refl.MA + "ModularAvatarParameters");
            if (mp == null) return false;
            foreach (Component c in under.GetComponentsInChildren(mp, true))
            {
                if (c == null) continue;
                SerializedProperty arr = new SerializedObject(c).FindProperty("parameters");
                if (arr == null || !arr.isArray) continue;
                for (int i = 0; i < arr.arraySize; i++)
                {
                    string name = RelStr(arr.GetArrayElementAtIndex(i), "nameOrPrefix");
                    if (name.StartsWith("FT/", StringComparison.Ordinal) || name.StartsWith("v2/", StringComparison.Ordinal)) return true;
                }
            }
            return false;
        }

        // The scripts on a prefab (or an object), by short name, and how many of them are missing: a prefab whose
        // scripts are missing needs a package the project does not have. selfInstalling: it brings what installs
        // it when the avatar is built — a Modular Avatar menu, animator or parameters, VRCFury, or a build-time
        // component of a package of its own (those are marked IEditorOnly).
        public static List<object> Scripts(Transform under, out int missing, out bool selfInstalling, out bool fury)
        {
            var names = new List<object>();
            missing = 0;
            selfInstalling = fury = false;
            Type editorOnly = Refl.Find("VRC.SDKBase.IEditorOnly");
            foreach (Component c in under.GetComponentsInChildren<Component>(true))
            {
                if (c == null) { missing++; continue; }
                if (!(c is MonoBehaviour)) continue;
                Type t = c.GetType();
                string full = t.FullName ?? t.Name;
                string n = t.Name.Replace("ModularAvatar", "MA ");
                if (full.StartsWith("VF.") || t.Name.StartsWith("VRCFury")) { fury = selfInstalling = true; n = "VRCFury"; }
                else if (full.StartsWith(Refl.MA))
                {
                    if (t.Name == "ModularAvatarMenuInstaller" || t.Name == "ModularAvatarMergeAnimator" || t.Name == "ModularAvatarParameters" || t.Name == "ModularAvatarMenuItem")
                        selfInstalling = true;
                }
                else if (editorOnly != null && editorOnly.IsAssignableFrom(t) && !full.StartsWith("VRC.")) selfInstalling = true;
                if (!names.Contains(n) && names.Count < 16) names.Add(n);
            }
            return names;
        }

        // ---------- synced parameters ----------

        // VRChat syncs 256 bits of expression parameters per avatar: a Bool costs 1, an Int or a Float 8.
        public const int ParameterBitLimit = 256;

        internal class ParamBudget
        {
            public int asset = -1; // what the descriptor's parameter asset costs as it is (-1: the avatar has none)
            public int added;      // what Modular Avatar adds when the avatar is built
            public int total;
            public bool fury;      // VRCFury components are there: they add parameters too, which cannot be told here
        }

        // The synced bits the avatar will have once it is built: the descriptor's asset, plus the parameters
        // Modular Avatar creates at build time — the entries of MA Parameters components and the parameters of
        // MA Menu Items that the asset does not declare. An estimate: it follows what Modular Avatar 1.10–1.13
        // does, and leaves out what cannot be read. It never fails.
        public static ParamBudget Bits(Component desc)
        {
            var b = new ParamBudget();
            var declared = new HashSet<string>(); // names somebody declares: a menu item on one of them adds nothing
            var scoped = new HashSet<string>();   // names an MA Parameters component keeps to its own objects
            try
            {
                object pars = Field(desc, "expressionParameters");
                if (pars is UnityEngine.Object && (UnityEngine.Object)pars != null)
                {
                    MethodInfo cost = pars.GetType().GetMethod("CalcTotalCost", Type.EmptyTypes);
                    if (cost != null) b.asset = Convert.ToInt32(cost.Invoke(pars, null));
                    var list = Field(pars, "parameters") as IEnumerable;
                    if (list != null)
                        foreach (object p in list)
                        {
                            string n = p != null ? Convert.ToString(Field(p, "name")) : "";
                            if (!string.IsNullOrEmpty(n)) declared.Add(n);
                        }
                }
            }
            catch (Exception) { }
            try { b.added = MaParameterBits(desc.transform, declared, scoped); } catch (Exception) { }
            try { b.added += MaMenuItemBits(desc.transform, declared, scoped); } catch (Exception) { }
            try { b.fury = HasFury(desc.transform); } catch (Exception) { }
            b.total = Math.Max(b.asset, 0) + b.added;
            return b;
        }

        // MA Parameters: every entry with a sync type becomes an expression parameter when the avatar is built,
        // unless the asset has the name already. Entries marked local cost nothing; an internal one gets a name of
        // its own, so it never meets the asset's or another component's.
        static int MaParameterBits(Transform root, HashSet<string> declared, HashSet<string> scoped)
        {
            Type mp = Refl.Find(Refl.MA + "ModularAvatarParameters");
            if (mp == null) return 0;
            int bits = 0;
            foreach (Component c in root.GetComponentsInChildren(mp, true))
            {
                if (c == null || Refl.EditorOnly(c.transform)) continue;
                SerializedProperty arr = new SerializedObject(c).FindProperty("parameters");
                if (arr == null || !arr.isArray) continue;
                var own = new HashSet<string>();
                for (int i = 0; i < arr.arraySize; i++)
                {
                    SerializedProperty e = arr.GetArrayElementAtIndex(i);
                    string name = RelStr(e, "nameOrPrefix");
                    string sync = RelEnum(e, "syncType");
                    if (name == "" || RelBool(e, "isPrefix") || sync == "" || sync == "NotSynced") continue;
                    string remap = RelStr(e, "remapTo");
                    if (remap != "") name = remap;
                    if (RelBool(e, "internalParameter")) { scoped.Add(name); if (!own.Add(name)) continue; }
                    else if (!declared.Add(name)) continue;
                    if (!RelBool(e, "localOnly")) bits += sync == "Bool" ? 1 : 8;
                }
            }
            return bits;
        }

        // MA Menu Items: Modular Avatar makes a parameter for every name its items use that nobody declares,
        // and one for each Toggle or Button without a name that an Object Toggle (or another reactive component)
        // hangs on. Items that share a name share the parameter.
        static int MaMenuItemBits(Transform root, HashSet<string> declared, HashSet<string> scoped)
        {
            Type item = Refl.Find(Refl.MA + "ModularAvatarMenuItem");
            if (item == null) return 0;
            var reactive = new HashSet<Transform>(); // the menu item nearest above each reactive component
            Type rc = Refl.Find(Refl.MA + "ReactiveComponent");
            if (rc != null)
                foreach (Component r in root.GetComponentsInChildren(rc, true))
                {
                    if (r == null) continue;
                    for (Transform t = r.transform; t != null; t = t == root ? null : t.parent)
                        if (t.GetComponent(item) != null) { reactive.Add(t); break; }
                }
            var groups = new Dictionary<string, List<SerializedObject>>();
            var order = new List<string>();
            foreach (Component mi in root.GetComponentsInChildren(item, true))
            {
                if (mi == null || Refl.EditorOnly(mi.transform)) continue;
                var so = new SerializedObject(mi);
                if (so.FindProperty("automaticValue") == null) return 0; // before 1.10 a menu item made no parameter
                SerializedProperty pn = so.FindProperty("Control.parameter.name");
                string name = pn != null ? pn.stringValue : "";
                if (string.IsNullOrWhiteSpace(name))
                {
                    string type = Refl.GetEnum(so, "Control.type");
                    if ((type != "Toggle" && type != "Button") || !reactive.Contains(mi.transform)) continue;
                    name = "\n" + mi.GetInstanceID(); // a parameter of its own
                }
                else if (declared.Contains(name) || scoped.Contains(name)) continue;
                if (!groups.ContainsKey(name)) { groups[name] = new List<SerializedObject>(); order.Add(name); }
                groups[name].Add(so);
            }
            int bits = 0;
            foreach (string name in order) bits += ItemBits(groups[name]);
            return bits;
        }

        // What the parameter of these items costs, the way Modular Avatar types it: items without a value of
        // their own are numbered from 1 past the values in use; a Bool as long as every value is 0 or 1, an Int
        // when one is higher, a Float when one is negative or not whole. Synced when any of the items is.
        static int ItemBits(List<SerializedObject> items)
        {
            bool synced = false, isInt = false, isFloat = false;
            var used = new HashSet<int>();
            int def = -1;
            foreach (SerializedObject so in items)
            {
                SerializedProperty sync = so.FindProperty("isSynced");
                synced |= sync == null || sync.boolValue;
                if (SoBool(so, "automaticValue")) continue;
                SerializedProperty pv = so.FindProperty("Control.value");
                float v = pv != null ? pv.floatValue : 1;
                used.Add((int)v);
                if (v < 0 || Math.Abs(v - Math.Round(v)) > 0.01) isFloat = true;
                else if (v > 1) isInt = true;
                if (def < 0 && SoBool(so, "isDefault")) def = (int)v;
            }
            if (items.Count == 1 && SoBool(items[0], "automaticValue") && SoBool(items[0], "isDefault")) def = 1;
            if (def >= 0) used.Add(def);
            else
                for (int i = 0; i < 256; i++)
                    if (used.Add(i)) { def = i; break; }
            int next = 1;
            foreach (SerializedObject so in items)
            {
                if (!SoBool(so, "automaticValue")) continue;
                int v = def;
                if (!SoBool(so, "isDefault"))
                {
                    while (used.Contains(next)) next++;
                    v = next;
                    used.Add(v);
                }
                if (v > 1) isInt = true;
            }
            if (!synced) return 0;
            return isInt || isFloat ? 8 : 1;
        }

        static bool SoBool(SerializedObject so, string path)
        {
            SerializedProperty p = so.FindProperty(path);
            return p != null && p.boolValue;
        }

        static string RelStr(SerializedProperty e, string name)
        {
            SerializedProperty p = e.FindPropertyRelative(name);
            return p != null && p.stringValue != null ? p.stringValue : "";
        }

        static bool RelBool(SerializedProperty e, string name)
        {
            SerializedProperty p = e.FindPropertyRelative(name);
            return p != null && p.boolValue;
        }

        static string RelEnum(SerializedProperty e, string name)
        {
            SerializedProperty p = e.FindPropertyRelative(name);
            if (p == null || p.enumNames == null || p.enumValueIndex < 0 || p.enumValueIndex >= p.enumNames.Length) return "";
            return p.enumNames[p.enumValueIndex];
        }

        public static bool HasFury(Transform root)
        {
            foreach (Component c in root.GetComponentsInChildren<Component>(true))
            {
                if (c == null) continue;
                Type t = c.GetType();
                if ((t.FullName ?? "").StartsWith("VF.") || t.Name.StartsWith("VRCFury")) return true;
            }
            return false;
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
                // "take everything off", as MenuBuilder marks it: the parameter whose outfits it takes off
                SerializedProperty mark = so.FindProperty("Control.name");
                if (mark != null && mark.stringValue.StartsWith(MenuBuilder.StripMark)) d["strip"] = mark.stringValue.Substring(MenuBuilder.StripMark.Length);
            }
            else
            {
                d["label"] = t.name;
                d["type"] = "MenuInstaller";
            }
            if (children && depth < 6)
            {
                var kids = new List<object>();
                Type target = Refl.Find(Refl.MA + "ModularAvatarMenuInstallTarget");
                foreach (Transform c in t)
                {
                    if (c.GetComponent(item) != null) kids.Add(MaNode(avatar, c, item, depth + 1));
                    else if (target != null && c.GetComponent(target) != null)
                    {
                        // a plugin's own menu, brought here: where it comes from
                        var k = new Dictionary<string, object> { { "object", Refl.RelPath(avatar, c) }, { "label", c.name }, { "type", "InstallTarget" } };
                        SerializedProperty ins = new SerializedObject(c.GetComponent(target)).FindProperty("installer");
                        var from = ins != null ? ins.objectReferenceValue as Component : null;
                        if (from != null && from.transform.IsChildOf(avatar)) k["installer"] = Refl.RelPath(avatar, from.transform);
                        kids.Add(k);
                    }
                }
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
                // what kind of thing it is, as far as its components say: a plugin that installs itself (its own
                // menu, animator or parameters), face tracking, something whose scripts the project lacks
                int missing;
                bool self, fury;
                List<object> scripts = Scripts(go.transform, out missing, out self, out fury);
                if (scripts.Count > 0) d["scripts"] = scripts;
                if (missing > 0) d["missingScripts"] = missing;
                if (self) d["selfInstalling"] = true;
                if (fury) d["vrcFury"] = true;
                Type installer = Refl.Find(Refl.MA + "ModularAvatarMenuInstaller");
                int menus = installer != null ? go.GetComponentsInChildren(installer, true).Length : 0;
                if (menus > 0) d["menuInstallers"] = menus;
                if (FaceTracking(go.transform)) d["faceTracking"] = true;
                list.Add(d);
            }
            return o;
        }
    }
}
