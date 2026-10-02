package main

import (
	"bufio"
	"context"
	"flag"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesktopParentDisconnectStopsServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIProcess$", "--", "-no-browser", "-exit-on-stdin-close", "-data-dir", dir)
	command.Env = append(os.Environ(), "AIVPN_CLI_TEST_HELPER=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	address := ""
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "Open http://127.0.0.1:") {
			address = strings.TrimPrefix(scanner.Text(), "Open ")
			break
		}
	}
	if address == "" {
		t.Fatal("desktop server did not announce its local URL")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("local UI returned %d", response.StatusCode)
	}
	if err = input.Close(); err != nil {
		t.Fatal(err)
	}
	if err = command.Wait(); err != nil {
		t.Fatalf("parent disconnect did not shut down cleanly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "store.lock")); !os.IsNotExist(err) {
		t.Fatalf("shutdown left the data lock: %v", err)
	}
	if response, err := client.Get(address); err == nil {
		response.Body.Close()
		t.Fatal("server still reachable after parent disconnected")
	}
}

func TestCLIProcess(t *testing.T) {
	if os.Getenv("AIVPN_CLI_TEST_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append(os.Args[:1], os.Args[i+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet("aivpn-tools", flag.ExitOnError)
	main()
	os.Exit(0)
}
func TestCLIStartupFailure(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	for _, args := range [][]string{{"-port", port}, {"-proxy", "invalid://user:secret@localhost"}, {"-stun", "https://invalid.example"}} {
		t.Run(args[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			dir := t.TempDir()
			command := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIProcess$", "--", "-no-browser", "-data-dir", dir}, args...)...)
			command.Env = append(os.Environ(), "AIVPN_CLI_TEST_HELPER=1")
			out, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("startup failure did not terminate")
			}
			if err == nil {
				t.Errorf("startup failure exited successfully: %s", out)
			}
			if strings.Contains(string(out), "secret") {
				t.Fatal("startup error exposed proxy credentials")
			}
			if _, err := os.Stat(filepath.Join(dir, "store.lock")); !os.IsNotExist(err) {
				t.Fatalf("startup failure left a lock: %v", err)
			}
		})
	}
}
