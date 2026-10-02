// A menu icon for an outfit or a part of it: a 256 px picture of just those objects, from the front, on a
// transparent background, saved under Assets/MioVRCA/<avatar>/Icons.
using System;
using System.Collections.Generic;
using System.IO;
using UnityEditor;
using UnityEngine;

namespace MioVRCA.Pipeline
{
    internal static class IconShot
    {
        const int Size = 256;

        public static string SafeName(string s)
        {
            var sb = new System.Text.StringBuilder();
            foreach (char ch in s)
                sb.Append(ch < 0x20 || "\\/:*?\"<>|#%&{}$!'@+`=".IndexOf(ch) >= 0 ? '_' : ch);
            string o = sb.ToString().Trim().TrimEnd('.');
            return o == "" ? "_" : (o.Length > 60 ? o.Substring(0, 60) : o);
        }

        public static object Run(Dictionary<string, object> args)
        {
            Component avatar = Refl.FindAvatar(J.Str(args, "avatar"));
            var objs = new List<Transform>();
            foreach (string p in J.Strs(args, "objects")) objs.Add(Refl.FindUnder(avatar.transform, p));
            string file = J.Str(args, "file");
            if (file == "") file = "Assets/MioVRCA/" + SafeName(avatar.gameObject.name) + "/Icons/" + SafeName(objs.Count > 0 ? objs[0].name : "icon") + "_icon.png";
            Texture2D tex = Capture(avatar.transform, objs, file);
            return new Dictionary<string, object> { { "file", tex != null ? file : "" } };
        }

        // swaps: materials worn for the picture only (another colour of the outfit)
        public static Texture2D Capture(Transform avatar, List<Transform> targets, string assetPath, List<MenuBuilder.Swap> swaps = null)
        {
            if (targets.Count == 0) return null;
            var worn = new Dictionary<Renderer, Material[]>();
            // shown for the picture only: what the objects need to be visible is turned on, and put back after
            var woken = new List<GameObject>();
            var hidden = new List<Renderer>();
            GameObject camGo = null, lightGo = null;
            RenderTexture rt = null;
            Texture2D onBlack = null, onWhite = null, result = null;
            RenderTexture before = RenderTexture.active;
            try
            {
                foreach (Transform t in targets)
                    for (Transform p = t; p != null; p = p.parent)
                    {
                        if (!p.gameObject.activeSelf) { p.gameObject.SetActive(true); woken.Add(p.gameObject); }
                        if (p == avatar) break;
                    }
                if (swaps != null)
                    foreach (MenuBuilder.Swap s in swaps)
                    {
                        if (s.renderer == null) continue;
                        if (!worn.ContainsKey(s.renderer)) worn[s.renderer] = s.renderer.sharedMaterials;
                        Material[] ms = s.renderer.sharedMaterials;
                        if (s.index < ms.Length) { ms[s.index] = s.material; s.renderer.sharedMaterials = ms; }
                    }
                var mine = new HashSet<Renderer>();
                Bounds? bounds = null;
                foreach (Transform t in targets)
                    foreach (Renderer r in t.GetComponentsInChildren<Renderer>(false))
                    {
                        if (!r.enabled || r is ParticleSystemRenderer || r is TrailRenderer || r is LineRenderer) continue;
                        mine.Add(r);
                        Bounds rb = Refl.TightBounds(r);
                        if (bounds.HasValue) { Bounds b = bounds.Value; b.Encapsulate(rb); bounds = b; }
                        else bounds = rb;
                    }
                if (!bounds.HasValue || bounds.Value.size.sqrMagnitude < 1e-8f) return null;
                foreach (Renderer r in UnityEngine.Object.FindObjectsOfType<Renderer>())
                    if (r.enabled && !r.forceRenderingOff && !mine.Contains(r)) { r.forceRenderingOff = true; hidden.Add(r); }

                Vector3 fwd = avatar.forward;
                fwd.y = 0f;
                if (fwd.sqrMagnitude < 1e-4f) fwd = Vector3.forward;
                fwd.Normalize();
                Vector3 right = Vector3.Cross(Vector3.up, fwd);
                Bounds bb = bounds.Value;
                float halfW = Mathf.Abs(right.x) * bb.extents.x + Mathf.Abs(right.z) * bb.extents.z;
                float halfD = Mathf.Abs(fwd.x) * bb.extents.x + Mathf.Abs(fwd.z) * bb.extents.z;
                float half = Mathf.Max(bb.extents.y, halfW) * 1.12f + 0.005f;

                camGo = new GameObject("MioVRCA Icon Camera") { hideFlags = HideFlags.HideAndDontSave };
                var cam = camGo.AddComponent<Camera>();
                cam.enabled = false;
                cam.orthographic = true;
                cam.orthographicSize = half;
                cam.nearClipPlane = 0.01f;
                cam.farClipPlane = halfD * 2f + 4f;
                cam.clearFlags = CameraClearFlags.SolidColor;
                cam.allowHDR = false;
                cam.transform.SetPositionAndRotation(bb.center + fwd * (halfD + 2f), Quaternion.LookRotation(-fwd, Vector3.up));

                bool lit = false;
                foreach (Light l in UnityEngine.Object.FindObjectsOfType<Light>())
                    lit |= l.enabled && l.type == LightType.Directional;
                if (!lit)
                {
                    lightGo = new GameObject("MioVRCA Icon Light") { hideFlags = HideFlags.HideAndDontSave };
                    var light = lightGo.AddComponent<Light>();
                    light.type = LightType.Directional;
                    light.intensity = 1f;
                    lightGo.transform.rotation = Quaternion.LookRotation(-fwd + Vector3.down * 0.6f + right * 0.3f, Vector3.up);
                }

                rt = new RenderTexture(Size, Size, 24, RenderTextureFormat.ARGB32, RenderTextureReadWrite.sRGB) { antiAliasing = 4 };
                cam.targetTexture = rt;
                // drawn on black and on white: what differs is background, so the outline is right whatever the
                // materials write into alpha
                onBlack = Shot(cam, rt, Color.black);
                onWhite = Shot(cam, rt, Color.white);
                Color32[] k = onBlack.GetPixels32(), w = onWhite.GetPixels32();
                var px = new Color32[k.Length];
                bool any = false;
                for (int i = 0; i < k.Length; i++)
                {
                    int diff = Mathf.Max(w[i].r - k[i].r, Mathf.Max(w[i].g - k[i].g, w[i].b - k[i].b));
                    int a = Mathf.Clamp(255 - diff, 0, 255);
                    if (a == 0) { px[i] = new Color32(0, 0, 0, 0); continue; }
                    any = true;
                    px[i] = new Color32((byte)Mathf.Min(255, k[i].r * 255 / a), (byte)Mathf.Min(255, k[i].g * 255 / a), (byte)Mathf.Min(255, k[i].b * 255 / a), (byte)a);
                }
                if (!any) return null;
                result = new Texture2D(Size, Size, TextureFormat.RGBA32, false);
                result.SetPixels32(px);
                result.Apply();
                byte[] png = result.EncodeToPNG();

                string dir = Path.GetDirectoryName(assetPath).Replace('\\', '/');
                Directory.CreateDirectory(dir);
                File.WriteAllBytes(assetPath, png);
            }
            finally
            {
                RenderTexture.active = before;
                foreach (var kv in worn) if (kv.Key != null) kv.Key.sharedMaterials = kv.Value;
                foreach (Renderer r in hidden) if (r != null) r.forceRenderingOff = false;
                for (int i = woken.Count - 1; i >= 0; i--) if (woken[i] != null) woken[i].SetActive(false);
                if (camGo != null) { camGo.GetComponent<Camera>().targetTexture = null; UnityEngine.Object.DestroyImmediate(camGo); }
                if (lightGo != null) UnityEngine.Object.DestroyImmediate(lightGo);
                if (rt != null) { rt.Release(); UnityEngine.Object.DestroyImmediate(rt); }
                if (onBlack != null) UnityEngine.Object.DestroyImmediate(onBlack);
                if (onWhite != null) UnityEngine.Object.DestroyImmediate(onWhite);
                if (result != null) UnityEngine.Object.DestroyImmediate(result);
            }
            AssetDatabase.ImportAsset(assetPath, ImportAssetOptions.ForceUpdate);
            var imp = AssetImporter.GetAtPath(assetPath) as TextureImporter;
            if (imp != null && (!imp.alphaIsTransparency || imp.mipmapEnabled || imp.maxTextureSize != Size))
            {
                imp.alphaIsTransparency = true;
                imp.mipmapEnabled = false;
                imp.maxTextureSize = Size;
                imp.SaveAndReimport();
            }
            return AssetDatabase.LoadAssetAtPath<Texture2D>(assetPath);
        }

        static Texture2D Shot(Camera cam, RenderTexture rt, Color background)
        {
            cam.backgroundColor = background;
            cam.Render();
            RenderTexture.active = rt;
            var tex = new Texture2D(Size, Size, TextureFormat.RGB24, false);
            tex.ReadPixels(new Rect(0, 0, Size, Size), 0, 0);
            tex.Apply();
            return tex;
        }
    }
}
