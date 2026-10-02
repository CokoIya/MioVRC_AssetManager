//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// regStr reads one value of a registry key ("" = the default value) with reg.exe.
func regStr(key, name string) string {
	args := []string{"query", key, "/v", name}
	if name == "" {
		args = []string{"query", key, "/ve"}
	}
	out, err := hidden(exec.Command("reg", args...)).Output()
	if err != nil {
		return ""
	}
	return expandWinEnv(regValue(string(out), name))
}

// appFromUninstall: the exe of a program by the entry it left in "Apps & features".
func appFromUninstall(names []string, exe string) string {
	for _, root := range []string{`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\`, `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\`,
		`HKLM\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\`} {
		for _, n := range names {
			if dir := regStr(root+n, "InstallLocation"); dir != "" && statOK(filepath.Join(dir, exe)) {
				return filepath.Join(dir, exe)
			}
			if icon := regStr(root+n, "DisplayIcon"); icon != "" {
				if p := exeFromCommand(icon); p != "" && strings.EqualFold(filepath.Base(p), exe) && statOK(p) {
					return p
				}
				if i := strings.Index(strings.ToLower(icon), ".exe"); i > 0 && statOK(icon[:i+4]) && strings.EqualFold(filepath.Base(icon[:i+4]), exe) {
					return icon[:i+4]
				}
			}
		}
	}
	return ""
}

// protocolExe: the program registered for a URL scheme ("vcc", "unityhub").
func protocolExe(scheme string) string {
	for _, root := range []string{`HKCU\Software\Classes\`, `HKCR\`, `HKLM\Software\Classes\`} {
		if c := regStr(root+scheme+`\shell\open\command`, ""); c != "" {
			if p := exeFromCommand(c); p != "" && statOK(p) {
				return p
			}
		}
	}
	return ""
}

func findAlcom() string {
	if p := appFromUninstall([]string{"ALCOM", "{com.anatawa12.vrc-get-gui}", "vrc-get-gui"}, "ALCOM.exe"); p != "" {
		return p
	}
	if p := protocolExe("vcc"); p != "" && strings.EqualFold(filepath.Base(p), "ALCOM.exe") {
		return p
	}
	la := os.Getenv("LOCALAPPDATA")
	for _, c := range []string{filepath.Join(la, "ALCOM", "ALCOM.exe"), filepath.Join(la, "Programs", "ALCOM", "ALCOM.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "ALCOM", "ALCOM.exe")} {
		if statOK(c) {
			return c
		}
	}
	return ""
}

func findVCC() string {
	if p := appFromUninstall([]string{"VRChat Creator Companion", "{F0E9D5F3-3B9B-4F7D-8E9E-2D4B5C6D7E8F}"}, "CreatorCompanion.exe"); p != "" {
		return p
	}
	if p := protocolExe("vcc"); p != "" && strings.EqualFold(filepath.Base(p), "CreatorCompanion.exe") {
		return p
	}
	for _, c := range []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "VRChat Creator Companion", "CreatorCompanion.exe")} {
		if statOK(c) {
			return c
		}
	}
	return ""
}

func findUnityHub() string {
	if p := vccSetting("pathToUnityHub"); p != "" && statOK(p) {
		return p
	}
	if p := protocolExe("unityhub"); p != "" {
		return p
	}
	for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
		if pf := os.Getenv(env); pf != "" && statOK(filepath.Join(pf, "Unity Hub", "Unity Hub.exe")) {
			return filepath.Join(pf, "Unity Hub", "Unity Hub.exe")
		}
	}
	return ""
}

// alcomConfigured: ALCOM has run on this computer (its settings are there) even when its exe is not found.
func alcomConfigured() bool {
	la := os.Getenv("LOCALAPPDATA")
	return la != "" && (statOK(filepath.Join(la, "com.anatawa12.vrc-get-gui")) || statOK(filepath.Join(la, "VRChatCreatorCompanion", "vrc-get", "gui-config.json")))
}

func launchApp(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
