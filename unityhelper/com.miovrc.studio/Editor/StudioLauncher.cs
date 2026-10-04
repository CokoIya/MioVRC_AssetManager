// MioVRCA — 摄影棚, the editor side.
//
// Opens the studio: from the menu (Tools/MioVRCA/摄影棚), or when MioVRCA asks for it by writing
// UserSettings/MioVRCA/studio/open.json ({"at": unix seconds, "lang": "zh|en|ja"}) — before it opens Unity, or while
// Unity is open. The request is taken once (the file is deleted) and only when it is recent. Opening = entering Play
// mode (Game view in front) and starting the studio there. Also gives the studio what it needs from the editor
// (leaving Play mode, showing a file, opening a folder, keeping Unity's own shortcuts out of the Game view), and
// carries an open studio over a script reload in Play mode: closed at once before it, opened again after it.
using System;
using System.Globalization;
using System.IO;
using System.Reflection;
using UnityEditor;
using UnityEngine;

namespace MioVRCA.Studio
{
    [InitializeOnLoad]
    internal static class StudioLauncher
    {
        const string PendingKey = "MioVRCA.Studio.Pending";
        const string ReopenKey = "MioVRCA.Studio.Reopen";
        const double MaxRequestAge = 15 * 60; // seconds: Unity may take a while to open and compile
        const long PendingLife = 180;          // seconds from asking to being in Play mode (with a domain reload)
        const long ReopenLife = 300;           // seconds from closing before a script reload to after it (big projects)

        static double nextPoll, nextBeat;
        static bool deleteWarned;
        static PropertyInfo shortcutSwitch;
        static bool shortcutSwitchLooked, shortcutsWarned;

        // open.json
        [Serializable]
        internal sealed class OpenRequest
        {
            public long at = 0;
            public string lang = "";
        }

        static StudioLauncher()
        {
            StudioHost.ExitPlayMode = () => { EditorApplication.isPlaying = false; };
            StudioHost.Reveal = p => EditorUtility.RevealInFinder(p);
            StudioHost.OpenFolder = d => EditorUtility.OpenWithDefaultApp(d);
            StudioHost.CaptureShortcuts = CaptureShortcuts;
            // the "pending" and "reopen" entries live in SessionState, so they outlive the domain reload of entering
            // Play mode and of a script reload
            EditorApplication.playModeStateChanged += PlayModeChanged;
            EditorApplication.update += Poll;
            AssemblyReloadEvents.beforeAssemblyReload += BeforeReload;

            // A fresh domain has no studio yet: shortcuts still switched off were left by one that never closed
            // (Unity crashed, or was killed, with the studio open).
            CaptureShortcuts(false);
            if (EditorApplication.isPlaying) ReopenAfterReload();
        }

        [MenuItem("Tools/MioVRCA/摄影棚")]
        static void OpenFromMenu()
        {
            Start("");
        }

        static void Start(string lang)
        {
            if (lang == null) lang = "";
            if (EditorApplication.isPlaying)
            {
                Studio.Open(lang);
                FocusGameView();
                return;
            }
            FocusGameView();
            SessionState.SetString(PendingKey, lang + "|" + Now().ToString(CultureInfo.InvariantCulture));
            Debug.Log("[MioVRCA] 正在进入摄影棚");
            if (EditorUtility.scriptCompilationFailed)
                Debug.LogWarning("[MioVRCA] 工程里有脚本编译错误，Unity 进不了 Play 模式，摄影棚打不开；请先解决 Console 里的红色错误");
            // when this fails (compile errors) the pending entry simply expires
            EditorApplication.EnterPlaymode();
        }

        static void PlayModeChanged(PlayModeStateChange change)
        {
            if (change == PlayModeStateChange.EnteredPlayMode)
            {
                string lang;
                if (TakeEntry(PendingKey, PendingLife, out lang)) Studio.Open(lang);
            }
            else if (change == PlayModeStateChange.EnteredEditMode)
            {
                SessionState.EraseString(ReopenKey); // Play mode ended before a reload finished: no studio now
                // the studio cleans up when Play mode ends; this only catches one that somehow did not
                Studio s = Studio.Current;
                if (s != null) s.Close(false);
                CaptureShortcuts(false);
            }
        }

        // Scripts are about to be reloaded in Play mode (a script was saved, MioVRCA updated the package, ...). The
        // studio would come back holding nothing, with its hidden camera and lights left behind. So it closes now:
        // the scene is put back, its settings saved and everything it made destroyed at once (a delayed Destroy
        // would not run before the reload). It opens again after the reload (the avatar's pose starts afresh).
        static void BeforeReload()
        {
            Studio s = Studio.Current;
            if (s != null && EditorApplication.isPlaying)
            {
                string lang = "";
                try { lang = s.Language; } catch (Exception) { }
                SessionState.SetString(ReopenKey, lang + "|" + Now().ToString(CultureInfo.InvariantCulture));
                try { s.CloseNow(); }
                catch (Exception e) { Debug.LogWarning("[MioVRCA] 脚本重新加载前没能关闭摄影棚：" + e); }
            }
            CaptureShortcuts(false);
        }

        // after a script reload in Play mode: opens the studio that BeforeReload closed
        static void ReopenAfterReload()
        {
            string lang;
            if (!TakeEntry(ReopenKey, ReopenLife, out lang)) return;
            // once the reload is over, not from inside it
            EditorApplication.delayCall += () =>
            {
                if (!EditorApplication.isPlaying || Studio.Current != null) return;
                Debug.Log("[MioVRCA] 脚本重新加载后，摄影棚重新打开");
                Studio.Open(lang);
            };
        }

        // Takes a "lang|unix seconds" entry from SessionState, once. False when there is none or it is old (a wish
        // from a Play mode that never came).
        static bool TakeEntry(string key, long life, out string lang)
        {
            lang = "";
            string p = SessionState.GetString(key, "");
            if (p == "") return false;
            SessionState.EraseString(key);
            int bar = p.LastIndexOf('|');
            long at;
            if (bar < 0 || !long.TryParse(p.Substring(bar + 1), NumberStyles.Integer, CultureInfo.InvariantCulture, out at)) return false;
            long age = Now() - at;
            if (age < -60 || age > life) return false;
            lang = p.Substring(0, bar);
            return true;
        }

        // ---- Unity's own shortcuts ----

        // EditorPrefs (outlives a crash, unlike SessionState), per project: what the Game view's "Unity Shortcuts"
        // switch was before the studio turned them off. Present only while they are off because of the studio.
        static string ShortcutsKey
        {
            get { return "MioVRCA.Studio.ShortcutsBefore:" + StudioHost.ProjectDir; }
        }

        // The Game view's "Unity Shortcuts" switch (internal ShortcutIntegration.ignoreWhenPlayModeFocused) is off
        // by default, so in Play mode Unity's menu shortcuts take their keys before the game: Ctrl+Z ran Edit/Undo,
        // undoing some earlier edit in the editor, instead of the studio's pose undo (Ctrl+Y: Edit/Redo). While the
        // studio is open they are kept out of the Game view; afterwards the switch is put back. Its setter also
        // writes EditorPrefs "GameView.IgnoreWhenPlayModeFocused", hence ShortcutsKey for the way back after a crash.
        static void CaptureShortcuts(bool on)
        {
            // also runs in the static constructor and before a reload: it must never throw
            try
            {
                string key = ShortcutsKey;
                if (!on && !EditorPrefs.HasKey(key)) return; // not switched by the studio
                PropertyInfo p = ShortcutSwitch();
                if (p == null)
                {
                    EditorPrefs.DeleteKey(key); // nothing was switched
                    return;
                }
                if (on)
                {
                    if (!EditorPrefs.HasKey(key)) EditorPrefs.SetBool(key, (bool)p.GetValue(null, null));
                    p.SetValue(null, true, null);
                }
                else
                {
                    bool before = EditorPrefs.GetBool(key, false);
                    EditorPrefs.DeleteKey(key);
                    p.SetValue(null, before, null);
                }
            }
            catch (Exception e)
            {
                WarnShortcuts(e.GetBaseException().Message);
            }
        }

        static PropertyInfo ShortcutSwitch()
        {
            if (shortcutSwitchLooked) return shortcutSwitch;
            shortcutSwitchLooked = true;
            try
            {
                Type t = typeof(EditorWindow).Assembly.GetType("UnityEditor.ShortcutManagement.ShortcutIntegration");
                PropertyInfo p = t != null
                    ? t.GetProperty("ignoreWhenPlayModeFocused", BindingFlags.Static | BindingFlags.Public | BindingFlags.NonPublic)
                    : null;
                if (p != null && p.PropertyType == typeof(bool) && p.CanRead && p.CanWrite) shortcutSwitch = p;
            }
            catch (Exception) { }
            if (shortcutSwitch == null) WarnShortcuts("ShortcutIntegration.ignoreWhenPlayModeFocused");
            return shortcutSwitch;
        }

        static void WarnShortcuts(string why)
        {
            if (shortcutsWarned) return;
            shortcutsWarned = true;
            Debug.LogWarning("[MioVRCA] 没能让 Unity 自带的快捷键避开摄影棚（" + why + "），Ctrl+Z 可能会执行 Unity 的撤销；" +
                             "在摄影棚里请用 Z / Shift+Z 撤销和重做姿势");
        }

        // ---- requests from MioVRCA ----

        // once a second: is there a request from MioVRCA?
        static void Poll()
        {
            double t = EditorApplication.timeSinceStartup;
            if (t < nextPoll) return;
            nextPoll = t + 1;
            Studio open = EditorApplication.isPlaying ? Studio.Current : null;
            if (open != null && t >= nextBeat)
            {
                nextBeat = t + 5;
                open.Heartbeat();
            }
            if (EditorApplication.isCompiling || EditorApplication.isUpdating || BuildPipeline.isBuildingPlayer) return;
            if (EditorApplication.isPlaying != EditorApplication.isPlayingOrWillChangePlaymode) return; // between modes

            string path;
            try
            {
                path = Path.Combine(StudioHost.DataDir, "open.json");
                if (!File.Exists(path)) return;
            }
            catch (Exception) { return; }
            string text = StudioHost.ReadText(path);
            if (text == null) return; // locked: next time
            try { File.Delete(path); }
            catch (Exception e)
            {
                // taken only once it is gone, so it is never taken twice
                if (!deleteWarned) Debug.LogWarning("[MioVRCA] 删不掉摄影棚的打开请求，稍后再试：" + e.Message);
                deleteWarned = true;
                return;
            }

            OpenRequest req;
            try { req = JsonUtility.FromJson<OpenRequest>(text); }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 摄影棚的打开请求读不懂：" + e.Message);
                return;
            }
            if (req == null || req.at <= 0)
            {
                Debug.LogWarning("[MioVRCA] 摄影棚的打开请求没有时间，已忽略");
                return;
            }
            long age = Now() - req.at;
            if (age > MaxRequestAge)
            {
                Debug.LogWarning("[MioVRCA] 摄影棚的打开请求已经过期（" + age + " 秒前），已忽略");
                return;
            }
            Start(req.lang);
        }

        static void FocusGameView()
        {
            try { EditorApplication.ExecuteMenuItem("Window/General/Game"); }
            catch (Exception) { }
        }

        static long Now()
        {
            return DateTimeOffset.UtcNow.ToUnixTimeSeconds();
        }
    }
}
