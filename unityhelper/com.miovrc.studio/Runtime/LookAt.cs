// Eyes (and some of the head) turn to the camera or to a point. Applied every frame on top of the pose, so the pose
// itself keeps the head where the player put it (PoseModel.Apply writes the pose again before the next gaze, and
// PoseModel.Capture takes the gaze out of an edit through AvatarRig.GazeSet).
using UnityEngine;

namespace MioVRCA.Studio
{
    internal sealed class LookAt
    {
        public bool Enabled = true;
        public bool FollowCamera = true; // else Target
        public float Eyes = 1f;   // 0..1
        public float Head = 0.3f; // 0..1, the neck takes part of it
        public Vector3 Target;    // world, when not following the camera

        const float MaxHead = 60f, MaxEyes = 25f;       // degrees
        const float FadeFrom = 95f, FadeTo = 125f;      // a target behind the face: the gaze fades out around 110°
        const float NeckShare = 0.4f;

        AvatarRig bound;
        // the face's forward in each bone's own space (taken at bind, when the face looks along rig.Forward)
        Vector3 neckFwd = Vector3.forward, headFwd = Vector3.forward, leftEyeFwd = Vector3.forward, rightEyeFwd = Vector3.forward;

        // remembers which way the head and the eyes look in their own space
        public void Bind(AvatarRig rig)
        {
            bound = rig;
            if (rig == null || !rig.Alive) return;
            Vector3 fwd = rig.Forward;
            neckFwd = LocalForward(rig[HumanBodyBones.Neck], fwd);
            headFwd = LocalForward(rig[HumanBodyBones.Head], fwd);
            leftEyeFwd = LocalForward(rig.LeftEye, fwd);
            rightEyeFwd = LocalForward(rig.RightEye, fwd);
        }

        // after PoseModel.Apply, every frame
        public void Apply(AvatarRig rig, Vector3 cameraPosition)
        {
            if (rig == null || !rig.Alive) return;
            if (rig != bound) Bind(rig);
            // eyes the pose does not write start from rest every frame, so the gaze never adds up
            if (rig.LeftEyeExtra && rig.LeftEye != null) rig.LeftEye.localRotation = rig.LeftEyeRest;
            if (rig.RightEyeExtra && rig.RightEye != null) rig.RightEye.localRotation = rig.RightEyeRest;
            if (!Enabled) return;
            Transform head = rig[HumanBodyBones.Head];
            if (head == null) return;

            Vector3 target = FollowCamera ? cameraPosition : Target;
            Vector3 from;
            if (!rig.EyeCenter(out from)) from = head.position;
            Vector3 to = target - from;
            if (to.sqrMagnitude < 1e-6f) return;
            Vector3 face = head.rotation * headFwd;
            float off = Vector3.Angle(face, to);
            float fade = 1f - Mathf.SmoothStep(0f, 1f, Mathf.InverseLerp(FadeFrom, FadeTo, off));
            if (fade <= 0f) return;

            float hw = Mathf.Clamp01(Head) * fade;
            if (hw > 0f && off > 0.01f)
            {
                Vector3 axis = Vector3.Cross(face, to); // turning about Cross(a, b) moves a towards b
                if (axis.sqrMagnitude > 1e-12f)
                {
                    axis.Normalize();
                    float turn = Mathf.Min(off, MaxHead) * hw;
                    Transform neck = rig[HumanBodyBones.Neck];
                    if (neck != null)
                    {
                        Turn(rig, HumanBodyBones.Neck, neck, axis, turn * NeckShare);
                        Turn(rig, HumanBodyBones.Head, head, axis, turn * (1f - NeckShare));
                    }
                    else Turn(rig, HumanBodyBones.Head, head, axis, turn);
                }
            }

            float ew = Mathf.Clamp01(Eyes) * fade;
            if (ew > 0f)
            {
                Eye(rig, rig.LeftEye, rig.LeftEyeExtra ? HumanBodyBones.LastBone : HumanBodyBones.LeftEye, leftEyeFwd, target, ew);
                Eye(rig, rig.RightEye, rig.RightEyeExtra ? HumanBodyBones.LastBone : HumanBodyBones.RightEye, rightEyeFwd, target, ew);
            }
        }

        // a point straight ahead of the face, for when the target handle first appears
        public Vector3 DefaultTarget(AvatarRig rig)
        {
            if (rig == null || !rig.Alive) return Target;
            Vector3 from;
            if (!rig.EyeCenter(out from))
            {
                Transform head = rig[HumanBodyBones.Head];
                from = head != null ? head.position : rig.Root.position + Vector3.up * rig.Height * 0.9f;
            }
            return from + rig.Forward * 0.8f;
        }

        static Vector3 LocalForward(Transform t, Vector3 fwd)
        {
            return t != null ? Quaternion.Inverse(t.rotation) * fwd : Vector3.forward;
        }

        static void Eye(AvatarRig rig, Transform eye, HumanBodyBones bone, Vector3 localFwd, Vector3 target, float weight)
        {
            if (eye == null) return;
            Vector3 look = eye.rotation * localFwd, to = target - eye.position;
            if (to.sqrMagnitude < 1e-6f) return;
            float off = Vector3.Angle(look, to);
            if (off < 0.01f) return;
            Vector3 axis = Vector3.Cross(look, to);
            if (axis.sqrMagnitude < 1e-12f) return;
            Turn(rig, bone, eye, axis.normalized, Mathf.Min(off, MaxEyes) * weight);
        }

        // turns a bone in world space and notes it for PoseModel.Capture (humanoid bones only)
        static void Turn(AvatarRig rig, HumanBodyBones bone, Transform t, Vector3 axis, float degrees)
        {
            int i = (int)bone;
            bool noted = i >= 0 && i < rig.GazeSet.Length && rig.Bones[i] == t;
            if (noted && !rig.GazeSet[i])
            {
                rig.GazeBefore[i] = t.localRotation;
                rig.GazeSet[i] = true;
            }
            t.rotation = Quaternion.AngleAxis(degrees, axis) * t.rotation;
            if (noted) rig.GazeAfter[i] = t.localRotation;
        }
    }
}
