// The studio's settings for this project, in UserSettings/MioVRCA/studio/prefs.json (JsonUtility): lens, light,
// backdrop, output and display choices, so the studio opens the way it was left. Values read from the file are
// clamped to what the parts accept, so a hand-edited or old file cannot break anything.
using System;
using UnityEngine;

namespace MioVRCA.Studio
{
    [Serializable]
    internal sealed class StudioPrefs
    {
        public string lang = "";
        public float uiScale = 1f;
        public int panel = 1;

        // handles
        public bool handles = true, detail, fingers = true, pinFeet = true;
        public float dragSpeed = 1f;
        public int handSide = 2;
        public float handStrength = 1f;

        // look
        public bool look = true, lookCamera = true;
        public float lookEyes = 1f, lookHead = 0.3f;

        // camera
        public float focal = 50f, ortho, orbitSpeed = 1f;
        public bool follow = true, thirds = true;

        // light
        public float lightYaw = 35f, lightPitch = 35f, intensity = 1.1f, kelvin = 6500f, rimIntensity = 0.9f, ambient = 0.55f, shadowStrength = 0.7f;
        public bool lightFollow = true, rim, shadows = true, onlyStudioLights = true;
        public Color tint = Color.white;

        // backdrop
        public int backdrop = (int)BackdropMode.Gradient, gradient;
        public Color solid = new Color(0.96f, 0.96f, 0.97f);

        // output
        public int size = 2, aspect, timer;
        public bool supersample = true;

        public static string FilePath
        {
            get { return System.IO.Path.Combine(StudioHost.DataDir, "prefs.json"); }
        }

        // the saved settings, or the defaults when there are none or they cannot be read
        public static StudioPrefs Load()
        {
            var p = new StudioPrefs();
            try
            {
                string text = StudioHost.ReadText(FilePath);
                if (!string.IsNullOrEmpty(text)) JsonUtility.FromJsonOverwrite(text, p);
            }
            catch (Exception e)
            {
                Debug.LogWarning("[MioVRCA] 摄影棚设置读取失败，使用默认设置：" + e.Message);
                p = new StudioPrefs(); // not half of the file
            }
            if (p.lang == null) p.lang = "";
            return p;
        }

        public void Save()
        {
            try { StudioHost.WriteText(FilePath, JsonUtility.ToJson(this, true)); }
            catch (Exception e) { Debug.LogWarning("[MioVRCA] 摄影棚设置没能保存：" + e.Message); }
        }

        // copies the settings into the studio's parts
        public void ApplyTo(Studio s)
        {
            if (s == null) return;
            s.Ui.Panel = (StudioPanel)Mathf.Clamp(panel, 0, (int)StudioPanel.Output);
            s.Ui.UserScale = Range(uiScale, 0.75f, 1.5f, 1f);

            PoseHandles h = s.Handles;
            h.Visible = handles;
            h.Detail = detail;
            h.Fingers = fingers;
            h.PinFeet = pinFeet;
            h.Sensitivity = Range(dragSpeed, 0.25f, 3f, 1f);
            s.HandSide = (HandSide)Mathf.Clamp(handSide, 0, (int)HandSide.Both);
            s.HandStrength = Range(handStrength, 0f, 1f, 1f);

            LookAt l = s.Look;
            l.Enabled = look;
            l.FollowCamera = lookCamera;
            l.Eyes = Range(lookEyes, 0f, 1f, 1f);
            l.Head = Range(lookHead, 0f, 1f, 0.3f);

            StudioCamera v = s.View;
            v.FocalLength = Range(focal, 14f, 300f, 50f);
            v.Ortho = Range(ortho, 0f, 1f, 0f);
            v.OrbitSpeed = Range(orbitSpeed, 0.25f, 3f, 1f);
            v.Follow = follow;
            v.Thirds = thirds;

            StudioLights li = s.Lights;
            li.Yaw = Finite(lightYaw, 35f);
            li.Yaw = Mathf.Repeat(li.Yaw + 180f, 360f) - 180f;
            li.Pitch = Range(lightPitch, -10f, 89f, 35f);
            li.FollowCamera = lightFollow;
            li.Intensity = Range(intensity, 0f, 3f, 1.1f);
            li.Kelvin = Range(kelvin, 2500f, 10000f, 6500f);
            li.Tint = Opaque(tint, Color.white);
            li.Shadows = shadows;
            li.ShadowStrength = Range(shadowStrength, 0f, 1f, 0.7f);
            li.Rim = rim;
            li.RimIntensity = Range(rimIntensity, 0f, 3f, 0.9f);
            li.Ambient = Range(ambient, 0f, 1.5f, 0.55f);
            li.OnlyStudioLights = onlyStudioLights;

            Backdrop b = s.Back;
            b.Mode = (BackdropMode)Mathf.Clamp(backdrop, 0, (int)BackdropMode.Transparent);
            b.Gradient = Mathf.Clamp(gradient, 0, Backdrop.GradientNames.Length - 1);
            b.Solid = Opaque(solid, new Color(0.96f, 0.96f, 0.97f));

            PhotoCapture p = s.Photo;
            p.SizeIndex = Mathf.Clamp(size, 0, PhotoCapture.Sizes.Length - 1);
            p.AspectIndex = Mathf.Clamp(aspect, 0, PhotoCapture.Aspects.Length - 1);
            p.TimerIndex = Mathf.Clamp(timer, 0, PhotoCapture.Timers.Length - 1);
            p.Supersample = supersample;
        }

        // reads the settings back from the studio's parts
        public void ReadFrom(Studio s)
        {
            if (s == null) return;
            lang = L.Lang;
            panel = (int)s.Ui.Panel;
            uiScale = s.Ui.UserScale;

            PoseHandles h = s.Handles;
            handles = h.Visible;
            detail = h.Detail;
            fingers = h.Fingers;
            pinFeet = h.PinFeet;
            dragSpeed = h.Sensitivity;
            handSide = (int)s.HandSide;
            handStrength = s.HandStrength;

            LookAt l = s.Look;
            look = l.Enabled;
            lookCamera = l.FollowCamera;
            lookEyes = l.Eyes;
            lookHead = l.Head;

            StudioCamera v = s.View;
            focal = v.FocalLength;
            ortho = v.Ortho;
            orbitSpeed = v.OrbitSpeed;
            follow = v.Follow;
            thirds = v.Thirds;

            StudioLights li = s.Lights;
            lightYaw = li.Yaw;
            lightPitch = li.Pitch;
            lightFollow = li.FollowCamera;
            intensity = li.Intensity;
            kelvin = li.Kelvin;
            tint = li.Tint;
            shadows = li.Shadows;
            shadowStrength = li.ShadowStrength;
            rim = li.Rim;
            rimIntensity = li.RimIntensity;
            ambient = li.Ambient;
            onlyStudioLights = li.OnlyStudioLights;

            Backdrop b = s.Back;
            backdrop = (int)b.Mode;
            gradient = b.Gradient;
            solid = b.Solid;

            PhotoCapture p = s.Photo;
            size = p.SizeIndex;
            aspect = p.AspectIndex;
            timer = p.TimerIndex;
            supersample = p.Supersample;
        }

        static float Finite(float v, float fallback)
        {
            return float.IsNaN(v) || float.IsInfinity(v) ? fallback : v;
        }

        static float Range(float v, float min, float max, float fallback)
        {
            return Mathf.Clamp(Finite(v, fallback), min, max);
        }

        static Color Opaque(Color c, Color fallback)
        {
            if (float.IsNaN(c.r) || float.IsNaN(c.g) || float.IsNaN(c.b)) return fallback;
            return new Color(Mathf.Clamp01(c.r), Mathf.Clamp01(c.g), Mathf.Clamp01(c.b), 1f);
        }
    }
}
