// MioVRCA — 摄影棚
//
// A photo studio inside Unity's Play mode: pose the avatar by dragging its bones, set hands, face and gaze, light it,
// frame it and take pictures. Everything the studio makes (camera, lights, backdrop, its own component) is created
// in Play mode with DontSave flags, so the scene is never changed and nothing goes into an upload. The assembly is
// compiled only inside the editor (defineConstraints UNITY_EDITOR).
// Settings and saved poses live in UserSettings/MioVRCA/studio; pictures go to Pictures/MioVRCA/<project>.
// To remove it, click 「移除摄影棚」 in the project's details in MioVRCA, or delete Packages/com.miovrc.studio.
using System;
using System.IO;
using UnityEngine;

namespace MioVRCA.Studio
{
    // What the studio needs from the editor. StudioLauncher (the editor assembly) fills these in; without them the
    // studio still works, it just cannot leave Play mode, show a file or open a folder by itself, and Unity's own
    // shortcuts keep Ctrl+Z (the studio's Z still works).
    public static class StudioHost
    {
        public static Action ExitPlayMode;
        public static Action<string> Reveal;     // shows a file selected in Explorer / Finder
        public static Action<string> OpenFolder; // opens a folder itself (Reveal on a folder shows its parent)

        // true: the studio opened, Unity's own shortcuts (Edit/Undo on Ctrl+Z, Edit/Redo on Ctrl+Y, ...) are kept out
        // of the Game view in Play mode so the keys reach the studio; false: the studio closed, they are as before
        public static Action<bool> CaptureShortcuts;

        public static string ProjectDir
        {
            get { return Path.GetDirectoryName(Application.dataPath); }
        }

        public static string ProjectName
        {
            get { return Path.GetFileName(ProjectDir); }
        }

        // UserSettings/MioVRCA/studio: outside Assets, so Unity imports nothing from it
        public static string DataDir
        {
            get { return Path.Combine(Path.Combine(Path.Combine(ProjectDir, "UserSettings"), "MioVRCA"), "studio"); }
        }

        // DataDir/running: rewritten every 5 s while the studio is open (the unix seconds in it, and its time), deleted
        // when it closes. MioVRCA refuses to remove the package while it is fresh.
        public static string RunningFile
        {
            get { return Path.Combine(DataDir, "running"); }
        }

        // the user's Pictures/MioVRCA/<project>, or UserSettings/MioVRCA/studio/photos when there is no Pictures folder
        public static string PhotoDir
        {
            get
            {
                string pics = "";
                try { pics = Environment.GetFolderPath(Environment.SpecialFolder.MyPictures); } catch (Exception) { }
                if (string.IsNullOrEmpty(pics)) return Path.Combine(DataDir, "photos");
                return Path.Combine(Path.Combine(pics, "MioVRCA"), SafeName(ProjectName));
            }
        }

        public static string SafeName(string s)
        {
            var sb = new System.Text.StringBuilder();
            foreach (char ch in s ?? "")
                sb.Append(ch < 0x20 || "\\/:*?\"<>|".IndexOf(ch) >= 0 ? '_' : ch);
            string o = sb.ToString().Trim().TrimEnd('.');
            return o == "" ? "_" : (o.Length > 60 ? o.Substring(0, 60) : o);
        }

        // writes through a temporary file, so a reader never sees half of it
        public static void WriteAtomic(string path, byte[] data)
        {
            Directory.CreateDirectory(Path.GetDirectoryName(path));
            string tmp = path + ".tmp";
            File.WriteAllBytes(tmp, data);
            if (File.Exists(path)) File.Delete(path);
            File.Move(tmp, path);
        }

        public static void WriteText(string path, string text)
        {
            WriteAtomic(path, new System.Text.UTF8Encoding(false).GetBytes(text));
        }

        public static string ReadText(string path)
        {
            try { return File.Exists(path) ? File.ReadAllText(path).TrimStart('﻿') : null; }
            catch (IOException) { return null; }
            catch (UnauthorizedAccessException) { return null; }
        }
    }
}
