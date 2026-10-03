package naming

import (
	"strings"
	"testing"

	"vrclib/internal/core"
)

func TestForBases(t *testing.T) {
	defs := ParseBases(core.DefaultSettings().Bases)
	cases := map[string]string{
		"HYPERTECH_EXO_FRAME_For_Milfy_v1.01":    "Milfy",
		"HYPERTECH_EXO_FRAME_For_LUMINA_v1.01":   "Lumina",
		"HYPERTECH_EXO_FRAME_For_Milltina_v1.01": "Milltina",
		"FaceTracking_v1_0_0_for_Plum":           "Plum",
		"Shader for VRChat":                      "",
		"Outfit for liltoon 1.8":                 "",
		"Mayo対応 ドレス":                             "Mayo",
		"DPSFullSet2022用傻瓜一键包":                   "",
		"Quest対応":                                "",
	}
	for in, want := range cases {
		got := strings.Join(ForBases(in, defs), ",")
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
