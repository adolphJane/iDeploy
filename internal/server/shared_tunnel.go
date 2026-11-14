package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// SharedHTTPClient sends each request through a verified SSH connection to the
// shared service bound to the server's loopback interface. No local listener or
// public shared-service port is created.
func SharedHTTPClient(connection Connection) *http.Client {
	transport := &http.Transport{DisableKeepAlives: true}
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialSharedTunnel(ctx, connection)
	}
	return &http.Client{Transport: transport, Timeout: 40 * time.Second}
}

type tunnelConn struct {
	input   io.WriteCloser
	output  io.ReadCloser
	command *exec.Cmd
	cleanup string
	once    sync.Once
}

func (c *tunnelConn) Read(p []byte) (int, error)  { return c.output.Read(p) }
func (c *tunnelConn) Write(p []byte) (int, error) { return c.input.Write(p) }
func (c *tunnelConn) Close() error {
	c.once.Do(func() {
		_ = c.input.Close()
		_ = c.output.Close()
		if c.command.Process != nil {
			_ = c.command.Process.Kill()
			_ = c.command.Wait()
		}
		if c.cleanup != "" {
			_ = os.RemoveAll(c.cleanup)
		}
	})
	return nil
}
func (c *tunnelConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *tunnelConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *tunnelConn) SetDeadline(time.Time) error      { return nil }
func (c *tunnelConn) SetReadDeadline(time.Time) error  { return nil }
func (c *tunnelConn) SetWriteDeadline(time.Time) error { return nil }

func dialSharedTunnel(ctx context.Context, connection Connection) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	method := connection.AuthMethod
	if method == "" {
		method = AuthMethodAgent
		if connection.CredentialRef != "" {
			method = AuthMethodPassword
		}
	}
	args := []string{"-o", "ConnectTimeout=8", "-o", "ConnectionAttempts=1", "-o", "StrictHostKeyChecking=yes", "-o", "ExitOnForwardFailure=yes"}
	switch method {
	case AuthMethodAgent:
		args = append(args, "-o", "BatchMode=yes")
	case AuthMethodPassword:
		args = append(args, "-o", "BatchMode=no", "-o", "PreferredAuthentications=password,keyboard-interactive", "-o", "PubkeyAuthentication=no", "-o", "NumberOfPasswordPrompts=1")
	case AuthMethodPrivateKey:
		if connection.PrivateKeyPath == "" {
			return nil, fmt.Errorf("SSH 私钥尚未在此设备配置")
		}
		args = append(args, "-i", connection.PrivateKeyPath, "-o", "IdentitiesOnly=yes", "-o", "NumberOfPasswordPrompts=1")
	default:
		return nil, fmt.Errorf("SSH 认证方式无效")
	}
	args = append(args, "-p", strconv.Itoa(connection.Port), "-W", "127.0.0.1:8765", connection.SSHUser+"@"+connection.Host)
	command := exec.Command("ssh", args...)
	command.Env = os.Environ()
	cleanup := ""
	if connection.CredentialRef != "" {
		secret, err := LoadPassword(ctx, connection.CredentialRef)
		if err != nil {
			return nil, err
		}
		dir, err := os.MkdirTemp("", "ideploy-shared-askpass-")
		if err != nil {
			return nil, err
		}
		cleanup = dir
		path := filepath.Join(dir, "askpass.sh")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' "+shellQuote(secret)+"\n"), 0700); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		command.Env = append(command.Env, "SSH_ASKPASS="+path, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=ideploy")
	}
	input, err := command.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(cleanup)
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		_ = os.RemoveAll(cleanup)
		return nil, err
	}
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		_ = os.RemoveAll(cleanup)
		return nil, err
	}
	return &tunnelConn{input: input, output: output, command: command, cleanup: cleanup}, nil
}
