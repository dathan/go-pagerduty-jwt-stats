package pdcapture

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	analyticsPath  = "/api/v2/analytics/raw/incidents"
	captureTimeout = 5 * time.Minute
)

// Capture opens a headed Chrome browser, navigates to the PagerDuty analytics
// page, and waits for the analytics XHR to fire. It extracts session cookies
// from the browser's cookie store at that moment, writes them to curlFilePath
// for future reuse, and returns the raw cookie string.
//
// Chrome must be installed (standard macOS/Linux install is sufficient —
// no additional driver download is required).
//
// If the user is not yet logged in, Capture waits up to captureTimeout for
// them to complete login. The cookies are captured automatically once the
// analytics page loads and the XHR fires.
func Capture(baseURL, curlFilePath string) (string, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(
		context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", false),
			chromedp.Flag("disable-background-timer-throttling", true),
		)...,
	)
	defer allocCancel()

	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	defer ctxCancel()

	ctx, timeoutCancel := context.WithTimeout(ctx, captureTimeout)
	defer timeoutCancel()

	cookieCh := make(chan string, 1)

	// Listen for the analytics API response — fires once the user is logged in
	// and the analytics page loads. We spawn a goroutine to avoid blocking the
	// CDP event-dispatch loop.
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		e, ok := ev.(*network.EventResponseReceived)
		if !ok || !strings.Contains(string(e.Response.URL), analyticsPath) {
			return
		}
		go func() {
			cookies, err := network.GetCookies().Do(ctx)
			if err != nil || len(cookies) == 0 {
				return
			}
			parts := make([]string, 0, len(cookies))
			for _, c := range cookies {
				parts = append(parts, c.Name+"="+c.Value)
			}
			select {
			case cookieCh <- strings.Join(parts, "; "):
			default:
			}
		}()
	})

	target := baseURL + "/analytics/insights/"
	fmt.Fprintf(os.Stderr, "\n==> Opening browser: %s\n", target)
	fmt.Fprintln(os.Stderr, "    Log in if prompted. Cookies will be captured automatically once the analytics page loads.")
	fmt.Fprintf(os.Stderr, "    Waiting up to %v...\n\n", captureTimeout)

	// Enable network monitoring then navigate. Navigation may redirect to login —
	// that is expected and non-fatal. The browser stays open and the listener
	// remains active until cookies are captured or the timeout fires.
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		return "", fmt.Errorf("enable network monitoring: %w", err)
	}

	// Navigate in a goroutine so we can enter the select below immediately.
	// chromedp actions are serialized through the CDP session so this is safe.
	go func() {
		if err := chromedp.Run(ctx, chromedp.Navigate(target)); err != nil {
			if !isContextErr(err) {
				fmt.Fprintf(os.Stderr, "    (navigation: %v — waiting for login)\n", err)
			}
		}
	}()

	select {
	case cookie := <-cookieCh:
		if err := saveCurlFile(curlFilePath, baseURL, cookie); err != nil {
			fmt.Fprintf(os.Stderr, "warn: could not save curl file %q: %v\n", curlFilePath, err)
		} else {
			fmt.Fprintf(os.Stderr, "==> Session saved to %s\n\n", curlFilePath)
		}
		return cookie, nil
	case <-ctx.Done():
		return "", fmt.Errorf("timed out after %v — did you complete login in the browser?", captureTimeout)
	}
}

func isContextErr(err error) bool {
	s := err.Error()
	return strings.Contains(s, "context canceled") || strings.Contains(s, "context deadline exceeded")
}

// saveCurlFile writes a curl command in the format parseCurlCookies expects,
// so the next run can skip browser capture entirely.
func saveCurlFile(path, baseURL, cookie string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("curl '%s%s' -b '%s'\n", baseURL, analyticsPath, cookie)
	return os.WriteFile(path, []byte(content), 0o600)
}
