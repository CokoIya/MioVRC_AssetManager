// MioVRCA — the Unity side of its AI assistant.
//
// The program and this editor talk through files in UserSettings/MioVRCA/bridge (no network port):
//   alive.json       written here every two seconds: the editor is open, what it is doing, the UnitySkills port
//   req_<id>.json    a request from the program: {"id": "...", "cmd": "...", "args": {...}}
//   run_<id>.json    the same file while the request is being carried out (so the program knows it was taken)
//   res_<id>.json    the answer: {"id": "...", "ok": true, "result": {...}} or {"ok": false, "error": "..."}
// Requests are carried out on the editor's main thread, one at a time, in the order of their names.
// Everything that changes the scene goes through Undo (Ctrl+Z takes it back) and the scene is never saved here.
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using UnityEditor;
using UnityEditor.SceneManagement;
using UnityEngine;
using Debug = UnityEngine.Debug;

namespace MioVRCA.Pipeline
{
    [InitializeOnLoad]
    internal static class Bridge
    {
        public const string Version = "1.2.1";
        const string Dir = "UserSettings/MioVRCA/bridge";

        const string SkillsAsked = "MioVRCA.Pipeline.SkillsAsked";

        static double nextAlive, nextScan, nextSweep;

        static Bridge()
        {
            EditorApplication.update += Tick;
            EditorApplication.quitting += () => { try { File.Delete(Path.Combine(Dir, "alive.json")); } catch (Exception) { } };
        }

        static void Tick()
        {
            double now = EditorApplication.timeSinceStartup;
            if (now >= nextAlive)
            {
                nextAlive = now + 2;
                try { WriteAlive(); } catch (Exception) { }
            }
            if (now < nextScan) return;
            nextScan = now + 0.25;
            string[] reqs;
            try
            {
                if (!Directory.Exists(Dir)) return;
                if (now >= nextSweep) { nextSweep = now + 30; Sweep(); }
                reqs = Directory.GetFiles(Dir, "req_*.json");
            }
            catch (Exception) { return; }
            if (reqs.Length == 0) return;
            Array.Sort(reqs, StringComparer.Ordinal);
            foreach (string r in reqs)
                if (Handle(r)) break; // one per tick; a file still being written waits for the next
        }

        // Requests nobody is waiting for any more are dropped unread (the program gives up after two minutes, and
        // a change asked for long ago must not happen when the editor is opened later); old answers are cleared.
        const double RequestLife = 120, AnswerLife = 600;

        static void Sweep()
        {
            DateTime now = DateTime.UtcNow;
            foreach (string f in Directory.GetFiles(Dir))
            {
                string n = Path.GetFileName(f);
                double age;
                try { age = (now - File.GetLastWriteTimeUtc(f)).TotalSeconds; }
                catch (Exception) { continue; }
                bool old = (n.StartsWith("req_") && age > RequestLife) || ((n.StartsWith("res_") || n.StartsWith("run_") || n.EndsWith(".tmp")) && age > AnswerLife);
                if (!old) continue;
                try { File.Delete(f); } catch (Exception) { }
            }
        }

        static void WriteAlive()
        {
            Directory.CreateDirectory(Dir);
            var d = new Dictionary<string, object>();
            d["bridge"] = Version;
            d["pid"] = Process.GetCurrentProcess().Id;
            d["time"] = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
            d["unity"] = Application.unityVersion;
            d["project"] = Path.GetFullPath(".");
            d["compiling"] = EditorApplication.isCompiling || EditorApplication.isUpdating;
            d["playing"] = EditorApplication.isPlayingOrWillChangePlaymode;
            d["ma"] = Refl.PackageVersion(Refl.MA + "ModularAvatarMenuItem");
            d["sdk"] = Refl.DescriptorType != null;
            d["skills"] = Skills();
            WriteAtomic(Path.Combine(Dir, "alive.json"), MiniJson.Write(d));
        }

        // UnitySkills in this editor: installed, serving, on which port. With want_skills next to the requests
        // (the program puts it there when the player turns the AI assistant on for this project) its server is
        // started once per editor session when it is not running.
        static Dictionary<string, object> Skills()
        {
            var d = new Dictionary<string, object>();
            Type server = Refl.Find("UnitySkills.SkillsHttpServer");
            d["installed"] = server != null;
            if (server == null) return d;
            try
            {
                bool running = (bool)server.GetProperty("IsRunning", BindingFlags.Public | BindingFlags.Static).GetValue(null, null);
                if (!running && !SessionState.GetBool(SkillsAsked, false) && File.Exists(Path.Combine(Dir, "want_skills")) && !EditorApplication.isCompiling)
                {
                    SessionState.SetBool(SkillsAsked, true); // once per editor session: a server the player stopped stays stopped
                    MethodInfo start = server.GetMethod("Start", BindingFlags.Public | BindingFlags.Static);
                    if (start != null)
                    {
                        ParameterInfo[] ps = start.GetParameters();
                        var args = new object[ps.Length];
                        for (int i = 0; i < ps.Length; i++) args[i] = ps[i].ParameterType == typeof(bool) ? (object)true : (ps[i].ParameterType == typeof(int) ? (object)0 : null);
                        start.Invoke(null, args);
                        running = (bool)server.GetProperty("IsRunning", BindingFlags.Public | BindingFlags.Static).GetValue(null, null);
                    }
                }
                d["running"] = running;
                d["port"] = running ? (int)server.GetProperty("Port", BindingFlags.Public | BindingFlags.Static).GetValue(null, null) : 0;
                d["version"] = Refl.PackageVersion("UnitySkills.SkillsHttpServer");
                Type mode = Refl.Find("UnitySkills.SkillsModeManager");
                PropertyInfo cur = mode != null ? mode.GetProperty("CurrentMode", BindingFlags.Public | BindingFlags.Static) : null;
                if (cur != null) d["mode"] = Convert.ToString(cur.GetValue(null, null));
            }
            catch (Exception e)
            {
                d["error"] = e.InnerException != null ? e.InnerException.Message : e.Message;
            }
            return d;
        }

        // false: nothing was done with this file (it is still being written, or too old)
        static bool Handle(string reqFile)
        {
            string id = Path.GetFileNameWithoutExtension(reqFile).Substring(4);
            var res = new Dictionary<string, object>();
            res["id"] = id;
            string runFile = Path.Combine(Dir, "run_" + id + ".json");
            string text;
            try
            {
                if ((DateTime.UtcNow - File.GetLastWriteTimeUtc(reqFile)).TotalSeconds > RequestLife) { File.Delete(reqFile); return false; }
                if (File.Exists(runFile)) File.Delete(runFile);
                File.Move(reqFile, runFile); // taken: from here on it is carried out exactly once
                text = File.ReadAllText(runFile);
            }
            catch (IOException) { return false; }
            catch (UnauthorizedAccessException) { return false; }
            try
            {
                var req = J.Obj(MiniJson.Parse(text));
                string cmd = J.Str(req, "cmd");
                var args = J.Obj(J.Get(req, "args"));
                res["cmd"] = cmd;
                res["result"] = Run(cmd, args);
                res["ok"] = true;
            }
            catch (Exception e)
            {
                Exception inner = e is TargetInvocationException && e.InnerException != null ? e.InnerException : e;
                res["ok"] = false;
                res["error"] = inner is PipelineException || inner is FormatException ? inner.Message : inner.GetType().Name + ": " + inner.Message;
                if (!(inner is PipelineException)) Debug.LogWarning("[MioVRCA] " + inner);
            }
            try { WriteAtomic(Path.Combine(Dir, "res_" + id + ".json"), MiniJson.Write(res)); }
            catch (Exception e) { Debug.LogWarning("[MioVRCA] 结果没写出去：" + e.Message); }
            try { File.Delete(runFile); } catch (Exception) { }
            // a long step left the heartbeat stale; the program's next request should not find the editor "gone"
            try { WriteAlive(); nextAlive = EditorApplication.timeSinceStartup + 2; } catch (Exception) { }
            return true;
        }

        static object Run(string cmd, Dictionary<string, object> args)
        {
            if (cmd == "ping") return new Dictionary<string, object> { { "bridge", Version } };
            if (EditorApplication.isCompiling || EditorApplication.isUpdating)
                throw new PipelineException("Unity 正在编译或导入，请稍后重试");
            switch (cmd)
            {
                case "inspect": return AvatarInspect.Overview(args);
                case "inspect_object": return AvatarInspect.ObjectDetail(args);
                case "prefabs": return AvatarInspect.Prefabs(args);
                case "snapshot": return Snapshot.Run(args);
                case "refresh":
                    AssetDatabase.Refresh();
                    return new Dictionary<string, object> { { "refreshed", true } };
            }
            if (EditorApplication.isPlayingOrWillChangePlaymode)
                throw new PipelineException("Unity 处于 Play 模式，请先退出 Play 模式再修改场景");
            switch (cmd)
            {
                case "dress": return Dresser.Dress(args);
                case "build_menu": return MenuBuilder.Build(args);
                case "icons": return IconShot.Run(args);
                case "place_avatar": return PlaceAvatar(args);
                case "undo":
                    {
                        string what = Undo.GetCurrentGroupName();
                        Undo.PerformUndo();
                        Undo.IncrementCurrentGroup();
                        return new Dictionary<string, object> { { "undone", what } };
                    }
                case "select":
                    {
                        Component avatar = Refl.FindAvatar(J.Str(args, "avatar"));
                        string path = J.Str(args, "path");
                        GameObject go = path == "" ? avatar.gameObject : Refl.FindUnder(avatar.transform, path).gameObject;
                        Selection.activeGameObject = go;
                        EditorGUIUtility.PingObject(go);
                        return new Dictionary<string, object> { { "selected", Refl.ScenePath(go.transform) } };
                    }
            }
            throw new PipelineException("不认识的操作：" + cmd);
        }

        // A new scene with one avatar in it, saved as Assets/<project>/<project>.unity (numbered when that is taken).
        // Nothing unsaved is thrown away: a dirty scene stops this before anything happens.
        static object PlaceAvatar(Dictionary<string, object> args)
        {
            string prefab = J.Str(args, "prefab").Replace('\\', '/').Trim();
            if (prefab == "") throw new PipelineException("未指定要放置的 prefab（prefab）");
            var go = AssetDatabase.LoadAssetAtPath<GameObject>(prefab);
            if (go == null) throw new PipelineException("工程中未找到该 prefab：" + prefab);
            if (Refl.DescriptorType == null) throw new PipelineException("工程未安装 VRChat SDK（Avatars）");
            if (go.GetComponentInChildren(Refl.DescriptorType, true) == null)
                throw new PipelineException("「" + go.name + "」不是完整的模型（缺少 VRC Avatar Descriptor），应选择素体自带的模型 prefab");
            for (int i = 0; i < UnityEngine.SceneManagement.SceneManager.sceneCount; i++)
            {
                var s = UnityEngine.SceneManagement.SceneManager.GetSceneAt(i);
                if (s.isDirty) throw new PipelineException("当前打开的场景「" + (s.name == "" ? "Untitled" : s.name) + "」有未保存的改动。请先在 Unity 中按 Ctrl+S 保存（或放弃改动），再新建场景");
            }
            string project = Path.GetFileName(Path.GetDirectoryName(Application.dataPath.TrimEnd('/')));
            string folderName = IconShot.SafeName(J.Str(args, "folder", project));
            if (folderName == "") folderName = "Avatar";
            string scenePath = J.Str(args, "scene").Replace('\\', '/').Trim();
            if (scenePath == "")
            {
                if (!AssetDatabase.IsValidFolder("Assets/" + folderName)) AssetDatabase.CreateFolder("Assets", folderName);
                string baseName = IconShot.SafeName(J.Str(args, "name", folderName));
                if (baseName == "") baseName = "Avatar";
                scenePath = "Assets/" + folderName + "/" + baseName + ".unity";
                for (int n = 2; File.Exists(scenePath); n++) scenePath = "Assets/" + folderName + "/" + baseName + "_" + n + ".unity";
            }
            else
            {
                if (!scenePath.StartsWith("Assets/") || !scenePath.EndsWith(".unity")) throw new PipelineException("场景路径须位于 Assets/ 下并以 .unity 结尾：" + scenePath);
                if (File.Exists(scenePath)) throw new PipelineException("场景已存在：" + scenePath);
                string dir = Path.GetDirectoryName(scenePath).Replace('\\', '/');
                if (!AssetDatabase.IsValidFolder(dir)) Directory.CreateDirectory(dir);
            }
            var scene = EditorSceneManager.NewScene(NewSceneSetup.DefaultGameObjects, NewSceneMode.Single);
            var inst = (GameObject)PrefabUtility.InstantiatePrefab(go, scene);
            inst.name = go.name;
            inst.transform.position = Vector3.zero;
            inst.transform.rotation = Quaternion.identity;
            if (!EditorSceneManager.SaveScene(scene, scenePath)) throw new PipelineException("场景保存失败：" + scenePath);
            AssetDatabase.Refresh();
            Selection.activeGameObject = inst;
            EditorGUIUtility.PingObject(inst);
            return new Dictionary<string, object>
            {
                { "scene", scenePath }, { "avatar", inst.name }, { "prefab", prefab },
                { "note", "新场景已保存，里面放好了「" + inst.name + "」（还有默认的相机和灯光）。接下来的穿衣服、做菜单都在这个场景里进行" },
            };
        }

        // A copy of the scene file as it is on disk, taken once before this package first changes the scene in an
        // editor session: Assets/_Backup/<scene>_<time>.unity.
        public static string BackupScene(GameObject inScene)
        {
            string path = inScene.scene.path;
            if (string.IsNullOrEmpty(path)) return "";
            string key = "MioVRCA.Pipeline.Backup." + path;
            string done = SessionState.GetString(key, "");
            if (done != "" && File.Exists(done)) return done; // still there: one copy per editor session is enough
            if (!AssetDatabase.IsValidFolder("Assets/_Backup")) AssetDatabase.CreateFolder("Assets", "_Backup");
            string to = "Assets/_Backup/" + Path.GetFileNameWithoutExtension(path) + "_" + DateTime.Now.ToString("yyyyMMdd_HHmmss") + ".unity";
            if (!AssetDatabase.CopyAsset(path, to)) return "";
            SessionState.SetString(key, to);
            return to;
        }

        static void WriteAtomic(string path, string text)
        {
            string tmp = path + ".tmp";
            File.WriteAllText(tmp, text, new System.Text.UTF8Encoding(false));
            if (File.Exists(path))
            {
                // swapped in one step, so a reader never finds the file missing
                try { File.Replace(tmp, path, null); return; }
                catch (Exception) { File.Delete(path); }
            }
            File.Move(tmp, path);
        }
    }
}
