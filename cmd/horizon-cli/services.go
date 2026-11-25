package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type serviceState struct {
	Service   string `json:"service"`
	Status    string `json:"status"`
	PID       int    `json:"pid,omitempty"`
	Log       string `json:"log"`
	StartedAt string `json:"startedAt,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Origin    string `json:"origin,omitempty"`
}

type processIdentity struct {
	PID       int    `json:"pid"`
	Stamp     string `json:"stamp"`
	StartedAt string `json:"startedAt,omitempty"`
	Exited    bool   `json:"-"`
	Origin    string `json:"origin,omitempty"`
}

func selectServices(args []string) ([]string, error) {
	selected := []string{"backend", "frontend"}
	if len(args) > 1 || len(args) == 1 && !slices.Contains([]string{"all", "backend", "frontend"}, args[0]) {
		return nil, fmt.Errorf("expected all, frontend or backend")
	}
	if len(args) == 1 && args[0] != "all" {
		selected = args
	}
	return selected, nil
}

func developmentLock(root string) (func(), error) {
	dir := filepath.Join(root, ".local", "horizon-cli")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return lockFile(filepath.Join(dir, "lock"))
}

func services(action string, args []string) error {
	selected, err := selectServices(args)
	if err != nil {
		return err
	}
	root, err := repositoryRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, ".local", "horizon-cli")
	unlock, err := developmentLock(root)
	if err != nil {
		return err
	}
	defer unlock()

	var commands map[string]*exec.Cmd
	if action == "start" || action == "restart" {
		pending := []string{}
		for _, name := range selected {
			p, err := readProcess(dir, name)
			if err != nil {
				return err
			}
			if action == "restart" || p.PID == 0 {
				pending = append(pending, name)
			}
		}
		if len(pending) > 0 {
			commands, err = prepareServices(root, pending)
			if err != nil {
				return err
			}
		}
	}

	var problems []error
	if action == "stop" || action == "restart" {
		for i := len(selected) - 1; i >= 0; i-- {
			name := selected[i]
			if err = stopService(dir, name); err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", name, err))
			}
		}
	}
	if (action == "start" || action == "restart") && len(problems) == 0 {
		for _, name := range selected {
			if commands[name] == nil {
				continue
			}
			if err = startService(dir, name, commands[name]); err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", name, err))
			}
		}
	}
	states := make([]serviceState, 0, len(selected))
	for _, name := range selected {
		state := serviceState{Service: name, Status: "stopped", Log: filepath.Join(dir, name+".log")}
		p, err := readProcess(dir, name)
		switch {
		case err != nil:
			state.Status = "unknown"
			problems = append(problems, fmt.Errorf("%s: %w", name, err))
		case p.PID != 0:
			state.Status, state.PID, state.StartedAt = "running", p.PID, p.StartedAt
			state.Origin = listeningServiceOrigin(p.Origin)
		case p.Exited:
			state.Reason, state.StartedAt = "process_exited", p.StartedAt
		}
		states = append(states, state)
	}
	return errors.Join(printJSON(states), errors.Join(problems...))
}

func repositoryRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	binary, _ := os.Executable()
	for _, start := range []string{cwd, filepath.Dir(binary)} {
		for dir := start; ; dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, "cmd", "api", "main.go")); err == nil {
				if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
					return dir, nil
				}
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	return "", fmt.Errorf("cannot locate the Horizon checkout")
}

func readProcess(dir, name string) (processIdentity, error) {
	var p processIdentity
	path := filepath.Join(dir, name+".json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		return processIdentity{}, err
	}
	stamp, err := processStamp(p.PID)
	if err != nil {
		return processIdentity{}, err
	}
	if p.StartedAt == "" && p.PID > 1 && p.Stamp != "" {
		// Older state files were written once, immediately after starting the process.
		if info, err := os.Stat(path); err == nil {
			p.StartedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
	}
	if stamp == "" || stamp != p.Stamp {
		return processIdentity{StartedAt: p.StartedAt, Exited: p.PID > 1 && p.Stamp != ""}, nil
	}
	return p, nil
}

// Capture the effective launch environment before it can change independently
// of the managed process.
func serviceOrigin(name string, cmd *exec.Cmd) string {
	key, address := "API_ADDR", "127.0.0.1:8080"
	if name == "frontend" {
		key, address = "WEB_PORT", "5173"
	}
	for _, assignment := range cmd.Environ() {
		if k, value, ok := strings.Cut(assignment, "="); ok && k == key && value != "" {
			address = value
		}
	}
	if name == "frontend" {
		address = net.JoinHostPort("127.0.0.1", address)
	}
	return localServiceOrigin(address)
}

func localServiceOrigin(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return ""
	}
	ip := net.ParseIP(host)
	switch {
	case ip == nil:
		return ""
	case ip.IsUnspecified() && ip.To4() != nil:
		host = "127.0.0.1"
	case ip.IsUnspecified():
		host = "::1"
	case !ip.IsLoopback():
		return ""
	}
	return "http://" + net.JoinHostPort(host, port)
}

func listeningServiceOrigin(origin string) string {
	address, ok := strings.CutPrefix(origin, "http://")
	if !ok || localServiceOrigin(address) != origin {
		return ""
	}
	connection, err := net.DialTimeout("tcp", address, 150*time.Millisecond)
	if err != nil {
		return ""
	}
	_ = connection.Close()
	return origin
}

func startService(dir, name string, cmd *exec.Cmd) error {
	p := processIdentity{Origin: serviceOrigin(name, cmd)}
	if listeningServiceOrigin(p.Origin) != "" {
		return fmt.Errorf("the development address %s is already in use", p.Origin)
	}
	logPath := filepath.Join(dir, name+".log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd, err = backgroundCommand(cmd)
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	p.PID = cmd.Process.Pid
	p.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	statePath := filepath.Join(dir, name+".json")
	p.Stamp, err = processStamp(p.PID)
	if err != nil || p.Stamp == "" {
		_ = cmd.Process.Kill()
		return fmt.Errorf("process exited or could not be identified, see %s", logPath)
	}
	raw, err := json.Marshal(p)
	if err == nil {
		err = os.WriteFile(statePath+".tmp", raw, 0600)
	}
	if err == nil {
		err = os.Rename(statePath+".tmp", statePath)
	}
	if err != nil {
		_ = terminateProcess(p)
		return err
	}
	select {
	case err := <-exited:
		_ = os.Remove(statePath)
		return fmt.Errorf("process exited during startup (%v), see %s", err, logPath)
	case <-time.After(time.Second):
		return nil
	}
}

func stopService(dir, name string) error {
	p, err := readProcess(dir, name)
	if err != nil {
		return err
	}
	if p.PID != 0 {
		if err = terminateProcess(p); err != nil {
			return err
		}
	}
	err = os.Remove(filepath.Join(dir, name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
