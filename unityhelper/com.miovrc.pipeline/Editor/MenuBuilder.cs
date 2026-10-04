// Building the avatar's menu out of Modular Avatar components, the way the plan from the program says.
//
// Under the avatar there is one object for the menu (by default "Avatar Menu": MA Menu Installer + MA Menu Group);
// below it one object per menu entry (MA Menu Item), sub menus as objects with children. The kinds of entry:
//   outfit  picks one of a group: a Toggle on a shared Int parameter ("parameter", by default the plan's; outfits
//           share Clothtoggle, hair styles share Hair_Choose), each with its own value. Its MA Object Toggle turns
//           this entry's objects on and every other entry's objects (on the same parameter) off, so only one is worn.
//           With "materialsFrom" (another colour of the same outfit, as a prefab) it also carries an MA Material
//           Setter that puts that prefab's materials on, so several colours share one set of meshes.
//   part    hides (or with "show": shows) some objects of an outfit: a Toggle with a parameter of its own.
//   toggle  an on/off switch of its own for something independent (a prop, an accessory): selected = shown.
//           "default" says whether it is shown at first (an accessory) or hidden (a prop).
//   strip   takes every outfit of a group off. It is marked as what it is (in a field Modular Avatar does not
//           use), so a later plan that adds an outfit teaches it to take that one off too. "objects" are more
//           things it turns off (the body's own underwear).
//   skin    picks one look of the body: a Toggle on a shared Int parameter (Skin_Choose) carrying an MA Material
//           Setter and no Object Toggle. The materials come from a prefab ("materialsFrom", compared with
//           "objects", the body's meshes) or are named one by one ("materials"); with neither it is the look the
//           scene has, there to go back to.
//   install brings the menu a plugin installs for itself (its MA Menu Installer, under "objects") to the place
//           "path" names, with an MA Menu Install Target. The plugin is not changed and is no menu item of ours.
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
            public Type item, installer, group, toggle, setter, target;
            public string parameter;
            public List<object> warnings = new List<object>();
            public List<object> created = new List<object>();
            public List<object> installed = new List<object>(); // plugin menus brought into the avatar's menu
            public HashSet<Transform> touchedMenus = new HashSet<Transform>();
            public bool changed;       // something in the scene is not as it was: there is a step to undo
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
            c.target = Refl.Find(Refl.MA + "ModularAvatarMenuInstallTarget");
            c.parameter = J.Str(args, "parameter", "Clothtoggle");
            if (c.parameter == "") c.parameter = "Clothtoggle";
            CheckSchema(c);

            List<object> items = J.Arr(J.Get(args, "items"));
            if (items.Count == 0) throw new PipelineException("计划中没有菜单项（items 为空）");
            // everything is looked up before anything is changed: a plan with a wrong path changes nothing
            var plan = new List<Entry>();
            foreach (object o in items) plan.Add(ReadEntry(c, J.Obj(o)));

            string backup = Bridge.BackupScene(c.avatar.gameObject);
            AvatarInspect.ParamBudget bitsBefore = AvatarInspect.Bits(desc);
            // a step of its own in the undo history; a plan that fails half way is taken back whole
            Undo.IncrementCurrentGroup();
            int undoGroup = Undo.GetCurrentGroup();
            Transform root;
            var made = new List<KeyValuePair<Entry, Transform>>();
            // the parameters this plan picks on (outfits on Clothtoggle, hair on Hair_Choose, ...), in plan order
            var groups = new List<string>();
            var skins = new HashSet<string>(); // those of them that pick a skin
            foreach (Entry e in plan)
            {
                if ((e.kind == "outfit" || e.kind == "strip" || e.kind == "skin") && !groups.Contains(e.parameter)) groups.Add(e.parameter);
                if (e.kind == "skin") skins.Add(e.parameter);
            }
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
                    if (e.kind == "install") { InstallMenus(c, root, e); continue; }
                    bool picks = e.kind == "outfit" || e.kind == "skin";
                    Transform parent = EnsurePath(c, root, e.path);
                    Transform t = EnsureItem(c, parent, e, picks ? usedValues[e.parameter] : null);
                    made.Add(new KeyValuePair<Entry, Transform>(e, t));
                    if (picks && e.isDefault) defaults[e.parameter].Add(t);
                }

                foreach (string g in groups)
                {
                    string what = skins.Contains(g) ? "皮肤" : g == "Hair_Choose" ? "头发" : "衣服";
                    // only one at a time: every picker of the group turns the others' objects off
                    var all = new List<Transform>();
                    foreach (Transform t in SelectItems(c, g))
                        foreach (Transform o in OwnObjects(c, t)) if (!all.Contains(o)) all.Add(o);
                    foreach (Transform t in SelectItems(c, g))
                    {
                        var own = OwnObjects(c, t);
                        foreach (Transform o in all) if (!own.Contains(o)) AddToggle(c, t, o, false, false);
                    }
                    // "take everything off", made by this plan or an earlier one, covers every outfit there is now
                    foreach (Transform t in c.avatar.GetComponentsInChildren<Transform>(true))
                    {
                        if (t.GetComponent(c.item) == null || t.GetComponent(c.toggle) == null) continue;
                        if (!IsStrip(c, t, g, before[g], made)) continue;
                        MarkStrip(c, t, g);
                        foreach (Transform o in all) AddToggle(c, t, o, false, false);
                        KeepLast(c, t, g, what);
                        if (all.Count == 0)
                            c.warnings.Add("「" + Label(c, t) + "」目前不控制任何物体：参数 " + g + " 上还没有" + what + "项。添加" + what + "后再次生成菜单，它会自动生效");
                    }

                    var defs = defaults[g];
                    if (defs.Count > 1) c.warnings.Add("计划中有 " + defs.Count + " 个默认" + what + "（" + g + "），仅采用最后一个");
                    // the scene shows what a visitor sees first: the default one on, the others off
                    Transform def = null;
                    if (defs.Count > 0)
                    {
                        def = defs[defs.Count - 1];
                        foreach (Transform t in ParamItems(c, g)) c.changed |= SetBool(t.GetComponent(c.item), "isDefault", t == def);
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
                            c.changed |= SetActive(o, own.Contains(o), "MioVRCA 默认" + what);
                        }
                    }
                    else if (all.Count > 0)
                        c.warnings.Add("尚未指定默认" + what + "（" + g + "），进入游戏时将保持场景中当前的显示状态，可将常用的一件设为默认（default）");
                }

                // an independent switch: the scene shows its first state
                foreach (var m in made)
                    if (m.Key.kind == "toggle")
                        foreach (Transform o in m.Key.objects) c.changed |= SetActive(o, m.Key.isDefault, "MioVRCA 开关默认");
            }
            catch (Exception)
            {
                Undo.RevertAllInCurrentGroup();
                throw;
            }

            var res = new Dictionary<string, object>();
            if (J.Get(args, "icons") is bool && !J.Bool(args, "icons")) res["icons"] = 0;
            else
            {
                int icons = Icons(c, root, made);
                res["icons"] = icons;
                if (icons > 0) c.changed = true;
            }

            foreach (Transform m in c.touchedMenus)
            {
                int n = 0;
                foreach (Transform k in m) if (k.GetComponent(c.item) != null || (c.target != null && k.GetComponent(c.target) != null)) n++;
                if (n > 8) c.warnings.Add("「" + m.name + "」中有 " + n + " 项，超过每页 8 项的上限，游戏中将自动出现「More」翻页，可再分一层");
            }
            // counted anew every time, with what Modular Avatar adds at build time: a plan that changes nothing on an
            // avatar that is over the limit says so as well
            AvatarInspect.ParamBudget bits = AvatarInspect.Bits(desc);
            res["parameterBits"] = bits.total;
            res["parameterBitsBefore"] = bitsBefore.total;
            res["parameterBitsAdded"] = bits.total - bitsBefore.total;
            if (bits.fury) res["parameterBitsAtLeast"] = true;
            if (bits.total > AvatarInspect.ParameterBitLimit)
                c.warnings.Add("同步参数预计" + (bits.fury ? "至少 " : " ") + bits.total + " / " + AvatarInspect.ParameterBitLimit + " 位（已计入 Modular Avatar 在构建时生成的参数"
                    + (bits.fury ? "；模型上有 VRCFury 组件，其参数无法预估，未计入" : "") + "），超出上限将导致上传失败，可减少部件开关的数量（如已安装 VRCFury，或可压缩至上限内）");
            // named only when there is something in it: the name of an empty step would pass for the one below it
            if (c.changed) Undo.SetCurrentGroupName("MioVRCA 生成菜单");
            Undo.CollapseUndoOperations(undoGroup);
            res["changed"] = c.changed; // false: the scene is as it was, there is nothing of this call to undo
            res["root"] = Refl.RelPath(c.avatar, root);
            res["parameter"] = c.parameter;
            res["parameters"] = new List<object>(groups.ToArray());
            res["created"] = c.created;
            if (c.installed.Count > 0) res["installed"] = c.installed;
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

        static readonly string[] Kinds = { "outfit", "part", "toggle", "strip", "skin", "install" };
        const string SkinParameter = "Skin_Choose";

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
            if (Array.IndexOf(Kinds, e.kind) < 0) throw new PipelineException("菜单项的 kind 只能是 outfit、part、toggle、strip、skin、install，当前为「" + e.kind + "」");
            if (e.label == "") throw new PipelineException("有菜单项缺少名称（label）");
            // skins pick on a parameter of their own, whatever the plan's outfits share
            string shared = e.kind == "skin" ? SkinParameter : c.parameter;
            e.parameter = J.Str(d, "parameter", shared).Trim();
            if (e.parameter == "") e.parameter = shared;
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
            if (e.kind != "strip" && e.kind != "skin" && e.objects.Count == 0) throw new PipelineException("「" + e.label + "」未指定所控制的物体（objects）");
            if (e.kind == "install" && c.target == null) throw new PipelineException("当前版本的 Modular Avatar 不含 Menu Install Target，无法移动「" + e.label + "」的菜单，请升级 Modular Avatar");
            e.isDefault = J.Bool(d, "default");
            e.show = J.Bool(d, "show");
            if (e.kind == "toggle" && e.isDefault && !c.toggleInverse)
                throw new PipelineException("「" + e.label + "」需默认显示，但当前版本 Modular Avatar 的 Object Toggle 不支持「反转条件」（需要 1.10 以上），可升级 Modular Avatar，或改为默认隐藏的开关");
            e.materialsFrom = J.Str(d, "materialsFrom").Replace('\\', '/').Trim();
            if (e.materialsFrom != "")
            {
                if (e.kind != "outfit" && e.kind != "skin") throw new PipelineException("「" + e.label + "」：仅衣服（outfit）和皮肤（skin）可使用 materialsFrom");
                if (e.objects.Count == 0) throw new PipelineException("「" + e.label + "」使用了 materialsFrom，需在 objects 中写明要换材质的网格（素体的身体、脸等）");
                if (c.setter == null) throw new PipelineException("当前版本的 Modular Avatar 不含 Material Setter（需要 1.12 以上），无法生成共用模型的配色");
                e.swaps = ReadSwaps(e);
            }
            List<object> named = J.Arr(J.Get(d, "materials"));
            if (named.Count > 0)
            {
                if (e.kind != "skin") throw new PipelineException("「" + e.label + "」：仅皮肤（skin）可使用 materials");
                if (c.setter == null) throw new PipelineException("当前版本的 Modular Avatar 不含 Material Setter（需要 1.12 以上），无法生成皮肤切换");
                foreach (object o in named) e.swaps.Add(ReadMaterial(c, e, J.Obj(o)));
            }
            return e;
        }

        // one slot named outright: {"object": the mesh below the avatar, "slot": its material slot, "material": the asset}
        static Swap ReadMaterial(Ctx c, Entry e, Dictionary<string, object> d)
        {
            Transform t = Refl.FindUnder(c.avatar, J.Str(d, "object"));
            Renderer r = t.GetComponent<Renderer>();
            if (r == null) throw new PipelineException("「" + e.label + "」：「" + J.Str(d, "object") + "」上没有网格渲染器，无法更换材质");
            int slot = (int)J.Num(d, "slot");
            if (slot < 0 || slot >= r.sharedMaterials.Length)
                throw new PipelineException("「" + e.label + "」：「" + t.name + "」只有 " + r.sharedMaterials.Length + " 个材质槽，没有第 " + slot + " 个（从 0 起算）");
            string path = J.Str(d, "material").Replace('\\', '/').Trim();
            var mat = AssetDatabase.LoadAssetAtPath<Material>(path);
            if (mat == null) throw new PipelineException("「" + e.label + "」的材质不在工程中：" + path);
            return new Swap { renderer = r, index = slot, material = mat };
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
            if (matched == 0)
                throw new PipelineException(e.kind == "skin"
                    ? "「" + e.label + "」：prefab「" + asset.name + "」中没有与所选网格同名的网格，无法从中读取皮肤材质"
                    : "「" + e.label + "」：配色 prefab「" + asset.name + "」与已装配的衣服不匹配（没有同名网格），两者不是同一件衣服的不同配色");
            return o;
        }

        static void WriteSwaps(Ctx c, Transform item, Entry e)
        {
            Component ms = item.GetComponent(c.setter);
            if (e.swaps.Count == 0)
            {
                if (ms == null && e.materialsFrom != "")
                    c.warnings.Add(e.kind == "skin" ? "「" + e.label + "」的 prefab 与模型当前的材质相同，该项不会改变外观" : "「" + e.label + "」的配色 prefab 与已装配的衣服材质相同，无需替换");
                return;
            }
            bool dirty = ms == null;
            if (ms == null) ms = Undo.AddComponent(item.gameObject, c.setter);
            var so = new SerializedObject(ms);
            SerializedProperty arr = Refl.Prop(so, "m_objects");
            if (arr.arraySize != e.swaps.Count) { arr.arraySize = e.swaps.Count; dirty = true; }
            for (int i = 0; i < e.swaps.Count; i++)
            {
                SerializedProperty n = arr.GetArrayElementAtIndex(i);
                SerializedProperty rp = n.FindPropertyRelative("Object.referencePath");
                SerializedProperty obj = n.FindPropertyRelative("Object.targetObject");
                SerializedProperty mat = n.FindPropertyRelative("Material");
                SerializedProperty idx = n.FindPropertyRelative("MaterialIndex");
                if (rp == null || mat == null || idx == null) throw new PipelineException("当前版本的 MA Material Setter 结构不兼容，Modular Avatar 版本可能过旧");
                string path = Refl.RelPath(c.avatar, e.swaps[i].renderer.transform);
                if (rp.stringValue == path && mat.objectReferenceValue == e.swaps[i].material && idx.intValue == e.swaps[i].index
                    && (obj == null || obj.objectReferenceValue == e.swaps[i].renderer.gameObject)) continue;
                rp.stringValue = path;
                if (obj != null) obj.objectReferenceValue = e.swaps[i].renderer.gameObject;
                mat.objectReferenceValue = e.swaps[i].material;
                idx.intValue = e.swaps[i].index;
                dirty = true;
            }
            if (!dirty) return;
            so.ApplyModifiedProperties();
            c.changed = true;
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
            Transform root = Refl.AtPath(c.avatar, name);
            if (root == null)
            {
                var go = new GameObject(name);
                Undo.RegisterCreatedObjectUndo(go, "MioVRCA 菜单");
                go.transform.SetParent(c.avatar, false);
                root = go.transform;
                c.created.Add(name);
                c.changed = true;
            }
            // a menu of its own already (a sub menu item, or an installer): used as it is
            if (root.GetComponent(c.item) == null && root.GetComponent(c.installer) == null) { Undo.AddComponent(root.gameObject, c.installer); c.changed = true; }
            if (root.GetComponent(c.item) == null && root.GetComponent(c.group) == null) { Undo.AddComponent(root.gameObject, c.group); c.changed = true; }
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
            c.changed = true;
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
            // only what differs is written: a plan that is carried out a second time leaves the scene untouched
            bool dirty = false;
            if (Refl.GetEnum(so, "Control.type") != "Toggle") { Refl.SetEnum(so, "Control.type", "Toggle"); dirty = true; }
            if (e.kind == "outfit" || e.kind == "skin")
            {
                bool had = !isNew && Refl.Prop(so, "Control.parameter.name").stringValue == e.parameter && !Refl.Prop(so, "automaticValue").boolValue;
                if (!had)
                {
                    // one past the highest in use: a number an outfit once had is not handed to another,
                    // or what players saved for the old one would pick the new one
                    int v = 1;
                    foreach (int u in usedValues) if (u >= v) v = u + 1;
                    if (v > 255) { v = 1; while (usedValues.Contains(v)) v++; } // an Int parameter holds 0..255
                    usedValues.Add(v);
                    Refl.Prop(so, "Control.parameter.name").stringValue = e.parameter;
                    Refl.Prop(so, "Control.value").floatValue = v;
                    Refl.Prop(so, "automaticValue").boolValue = false;
                    dirty = true;
                }
            }
            else
            {
                if (isNew)
                {
                    Refl.Prop(so, "Control.parameter.name").stringValue = "";
                    Refl.Prop(so, "Control.value").floatValue = 1;
                    Refl.Prop(so, "automaticValue").boolValue = true;
                    dirty = true;
                }
                if (e.kind == "toggle" && Refl.Prop(so, "isDefault").boolValue != e.isDefault) { Refl.Prop(so, "isDefault").boolValue = e.isDefault; dirty = true; }
            }
            if (dirty) { so.ApplyModifiedProperties(); c.changed = true; }
            if (e.kind == "strip") MarkStrip(c, t, e.parameter);
            // a skin changes materials only: no object toggle on it
            if (e.kind == "skin")
            {
                if (e.materialsFrom != "" || e.swaps.Count > 0) WriteSwaps(c, t, e);
                return t;
            }

            if (t.GetComponent(c.toggle) == null) { Undo.AddComponent(t.gameObject, c.toggle); c.changed = true; }
            if (e.kind == "outfit")
            {
                foreach (Transform o in e.objects) AddToggle(c, t, o, true, true);
                if (e.materialsFrom != "") WriteSwaps(c, t, e);
            }
            else if (e.kind == "part") foreach (Transform o in e.objects) AddToggle(c, t, o, e.show, true);
            else if (e.kind == "strip") foreach (Transform o in e.objects) AddToggle(c, t, o, false, true);
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
            if (p.boolValue != inverse) { p.boolValue = inverse; so.ApplyModifiedProperties(); c.changed = true; }
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
                SerializedProperty act = e.FindPropertyRelative("Active");
                if (replace && (act.boolValue != active || (rp != null && rp.stringValue != path) || (obj != null && obj.objectReferenceValue != target.gameObject)))
                {
                    act.boolValue = active;
                    if (rp != null) rp.stringValue = path;
                    if (obj != null) obj.objectReferenceValue = target.gameObject;
                    so.ApplyModifiedProperties();
                    c.changed = true;
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
            c.changed = true;
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

        // true: it was the other way round
        static bool SetActive(Transform o, bool on, string why)
        {
            if (o.gameObject.activeSelf == on) return false;
            Undo.RecordObject(o.gameObject, why);
            o.gameObject.SetActive(on);
            PrefabUtility.RecordPrefabInstancePropertyModifications(o.gameObject);
            return true;
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

        // what a "take everything off" item carries in Control.name, a field Modular Avatar never reads (the name a
        // menu shows comes from the label or the object): what it is, and for which parameter
        internal const string StripMark = "MioVRCA:strip:";

        static void MarkStrip(Ctx c, Transform t, string group)
        {
            var so = new SerializedObject(t.GetComponent(c.item));
            SerializedProperty p = so.FindProperty("Control.name");
            if (p == null || p.stringValue == StripMark + group) return;
            p.stringValue = StripMark + group;
            so.ApplyModifiedProperties();
            c.changed = true;
        }

        // "take everything off" of this group: named so in this plan, or marked as one by an earlier plan. One made
        // before there was a mark is known by what it does — its own switch, turning off every outfit the group
        // had so far and nothing on; with a single outfit that is also what a switch hiding that outfit looks
        // like, so then its name has to say it.
        static bool IsStrip(Ctx c, Transform t, string group, HashSet<Transform> before, List<KeyValuePair<Entry, Transform>> made)
        {
            foreach (var m in made) if (m.Value == t) return m.Key.kind == "strip" && m.Key.parameter == group;
            var so = new SerializedObject(t.GetComponent(c.item));
            SerializedProperty mark = so.FindProperty("Control.name");
            if (mark != null && mark.stringValue.StartsWith(StripMark)) return mark.stringValue == StripMark + group;
            if (before.Count < 1) return false;
            if (!so.FindProperty("automaticValue").boolValue) return false;
            var tg = new SerializedObject(t.GetComponent(c.toggle));
            SerializedProperty inv = tg.FindProperty("m_inverted");
            if (inv != null && inv.boolValue) return false;
            var off = new HashSet<Transform>();
            SerializedProperty arr = tg.FindProperty("m_objects");
            if (arr == null) return false;
            for (int i = 0; i < arr.arraySize; i++)
            {
                SerializedProperty e = arr.GetArrayElementAtIndex(i);
                if (e.FindPropertyRelative("Active").boolValue) return false;
                Transform o = Resolve(c, e);
                if (o != null) off.Add(o);
            }
            foreach (Transform o in before) if (!off.Contains(o)) return false;
            if (before.Count >= 2) return true;
            string label = Label(c, t);
            return label.Contains("脱") || label.Contains("裸") || label.IndexOf("strip", StringComparison.OrdinalIgnoreCase) >= 0;
        }

        // Where two object toggles say different things about one object, Modular Avatar lets the one further
        // down the hierarchy win. "Take everything off" therefore has to come after every outfit of its group,
        // or an outfit that is picked stays on. Moving it among its siblings leaves its path, and with it the
        // name of its own parameter, as it was.
        static void KeepLast(Ctx c, Transform strip, string group, string what)
        {
            if (!Before(c, strip, group)) return;
            Transform p = strip.parent;
            if (p != null && strip.GetSiblingIndex() != p.childCount - 1)
            {
                try
                {
                    Undo.RegisterFullObjectHierarchyUndo(p.gameObject, "MioVRCA 菜单顺序");
                    strip.SetAsLastSibling();
                    c.changed = true;
                }
                catch (Exception) { }
            }
            if (Before(c, strip, group))
                c.warnings.Add("「" + Label(c, strip) + "」排在部分" + what + "项之前，这些" + what + "选中时不会被脱下。请在 Hierarchy 中将它拖到所有" + what + "菜单项之后");
        }

        // is this item above any outfit picker of the group, in hierarchy order?
        static bool Before(Ctx c, Transform item, string group)
        {
            var pickers = new HashSet<Transform>(SelectItems(c, group));
            bool seen = false;
            foreach (Transform t in c.avatar.GetComponentsInChildren<Transform>(true))
            {
                if (t == item) seen = true;
                else if (seen && pickers.Contains(t)) return true;
            }
            return false;
        }

        // The menu a plugin brings along, moved to where the plan says: one MA Menu Install Target per MA Menu
        // Installer of the plugin, the way Modular Avatar's own 「选择菜单」 does it. An installer that brings one
        // entry (its own sub menu) goes straight into the plan's menu; one that brings several gets a level named
        // by the label. Installers that have a target already stay where they were put.
        static void InstallMenus(Ctx c, Transform root, Entry e)
        {
            var installers = new List<Component>();
            foreach (Transform o in e.objects)
                foreach (Component mi in o.GetComponentsInChildren(c.installer, true))
                    if (mi != null && !mi.transform.IsChildOf(root) && !installers.Contains(mi)) installers.Add(mi);
            if (installers.Count == 0)
            {
                c.warnings.Add("「" + e.label + "」没有 MA Menu Installer，菜单未移动：它的菜单可能由 VRCFury 或素材自己的脚本在构建时生成，位置需在对应组件中设置");
                return;
            }
            var taken = new HashSet<UnityEngine.Object>();
            foreach (Component tg in c.avatar.GetComponentsInChildren(c.target, true))
            {
                SerializedProperty ins = new SerializedObject(tg).FindProperty("installer");
                if (ins != null && ins.objectReferenceValue != null) taken.Add(ins.objectReferenceValue);
            }
            foreach (Component mi in installers)
            {
                if (taken.Contains(mi)) continue;
                int n = Controls(c, mi);
                if (n == 0) continue; // an installer with nothing to install
                var path = new List<string>(e.path);
                if (n > 1) path.Add(e.label);
                Transform parent = EnsurePath(c, root, path);
                var go = new GameObject(mi.gameObject.name);
                Undo.RegisterCreatedObjectUndo(go, "MioVRCA 菜单位置");
                go.transform.SetParent(parent, false);
                Component tc = Undo.AddComponent(go, c.target);
                var so = new SerializedObject(tc);
                Refl.Prop(so, "installer").objectReferenceValue = mi;
                so.ApplyModifiedProperties();
                c.created.Add(Refl.RelPath(c.avatar, go.transform));
                c.installed.Add(new Dictionary<string, object> { { "installer", Refl.RelPath(c.avatar, mi.transform) }, { "at", Refl.RelPath(c.avatar, go.transform) }, { "entries", n } });
                c.changed = true;
            }
        }

        // how many entries an installer brings: its own menu item is one, a menu group brings its children, a menu
        // asset its controls
        static int Controls(Ctx c, Component installer)
        {
            Transform t = installer.transform;
            if (t.GetComponent(c.item) != null) return 1;
            Component g = t.GetComponent(c.group);
            if (g != null)
            {
                SerializedProperty to = new SerializedObject(g).FindProperty("targetObject");
                Transform under = to != null && to.objectReferenceValue is GameObject ? ((GameObject)to.objectReferenceValue).transform : t;
                int n = 0;
                foreach (Transform k in under) if (k.GetComponent(c.item) != null) n++;
                return n;
            }
            SerializedProperty menu = new SerializedObject(installer).FindProperty("menuToAppend");
            if (menu == null || menu.objectReferenceValue == null) return 0;
            SerializedProperty controls = new SerializedObject(menu.objectReferenceValue).FindProperty("controls");
            return controls != null && controls.isArray ? controls.arraySize : 0;
        }

        // true: it was the other way round
        static bool SetBool(Component mi, string field, bool v)
        {
            var so = new SerializedObject(mi);
            SerializedProperty p = Refl.Prop(so, field);
            if (p.boolValue == v) return false;
            p.boolValue = v;
            so.ApplyModifiedProperties();
            return true;
        }

        // a picture for every entry of this plan that has none, and for the outfit's sub menu
        static int Icons(Ctx c, Transform root, List<KeyValuePair<Entry, Transform>> made)
        {
            int n = 0;
            string folder = "Assets/MioVRCA/" + IconShot.SafeName(c.avatar.name) + "/Icons";
            foreach (var m in made)
            {
                Entry e = m.Key;
                if (e.kind == "strip" || e.kind == "skin" || e.objects.Count == 0) continue;
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
