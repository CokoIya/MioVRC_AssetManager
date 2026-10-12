package naming

import "testing"

func TestIsPartName(t *testing.T) {
	for n, want := range map[string]bool{
		"Eye_Blue": true, "EyeBlue": true, "eyes": true, "Color02": true, "Color_02.unitypackage": true, "Type A": true,
		"黒": true, "目テクスチャ": true, "瞳_ピンク": true, "预制体": true, "粉色": true, "黑白": true, "Black&White": true,
		"マテリアル": true, "Eye texture": true, "眼睛贴图": false, // (贴图 names a category too: what it holds decides)
		"Black Cat Hoodie": false, "白丝": false, "Sailor Dress": false, "01": false, "A": false, "Riptide": false,
		"Kikyo": false, "Moonlight Fleur": false, "Melvii": false, "Pink Heart Bag": false, "Eye Patch": false,
		"金发": false, "Textures_v2": true, "Prefabs": true, "unitypackage": true, "README": false,
	} {
		if got := IsPartName(n); got != want {
			t.Errorf("IsPartName(%q) = %v", n, got)
		}
	}
}
