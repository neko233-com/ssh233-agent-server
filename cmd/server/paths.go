package main

import (
	"path/filepath"
)

func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return "config.yaml"
}

func configDir(configPath string) string {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return filepath.Dir(configPath)
	}
	return filepath.Dir(abs)
}

func runtimeDir(configPath string) string {
	return filepath.Join(configDir(configPath), "runtime")
}

func runtimeStatePath(configPath string) string {
	return filepath.Join(runtimeDir(configPath), "state.json")
}

func runtimePIDPath(configPath string) string {
	return filepath.Join(runtimeDir(configPath), "ssh233.pid")
}

func runtimeDaemonLogPath(configPath string) string {
	return filepath.Join(runtimeDir(configPath), "daemon.log")
}
