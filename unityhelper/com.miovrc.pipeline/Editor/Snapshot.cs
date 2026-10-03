// A look at the avatar, for the AI and for the player: pictures of the avatar as the scene shows it now, from
// the front, the back, the sides, or close on the face or on one object. The camera frames the avatar by
// itself, so it does not matter where the Scene view is looking.
//
// Nothing in the scene is changed: what is shown or hidden for a picture is put back afterwards, and the files
// go to UserSettings/MioVRCA/shots (outside Assets, so Unity imports nothing).
using System;
using System.Collections.Generic;
using System.IO;
using UnityEditor;
using UnityEngine;

namespace MioVRCA.Pipeline
{
    internal static class Snapshot
    {
        const string Dir = "UserSettings/MioVRCA/shots";
        static readonly string[] Views = { "front", "back", "left", "right", "front_left", "front_right", "back_left", "back_right", "face" };

        public static object Run(Dictionary<string, object> args)
        {
            Component avatar = Refl.FindAvatar(J.Str(args, "avatar"));
            Transform root = avatar.transform;
            List<string> views = J.Strs(args, "views");
            if (views.Count == 0) views.Add("front");
            if (views.Count > 4) throw new PipelineException("一次最多拍 4 个角度");
            foreach (string v in views)
                if (Array.IndexOf(Views, v) < 0) throw new PipelineException("不认识的角度「" + v + "」，可以用：" + string.Join("、", Views));
            int size = Mathf.Clamp((int)J.Num(args, "size", 768), 256, 1280);
            string targetPath = J.Str(args, "target").Trim();
            Transform target = targetPath != "" ? Refl.FindUnder(root, targetPath) : null;
            var show = new List<Transform>();
            foreach (string p in J.Strs(args, "show")) show.Add(Refl.FindUnder(root, p));
            var hide = new List<Transform>();
            foreach (string p in J.Strs(args, "hide")) hide.Add(Refl.FindUnder(root, p));
            if (target != null && !show.Contains(target) && !target.gameObject.activeInHierarchy) show.Add(target);

            var woken = new List<GameObject>();
            var slept = new List<GameObject>();
            var dark = new List<Renderer>();
            var unlit = new List<Light>();
            GameObject camGo = null, lightGo = null;
            RenderTexture before = RenderTexture.active;
            var shots = new List<object>();
            try
            {
                // shown and hidden for the pictures only
                if (!root.gameObject.activeSelf) { root.gameObject.SetActive(true); woken.Add(root.gameObject); }
                foreach (Transform t in show)
                    for (Transform p = t; p != null && p != root; p = p.parent)
                        if (!p.gameObject.activeSelf) { p.gameObject.SetActive(true); woken.Add(p.gameObject); }
                foreach (Transform t in hide)
                    if (t != root && t.gameObject.activeSelf) { t.gameObject.SetActive(false); slept.Add(t.gameObject); }

                Bounds? whole = BoundsOf(root, root);
                if (!whole.HasValue) throw new PipelineException("头像「" + root.name + "」上没有显示着的网格，拍不出东西");
                Bounds? focus = target != null ? BoundsOf(target, root) : null;
                if (target != null && !focus.HasValue) throw new PipelineException("「" + targetPath + "」上没有显示着的网格");

                // only the avatar is in the picture, without what an upload strips (EditorOnly: placement helpers and the like)
                foreach (Renderer r in UnityEngine.Object.FindObjectsOfType<Renderer>())
                    if (r.enabled && !r.forceRenderingOff && (!r.transform.IsChildOf(root) || EditorOnly(r.transform, root))) { r.forceRenderingOff = true; dark.Add(r); }

                Vector3 fwd = root.forward;
                fwd.y = 0f;
                if (fwd.sqrMagnitude < 1e-4f) fwd = Vector3.forward;
                fwd.Normalize();
                Vector3 right = Vector3.Cross(Vector3.up, fwd);

                camGo = new GameObject("MioVRCA Snapshot Camera") { hideFlags = HideFlags.HideAndDontSave };
                var cam = camGo.AddComponent<Camera>();
                cam.enabled = false;
                cam.orthographic = false;
                cam.fieldOfView = 22f;
                cam.clearFlags = CameraClearFlags.SolidColor;
                cam.backgroundColor = new Color(0.20f, 0.21f, 0.25f);
                cam.allowHDR = false;

                // lit from where the camera stands, whatever the scene's own lights do (they are back on afterwards)
                foreach (Light l in UnityEngine.Object.FindObjectsOfType<Light>())
                    if (l.enabled) { l.enabled = false; unlit.Add(l); }
                lightGo = new GameObject("MioVRCA Snapshot Light") { hideFlags = HideFlags.HideAndDontSave };
                var light = lightGo.AddComponent<Light>();
                light.type = LightType.Directional;
                light.color = Color.white;
                light.intensity = 1f;
                light.shadows = LightShadows.None;

                Directory.CreateDirectory(Dir);
                Sweep();
                string stamp = DateTime.Now.ToString("yyyyMMdd_HHmmss_fff");
                foreach (string view in views)
                {
                    Bounds b = focus ?? whole.Value;
                    Vector3 dir = Direction(view, fwd, right);
                    if (view == "face")
                    {
                        b = FaceBounds(root, whole.Value);
                        dir = (fwd + right * 0.12f).normalized;
                    }
                    // what the camera sees of the box: its height, and its width across the line of sight
                    Vector3 side = Vector3.Cross(Vector3.up, dir).normalized;
                    float halfH = b.extents.y;
                    float halfW = Mathf.Abs(side.x) * b.extents.x + Mathf.Abs(side.z) * b.extents.z;
                    float halfD = Mathf.Abs(dir.x) * b.extents.x + Mathf.Abs(dir.z) * b.extents.z;
                    halfH = Mathf.Max(halfH, 0.02f) * 1.08f;
                    halfW = Mathf.Max(halfW, 0.02f) * 1.08f;
                    float aspect = Mathf.Clamp(halfW / halfH, 0.6f, 1.6f);
                    int w = aspect >= 1f ? size : Mathf.Max(64, Mathf.RoundToInt(size * aspect));
                    int h = aspect >= 1f ? Mathf.Max(64, Mathf.RoundToInt(size / aspect)) : size;
                    float half = Mathf.Max(halfH, halfW / aspect);
                    float dist = half / Mathf.Tan(cam.fieldOfView * 0.5f * Mathf.Deg2Rad) + halfD;
                    cam.aspect = (float)w / h;
                    cam.nearClipPlane = Mathf.Max(0.01f, dist - halfD - 2f);
                    cam.farClipPlane = dist + halfD + 6f;
                    cam.transform.SetPositionAndRotation(b.center + dir * dist, Quaternion.LookRotation(-dir, Vector3.up));
                    lightGo.transform.rotation = Quaternion.LookRotation(-dir + Vector3.down * 0.35f + side * 0.25f, Vector3.up);

                    string file = Path.GetFullPath(Path.Combine(Dir, stamp + "_" + view + ".jpg"));
                    Render(cam, w, h, file);
                    shots.Add(new Dictionary<string, object> { { "view", view }, { "file", file.Replace('\\', '/') }, { "width", w }, { "height", h } });
                }
            }
            finally
            {
                RenderTexture.active = before;
                foreach (Renderer r in dark) if (r != null) r.forceRenderingOff = false;
                foreach (Light l in unlit) if (l != null) l.enabled = true;
                for (int i = slept.Count - 1; i >= 0; i--) if (slept[i] != null) slept[i].SetActive(true);
                for (int i = woken.Count - 1; i >= 0; i--) if (woken[i] != null) woken[i].SetActive(false);
                if (camGo != null) { camGo.GetComponent<Camera>().targetTexture = null; UnityEngine.Object.DestroyImmediate(camGo); }
                if (lightGo != null) UnityEngine.Object.DestroyImmediate(lightGo);
            }

            // what was in the picture, so the pictures can be read: the avatar's own children, shown or not
            var visible = new List<object>();
            var hiddenNow = new List<object>();
            foreach (Transform c in root)
            {
                if (c.GetComponentsInChildren<Renderer>(true).Length == 0) continue;
                bool on = c.gameObject.activeSelf;
                if (show.Contains(c)) on = true;
                if (hide.Contains(c)) on = false;
                (on ? visible : hiddenNow).Add(c.name);
            }
            return new Dictionary<string, object>
            {
                { "avatar", root.name }, { "shots", shots }, { "visible", visible }, { "hidden", hiddenNow }, { "playing", EditorApplication.isPlaying },
                { "note", EditorApplication.isPlaying
                    ? "Play 模式里的画面：菜单开关的效果已经算进去了"
                    : "编辑模式里的画面：是场景里现在显示着的东西，菜单开关的效果没有算进去（要看开关效果，用 show / hide 临时显示或隐藏，或者进 Play 模式）" },
            };
        }

        static Vector3 Direction(string view, Vector3 fwd, Vector3 right)
        {
            switch (view)
            {
                case "back": return -fwd;
                case "left": return -right; // from the avatar's left side
                case "right": return right;
                case "front_left": return (fwd - right).normalized;
                case "front_right": return (fwd + right).normalized;
                case "back_left": return (-fwd - right).normalized;
                case "back_right": return (-fwd + right).normalized;
            }
            return fwd;
        }

        static bool EditorOnly(Transform t, Transform root)
        {
            for (; t != null; t = t.parent)
            {
                if (t.CompareTag("EditorOnly")) return true;
                if (t == root) break;
            }
            return false;
        }

        // every visible mesh under t, particles and trails left out
        static Bounds? BoundsOf(Transform t, Transform root)
        {
            Bounds? bounds = null;
            foreach (Renderer r in t.GetComponentsInChildren<Renderer>(false))
            {
                if (!r.enabled || r is ParticleSystemRenderer || r is TrailRenderer || r is LineRenderer || EditorOnly(r.transform, root)) continue;
                Bounds rb = Refl.TightBounds(r);
                if (rb.size.sqrMagnitude < 1e-10f) continue;
                if (bounds.HasValue) { Bounds b = bounds.Value; b.Encapsulate(rb); bounds = b; }
                else bounds = rb;
            }
            return bounds;
        }

        // around the head bone; without a humanoid rig, the top of the avatar
        static Bounds FaceBounds(Transform root, Bounds whole)
        {
            Animator an = root.GetComponent<Animator>();
            Transform head = an != null && an.isHuman ? an.GetBoneTransform(HumanBodyBones.Head) : null;
            float height = Mathf.Max(whole.size.y, 0.3f);
            float half = Mathf.Clamp(height * 0.13f, 0.09f, 0.4f);
            Vector3 center = head != null
                ? head.position + Vector3.up * half * 0.45f
                : new Vector3(whole.center.x, whole.max.y - half, whole.center.z);
            return new Bounds(center, new Vector3(half * 2f, half * 2f, half * 2f));
        }

        static void Render(Camera cam, int w, int h, string file)
        {
            RenderTexture rt = null;
            Texture2D tex = null;
            try
            {
                rt = new RenderTexture(w, h, 24, RenderTextureFormat.ARGB32, RenderTextureReadWrite.sRGB) { antiAliasing = 4 };
                cam.targetTexture = rt;
                cam.Render();
                RenderTexture.active = rt;
                tex = new Texture2D(w, h, TextureFormat.RGB24, false);
                tex.ReadPixels(new Rect(0, 0, w, h), 0, 0);
                tex.Apply();
                File.WriteAllBytes(file, tex.EncodeToJPG(88));
            }
            finally
            {
                cam.targetTexture = null;
                RenderTexture.active = null;
                if (rt != null) { rt.Release(); UnityEngine.Object.DestroyImmediate(rt); }
                if (tex != null) UnityEngine.Object.DestroyImmediate(tex);
            }
        }

        // pictures older than two days go
        static void Sweep()
        {
            try
            {
                foreach (string f in Directory.GetFiles(Dir, "*.jpg"))
                    if ((DateTime.Now - File.GetLastWriteTime(f)).TotalDays > 2) File.Delete(f);
            }
            catch (Exception) { }
        }
    }
}
