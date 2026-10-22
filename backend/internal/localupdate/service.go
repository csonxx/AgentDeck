package localupdate

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func xmlText(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
func serviceID(dir string) string {
	h := sha256.Sum256([]byte(dir))
	return fmt.Sprintf("agentdeck-%x", h[:6])
}
func systemdArg(s string) string { return strconv.Quote(strings.ReplaceAll(s, "%", "%%")) }

// RegisterService installs a current-user login service, never a privileged
// system daemon. Pairing keys and native agents continue to belong to that user.
func RegisterService(dir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(dir, "logs"), 0700); err != nil {
		return err
	}
	id := serviceID(dir)
	switch runtime.GOOS {
	case "darwin":
		label := "dev.agentdeck." + id
		path := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		data := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + xmlText(label) + `</string><key>ProgramArguments</key><array><string>` + xmlText(Launcher(dir)) + `</string><string>local-run</string><string>--data</string><string>` + xmlText(dir) + `</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>10</integer><key>StandardOutPath</key><string>` + xmlText(filepath.Join(dir, "logs", "updater.stdout.log")) + `</string><key>StandardErrorPath</key><string>` + xmlText(filepath.Join(dir, "logs", "updater.stderr.log")) + `</string></dict></plist>`
		if err = os.WriteFile(path, []byte(data), 0600); err != nil {
			return err
		}
		return exec.Command("launchctl", "bootstrap", "gui/"+strconv.Itoa(os.Getuid()), path).Run()
	case "linux":
		path := filepath.Join(home, ".config", "systemd", "user", id+".service")
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		unit := "[Unit]\nDescription=AgentDeck workstation\nAfter=network-online.target\n[Service]\nExecStart=" + systemdArg(Launcher(dir)) + " local-run --data " + systemdArg(dir) + "\nRestart=on-failure\nRestartSec=10\nTimeoutStopSec=45\n[Install]\nWantedBy=default.target\n"
		if err = os.WriteFile(path, []byte(unit), 0600); err != nil {
			return err
		}
		if err = exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			return err
		}
		return exec.Command("systemctl", "--user", "enable", "--now", id+".service").Run()
	case "windows":
		u, err := user.Current()
		if err != nil {
			return err
		}
		// Current-user Scheduled Task runs in the interactive logon, matching the
		// agent/Desktop account without storing any Windows password.
		args := "local-run --data " + windowsArg(dir)
		task := `<?xml version="1.0" encoding="UTF-8"?><Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><LogonTrigger><Enabled>true</Enabled><UserId>` + xmlText(u.Uid) + `</UserId></LogonTrigger></Triggers><Principals><Principal id="Owner"><UserId>` + xmlText(u.Uid) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure></Settings><Actions Context="Owner"><Exec><Command>` + xmlText(Launcher(dir)) + `</Command><Arguments>` + xmlText(args) + `</Arguments><WorkingDirectory>` + xmlText(dir) + `</WorkingDirectory></Exec></Actions></Task>`
		path := filepath.Join(root(dir), "login-task.xml")
		if err = os.WriteFile(path, []byte(task), 0600); err != nil {
			return err
		}
		if err = exec.Command("schtasks.exe", "/Create", "/TN", id, "/XML", path).Run(); err != nil {
			return err
		}
		return exec.Command("schtasks.exe", "/Run", "/TN", id).Run()
	}
	return fmt.Errorf("unsupported service OS %s", runtime.GOOS)
}

// CommandLineToArgvW escaping, also used by the generated task XML. Unlike a
// shell command, paths are passed only as an executable argument.
func windowsArg(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, c := range s {
		if c == '\\' {
			slashes++
			continue
		}
		if c == '"' {
			b.WriteString(strings.Repeat("\\", slashes*2+1))
		} else {
			b.WriteString(strings.Repeat("\\", slashes))
		}
		b.WriteRune(c)
		slashes = 0
	}
	b.WriteString(strings.Repeat("\\", slashes*2))
	b.WriteByte('"')
	return b.String()
}
