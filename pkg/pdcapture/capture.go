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
	pollInterval   = 1 * time.Second
)

// Capture opens a headed Chrome browser, navigates to the PagerDuty analytics
// page, and polls until the user is logged in and back on the analytics URL.
// At that point it extracts session cookies from the browser, writes them to
// curlFilePath for future reuse, and returns the raw cookie string.
//
// Chrome must be installed (standard macOS/Linux install is sufficient —
// no additional driver download is required).
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

	target := baseURL + "/analytics/insights/"
	fmt.Fprintf(os.Stderr, "\n==> Opening browser: %s\n", target)
	fmt.Fprintln(os.Stderr, "    Log in if prompted, then wait for the analytics page to fully load.")
	fmt.Fprintf(os.Stderr, "    Waiting up to %v...\n\n", captureTimeout)

	// Enable network monitoring and navigate. Navigation returns once the
	// initial page (analytics or login) fires its load event — which is fast.
	// We handle navigation errors gracefully since a redirect to login
	// will surface as an error on some Chrome builds.
	if err := chromedp.Run(ctx, network.Enable(), chromedp.Navigate(target)); err != nil {
		if !isContextErr(err) {
			fmt.Fprintf(os.Stderr, "    (navigation: %v — waiting for login)\n", err)
		}
	}

	// Poll the current URL every second. Once the user completes login,
	// PagerDuty redirects back to /analytics/insights/ — at which point the
	// session cookies are present in the browser and ready to capture.
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cookie, err := tryCaptureCookies(ctx, baseURL)
			if err != nil || cookie == "" {
				continue
			}
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
}

// tryCaptureCookies checks whether the browser is currently on the analytics
// page and, if so, returns the formatted cookie string. Returns ("", nil)
// when the user is not yet on the analytics page.
func tryCaptureCookies(ctx context.Context, baseURL string) (string, error) {
	var currentURL string
	if err := chromedp.Run(ctx, chromedp.Location(&currentURL)); err != nil {
		return "", nil // transient — ignore and retry
	}
	if !strings.Contains(currentURL, "/analytics/") {
		return "", nil
	}

	var cookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		cookies, err = network.GetCookies().Do(c)
		return err
	})); err != nil {
		return "", fmt.Errorf("get cookies: %w", err)
	}
	if len(cookies) == 0 {
		return "", nil
	}

	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; "), nil
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
