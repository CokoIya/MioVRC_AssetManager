// The studio camera: orbits a pivot (which can follow a bone), with lens presets in millimetres (36x24 sensor), a
// blend towards orthographic (a dolly zoom, then a true orthographic camera), and the output frame: the picture is
// the part of the Game view inside FrameRect, so what the player sees in the frame is what gets saved.
using System.Collections.Generic;
using UnityEngine;

namespace MioVRCA.Studio
{
    internal enum Framing { Full, Upper, Face }

    internal sealed class StudioCamera
    {
        public const float FrameMargin = 0.9f; // the frame fills at most this much of the view
        public static readonly float[] Lenses = { 24f, 35f, 50f, 85f, 135f };

        public Camera Cam { get; private set; }

        public Vector3 Pivot;            // world point the camera looks at and orbits
        public float Yaw, Pitch;         // degrees; Yaw 0 looks along +Z
        public float Distance = 2.5f;    // metres from the pivot
        public float FocalLength = 50f;  // mm, 14..300
        public float Ortho;              // 0 = perspective .. 1 = orthographic (same size at the pivot)
        public bool Follow = true;       // the pivot moves with FollowBone
        public HumanBodyBones FollowBone = HumanBodyBones.Chest;
        public float OrbitSpeed = 1f;    // 0.25..3
        public bool Thirds = true;       // rule-of-thirds lines in the frame

        const float Near = 0.01f, Far = 1000f;
        const float MinDistance = 0.15f, MaxDistance = 30f;
        // Ortho t: the camera stands Distance / (1 - OrthoReach * t) from the pivot, its field of view narrowed to
        // keep the size there (at t near 1 about 20 times as far: hardly any perspective left); from OrthoFrom on it
        // is a true orthographic camera.
        const float OrthoReach = 0.95f, OrthoFrom = 0.999f;
        static readonly Color StartColor = new Color(0.32f, 0.33f, 0.36f, 1f);

        // the scene's cameras the studio turned off (Destroy turns exactly these back on)
        readonly List<Camera> turnedOff = new List<Camera>();

        // following: Pivot = bone position + offset. The offset is taken again from where the pivot is when following
        // starts or the bone changes, so the view never jumps.
        Transform followFrom;
        Vector3 followOffset;
        bool following;

        // the lens Place worked out last, for the Game view and (scaled) for pictures
        bool lensOrtho;
        float lensHalf; // orthographic: half the view height in metres; perspective: tan of half the vertical fov

        // FrameAvatar runs once more on the next Apply, after the pose has been written to the bones (a pose set
        // just before framing, e.g. 自然站立 at bind, is only on the transforms after PoseModel.Apply)
        AvatarRig reframeRig;
        Framing reframeFraming;
        float reframeAspect = 1f;

        static readonly HumanBodyBones[] ArmBones =
        {
            HumanBodyBones.LeftShoulder, HumanBodyBones.RightShoulder, HumanBodyBones.LeftUpperArm, HumanBodyBones.RightUpperArm,
            HumanBodyBones.LeftLowerArm, HumanBodyBones.RightLowerArm, HumanBodyBones.LeftHand, HumanBodyBones.RightHand,
        };
        static readonly HumanBodyBones[] LegBones =
        {
            HumanBodyBones.LeftLowerLeg, HumanBodyBones.RightLowerLeg, HumanBodyBones.LeftFoot, HumanBodyBones.RightFoot,
        };
        static readonly HumanBodyBones[] FootBones =
        {
            HumanBodyBones.LeftFoot, HumanBodyBones.RightFoot, HumanBodyBones.LeftToes, HumanBodyBones.RightToes,
        };

        // vertical field of view of the Game view, from the focal length
        public float VerticalFov
        {
            get { return Camera.FocalLengthToFieldOfView(Mathf.Clamp(FocalLength, 1f, 1000f), 24f); }
        }

        Quaternion Rotation
        {
            get { return Quaternion.Euler(Pitch, Yaw, 0f); }
        }

        // Makes the camera (DontSave, hidden), turns the scene's other cameras off and remembers them.
        public void Create()
        {
            if (Cam != null) return;
            turnedOff.Clear();
            float top = 0f;
            bool any = false;
            foreach (Camera c in Camera.allCameras) // the enabled ones
            {
                if (c == null) continue;
                if (!any || c.depth > top) top = c.depth;
                any = true;
                if (c.enabled)
                {
                    c.enabled = false;
                    turnedOff.Add(c);
                }
            }

            var go = new GameObject("MioVRCA Studio Camera") { hideFlags = HideFlags.HideAndDontSave };
            Camera cam = go.AddComponent<Camera>();
            cam.allowHDR = false;
            cam.allowMSAA = true;
            cam.depthTextureMode |= DepthTextureMode.Depth; // for shaders that read the depth texture (rim, soft edges)
            cam.depth = (any ? top : 0f) + 10f;
            cam.clearFlags = CameraClearFlags.SolidColor; // Backdrop decides every frame
            cam.backgroundColor = StartColor;
            // VRChat's mirror-only copies (e.g. an emulator's mirror clone) are not for the main view
            int mirror = LayerMask.NameToLayer("MirrorReflection");
            if (mirror >= 0) cam.cullingMask &= ~(1 << mirror);
            Cam = cam;
            following = false;
            followFrom = null;
            Place();
            Lens(1f);
        }

        // Destroys the camera and turns the other cameras back on.
        public void Destroy()
        {
            foreach (Camera c in turnedOff)
                if (c != null) c.enabled = true;
            turnedOff.Clear();
            // immediately: a deferred Destroy may not run any more when this is called on Play mode exit, and a hidden
            // DontSave camera would then be left over in edit mode
            if (Cam != null) Object.DestroyImmediate(Cam.gameObject);
            Cam = null;
            followFrom = null;
            following = false;
            reframeRig = null;
        }

        // Frames the avatar from the front: whole body, upper body or face, for a frame of this aspect (w/h).
        // Also sets FollowBone (hips / chest / head) so the frame keeps up with the pose.
        public void FrameAvatar(AvatarRig rig, Framing framing, float frameAspect)
        {
            if (rig == null || !rig.Alive) return;
            Frame(rig, framing, frameAspect);
            reframeRig = rig;
            reframeFraming = framing;
            reframeAspect = frameAspect;
        }

        // mouse deltas in GUI pixels (y down)
        public void Orbit(Vector2 delta)
        {
            float k = 0.3f * Mathf.Clamp(OrbitSpeed, 0.05f, 10f);
            Yaw = Mathf.Repeat(Yaw + delta.x * k + 180f, 360f) - 180f;
            Pitch = Mathf.Clamp(Pitch + delta.y * k, -85f, 85f);
        }

        // the point under the mouse moves with it (exactly so at the pivot's depth)
        public void Pan(Vector2 delta)
        {
            float perPixel = 2f * Distance * Mathf.Tan(VerticalFov * 0.5f * Mathf.Deg2Rad) / Mathf.Max(1, Screen.height);
            Quaternion r = Rotation;
            Vector3 move = (r * Vector3.right) * (-delta.x * perPixel) + (r * Vector3.up) * (delta.y * perPixel);
            Pivot += move;
            if (following) followOffset += move;
        }

        // wheel: positive = away
        public void Dolly(float wheel)
        {
            Distance = Mathf.Clamp(Distance * Mathf.Pow(1.1f, wheel / 3f), MinDistance, MaxDistance);
        }

        // starts following a bone from where the pivot is now (keeps the offset); also turns Follow on
        public void SetFollow(AvatarRig rig, HumanBodyBones bone)
        {
            FollowBone = bone;
            Follow = true;
            Transform b = rig != null && rig.Alive ? rig[bone] : null;
            if (b != null) StartFollowing(b);
            else following = false; // taken on the first Apply that has the bone
        }

        // Every frame: follows the bone, places the camera and sets its projection for the Game view.
        public void Apply(AvatarRig rig)
        {
            if (Cam == null) return;
            bool alive = rig != null && rig.Alive;
            if (reframeRig != null)
            {
                AvatarRig r = reframeRig;
                reframeRig = null;
                if (alive && r == rig) Frame(rig, reframeFraming, reframeAspect);
            }

            // Follow off for a while (Studio turns it off during handle drags) ends following here; when it is back
            // on, the offset is taken again from where the pivot is then, so the view does not jump.
            Transform b = Follow && alive ? rig[FollowBone] : null;
            if (b != null)
            {
                if (!following || b != followFrom) StartFollowing(b);
                Pivot = b.position + followOffset;
            }
            else following = false;

            Place();
            Lens(1f);
        }

        // For a picture of this aspect (w/h) that shows exactly what is inside FrameRect: the camera stays where it
        // is (same place, near and far, as the frame shows), only the vertical extent is the Game view's times
        // frameScale (= FrameRect height / view height). ViewLens puts the Game view's lens back.
        public void PictureLens(float aspect, float frameScale)
        {
            if (Cam == null) return;
            if (!(aspect > 1e-4f)) aspect = 1f; // also NaN
            if (!(frameScale > 1e-4f)) frameScale = 1f;
            Cam.aspect = aspect;
            Lens(frameScale);
        }

        public void ViewLens()
        {
            if (Cam == null) return;
            Cam.ResetAspect();
            Lens(1f);
        }

        // The output frame in GUI pixels: centred, aspect w/h, as large as fits in FrameMargin of the view.
        public static Rect FrameRect(float aspect, Vector2 view)
        {
            float h = view.y * FrameMargin, w = h * aspect;
            if (w > view.x * FrameMargin)
            {
                w = view.x * FrameMargin;
                h = w / aspect;
            }
            return new Rect((view.x - w) / 2f, (view.y - h) / 2f, w, h);
        }

        // Places the camera and works out the lens. Only standard projections, so Unity's depth-based passes
        // (screen-space shadows, the depth texture) know what they render with. Towards orthographic the camera moves
        // back along its axis with a narrower field of view (the size at the pivot stays), and near / far move back
        // with it: what is clipped stays exactly as before, so nothing between the old and the new place (a wall in
        // the scene behind the camera) comes into the picture, and the depth range gets only better.
        void Place()
        {
            if (Cam == null) return;
            float d = Mathf.Max(Distance, 1e-3f);
            float tanHalf = Mathf.Tan(VerticalFov * 0.5f * Mathf.Deg2Rad);
            float t = Ortho > 0f ? Mathf.Min(Ortho, 1f) : 0f; // also NaN
            float back = d / (1f - OrthoReach * t) - d;
            // shadows end at QualitySettings.shadowDistance from the camera: it must not move back so far that the
            // avatar loses them (only less perspective is left then)
            float sd = QualitySettings.shadowDistance;
            if (sd > 0f && QualitySettings.shadows != ShadowQuality.Disable)
                back = Mathf.Min(back, Mathf.Max(0f, 0.8f * sd - d));
            lensOrtho = t >= OrthoFrom;
            lensHalf = lensOrtho ? d * tanHalf : tanHalf * d / (d + back);

            Quaternion r = Rotation;
            Cam.transform.SetPositionAndRotation(Pivot - r * Vector3.forward * (d + back), r);
            Cam.nearClipPlane = back + Near;
            Cam.farClipPlane = back + Far;
        }

        // the lens of the last Place, with the vertical extent times frameScale
        void Lens(float frameScale)
        {
            Cam.ResetProjectionMatrix(); // fieldOfView / orthographicSize decide
            Cam.orthographic = lensOrtho;
            if (lensOrtho) Cam.orthographicSize = Mathf.Max(lensHalf * frameScale, 1e-4f);
            else Cam.fieldOfView = Mathf.Clamp(2f * Mathf.Atan(lensHalf * frameScale) * Mathf.Rad2Deg, 1e-3f, 179f);
        }

        void StartFollowing(Transform b)
        {
            followFrom = b;
            followOffset = Pivot - b.position;
            following = true;
        }

        void Frame(AvatarRig rig, Framing framing, float frameAspect)
        {
            if (!(frameAspect > 0.01f)) frameAspect = 1f;
            float H = Mathf.Max(0.2f, rig.Height);
            Transform hips = rig[HumanBodyBones.Hips];
            Transform neck = rig[HumanBodyBones.Neck], head = rig[HumanBodyBones.Head];
            HumanBodyBones bodyBone = rig[HumanBodyBones.Chest] != null ? HumanBodyBones.Chest :
                (rig[HumanBodyBones.Spine] != null ? HumanBodyBones.Spine : HumanBodyBones.Hips);
            Transform body = rig[bodyBone];
            Vector3 headTop = rig.HeadTop();

            float top, bottom, pitch, minHalfWidth = 0f;
            Vector3 c;
            HumanBodyBones followBone;
            HumanBodyBones[] wide1 = null, wide2 = null;
            switch (framing)
            {
                case Framing.Upper:
                    top = headTop.y + 0.05f * H;
                    bottom = hips.position.y - 0.04f * H;
                    c = body.position;
                    pitch = 3f;
                    followBone = bodyBone;
                    wide1 = ArmBones;
                    break;
                case Framing.Face:
                    top = headTop.y + 0.03f * H;
                    float neckY = neck != null ? neck.position.y : (head != null ? head.position.y - 0.12f * H : headTop.y - 0.25f * H);
                    bottom = neckY - 0.05f * H;
                    c = head != null ? head.position : headTop;
                    pitch = 0f;
                    followBone = head != null ? HumanBodyBones.Head : (neck != null ? HumanBodyBones.Neck : HumanBodyBones.Hips);
                    minHalfWidth = 0.1f * H; // the head with its hair, for tall frames
                    break;
                default:
                    top = headTop.y + 0.04f * H;
                    float low = rig.Root != null ? rig.Root.position.y : hips.position.y - 0.5f * H;
                    foreach (HumanBodyBones fb in FootBones)
                    {
                        Transform t = rig[fb];
                        if (t != null) low = Mathf.Min(low, t.position.y);
                    }
                    bottom = low - 0.02f * H;
                    c = body.position;
                    pitch = 2f;
                    followBone = HumanBodyBones.Hips;
                    wide1 = ArmBones;
                    wide2 = LegBones;
                    break;
            }
            float range = Mathf.Max(top - bottom, 0.05f);

            Pivot = new Vector3(c.x, (top + bottom) * 0.5f, c.z);
            Vector3 f = rig.Forward; // camera forward = -f: it looks at the avatar's front
            Yaw = Mathf.Atan2(-f.x, -f.z) * Mathf.Rad2Deg;
            Pitch = pitch;

            float sw = Mathf.Max(1, Screen.width), sh = Mathf.Max(1, Screen.height);
            float frameScale = FrameRect(frameAspect, new Vector2(sw, sh)).height / sh;
            float tanHalf = Mathf.Tan(VerticalFov * 0.5f * Mathf.Deg2Rad) * Mathf.Max(frameScale, 0.05f);
            float dist = range * 0.5f * 1.06f / tanHalf;

            // the arms (and legs) across must fit too
            Vector3 right = Rotation * Vector3.right;
            float spread = Mathf.Max(Spread(rig, wide1, right), Spread(rig, wide2, right));
            if (spread > 0f) spread += 0.05f * H; // hands and feet reach past their bones
            float halfWidth = Mathf.Max(minHalfWidth, spread);
            if (halfWidth > 0f) dist = Mathf.Max(dist, halfWidth * 1.06f / (tanHalf * frameAspect));
            Distance = Mathf.Clamp(dist, MinDistance, MaxDistance);

            FollowBone = followBone;
            Transform follow = rig[followBone];
            if (follow != null) StartFollowing(follow);
            else following = false;
        }

        // how far the bones reach to either side of the pivot along `right`
        float Spread(AvatarRig rig, HumanBodyBones[] bones, Vector3 right)
        {
            if (bones == null) return 0f;
            float m = 0f;
            foreach (HumanBodyBones hb in bones)
            {
                Transform t = rig[hb];
                if (t != null) m = Mathf.Max(m, Mathf.Abs(Vector3.Dot(t.position - Pivot, right)));
            }
            return m;
        }
    }
}
