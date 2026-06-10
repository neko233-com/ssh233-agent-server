package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/neko233/ssh233-agent-server/internal/config"
)

func runStart(args []string) error {
	fs := flag.NewFlagSet("ssh233-server start", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgPath := resolveConfigPath(*configPath)

	if status, err := detectAutostartStatus(cfgPath); err == nil && status.Enabled {
		if err := startManagedAutostart(cfgPath); err != nil {
			return err
		}
		fmt.Printf("started ssh233-server via %s url=%s config=%s\n", status.Backend, browserURLFromConfig(cfgPath), cfgPath)
		return nil
	}

	if st, _, ok, err := loadRuntimeState(cfgPath); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("ssh233-server already running pid=%d url=%s config=%s", st.PID, browserURL(st.HTTPAddr), cfgPath)
	}

	state, err := startDetachedServer(cfgPath)
	if err != nil {
		return err
	}
	fmt.Printf("started ssh233-server pid=%d url=%s config=%s\n", state.PID, browserURL(state.HTTPAddr), cfgPath)
	return nil
}

func runStop(args []string) error {
	fs := flag.NewFlagSet("ssh233-server stop", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgPath := resolveConfigPath(*configPath)

	if status, err := detectAutostartStatus(cfgPath); err == nil && status.Enabled {
		if err := stopManagedAutostart(cfgPath); err != nil {
			return err
		}
		fmt.Printf("stopped ssh233-server via %s\n", status.Backend)
		return nil
	}

	st, path, ok, err := loadRuntimeState(cfgPath)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("ssh233-server is not running")
		return nil
	}
	if err := stopServer(st); err != nil {
		return err
	}
	_ = os.Remove(path)
	_ = os.Remove(runtimePIDPath(cfgPath))
	fmt.Printf("stopped ssh233-server pid=%d\n", st.PID)
	return nil
}

func runRestart(args []string) error {
	fs := flag.NewFlagSet("ssh233-server restart", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgPath := resolveConfigPath(*configPath)

	if status, err := detectAutostartStatus(cfgPath); err == nil && status.Enabled {
		if err := restartManagedAutostart(cfgPath); err != nil {
			return err
		}
		fmt.Printf("restarted ssh233-server via %s\n", status.Backend)
		return nil
	}

	if st, _, ok, err := loadRuntimeState(cfgPath); err != nil {
		return err
	} else if ok {
		if err := stopServer(st); err != nil {
			return err
		}
	}
	state, err := startDetachedServer(cfgPath)
	if err != nil {
		return err
	}
	fmt.Printf("restarted ssh233-server pid=%d url=%s\n", state.PID, browserURL(state.HTTPAddr))
	return nil
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("ssh233-server status", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgPath := resolveConfigPath(*configPath)

	st, _, ok, err := loadRuntimeState(cfgPath)
	if err != nil {
		return err
	}
	if ok {
		fmt.Printf("status=running\npid=%d\nhttp_addr=%s\nurl=%s\nconfig=%s\nversion=%s\n",
			st.PID, st.HTTPAddr, browserURL(st.HTTPAddr), cfgPath, st.Version)
	} else {
		fmt.Printf("status=stopped\nconfig=%s\nurl=%s\n", cfgPath, browserURLFromConfig(cfgPath))
	}
	if auto, err := detectAutostartStatus(cfgPath); err == nil {
		fmt.Printf("autostart_backend=%s\nautostart_enabled=%t\nautostart_active=%t\n", auto.Backend, auto.Enabled, auto.Active)
		if auto.Detail != "" {
			fmt.Printf("autostart_detail=%s\n", auto.Detail)
		}
	}
	return nil
}

func startDetachedServer(configPath string) (*runtimeState, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(runtimeDir(configPath), 0o755); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(runtimeDaemonLogPath(configPath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(exe, "serve", "-config", configPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = detachedProcessAttr()
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		st, _, ok, err := loadRuntimeState(configPath)
		if err == nil && ok {
			return &st, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("server started but runtime state was not published in time; config=%s log=%s", configPath, runtimeDaemonLogPath(configPath))
}

func stopServer(st runtimeState) error {
	if st.ControlToken == "" {
		return errors.New("runtime control token missing")
	}
	if err := postControl(st, "/__admin/shutdown"); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !healthOK(st.HTTPAddr) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for shutdown")
}

func postControl(st runtimeState, path string) error {
	req, err := http.NewRequest(http.MethodPost, browserURL(st.HTTPAddr)+path, bytes.NewReader(nil))
	if err != nil {
		return err
	}
	req.Header.Set("X-SSH233-Control-Token", st.ControlToken)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("control request failed: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func browserURLFromConfig(configPath string) string {
	cfg, err := config.Load(configPath)
	if err != nil {
		return "http://127.0.0.1:6030"
	}
	return browserURL(cfg.Server.HTTPAddr)
}
