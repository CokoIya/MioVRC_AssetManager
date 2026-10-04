// Two-bone IK for arms and legs: the hand (or foot) goes to the target, the elbow (or knee) bends towards the pole.
using UnityEngine;

namespace MioVRCA.Studio
{
    internal static class Ik
    {
        // A limb whose joint is less than this share of its length off the line from the shoulder (hip) to the hand
        // (foot) counts as straight: its bend says nothing reliable about where the joint points.
        const float StraightBelow = 0.02f;

        // sin(0.1 degrees): a smaller bend has no direction worth keeping (it may be rounding in the model)
        const float MinBendSin = 0.001745f;

        // Turns upper and mid so that end reaches target, or as far towards it as the bones reach. The joint bends
        // about its own hinge and the whole limb rolls about the line to the target until the joint is on the side
        // of pole (a world point), so the result depends only on target and pole, not on the way the drag went, and
        // a joint never bends sideways or twists. keepEndRotation: the end bone keeps its world rotation (the hand
        // does not twist while the arm moves).
        public static void TwoBone(Transform upper, Transform mid, Transform end, Vector3 target, Vector3 pole, bool keepEndRotation)
        {
            if (upper == null || mid == null || end == null) return;
            Quaternion endRot = end.rotation;
            Vector3 a = upper.position, b = mid.position, c = end.position;
            Vector3 u = b - a, v = c - b;
            float l1 = u.magnitude, l2 = v.magnitude;
            Vector3 toT = target - a;
            float d0 = toT.magnitude, d = d0;
            if (l1 < 1e-5f || l2 < 1e-5f || d0 < 1e-5f) return;
            // out of reach or too close: as far as the bones go, but never quite straight, so the bend keeps its plane
            float margin = (l1 + l2) * 1e-4f;
            d = Mathf.Clamp(d, Mathf.Abs(l1 - l2) + margin, l1 + l2 - margin);

            // 1. the joint angle that makes the shoulder-to-hand distance d, set by turning the middle bone about the
            // hinge (a bend that stays in the plane it is in relative to the upper bone)
            Vector3 hinge = Hinge(u, v, c - a, pole - a, l1 + l2);
            float cosJoint = Mathf.Clamp((l1 * l1 + l2 * l2 - d * d) / (2f * l1 * l2), -1f, 1f);
            float bend = 180f - Mathf.Acos(cosJoint) * Mathf.Rad2Deg; // 0 = straight
            Vector3 lower = Quaternion.AngleAxis(bend, hinge) * (u / l1);
            // with the hinge from the bend, v and lower both lie square to it, so this turns about the hinge itself
            mid.rotation = Quaternion.FromToRotation(v, lower) * mid.rotation;

            // 2. the whole limb aimed at the target
            float eps = margin * margin;
            Vector3 reach = end.position - a;
            if (reach.sqrMagnitude > eps) upper.rotation = Quaternion.FromToRotation(reach, toT) * upper.rotation;

            // 3. rolled about the line to the target until the joint is on the side of the pole
            Vector3 n = toT / d0;
            Vector3 jointSide = Vector3.ProjectOnPlane(mid.position - a, n), poleSide = Vector3.ProjectOnPlane(pole - a, n);
            if (jointSide.sqrMagnitude > eps && poleSide.sqrMagnitude > eps)
                upper.rotation = Quaternion.AngleAxis(AngleAbout(jointSide, poleSide, n), n) * upper.rotation;

            if (keepEndRotation) end.rotation = endRot;
        }

        // A world point on the side the middle joint bends to now. When the limb is straight there is no bend, so
        // fallbackBend (a world direction: back for elbows, forward for knees) is used.
        public static Vector3 PolePoint(Transform upper, Transform mid, Transform end, Vector3 fallbackBend)
        {
            Vector3 a = upper.position, b = mid.position, c = end.position;
            Vector3 ac = c - a;
            float len = (b - a).magnitude + (c - b).magnitude;
            Vector3 off = b - a;
            if (ac.sqrMagnitude > 1e-10f) off -= Vector3.Dot(off, ac) / ac.sqrMagnitude * ac;
            if (off.magnitude < len * StraightBelow) off = fallbackBend.normalized * len * 0.5f;
            else off = off.normalized * len * 0.5f;
            return b + off;
        }

        // The axis the middle joint bends about (u: upper bone, v: lower bone, line: shoulder to hand, toPole: shoulder
        // to pole; turning v about it by a positive angle bends the limb further). Normally the plane the limb bends
        // in now, so the joint keeps its own hinge and step 3 of TwoBone rolls the limb. A straight limb has no such
        // plane, and a nearly straight one bent away from the pole (an overstretched knee in the model; PolePoint
        // then gave the natural side) would be rolled half round, twisting the wrist or ankle: both bend towards the
        // pole instead.
        static Vector3 Hinge(Vector3 u, Vector3 v, Vector3 line, Vector3 toPole, float len)
        {
            Vector3 bent = Vector3.Cross(u, v);
            float uv = u.sqrMagnitude * v.sqrMagnitude;
            Vector3 jointSide = Vector3.ProjectOnPlane(u, line), poleSide = Vector3.ProjectOnPlane(toPole, line);
            if (bent.sqrMagnitude > uv * MinBendSin * MinBendSin &&
                (jointSide.magnitude >= len * StraightBelow || Vector3.Dot(jointSide, poleSide) >= 0f))
                return bent.normalized;
            // turning about Cross(toPole, u) moves the lower bone away from the pole: the joint goes towards it
            Vector3 h = Vector3.Cross(toPole, u);
            if (h.sqrMagnitude > toPole.sqrMagnitude * u.sqrMagnitude * 1e-8f) return h.normalized;
            if (bent.sqrMagnitude > uv * 1e-12f) return bent.normalized; // the pole is on the limb's line
            return Vector3.Cross(u, Mathf.Abs(u.y) < 0.9f * u.magnitude ? Vector3.up : Vector3.right).normalized;
        }

        // the angle (degrees) that turns from to to about axis (a unit vector; from and to lie square to it)
        static float AngleAbout(Vector3 from, Vector3 to, Vector3 axis)
        {
            return Mathf.Atan2(Vector3.Dot(Vector3.Cross(from, to), axis), Vector3.Dot(from, to)) * Mathf.Rad2Deg;
        }
    }
}
