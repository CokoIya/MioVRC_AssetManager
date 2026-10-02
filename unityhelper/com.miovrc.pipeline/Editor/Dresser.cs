// Putting an outfit prefab on an avatar: an instance under the avatar root, set up with Modular Avatar's
// "Setup Outfit" (Merge Armature + Mesh Settings). The outfit's own files are not touched.
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
            if (asset == null) throw new PipelineException("工程里没有这个 prefab：" + path);
            Type desc = Refl.DescriptorType;
            if (desc != null && asset.GetComponentInChildren(desc, true) != null)
                throw new PipelineException("「" + asset.name + "」是带素体的整只模型，不是单独的衣服；换一个只有衣服的 prefab");
            Type merge = Refl.MAType("ModularAvatarMergeArmature");

            var res = new Dictionary<string, object>();
            var warnings = new List<object>();
            res["warnings"] = warnings;
            // a step of its own in the undo history: one Ctrl+Z takes back this outfit and nothing else
            Undo.IncrementCurrentGroup();
            int undoGroup = Undo.GetCurrentGroup();

            // the same prefab is on the avatar already: use that one
            GameObject go = null;
            foreach (Transform c in root)
                if (Refl.PrefabSource(c.gameObject) == path) { go = c.gameObject; break; }
            if (go != null)
            {
                res["existing"] = true;
            }
            else
            {
                string backup = Bridge.BackupScene(root.gameObject);
                if (backup != "") res["sceneBackup"] = backup;
                Undo.SetCurrentGroupName("MioVRCA 穿戴 " + asset.name);
                go = PrefabUtility.InstantiatePrefab(asset, root) as GameObject;
                if (go == null) throw new PipelineException("没能把「" + asset.name + "」放进场景");
                Undo.RegisterCreatedObjectUndo(go, "MioVRCA 穿戴 " + asset.name);
            }
            string name = J.Str(args, "name");
            if (name != "" && go.name != name)
            {
                Undo.RecordObject(go, "MioVRCA 改名");
                go.name = name;
                PrefabUtility.RecordPrefabInstancePropertyModifications(go);
            }

            Type proxy = Refl.Find(Refl.MA + "ModularAvatarBoneProxy");
            bool pinned = proxy != null && go.GetComponentInChildren(proxy, true) != null; // an accessory that follows a bone already
            if (go.GetComponentInChildren(merge, true) == null && !pinned)
            {
                string why = SetupOutfit(go);
                if (go.GetComponentInChildren(merge, true) == null)
                {
                    bool skinned = go.GetComponentInChildren<SkinnedMeshRenderer>(true) != null;
                    warnings.Add(skinned
                        ? "Modular Avatar 没能把它的骨架对到头像上" + (why != "" ? "（" + why + "）" : "") + "：多半不是给这个素体做的，或者骨架结构特殊；它现在只是放在头像下面，不会跟着动"
                        : "它没有骨架（配饰、道具这类）：已经放到头像下面，位置需要自己摆，或者挂到对应的骨头上");
                }
            }
            res["setUp"] = pinned || go.GetComponentInChildren(merge, true) != null;
            if (pinned) res["accessory"] = true;
            if (J.Get(args, "active") is bool)
            {
                bool on = J.Bool(args, "active");
                if (go.activeSelf != on) { Undo.RecordObject(go, "MioVRCA 显示"); go.SetActive(on); }
            }
            Undo.SetCurrentGroupName("MioVRCA 穿戴 " + asset.name); // Modular Avatar named the group after itself
            Undo.CollapseUndoOperations(undoGroup);
            res["outfit"] = AvatarInspect.Describe(root, go.transform);
            res["note"] = "场景没有保存；Ctrl+Z 可以撤销";
            return res;
        }

        // Modular Avatar's own Setup Outfit, without its error window. Returns what went wrong, if it says.
        static string SetupOutfit(GameObject outfit)
        {
            Type setup = Refl.Find("nadena.dev.modular_avatar.core.editor.SetupOutfit");
            MethodInfo run = setup != null ? setup.GetMethod("SetupOutfitUI", BindingFlags.Public | BindingFlags.Static, null, new[] { typeof(GameObject) }, null) : null;
            if (run == null) throw new PipelineException("这个版本的 Modular Avatar 没有 Setup Outfit 接口（需要 1.8 以上）");
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
