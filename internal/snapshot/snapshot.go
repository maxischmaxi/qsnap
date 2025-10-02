package snapshot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/maxischmaxi/qsnap/internal/browser"
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
				return errors.New("timeout waiting for any selector")
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

func Capture(ctx context.Context, inst *browser.Instance, url, outPath string, vw, vh int, waitSelectors []string) ([]byte, error) {
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
		return nil, err
	}

	return buf, nil
}
