package browser

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/chromedp/chromedp"
	"github.com/maxischmaxi/qsnap/internal/logging"
	"github.com/maxischmaxi/qsnap/internal/tools"
	"go.uber.org/zap"
)

type Instance struct {
	AllocCancel context.CancelFunc
	Ctx         context.Context
	cancel      context.CancelFunc
	ID          int
}

type Instances []*Instance

func (is Instances) Pick() *Instance {
	if len(is) == 1 {
		return is[0]
	}
	return is[rand.Intn(len(is))]
}

func (is Instances) CloseAll() {
	for _, it := range is {
		if it.cancel != nil {
			it.cancel()
		}
		if it.AllocCancel != nil {
			it.AllocCancel()
		}
	}
}

func (is Instances) PickIdx() (*Instance, int) {
	if len(is) == 1 {
		return is[0], is[0].ID
	}
	return is[rand.Intn(len(is))], is[0].ID
}

func LaunchPool(root context.Context, n int) (Instances, error) {
	if n < 1 {
		n = 1
	}
	instances := make([]*Instance, 0, n)
	for i := 0; i < n; i++ {
		inst, err := launchOne(root, false)
		if err != nil {
			// alle bereits gestarteten wieder schließen
			for _, it := range instances {
				if it.cancel != nil {
					it.cancel()
				}
				if it.AllocCancel != nil {
					it.AllocCancel()
				}
			}
			logging.L.Error("failed to launch browser instance", zap.Error(err))
			return nil, err
		}
		inst.ID = i
		instances = append(instances, inst)
	}

	logging.L.Info("launched browser instances", zap.Int("count", len(instances)))
	return instances, nil
}

func launchOne(root context.Context, devtools bool) (*Instance, error) {
	opts := chromedp.DefaultExecAllocatorOptions[:]
	opts = append(opts,
		chromedp.Flag("headless", !devtools),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-ipc-flooding-protection", true),
		chromedp.Flag("disable-features", "Translate,BackForwardCache"),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
	)

	// Optional: eigenen Binary pflegen
	if bin := os.Getenv("CHROME_BIN"); bin != "" {
		if tools.FileExists(bin) {
			opts = append(opts, chromedp.ExecPath(bin))
		}
	} else if p, _ := findChrome(); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(root, opts...)
	ctx, cancel := chromedp.NewContext(allocCtx) // browser-wide context
	// Warmup: starten
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		allocCancel()
		logging.L.Error("failed to start browser", zap.Error(err))
		return nil, err
	}

	return &Instance{AllocCancel: allocCancel, Ctx: ctx, cancel: cancel}, nil
}

func findChrome() (string, error) {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "linux":
		candidates = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"}
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		prog := os.Getenv("ProgramFiles")
		prog86 := os.Getenv("ProgramFiles(x86)")
		candidates = []string{
			filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(prog, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(prog86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(prog, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(prog86, `Microsoft\Edge\Application\msedge.exe`),
		}
	default:
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
		if tools.FileExists(c) {
			return c, nil
		}
	}
	logging.L.Warn("chrome binary not found in standard paths, relying on system PATH")
	return "", errors.New("chrome not found")
}
