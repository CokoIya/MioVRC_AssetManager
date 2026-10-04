// One-click fixes from the check-up: mechanical changes that can be taken back, and nothing else (a pink material
// needs its shader, a parameter overrun needs a decision). A scene change is one "MioVRCA …" step in the undo
// history. A texture's import settings are not in Unity's undo history, so what they were is written to
// UserSettings/MioVRCA/fixes/<time>.json first, and fix_revert puts them back from that file.
using System;
using System.Collections.Generic;
using System.IO;
using UnityEditor;
using UnityEngine;

namespace MioVRCA.Pipeline
{
    internal static class Fixes
    {
        const string Dir = "UserSettings/MioVRCA/fixes";
        const int Big = 4096, ListMax = 60;
        // the platform overrides that matter for an avatar: PC, Quest, iOS
        static readonly string[] Platforms = { "Standalone", "Android", "iPhone" };

        public static object Run(Dictionary<string, object> args)
        {
            string kind = J.Str(args, "kind");
            Component avatar = Refl.FindAvatar(J.Str(args, "avatar"));
            switch (kind)
            {
                case "textures": return Textures(args, avatar.transform);
                case "lights": return Lights(args, avatar.transform);
                case "missing": return Missing(avatar.transform);
            }
            throw new PipelineException("没有「" + kind + "」这项一键处理");
        }

        static Dictionary<string, object> Line(string text, string note)
        {
            var d = new Dictionary<string, object> { { "t", text } };
            if (!string.IsNullOrEmpty(note)) d["n"] = note;
            return d;
        }

        static string PathOf(Transform root, Transform t)
        {
            return t == root ? root.name : Refl.RelPath(root, t);
        }

        // ---------- textures: Max Size down to 2048 (or 1024) ----------
        // The import settings belong to the texture asset, not to the avatar: every avatar and scene of the
        // project that uses the texture gets the smaller one.

        // What the fix can change: an asset of the project's own (not a package's) with texture import settings.
        // The check-up counts by it (fixable), so the button is only offered for what it will change.
        public static bool CanLower(string path)
        {
            return !string.IsNullOrEmpty(path) && path.StartsWith("Assets/") && AssetImporter.GetAtPath(path) is TextureImporter;
        }

        static object Textures(Dictionary<string, object> args, Transform root)
        {
            int max = (int)J.Num(args, "max", 2048);
            if (max != 1024 && max != 2048) throw new PipelineException("Max Size 只能降到 2048 或 1024");
            var paths = new List<string>();
            foreach (string p in J.Strs(args, "paths"))
            {
                string q = p.Replace('\\', '/').Trim();
                if (q != "" && !paths.Contains(q)) paths.Add(q);
            }
            if (paths.Count == 0 && J.Bool(args, "all"))
                foreach (Texture t in Used(root))
                {
                    if (Mathf.Max(t.width, t.height) < Big) continue;
                    string p = AssetDatabase.GetAssetPath(t);
                    if (p != "" && !paths.Contains(p)) paths.Add(p);
                }
            if (paths.Count == 0 && !J.Bool(args, "all")) throw new PipelineException("未指定要处理的贴图（paths），或 all: true 处理模型用到的全部 4096 像素贴图");

            // first what each one is now, so the record is complete before anything is reimported
            var records = new List<object>();
            var skipped = new List<object>();
            var todo = new List<string>();
            long before = 0;
            foreach (string p in paths)
            {
                var imp = p.StartsWith("Assets/") ? AssetImporter.GetAtPath(p) as TextureImporter : null;
                if (imp == null) { skipped.Add(Line(p, p.StartsWith("Assets/") ? "不是可设置 Max Size 的贴图" : "不在 Assets 文件夹内")); continue; }
                bool change = imp.maxTextureSize > max;
                var plats = new Dictionary<string, object>();
                foreach (string plat in Platforms)
                {
                    TextureImporterPlatformSettings s = imp.GetPlatformTextureSettings(plat);
                    plats[plat] = new Dictionary<string, object> { { "overridden", s.overridden }, { "max", s.maxTextureSize } };
                    if (s.overridden && s.maxTextureSize > max) change = true;
                }
                if (!change) { skipped.Add(Line(p, "Max Size 已不高于 " + max)); continue; }
                records.Add(new Dictionary<string, object> { { "path", p }, { "max", imp.maxTextureSize }, { "platforms", plats } });
                var tex = AssetDatabase.LoadAssetAtPath<Texture>(p);
                if (tex != null) before += Checkup.TextureBytes(tex);
                todo.Add(p);
            }
            string record = "";
            if (todo.Count > 0)
            {
                Directory.CreateDirectory(Dir);
                record = Dir + "/" + DateTime.Now.ToString("yyyyMMdd_HHmmss_fff") + ".json";
                var d = new Dictionary<string, object> { { "kind", "textures" }, { "at", DateTimeOffset.UtcNow.ToUnixTimeSeconds() }, { "max", max }, { "avatar", root.name }, { "textures", records } };
                File.WriteAllText(record, MiniJson.Write(d), new System.Text.UTF8Encoding(false));
            }

            var items = new List<object>();
            long after = 0;
            foreach (string p in todo)
            {
                var imp = (TextureImporter)AssetImporter.GetAtPath(p);
                int was = imp.maxTextureSize;
                var lowered = new List<object>();
                if (was > max) imp.maxTextureSize = max;
                foreach (string plat in Platforms)
                {
                    TextureImporterPlatformSettings s = imp.GetPlatformTextureSettings(plat);
                    if (!s.overridden || s.maxTextureSize <= max) continue;
                    s.maxTextureSize = max; // the override keeps its format and compression
                    imp.SetPlatformTextureSettings(s);
                    lowered.Add(plat);
                }
                try { imp.SaveAndReimport(); }
                // (the record is in the project: the program finds it there and offers 「恢复」 from it)
                catch (Exception e) { throw new PipelineException("重新导入「" + p + "」失败：" + e.Message + "。此前已改动的贴图可在体检面板中点击「恢复」改回原设置"); }
                var tex = AssetDatabase.LoadAssetAtPath<Texture>(p);
                if (tex != null) after += Checkup.TextureBytes(tex);
                var d = new Dictionary<string, object> { { "path", p }, { "name", tex != null ? tex.name : Path.GetFileNameWithoutExtension(p) }, { "from", was }, { "to", max } };
                if (lowered.Count > 0) d["platforms"] = lowered;
                if (items.Count < ListMax) items.Add(d);
            }
            var o = new Dictionary<string, object> { { "kind", "textures" }, { "max", max }, { "changed", todo.Count }, { "items", items }, { "skipped", skipped } };
            if (record != "") o["record"] = record;
            if (todo.Count > 0) { o["memoryBefore"] = Math.Round(before / 1048576.0, 1); o["memoryAfter"] = Math.Round(after / 1048576.0, 1); }
            return o;
        }

        // every texture the avatar's materials use, as the check-up gathers them
        static List<Texture> Used(Transform root)
        {
            var o = new List<Texture>();
            var seen = new HashSet<Material>();
            foreach (Renderer r in root.GetComponentsInChildren<Renderer>(true))
            {
                if (r == null || Refl.EditorOnly(r.transform)) continue;
                foreach (Material m in r.sharedMaterials)
                {
                    if (m == null || m.shader == null || !seen.Add(m)) continue;
                    int[] ids;
                    try { ids = m.GetTexturePropertyNameIDs(); }
                    catch (Exception) { continue; }
                    foreach (int id in ids)
                    {
                        Texture t = m.GetTexture(id);
                        if (t != null && !o.Contains(t)) o.Add(t);
                    }
                }
            }
            return o;
        }

        // fix_revert: the import settings as the record says they were. The record is spent by it, unless a
        // texture it names is no longer at its path (moved or renamed since): then it stays, for another go.
        public static object Revert(Dictionary<string, object> args)
        {
            string record = J.Str(args, "record").Replace('\\', '/').Trim();
            if (!record.StartsWith(Dir + "/") || record.Contains("..") || !record.EndsWith(".json")) throw new PipelineException("无效的恢复记录：" + record);
            if (!File.Exists(record)) throw new PipelineException("恢复记录已不存在，无法恢复：" + record);
            var d = J.Obj(MiniJson.Parse(File.ReadAllText(record)));
            if (J.Str(d, "kind") != "textures") throw new PipelineException("该记录不是贴图处理的记录：" + record);
            var items = new List<object>();
            var missing = new List<object>();
            foreach (object x in J.Arr(J.Get(d, "textures")))
            {
                var r = J.Obj(x);
                string p = J.Str(r, "path");
                var imp = AssetImporter.GetAtPath(p) as TextureImporter;
                if (imp == null) { missing.Add(p); continue; }
                // only what differs is written: a platform that was never overridden stays out of the .meta
                bool dirty = false;
                int max = (int)J.Num(r, "max", imp.maxTextureSize);
                if (imp.maxTextureSize != max) { imp.maxTextureSize = max; dirty = true; }
                foreach (var kv in J.Obj(J.Get(r, "platforms")))
                {
                    var ps = J.Obj(kv.Value);
                    TextureImporterPlatformSettings s = imp.GetPlatformTextureSettings(kv.Key);
                    bool ov = J.Bool(ps, "overridden", s.overridden);
                    int pm = (int)J.Num(ps, "max", s.maxTextureSize);
                    if (s.overridden == ov && s.maxTextureSize == pm) continue;
                    s.overridden = ov;
                    s.maxTextureSize = pm;
                    imp.SetPlatformTextureSettings(s);
                    dirty = true;
                }
                if (dirty) imp.SaveAndReimport();
                if (items.Count < ListMax) items.Add(new Dictionary<string, object> { { "path", p }, { "to", imp.maxTextureSize } });
            }
            if (missing.Count == 0) try { File.Delete(record); } catch (Exception) { }
            return new Dictionary<string, object> { { "kind", "textures" }, { "reverted", J.Arr(J.Get(d, "textures")).Count - missing.Count }, { "items", items }, { "missing", missing } };
        }

        // ---------- lights: removed (or, with remove: false, only off), one undo step ----------

        static object Lights(Dictionary<string, object> args, Transform root)
        {
            bool remove = J.Bool(args, "remove", true); // the SDK counts a light that is off as well, so off does nothing for the rank
            string label = remove ? "MioVRCA 移除灯光" : "MioVRCA 关闭灯光";
            Undo.IncrementCurrentGroup();
            int group = Undo.GetCurrentGroup();
            int n = 0; // (the list is cut short, the count is not)
            var items = new List<object>();
            foreach (Light l in root.GetComponentsInChildren<Light>(true))
            {
                if (l == null || Refl.EditorOnly(l.transform) || (!remove && !l.enabled)) continue;
                string path = PathOf(root, l.transform);
                if (remove) Undo.DestroyObjectImmediate(l);
                else { Undo.RecordObject(l, label); l.enabled = false; }
                n++;
                if (items.Count < ListMax) items.Add(Line(path, remove ? "已移除" : "已关闭"));
            }
            if (n > 0) { Undo.SetCurrentGroupName(label); Undo.CollapseUndoOperations(group); }
            return new Dictionary<string, object> { { "kind", "lights" }, { "changed", n }, { "items", items }, { "undo", n > 0 ? label : "" } };
        }

        // ---------- missing scripts: the components removed, one undo step ----------
        // A component of a plugin that is not installed (Modular Avatar, VRCFury) looks the same as one whose
        // script was deleted, and so does every component of a plugin that does not compile at the moment (after
        // an SDK update, say). Removing those loses their settings for good once the scene is saved. The second
        // case can be told: it is refused. The first cannot: the program's confirmation says it.

        static object Missing(Transform root)
        {
            const string label = "MioVRCA 移除丢失的脚本";
            if (EditorUtility.scriptCompilationFailed)
                throw new PipelineException("工程中有脚本编译错误，未执行移除：此时显示为丢失的脚本可能只是暂时无法加载（例如 Modular Avatar、VRCFury 在 SDK 更新后编译失败），移除后其设置无法找回。请先解决 Unity Console 中的编译错误，待编译完成后重新体检");
            Undo.IncrementCurrentGroup();
            int group = Undo.GetCurrentGroup();
            int n = 0;
            var items = new List<object>();
            foreach (Transform t in root.GetComponentsInChildren<Transform>(true))
            {
                if (Refl.EditorOnly(t) || GameObjectUtility.GetMonoBehavioursWithMissingScriptCount(t.gameObject) == 0) continue;
                Undo.RegisterCompleteObjectUndo(t.gameObject, label);
                int here = GameObjectUtility.RemoveMonoBehavioursWithMissingScript(t.gameObject);
                if (here == 0) continue;
                n += here;
                if (items.Count < ListMax) items.Add(new Dictionary<string, object> { { "t", PathOf(root, t) }, { "v", here }, { "u", "个" } });
            }
            if (n > 0) { Undo.SetCurrentGroupName(label); Undo.CollapseUndoOperations(group); }
            return new Dictionary<string, object> { { "kind", "missing" }, { "changed", n }, { "items", items }, { "undo", n > 0 ? label : "" } };
        }
    }
}
