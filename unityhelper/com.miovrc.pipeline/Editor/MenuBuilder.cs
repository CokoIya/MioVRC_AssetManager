// Building the avatar's menu out of Modular Avatar components, the way the plan from the program says.
//
// Under the avatar there is one object for the menu (by default "Avatar Menu": MA Menu Installer + MA Menu Group);
// below it one object per menu entry (MA Menu Item), sub menus as objects with children. Four kinds of entry:
//   outfit  picks one of a group: a Toggle on a shared Int parameter ("parameter", by default the plan's; outfits
//           share Clothtoggle, hair styles share Hair_Choose), each with its own value. Its MA Object Toggle turns
//           this entry's objects on and every other entry's objects (on the same parameter) off, so only one is worn.
//           With "materialsFrom" (another colour of the same outfit, as a prefab) it also carries an MA Material
//           Setter that puts that prefab's materials on, so several colours share one set of meshes.
//   part    hides (or with "show": shows) some objects of an outfit: a Toggle with a parameter of its own.
//   toggle  an on/off switch of its own for something independent (a prop, an accessory): selected = shown.
//           "default" says whether it is shown at first (an accessory) or hidden (a prop).
//   strip   takes every outfit of a group off.
// Entries that are there already (same place, same name) are updated, so running a plan twice changes nothing more,
// and adding an outfit later also teaches the older entries to turn the new one off.
using System;
using System.Collections.Generic;
using UnityEditor;
using UnityEngine;

namespace MioVRCA.Pipeline
{
    internal static class MenuBuilder
    {
        class Ctx
        {
            public Transform avatar;
            public Type item, installer, group, toggle, setter;
            public string parameter;
            public List<object> warnings = new List<object>();
            public List<object> created = new List<object>();
            public HashSet<Transform> touchedMenus = new HashSet<Transform>();
            public int newBits;
            public bool toggleInverse; // this Modular Avatar's Object Toggle can act when the item is NOT selected
        }

        public static object Build(Dictionary<string, object> args)
        {
            Component desc = Refl.FindAvatar(J.Str(args, "avatar"));
            var c = new Ctx();
            c.avatar = desc.transform;
            c.item = Refl.MAType("ModularAvatarMenuItem");
            c.installer = Refl.MAType("ModularAvatarMenuInstaller");
            c.group = Refl.MAType("ModularAvatarMenuGroup");
            c.toggle = Refl.MAType("ModularAvatarObjectToggle");
            c.setter = Refl.Find(Refl.MA + "ModularAvatarMaterialSetter");
            c.parameter = J.Str(args, "parameter", "Clothtoggle");
            if (c.parameter == "") c.parameter = "Clothtoggle";
            CheckSchema(c);

            List<object> items = J.Arr(J.Get(args, "items"));
            if (items.Count == 0) throw new PipelineException("计划中没有菜单项（items 为空）");
            // everything is looked up before anything is changed: a plan with a wrong path changes nothing
            var plan = new List<Entry>();
            foreach (object o in items) plan.Add(ReadEntry(c, J.Obj(o)));

            string backup = Bridge.BackupScene(c.avatar.gameObject);
            // a step of its own in the undo history; a plan that fails half way is taken back whole
            Undo.IncrementCurrentGroup();
            Undo.SetCurrentGroupName("MioVRCA 生成菜单");
            int undoGroup = Undo.GetCurrentGroup();
            Transform root;
            var made = new List<KeyValuePair<Entry, Transform>>();
            // the parameters this plan picks on (outfits on Clothtoggle, hair on Hair_Choose, ...), in plan order
            var groups = new List<string>();
            foreach (Entry e in plan)
                if ((e.kind == "outfit" || e.kind == "strip") && !groups.Contains(e.parameter)) groups.Add(e.parameter);
            try
            {
                root = EnsureRoot(c, J.Str(args, "root", "Avatar Menu"));
                // the objects each group's menu knew before this plan
                var before = new Dictionary<string, HashSet<Transform>>();
                var usedValues = new Dictionary<string, HashSet<int>>();
                foreach (string g in groups)
                {
                    before[g] = new HashSet<Transform>();
                    foreach (Transform t in SelectItems(c, g))
                        foreach (Transform o in OwnObjects(c, t)) before[g].Add(o);
                    usedValues[g] = UsedValues(c, g);
                }

                var defaults = new Dictionary<string, List<Transform>>();
                foreach (string g in groups) defaults[g] = new List<Transform>();
                foreach (Entry e in plan)
                {
                    Transform parent = EnsurePath(c, root, e.path);
                    Transform t = EnsureItem(c, parent, e, e.kind == "outfit" ? usedValues[e.parameter] : null);
                    made.Add(new KeyValuePair<Entry, Transform>(e, t));
                    if (e.kind == "outfit" && e.isDefault) defaults[e.parameter].Add(t);
                }

                foreach (string g in groups)
                {
                    string what = g == "Hair_Choose" ? "头发" : "衣服";
                    // only one at a time: every picker of the group turns the others' objects off
                    var all = new List<Transform>();
                    foreach (Transform t in SelectItems(c, g))
                        foreach (Transform o in OwnObjects(c, t)) if (!all.Contains(o)) all.Add(o);
                    foreach (Transform t in SelectItems(c, g))
                    {
                        var own = OwnObjects(c, t);
                        foreach (Transform o in all) if (!own.Contains(o)) AddToggle(c, t, o, false, false);
                    }
                    foreach (Transform t in c.avatar.GetComponentsInChildren<Transform>(true))
                    {
                        if (t.GetComponent(c.item) == null || t.GetComponent(c.toggle) == null) continue;
                        if (IsStrip(c, t, g, before[g], made)) foreach (Transform o in all) AddToggle(c, t, o, false, false);
                    }

                    var defs = defaults[g];
                    if (defs.Count > 1) c.warnings.Add("计划中有 " + defs.Count + " 个默认" + what + "（" + g + "），仅采用最后一个");
                    // the scene shows what a visitor sees first: the default one on, the others off
                    Transform def = null;
                    if (defs.Count > 0)
                    {
                        def = defs[defs.Count - 1];
                        foreach (Transform t in ParamItems(c, g)) SetBool(t.GetComponent(c.item), "isDefault", t == def);
                    }
                    else
                    {
                        foreach (Transform t in ParamItems(c, g))
                            if (new SerializedObject(t.GetComponent(c.item)).FindProperty("isDefault").boolValue) def = t;
                    }
                    if (def != null)
                    {
                        var own = OwnObjects(c, def);
                        foreach (Transform o in all)
                        {
                            // without a new default only what this plan brought is put in line; the rest stays as the player has it
                            if (defs.Count == 0 && before[g].Contains(o)) continue;
                            SetActive(o, own.Contains(o), "MioVRCA 默认" + what);
                        }
                    }
                    else if (all.Count > 0)
                        c.warnings.Add("尚未指定默认" + what + "（" + g + "），进入游戏时将保持场景中当前的显示状态，可将常用的一件设为默认（default）");
                }

                // an independent switch: the scene shows its first state
                foreach (var m in made)
                    if (m.Key.kind == "toggle")
                        foreach (Transform o in m.Key.objects) SetActive(o, m.Key.isDefault, "MioVRCA 开关默认");
            }
            catch (Exception)
            {
                Undo.RevertAllInCurrentGroup();
                throw;
            }

            var res = new Dictionary<string, object>();
            if (J.Get(args, "icons") is bool && !J.Bool(args, "icons")) res["icons"] = 0;
            else res["icons"] = Icons(c, root, made);

            foreach (Transform m in c.touchedMenus)
            {
                int n = 0;
                foreach (Transform k in m) if (k.GetComponent(c.item) != null) n++;
                if (n > 8) c.warnings.Add("「" + m.name + "」中有 " + n + " 项，超过每页 8 项的上限，游戏中将自动出现「More」翻页，可再分一层");
            }
            int bits = CurrentBits(desc);
            if (bits >= 0)
            {
                res["parameterBitsBefore"] = bits;
                res["parameterBitsAdded"] = c.newBits;
                if (bits + c.newBits > 256) c.warnings.Add("同步参数约 " + (bits + c.newBits) + " / 256 位，超出上限将导致上传失败（如已安装 VRCFury，或可压缩至上限内），可减少部件开关的数量");
            }
            Undo.CollapseUndoOperations(undoGroup);
            res["root"] = Refl.RelPath(c.avatar, root);
            res["parameter"] = c.parameter;
            res["parameters"] = new List<object>(groups.ToArray());
            res["created"] = c.created;
            res["warnings"] = c.warnings;
            if (backup != "") res["sceneBackup"] = backup;
            res["menu"] = AvatarInspect.Overview(new Dictionary<string, object> { { "avatar", Refl.ScenePath(c.avatar) } });
            res["note"] = "场景没有保存；Ctrl+Z 可以撤销。菜单效果要在 Play 模式里用 Gesture Manager 看";
            return res;
        }

        class Entry
        {
            public string kind, label, parameter;
            public List<string> path = new List<string>();
            public List<Transform> objects = new List<Transform>();
            public bool isDefault, show;
            public string materialsFrom = "";
            public List<Swap> swaps = new List<Swap>();
        }

        static readonly string[] Kinds = { "outfit", "part", "toggle", "strip" };

        // one material slot of a renderer that another colour changes
        internal class Swap
        {
            public Renderer renderer;
            public int index;
            public Material material;
        }

        static Entry ReadEntry(Ctx c, Dictionary<string, object> d)
        {
            var e = new Entry();
            e.kind = J.Str(d, "kind").Trim();
            if (e.kind == "pick" || e.kind == "hair") e.kind = "outfit";
            e.label = J.Str(d, "label").Trim();
            if (Array.IndexOf(Kinds, e.kind) < 0) throw new PipelineException("菜单项的 kind 只能是 outfit、part、toggle、strip，当前为「" + e.kind + "」");
            if (e.label == "") throw new PipelineException("有菜单项缺少名称（label）");
            e.parameter = J.Str(d, "parameter", c.parameter).Trim();
            if (e.parameter == "") e.parameter = c.parameter;
            foreach (char ch in e.parameter)
                if (char.IsWhiteSpace(ch) || ch == '"' || ch == '\'') throw new PipelineException("「" + e.label + "」的参数名「" + e.parameter + "」不能包含空格或引号");
            foreach (string s in J.Strs(d, "path"))
            {
                string n = s.Trim();
                if (n != "" && n != "主菜单") e.path.Add(n);
            }
            foreach (string p in J.Strs(d, "objects"))
            {
                Transform t = Refl.FindUnder(c.avatar, p);
                if (t == c.avatar) throw new PipelineException("不能将模型本身作为衣服开关");
                if (!e.objects.Contains(t)) e.objects.Add(t);
            }
            if (e.kind != "strip" && e.objects.Count == 0) throw new PipelineException("「" + e.label + "」未指定所控制的物体（objects）");
            e.isDefault = J.Bool(d, "default");
            e.show = J.Bool(d, "show");
            if (e.kind == "toggle" && e.isDefault && !c.toggleInverse)
                throw new PipelineException("「" + e.label + "」需默认显示，但当前版本 Modular Avatar 的 Object Toggle 不支持「反转条件」（需要 1.10 以上），可升级 Modular Avatar，或改为默认隐藏的开关");
            e.materialsFrom = J.Str(d, "materialsFrom").Replace('\\', '/').Trim();
            if (e.materialsFrom != "")
            {
                if (e.kind != "outfit") throw new PipelineException("「" + e.label + "」：仅衣服（outfit）可使用 materialsFrom");
                if (c.setter == null) throw new PipelineException("当前版本的 Modular Avatar 不含 Material Setter（需要 1.12 以上），无法生成共用模型的配色");
                e.swaps = ReadSwaps(e);
            }
            return e;
        }

        // The materials another colour of the same outfit wears, slot by slot, where they differ from what the
        // dressed outfit has now. Renderers are matched by their path below the outfit, then by name.
        static List<Swap> ReadSwaps(Entry e)
        {
            var asset = AssetDatabase.LoadAssetAtPath<GameObject>(e.materialsFrom);
            if (asset == null) throw new PipelineException("「" + e.label + "」的配色 prefab 不在工程中：" + e.materialsFrom);
            var o = new List<Swap>();
            int matched = 0;
            foreach (Transform outfit in e.objects)
            {
                var byPath = new Dictionary<string, Renderer>();
                var byName = new Dictionary<string, Renderer>();
                var twice = new HashSet<string>();
                foreach (Renderer r in outfit.GetComponentsInChildren<Renderer>(true))
                {
                    byPath[Refl.RelPath(outfit, r.transform)] = r;
                    if (byName.ContainsKey(r.name)) twice.Add(r.name); else byName[r.name] = r;
                }
                foreach (Renderer src in asset.GetComponentsInChildren<Renderer>(true))
                {
                    Renderer dst;
                    if (!byPath.TryGetValue(Refl.RelPath(asset.transform, src.transform), out dst) && (twice.Contains(src.name) || !byName.TryGetValue(src.name, out dst))) continue;
                    matched++;
                    Material[] want = src.sharedMaterials, have = dst.sharedMaterials;
                    for (int i = 0; i < want.Length && i < have.Length; i++)
                        if (want[i] != null && want[i] != have[i]) o.Add(new Swap { renderer = dst, index = i, material = want[i] });
                }
            }
            if (matched == 0) throw new PipelineException("「" + e.label + "」：配色 prefab「" + asset.name + "」与已装配的衣服不匹配（没有同名网格），两者不是同一件衣服的不同配色");
            return o;
        }

        static void WriteSwaps(Ctx c, Transform item, Entry e)
        {
            Component ms = item.GetComponent(c.setter);
            if (e.swaps.Count == 0)
            {
                if (ms == null) c.warnings.Add("「" + e.label + "」的配色 prefab 与已装配的衣服材质相同，无需替换");
                return;
            }
            if (ms == null) ms = Undo.AddComponent(item.gameObject, c.setter);
            var so = new SerializedObject(ms);
            SerializedProperty arr = Refl.Prop(so, "m_objects");
            arr.arraySize = e.swaps.Count;
            for (int i = 0; i < e.swaps.Count; i++)
            {
                SerializedProperty n = arr.GetArrayElementAtIndex(i);
                SerializedProperty rp = n.FindPropertyRelative("Object.referencePath");
                SerializedProperty obj = n.FindPropertyRelative("Object.targetObject");
                SerializedProperty mat = n.FindPropertyRelative("Material");
                SerializedProperty idx = n.FindPropertyRelative("MaterialIndex");
                if (rp == null || mat == null || idx == null) throw new PipelineException("当前版本的 MA Material Setter 结构不兼容，Modular Avatar 版本可能过旧");
                rp.stringValue = Refl.RelPath(c.avatar, e.swaps[i].renderer.transform);
                if (obj != null) obj.objectReferenceValue = e.swaps[i].renderer.gameObject;
                mat.objectReferenceValue = e.swaps[i].material;
                idx.intValue = e.swaps[i].index;
            }
            so.ApplyModifiedProperties();
        }

        // the fields this code writes must exist in the installed Modular Avatar
        static void CheckSchema(Ctx c)
        {
            var go = new GameObject("MioVRCA schema check") { hideFlags = HideFlags.HideAndDontSave };
            try
            {
                var so = new SerializedObject(go.AddComponent(c.item));
                foreach (string f in new[] { "Control.type", "Control.parameter.name", "Control.value", "Control.icon", "MenuSource", "isSynced", "isSaved", "isDefault", "automaticValue", "label" })
                    Refl.Prop(so, f);
                var st = new SerializedObject(go.AddComponent(c.toggle));
                Refl.Prop(st, "m_objects");
                c.toggleInverse = st.FindProperty("m_inverted") != null;
            }
            finally
            {
                UnityEngine.Object.DestroyImmediate(go);
            }
        }

        static Transform EnsureRoot(Ctx c, string name)
        {
            if (name == "") name = "Avatar Menu";
            Transform root = c.avatar.Find(name);
            if (root == null)
            {
                var go = new GameObject(name);
                Undo.RegisterCreatedObjectUndo(go, "MioVRCA 菜单");
                go.transform.SetParent(c.avatar, false);
                root = go.transform;
                c.created.Add(name);
            }
            // a menu of its own already (a sub menu item, or an installer): used as it is
            if (root.GetComponent(c.item) == null && root.GetComponent(c.installer) == null) Undo.AddComponent(root.gameObject, c.installer);
            if (root.GetComponent(c.item) == null && root.GetComponent(c.group) == null) Undo.AddComponent(root.gameObject, c.group);
            c.touchedMenus.Add(root);
            return root;
        }

        static string Label(Ctx c, Transform t)
        {
            Component mi = t.GetComponent(c.item);
            if (mi == null) return t.name;
            SerializedProperty p = new SerializedObject(mi).FindProperty("label");
            return p != null && p.stringValue != "" ? p.stringValue : t.name;
        }

        static Transform FindEntry(Ctx c, Transform parent, string label)
        {
            foreach (Transform k in parent)
                if (k.GetComponent(c.item) != null && (Label(c, k) == label || k.name == label)) return k;
            return null;
        }

        static Transform EnsurePath(Ctx c, Transform root, List<string> path)
        {
            Transform at = root;
            foreach (string level in path)
            {
                Transform next = FindEntry(c, at, level);
                if (next == null)
                {
                    next = NewItem(c, at, level);
                    var so = new SerializedObject(next.GetComponent(c.item));
                    Refl.SetEnum(so, "Control.type", "SubMenu");
                    Refl.SetEnum(so, "MenuSource", "Children");
                    so.ApplyModifiedProperties();
                }
                else if (Refl.GetEnum(new SerializedObject(next.GetComponent(c.item)), "Control.type") != "SubMenu")
                    throw new PipelineException("「" + level + "」已是开关而非子菜单，无法在其中添加菜单项");
                at = next;
                c.touchedMenus.Add(at);
            }
            return at;
        }

        static Transform NewItem(Ctx c, Transform parent, string label)
        {
            var go = new GameObject(label);
            Undo.RegisterCreatedObjectUndo(go, "MioVRCA 菜单项");
            go.transform.SetParent(parent, false);
            Component mi = Undo.AddComponent(go, c.item);
            var so = new SerializedObject(mi);
            Refl.Prop(so, "label").stringValue = label;
            Refl.Prop(so, "isSynced").boolValue = true;
            Refl.Prop(so, "isSaved").boolValue = true;
            so.ApplyModifiedProperties();
            c.created.Add(Refl.RelPath(c.avatar, go.transform));
            return go.transform;
        }

        static Transform EnsureItem(Ctx c, Transform parent, Entry e, HashSet<int> usedValues)
        {
            Transform t = FindEntry(c, parent, e.label);
            bool isNew = t == null;
            if (isNew) t = NewItem(c, parent, e.label);
            Component mi = t.GetComponent(c.item);
            var so = new SerializedObject(mi);
            if (!isNew && Refl.GetEnum(so, "Control.type") == "SubMenu")
                throw new PipelineException("「" + e.label + "」已是子菜单，无法改为开关，请更换名称");
            Refl.SetEnum(so, "Control.type", "Toggle");
            if (e.kind == "outfit")
            {
                bool had = !isNew && Refl.Prop(so, "Control.parameter.name").stringValue == e.parameter && !Refl.Prop(so, "automaticValue").boolValue;
                if (!had)
                {
                    int v = 1;
                    while (usedValues.Contains(v)) v++;
                    if (usedValues.Count == 0) c.newBits += 8; // the shared Int parameter is new with the first entry
                    usedValues.Add(v);
                    Refl.Prop(so, "Control.parameter.name").stringValue = e.parameter;
                    Refl.Prop(so, "Control.value").floatValue = v;
                    Refl.Prop(so, "automaticValue").boolValue = false;
                }
            }
            else
            {
                if (isNew)
                {
                    Refl.Prop(so, "Control.parameter.name").stringValue = "";
                    Refl.Prop(so, "Control.value").floatValue = 1;
                    Refl.Prop(so, "automaticValue").boolValue = true;
                    c.newBits += 1;
                }
                if (e.kind == "toggle") Refl.Prop(so, "isDefault").boolValue = e.isDefault;
            }
            so.ApplyModifiedProperties();

            if (t.GetComponent(c.toggle) == null) Undo.AddComponent(t.gameObject, c.toggle);
            if (e.kind == "outfit")
            {
                foreach (Transform o in e.objects) AddToggle(c, t, o, true, true);
                if (e.materialsFrom != "") WriteSwaps(c, t, e);
            }
            else if (e.kind == "part") foreach (Transform o in e.objects) AddToggle(c, t, o, e.show, true);
            else if (e.kind == "toggle")
            {
                // shown at first: the item is on by default and its toggle acts when it is NOT selected, turning the
                // objects off (the scene keeps them on, so the editor shows them). Hidden at first: the usual way round.
                SetInverse(c, t, e.isDefault);
                foreach (Transform o in e.objects) AddToggle(c, t, o, !e.isDefault, true);
            }
            return t;
        }

        static void SetInverse(Ctx c, Transform item, bool inverse)
        {
            var so = new SerializedObject(item.GetComponent(c.toggle));
            SerializedProperty p = so.FindProperty("m_inverted");
            if (p == null) return;
            if (p.boolValue != inverse) { p.boolValue = inverse; so.ApplyModifiedProperties(); }
        }

        // one line of an object toggle; replace: change a line that is there, else leave it as the player set it
        static void AddToggle(Ctx c, Transform item, Transform target, bool active, bool replace)
        {
            Component tg = item.GetComponent(c.toggle);
            var so = new SerializedObject(tg);
            SerializedProperty arr = Refl.Prop(so, "m_objects");
            string path = Refl.RelPath(c.avatar, target);
            for (int i = 0; i < arr.arraySize; i++)
            {
                SerializedProperty e = arr.GetArrayElementAtIndex(i);
                SerializedProperty obj = e.FindPropertyRelative("Object.targetObject");
                SerializedProperty rp = e.FindPropertyRelative("Object.referencePath");
                bool same = (obj != null && obj.objectReferenceValue == target.gameObject) || (rp != null && rp.stringValue == path);
                if (!same) continue;
                if (replace)
                {
                    e.FindPropertyRelative("Active").boolValue = active;
                    if (rp != null) rp.stringValue = path;
                    if (obj != null) obj.objectReferenceValue = target.gameObject;
                    so.ApplyModifiedProperties();
                }
                return;
            }
            arr.arraySize++;
            SerializedProperty n = arr.GetArrayElementAtIndex(arr.arraySize - 1);
            SerializedProperty nrp = n.FindPropertyRelative("Object.referencePath");
            SerializedProperty nobj = n.FindPropertyRelative("Object.targetObject");
            SerializedProperty nact = n.FindPropertyRelative("Active");
            if (nrp == null || nact == null) throw new PipelineException("当前版本的 MA Object Toggle 结构不兼容，Modular Avatar 版本可能过旧（需要 1.12 以上）");
            nrp.stringValue = path;
            if (nobj != null) nobj.objectReferenceValue = target.gameObject;
            nact.boolValue = active;
            so.ApplyModifiedProperties();
        }

        // every menu item of the avatar on a shared parameter, wherever it sits: its value is taken
        static List<Transform> ParamItems(Ctx c, string parameter)
        {
            var o = new List<Transform>();
            foreach (Transform t in c.avatar.GetComponentsInChildren<Transform>(true))
            {
                Component mi = t.GetComponent(c.item);
                if (mi == null) continue;
                var so = new SerializedObject(mi);
                string type = Refl.GetEnum(so, "Control.type");
                if (type != "Toggle" && type != "Button") continue;
                if (so.FindProperty("Control.parameter.name").stringValue != parameter) continue;
                if (so.FindProperty("automaticValue").boolValue) continue;
                o.Add(t);
            }
            return o;
        }

        // those of them that pick one by turning objects on and off
        static List<Transform> SelectItems(Ctx c, string parameter)
        {
            return ParamItems(c, parameter).FindAll(t => t.GetComponent(c.toggle) != null);
        }

        static HashSet<int> UsedValues(Ctx c, string parameter)
        {
            var o = new HashSet<int>();
            foreach (Transform t in ParamItems(c, parameter))
                o.Add(Mathf.RoundToInt(new SerializedObject(t.GetComponent(c.item)).FindProperty("Control.value").floatValue));
            return o;
        }

        static void SetActive(Transform o, bool on, string why)
        {
            if (o.gameObject.activeSelf == on) return;
            Undo.RecordObject(o.gameObject, why);
            o.gameObject.SetActive(on);
            PrefabUtility.RecordPrefabInstancePropertyModifications(o.gameObject);
        }

        // the objects an outfit picker turns on
        static List<Transform> OwnObjects(Ctx c, Transform item)
        {
            var o = new List<Transform>();
            foreach (Component tg in item.GetComponents(c.toggle))
            {
                SerializedProperty arr = new SerializedObject(tg).FindProperty("m_objects");
                if (arr == null) continue;
                for (int i = 0; i < arr.arraySize; i++)
                {
                    SerializedProperty e = arr.GetArrayElementAtIndex(i);
                    if (!e.FindPropertyRelative("Active").boolValue) continue;
                    Transform t = Resolve(c, e);
                    if (t != null && !o.Contains(t)) o.Add(t);
                }
            }
            return o;
        }

        static Transform Resolve(Ctx c, SerializedProperty entry)
        {
            SerializedProperty obj = entry.FindPropertyRelative("Object.targetObject");
            if (obj != null && obj.objectReferenceValue is GameObject) return ((GameObject)obj.objectReferenceValue).transform;
            SerializedProperty rp = entry.FindPropertyRelative("Object.referencePath");
            return rp != null && rp.stringValue != "" ? c.avatar.Find(rp.stringValue) : null;
        }

        // "take everything off": named so in this plan, or an older one that turns off every outfit of the group known so far
        static bool IsStrip(Ctx c, Transform t, string group, HashSet<Transform> before, List<KeyValuePair<Entry, Transform>> made)
        {
            foreach (var m in made) if (m.Value == t) return m.Key.kind == "strip" && m.Key.parameter == group;
            if (before.Count < 2) return false;
            var so = new SerializedObject(t.GetComponent(c.item));
            if (!so.FindProperty("automaticValue").boolValue) return false;
            var tg = new SerializedObject(t.GetComponent(c.toggle));
            SerializedProperty inv = tg.FindProperty("m_inverted");
            if (inv != null && inv.boolValue) return false;
            var off = new HashSet<Transform>();
            SerializedProperty arr = tg.FindProperty("m_objects");
            for (int i = 0; i < arr.arraySize; i++)
            {
                SerializedProperty e = arr.GetArrayElementAtIndex(i);
                if (e.FindPropertyRelative("Active").boolValue) return false;
                Transform o = Resolve(c, e);
                if (o != null) off.Add(o);
            }
            foreach (Transform o in before) if (!off.Contains(o)) return false;
            return true;
        }

        static void SetBool(Component mi, string field, bool v)
        {
            var so = new SerializedObject(mi);
            SerializedProperty p = Refl.Prop(so, field);
            if (p.boolValue == v) return;
            p.boolValue = v;
            so.ApplyModifiedProperties();
        }

        static int CurrentBits(Component desc)
        {
            var f = desc.GetType().GetField("expressionParameters");
            var pars = f != null ? f.GetValue(desc) as UnityEngine.Object : null;
            if (pars == null) return -1;
            var cost = pars.GetType().GetMethod("CalcTotalCost", Type.EmptyTypes);
            return cost != null ? Convert.ToInt32(cost.Invoke(pars, null)) : -1;
        }

        // a picture for every entry of this plan that has none, and for the outfit's sub menu
        static int Icons(Ctx c, Transform root, List<KeyValuePair<Entry, Transform>> made)
        {
            int n = 0;
            string folder = "Assets/MioVRCA/" + IconShot.SafeName(c.avatar.name) + "/Icons";
            foreach (var m in made)
            {
                Entry e = m.Key;
                if (e.kind == "strip" || e.objects.Count == 0) continue;
                var so = new SerializedObject(m.Value.GetComponent(c.item));
                SerializedProperty icon = Refl.Prop(so, "Control.icon");
                if (icon.objectReferenceValue != null) continue;
                Transform parent = m.Value.parent;
                // named after the whole way down the menu: two outfits may both have a "配色 / 黑"
                string name = e.label;
                for (Transform up = parent; up != null && up != root; up = up.parent) name = Label(c, up) + "_" + name;
                if (name.Length > 56) name = name.Substring(name.Length - 56);
                string file = folder + "/" + IconShot.SafeName(name) + "_icon.png";
                Texture2D tex;
                try { tex = IconShot.Capture(c.avatar, e.objects, file, e.swaps); }
                catch (Exception ex) { c.warnings.Add("「" + e.label + "」的图标生成失败：" + ex.Message); continue; }
                if (tex == null) continue;
                icon.objectReferenceValue = tex;
                so.ApplyModifiedProperties();
                n++;
                // the sub menu of an outfit (or of a prop) shows the same picture
                if ((e.kind == "outfit" || e.kind == "toggle") && parent != root)
                {
                    var ps = new SerializedObject(parent.GetComponent(c.item));
                    SerializedProperty pi = Refl.Prop(ps, "Control.icon");
                    if (pi.objectReferenceValue == null) { pi.objectReferenceValue = tex; ps.ApplyModifiedProperties(); }
                }
            }
            return n;
        }
    }
}
