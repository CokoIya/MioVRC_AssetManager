// Putting an outfit prefab on an avatar: an instance under the avatar root, set up with Modular Avatar's
// "Setup Outfit" (Merge Armature + Mesh Settings). The outfit's own files are not touched.
// A plugin prefab (Place) goes under the avatar as it is.
using System;
using System.Collections.Generic;
using System.Reflection;
using UnityEditor;
using UnityEngine;

namespace MioVRCA.Pipeline
{
    internal static class Dresser
    {
        public static object Dress(Dictionary<string, object> args)
        {
            Component avatar = Refl.FindAvatar(J.Str(args, "avatar"));
            Transform root = avatar.transform;
            string path = J.Str(args, "prefab").Replace('\\', '/');
            var asset = AssetDatabase.LoadAssetAtPath<GameObject>(path);
            if (asset == null) throw new PipelineException("工程中未找到该 prefab：" + path);
            Type desc = Refl.DescriptorType;
            if (desc != null && asset.GetComponentInChildren(desc, true) != null)
                throw new PipelineException("「" + asset.name + "」是包含素体的完整模型，不是单独的衣服，请改用仅含衣服的 prefab");
            Type merge = Refl.MAType("ModularAvatarMergeArmature");

            var res = new Dictionary<string, object>();
            var warnings = new List<object>();
            res["warnings"] = warnings;
            // a step of its own in the undo history: one Ctrl+Z takes back this outfit and nothing else
            Undo.IncrementCurrentGroup();
            int undoGroup = Undo.GetCurrentGroup();
            string label = "MioVRCA 装配 " + asset.name;
            string asked = J.Str(args, "name").Trim(); // "": the prefab's own name
            string name = asked != "" ? asked : asset.name;

            // the same prefab is on the avatar already: use that one. An instance the player unpacked no longer
            // says where it came from; it is known by its name and its meshes.
            GameObject go = null;
            foreach (Transform c in root)
                if (Refl.PrefabSource(c.gameObject) == path) { go = c.gameObject; break; }
            if (go == null) go = Unpacked(root, asset, name);
            bool created = go == null, changed = false, touched = false; // touched: something may have to be taken back
            try
            {
                if (created)
                {
                    string backup = Bridge.BackupScene(root.gameObject);
                    if (backup != "") res["sceneBackup"] = backup;
                    Undo.SetCurrentGroupName(label);
                    name = FreeName(root, name, path, warnings);
                    go = PrefabUtility.InstantiatePrefab(asset, root) as GameObject;
                    if (go == null) throw new PipelineException("无法将「" + asset.name + "」放入场景");
                    changed = touched = true;
                    Undo.RegisterCreatedObjectUndo(go, label);
                }
                else
                {
                    res["existing"] = true;
                    if (asked == "") name = go.name; // nobody asked for another name: it keeps the one it has
                }
                if (go.name != name)
                {
                    // what is there keeps its name when another child has the one asked for: two children with
                    // one name cannot be told apart by their path
                    Transform other = Refl.Child(root, name);
                    if (other != null && other != go.transform)
                        warnings.Add("模型下已有另一个名为「" + name + "」的物体，「" + go.name + "」保持原名");
                    else
                    {
                        changed = touched = true;
                        Undo.RecordObject(go, "MioVRCA 改名");
                        go.name = name;
                        PrefabUtility.RecordPrefabInstancePropertyModifications(go);
                    }
                }

                Type proxy = Refl.Find(Refl.MA + "ModularAvatarBoneProxy");
                bool pinned = proxy != null && go.GetComponentInChildren(proxy, true) != null; // an accessory that follows a bone already
                if (go.GetComponentInChildren(merge, true) == null && !pinned)
                {
                    // on something that was there already, Setup Outfit may find nothing to do (a prop has no armature)
                    int had = go.GetComponentsInChildren<Component>(true).Length;
                    touched = true;
                    string why = SetupOutfit(go);
                    if (go.GetComponentsInChildren<Component>(true).Length != had) changed = true;
                    if (go.GetComponentInChildren(merge, true) == null)
                    {
                        bool skinned = go.GetComponentInChildren<SkinnedMeshRenderer>(true) != null;
                        warnings.Add(skinned
                            ? "Modular Avatar 无法将该素材的骨架对应到模型" + (why != "" ? "（" + why + "）" : "") + "，可能不是为该素体制作，或骨架结构特殊，目前仅放置在模型下，不会跟随模型运动"
                            : "该素材不含骨架（配饰、道具等），已放置在模型下，需手动调整位置或挂到对应骨骼上");
                    }
                }
                res["setUp"] = pinned || go.GetComponentInChildren(merge, true) != null;
                if (pinned) res["accessory"] = true;
                // shown or hidden is said for what this call puts on. What was on the avatar already keeps the
                // state it has: the menu, or the player, decided that.
                if (created && J.Get(args, "active") is bool)
                {
                    bool on = J.Bool(args, "active");
                    if (go.activeSelf != on) { Undo.RecordObject(go, "MioVRCA 显示"); go.SetActive(on); }
                }
            }
            catch (Exception)
            {
                // nothing half done stays in the scene: an outfit that was put in but could not be set up is
                // taken out again, with whatever else this call changed
                if (touched) Undo.RevertAllDownToGroup(undoGroup);
                if (created && go != null) UnityEngine.Object.DestroyImmediate(go);
                throw;
            }
            if (changed)
            {
                Undo.SetCurrentGroupName(label); // Modular Avatar named the group after itself
                Undo.CollapseUndoOperations(undoGroup);
            }
            res["changed"] = changed; // false: the scene is as it was, there is nothing of this call to undo
            res["name"] = go.name;
            res["active"] = go.activeSelf;
            res["outfit"] = AvatarInspect.Describe(root, go.transform);
            int bones = AvatarInspect.PhysBones(root);
            if (bones >= 0)
            {
                res["physBones"] = bones;
                int own = AvatarInspect.PhysBones(go.transform);
                if (bones > AvatarInspect.PhysBoneLimit)
                    warnings.Add("模型上共有 " + bones + " 个 PhysBone（本素材占 " + own + " 个），超过 " + AvatarInspect.PhysBoneLimit + " 个的上限，上传时将校验失败。隐藏的物体同样计入：同款不同配色只需装配一件，其余配色在菜单中做成换色项；不再使用的素材可从模型上移除");
            }
            res["note"] = "场景没有保存；Ctrl+Z 可以撤销";
            return res;
        }

        // Putting a plugin prefab under the avatar as it is: face tracking, a lighting control, an interaction
        // gimmick, an expression add-on. Such a prefab installs itself when the avatar is built (its own menu
        // installer, animator, parameters), and its animations find objects by path: it keeps its name and its
        // hierarchy, gets no Setup Outfit and no switch. Its files are not touched.
        public static object Place(Dictionary<string, object> args)
        {
            Component avatar = Refl.FindAvatar(J.Str(args, "avatar"));
            Transform root = avatar.transform;
            string path = J.Str(args, "prefab").Replace('\\', '/').Trim();
            var asset = AssetDatabase.LoadAssetAtPath<GameObject>(path);
            if (asset == null) throw new PipelineException("工程中未找到该 prefab：" + path);
            Type desc = Refl.DescriptorType;
            if (desc != null && asset.GetComponentInChildren(desc, true) != null)
                throw new PipelineException("「" + asset.name + "」是包含素体的完整模型，不能作为插件放置到模型下");
            int missing;
            bool self, fury;
            List<object> scripts = AvatarInspect.Scripts(asset.transform, out missing, out self, out fury);
            if (missing > 0)
                throw new PipelineException("「" + asset.name + "」中有 " + missing + " 个缺失的脚本：它依赖的插件尚未安装到工程中，未放置。请先按素材说明安装所需的插件（常见的有 Modular Avatar、VRCFury 或素材自带的框架包）");

            var res = new Dictionary<string, object>();
            var warnings = new List<object>();
            res["warnings"] = warnings;
            GameObject go = null;
            foreach (Transform c in root)
                if (Refl.PrefabSource(c.gameObject) == path) { go = c.gameObject; break; }
            AvatarInspect.ParamBudget bitsBefore = AvatarInspect.Bits(avatar);
            bool created = go == null;
            if (created)
            {
                // a plugin cannot go by another name, so a name that is taken stops this before anything happens
                if (Refl.Child(root, asset.name) != null)
                    throw new PipelineException("模型下已有名为「" + asset.name + "」的物体（不是该 prefab 的实例）。插件的动画按物体路径查找，不能改名放置：请先在 Unity 中处理同名物体后重试");
                string backup = Bridge.BackupScene(root.gameObject);
                if (backup != "") res["sceneBackup"] = backup;
                // a step of its own in the undo history: one Ctrl+Z takes this plugin out again
                Undo.IncrementCurrentGroup();
                int undoGroup = Undo.GetCurrentGroup();
                string label = "MioVRCA 放置 " + asset.name;
                Undo.SetCurrentGroupName(label);
                go = PrefabUtility.InstantiatePrefab(asset, root) as GameObject;
                if (go == null) throw new PipelineException("无法将「" + asset.name + "」放入场景");
                Undo.RegisterCreatedObjectUndo(go, label);
                Undo.SetCurrentGroupName(label);
                Undo.CollapseUndoOperations(undoGroup);
            }
            else res["existing"] = true;
            res["changed"] = created; // false: it was there already, there is nothing of this call to undo
            res["name"] = go.name;
            res["object"] = Refl.RelPath(root, go.transform);
            res["active"] = go.activeSelf;
            res["scripts"] = scripts;
            if (self) res["selfInstalling"] = true;
            if (fury) res["vrcFury"] = true;
            if (AvatarInspect.FaceTracking(go.transform)) res["faceTracking"] = true;
            // its own menus: where its MA Menu Installers sit, for a plan that brings them into the avatar's menu
            var installers = new List<object>();
            Type mi = Refl.Find(Refl.MA + "ModularAvatarMenuInstaller");
            if (mi != null)
                foreach (Component c in go.GetComponentsInChildren(mi, true))
                    if (c != null) installers.Add(Refl.RelPath(root, c.transform));
            res["menuInstallers"] = installers;
            if (!self) warnings.Add("「" + go.name + "」不含 Modular Avatar 或 VRCFury 的安装组件，仅放置在模型下，是否生效取决于素材自身的说明");
            int bones = AvatarInspect.PhysBones(root);
            if (bones >= 0)
            {
                res["physBones"] = bones;
                if (bones > AvatarInspect.PhysBoneLimit)
                    warnings.Add("模型上共有 " + bones + " 个 PhysBone（「" + go.name + "」占 " + AvatarInspect.PhysBones(go.transform) + " 个），超过 " + AvatarInspect.PhysBoneLimit + " 个的上限，上传时将校验失败");
            }
            AvatarInspect.ParamBudget bits = AvatarInspect.Bits(avatar);
            res["parameterBits"] = bits.total;
            res["parameterBitsAdded"] = bits.total - bitsBefore.total;
            if (bits.fury) res["parameterBitsAtLeast"] = true;
            if (bits.total > AvatarInspect.ParameterBitLimit)
                warnings.Add("同步参数预计" + (bits.fury ? "至少 " : " ") + bits.total + " / " + AvatarInspect.ParameterBitLimit + " 位（「" + go.name + "」新增约 " + (bits.total - bitsBefore.total) + " 位），超出上限将导致上传失败");
            res["note"] = "场景没有保存；Ctrl+Z 可以撤销";
            return res;
        }

        // A child of the avatar that was an instance of this prefab once: it has the name the prefab would get
        // and renderers of the same names. Used as it is, instead of a second copy beside it.
        static GameObject Unpacked(Transform root, GameObject asset, string name)
        {
            List<string> want = RendererNames(asset.transform);
            if (want.Count == 0) return null;
            foreach (Transform c in root)
            {
                if (c.name != name || Refl.PrefabSource(c.gameObject) != "") continue; // an instance of another prefab is not it
                List<string> have = RendererNames(c);
                if (have.Count != want.Count) continue;
                bool same = true;
                for (int i = 0; i < want.Count && same; i++) same = want[i] == have[i];
                if (same) return c.gameObject;
            }
            return null;
        }

        static List<string> RendererNames(Transform t)
        {
            var o = new List<string>();
            foreach (Renderer r in t.GetComponentsInChildren<Renderer>(true)) o.Add(r.name);
            o.Sort(StringComparer.Ordinal);
            return o;
        }

        // The name a new object gets. Another child of the avatar may have it already (two shops both call their
        // prefab "Dress", or one outfit comes in a folder per base body): menu items find objects by path, and
        // those for the second would control the first. The new one is then named after the folder that sets its
        // prefab apart, or numbered.
        static string FreeName(Transform root, string name, string prefab, List<object> warnings)
        {
            Transform clash = Refl.Child(root, name);
            if (clash == null) return name;
            string free = "";
            string folder = DifferentFolder(prefab, Refl.PrefabSource(clash.gameObject));
            if (folder != "" && Refl.Child(root, name + "_" + folder) == null) free = name + "_" + folder;
            for (int n = 2; free == ""; n++)
                if (Refl.Child(root, name + " (" + n + ")") == null) free = name + " (" + n + ")";
            warnings.Add("模型下已有名为「" + name + "」的物体（不是同一个 prefab），新装配的物体已命名为「" + free + "」，菜单项请使用该名称。素材自带的动画若按原名查找物体，将不会生效");
            return free;
        }

        // the nearest folder of a prefab's path that the other prefab's path does not share ("" when there is none)
        static string DifferentFolder(string prefab, string other)
        {
            if (string.IsNullOrEmpty(other)) return "";
            string[] a = prefab.Split('/'), b = other.Split('/');
            for (int i = a.Length - 2, j = b.Length - 2; i >= 0 && j >= 0; i--, j--)
                if (!string.Equals(a[i], b[j], StringComparison.OrdinalIgnoreCase)) return a[i];
            return "";
        }

        // Modular Avatar's own Setup Outfit, without its error window. Returns what went wrong, if it says.
        static string SetupOutfit(GameObject outfit)
        {
            Type setup = Refl.Find("nadena.dev.modular_avatar.core.editor.SetupOutfit");
            MethodInfo run = setup != null ? setup.GetMethod("SetupOutfitUI", BindingFlags.Public | BindingFlags.Static, null, new[] { typeof(GameObject) }, null) : null;
            if (run == null) throw new PipelineException("当前版本的 Modular Avatar 不支持 Setup Outfit 接口（需要 1.8 以上）");
            FieldInfo msgs = setup.GetField("errorMessageGroups", BindingFlags.NonPublic | BindingFlags.Static);
            if (msgs != null) msgs.SetValue(null, null); // what an earlier run left there is not about this outfit
            Type win = Refl.Find("nadena.dev.modular_avatar.core.editor.ESOErrorWindow");
            FieldInfo suppress = win != null ? win.GetField("Suppress", BindingFlags.NonPublic | BindingFlags.Public | BindingFlags.Static) : null;
            object was = suppress != null ? suppress.GetValue(null) : null;
            try
            {
                if (suppress != null) suppress.SetValue(null, true);
                run.Invoke(null, new object[] { outfit });
            }
            catch (TargetInvocationException e)
            {
                return e.InnerException != null ? e.InnerException.Message : e.Message;
            }
            finally
            {
                if (suppress != null) suppress.SetValue(null, was);
                else if (win != null)
                    foreach (UnityEngine.Object w in Resources.FindObjectsOfTypeAll(win))
                        if (w is EditorWindow) ((EditorWindow)w).Close();
            }
            var m = msgs != null ? msgs.GetValue(null) as string[] : null;
            return m != null && m.Length > 0 ? m[0] : "";
        }
    }
}
