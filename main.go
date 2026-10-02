package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"aivpn.tools/internal/app"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	data := flag.String("data-dir", "", "Local data directory (defaults to the user configuration directory)")
	port := flag.Int("port", 0, "Loopback HTTP port (0 chooses an available port)")
	noBrowser := flag.Bool("no-browser", false, "Print the URL without opening a browser")
	exitOnStdinClose := flag.Bool("exit-on-stdin-close", false, "Stop when the desktop launcher's input pipe closes")
	proxy := flag.String("proxy", "", "Explicit HTTP/HTTPS/SOCKS5 proxy for diagnostic requests")
	stun := flag.String("stun", "stun:stun.l.google.com:19302", "STUN server used only when the user enables WebRTC checks")
	flag.Parse()
	if *port < 0 || *port > 65535 {
		return errors.New("Port must be between 0 and 65535.")
	}
	if *data == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return errors.New("Cannot find the user configuration directory; use -data-dir.")
		}
		*data = filepath.Join(dir, "aivpn-tools")
	}
	store, err := app.OpenStore(*data)
	if err != nil {
		return err
	}
	defer store.Close()
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return errors.New("Cannot open the local port.")
	}
	defer listener.Close()
	handler, err := app.NewServer(store, app.Options{Host: listener.Addr().String(), Proxy: *proxy, STUN: *stun, IPKey: os.Getenv("AIVPN_IPAPI_KEY"), ModelToken: os.Getenv("AIVPN_MODEL_TOKEN")})
	if err != nil {
		return err
	}
	requests, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	srv := &http.Server{Handler: handler.Handler(), BaseContext: func(net.Listener) context.Context { return requests }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 200 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	url := "http://" + listener.Addr().String()
	fmt.Printf("AIvia\nOpen %s\nLocal data: %s\nPress Ctrl+C to stop.\n", url, *data)
	if !*noBrowser {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			log.Print("Open the printed URL in your browser.")
		} else {
			go func() { _ = cmd.Wait() }()
		}
	}
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(done)
	if *exitOnStdinClose {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			select {
			case done <- syscall.SIGTERM:
			default:
			}
		}()
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-done
		cancelRequests()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return errors.New("Local HTTP server stopped unexpectedly.")
	}
	<-stopped
	return nil
}
