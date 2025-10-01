package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/maxischmaxi/qsnap/internal/browser"
	"github.com/maxischmaxi/qsnap/internal/logging"
	"go.uber.org/zap"
)

func ParseSelectors(csv string) []string {
	parts := strings.Split(csv, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{"body"}
	}
	return out
}

func waitAny(selectors []string, timeout time.Duration) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(timeout)
		for {
			for _, sel := range selectors {
				if err := chromedp.Run(ctx, chromedp.WaitReady(sel, chromedp.ByQuery)); err == nil {
					return nil
				}
			}
			if time.Now().After(deadline) {
				logging.L.Error(
					"timeout waiting for any selector",
					zap.Strings("selectors", selectors),
					zap.Duration("timeout", timeout),
				)
				return errors.New("timeout waiting for any selector")
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

func Capture(ctx context.Context, inst *browser.Instance, url, outPath string, vw, vh int, waitSelectors []string) ([]byte, error) {
	logging.L.Info(
		"capturing snapshot",
		zap.String("url", url),
		zap.String("file", outPath),
		zap.Strings("waitSelectors", waitSelectors),
		zap.Int("width", vw),
		zap.Int("height", vh),
		zap.String("instance", fmt.Sprintf("%d", inst.ID)),
	)

	tabCtx, cancel := chromedp.NewContext(inst.Ctx)
	defer cancel()

	// Set viewport und navigate
	var buf []byte
	err := chromedp.Run(tabCtx,
		chromedp.EmulateViewport(int64(vw), int64(vh)),
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery), // Grundvoraussetzung
		waitAny(waitSelectors, 10*time.Second),
		chromedp.Sleep(50*time.Millisecond), // kleines settle gegen Fonts/Transitions
		chromedp.FullScreenshot(&buf, 100),
	)
	if err != nil {
		logging.L.Error(
			"failed to capture screenshot",
			zap.String("url", url),
			zap.String("file", outPath),
			zap.Int("width", vw),
			zap.Int("height", vh),
			zap.String("instance", fmt.Sprintf("%d", inst.ID)),
			zap.Error(err),
		)
		return nil, err
	}

	logging.L.Info(
		"screenshot captured",
		zap.String("url", url),
		zap.String("file", outPath),
		zap.Int("width", vw),
		zap.Int("height", vh),
		zap.Int("bytes", len(buf)),
	)

	return buf, os.WriteFile(outPath, buf, 0o644)
}
