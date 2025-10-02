package storybook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Controller struct {
	srv     *http.Server
	cancel  context.CancelFunc
	started bool
	port    int
}

func (c *Controller) Stop() {
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if c.srv != nil {
		_ = c.srv.Shutdown(ctx)
	}
	if c.cancel != nil {
		c.cancel()
	}
}

// ---------- Build ----------

func BuildIfNeeded(ctx context.Context, buildCmd, buildDir, workDir string, force bool) error {
	if !force {
		if st, err := os.Stat(buildDir); err == nil && st.IsDir() {
			return nil
		}
	}

	bin, args := splitCmd(buildCmd)
	if bin == "" {
		return errors.New("storybook: build command empty")
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("storybook: build command failed: %w", err)
	}

	if st, err := os.Stat(buildDir); err != nil || !st.IsDir() {
		return fmt.Errorf("storybook: buildDir %q not found after build", buildDir)
	}

	return nil
}

func splitCmd(s string) (string, []string) {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

func ServeBuildIfNeeded(
	parent context.Context,
	port int,
	dir string,
	healthPath string,
	wait time.Duration,
	logFile string, // "" = silent
) (*Controller, bool, error) {
	if IsPortOpen(port, 200*time.Millisecond) {
		return &Controller{started: false, port: port}, false, nil
	}

	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, false, fmt.Errorf("storybook: build dir %q missing", dir)
	}

	mux := http.NewServeMux()
	mux.Handle("/", withIndexFallback(dir))

	srv := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	_, cancel := context.WithCancel(parent)
	go func() {
		if logFile != "" {
			f, _ := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			defer f.Close()
			_ = srv.ListenAndServe()
		} else {
			_ = srv.ListenAndServe()
		}
	}()

	if !WaitHTTP(port, healthPath, wait) {
		cancel()
		return nil, false, fmt.Errorf("storybook: static server not ready on port %d", port)
	}

	return &Controller{srv: srv, cancel: cancel, started: true, port: port}, true, nil
}

func withIndexFallback(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	indexPath := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1) Versuche, die angeforderte Datei zu finden
		p := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			// Normale Datei -> FileServer
			fs.ServeHTTP(w, r)
			return
		}

		// 2) Fallback: index.html direkt serven (ohne URL-Rewrite)
		if _, err := os.Stat(indexPath); err == nil {
			// Optional: Caching-Header für Stabilität (Fonts/Assets lädt Client separat)
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, indexPath)
			return
		}

		http.NotFound(w, r)
	})
}

func IsPortOpen(port int, timeout time.Duration) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func WaitHTTP(port int, path string, timeout time.Duration) bool {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 500 {
			_ = resp.Body.Close()
			return true
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
