// What is behind the avatar: the scene as it is, a solid colour, a vertical gradient, or nothing (a transparent
// picture). Except for Scene, everything that is not the avatar is hidden while the studio is open: renderers
// (forceRenderingOff, not saved), terrains and world-space canvases (put back as they were when it closes).
using System.Collections.Generic;
using UnityEngine;
using UnityEngine.Rendering;

namespace MioVRCA.Studio
{
    internal enum BackdropMode { Scene, Solid, Gradient, Transparent }

    internal sealed class Backdrop
    {
        public static readonly Color[] SolidSwatches =
        {
            new Color(0.96f, 0.96f, 0.97f), new Color(0.2f, 0.21f, 0.25f), new Color(0.08f, 0.08f, 0.11f),
            new Color(1f, 0.85f, 0.88f), new Color(0.8f, 0.9f, 1f), new Color(0.86f, 0.97f, 0.9f), new Color(1f, 0.95f, 0.8f),
        };

        // top and bottom colours of the gradient presets, and their names (through L.T)
        public static readonly string[] GradientNames = { "晴空", "樱花", "黄昏", "夜色", "影棚灰" };
        public static readonly Color[] GradientTop =
        {
            new Color(0.36f, 0.62f, 0.95f), new Color(1f, 0.82f, 0.88f), new Color(0.98f, 0.62f, 0.45f),
            new Color(0.08f, 0.09f, 0.2f), new Color(0.55f, 0.56f, 0.6f),
        };
        public static readonly Color[] GradientBottom =
        {
            new Color(0.86f, 0.93f, 1f), new Color(1f, 0.96f, 0.97f), new Color(0.4f, 0.32f, 0.55f),
            new Color(0.26f, 0.2f, 0.4f), new Color(0.2f, 0.2f, 0.23f),
        };

        public BackdropMode Mode = BackdropMode.Gradient;
        public Color Solid = new Color(0.96f, 0.96f, 0.97f);
        public int Gradient;

        public bool Transparent
        {
            get { return Mode == BackdropMode.Transparent; }
        }

        const float RescanSeconds = 2f;
        const int Steps = 8;             // gradient bands across the view
        const float Overscan = 4f;       // the quad reaches this many view half-heights up and down
        static readonly Color SceneGrey = new Color(0.32f, 0.33f, 0.36f, 1f);
        static readonly Color ViewGrey = new Color(0.42f, 0.43f, 0.46f, 1f); // Transparent, in the Game view only

        GameObject quad;
        Mesh mesh;
        Material mat;
        MeshRenderer quadRenderer;
        bool noShader;
        Color[] colors;
        int shownGradient = -1;
        bool shownLinear;
        Vector3 shownScale;
        float shownZ;

        // what Apply set last, for EndShot
        CameraClearFlags flags = CameraClearFlags.SolidColor;
        Color background = SceneGrey;
        bool quadOn;

        // what this hid, for the avatar hiddenFor: renderers (forceRenderingOff); terrains, which draw without a
        // Renderer (heightmap, trees and details off, their settings before); world-space canvases (switched off)
        readonly List<Renderer> hidden = new List<Renderer>();
        readonly List<HiddenTerrain> hiddenTerrains = new List<HiddenTerrain>();
        readonly List<Canvas> hiddenCanvases = new List<Canvas>();
        readonly List<Canvas> canvasScan = new List<Canvas>();
        bool hiding;
        Transform hiddenFor;
        float nextScan;

        // the backdrop quad (DontSave, hidden), parented to the camera
        public void Create(StudioCamera view)
        {
            // a big quad far in front of the camera with vertex colours and the built-in Sprites/Default material,
            // so it needs no shader of its own and writes no depth
            if (quad != null || view == null || view.Cam == null) return;
            // the quad went with a camera destroyed before this: do not leave its mesh and material behind
            if (mesh != null) Object.DestroyImmediate(mesh);
            if (mat != null) Object.DestroyImmediate(mat);
            Shader shader = Shader.Find("Sprites/Default");
            if (shader == null) shader = Shader.Find("UI/Default");
            if (shader == null)
            {
                noShader = true;
                Debug.LogWarning("[MioVRCA] 找不到 Sprites/Default 着色器，渐变背景改用纯色");
                return;
            }

            // rows from the bottom: overscan, then Steps + 1 rows across the view (y -1..1), then overscan; the colour
            // runs in gamma space across the view, so the gradient looks like the swatch in the panel
            int rows = Steps + 3;
            var v = new Vector3[rows * 2];
            var uv = new Vector2[rows * 2];
            for (int r = 0; r < rows; r++)
            {
                float y = RowY(r);
                v[r * 2] = new Vector3(-1f, y, 0f);
                v[r * 2 + 1] = new Vector3(1f, y, 0f);
                uv[r * 2] = new Vector2(0f, 0f);
                uv[r * 2 + 1] = new Vector2(1f, 0f);
            }
            var tris = new int[(rows - 1) * 6];
            for (int r = 0, i = 0; r < rows - 1; r++)
            {
                int a = r * 2, b = a + 1, c = a + 2, d = a + 3; // bottom-left, bottom-right, top-left, top-right
                tris[i++] = a; tris[i++] = c; tris[i++] = b;
                tris[i++] = c; tris[i++] = d; tris[i++] = b;
            }
            colors = new Color[rows * 2];
            for (int i = 0; i < colors.Length; i++) colors[i] = Color.white;
            mesh = new Mesh { name = "MioVRCA Studio Backdrop", hideFlags = HideFlags.HideAndDontSave };
            mesh.vertices = v;
            mesh.uv = uv;
            mesh.colors = colors;
            mesh.triangles = tris;
            mesh.RecalculateBounds();

            // drawn first (Background queue) without depth: everything else draws over it, also blended materials
            // in queues below Transparent, and depth-based effects still see the background as far away
            mat = new Material(shader) { name = "MioVRCA Studio Backdrop", hideFlags = HideFlags.HideAndDontSave };
            mat.renderQueue = (int)RenderQueue.Background;

            quad = new GameObject("MioVRCA Studio Backdrop") { hideFlags = HideFlags.HideAndDontSave };
            quad.layer = 0;
            quad.transform.SetParent(view.Cam.transform, false);
            quad.transform.localPosition = Vector3.zero; // Fit places and sizes it before it is shown
            quad.transform.localRotation = Quaternion.identity;
            quad.AddComponent<MeshFilter>().sharedMesh = mesh;
            quadRenderer = quad.AddComponent<MeshRenderer>();
            quadRenderer.sharedMaterial = mat;
            quadRenderer.shadowCastingMode = ShadowCastingMode.Off;
            quadRenderer.receiveShadows = false;
            quadRenderer.lightProbeUsage = LightProbeUsage.Off;
            quadRenderer.reflectionProbeUsage = ReflectionProbeUsage.Off;
            quadRenderer.motionVectorGenerationMode = MotionVectorGenerationMode.ForceNoMotion;
            quadRenderer.allowOcclusionWhenDynamic = false;
            quadRenderer.enabled = false;
            shownGradient = -1;
            shownScale = Vector3.zero;
            shownZ = -1f;
            quadOn = false;
        }

        // Destroys the quad and shows the hidden renderers again.
        public void Destroy()
        {
            ShowAll();
            // immediately, so nothing hidden is left over when this runs on Play mode exit
            if (quad != null) Object.DestroyImmediate(quad);
            if (mesh != null) Object.DestroyImmediate(mesh);
            if (mat != null) Object.DestroyImmediate(mat);
            quad = null;
            mesh = null;
            mat = null;
            quadRenderer = null;
            noShader = false;
            quadOn = false;
        }

        // Every frame: camera clear flags / colour, the quad, hiding everything that is not the avatar.
        // While Transparent the Game view shows a neutral grey (the picture itself gets alpha).
        public void Apply(StudioCamera view, AvatarRig rig)
        {
            Camera cam = view != null ? view.Cam : null;
            if (cam == null) return;
            if (quad == null && !noShader) Create(view);

            int g = Mathf.Clamp(Gradient, 0, Mathf.Min(GradientTop.Length, GradientBottom.Length) - 1);
            CameraClearFlags fl = CameraClearFlags.SolidColor;
            Color bg;
            bool useQuad = false;
            switch (Mode)
            {
                case BackdropMode.Solid:
                    bg = Solid;
                    break;
                case BackdropMode.Gradient:
                    bg = GradientBottom[g];
                    useQuad = quadRenderer != null;
                    break;
                case BackdropMode.Transparent:
                    bg = ViewGrey;
                    break;
                default:
                    if (RenderSettings.skybox != null) fl = CameraClearFlags.Skybox;
                    bg = SceneGrey;
                    break;
            }
            bg.a = 1f;
            flags = fl;
            background = bg;
            quadOn = useQuad;
            if (cam.clearFlags != fl) cam.clearFlags = fl;
            cam.backgroundColor = bg;

            if (quadRenderer != null)
            {
                if (useQuad)
                {
                    SetColours(g);
                    Fit(cam);
                }
                if (quadRenderer.enabled != useQuad) quadRenderer.enabled = useQuad;
            }

            // only the avatar, once there is one (a new avatar may be among what was hidden for the last one)
            Transform root = rig != null && rig.Alive ? rig.Root : null;
            if (Mode != BackdropMode.Scene && root != null) Hide(root);
            else if (hiding) ShowAll();
        }

        // For the two shots of a transparent picture: hide the quad and clear to this colour. EndShot puts it back.
        public void BeginShot(Camera cam, Color clear)
        {
            if (quadRenderer != null) quadRenderer.enabled = false;
            if (cam == null) return;
            cam.clearFlags = CameraClearFlags.SolidColor;
            cam.backgroundColor = clear;
        }

        public void EndShot(Camera cam)
        {
            if (quadRenderer != null) quadRenderer.enabled = quadOn;
            if (cam == null) return;
            cam.clearFlags = flags;
            cam.backgroundColor = background;
        }

        static float RowY(int r)
        {
            if (r == 0) return -Overscan;
            if (r == Steps + 2) return Overscan;
            return -1f + 2f * (r - 1) / Steps;
        }

        void SetColours(int g)
        {
            bool linear = QualitySettings.activeColorSpace == ColorSpace.Linear;
            if (g == shownGradient && linear == shownLinear) return;
            shownGradient = g;
            shownLinear = linear;
            Color top = GradientTop[g], bottom = GradientBottom[g];
            int rows = Steps + 3;
            for (int r = 0; r < rows; r++)
            {
                float k = Mathf.Clamp01((RowY(r) + 1f) * 0.5f);
                Color c = Color.Lerp(bottom, top, k);
                c.a = 1f;
                // vertex colours are not converted by Unity: in Linear colour space they must be linear already
                if (linear) c = c.linear;
                colors[r * 2] = c;
                colors[r * 2 + 1] = c;
            }
            mesh.colors = colors;
        }

        // The quad covers the view: its rows at y = +-1 are the top and bottom of the Game view (pictures show a part
        // of the view: the same camera with a smaller field of view), and it is wide enough for any view or picture
        // aspect. It stands halfway between the near and far planes (StudioCamera moves both back towards
        // orthographic), sized from the lens StudioCamera.Apply has just set.
        void Fit(Camera cam)
        {
            float z = (cam.nearClipPlane + cam.farClipPlane) * 0.5f;
            float half = cam.orthographic ? cam.orthographicSize : Mathf.Tan(cam.fieldOfView * 0.5f * Mathf.Deg2Rad) * z;
            float wide = Mathf.Max(cam.aspect, 16f / 9f) * 1.5f;
            var s = new Vector3(half * wide, half, 1f);
            if (s == shownScale && z == shownZ) return;
            shownScale = s;
            shownZ = z;
            quad.transform.localPosition = new Vector3(0f, 0f, z);
            quad.transform.localScale = s;
        }

        // Hides everything in the open scenes that draws and is not part of the avatar: renderers, terrains and
        // world-space canvases. Looks again when the avatar changes and every few seconds (objects turned on since,
        // an avatar rebuilt by NDMF / VRCFury).
        void Hide(Transform root)
        {
            float now = Time.unscaledTime;
            if (hiding && root == hiddenFor && now < nextScan) return;
            if (root != hiddenFor) ShowAll();
            hiding = true;
            hiddenFor = root;
            nextScan = now + RescanSeconds;
            for (int i = hidden.Count - 1; i >= 0; i--)
                if (hidden[i] == null) hidden.RemoveAt(i);
            for (int i = hiddenTerrains.Count - 1; i >= 0; i--)
                if (hiddenTerrains[i].Terrain == null) hiddenTerrains.RemoveAt(i);
            for (int i = hiddenCanvases.Count - 1; i >= 0; i--)
                if (hiddenCanvases[i] == null) hiddenCanvases.RemoveAt(i);

            foreach (Renderer r in Object.FindObjectsOfType<Renderer>())
            {
                if (r == null || r == quadRenderer || !r.enabled || r.forceRenderingOff) continue;
                if (r.transform.IsChildOf(root)) continue;
                r.forceRenderingOff = true;
                hidden.Add(r);
            }

            foreach (Terrain t in Object.FindObjectsOfType<Terrain>())
            {
                if (t == null || !t.enabled || (!t.drawHeightmap && !t.drawTreesAndFoliage)) continue;
                if (t.transform.IsChildOf(root)) continue;
                // drawn again since (by a script): the settings from before the studio are the ones remembered first
                if (!HidingTerrain(t))
                    hiddenTerrains.Add(new HiddenTerrain { Terrain = t, Heightmap = t.drawHeightmap, Trees = t.drawTreesAndFoliage });
                t.drawHeightmap = false;
                t.drawTreesAndFoliage = false;
            }

            // Every canvas under a world-space root, nested ones too: with only its parent switched off, a nested
            // canvas can become a root of its own and be drawn anyway. Picked first, switched off after, so switching
            // one off does not change what the others' rootCanvas is while looking.
            canvasScan.Clear();
            foreach (Canvas c in Object.FindObjectsOfType<Canvas>())
            {
                if (c == null || !c.enabled || c.transform.IsChildOf(root)) continue;
                Canvas top = c.rootCanvas;
                if (top != null && top.renderMode == RenderMode.WorldSpace) canvasScan.Add(c);
            }
            foreach (Canvas c in canvasScan)
            {
                c.enabled = false;
                if (!hiddenCanvases.Contains(c)) hiddenCanvases.Add(c);
            }
            canvasScan.Clear();
        }

        bool HidingTerrain(Terrain t)
        {
            foreach (HiddenTerrain h in hiddenTerrains)
                if (h.Terrain == t) return true;
            return false;
        }

        void ShowAll()
        {
            foreach (Renderer r in hidden)
                if (r != null) r.forceRenderingOff = false;
            hidden.Clear();
            foreach (HiddenTerrain h in hiddenTerrains)
            {
                if (h.Terrain == null) continue;
                h.Terrain.drawHeightmap = h.Heightmap;
                h.Terrain.drawTreesAndFoliage = h.Trees;
            }
            hiddenTerrains.Clear();
            foreach (Canvas c in hiddenCanvases)
                if (c != null) c.enabled = true;
            hiddenCanvases.Clear();
            hiding = false;
            hiddenFor = null;
        }

        struct HiddenTerrain
        {
            public Terrain Terrain;
            public bool Heightmap, Trees; // drawHeightmap, drawTreesAndFoliage as they were
        }
    }
}
