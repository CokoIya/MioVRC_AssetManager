// Which object below the avatar a path means when it does not lead there level by level: the AI writes paths
// from what the tools showed it, now and then a little off. Plain strings, so it is tried outside Unity too.
using System;
using System.Collections.Generic;

namespace MioVRCA.Pipeline
{
    internal static class PathMatch
    {
        // rel: the paths of the objects below the avatar, names their names, weak whether one is most likely a bone
        // (it goes last among the suggestions), all in the order of the hierarchy. The index of the one object the
        // path means: the one of that name; else the one whose path ends with it ("Jacket/Sleeve" for
        // "Outfit/Jacket/Sleeve"); else the one with the same path in other capitals or spaces (as a whole, then
        // as its end). -1 when none or several fit, with why: the paths that fit, or those with the nearest names.
        public static int Pick(IList<string> rel, IList<string> names, IList<bool> weak, string asked, out string why)
        {
            why = "";
            string p = (asked ?? "").Replace('\\', '/').Trim('/');
            var named = Where(rel.Count, i => names[i] == p);
            if (named.Count == 1) return named[0];
            if (named.Count > 1)
            {
                why = "模型下有 " + named.Count + " 个名为「" + p + "」的物体，需填写完整路径：" + Listed(rel, named);
                return -1;
            }
            string tail = "/" + p, np = Norm(p), ntail = "/" + np;
            var norm = new string[rel.Count];
            for (int i = 0; i < rel.Count; i++) norm[i] = Norm(rel[i]);
            var tests = new Predicate<int>[] {
                i => rel[i].EndsWith(tail, StringComparison.Ordinal),
                i => norm[i] == np,
                i => norm[i].EndsWith(ntail, StringComparison.Ordinal),
            };
            foreach (var fits in tests)
            {
                var hit = Where(rel.Count, fits);
                if (hit.Count == 1) return hit[0];
                if (hit.Count > 1)
                {
                    why = "模型下未找到「" + asked + "」，有 " + hit.Count + " 个物体的路径与它相符，需填写完整路径：" + Listed(rel, hit);
                    return -1;
                }
            }
            var near = Nearest(names, weak, np);
            why = near.Count > 0 ? "模型下未找到「" + asked + "」。名称相近的物体：" + Listed(rel, near) : "模型下未找到「" + asked + "」";
            return -1;
        }

        static List<int> Where(int n, Predicate<int> fits)
        {
            var out_ = new List<int>();
            for (int i = 0; i < n; i++)
                if (fits(i)) out_.Add(i);
            return out_;
        }

        // a path to compare: small letters, the spaces at the ends of each name dropped, those inside made one
        public static string Norm(string p)
        {
            string[] seg = p.Split('/');
            for (int i = 0; i < seg.Length; i++)
                seg[i] = string.Join(" ", seg[i].Trim().ToLowerInvariant().Split(new[] { ' ', '\t' }, StringSplitOptions.RemoveEmptyEntries));
            return string.Join("/", seg);
        }

        // paths for a message: the first few
        static string Listed(IList<string> rel, List<int> which)
        {
            const int most = 6;
            var names = new List<string>();
            for (int i = 0; i < which.Count && i < most; i++) names.Add(rel[which[i]]);
            return string.Join("、", names.ToArray()) + (which.Count > most ? " …" : "");
        }

        // The objects whose names are nearest to the last name of the path (np, compared as Norm has it): the same
        // name, one name inside the other, or a few letters apart; nearest first, bones after the rest.
        static List<int> Nearest(IList<string> names, IList<bool> weak, string np)
        {
            string want = np.Substring(np.LastIndexOf('/') + 1);
            var scored = new List<KeyValuePair<long, int>>();
            if (want == "") return new List<int>();
            for (int i = 0; i < names.Count; i++)
            {
                string name = Norm(names[i]);
                int score;
                if (name == want) score = 0;
                else if (name.Length >= 2 && want.Length >= 2 && (name.Contains(want) || want.Contains(name))) score = 10;
                else
                {
                    int d = Distance(name, want), most = Math.Max(1, Math.Min(name.Length, want.Length) / 3);
                    if (d > most) continue;
                    score = 20 + d;
                }
                if (weak[i]) score += 5;
                scored.Add(new KeyValuePair<long, int>((long)score * 1000000 + i, i));
            }
            scored.Sort((a, b) => a.Key.CompareTo(b.Key));
            var out_ = new List<int>();
            foreach (var e in scored) out_.Add(e.Value);
            return out_;
        }

        // how many letters must change to turn one name into the other
        public static int Distance(string a, string b)
        {
            if (Math.Abs(a.Length - b.Length) > 8) return 99;
            var row = new int[b.Length + 1];
            for (int j = 0; j <= b.Length; j++) row[j] = j;
            for (int i = 1; i <= a.Length; i++)
            {
                int diag = row[0];
                row[0] = i;
                for (int j = 1; j <= b.Length; j++)
                {
                    int up = row[j];
                    row[j] = Math.Min(Math.Min(row[j] + 1, row[j - 1] + 1), diag + (a[i - 1] == b[j - 1] ? 0 : 1));
                    diag = up;
                }
            }
            return row[b.Length];
        }
    }
}
