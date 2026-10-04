// Taking the picture: the studio camera renders the inside of the output frame at the chosen size (twice the size
// and scaled down when supersampling), with alpha when the backdrop is transparent (one shot on black, one on white;
// what changes between them is background). PNG files go to StudioHost.PhotoDir.
using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using UnityEngine;

namespace MioVRCA.Studio
{
    internal sealed class Shot
    {
        public string Path;
        public Texture2D Thumb; // about 160 px high, DontSave; null when it could not be made
    }

    internal sealed class PhotoCapture
    {
        public static readonly int[] Sizes = { 480, 720, 1080, 1440, 2160 }; // the short side in pixels
        public static readonly string[] AspectNames = { "1:1", "3:4", "4:3", "9:16", "16:9" };
        public static readonly float[] Aspects = { 1f, 3f / 4f, 4f / 3f, 9f / 16f, 16f / 9f };
        public static readonly int[] Timers = { 0, 3, 5, 10 };
        public const int RecentMax = 8;

        public int SizeIndex = 2;
        public int AspectIndex;
        public int TimerIndex;
        public bool Supersample = true;

        public readonly List<Shot> Recent = new List<Shot>(); // newest first

        // seconds left while counting down, else 0
        public float Countdown { get; private set; }

        // 1 right after a shot, fading to 0 (the white flash)
        public float Flash;

        const int ThumbHeight = 160;
        const int MaxSide = 8192;                  // the render target's long side, supersampled
        const long SampleBudget = 8192L * 8192L;   // MSAA samples of the render target (about 512 MB with depth)

        bool due; // a shot is wanted on the next Tick

        public float Aspect
        {
            get { return Aspects[Mathf.Clamp(AspectIndex, 0, Aspects.Length - 1)]; }
        }

        // the picture size in pixels
        public Vector2Int Size
        {
            get
            {
                int s = Sizes[Mathf.Clamp(SizeIndex, 0, Sizes.Length - 1)];
                float a = Aspect;
                return a >= 1f ? new Vector2Int(Mathf.RoundToInt(s * a), s) : new Vector2Int(s, Mathf.RoundToInt(s / a));
            }
        }

        // Space / the shutter button: starts the countdown, or asks for a shot on the next frame without a timer.
        public void Request()
        {
            if (Countdown > 0f || due) return; // already on its way (Esc cancels)
            int t = Timers[Mathf.Clamp(TimerIndex, 0, Timers.Length - 1)];
            if (t > 0) Countdown = t;
            else due = true;
        }

        public void Cancel()
        {
            Countdown = 0f;
            due = false;
        }

        // Every frame (unscaled time): counts down and fades the flash. True when the shot is due now.
        public bool Tick(float dt)
        {
            if (!(dt > 0f)) dt = 0f;
            if (Flash > 0f) Flash = Mathf.Max(0f, Flash - dt * 3f);
            if (Countdown > 0f)
            {
                Countdown -= dt;
                if (Countdown <= 0f)
                {
                    Countdown = 0f;
                    due = true;
                }
            }
            if (!due) return false;
            due = false;
            return true;
        }

        // Renders and saves the picture. frameScale = FrameRect height / view height (see StudioCamera.PictureLens).
        // Returns the saved path, or null with error (中文 through L.T). Restores the camera's target, lens and clear
        // settings afterwards whatever happens; adds a thumbnail to Recent.
        public string Shoot(StudioCamera view, Backdrop back, float frameScale, out string error)
        {
            error = null;
            Camera cam = view != null ? view.Cam : null;
            if (cam == null)
            {
                error = L.T("相机不可用，无法拍照");
                return null;
            }
            if (!(frameScale > 0.01f)) frameScale = StudioCamera.FrameMargin; // also NaN
            frameScale = Mathf.Min(frameScale, 1f);

            Vector2Int size = Size;
            int w = size.x, h = size.y;
            int maxSide = Mathf.Min(MaxSide, SystemInfo.maxTextureSize);
            if (w < 1 || h < 1 || Mathf.Max(w, h) > maxSide)
            {
                error = L.T("显卡不支持这么大的照片，请把尺寸调小");
                return null;
            }
            int ss = Supersample && Mathf.Max(w, h) * 2 <= maxSide ? 2 : 1;
            bool transparent = back != null && back.Transparent;

            RenderTexture before = RenderTexture.active;
            RenderTexture target = cam.targetTexture;
            CameraClearFlags flags = cam.clearFlags;
            Color background = cam.backgroundColor;
            bool backShot = false, lensSet = false;
            RenderTexture rt = null, half = null;
            Texture2D onBlack = null, onWhite = null, result = null;
            try
            {
                rt = Target(w, h, ref ss);
                if (rt == null)
                {
                    error = L.T("显存不足，无法拍这么大的照片，请把尺寸调小");
                    return null;
                }
                if (ss == 2) half = NewRt(w, h, 1, 0);

                cam.targetTexture = rt;
                lensSet = true;
                view.PictureLens((float)w / h, frameScale);
                if (transparent)
                {
                    // what differs between the two is background, so the outline is right whatever the materials
                    // write into alpha (as MioVRCA's menu icons)
                    backShot = true;
                    back.BeginShot(cam, Color.black);
                    onBlack = Render(cam, rt, half, w, h);
                    back.BeginShot(cam, Color.white);
                    onWhite = Render(cam, rt, half, w, h);
                    result = Combine(onBlack, onWhite, w, h);
                }
                else
                {
                    result = Render(cam, rt, half, w, h); // RGB: the picture is opaque whatever the shaders write to alpha
                }

                byte[] png = result.EncodeToPNG();
                if (png == null || png.Length == 0)
                {
                    error = L.T("照片编码失败");
                    return null;
                }
                string path = NewPath();
                StudioHost.WriteAtomic(path, png);
                AddRecent(path, result);
                Flash = 1f;
                return path;
            }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 照片保存失败：" + e);
                error = L.T("照片保存失败：") + e.Message;
                return null;
            }
            finally
            {
                RenderTexture.active = before;
                if (cam != null)
                {
                    cam.targetTexture = target;
                    if (lensSet) view.ViewLens(); // the Game view's aspect and lens (the camera has not moved)
                    if (backShot && back != null) back.EndShot(cam);
                    cam.clearFlags = flags;
                    cam.backgroundColor = background;
                }
                Release(rt);
                Release(half);
                Kill(onBlack);
                Kill(onWhite);
                Kill(result);
            }
        }

        // destroys the thumbnails
        public void Clear()
        {
            foreach (Shot s in Recent)
                if (s != null) Kill(s.Thumb);
            Recent.Clear();
            Cancel();
            Flash = 0f;
        }

        // The render target, supersampled when ss is 2. MSAA 8x (4x when supersampling), less for very big
        // pictures so it stays within SampleBudget. When it cannot be made: without supersampling and MSAA, else null.
        static RenderTexture Target(int w, int h, ref int ss)
        {
            int aa = ss == 2 ? 4 : 8;
            while (aa > 1 && (long)w * ss * h * ss * aa > SampleBudget) aa /= 2;
            RenderTexture rt = NewRt(w * ss, h * ss, aa, 24);
            if (rt.Create()) return rt;
            Release(rt);
            if (ss == 1 && aa == 1) return null;
            Debug.LogWarning("[MioVRCA] 照片的渲染纹理创建失败，改为不超采样、不抗锯齿再试");
            ss = 1;
            rt = NewRt(w, h, 1, 24);
            if (rt.Create()) return rt;
            Release(rt);
            return null;
        }

        static RenderTexture NewRt(int w, int h, int aa, int depth)
        {
            var rt = new RenderTexture(w, h, depth, RenderTextureFormat.ARGB32, RenderTextureReadWrite.sRGB)
            {
                antiAliasing = Mathf.Max(1, aa),
                filterMode = FilterMode.Bilinear,
                wrapMode = TextureWrapMode.Clamp,
                hideFlags = HideFlags.HideAndDontSave,
            };
            // not every GPU has 8x for this format
            int supported = rt.antiAliasing > 1 ? SystemInfo.GetRenderTextureSupportedMSAASampleCount(rt.descriptor) : 1;
            if (supported >= 1 && rt.antiAliasing > supported) rt.antiAliasing = supported;
            return rt;
        }

        // one cam.Render into rt, halved into `half` when supersampling (bilinear at exactly half = a 2x2 box filter),
        // read back as RGB
        static Texture2D Render(Camera cam, RenderTexture rt, RenderTexture half, int w, int h)
        {
            cam.Render();
            RenderTexture src = rt;
            if (half != null)
            {
                Graphics.Blit(rt, half);
                src = half;
            }
            RenderTexture.active = src;
            var tex = new Texture2D(w, h, TextureFormat.RGB24, false, false) { hideFlags = HideFlags.HideAndDontSave };
            tex.ReadPixels(new Rect(0, 0, w, h), 0, 0, false);
            return tex;
        }

        // alpha from how much a pixel changes between the black and the white background
        static Texture2D Combine(Texture2D onBlack, Texture2D onWhite, int w, int h)
        {
            Color32[] k = onBlack.GetPixels32(), wh = onWhite.GetPixels32();
            // written out (no Mathf calls): this runs for every pixel of a picture up to 3840x2160
            for (int i = 0; i < k.Length; i++)
            {
                Color32 b = k[i], c = wh[i];
                int diff = c.r - b.r, dg = c.g - b.g, db = c.b - b.b;
                if (dg > diff) diff = dg;
                if (db > diff) diff = db;
                int a = 255 - diff;
                if (a <= 0)
                {
                    k[i] = new Color32(0, 0, 0, 0);
                    continue;
                }
                if (a > 255) a = 255;
                int r = b.r * 255 / a, g = b.g * 255 / a, bl = b.b * 255 / a;
                k[i] = new Color32((byte)(r > 255 ? 255 : r), (byte)(g > 255 ? 255 : g), (byte)(bl > 255 ? 255 : bl), (byte)a);
            }
            var tex = new Texture2D(w, h, TextureFormat.RGBA32, false, false) { hideFlags = HideFlags.HideAndDontSave };
            tex.SetPixels32(k);
            return tex;
        }

        // Pictures/MioVRCA/<project>/MioVRCA_yyyyMMdd_HHmmss.png, with _2, _3 ... when that name is taken
        static string NewPath()
        {
            string dir = StudioHost.PhotoDir;
            Directory.CreateDirectory(dir);
            string stem = "MioVRCA_" + DateTime.Now.ToString("yyyyMMdd_HHmmss", CultureInfo.InvariantCulture);
            string path = Path.Combine(dir, stem + ".png");
            for (int i = 2; File.Exists(path); i++) path = Path.Combine(dir, stem + "_" + i + ".png");
            return path;
        }

        // the picture is saved already: a thumbnail that cannot be made is only logged
        void AddRecent(string path, Texture2D picture)
        {
            Texture2D thumb = null;
            try
            {
                thumb = MakeThumb(picture);
            }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 缩略图生成失败：" + e.Message);
            }
            Recent.Insert(0, new Shot { Path = path, Thumb = thumb });
            while (Recent.Count > RecentMax)
            {
                Shot old = Recent[Recent.Count - 1];
                Recent.RemoveAt(Recent.Count - 1);
                if (old != null) Kill(old.Thumb);
            }
        }

        // halved on the GPU until close to the size (each step a 2x2 box), then the last step to ThumbHeight
        static Texture2D MakeThumb(Texture2D picture)
        {
            picture.wrapMode = TextureWrapMode.Clamp;
            picture.Apply(false); // ReadPixels / SetPixels32 only filled the CPU copy
            int th = Mathf.Min(ThumbHeight, picture.height);
            int tw = Mathf.Max(1, Mathf.RoundToInt(th * (float)picture.width / picture.height));
            RenderTexture before = RenderTexture.active;
            RenderTexture cur = null, last = null;
            try
            {
                Texture src = picture;
                int sw = picture.width, sh = picture.height;
                while (sh > th * 2)
                {
                    sw = Mathf.Max(tw, sw / 2);
                    sh = Mathf.Max(th, sh / 2);
                    RenderTexture next = Temp(sw, sh);
                    Graphics.Blit(src, next);
                    if (cur != null) RenderTexture.ReleaseTemporary(cur);
                    cur = next;
                    src = next;
                }
                last = Temp(tw, th);
                Graphics.Blit(src, last);
                RenderTexture.active = last;
                var thumb = new Texture2D(tw, th, TextureFormat.RGBA32, false, false)
                {
                    name = "MioVRCA Studio Thumb",
                    hideFlags = HideFlags.HideAndDontSave,
                    wrapMode = TextureWrapMode.Clamp,
                };
                thumb.ReadPixels(new Rect(0, 0, tw, th), 0, 0, false);
                thumb.Apply(false);
                return thumb;
            }
            finally
            {
                RenderTexture.active = before;
                if (cur != null) RenderTexture.ReleaseTemporary(cur);
                if (last != null) RenderTexture.ReleaseTemporary(last);
            }
        }

        static RenderTexture Temp(int w, int h)
        {
            RenderTexture rt = RenderTexture.GetTemporary(w, h, 0, RenderTextureFormat.ARGB32, RenderTextureReadWrite.sRGB);
            rt.filterMode = FilterMode.Bilinear;
            rt.wrapMode = TextureWrapMode.Clamp;
            return rt;
        }

        static void Release(RenderTexture rt)
        {
            if (rt == null) return;
            if (RenderTexture.active == rt) RenderTexture.active = null;
            rt.Release();
            UnityEngine.Object.DestroyImmediate(rt);
        }

        static void Kill(UnityEngine.Object o)
        {
            if (o != null) UnityEngine.Object.DestroyImmediate(o);
        }
    }
}
