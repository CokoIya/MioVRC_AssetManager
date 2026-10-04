// Hand shapes from finger muscles, for either hand or both, with a strength (0 = the muscles' neutral, 1 = the full
// shape). Eight of the shapes are VRChat's own hand gestures (the SDK's proxy_hands_*.anim, left and right averaged),
// so they look the way the avatar's gestures look in VRChat; OK and 比心 are our first estimates, to tune in Unity.
using System;
using UnityEngine;

namespace MioVRCA.Studio
{
    internal enum HandSide { Left, Right, Both }

    internal static class HandPresets
    {
        // display names (through L.T); the index is the preset number
        public static readonly string[] Names = { "放松", "握拳", "张开", "比耶", "指向", "手枪", "点赞", "OK", "摇滚", "比心" };

        static readonly string[] FingerNames = { "Thumb", "Index", "Middle", "Ring", "Little" };
        static readonly string[] PartNames = { "1 Stretched", "Spread", "2 Stretched", "3 Stretched" };

        // Per preset, per finger (thumb, index, middle, ring, little): 1 Stretched, Spread, 2 Stretched, 3 Stretched.
        // Stretched: negative curls, positive opens. Spread turns the finger sideways: VRChat's shapes give the index and
        // middle negative and the ring and little positive values to fan the hand. Like VRChat's gestures, some values
        // pass ±1 (thumb, middle / ring spread): Unity carries the muscle on past its limit.
        static readonly float[][] Shapes =
        {
            // 放松 relaxed (VRChat idle): a slight curl that grows towards the little finger
            new[] { -1.15f, -0.57f, -0.03f, 0.69f,   0.18f, -1.01f, 0.10f, 0.22f,   -0.04f, -2.35f, -0.18f, 0.04f,   -0.16f, 1.72f, -0.26f, 0.08f,   -0.18f, 0.45f, -0.26f, -0.08f },
            // 握拳 fist
            new[] { -1.35f, -0.88f, -0.20f, 0.53f,   -0.33f, -0.85f, -0.30f, -0.18f,   -0.49f, -1.93f, -0.41f, -0.24f,   -0.52f, 1.31f, -0.45f, -0.26f,   -0.58f, 0.34f, -0.47f, -0.32f },
            // 张开 open, spread
            new[] { -1.22f, -0.40f, 0.49f, 1.04f,   0.51f, -0.43f, 0.51f, 0.79f,   0.43f, -1.37f, 0.36f, 0.66f,   0.40f, 1.86f, 0.32f, 0.64f,   0.33f, 1.03f, 0.30f, 0.58f },
            // 比耶 V sign: index and middle open, ring and little curled, the thumb over them
            new[] { -1.87f, -1.18f, -0.25f, 0.44f,   0.41f, -0.54f, 0.47f, 0.57f,   0.20f, -1.89f, 0.34f, 0.51f,   -0.51f, 1.35f, -0.45f, 0.16f,   -0.58f, 0.34f, -0.42f, 0.08f },
            // 指向 pointing: the index open
            new[] { -1.40f, -0.88f, -0.25f, 0.44f,   0.42f, -0.74f, 0.47f, 0.57f,   -0.49f, -1.93f, -0.41f, -0.24f,   -0.51f, 1.32f, -0.45f, -0.26f,   -0.58f, 0.34f, -0.47f, -0.32f },
            // 手枪 finger gun: index open, thumb up
            new[] { -0.30f, -0.12f, 0.49f, 1.04f,   0.48f, -0.71f, 0.46f, 0.67f,   -0.29f, -1.85f, -0.44f, -0.20f,   -0.32f, 1.09f, -0.48f, -0.22f,   -0.39f, 0.23f, -0.50f, -0.28f },
            // 点赞 thumbs up
            new[] { -0.38f, 0.13f, 0.08f, 1.00f,   -0.33f, -0.85f, -0.30f, -0.18f,   -0.49f, -1.93f, -0.41f, -0.24f,   -0.52f, 1.31f, -0.45f, -0.26f,   -0.58f, 0.34f, -0.47f, -0.32f },
            // OK: thumb and index bent to meet, the others open and fanned (estimate)
            new[] { -1.25f, -0.65f, -0.05f, 0.45f,   -0.15f, -0.95f, -0.25f, -0.05f,   0.35f, -1.60f, 0.30f, 0.55f,   0.25f, 1.75f, 0.20f, 0.50f,   0.15f, 1.00f, 0.15f, 0.45f },
            // 摇滚 horns: index and little open, middle and ring curled under the thumb
            new[] { -1.28f, -0.28f, -0.47f, 0.44f,   0.41f, -0.54f, 0.47f, 0.57f,   -0.51f, -2.08f, -0.74f, 0.39f,   -0.51f, 1.35f, -0.67f, 0.16f,   -0.04f, 0.27f, 0.24f, 0.42f },
            // 比心 finger heart: index bent towards the thumb, the thumb laid across it, the others curled (estimate)
            new[] { -0.80f, -0.30f, 0.20f, 0.80f,   -0.15f, -1.00f, -0.40f, -0.10f,   -0.49f, -1.93f, -0.41f, -0.24f,   -0.52f, 1.31f, -0.45f, -0.26f,   -0.58f, 0.34f, -0.47f, -0.32f },
        };

        static readonly Predicate<HumanBodyBones> LeftFingers = b => b >= HumanBodyBones.LeftThumbProximal && b <= HumanBodyBones.LeftLittleDistal;
        static readonly Predicate<HumanBodyBones> RightFingers = b => b >= HumanBodyBones.RightThumbProximal && b <= HumanBodyBones.RightLittleDistal;
        static readonly Predicate<HumanBodyBones> AllFingers = PoseModel.IsFinger;

        // muscle index by [hand * 20 + finger * 4 + part] (hand 0 = left), -1 when Unity has no such muscle
        static int[] muscles;

        // Sets the fingers of the chosen hand(s) to preset `preset` at `strength` (0..1). Calls model.BeginEdit()
        // first, changes only finger bones, and updates the model.
        public static void Apply(PoseModel model, int preset, HandSide side, float strength)
        {
            if (model == null || preset < 0 || preset >= Shapes.Length || !model.Ready || !model.HasHumanPose) return;
            model.BeginEdit();
            Set(model, preset, side, strength);
        }

        // Apply without the undo step (StandNaturally uses it inside its own edit).
        internal static void Set(PoseModel model, int preset, HandSide side, float strength)
        {
            if (model == null || preset < 0 || preset >= Shapes.Length || !model.HasHumanPose) return;
            Resolve();
            Vector3 bodyPosition;
            Quaternion bodyRotation;
            float[] m = model.GetMuscles(out bodyPosition, out bodyRotation);
            float[] shape = Shapes[preset];
            float s = Mathf.Clamp01(strength);
            for (int hand = 0; hand < 2; hand++)
            {
                if ((hand == 0 && side == HandSide.Right) || (hand == 1 && side == HandSide.Left)) continue;
                for (int k = 0; k < 20; k++)
                {
                    int mi = muscles[hand * 20 + k];
                    if (mi >= 0 && mi < m.Length) m[mi] = shape[k] * s;
                }
            }
            Predicate<HumanBodyBones> only = side == HandSide.Left ? LeftFingers : side == HandSide.Right ? RightFingers : AllFingers;
            model.SetMuscles(m, bodyPosition, bodyRotation, false, only);
        }

        // finds the finger muscles by name once ("Left Index 1 Stretched", "Right Thumb Spread", ...)
        static void Resolve()
        {
            if (muscles != null) return;
            var found = new int[40];
            string[] names = HumanTrait.MuscleName;
            for (int hand = 0; hand < 2; hand++)
                for (int f = 0; f < FingerNames.Length; f++)
                    for (int p = 0; p < PartNames.Length; p++)
                    {
                        string want = (hand == 0 ? "Left " : "Right ") + FingerNames[f] + " " + PartNames[p];
                        found[hand * 20 + f * 4 + p] = Array.IndexOf(names, want);
                    }
            muscles = found;
        }
    }
}
