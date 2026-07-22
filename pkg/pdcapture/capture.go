package pdcapture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

const (
	analyticsPath  = "/api/v2/analytics/raw/incidents"
	captureTimeout = 5 * time.Minute
)

// Capture opens a headed Chromium browser, navigates to the PagerDuty analytics
// page, and waits for the analytics XHR request to fire. It extracts the session
// cookies from that request, writes a curl file to curlFilePath for future reuse,
// and returns the raw cookie string.
//
// If the user is not logged in, Capture waits up to captureTimeout for them to
// complete login — the analytics XHR is captured automatically once the page loads.
func Capture(baseURL, curlFilePath string) (string, error) {
	if err := ensureInstalled(); err != nil {
		return "", err
	}

	pw, err := playwright.Run()
	if err != nil {
		return "", fmt.Errorf("start playwright: %w\n  hint: run `make playwright-install` first", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	})
	if err != nil {
		return "", fmt.Errorf("launch chromium: %w", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		return "", fmt.Errorf("new page: %w", err)
	}

	cookieCh := make(chan string, 1)
	page.OnRequest(func(req playwright.Request) {
		if !strings.Contains(req.URL(), analyticsPath) {
			return
		}
		headers, err := req.AllHeaders()
		if err != nil {
			return
		}
		cookie := headers["cookie"]
		if cookie == "" {
			return
		}
		select {
		case cookieCh <- cookie:
		default:
		}
	})

	target := baseURL + "/analytics/insights/"
	fmt.Fprintf(os.Stderr, "\n==> Browser opened: %s\n", target)
	fmt.Fprintln(os.Stderr, "    Log in if prompted — cookies will be captured automatically once the analytics page loads.")
	fmt.Fprintf(os.Stderr, "    Waiting up to %v...\n\n", captureTimeout)

	if _, err := page.Goto(target); err != nil {
		// Non-fatal: a redirect to the login page causes Goto to error on some
		// playwright builds. The request listener remains active so we keep waiting.
		fmt.Fprintf(os.Stderr, "    (navigation: %v — waiting for login and redirect back)\n", err)
	}

	select {
	case cookie := <-cookieCh:
		if err := saveCurlFile(curlFilePath, baseURL, cookie); err != nil {
			fmt.Fprintf(os.Stderr, "warn: could not save curl file %q: %v\n", curlFilePath, err)
		} else {
			fmt.Fprintf(os.Stderr, "==> Session saved to %s\n\n", curlFilePath)
		}
		return cookie, nil

	case <-time.After(captureTimeout):
		return "", fmt.Errorf("timed out after %v — did you complete login in the browser?", captureTimeout)
	}
}

// ensureInstalled installs the playwright driver + chromium browser if not already
// present. The install is idempotent and skips the download when up to date.
func ensureInstalled() error {
	err := playwright.Install(&playwright.RunOptions{
		Browsers: []string{"chromium"},
		Verbose:  false,
		Stdout:   os.Stderr,
		Stderr:   os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("install playwright chromium: %w", err)
	}
	return nil
}

// saveCurlFile writes a minimal curl command containing the captured cookie so
// that parseCurlCookies in main can read it back on the next run.
func saveCurlFile(path, baseURL, cookie string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("curl '%s%s' -b '%s'\n", baseURL, analyticsPath, cookie)
	return os.WriteFile(path, []byte(content), 0o600)
}
