// MioVRCA — 工程封面
//
// Takes a front picture of the avatar in the open scene for this project's card in MioVRCA:
// once when the project is opened, and a moment after a scene is saved or opened. The picture is written to
// UserSettings/MioVRCA/cover_<time>.png (outside Assets, so Unity imports nothing) and the older ones are deleted.
// Nothing in the scene changes: other objects are only left out of that one picture, and the camera (and a light,
// when the scene has none) exist just for it. Editor only, so it never goes into an upload.
// To remove it, click 「关闭封面」 on the project card, or delete Packages/com.miovrc.projectcard.
using System;
using System.IO;
using System.Collections.Generic;
using UnityEditor;
using UnityEditor.SceneManagement;
using UnityEngine;
using UnityEngine.SceneManagement;

namespace MioVRCA.ProjectCard
{
    [InitializeOnLoad]
    internal static class ProjectCover
    {
        const string Folder = "UserSettings/MioVRCA";
        const string TakenKey = "MioVRCA.ProjectCard.Taken";
        const int Width = 960, Height = 720;
        const float Fov = 20f;

        static double due = -1;

        static ProjectCover()
        {
            EditorSceneManager.sceneSaved += scene => Later(2);
            EditorSceneManager.sceneOpened += (scene, mode) => Later(3);
            if (!SessionState.GetBool(TakenKey, false)) // once per editor session, not after every script reload
                Later(5);
        }

        [MenuItem("Tools/MioVRCA/截取工程封面")]
        static void TakeNow()
        {
            string err = Take();
            if (err == null) Debug.Log("[MioVRCA] 工程封面已更新：" + Path.GetFullPath(Folder));
            else Debug.LogWarning("[MioVRCA] " + err);
        }

        static void Later(double seconds)
        {
            bool waiting = due >= 0;
            due = EditorApplication.timeSinceStartup + seconds;
            if (!waiting) EditorApplication.update += Tick;
        }

        static void Tick()
        {
            if (EditorApplication.timeSinceStartup < due) return;
            if (EditorApplication.isCompiling || EditorApplication.isUpdating || EditorApplication.isPlayingOrWillChangePlaymode ||
                BuildPipeline.isBuildingPlayer)
            {
                due = EditorApplication.timeSinceStartup + 5;
                return;
            }
            EditorApplication.update -= Tick;
            due = -1;
            SessionState.SetBool(TakenKey, true);
            try { Take(); }
            catch (Exception e) { Debug.LogWarning("[MioVRCA] 工程封面没拍成：" + e.Message); }
        }

        // Take returns why there is no picture, or null.
        static string Take()
        {
            Type descriptor = FindType("VRC.SDKBase.VRC_AvatarDescriptor");
            Component avatar = descriptor != null ? FirstActive(descriptor) : null;
            Transform root = avatar != null ? avatar.transform : FirstHumanoid();
            if (root == null) return "场景里没有模型（Avatar Descriptor）";

            // how tall it is: from the eye height the avatar is set up with, else from what is drawn
            float baseY = root.position.y, height = 0f;
            if (avatar != null)
            {
                var f = descriptor.GetField("ViewPosition");
                if (f != null && f.GetValue(avatar) is Vector3 view && view.y > 0.05f)
                    height = view.y * Mathf.Abs(root.lossyScale.y) / 0.92f;
            }
            if (height <= 0f)
            {
                Bounds? b = DrawnBounds(root);
                if (b.HasValue) height = b.Value.max.y - baseY;
            }
            if (height < 0.05f) return "量不出模型的高度";

            // from the thighs to just above the head, straight from the front
            float bottom = baseY + 0.25f * height, top = baseY + 1.08f * height;
            Vector3 fwd = root.forward;
            fwd.y = 0f;
            if (fwd.sqrMagnitude < 1e-4f) fwd = Vector3.forward;
            fwd.Normalize();
            Vector3 center = new Vector3(root.position.x, (top + bottom) / 2f, root.position.z);
            float dist = (top - bottom) / 2f / Mathf.Tan(Fov / 2f * Mathf.Deg2Rad);

            var left = new List<Renderer>();
            GameObject camGo = null, lightGo = null;
            RenderTexture rt = null;
            Texture2D tex = null;
            RenderTexture before = RenderTexture.active;
            try
            {
                camGo = new GameObject("MioVRCA Cover Camera") { hideFlags = HideFlags.HideAndDontSave };
                var cam = camGo.AddComponent<Camera>();
                cam.enabled = false;
                cam.fieldOfView = Fov;
                cam.nearClipPlane = Mathf.Max(0.01f, dist - height * 2f);
                cam.farClipPlane = dist + height * 4f;
                cam.clearFlags = CameraClearFlags.SolidColor;
                cam.backgroundColor = new Color(0.32f, 0.33f, 0.36f, 1f);
                cam.allowHDR = false;
                cam.transform.SetPositionAndRotation(center + fwd * dist, Quaternion.LookRotation(-fwd, Vector3.up));

                // only the avatar in the picture (forceRenderingOff is not saved with the scene)
                foreach (var r in UnityEngine.Object.FindObjectsOfType<Renderer>())
                {
                    if (r.enabled && !r.forceRenderingOff && !r.transform.IsChildOf(root))
                    {
                        r.forceRenderingOff = true;
                        left.Add(r);
                    }
                }
                bool lit = false;
                foreach (var l in UnityEngine.Object.FindObjectsOfType<Light>())
                    lit |= l.enabled && l.type == LightType.Directional;
                if (!lit)
                {
                    lightGo = new GameObject("MioVRCA Cover Light") { hideFlags = HideFlags.HideAndDontSave };
                    var light = lightGo.AddComponent<Light>();
                    light.type = LightType.Directional;
                    light.intensity = 1f;
                    Vector3 side = Vector3.Cross(Vector3.up, fwd);
                    lightGo.transform.rotation = Quaternion.LookRotation(-fwd + Vector3.down * 0.6f + side * 0.3f, Vector3.up);
                }

                rt = new RenderTexture(Width, Height, 24, RenderTextureFormat.ARGB32, RenderTextureReadWrite.sRGB) { antiAliasing = 4 };
                cam.targetTexture = rt;
                cam.Render();
                RenderTexture.active = rt;
                tex = new Texture2D(Width, Height, TextureFormat.RGB24, false);
                tex.ReadPixels(new Rect(0, 0, Width, Height), 0, 0);
                tex.Apply();
                Save(tex.EncodeToPNG());
                return null;
            }
            finally
            {
                RenderTexture.active = before;
                foreach (var r in left)
                    if (r != null) r.forceRenderingOff = false;
                if (camGo != null) { camGo.GetComponent<Camera>().targetTexture = null; UnityEngine.Object.DestroyImmediate(camGo); }
                if (lightGo != null) UnityEngine.Object.DestroyImmediate(lightGo);
                if (rt != null) { rt.Release(); UnityEngine.Object.DestroyImmediate(rt); }
                if (tex != null) UnityEngine.Object.DestroyImmediate(tex);
            }
        }

        static void Save(byte[] png)
        {
            Directory.CreateDirectory(Folder);
            string path = Path.Combine(Folder, "cover_" + DateTimeOffset.UtcNow.ToUnixTimeSeconds() + ".png");
            string tmp = path + ".tmp";
            File.WriteAllBytes(tmp, png);
            if (File.Exists(path)) File.Delete(path);
            File.Move(tmp, path);
            string keep = Path.GetFullPath(path);
            foreach (string old in Directory.GetFiles(Folder, "cover_*.png"))
            {
                if (string.Equals(Path.GetFullPath(old), keep, StringComparison.OrdinalIgnoreCase)) continue;
                try { File.Delete(old); } catch (IOException) { } catch (UnauthorizedAccessException) { }
            }
        }

        static Type FindType(string name)
        {
            foreach (var asm in AppDomain.CurrentDomain.GetAssemblies())
            {
                Type t = asm.GetType(name, false);
                if (t != null) return t;
            }
            return null;
        }

        // the first active one, in hierarchy order
        static Component FirstActive(Type type)
        {
            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                Scene scene = SceneManager.GetSceneAt(i);
                if (!scene.isLoaded) continue;
                foreach (GameObject go in scene.GetRootGameObjects())
                {
                    if (!go.activeInHierarchy) continue;
                    Component c = go.GetComponentInChildren(type, false);
                    if (c != null) return c;
                }
            }
            return null;
        }

        // without the VRChat SDK: a humanoid model
        static Transform FirstHumanoid()
        {
            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                Scene scene = SceneManager.GetSceneAt(i);
                if (!scene.isLoaded) continue;
                foreach (GameObject go in scene.GetRootGameObjects())
                {
                    if (!go.activeInHierarchy) continue;
                    foreach (Animator a in go.GetComponentsInChildren<Animator>(false))
                        if (a.isHuman) return a.transform;
                }
            }
            return null;
        }

        static Bounds? DrawnBounds(Transform root)
        {
            Bounds? all = null;
            foreach (Renderer r in root.GetComponentsInChildren<Renderer>(false))
            {
                if (!r.enabled || r is ParticleSystemRenderer || r is TrailRenderer || r is LineRenderer) continue;
                if (all.HasValue) { Bounds b = all.Value; b.Encapsulate(r.bounds); all = b; }
                else all = r.bounds;
            }
            return all;
        }
    }
}
