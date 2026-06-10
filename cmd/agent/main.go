package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

// Example external agent that registers with SSH233 and sends heartbeats.
func main() {
	server := flag.String("server", "http://localhost:6030", "SSH233 server URL")
	name := flag.String("name", "agent-"+hostname(), "agent name")
	token := flag.String("register-token", "your-agent-register-token", "register token from config")
	agentToken := flag.String("agent-token", "", "existing agent token (skip register)")
	flag.Parse()

	var at string
	if *agentToken != "" {
		at = *agentToken
		fmt.Println("using existing token")
	} else {
		resp, err := post(*server+"/api/v1/agents/register", map[string]any{
			"name":           *name,
			"register_token": *token,
			"hostname":       hostname(),
			"ip":             "127.0.0.1",
			"version":        "1.0.0",
			"capabilities":   []string{"exec", "proxy"},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "register failed:", err)
			os.Exit(1)
		}
		at = resp["token"].(string)
		fmt.Println("registered, token:", at)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			fmt.Println("shutting down")
			return
		case <-ticker.C:
			_, err := post(*server+"/api/v1/agents/heartbeat", map[string]string{
				"token": at,
				"ip":    "127.0.0.1",
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, "heartbeat failed:", err)
			} else {
				fmt.Println("heartbeat ok")
			}
		}
	}
}

func post(url string, body any) (map[string]any, error) {
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return result, nil
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "unknown"
	}
	return h + "-" + runtime.GOOS
}
