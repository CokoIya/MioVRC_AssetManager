//go:build windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

func Hidden(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd
}

func OpenInExplorer(p string, isDir bool) error {
	cmd := exec.Command("explorer.exe")
	if isDir {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe "` + p + `"`}
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + p + `"`}
	}
	return cmd.Start()
}

// OpenURL hands the link to Windows, which opens it in the user's default browser.
func OpenURL(u string) error {
	if err := shellOpen(u); err == nil {
		return nil
	}
	return Hidden(exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", u)).Start()
}

// DefaultBrowserExe: the program Windows opens https links with (the user's default browser).
func DefaultBrowserExe() string {
	for _, proto := range []string{"https", "http"} {
		if exe := assocExe(proto); exe != "" && !strings.Contains(strings.ToLower(exe), `\openwith.exe`) {
			return exe
		}
	}
	return defaultBrowserFromRegistry()
}

func defaultBrowserFromRegistry() string {
	progID := ""
	for _, k := range []string{"UserChoiceLatest", "UserChoice"} {
		out, err := Hidden(exec.Command("reg", "query",
			`HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\`+k, "/v", "ProgId")).Output()
		if err == nil {
			if progID = RegValue(string(out), "ProgId"); progID != "" {
				break
			}
		}
	}
	if progID == "" {
		return ""
	}
	for _, root := range []string{`HKCU\Software\Classes\`, `HKCR\`, `HKLM\Software\Classes\`} {
		out, err := Hidden(exec.Command("reg", "query", root+progID+`\shell\open\command`, "/ve")).Output()
		if err != nil {
			continue
		}
		if cmdline := RegValue(string(out), ""); cmdline != "" {
			if exe := ExeFromCommand(ExpandWinEnv(cmdline)); exe != "" {
				return exe
			}
		}
	}
	return ""
}

// RegValue pulls the data of a value out of `reg query` output (name "" = the default value).
func RegValue(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		for _, typ := range []string{"REG_EXPAND_SZ", "REG_SZ"} {
			i := strings.Index(line, typ)
			if i < 0 {
				continue
			}
			label := strings.TrimSpace(line[:i])
			if name != "" && !strings.EqualFold(label, name) {
				continue
			}
			return strings.TrimSpace(line[i+len(typ):])
		}
	}
	return ""
}

func ExeFromCommand(c string) string {
	c = strings.TrimSpace(c)
	if strings.HasPrefix(c, `"`) {
		if j := strings.Index(c[1:], `"`); j > 0 {
			return c[1 : j+1]
		}
		return ""
	}
	if i := strings.Index(strings.ToLower(c), ".exe"); i > 0 {
		return c[:i+4]
	}
	return ""
}

func ExpandWinEnv(s string) string {
	for {
		i := strings.Index(s, "%")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+1:], "%")
		if j < 0 {
			return s
		}
		name := s[i+1 : i+1+j]
		s = s[:i] + os.Getenv(name) + s[i+2+j:]
	}
}

func FindEdge() string {
	cands := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("LocalAppData"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("LocalAppData"), `Google\Chrome\Application\chrome.exe`),
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func AppWindow(browser, url, profile string) *exec.Cmd {
	return exec.Command(browser, "--app="+url, "--user-data-dir="+profile, "--window-size=1500,940",
		"--no-first-run", "--no-default-browser-check", "--disable-features=Translate", "--lang=zh-CN")
}

func systemProxy() string {
	out, err := Hidden(exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`)).Output()
	if err != nil {
		return ""
	}
	enabled := false
	server := ""
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "ProxyEnable" {
			enabled = strings.HasSuffix(f[2], "1")
		}
		if len(f) >= 3 && f[0] == "ProxyServer" {
			server = f[2]
		}
	}
	if !enabled || server == "" {
		return ""
	}
	if strings.Contains(server, "=") { // http=host:port;https=host:port
		for _, part := range strings.Split(server, ";") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 && (kv[0] == "https" || kv[0] == "http") {
				server = kv[1]
				if kv[0] == "https" {
					break
				}
			}
		}
	}
	return server
}
