// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tunnel

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Config holds SSH tunnel parameters from YAML or CLI.
type Config struct {
	Host       string `yaml:"host"`
	User       string `yaml:"user"`
	SSHPort    int    `yaml:"ssh_port"`
	RemotePort int    `yaml:"remote_port"`
	LocalPort  int    `yaml:"local_port"`
}

// IsConfigured returns true if enough info exists to start a tunnel.
func (c *Config) IsConfigured() bool {
	return c != nil && c.Host != ""
}

// LocalAddr returns the TCP address string for the local end of the tunnel.
func (c *Config) LocalAddr() string {
	port := c.LocalPort
	if port == 0 {
		port = 11434
	}
	return fmt.Sprintf("tcp://localhost:%d", port)
}

// Tunnel represents a running SSH tunnel process.
type Tunnel struct {
	cmd *exec.Cmd
}

// Start launches an SSH tunnel based on the given config.
// If the local port is already open (e.g. user started the tunnel manually),
// it returns a nil Tunnel and no error.
func Start(cfg *Config) (*Tunnel, error) {
	if cfg.SSHPort == 0 {
		cfg.SSHPort = 22
	}
	if cfg.RemotePort == 0 {
		cfg.RemotePort = 11434
	}
	if cfg.LocalPort == 0 {
		cfg.LocalPort = 11434
	}

	// If port is already open, skip spawning.
	if isPortOpen(cfg.LocalPort) {
		return nil, nil
	}

	target := fmt.Sprintf("%s@%s", cfg.User, cfg.Host)
	forward := fmt.Sprintf("%d:localhost:%d", cfg.LocalPort, cfg.RemotePort)

	args := []string{
		"-N",                                   // no remote command
		"-L", forward,                          // local port forward
		"-p", fmt.Sprintf("%d", cfg.SSHPort),   // SSH port
		"-o", "StrictHostKeyChecking=accept-new", // auto-accept new host keys
		"-o", "ExitOnForwardFailure=yes",       // fail fast if port forward fails
		"-o", "ServerAliveInterval=30",         // keep connection alive
		target,
	}

	cmd := exec.Command("ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start SSH tunnel: %w", err)
	}

	// Wait for the local port to become reachable.
	if err := waitForPort(cfg.LocalPort, 15*time.Second); err != nil {
		// Clean up the process if port never opened.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("SSH tunnel started but port %d never became reachable: %w", cfg.LocalPort, err)
	}

	return &Tunnel{cmd: cmd}, nil
}

// Close shuts down the SSH tunnel process gracefully.
// It sends SIGINT first, then force-kills after 2 seconds.
func (t *Tunnel) Close() {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return
	}

	// Try graceful shutdown with SIGINT.
	_ = t.cmd.Process.Signal(syscall.SIGINT)

	done := make(chan error, 1)
	go func() { done <- t.cmd.Wait() }()

	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
		_ = t.cmd.Process.Kill()
		<-done
	}
}

func isPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("localhost:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s", timeout)
}
