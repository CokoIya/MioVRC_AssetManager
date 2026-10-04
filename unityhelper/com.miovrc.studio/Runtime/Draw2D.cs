// Drawing on top of the Game view from OnGUI (Repaint only): lines through GL, discs and rounded boxes through
// GUI.DrawTexture with a corner radius (anti-aliased by Unity's own GUI shader). Coordinates are GUI pixels, origin
// at the top left.
using UnityEngine;
using UnityEngine.Rendering;

namespace MioVRCA.Studio
{
    internal static class Draw2D
    {
        static Material lineMat;
        static Texture2D white;

        public static Texture2D White
        {
            get
            {
                if (white == null)
                {
                    white = new Texture2D(1, 1, TextureFormat.RGBA32, false) { name = "MioVRCA Studio Draw2D", hideFlags = HideFlags.HideAndDontSave };
                    white.SetPixel(0, 0, Color.white);
                    white.Apply();
                }
                return white;
            }
        }

        static Material LineMat
        {
            get
            {
                if (lineMat == null)
                {
                    lineMat = new Material(Shader.Find("Hidden/Internal-Colored")) { name = "MioVRCA Studio Draw2D", hideFlags = HideFlags.HideAndDontSave };
                    lineMat.SetInt("_SrcBlend", (int)BlendMode.SrcAlpha);
                    lineMat.SetInt("_DstBlend", (int)BlendMode.OneMinusSrcAlpha);
                    lineMat.SetInt("_Cull", (int)CullMode.Off);
                    lineMat.SetInt("_ZWrite", 0);
                    lineMat.SetInt("_ZTest", (int)CompareFunction.Always);
                }
                return lineMat;
            }
        }

        static bool open;

        // Lines go between BeginLines and EndLines (one GL batch). Only during EventType.Repaint.
        public static void BeginLines()
        {
            if (open) return;
            LineMat.SetPass(0);
            GL.PushMatrix();
            GL.LoadPixelMatrix(0, Screen.width, Screen.height, 0);
            GL.Begin(GL.QUADS);
            open = true;
        }

        public static void Line(Vector2 a, Vector2 b, float width, Color c)
        {
            if (!open) return;
            Vector2 d = b - a;
            if (d.sqrMagnitude < 1e-6f) return;
            Vector2 n = new Vector2(-d.y, d.x).normalized * (width * 0.5f);
            // GUI.matrix scaling is not applied to GL, so callers pass raw pixels
            GL.Color(c);
            GL.Vertex3(a.x + n.x, a.y + n.y, 0);
            GL.Vertex3(b.x + n.x, b.y + n.y, 0);
            GL.Vertex3(b.x - n.x, b.y - n.y, 0);
            GL.Vertex3(a.x - n.x, a.y - n.y, 0);
        }

        public static void EndLines()
        {
            if (!open) return;
            GL.End();
            GL.PopMatrix();
            open = false;
        }

        // a filled disc
        public static void Disc(Vector2 center, float radius, Color c)
        {
            var r = new Rect(center.x - radius, center.y - radius, radius * 2f, radius * 2f);
            GUI.DrawTexture(r, White, ScaleMode.StretchToFill, true, 0f, c, 0f, radius);
        }

        // a circle outline
        public static void Ring(Vector2 center, float radius, float width, Color c)
        {
            var r = new Rect(center.x - radius, center.y - radius, radius * 2f, radius * 2f);
            GUI.DrawTexture(r, White, ScaleMode.StretchToFill, true, 0f, c, width, radius);
        }

        public static void Box(Rect r, Color c, float radius)
        {
            GUI.DrawTexture(r, White, ScaleMode.StretchToFill, true, 0f, c, 0f, radius);
        }

        public static void Frame(Rect r, Color c, float width, float radius)
        {
            GUI.DrawTexture(r, White, ScaleMode.StretchToFill, true, 0f, c, width, radius);
        }

        public static void Release()
        {
            if (lineMat != null) Object.DestroyImmediate(lineMat);
            if (white != null) Object.DestroyImmediate(white);
            lineMat = null;
            white = null;
        }
    }
}
