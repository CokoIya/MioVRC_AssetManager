// The check-up before an upload: what would stop the upload, what would look wrong in the game, and the
// avatar's figures with the ranks the VRChat SDK gives them.
//
// Read-only: nothing in the scene or in the assets is changed, and nothing is built. So these are the figures
// BEFORE the build: Modular Avatar, Avatar Optimizer and VRCFury change the avatar when it is uploaded.
using System;
using System.Collections;
using System.Collections.Generic;
using System.Globalization;
using System.Reflection;
using UnityEditor;
using UnityEngine;

namespace MioVRCA.Pipeline
{
    internal static class Checkup
    {
        const string PerfNS = "VRC.SDKBase.Validation.Performance.";
        const int MenuPageLimit = 8, ListMax = 20, BigTexture = 4096;
        static readonly string[] Tiers = { "Excellent", "Good", "Medium", "Poor" };
        const string Prebuild = "以上为构建前的数值：上传时 Modular Avatar、Avatar Optimizer（AAO）、VRCFury 会在构建阶段改动模型，实际数值会有出入（AAO 通常会减少网格、材质槽和骨骼的数量）。";

        public static object Run(Dictionary<string, object> args)
        {
            if (EditorApplication.isPlayingOrWillChangePlaymode)
                throw new PipelineException("Unity 处于 Play 模式，请先退出 Play 模式再体检（Play 模式下模型已被插件处理，数值与编辑时不同）");
            var watch = System.Diagnostics.Stopwatch.StartNew();
            var o = new Dictionary<string, object>();
            var items = new List<object>();
            o["prebuild"] = true;
            o["note"] = Prebuild;
            o["target"] = EditorUserBuildSettings.activeBuildTarget == BuildTarget.Android || EditorUserBuildSettings.activeBuildTarget == BuildTarget.iOS ? "quest" : "pc";

            List<Component> all = Refl.Avatars();
            Component avatar = Pick(all, J.Str(args, "avatar"));
            if (avatar == null)
            {
                // nothing to measure: the one thing to say is why
                var none = Item("avatar", "limit", "模型", "fail");
                none["text"] = "未找到";
                none["advice"] = Refl.DescriptorType == null
                    ? "该工程未安装 VRChat SDK（Avatars），请先在 ALCOM 或 VCC 中安装。"
                    : "请先在 Unity 中打开包含模型的场景。模型的根物体上需要有 VRC Avatar Descriptor 组件。";
                items.Add(none);
                o["avatar"] = "";
                o["items"] = items;
                o["ms"] = watch.ElapsedMilliseconds;
                return o;
            }
            Transform root = avatar.transform;
            o["avatar"] = root.name;
            o["path"] = Refl.ScenePath(root);

            bool fury = false, aao = false;
            try { fury = AvatarInspect.HasFury(root); } catch (Exception) { }
            try { aao = HasComponentNamed(root, "Anatawa12.AvatarOptimizer."); } catch (Exception) { }
            o["tools"] = new Dictionary<string, object> { { "modularAvatar", Refl.PackageVersion(Refl.MA + "ModularAvatarMenuItem") }, { "avatarOptimizer", aao }, { "vrcfury", fury } };

            // what the upload carries: everything under the avatar but what an EditorOnly object holds
            var renderers = new List<Renderer>();
            foreach (Renderer r in root.GetComponentsInChildren<Renderer>(true))
                if (r != null && !Refl.EditorOnly(r.transform)) renderers.Add(r);

            Step(items, () => Avatars(items, all, avatar));
            Step(items, () => Parameters(items, avatar));
            Dictionary<string, object> bones = null;
            Step(items, () => bones = PhysBones(items, root));
            Step(items, () => MissingScripts(items, root));
            Step(items, () => Menu(items, avatar));
            Step(items, () => Materials(items, root, renderers));
            Step(items, () => NoMesh(items, root, renderers));
            Dictionary<Texture, long> textures = null;
            Step(items, () => textures = Textures(items, renderers));

            var ranks = new Dictionary<string, object>();
            bool sdk = false;
            Step(items, () => sdk = SdkFigures(items, root, ranks, bones));
            if (!sdk) Step(items, () => OwnFigures(items, root, renderers, textures));
            o["sdkCalc"] = sdk;
            o["ranks"] = ranks;
            o["items"] = items;
            o["ms"] = watch.ElapsedMilliseconds;
            return o;
        }

        // one part failing (an SDK that names things differently) leaves the others standing
        static void Step(List<object> items, Action f)
        {
            try { f(); }
            catch (Exception e) { Debug.LogWarning("[MioVRCA] 体检的一项未能完成：" + (e.InnerException ?? e)); }
        }

        static Dictionary<string, object> Item(string id, string group, string label, string level)
        {
            return new Dictionary<string, object> { { "id", id }, { "group", group }, { "label", label }, { "level", level } };
        }

        // a line of an item's details: a name or path (t), and what is said about it — a note (n) or a number (v, u)
        static Dictionary<string, object> Line(string text, string note)
        {
            var d = new Dictionary<string, object> { { "t", text } };
            if (!string.IsNullOrEmpty(note)) d["n"] = note;
            return d;
        }

        static Dictionary<string, object> Line(string text, long value, string unit)
        {
            return new Dictionary<string, object> { { "t", text }, { "v", value }, { "u", unit } };
        }

        // an object as the player finds it in the Hierarchy: its path below the avatar
        static string PathOf(Transform root, Transform t)
        {
            return t == root ? root.name : Refl.RelPath(root, t);
        }

        // the avatar the request names; with none named the only active one, else the first active one
        static Component Pick(List<Component> all, string name)
        {
            if (all.Count == 0) return null;
            if (!string.IsNullOrEmpty(name))
            {
                foreach (Component c in all) if (Refl.ScenePath(c.transform) == name) return c;
                foreach (Component c in all) if (c.gameObject.name == name) return c;
                throw new PipelineException("场景中未找到模型「" + name + "」");
            }
            foreach (Component c in all) if (c.gameObject.activeInHierarchy) return c;
            return all[0];
        }

        static void Avatars(List<object> items, List<Component> all, Component picked)
        {
            if (all.Count < 2) return;
            var it = Item("avatar", "limit", "场景中的模型", "warn");
            it["value"] = all.Count;
            var lines = new List<object>();
            foreach (Component c in all)
                if (lines.Count < ListMax) lines.Add(Line(Refl.ScenePath(c.transform), c == picked ? "本次体检的模型" : (c.gameObject.activeInHierarchy ? "" : "已隐藏")));
            it["details"] = lines;
            it["advice"] = "场景中有多个模型。上传时请在 VRChat SDK 面板中确认所选的模型，本次体检只检查了其中一个。";
            items.Add(it);
        }

        // ---------- what stops an upload ----------

        static void Parameters(List<object> items, Component avatar)
        {
            AvatarInspect.ParamBudget b = AvatarInspect.Bits(avatar);
            int limit = AvatarInspect.ParameterBitLimit;
            bool over = b.total > limit;
            // with VRCFury the figure is a lower bound: close to the limit is already a reason to look
            bool near = b.fury && b.total > limit * 9 / 10;
            var it = Item("params", "limit", "同步参数", over ? "fail" : (near ? "warn" : "ok"));
            it["value"] = b.total;
            it["limit"] = limit;
            it["unit"] = "位";
            if (b.fury) it["atLeast"] = true;
            var lines = new List<object>();
            if (b.asset >= 0) lines.Add(Line("参数资产中已有", b.asset, "位"));
            if (b.added > 0) lines.Add(Line("Modular Avatar 在构建时生成（估算）", b.added, "位"));
            if (b.fury) lines.Add(Line("VRCFury 在构建时生成", "无法预估"));
            it["details"] = lines;
            if (over) it["advice"] = "同步参数超过 256 位，上传会被拒绝。请减少部件开关的数量（每个开关占 1 位，一组互斥的衣服占 8 位），或使用 VRCFury 的参数压缩功能。";
            else if (near) it["advice"] = "模型上有 VRCFury 组件，它的参数在构建时才生成，实际位数只会更多。当前已接近 256 位的上限，上传失败时请减少部件开关，或使用 VRCFury 的参数压缩功能。";
            items.Add(it);
        }

        static Dictionary<string, object> PhysBones(List<object> items, Transform root)
        {
            int n = AvatarInspect.PhysBones(root);
            if (n < 0) return null; // the SDK has no PhysBone: nothing to count
            int limit = AvatarInspect.PhysBoneLimit;
            var it = Item("physbones", "limit", "PhysBone 组件", n > limit ? "fail" : "ok");
            it["value"] = n;
            it["limit"] = limit;
            // who holds them: the avatar's own children, largest first
            var by = new List<KeyValuePair<string, int>>();
            foreach (Transform c in root)
            {
                int k = AvatarInspect.PhysBones(c);
                if (k > 0) by.Add(new KeyValuePair<string, int>(c.name, k));
            }
            by.Sort((a, b) => b.Value.CompareTo(a.Value));
            var lines = new List<object>();
            foreach (var kv in by)
                if (lines.Count < 8) lines.Add(Line(kv.Key, kv.Value, "个"));
            if (lines.Count > 0) it["details"] = lines;
            if (n > limit) it["advice"] = "PhysBone 组件超过 256 个，上传会被拒绝。隐藏的衣服同样计入：请移除不常用的衣服，或让同款不同配色的衣服共用一份物理（只装配一件，其余配色改为换色开关）。";
            items.Add(it);
            return it;
        }

        static void MissingScripts(List<object> items, Transform root)
        {
            int n = 0;
            var lines = new List<object>();
            foreach (Transform t in root.GetComponentsInChildren<Transform>(true))
            {
                int here = 0;
                foreach (Component c in t.GetComponents<Component>())
                    if (c == null) here++;
                if (here == 0 || Refl.EditorOnly(t)) continue;
                n += here;
                if (lines.Count < ListMax) lines.Add(Line(PathOf(root, t), here, "个"));
            }
            var it = Item("missing", "limit", "丢失的脚本", n > 0 ? "fail" : "ok");
            it["value"] = n;
            if (n > 0)
            {
                it["details"] = lines;
                it["advice"] = "这些物体上有脚本丢失的组件，说明该脚本所属的插件没有安装，带着它上传会失败或功能缺失。请在 ALCOM 或 VCC 中为工程安装对应的插件（常见的是 Modular Avatar、VRCFury）；确认不再需要时，可在 Unity 的 Inspector 中移除该组件。";
            }
            items.Add(it);
        }

        // the descriptor's own menu asset, page by page; menus Modular Avatar builds are split into pages by it
        static void Menu(List<object> items, Component avatar)
        {
            var menu = Field(avatar, "expressionsMenu") as UnityEngine.Object;
            if (menu == null) return;
            var over = new List<object>();
            int most = 0;
            MenuPages(menu, "", new HashSet<UnityEngine.Object>(), over, ref most, 0);
            var it = Item("menu", "limit", "菜单单页的控件", over.Count > 0 ? "fail" : "ok");
            it["value"] = most; // the fullest page
            it["limit"] = MenuPageLimit;
            if (over.Count > 0)
            {
                it["details"] = over;
                it["advice"] = "菜单的一页最多放 8 个控件，超出时无法上传。请在 Unity 中打开对应的菜单资产，把多出的控件移入子菜单。";
            }
            items.Add(it);
        }

        static void MenuPages(UnityEngine.Object menu, string path, HashSet<UnityEngine.Object> seen, List<object> over, ref int most, int depth)
        {
            if (menu == null || depth > 16 || !seen.Add(menu)) return; // a menu that leads back to itself is walked once
            var controls = Field(menu, "controls") as IList;
            if (controls == null) return;
            most = Math.Max(most, controls.Count);
            if (controls.Count > MenuPageLimit && over.Count < ListMax) over.Add(Line(path == "" ? "主菜单" : path, controls.Count, "个"));
            foreach (object c in controls)
            {
                if (c == null) continue;
                var sub = Field(c, "subMenu") as UnityEngine.Object;
                if (sub == null) continue;
                string name = Convert.ToString(Field(c, "name"));
                MenuPages(sub, (path == "" ? "" : path + "/") + (string.IsNullOrEmpty(name) ? sub.name : name), seen, over, ref most, depth + 1);
            }
        }

        // ---------- what looks wrong in the game ----------

        static void Materials(List<object> items, Transform root, List<Renderer> renderers)
        {
            int n = 0;
            var lines = new List<object>();
            var family = new Dictionary<Material, string>();
            foreach (Renderer r in renderers)
            {
                // a particle system's second slot is its trail material, empty unless it has trails
                bool particles = r is ParticleSystemRenderer;
                Material[] ms = r.sharedMaterials;
                int slots = SubMeshes(r);
                for (int i = 0; i < ms.Length; i++)
                {
                    Material m = ms[i];
                    string note = null, fam = "";
                    if (m == null)
                    {
                        if (particles || (slots >= 0 && i >= slots)) continue;
                        note = "材质槽为空";
                    }
                    else if (m.shader == null || m.shader.name == "Hidden/InternalErrorShader")
                    {
                        if (!family.TryGetValue(m, out fam)) { fam = ShaderFamily(m); family[m] = fam; }
                        note = "着色器未安装";
                    }
                    else if (!m.shader.isSupported) note = "着色器不受支持";
                    if (note == null) continue;
                    n++;
                    if (lines.Count >= ListMax) continue;
                    var d = Line(PathOf(root, r.transform), note);
                    if (m != null) d["m"] = m.name;
                    if (fam != "") d["f"] = fam;
                    lines.Add(d);
                }
            }
            var it = Item("materials", "look", "粉色或缺失的材质", n > 0 ? "fail" : "ok");
            it["value"] = n;
            if (n > 0)
            {
                it["details"] = lines;
                it["advice"] = "这些材质在游戏中会显示为粉色。多数情况是着色器插件没有安装：请在 ALCOM 或 VCC 中为工程安装对应的着色器（如 lilToon、Poiyomi），待 Unity 编译完成后重新体检。材质槽为空时，请在 Unity 中为该网格重新指定材质。";
            }
            items.Add(it);
        }

        // how many material slots the renderer's mesh draws with (-1: not known)
        static int SubMeshes(Renderer r)
        {
            var smr = r as SkinnedMeshRenderer;
            if (smr != null) return smr.sharedMesh != null ? smr.sharedMesh.subMeshCount : -1;
            var mf = r.GetComponent<MeshFilter>();
            return mf != null && mf.sharedMesh != null ? mf.sharedMesh.subMeshCount : -1;
        }

        // Which shader a material was made for, when its shader is gone: the properties saved in the material
        // file still carry the shader's own names.
        static string ShaderFamily(Material m)
        {
            try
            {
                SerializedProperty floats = new SerializedObject(m).FindProperty("m_SavedProperties.m_Floats");
                if (floats != null && floats.isArray)
                    for (int i = 0; i < floats.arraySize && i < 2000; i++)
                    {
                        SerializedProperty first = floats.GetArrayElementAtIndex(i).FindPropertyRelative("first");
                        string k = first != null ? first.stringValue : "";
                        if (k == "_lilToonVersion") return "lilToon";
                        if (k == "shader_is_using_thry_editor" || k == "_ShaderOptimizerEnabled" || k == "_ForgotToLockMaterial") return "Poiyomi";
                        if (k == "_utsTechnique" || k == "_utsVersion") return "Unity Toon Shader";
                        if (k == "_MToonVersion") return "MToon";
                    }
            }
            catch (Exception) { }
            string name = m.name.ToLowerInvariant();
            if (name.Contains("liltoon") || name.StartsWith("lil")) return "lilToon";
            if (name.Contains("poiyomi") || name.StartsWith("poi")) return "Poiyomi";
            return "";
        }

        static void NoMesh(List<object> items, Transform root, List<Renderer> renderers)
        {
            int n = 0;
            var lines = new List<object>();
            foreach (Renderer r in renderers)
            {
                var smr = r as SkinnedMeshRenderer;
                if (smr == null || smr.sharedMesh != null) continue;
                n++;
                if (lines.Count < ListMax) lines.Add(Line(PathOf(root, r.transform), ""));
            }
            var it = Item("nomesh", "look", "没有网格的蒙皮网格", n > 0 ? "warn" : "ok");
            it["value"] = n;
            if (n > 0)
            {
                it["details"] = lines;
                it["advice"] = "这些 Skinned Mesh Renderer 没有指定网格，在游戏中不会显示。通常是模型文件（FBX）没有导入或已被移动，请重新导入对应的素材。";
            }
            items.Add(it);
        }

        // every texture the avatar's materials use, with what it takes of video memory (an estimate from its
        // size and format; nothing is read from the textures themselves)
        static Dictionary<Texture, long> Textures(List<object> items, List<Renderer> renderers)
        {
            var sizes = new Dictionary<Texture, long>();
            var seen = new HashSet<Material>();
            foreach (Renderer r in renderers)
                foreach (Material m in r.sharedMaterials)
                {
                    if (m == null || m.shader == null || !seen.Add(m)) continue;
                    int[] ids;
                    try { ids = m.GetTexturePropertyNameIDs(); }
                    catch (Exception) { continue; }
                    foreach (int id in ids)
                    {
                        Texture t = m.GetTexture(id);
                        if (t != null && !sizes.ContainsKey(t)) sizes[t] = TextureBytes(t);
                    }
                }
            var list = new List<KeyValuePair<Texture, long>>(sizes);
            list.Sort((a, b) => b.Value.CompareTo(a.Value));
            int big = 0, fixable = 0; // fixable: the ones the one-click fix can lower (not a package's, with import settings)
            foreach (var kv in list)
            {
                if (Mathf.Max(kv.Key.width, kv.Key.height) < BigTexture) continue;
                big++;
                if (Fixes.CanLower(AssetDatabase.GetAssetPath(kv.Key))) fixable++;
            }
            var lines = new List<object>();
            foreach (var kv in list)
            {
                if (lines.Count >= 5) break;
                Texture t = kv.Key;
                var t2 = t as Texture2D;
                var d = Line(t.name, t.width + " × " + t.height + "　" + (t2 != null ? t2.format.ToString() : t.GetType().Name) + "　" + Megabytes(kv.Value) + " MB");
                if (Mathf.Max(t.width, t.height) >= BigTexture) d["w"] = true;
                string ap = AssetDatabase.GetAssetPath(t); // what a fix is asked for by
                if (ap != "") d["p"] = ap;
                lines.Add(d);
            }
            var it = Item("textures", "look", "4096 像素以上的贴图", big > 0 ? "warn" : "ok");
            it["value"] = big;
            it["fixable"] = fixable;
            it["unit"] = "张";
            it["total"] = sizes.Count;
            if (lines.Count > 0) { it["details"] = lines; it["detailsTitle"] = "占用显存最多的贴图"; }
            if (big > 0) it["advice"] = "分辨率达到 4096 像素的贴图占用显存较多，容易使模型的贴图显存评级变差。可在 Unity 中选中该贴图，将导入设置中的 Max Size 改为 2048。";
            items.Add(it);
            return sizes;
        }

        static string Megabytes(long bytes)
        {
            return (bytes / 1048576.0).ToString("0.0", CultureInfo.InvariantCulture);
        }

        public static long TextureBytes(Texture t)
        {
            double px = (double)t.width * t.height, bpp = 4;
            var t2 = t as Texture2D;
            if (t2 != null) bpp = BytesPerPixel(t2.format.ToString());
            else if (t is Cubemap) { px *= 6; bpp = BytesPerPixel(((Cubemap)t).format.ToString()); }
            return (long)(px * bpp * (t.mipmapCount > 1 ? 4.0 / 3.0 : 1.0));
        }

        // by the format's name, so that a format a Unity version does not have is no compile error
        static double BytesPerPixel(string f)
        {
            if (f.StartsWith("ASTC"))
            {
                int x = f.IndexOf('x');
                int block;
                if (x > 0 && int.TryParse(f.Substring(x + 1), out block) && block > 0) return 16.0 / (block * block);
                return 1;
            }
            if (f.StartsWith("DXT1") || f == "BC4" || f.StartsWith("ETC_RGB4") || f == "ETC2_RGB" || f == "ETC2_RGBA1" || (f.StartsWith("EAC_R") && !f.StartsWith("EAC_RG")) || f.StartsWith("PVRTC_RGB4") || f.StartsWith("PVRTC_RGBA4")) return 0.5;
            if (f.StartsWith("PVRTC")) return 0.25;
            if (f.StartsWith("DXT5") || f == "BC5" || f == "BC7" || f == "BC6H" || f == "ETC2_RGBA8" || f.StartsWith("EAC_RG") || f.StartsWith("ETC2_RGBA8")) return 1;
            if (f == "Alpha8" || f == "R8") return 1;
            if (f == "R16" || f == "RG16" || f == "RGB565" || f == "RGBA4444" || f == "ARGB4444" || f == "RHalf") return 2;
            if (f == "RGB24") return 3;
            if (f == "RGBAHalf" || f == "RGFloat" || f == "RGBA64") return 8;
            if (f == "RGBAFloat") return 16;
            return 4;
        }

        // ---------- the figures, as the SDK counts and rates them ----------

        class Fig
        {
            public string id, label, cat, field, sub, unit, kind;
            public Fig(string id, string label, string cat, string field, string sub = null, string unit = null, string kind = null)
            { this.id = id; this.label = label; this.cat = cat; this.field = field; this.sub = sub; this.unit = unit; this.kind = kind; }
        }

        static readonly Fig[] Figs =
        {
            new Fig("triangles", "三角面", "PolyCount", "polyCount"),
            new Fig("skinnedMeshes", "蒙皮网格", "SkinnedMeshCount", "skinnedMeshCount"),
            new Fig("meshes", "普通网格", "MeshCount", "meshCount"),
            new Fig("materials", "材质槽", "MaterialCount", "materialCount"),
            new Fig("bones", "骨骼", "BoneCount", "boneCount"),
            new Fig("physBoneComponents", "PhysBone 组件", "PhysBoneComponentCount", "physBone", "componentCount"),
            new Fig("physBoneTransforms", "PhysBone 影响的骨骼", "PhysBoneTransformCount", "physBone", "transformCount"),
            new Fig("physBoneColliders", "PhysBone 碰撞体", "PhysBoneColliderCount", "physBone", "colliderCount"),
            new Fig("physBoneChecks", "PhysBone 碰撞检测", "PhysBoneCollisionCheckCount", "physBone", "collisionCheckCount"),
            new Fig("contacts", "Contact 组件", "ContactCount", "contactCount"),
            new Fig("constraints", "约束", "ConstraintsCount", "constraintsCount"),
            new Fig("constraintDepth", "约束深度", "ConstraintDepth", "constraintDepth"),
            new Fig("animators", "Animator", "AnimatorCount", "animatorCount"),
            new Fig("lights", "灯光", "LightCount", "lightCount"),
            new Fig("particleSystems", "粒子系统", "ParticleSystemCount", "particleSystemCount"),
            new Fig("particles", "粒子总数", "ParticleTotalCount", "particleTotalCount"),
            new Fig("particleTriangles", "粒子网格面数", "ParticleMaxMeshPolyCount", "particleMaxMeshPolyCount"),
            new Fig("particleTrails", "粒子拖尾", "ParticleTrailsEnabled", "particleTrailsEnabled", null, null, "bool"),
            new Fig("particleCollision", "粒子碰撞", "ParticleCollisionEnabled", "particleCollisionEnabled", null, null, "bool"),
            new Fig("trailRenderers", "Trail Renderer", "TrailRendererCount", "trailRendererCount"),
            new Fig("lineRenderers", "Line Renderer", "LineRendererCount", "lineRendererCount"),
            new Fig("cloths", "Cloth 组件", "ClothCount", "clothCount"),
            new Fig("clothVertices", "Cloth 顶点", "ClothMaxVertices", "clothMaxVertices"),
            new Fig("colliders", "物理碰撞体", "PhysicsColliderCount", "physicsColliderCount"),
            new Fig("rigidbodies", "刚体", "PhysicsRigidbodyCount", "physicsRigidbodyCount"),
            new Fig("audioSources", "音源", "AudioSourceCount", "audioSourceCount"),
            new Fig("raycasts", "Raycast 组件", "RaycastCount", "raycastCount"),
            new Fig("textureMemory", "贴图显存", "TextureMegabytes", "textureMegabytes", null, "MB", "float"),
            new Fig("bounds", "包围盒", "AABB", "aabb", null, "米", "bounds"),
        };

        static readonly Dictionary<string, string> Advice = new Dictionary<string, string>
        {
            { "triangles", "三角面数超出 Poor 档的上限。可移除不常用的衣服和配饰，或安装 Avatar Optimizer（AAO），在构建时自动移除被遮挡的网格。" },
            { "skinnedMeshes", "蒙皮网格偏多。安装 Avatar Optimizer（AAO）并在模型上添加 Trace And Optimize 组件后，构建时会自动合并网格。" },
            { "meshes", "网格偏多。安装 Avatar Optimizer（AAO）并在模型上添加 Trace And Optimize 组件后，构建时会自动合并网格。" },
            { "materials", "材质槽偏多。安装 Avatar Optimizer（AAO）并在模型上添加 Trace And Optimize 组件后，构建时会自动合并相同的材质槽。" },
            { "bones", "骨骼偏多。每件衣服都带着一套骨骼：可移除不常用的衣服，或安装 Avatar Optimizer（AAO），在构建时自动移除无用的骨骼。" },
            { "physBoneComponents", "PhysBone 组件偏多。隐藏的衣服同样计入：可移除不常用的衣服，或让同款不同配色的衣服共用一份物理。" },
            { "physBoneTransforms", "受 PhysBone 影响的骨骼偏多。头发、尾巴和飘带占得最多，可移除不常用的衣服，或让同款不同配色的衣服共用一份物理。" },
            { "physBoneColliders", "PhysBone 碰撞体偏多。可移除不常用的衣服，或在 PhysBone 组件中去掉不需要的碰撞体。" },
            { "physBoneChecks", "PhysBone 碰撞检测次数偏多。可在 PhysBone 组件中减少碰撞体，或移除不常用的衣服。" },
            { "lights", "实时灯光对性能影响很大，建议移除模型上的灯光。" },
            { "particleSystems", "粒子系统偏多，建议移除不需要的特效。" },
            { "particles", "粒子数量偏多，建议降低粒子系统的 Max Particles。" },
            { "textureMemory", "贴图显存偏高。可在贴图的导入设置中将 Max Size 由 4096 改为 2048，并保持压缩开启。" },
            { "bounds", "模型的包围盒过大。请检查是否有物体离模型很远，或网格的 Bounds 被设置得过大。" },
        };
        const string AdviceOther = "该项超出 Poor 档的上限，模型的整体评级因此为 Very Poor，部分玩家的性能设置会默认不显示这类模型。";

        static Type FindPerf(string name)
        {
            return Refl.Find(PerfNS + "Stats." + name) ?? Refl.Find(PerfNS + name);
        }

        // The SDK's own calculator, for Windows and for Android: the numbers and ranks its panel shows.
        // false when this SDK has none that can be called (the caller counts by itself then).
        static bool SdkFigures(List<object> items, Transform root, Dictionary<string, object> ranks, Dictionary<string, object> bones)
        {
            Type calc = Refl.Find(PerfNS + "AvatarPerformance"), stats = FindPerf("AvatarPerformanceStats");
            Type cat = Refl.Find(PerfNS + "AvatarPerformanceCategory"), rating = Refl.Find(PerfNS + "PerformanceRating");
            if (calc == null || stats == null || cat == null) return false;
            MethodInfo run = null;
            foreach (MethodInfo m in calc.GetMethods(BindingFlags.Public | BindingFlags.Static))
                if (m.Name == "CalculatePerformanceStats" && (run == null || m.GetParameters().Length > run.GetParameters().Length)) run = m;
            if (run == null) return false;
            bool perPlatform = false;
            foreach (ParameterInfo p in run.GetParameters()) perPlatform |= p.ParameterType == typeof(bool);

            object pc = Calculate(run, stats, root, false);
            if (pc == null) return false;
            // an SDK whose calculator takes no platform rates for the editor's build target only
            object quest = perPlatform ? Calculate(run, stats, root, true) : null;
            MethodInfo rate = stats.GetMethod("GetPerformanceRatingForCategory", new[] { cat });
            string overall = Rating(rate, pc, cat, "Overall"), overallQuest = quest != null ? Rating(rate, quest, cat, "Overall") : "";
            if (overall != "") ranks["pc"] = overall;
            if (overallQuest != "") ranks["quest"] = overallQuest;

            int added = 0;
            foreach (Fig f in Figs)
            {
                object v = Value(pc, f);
                if (v == null) continue;
                string r = Rating(rate, pc, cat, f.cat), rq = quest != null ? Rating(rate, quest, cat, f.cat) : "";
                List<object> tiers = f.kind == "bounds" || f.kind == "bool" ? null : TierLimits(stats, rating, f, false);
                // the PhysBone count is the upload limit's row already: the SDK's ranks go onto it
                if (f.id == "physBoneComponents" && bones != null)
                {
                    if (r != "") bones["rating"] = r;
                    if (rq != "") bones["ratingQuest"] = rq;
                    if (tiers != null) bones["tiers"] = tiers;
                    added++;
                    continue;
                }
                var it = Item("fig." + f.id, "figure", f.label, r == "VeryPoor" ? "warn" : "ok");
                if (f.kind == "bounds")
                {
                    Vector3 s = ((Bounds)v).size;
                    it["text"] = Num(s.x) + " × " + Num(s.y) + " × " + Num(s.z);
                }
                else if (f.kind == "bool") { it["value"] = (bool)v ? 1 : 0; it["text"] = (bool)v ? "已启用" : "未启用"; }
                else if (f.kind == "float") it["value"] = Math.Round(Convert.ToDouble(v, CultureInfo.InvariantCulture), 1);
                else it["value"] = Convert.ToInt64(v, CultureInfo.InvariantCulture);
                if (f.unit != null) it["unit"] = f.unit;
                if (r != "") it["rating"] = r;
                if (rq != "") it["ratingQuest"] = rq;
                if (tiers != null) it["tiers"] = tiers;
                if (r == "VeryPoor") { string a; it["advice"] = Advice.TryGetValue(f.id, out a) ? a : AdviceOther; }
                items.Add(it);
                added++;
            }
            return added > 0;
        }

        static object Calculate(MethodInfo run, Type stats, Transform root, bool mobile)
        {
            object st = null;
            ConstructorInfo withPlatform = stats.GetConstructor(new[] { typeof(bool) });
            if (withPlatform != null) st = withPlatform.Invoke(new object[] { mobile });
            else if (stats.GetConstructor(Type.EmptyTypes) != null) st = Activator.CreateInstance(stats);
            if (st == null) return null;
            ParameterInfo[] ps = run.GetParameters();
            var a = new object[ps.Length];
            for (int i = 0; i < ps.Length; i++)
            {
                Type t = ps[i].ParameterType;
                if (t == typeof(string)) a[i] = root.name;
                else if (t == typeof(GameObject)) a[i] = root.gameObject;
                else if (t == typeof(bool)) a[i] = mobile;
                else if (t.IsInstanceOfType(st)) a[i] = st;
                else return null; // a signature this code does not know
            }
            run.Invoke(null, a);
            return st;
        }

        static string Rating(MethodInfo rate, object stats, Type cat, string name)
        {
            if (rate == null || !Enum.IsDefined(cat, name)) return "";
            try
            {
                string r = Convert.ToString(rate.Invoke(stats, new[] { Enum.Parse(cat, name) }));
                return r == "None" ? "" : r;
            }
            catch (Exception) { return ""; }
        }

        // a figure's value: null when this SDK has no such field or did not count it
        static object Value(object holder, Fig f)
        {
            object v = Field(holder, f.field);
            if (v != null && f.sub != null) v = Field(v, f.sub);
            return v;
        }

        // what each rank allows at most, Excellent to Poor (above Poor is Very Poor)
        static List<object> TierLimits(Type stats, Type rating, Fig f, bool mobile)
        {
            if (rating == null) return null;
            MethodInfo level = null;
            foreach (MethodInfo m in stats.GetMethods(BindingFlags.Public | BindingFlags.Static))
                if (m.Name == "GetStatLevelForRating") level = m;
            if (level == null) return null;
            var o = new List<object>();
            try
            {
                foreach (string tier in Tiers)
                {
                    if (!Enum.IsDefined(rating, tier)) return null;
                    object r = Enum.Parse(rating, tier);
                    object lv = level.Invoke(null, level.GetParameters().Length == 2 ? new[] { r, (object)mobile } : new[] { r });
                    object v = lv != null ? Value(lv, f) : null;
                    if (v == null) return null;
                    o.Add(f.kind == "float" ? Math.Round(Convert.ToDouble(v, CultureInfo.InvariantCulture), 1) : (object)Convert.ToInt64(v, CultureInfo.InvariantCulture));
                }
            }
            catch (Exception) { return null; }
            return o;
        }

        static string Num(float f) { return f.ToString("0.##", CultureInfo.InvariantCulture); }

        static object Field(object o, string name)
        {
            if (o == null) return null;
            FieldInfo f = o.GetType().GetField(name, BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Instance);
            return f != null ? f.GetValue(o) : null;
        }

        static bool HasComponentNamed(Transform root, string prefix)
        {
            foreach (Component c in root.GetComponentsInChildren<Component>(true))
                if (c != null && (c.GetType().FullName ?? "").StartsWith(prefix)) return true;
            return false;
        }

        // ---------- the figures, counted here (an SDK without the calculator): no ranks ----------

        static void OwnFigures(List<object> items, Transform root, List<Renderer> renderers, Dictionary<Texture, long> textures)
        {
            long triangles = 0, slots = 0;
            int skinned = 0, meshes = 0;
            var bones = new HashSet<Transform>();
            Bounds? box = null;
            foreach (Renderer r in renderers)
            {
                var smr = r as SkinnedMeshRenderer;
                Mesh mesh = null;
                if (smr != null)
                {
                    skinned++;
                    mesh = smr.sharedMesh;
                    foreach (Transform b in smr.bones) if (b != null) bones.Add(b);
                }
                else if (r is MeshRenderer)
                {
                    meshes++;
                    var mf = r.GetComponent<MeshFilter>();
                    mesh = mf != null ? mf.sharedMesh : null;
                }
                else continue;
                slots += r.sharedMaterials.Length;
                if (mesh != null)
                    for (int s = 0; s < mesh.subMeshCount; s++) triangles += mesh.GetIndexCount(s) / 3;
                if (box.HasValue) { Bounds b = box.Value; b.Encapsulate(r.bounds); box = b; }
                else box = r.bounds;
            }
            Own(items, "triangles", "三角面", triangles);
            Own(items, "skinnedMeshes", "蒙皮网格", skinned);
            Own(items, "meshes", "普通网格", meshes);
            Own(items, "materials", "材质槽", slots);
            Own(items, "bones", "骨骼", bones.Count);
            PhysBoneFigures(items, root);
            Own(items, "contacts", "Contact 组件", Count(root, Refl.Find("VRC.Dynamics.ContactBase")));
            Own(items, "constraints", "约束", Count(root, typeof(UnityEngine.Animations.IConstraint)) + Count(root, Refl.Find("VRC.Dynamics.VRCConstraintBase")));
            Own(items, "animators", "Animator", Count(root, typeof(Animator)));
            Own(items, "lights", "灯光", Count(root, typeof(Light)));
            Own(items, "particleSystems", "粒子系统", Count(root, typeof(ParticleSystem)));
            Own(items, "audioSources", "音源", Count(root, typeof(AudioSource)));
            if (textures != null)
            {
                long bytes = 0;
                foreach (var kv in textures) bytes += kv.Value;
                var it = Item("fig.textureMemory", "figure", "贴图显存", "info");
                it["value"] = Math.Round(bytes / 1048576.0, 1);
                it["unit"] = "MB";
                items.Add(it);
            }
            if (box.HasValue)
            {
                var it = Item("fig.bounds", "figure", "包围盒", "info");
                Vector3 s = box.Value.size;
                it["text"] = Num(s.x) + " × " + Num(s.y) + " × " + Num(s.z);
                it["unit"] = "米";
                items.Add(it);
            }
        }

        static void Own(List<object> items, string id, string label, long value)
        {
            var it = Item("fig." + id, "figure", label, "info");
            it["value"] = value;
            items.Add(it);
        }

        static int Count(Transform root, Type t)
        {
            if (t == null) return 0;
            int n = 0;
            foreach (Component c in root.GetComponentsInChildren(t, true))
                if (c != null && !Refl.EditorOnly(c.transform)) n++;
            return n;
        }

        // PhysBones as VRChat counts them, near enough: the transforms each one moves, the colliders in use,
        // and one collision check per moved transform and collider
        static void PhysBoneFigures(List<object> items, Transform root)
        {
            Type pb = Refl.Find("VRC.SDK3.Dynamics.PhysBone.Components.VRCPhysBone");
            if (pb == null) return;
            long moved = 0, checks = 0;
            var colliders = new HashSet<UnityEngine.Object>();
            foreach (Component c in root.GetComponentsInChildren(pb, true))
            {
                if (c == null || Refl.EditorOnly(c.transform)) continue;
                Transform top = Field(c, "rootTransform") as Transform;
                if (top == null) top = c.transform;
                var skip = new HashSet<Transform>();
                var ignore = Field(c, "ignoreTransforms") as IList;
                if (ignore != null)
                    foreach (object x in ignore) if (x is Transform && (Transform)x != null) skip.Add((Transform)x);
                int n = Under(top, skip);
                int mine = 0;
                var cs = Field(c, "colliders") as IList;
                if (cs != null)
                    foreach (object x in cs)
                    {
                        var u = x as UnityEngine.Object;
                        if (u == null) continue;
                        colliders.Add(u);
                        mine++;
                    }
                moved += n;
                checks += (long)Math.Max(n - 1, 0) * mine;
            }
            Own(items, "physBoneTransforms", "PhysBone 影响的骨骼", moved);
            Own(items, "physBoneColliders", "PhysBone 碰撞体", colliders.Count);
            Own(items, "physBoneChecks", "PhysBone 碰撞检测", checks);
        }

        static int Under(Transform t, HashSet<Transform> skip)
        {
            if (skip.Contains(t)) return 0;
            int n = 1;
            foreach (Transform c in t) n += Under(c, skip);
            return n;
        }
    }
}
