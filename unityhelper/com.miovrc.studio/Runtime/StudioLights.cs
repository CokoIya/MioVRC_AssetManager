// Studio lighting: a key light (direction, colour, intensity, colour temperature, shadows), an optional rim light
// from behind, and a flat ambient level. The scene's own lights can be switched off while the studio is open; all
// of it is put back when the studio closes.
using System.Collections.Generic;
using UnityEngine;
using UnityEngine.Rendering;

namespace MioVRCA.Studio
{
    internal sealed class StudioLights
    {
        public static readonly Color[] Swatches =
        {
            new Color(1f, 1f, 1f), new Color(1f, 0.86f, 0.72f), new Color(0.76f, 0.85f, 1f),
            new Color(1f, 0.78f, 0.88f), new Color(0.74f, 0.95f, 1f), new Color(1f, 0.89f, 0.6f),
        };

        public float Yaw = 35f;            // degrees; relative to the camera when FollowCamera, else to the avatar's front
        public float Pitch = 35f;          // degrees above the horizon, -10..89
        public bool FollowCamera = true;
        public float Intensity = 1.1f;     // 0..3
        public float Kelvin = 6500f;       // 2500..10000
        public Color Tint = Color.white;   // one of Swatches, or any colour
        public bool Shadows = true;
        public float ShadowStrength = 0.7f;
        public bool Rim;
        public float RimIntensity = 0.9f;  // 0..3
        public float Ambient = 0.55f;      // 0..1.5, flat ambient
        public bool OnlyStudioLights = true;
        public bool DragMode;              // left-drag on empty space turns the key light

        public Light Key { get; private set; }
        public Light RimLight { get; private set; }

        const float RescanSeconds = 2f;
        static readonly Color RimColor = new Color(0.92f, 0.96f, 1f, 1f);

        // the scene's lights the studio switched off (they were on); Destroy turns exactly these back on
        readonly List<Light> turnedOff = new List<Light>();
        float nextScan;

        // RenderSettings ambient as the scene had it
        AmbientMode oldMode;
        Color oldLight;
        float oldIntensity;
        SphericalHarmonicsL2 oldProbe;
        bool ambientSaved, ambientWritten;
        float lastAmbient = -1f;
        Color ambientColor;
        SphericalHarmonicsL2 ambientProbe;
        float nextProbeCheck;

        // the key colour, worked out again only when Kelvin or Tint change
        float lastKelvin = -1f;
        Color lastTint;
        Color keyColor = Color.white;

        // Makes the lights (DontSave, hidden) and remembers the scene's lights and ambient settings.
        public void Create()
        {
            if (Key != null) return;
            oldMode = RenderSettings.ambientMode;
            oldLight = RenderSettings.ambientLight;
            oldIntensity = RenderSettings.ambientIntensity;
            oldProbe = RenderSettings.ambientProbe;
            ambientSaved = true;
            ambientWritten = false;
            lastAmbient = -1f;
            lastKelvin = -1f;
            turnedOff.Clear();
            nextScan = 0f;

            Key = MakeLight("MioVRCA Studio Key Light");
            Key.shadows = LightShadows.Soft;
            Key.shadowResolution = LightShadowResolution.VeryHigh; // it is a photo
            RimLight = MakeLight("MioVRCA Studio Rim Light");
            RimLight.shadows = LightShadows.None;
            RimLight.color = RimColor;
            RimLight.enabled = false;
        }

        // Destroys the lights and puts the scene's lights and RenderSettings ambient back.
        public void Destroy()
        {
            SceneLightsOn();
            if (ambientWritten && ambientSaved)
            {
                RenderSettings.ambientMode = oldMode;
                RenderSettings.ambientLight = oldLight;
                RenderSettings.ambientIntensity = oldIntensity;
                RenderSettings.ambientProbe = oldProbe;
            }
            ambientWritten = false;
            ambientSaved = false;
            lastAmbient = -1f;
            // immediately, so nothing hidden is left over when this runs on Play mode exit
            if (Key != null) Object.DestroyImmediate(Key.gameObject);
            if (RimLight != null) Object.DestroyImmediate(RimLight.gameObject);
            Key = null;
            RimLight = null;
        }

        // Every frame: directions (from the camera when FollowCamera), colours, scene lights on/off, ambient.
        public void Apply(StudioCamera view, AvatarRig rig)
        {
            if (Key == null) return;
            if (OnlyStudioLights) SceneLightsOff();
            else SceneLightsOn();

            // Without FollowCamera the light keeps its place around the avatar (Yaw 0 = straight at its front).
            float baseYaw = FollowCamera && view != null ? view.Yaw : FrontYaw(rig);
            float pitch = Mathf.Clamp(Pitch, -10f, 89f);
            Key.transform.rotation = Quaternion.Euler(pitch, baseYaw + Yaw, 0f);

            if (Kelvin != lastKelvin || Tint != lastTint)
            {
                lastKelvin = Kelvin;
                lastTint = Tint;
                Color t = KelvinColor(Kelvin);
                keyColor = new Color(Tint.r * t.r, Tint.g * t.g, Tint.b * t.b, 1f);
            }
            Key.color = keyColor;
            Key.intensity = Mathf.Max(0f, Intensity);
            LightShadows s = Shadows ? LightShadows.Soft : LightShadows.None;
            if (Key.shadows != s) Key.shadows = s;
            Key.shadowStrength = Mathf.Clamp01(ShadowStrength);

            if (RimLight != null)
            {
                if (RimLight.enabled != Rim) RimLight.enabled = Rim;
                if (Rim)
                {
                    RimLight.transform.rotation = Quaternion.Euler(Mathf.Max(15f, pitch * 0.6f), baseYaw + 180f - Yaw * 0.5f, 0f);
                    RimLight.intensity = Mathf.Max(0f, RimIntensity);
                }
            }

            ApplyAmbient();
        }

        // mouse delta in GUI pixels: x turns Yaw, y turns Pitch
        public void Drag(Vector2 delta)
        {
            Yaw = Mathf.Repeat(Yaw + delta.x * 0.4f + 180f, 360f) - 180f;
            Pitch = Mathf.Clamp(Pitch + delta.y * 0.4f, -10f, 89f);
        }

        // The Light.color for a colour temperature: 6500 K is exactly white, the brightest channel is 1.
        // Mathf.CorrelatedColorTemperatureToRGB gives linear RGB, but Light.color is a gamma colour that Unity turns
        // into linear once more in a Linear project; taken as it is, the tint would count twice (2500 K nearly pure
        // orange, a pink cast at the default 6500 K). So: to gamma, then divided by 6500 K (about D65, the sRGB white).
        public static Color KelvinColor(float kelvin)
        {
            float k = kelvin > 0f ? Mathf.Clamp(kelvin, 1000f, 40000f) : 6500f; // also NaN
            Color c = Brightest(Mathf.CorrelatedColorTemperatureToRGB(k).gamma);
            Color w = Brightest(Mathf.CorrelatedColorTemperatureToRGB(6500f).gamma);
            return Brightest(new Color(c.r / Mathf.Max(w.r, 1e-4f), c.g / Mathf.Max(w.g, 1e-4f), c.b / Mathf.Max(w.b, 1e-4f), 1f));
        }

        // scaled so that the brightest channel is 1, alpha 1
        static Color Brightest(Color c)
        {
            float m = Mathf.Max(c.r, Mathf.Max(c.g, c.b));
            if (!(m > 1e-4f)) return Color.white; // also NaN
            return new Color(c.r / m, c.g / m, c.b / m, 1f);
        }

        static Light MakeLight(string name)
        {
            var go = new GameObject(name) { hideFlags = HideFlags.HideAndDontSave };
            Light l = go.AddComponent<Light>();
            l.type = LightType.Directional;
            l.renderMode = LightRenderMode.ForcePixel; // per-pixel even next to the scene's own lights
            return l;
        }

        // the yaw of a camera looking at the avatar's front (the light shines the same way)
        static float FrontYaw(AvatarRig rig)
        {
            if (rig == null || rig.Root == null) return 180f; // avatars usually face +Z
            Vector3 f = rig.Forward;
            return Mathf.Atan2(-f.x, -f.z) * Mathf.Rad2Deg;
        }

        // Switches the scene's lights off; looks again every few seconds for lights that came on since (an avatar
        // rebuilt by NDMF / VRCFury, objects turned on by scripts).
        void SceneLightsOff()
        {
            float now = Time.unscaledTime;
            if (now < nextScan) return;
            nextScan = now + RescanSeconds;
            foreach (Light l in Object.FindObjectsOfType<Light>())
            {
                if (l == null || l == Key || l == RimLight || !l.enabled) continue;
                l.enabled = false;
                turnedOff.Add(l);
            }
        }

        void SceneLightsOn()
        {
            nextScan = 0f; // look again at once when they go off the next time
            if (turnedOff.Count == 0) return;
            foreach (Light l in turnedOff)
                if (l != null) l.enabled = true;
            turnedOff.Clear();
        }

        // RenderSettings are written only when the level changes, or when something else changed them
        void ApplyAmbient()
        {
            float a = Mathf.Max(0f, Ambient);
            bool write = a != lastAmbient || RenderSettings.ambientMode != AmbientMode.Flat;
            if (a != lastAmbient)
            {
                lastAmbient = a;
                ambientColor = new Color(a, a, a, 1f); // a gamma colour, like the Lighting window's
                Color lin = QualitySettings.activeColorSpace == ColorSpace.Linear ? ambientColor.linear : ambientColor;
                ambientProbe = new SphericalHarmonicsL2();
                ambientProbe.AddAmbientLight(lin);
            }
            if (!write && Time.unscaledTime >= nextProbeCheck)
            {
                nextProbeCheck = Time.unscaledTime + RescanSeconds;
                write = RenderSettings.ambientProbe != ambientProbe;
            }
            if (!write) return;
            RenderSettings.ambientMode = AmbientMode.Flat;
            RenderSettings.ambientLight = ambientColor;
            RenderSettings.ambientIntensity = 1f;
            RenderSettings.ambientProbe = ambientProbe; // what probe-lit (SH) shaders use
            ambientWritten = true;
        }
    }
}
