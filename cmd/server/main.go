package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/neko233/ssh233-agent-server/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runServeCLI(args)
	}
	switch args[0] {
	case "run", "serve":
		return runServeCLI(args[1:])
	case "start":
		return runStart(args[1:])
	case "stop":
		return runStop(args[1:])
	case "restart":
		return runRestart(args[1:])
	case "status":
		return runStatus(args[1:])
	case "enable-autostart":
		return runEnableAutostart(args[1:])
	case "disable-autostart":
		return runDisableAutostart(args[1:])
	case "autostart-status":
		return runAutostartStatus(args[1:])
	case "version":
		fmt.Println("ssh233-server", version.Version, version.Commit, version.Date)
		return nil
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (try: ssh233-server help)", args[0])
	}
}

func runServeCLI(args []string) error {
	fs := flag.NewFlagSet("ssh233-server serve", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.yaml", "config file path")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("ssh233-server", version.Version, version.Commit, version.Date)
		return nil
	}
	return runServe(*configPath)
}

func printUsage() {
	fmt.Print(`SSH233 Agent Server

Usage:
  ssh233-server [serve] -config config.yaml   Run in foreground
  ssh233-server start   -config config.yaml   Start in background
  ssh233-server stop    -config config.yaml   Stop background server
  ssh233-server restart -config config.yaml   Restart background server
  ssh233-server status  -config config.yaml   Show running status

Autostart (opt-in, disabled by default on install):
  ssh233-server enable-autostart  -config config.yaml
  ssh233-server disable-autostart -config config.yaml
  ssh233-server autostart-status  -config config.yaml

Other:
  ssh233-server version
  ssh233-server help

Web UI: http://127.0.0.1:6030/login.html
`)
}
