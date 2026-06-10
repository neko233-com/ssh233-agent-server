package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	systemdServiceName = "ssh233-server.service"
	launchdLabel       = "com.neko233.ssh233-server"
	windowsTaskName    = "ssh233-server"
)

type autostartStatus struct {
	Backend string
	Enabled bool
	Active  bool
	Detail  string
}

func runEnableAutostart(args []string) error {
	fs := flag.NewFlagSet("ssh233-server enable-autostart", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgPath := resolveConfigPath(*configPath)
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if st, _, ok, err := loadRuntimeState(cfgPath); err == nil && ok {
		if err := stopServer(st); err != nil {
			return err
		}
	}
	if err := enableNativeAutostart(exePath, cfgPath); err != nil {
		return err
	}
	status, err := detectAutostartStatus(cfgPath)
	if err != nil {
		return err
	}
	fmt.Printf("autostart enabled via %s\n", status.Backend)
	if status.Detail != "" {
		fmt.Printf("detail=%s\n", status.Detail)
	}
	fmt.Println("note: autostart is opt-in; use disable-autostart to turn off")
	return nil
}

func runDisableAutostart(args []string) error {
	fs := flag.NewFlagSet("ssh233-server disable-autostart", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgPath := resolveConfigPath(*configPath)
	if err := disableNativeAutostart(cfgPath); err != nil {
		return err
	}
	fmt.Println("autostart disabled")
	return nil
}

func runAutostartStatus(args []string) error {
	fs := flag.NewFlagSet("ssh233-server autostart-status", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgPath := resolveConfigPath(*configPath)
	status, err := detectAutostartStatus(cfgPath)
	if err != nil {
		return err
	}
	fmt.Printf("autostart_backend=%s\nautostart_enabled=%t\nautostart_active=%t\n", status.Backend, status.Enabled, status.Active)
	if status.Detail != "" {
		fmt.Printf("autostart_detail=%s\n", status.Detail)
	}
	return nil
}

func detectAutostartStatus(configPath string) (autostartStatus, error) {
	switch runtime.GOOS {
	case "linux":
		unitPath := systemdUnitPath()
		status := autostartStatus{Backend: "systemd"}
		if _, err := os.Stat(unitPath); os.IsNotExist(err) {
			return status, nil
		} else if err != nil {
			return autostartStatus{}, err
		}
		status.Enabled = true
		status.Detail = unitPath
		status.Active = runCommand(exec.Command("systemctl", "is-active", "--quiet", systemdServiceName)) == nil
		return status, nil
	case "darwin":
		plistPath := launchdPlistPath()
		status := autostartStatus{Backend: "launchd"}
		if _, err := os.Stat(plistPath); os.IsNotExist(err) {
			return status, nil
		} else if err != nil {
			return autostartStatus{}, err
		}
		status.Enabled = true
		status.Detail = plistPath
		status.Active = runCommand(exec.Command("launchctl", "print", "system/"+launchdLabel)) == nil
		return status, nil
	case "windows":
		status := autostartStatus{Backend: "schtasks", Detail: windowsTaskName}
		if err := runCommand(exec.Command("schtasks", "/Query", "/TN", windowsTaskName)); err != nil {
			return status, nil
		}
		status.Enabled = true
		_, _, running, err := loadRuntimeState(configPath)
		if err != nil {
			return autostartStatus{}, err
		}
		status.Active = running
		return status, nil
	default:
		return autostartStatus{Backend: runtime.GOOS}, nil
	}
}

func enableNativeAutostart(exePath, configPath string) error {
	switch runtime.GOOS {
	case "linux":
		if err := os.WriteFile(systemdUnitPath(), []byte(systemdUnitContent(exePath, configPath)), 0o644); err != nil {
			return fmt.Errorf("write systemd unit: %w", err)
		}
		if err := runCommand(exec.Command("systemctl", "daemon-reload")); err != nil {
			return err
		}
		return runCommand(exec.Command("systemctl", "enable", "--now", systemdServiceName))
	case "darwin":
		if err := os.WriteFile(launchdPlistPath(), []byte(launchdPlistContent(exePath, configPath)), 0o644); err != nil {
			return fmt.Errorf("write launchd plist: %w", err)
		}
		_ = runCommand(exec.Command("launchctl", "bootout", "system/"+launchdLabel))
		if err := runCommand(exec.Command("launchctl", "bootstrap", "system", launchdPlistPath())); err != nil {
			return err
		}
		_ = runCommand(exec.Command("launchctl", "enable", "system/"+launchdLabel))
		return runCommand(exec.Command("launchctl", "kickstart", "-k", "system/"+launchdLabel))
	case "windows":
		command := windowsTaskCommand(exePath, configPath)
		if err := runCommand(exec.Command("schtasks", "/Create", "/F", "/TN", windowsTaskName, "/SC", "ONSTART", "/RL", "HIGHEST", "/RU", "SYSTEM", "/TR", command)); err != nil {
			return err
		}
		return runCommand(exec.Command("schtasks", "/Run", "/TN", windowsTaskName))
	default:
		return fmt.Errorf("autostart unsupported on %s", runtime.GOOS)
	}
}

func disableNativeAutostart(configPath string) error {
	status, err := detectAutostartStatus(configPath)
	if err != nil {
		return err
	}
	if !status.Enabled {
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		_ = runCommand(exec.Command("systemctl", "disable", "--now", systemdServiceName))
		if err := os.Remove(systemdUnitPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return runCommand(exec.Command("systemctl", "daemon-reload"))
	case "darwin":
		_ = runCommand(exec.Command("launchctl", "bootout", "system/"+launchdLabel))
		if err := os.Remove(launchdPlistPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case "windows":
		_ = runCommand(exec.Command("schtasks", "/End", "/TN", windowsTaskName))
		_ = stopManagedAutostart(configPath)
		return runCommand(exec.Command("schtasks", "/Delete", "/F", "/TN", windowsTaskName))
	default:
		return fmt.Errorf("autostart unsupported on %s", runtime.GOOS)
	}
}

func startManagedAutostart(configPath string) error {
	status, err := detectAutostartStatus(configPath)
	if err != nil {
		return err
	}
	if !status.Enabled {
		return errors.New("autostart is not enabled; run enable-autostart first")
	}
	switch runtime.GOOS {
	case "linux":
		return runCommand(exec.Command("systemctl", "start", systemdServiceName))
	case "darwin":
		if err := runCommand(exec.Command("launchctl", "bootstrap", "system", launchdPlistPath())); err != nil && !strings.Contains(err.Error(), "already bootstrapped") {
			return err
		}
		return runCommand(exec.Command("launchctl", "kickstart", "-k", "system/"+launchdLabel))
	case "windows":
		return runCommand(exec.Command("schtasks", "/Run", "/TN", windowsTaskName))
	default:
		return fmt.Errorf("autostart unsupported on %s", runtime.GOOS)
	}
}

func stopManagedAutostart(configPath string) error {
	status, err := detectAutostartStatus(configPath)
	if err != nil {
		return err
	}
	if !status.Enabled {
		return errors.New("autostart is not enabled")
	}
	switch runtime.GOOS {
	case "linux":
		return runCommand(exec.Command("systemctl", "stop", systemdServiceName))
	case "darwin":
		return runCommand(exec.Command("launchctl", "bootout", "system/"+launchdLabel))
	case "windows":
		if err := runCommand(exec.Command("schtasks", "/End", "/TN", windowsTaskName)); err == nil {
			return nil
		}
		st, path, ok, err := loadRuntimeState(configPath)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := stopServer(st); err != nil {
			return err
		}
		_ = os.Remove(path)
		_ = os.Remove(runtimePIDPath(configPath))
		return nil
	default:
		return fmt.Errorf("autostart unsupported on %s", runtime.GOOS)
	}
}

func restartManagedAutostart(configPath string) error {
	status, err := detectAutostartStatus(configPath)
	if err != nil {
		return err
	}
	if !status.Enabled {
		return errors.New("autostart is not enabled")
	}
	switch runtime.GOOS {
	case "linux":
		return runCommand(exec.Command("systemctl", "restart", systemdServiceName))
	case "darwin":
		_ = runCommand(exec.Command("launchctl", "bootout", "system/"+launchdLabel))
		if err := runCommand(exec.Command("launchctl", "bootstrap", "system", launchdPlistPath())); err != nil && !strings.Contains(err.Error(), "already bootstrapped") {
			return err
		}
		return runCommand(exec.Command("launchctl", "kickstart", "-k", "system/"+launchdLabel))
	case "windows":
		if err := stopManagedAutostart(configPath); err != nil {
			return err
		}
		return startManagedAutostart(configPath)
	default:
		return fmt.Errorf("autostart unsupported on %s", runtime.GOOS)
	}
}

func systemdUnitPath() string {
	return filepath.Join(string(os.PathSeparator), "etc", "systemd", "system", systemdServiceName)
}

func launchdPlistPath() string {
	return filepath.Join(string(os.PathSeparator), "Library", "LaunchDaemons", launchdLabel+".plist")
}

func systemdUnitContent(exePath, configPath string) string {
	return fmt.Sprintf("[Unit]\nDescription=SSH233 Agent Server\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=%s serve -config %s\nRestart=on-failure\nRestartSec=5\nWorkingDirectory=%s\n\n[Install]\nWantedBy=multi-user.target\n",
		systemdEscapeArg(exePath), systemdEscapeArg(configPath), systemdEscapeArg(filepath.Dir(exePath)))
}

func launchdPlistContent(exePath, configPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>-config</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>WorkingDirectory</key>
  <string>%s</string>
</dict>
</plist>
`, launchdLabel, xmlEscape(exePath), xmlEscape(configPath), xmlEscape(filepath.Dir(exePath)))
}

func windowsTaskCommand(exePath, configPath string) string {
	return fmt.Sprintf(`"%s" serve -config "%s"`, exePath, configPath)
}

func systemdEscapeArg(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, " ", "\\x20")
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

func runCommand(cmd *exec.Cmd) error {
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}
