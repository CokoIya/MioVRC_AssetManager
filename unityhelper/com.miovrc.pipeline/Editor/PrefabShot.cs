// A picture of a prefab by itself, for the cover of an asset that has none: seen from the front, framed on its
// meshes, on the dark background of the program's cards.
//
// The prefab is drawn in a preview scene of its own (PreviewRenderUtility), so the open scene is not touched and
// nothing is left in it; the pictures go to Temp/MioVRCA/prefabshots (outside Assets, gone when Unity closes).
using System;
using System.Collections.Generic;
using System.IO;
using UnityEditor;
using UnityEngine;

namespace MioVRCA.Pipeline
{
    internal static class PrefabShot
    {
        const string Dir = "Temp/MioVRCA/prefabshots";
        const int MaxPrefabs = 20;
        static readonly Color Background = new Color32(40, 43, 61, 255); // the cards' cover background

        public static object Run(Dictionary<string, object> args)
        {
            if (EditorApplication.isPlayingOrWillChangePlaymode)
                throw new PipelineException("Unity 处于 Play 模式，请先退出 Play 模式再生成封面");
            List<string> prefabs = J.Strs(args, "prefabs");
            if (prefabs.Count == 0) throw new PipelineException("未指定要截图的 prefab（prefabs）");
            if (prefabs.Count > MaxPrefabs) throw new PipelineException("单次最多为 " + MaxPrefabs + " 个 prefab 截图");
            int size = Mathf.Clamp((int)J.Num(args, "size", 512), 128, 1024);
            Directory.CreateDirectory(Dir);
            Sweep();
            string stamp = DateTime.Now.ToString("yyyyMMdd_HHmmss_fff");
            var shots = new List<object>();
            for (int i = 0; i < prefabs.Count; i++)
            {
                string path = prefabs[i].Replace('\\', '/').Trim();
                var d = new Dictionary<string, object> { { "prefab", path } };
                try
                {
                    string file = Path.GetFullPath(Path.Combine(Dir, stamp + "_" + i + "_" + IconShot.SafeName(Path.GetFileNameWithoutExtension(path)) + ".png"));
                    Shoot(path, size, file);
                    d["file"] = file.Replace('\\', '/');
                    d["width"] = size;
                    d["height"] = size;
                }
                catch (PipelineException e) { d["error"] = e.Message; }
                catch (Exception e)
                {
                    // one prefab that cannot be drawn does not stop the others
                    d["error"] = "无法渲染该 prefab：" + e.Message;
                    Debug.LogWarning("[MioVRCA] " + path + ": " + e);
                }
                shots.Add(d);
            }
            return new Dictionary<string, object> { { "shots", shots }, { "size", size } };
        }

        static void Shoot(string path, int size, string file)
        {
            if (!path.StartsWith("Assets/") && !path.StartsWith("Packages/")) throw new PipelineException("prefab 路径须位于 Assets/ 下");
            var asset = AssetDatabase.LoadAssetAtPath<GameObject>(path);
            if (asset == null) throw new PipelineException("工程中未找到该 prefab");
            PreviewRenderUtility pru = null;
            Texture2D tex = null;
            RenderTexture before = RenderTexture.active;
            try
            {
                pru = new PreviewRenderUtility();
                // a copy in the preview scene: the asset and the open scene stay as they are
                GameObject go = pru.InstantiatePrefabInScene(asset);
                if (go == null) throw new PipelineException("无法实例化该 prefab");
                go.transform.position = Vector3.zero;
                go.transform.rotation = Quaternion.identity;
                if (!go.activeSelf) go.SetActive(true);

                Bounds? box = null;
                bool pink = false;
                foreach (Renderer r in go.GetComponentsInChildren<Renderer>(false))
                {
                    if (!r.enabled || r is ParticleSystemRenderer || r is TrailRenderer || r is LineRenderer || Refl.EditorOnly(r.transform)) continue;
                    foreach (Material m in r.sharedMaterials)
                        pink |= m != null && (m.shader == null || m.shader.name == "Hidden/InternalErrorShader");
                    // a skinned mesh is measured as it stands (bind pose): its own bounds are often one big box
                    Bounds rb = Refl.TightBounds(r);
                    if (rb.size.sqrMagnitude < 1e-10f) continue;
                    if (box.HasValue) { Bounds b = box.Value; b.Encapsulate(rb); box = b; }
                    else box = rb;
                }
                if (!box.HasValue) throw new PipelineException("该 prefab 中没有可显示的网格");
                if (pink) throw new PipelineException("该 prefab 的材质缺少着色器（显示为粉色），请先安装对应的着色器");

                // from the front (avatars and outfits face +Z), a little from above; the box fills the square
                Bounds bb = box.Value;
                Vector3 dir = new Vector3(0f, 0.12f, 1f).normalized;
                Camera cam = pru.camera;
                cam.fieldOfView = 20f;
                cam.clearFlags = CameraClearFlags.SolidColor;
                cam.backgroundColor = Background;
                cam.allowHDR = false;
                float half = Mathf.Max(Mathf.Max(bb.extents.y, bb.extents.x), 0.02f) * 1.1f;
                float depth = Mathf.Max(bb.extents.z, bb.extents.x);
                float dist = half / Mathf.Tan(cam.fieldOfView * 0.5f * Mathf.Deg2Rad) + depth;
                cam.nearClipPlane = Mathf.Max(0.01f, dist - depth * 2f - 1f);
                cam.farClipPlane = dist + depth * 2f + 5f;
                cam.transform.position = bb.center + dir * dist;
                cam.transform.rotation = Quaternion.LookRotation(-dir, Vector3.up);

                // lit from the camera's side, with enough ambient light that a toon shader's shadow side is not black
                pru.ambientColor = new Color(0.45f, 0.45f, 0.48f, 1f);
                Light[] lights = pru.lights;
                if (lights != null && lights.Length > 0)
                {
                    lights[0].intensity = 1f;
                    lights[0].color = Color.white;
                    lights[0].transform.rotation = Quaternion.LookRotation(-dir + Vector3.down * 0.35f + Vector3.right * 0.25f, Vector3.up);
                    for (int i = 1; i < lights.Length; i++) lights[i].intensity = 0f;
                }

                pru.BeginStaticPreview(new Rect(0, 0, size, size));
                pru.Render(false, false);
                tex = pru.EndStaticPreview();
                if (tex == null) throw new PipelineException("渲染失败");
                File.WriteAllBytes(file, tex.EncodeToPNG());
            }
            finally
            {
                RenderTexture.active = before;
                if (tex != null) UnityEngine.Object.DestroyImmediate(tex);
                if (pru != null) pru.Cleanup(); // the preview scene goes, with the copy, the camera and the lights in it
            }
        }

        // pictures older than a day go (the program copies what it wants right away)
        static void Sweep()
        {
            try
            {
                foreach (string f in Directory.GetFiles(Dir, "*.png"))
                    if ((DateTime.Now - File.GetLastWriteTime(f)).TotalDays > 1) File.Delete(f);
            }
            catch (Exception) { }
        }
    }
}
