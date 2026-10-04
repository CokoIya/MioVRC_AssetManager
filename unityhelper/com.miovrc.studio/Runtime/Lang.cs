// The studio's own texts in 中文, English and 日本語. As in MioVRCA, the Chinese text is the key: write L.T("中文")
// in the code and add the same text here. Which language: the one MioVRCA asked for, else the last one used.
using System.Collections.Generic;

namespace MioVRCA.Studio
{
    internal static class L
    {
        public static string Lang = "zh"; // "zh", "en" or "ja"

        public static void Use(string lang)
        {
            Lang = lang == "en" || lang == "ja" ? lang : "zh";
        }

        public static string T(string zh)
        {
            if (zh == null || Lang == "zh") return zh;
            Dictionary<string, string> d = Lang == "en" ? En : Ja;
            string s;
            return d.TryGetValue(zh, out s) ? s : zh;
        }

        // T, then string.Format: L.F("已保存到 {0}", path)
        public static string F(string zh, params object[] args)
        {
            return string.Format(T(zh), args);
        }

        // Every L.T / L.F text of the package, in English: sentence case, no full stop on labels, buttons and one-line
        // notices; MioVRCA's terms (模型 avatar, 摄影棚 photo studio, 骨骼 bones, 表情 expression, 形态键 blend shape).
        static readonly Dictionary<string, string> En = new Dictionary<string, string>
        {
            // top bar, hidden interface, countdown
            { "MioVRCA 摄影棚", "MioVRCA photo studio" },
            { "切换模型", "Switch avatar" },
            { "已切换到 {0}", "Switched to {0}" },
            { "隐藏界面（Tab）", "Hide UI (Tab)" },
            { "退出摄影棚", "Exit photo studio" },
            { "Tab 显示界面", "Tab to show UI" },
            { "Esc 取消", "Esc to cancel" },

            // no avatar
            { "正在查找模型…", "Looking for an avatar…" },
            { "没有可以拍摄的模型", "No avatar to photograph" },
            { "进入播放模式后，NDMF、VRCFury 等工具可能还在生成模型，请稍候。", "After Play mode starts, tools such as NDMF and VRCFury may still be building the avatar. Please wait a moment." },
            { "重新查找", "Search again" },
            { "场景里没有找到模型（带 VRC Avatar Descriptor 或 Humanoid 的物体）", "No avatar found in the scene (an object with a VRC Avatar Descriptor or a Humanoid rig)" },
            { "没能使用这个模型", "Couldn't use this avatar" },
            { "没能使用这个模型：{0}", "Couldn't use this avatar: {0}" },
            { "模型已经不在场景里了", "The avatar is no longer in the scene" },
            { "该模型不是 Humanoid，无法摆姿势", "This avatar isn't Humanoid, so it can't be posed" },
            { "找不到模型的 Hips 骨骼，无法摆姿势", "Can't find the avatar's Hips bone, so it can't be posed" },

            // the panels (rail)
            { "姿势", "Pose" },
            { "手势", "Hands" },
            { "表情", "Expression" },
            { "镜头", "Camera" },
            { "灯光", "Lights" },
            { "背景", "Background" },
            { "输出", "Output" },

            // status line
            { "左键拖动关节摆姿势 · 右键拖动旋转视角 · 中键平移 · 滚轮缩放 · Space 拍照 · Tab 隐藏界面 · Ctrl+Z 撤销", "Left-drag joints to pose · Right-drag to orbit · Middle-drag to pan · Scroll to zoom · Space to take a photo · Tab to hide UI · Ctrl+Z to undo" },
            { "左键拖动关节摆姿势 · 左键拖动空白处转动主光 · 右键拖动旋转视角 · 中键平移 · 滚轮缩放 · Space 拍照", "Left-drag joints to pose · Left-drag empty space to turn the key light · Right-drag to orbit · Middle-drag to pan · Scroll to zoom · Space to take a photo" },

            // 姿势: undo, bones, pose slots
            { "撤销", "Undo" },
            { "重做", "Redo" },
            { "没有可以撤销的操作", "Nothing to undo" },
            { "没有可以重做的操作", "Nothing to redo" },
            { "骨骼", "Bones" },
            { "显示骨骼（H）", "Show bones (H)" },
            { "已显示骨骼控制点", "Bone handles shown" },
            { "已隐藏骨骼控制点", "Bone handles hidden" },
            { "细节", "More bones" },
            { "手指", "Fingers" },
            { "移动身体时固定双脚", "Pin feet when moving the body" },
            { "拖动灵敏度", "Drag sensitivity" },
            { "姿势槽", "Pose slots" },
            { "已套用姿势槽 {0}", "Pose slot {0} applied" },
            { "这个姿势槽是空的：右键点击可以保存当前姿势", "This pose slot is empty. Right-click it to save the current pose" },
            { "当前姿势已保存到姿势槽 {0}", "Current pose saved to slot {0}" },
            { "左键套用 · 右键保存", "Left-click to apply · Right-click to save" },
            { "自然站立", "Stand naturally" },
            { "初始姿势", "Initial pose" },

            // 姿势: gaze
            { "视线", "Gaze" },
            { "视线跟随", "Look-at" },
            { "看镜头", "Look at camera" },
            { "看目标", "Look at target" },
            { "拖动画面里的白色圆圈来移动视线目标", "Drag the white circle in the view to move the look target" },
            { "眼睛", "Eyes" },
            { "头部", "Head" },

            // the bone handles: names (the fingers' go into 「{0}{1}第 {2} 节」, so they are lowercase) and hints
            { "视线目标", "Look target" },
            { "腰", "Hips" },
            { "脊柱", "Spine" },
            { "胸", "Chest" },
            { "上胸", "Upper chest" },
            { "脖子", "Neck" },
            { "头", "Head" },
            { "左肩", "Left shoulder" },
            { "右肩", "Right shoulder" },
            { "左上臂", "Left upper arm" },
            { "右上臂", "Right upper arm" },
            { "左手肘", "Left elbow" },
            { "右手肘", "Right elbow" },
            { "左手", "Left hand" },
            { "右手", "Right hand" },
            { "左大腿", "Left thigh" },
            { "右大腿", "Right thigh" },
            { "左膝", "Left knee" },
            { "右膝", "Right knee" },
            { "左脚", "Left foot" },
            { "右脚", "Right foot" },
            { "左脚尖", "Left toes" },
            { "右脚尖", "Right toes" },
            { "拇指", "thumb" },
            { "食指", "index finger" },
            { "中指", "middle finger" },
            { "无名指", "ring finger" },
            { "小指", "little finger" },
            { "{0}{1}第 {2} 节", "{0} {1}, joint {2}" },
            { "{0} · 拖动：移动身体 · Shift：转身 · 双击：复原", "{0} · Drag: move the body · Shift: turn · Double-click: reset" },
            { "{0} · 拖动：移动（IK） · Shift：旋转 · 双击：复原", "{0} · Drag: move (IK) · Shift: rotate · Double-click: reset" },
            { "{0} · 拖动：改变弯曲方向 · 双击：复原", "{0} · Drag: change the bend direction · Double-click: reset" },
            { "{0} · 拖动：弯曲 · Alt：扭转 · 双击：复原", "{0} · Drag: bend · Alt: twist · Double-click: reset" },
            { "{0} · 拖动：移动视线目标 · 双击：回到正前方", "{0} · Drag: move the look target · Double-click: back to straight ahead" },
            { "{0} · 拖动：旋转 · Alt：扭转 · 双击：复原", "{0} · Drag: rotate · Alt: twist · Double-click: reset" },
            { "{0} · Esc：取消", "{0} · Esc: cancel" },

            // 手势
            { "双手", "Both hands" },
            { "力度", "Strength" },
            { "预设", "Presets" },
            { "已套用手势：{0}", "Hand shape applied: {0}" },
            { "套用后还可以拖动手指微调 · Ctrl+Z 撤销", "After applying, you can still drag the fingers to fine-tune · Ctrl+Z to undo" },
            { "放松", "Relaxed" },
            { "握拳", "Fist" },
            { "张开", "Open hand" },
            { "比耶", "Peace" },
            { "指向", "Point" },
            { "手枪", "Finger gun" },
            { "点赞", "Thumbs up" },
            { "摇滚", "Rock" },
            { "比心", "Finger heart" },

            // 表情
            { "这个模型没有带形态键的网格，没有可以调整的表情。", "This avatar has no mesh with blend shapes, so there is no expression to adjust." },
            { "选择网格", "Choose a mesh" },
            { "这个网格没有形态键。", "This mesh has no blend shapes." },
            { "筛选形态键…", "Filter blend shapes…" },
            { "全部复原", "Reset all" },
            { "已套用表情槽 {0}", "Expression slot {0} applied" },
            { "这个表情槽是空的：右键点击可以保存当前表情", "This expression slot is empty. Right-click it to save the current expression" },
            { "当前表情已保存到表情槽 {0}", "Current expression saved to slot {0}" },
            { "表情槽：左键套用 · 右键保存 · 双击滑条归零", "Expression slots: left-click to apply · right-click to save · double-click a slider to reset it to 0" },
            { "没有名字匹配的形态键。", "No blend shapes match that name." },

            // 镜头
            { "取景", "Framing" },
            { "全身", "Full body" },
            { "上半身", "Upper body" },
            { "脸", "Face" },
            { "焦距", "Focal length" },
            { "微调", "Fine-tune" },
            { "正交", "Orthographic" },
            { "视角", "View" },
            { "跟随骨骼", "Follow bone" },
            { "三分线", "Rule-of-thirds grid" },
            { "旋转灵敏度", "Orbit sensitivity" },
            { "重置视角（F）", "Reset view (F)" },
            { "右键拖动旋转 · 中键拖动平移 · 滚轮推拉 · 换焦距时人物大小不变", "Right-drag to orbit · Middle-drag to pan · Scroll to move closer · Changing the focal length keeps the avatar the same size" },

            // 灯光
            { "主光", "Key light" },
            { "水平角度", "Horizontal angle" },
            { "高度角度", "Vertical angle" },
            { "跟随镜头", "Follow camera" },
            { "在画面上拖动", "Drag in the view" },
            { "左键在画面空白处拖动，转动主光", "Left-drag an empty spot in the view to turn the key light" },
            { "强度", "Intensity" },
            { "色温", "Color temperature" },
            { "阴影与轮廓光", "Shadows and rim light" },
            { "阴影", "Shadows" },
            { "阴影强度", "Shadow strength" },
            { "轮廓光", "Rim light" },
            { "轮廓光强度", "Rim light intensity" },
            { "环境", "Environment" },
            { "环境光", "Ambient light" },
            { "只用摄影棚灯光", "Studio lights only" },
            { "恢复默认灯光", "Reset lights" },

            // 背景 (the gradient names sit under small tiles: keep them short)
            { "场景", "Scene" },
            { "纯色", "Solid" },
            { "渐变", "Gradient" },
            { "透明", "Transparent" },
            { "颜色", "Color" },
            { "晴空", "Sky" },
            { "樱花", "Sakura" },
            { "黄昏", "Dusk" },
            { "夜色", "Night" },
            { "影棚灰", "Gray" },
            { "照片保存为透明背景的 PNG；画面里用灰色代表透明的部分。", "Photos are saved as PNG with a transparent background; gray in the view stands for the transparent parts." },
            { "保留场景原本的样子作为背景，场景里的物体也会拍进照片。", "The scene stays as it is as the background, and objects in it also appear in the photo." },

            // 输出
            { "比例", "Aspect ratio" },
            { "比例：{0}", "Ratio: {0}" },
            { "尺寸（短边）", "Size (short side)" },
            { "照片 {0} × {1} 像素", "Photo: {0} × {1} pixels" },
            { "定时", "Timer" },
            { "关", "Off" },
            { "{0} 秒", "{0} s" },
            { "定时：关", "Timer: off" },
            { "定时：{0} 秒", "Timer: {0} s" },
            { "超采样（更细腻，稍慢）", "Supersampling (finer, slower)" },
            { "照片", "Photos" },
            { "打开照片文件夹", "Open photo folder" },
            { "无法创建照片文件夹：{0}", "Couldn't create the photo folder: {0}" },
            { "照片文件夹：{0}", "Photo folder: {0}" },
            { "界面", "Interface" },
            { "界面大小", "UI size" },
            { "透明 PNG", "Transparent PNG" },

            // taking and finding photos
            { "已取消倒计时", "Countdown cancelled" },
            { "已保存照片", "Photo saved" },
            { "照片没有保存成功", "The photo wasn't saved" },
            { "拍照时出错：{0}", "Couldn't take the photo: {0}" },
            { "相机不可用，无法拍照", "The camera isn't available, so no photo can be taken" },
            { "显卡不支持这么大的照片，请把尺寸调小", "The graphics card can't make a photo this large. Choose a smaller size" },
            { "显存不足，无法拍这么大的照片，请把尺寸调小", "Not enough video memory for a photo this large. Choose a smaller size" },
            { "照片编码失败", "Couldn't encode the photo" },
            { "照片保存失败：", "Couldn't save the photo: " },
            { "点击在文件夹中显示", "Click to show in folder" },
            { "这张照片已经不在原来的位置了", "This photo is no longer where it was" },
            { "照片在：{0}", "Photo location: {0}" },
        };

        // The same texts in Japanese: です・ます for sentences, short noun phrases for labels; MioVRCA's terms (模型 アバター,
        // 摄影棚 撮影スタジオ, 骨骼 ボーン, 姿势 ポーズ, 手势 ハンドサイン, 镜头 カメラ, 灯光 ライト, 形态键 シェイプキー).
        static readonly Dictionary<string, string> Ja = new Dictionary<string, string>
        {
            // top bar, hidden interface, countdown
            { "MioVRCA 摄影棚", "MioVRCA 撮影スタジオ" },
            { "切换模型", "アバター切り替え" },
            { "已切换到 {0}", "{0} に切り替えました" },
            { "隐藏界面（Tab）", "UI を隠す（Tab）" },
            { "退出摄影棚", "撮影スタジオを終了" },
            { "Tab 显示界面", "Tab で UI を表示" },
            { "Esc 取消", "Esc でキャンセル" },

            // no avatar
            { "正在查找模型…", "アバターを探しています…" },
            { "没有可以拍摄的模型", "撮影できるアバターがありません" },
            { "进入播放模式后，NDMF、VRCFury 等工具可能还在生成模型，请稍候。", "Play モードに入った後も、NDMF や VRCFury などのツールがアバターを生成していることがあります。しばらくお待ちください。" },
            { "重新查找", "再検索" },
            { "场景里没有找到模型（带 VRC Avatar Descriptor 或 Humanoid 的物体）", "シーンにアバター（VRC Avatar Descriptor の付いた、または Humanoid のオブジェクト）が見つかりません" },
            { "没能使用这个模型", "このアバターを使用できませんでした" },
            { "没能使用这个模型：{0}", "このアバターを使用できませんでした：{0}" },
            { "模型已经不在场景里了", "アバターがシーンからなくなっています" },
            { "该模型不是 Humanoid，无法摆姿势", "このアバターは Humanoid ではないため、ポーズを付けられません" },
            { "找不到模型的 Hips 骨骼，无法摆姿势", "アバターの Hips ボーンが見つからないため、ポーズを付けられません" },

            // the panels (rail)
            { "姿势", "ポーズ" },
            { "手势", "ハンドサイン" },
            { "表情", "表情" },
            { "镜头", "カメラ" },
            { "灯光", "ライト" },
            { "背景", "背景" },
            { "输出", "出力" },

            // status line
            { "左键拖动关节摆姿势 · 右键拖动旋转视角 · 中键平移 · 滚轮缩放 · Space 拍照 · Tab 隐藏界面 · Ctrl+Z 撤销", "関節を左ドラッグでポーズ · 右ドラッグで視点を回転 · 中ドラッグで移動 · ホイールでズーム · Space で撮影 · Tab で UI を隠す · Ctrl+Z で元に戻す" },
            { "左键拖动关节摆姿势 · 左键拖动空白处转动主光 · 右键拖动旋转视角 · 中键平移 · 滚轮缩放 · Space 拍照", "関節を左ドラッグでポーズ · 何もない所を左ドラッグでキーライトを回転 · 右ドラッグで視点を回転 · 中ドラッグで移動 · ホイールでズーム · Space で撮影" },

            // 姿势: undo, bones, pose slots
            { "撤销", "元に戻す" },
            { "重做", "やり直す" },
            { "没有可以撤销的操作", "元に戻せる操作がありません" },
            { "没有可以重做的操作", "やり直せる操作がありません" },
            { "骨骼", "ボーン" },
            { "显示骨骼（H）", "ボーンを表示（H）" },
            { "已显示骨骼控制点", "ボーンのハンドルを表示しました" },
            { "已隐藏骨骼控制点", "ボーンのハンドルを非表示にしました" },
            { "细节", "その他のボーン" },
            { "手指", "指" },
            { "移动身体时固定双脚", "体を動かすときに両足を固定" },
            { "拖动灵敏度", "ドラッグ感度" },
            { "姿势槽", "ポーズスロット" },
            { "已套用姿势槽 {0}", "ポーズスロット {0} を適用しました" },
            { "这个姿势槽是空的：右键点击可以保存当前姿势", "このポーズスロットは空です。右クリックで現在のポーズを保存できます" },
            { "当前姿势已保存到姿势槽 {0}", "現在のポーズをスロット {0} に保存しました" },
            { "左键套用 · 右键保存", "左クリックで適用 · 右クリックで保存" },
            { "自然站立", "自然な立ち姿" },
            { "初始姿势", "初期ポーズ" },

            // 姿势: gaze
            { "视线", "視線" },
            { "视线跟随", "視線追従" },
            { "看镜头", "カメラ目線" },
            { "看目标", "ターゲットを見る" },
            { "拖动画面里的白色圆圈来移动视线目标", "画面内の白い円をドラッグして視線ターゲットを動かします" },
            { "眼睛", "目" },
            { "头部", "頭" },

            // the bone handles: names (the fingers' go into 「{0}{1}第 {2} 节」) and hints
            { "视线目标", "視線ターゲット" },
            { "腰", "腰" },
            { "脊柱", "背骨" },
            { "胸", "胸" },
            { "上胸", "上胸" },
            { "脖子", "首" },
            { "头", "頭" },
            { "左肩", "左肩" },
            { "右肩", "右肩" },
            { "左上臂", "左上腕" },
            { "右上臂", "右上腕" },
            { "左手肘", "左ひじ" },
            { "右手肘", "右ひじ" },
            { "左手", "左手" },
            { "右手", "右手" },
            { "左大腿", "左太もも" },
            { "右大腿", "右太もも" },
            { "左膝", "左ひざ" },
            { "右膝", "右ひざ" },
            { "左脚", "左足" },
            { "右脚", "右足" },
            { "左脚尖", "左つま先" },
            { "右脚尖", "右つま先" },
            { "拇指", "親指" },
            { "食指", "人差し指" },
            { "中指", "中指" },
            { "无名指", "薬指" },
            { "小指", "小指" },
            { "{0}{1}第 {2} 节", "{0}{1} 第{2}節" },
            { "{0} · 拖动：移动身体 · Shift：转身 · 双击：复原", "{0} · ドラッグ：体を移動 · Shift：向きを変える · ダブルクリック：リセット" },
            { "{0} · 拖动：移动（IK） · Shift：旋转 · 双击：复原", "{0} · ドラッグ：移動（IK） · Shift：回転 · ダブルクリック：リセット" },
            { "{0} · 拖动：改变弯曲方向 · 双击：复原", "{0} · ドラッグ：曲げる向きを変更 · ダブルクリック：リセット" },
            { "{0} · 拖动：弯曲 · Alt：扭转 · 双击：复原", "{0} · ドラッグ：曲げる · Alt：ひねる · ダブルクリック：リセット" },
            { "{0} · 拖动：移动视线目标 · 双击：回到正前方", "{0} · ドラッグ：視線ターゲットを移動 · ダブルクリック：正面に戻す" },
            { "{0} · 拖动：旋转 · Alt：扭转 · 双击：复原", "{0} · ドラッグ：回転 · Alt：ひねる · ダブルクリック：リセット" },
            { "{0} · Esc：取消", "{0} · Esc：キャンセル" },

            // 手势 (preset names as VRChat players call the hand signs)
            { "双手", "両手" },
            { "力度", "強さ" },
            { "预设", "プリセット" },
            { "已套用手势：{0}", "ハンドサイン「{0}」を適用しました" },
            { "套用后还可以拖动手指微调 · Ctrl+Z 撤销", "適用後も指をドラッグして微調整できます · Ctrl+Z で元に戻す" },
            { "放松", "リラックス" },
            { "握拳", "グー" },
            { "张开", "パー" },
            { "比耶", "ピース" },
            { "指向", "指差し" },
            { "手枪", "ピストル" },
            { "点赞", "サムズアップ" },
            { "摇滚", "ロック" },
            { "比心", "指ハート" },

            // 表情
            { "这个模型没有带形态键的网格，没有可以调整的表情。", "このアバターにはシェイプキーのあるメッシュがないため、調整できる表情がありません。" },
            { "选择网格", "メッシュを選択" },
            { "这个网格没有形态键。", "このメッシュにはシェイプキーがありません。" },
            { "筛选形态键…", "シェイプキーを絞り込み…" },
            { "全部复原", "すべてリセット" },
            { "已套用表情槽 {0}", "表情スロット {0} を適用しました" },
            { "这个表情槽是空的：右键点击可以保存当前表情", "この表情スロットは空です。右クリックで現在の表情を保存できます" },
            { "当前表情已保存到表情槽 {0}", "現在の表情をスロット {0} に保存しました" },
            { "表情槽：左键套用 · 右键保存 · 双击滑条归零", "表情スロット：左クリックで適用 · 右クリックで保存 · スライダーをダブルクリックで 0 に戻す" },
            { "没有名字匹配的形态键。", "名前が一致するシェイプキーがありません。" },

            // 镜头
            { "取景", "フレーミング" },
            { "全身", "全身" },
            { "上半身", "上半身" },
            { "脸", "顔" },
            { "焦距", "焦点距離" },
            { "微调", "微調整" },
            { "正交", "平行投影" },
            { "视角", "視点" },
            { "跟随骨骼", "ボーンに追従" },
            { "三分线", "三分割グリッド" },
            { "旋转灵敏度", "回転感度" },
            { "重置视角（F）", "視点をリセット（F）" },
            { "右键拖动旋转 · 中键拖动平移 · 滚轮推拉 · 换焦距时人物大小不变", "右ドラッグで回転 · 中ドラッグで移動 · ホイールで寄り・引き · 焦点距離を変えてもアバターの大きさは変わりません" },

            // 灯光
            { "主光", "キーライト" },
            { "水平角度", "水平角度" },
            { "高度角度", "垂直角度" },
            { "跟随镜头", "カメラに追従" },
            { "在画面上拖动", "画面上でドラッグ" },
            { "左键在画面空白处拖动，转动主光", "画面の何もない所を左ドラッグして、キーライトを回します" },
            { "强度", "強度" },
            { "色温", "色温度" },
            { "阴影与轮廓光", "影とリムライト" },
            { "阴影", "影" },
            { "阴影强度", "影の濃さ" },
            { "轮廓光", "リムライト" },
            { "轮廓光强度", "リムライトの強度" },
            { "环境", "環境" },
            { "环境光", "環境光" },
            { "只用摄影棚灯光", "撮影スタジオのライトのみ" },
            { "恢复默认灯光", "ライトをリセット" },

            // 背景 (the mode buttons and the gradient names are narrow: グラデ, not グラデーション)
            { "场景", "シーン" },
            { "纯色", "単色" },
            { "渐变", "グラデ" },
            { "透明", "透明" },
            { "颜色", "色" },
            { "晴空", "青空" },
            { "樱花", "桜" },
            { "黄昏", "夕暮れ" },
            { "夜色", "夜空" },
            { "影棚灰", "グレー" },
            { "照片保存为透明背景的 PNG；画面里用灰色代表透明的部分。", "写真は背景が透明な PNG で保存されます。画面ではグレーの部分が透明になります。" },
            { "保留场景原本的样子作为背景，场景里的物体也会拍进照片。", "シーンをそのまま背景にします。シーン内のオブジェクトも写真に写ります。" },

            // 输出 (the switch label is narrow: スーパーサンプリング without 「やや遅い」)
            { "比例", "アスペクト比" },
            { "比例：{0}", "比率：{0}" },
            { "尺寸（短边）", "サイズ（短辺）" },
            { "照片 {0} × {1} 像素", "写真 {0} × {1} ピクセル" },
            { "定时", "タイマー" },
            { "关", "オフ" },
            { "{0} 秒", "{0}秒" },
            { "定时：关", "タイマー：オフ" },
            { "定时：{0} 秒", "タイマー：{0}秒" },
            { "超采样（更细腻，稍慢）", "スーパーサンプリング（高精細）" },
            { "照片", "写真" },
            { "打开照片文件夹", "写真フォルダーを開く" },
            { "无法创建照片文件夹：{0}", "写真フォルダーを作成できませんでした：{0}" },
            { "照片文件夹：{0}", "写真フォルダー：{0}" },
            { "界面", "UI" },
            { "界面大小", "UI サイズ" },
            { "透明 PNG", "透明 PNG" },

            // taking and finding photos
            { "已取消倒计时", "カウントダウンをキャンセルしました" },
            { "已保存照片", "写真を保存しました" },
            { "照片没有保存成功", "写真を保存できませんでした" },
            { "拍照时出错：{0}", "撮影できませんでした：{0}" },
            { "相机不可用，无法拍照", "カメラが使用できないため、撮影できません" },
            { "显卡不支持这么大的照片，请把尺寸调小", "グラフィックカードがこの大きさの写真に対応していません。サイズを小さくしてください" },
            { "显存不足，无法拍这么大的照片，请把尺寸调小", "VRAM が足りないため、この大きさの写真は撮影できません。サイズを小さくしてください" },
            { "照片编码失败", "写真のエンコードに失敗しました" },
            { "照片保存失败：", "写真を保存できませんでした：" },
            { "点击在文件夹中显示", "クリックでフォルダー内に表示" },
            { "这张照片已经不在原来的位置了", "この写真は元の場所にありません" },
            { "照片在：{0}", "写真の場所：{0}" },
        };
    }
}
