//go:build !windows

package ai

import (
	"net/url"
	"os"
	"strings"
)

// Builds outside Windows exist for development and tests only. The tests of the window put a stand-in for OpenAI
// on this computer (an address anywhere else is not taken); the Windows program never reads this variable.
func init() {
	if v := strings.TrimRight(os.Getenv("VRCLIB_OPENAI_BASE"), "/"); v != "" {
		if u, err := url.Parse(v); err == nil && u.Scheme == "http" && u.Hostname() == "127.0.0.1" && u.Path == "" && u.User == nil {
			gptAuthBase, gptAPIBase = v, v+"/v1"
		}
	}
}
