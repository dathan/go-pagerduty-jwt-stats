package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/dathan/go-pagerduty-jwt-stats/pkg/dashboard"
	"github.com/dathan/go-pagerduty-jwt-stats/pkg/pdcapture"
)

const (
	defaultSubD	= "" // this needs to be defined
	defaultBaseURL  = "https://%s.pagerduty.com"
	analyticsPath   = "/api/v2/analytics/raw/incidents"
	defaultTeamID   = "" // this needs to be defined
	defaultDays     = 21
	defaultWindow   = 7
	defaultOutput   = "dashboard.html"
)

// ── PagerDuty Analytics API types ────────────────────────────────────────────

type analyticsRequest struct {
	Filters       analyticsFilters `json:"filters"`
	TimeZone      string           `json:"time_zone"`
	Limit         int              `json:"limit"`
	StartingAfter string           `json:"starting_after,omitempty"`
}

type analyticsFilters struct {
	CreatedAtStart string   `json:"created_at_start"`
	CreatedAtEnd   string   `json:"created_at_end"`
	TeamIDs        []string `json:"team_ids"`
}

type analyticsResponse struct {
	Data []pdIncident `json:"data"`
	More bool         `json:"more"`
	Last string       `json:"last"`
}

type pdIncident struct {
	IncidentID       string   `json:"incident_id"`
	IncidentNumber   int      `json:"incident_number"`
	Description      string   `json:"description"`
	ServiceName      string   `json:"service_name"`
	Status           string   `json:"status"`
	Urgency          string   `json:"urgency"`
	CreatedAt        string   `json:"created_at"`
	ResolvedAt       *string  `json:"resolved_at"`
	SecondsToResolve *float64 `json:"seconds_to_resolve"`
}

// ── JS-serializable types (match the dashboard JS object shapes) ──────────────

type jsAlertGroup struct {
	Name   string   `json:"name"`
	Count  int      `json:"count"`
	Active int      `json:"active"`
	Sites  []string `json:"sites"`
	Cat    string   `json:"cat"`
}

type jsIncident struct {
	Num    string   `json:"num"`
	Alert  string   `json:"alert"`
	Site   string   `json:"site"`
	Svc    string   `json:"svc"`
	Status string   `json:"status"`
	Urg    string   `json:"urg"`
	Date   string   `json:"date"`
	TTR    *float64 `json:"ttr"`
}

// ── Template data ─────────────────────────────────────────────────────────────

// dashLink is one entry in the window-selector dropdown.
type dashLink struct {
	Label    string // human-readable range, e.g. "Jul 2 – Jul 9"
	Filename string // basename of the output file, e.g. "dashboard_2026-07-09.html"
}

type dashData struct {
	FetchedAt      string
	WindowStart    string
	WindowEnd      string
	TeamID         string
	TotalIncidents int
	ForgeCount     int
	ActiveCount    int
	SiteCount      int
	AlertTypeCount int
	PeakDay        string
	PeakCount      int
	// Window navigation dropdown
	CurrentFile string
	Siblings    []dashLink
	// JSON strings injected directly into the HTML <script> block
	AlertsJSON    string
	SiteDataJSON  string
	TLLabelsJSON  string
	TLCountsJSON  string
	IncidentsJSON string
}

// ── Regex patterns ────────────────────────────────────────────────────────────

var (
	// Matches Prometheus alertmanager title format:
	// [FIRING:1] alertname forge_site (forge_site forge-monitoring/...)
	forgeRE = regexp.MustCompile(`\[(FIRING|RESOLVED):\d+\]\s+(\S+)\s+(\S+)\s+\(`)

	// forge_site must be a short lowercase alphanumeric identifier
	siteRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,9}$`)

	// Site-like prefix: all-lowercase letters, optional -lowercase groups, optional digits, dash
	// Matches: ytl-, az06-, pdx-lab-, prod1-, tlv01-, hfa01-
	// Does NOT match camelCase alertnames like forgeUnhealthyHostsFound
	prefixRE = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*\d*-`)

	// Extracts the -b '...' cookie value from a curl command.
	curlCookieRE = regexp.MustCompile(`(?s)-b\s+'([^']+)'`)
)

// parseCurlCookies reads a curl command string and returns the cookie header value
// found after the -b '...' flag. This lets you paste a curl from DevTools directly
// into a file and avoid shell quoting issues with semicolons in the cookie string.
func parseCurlCookies(curlStr string) (string, error) {
	m := curlCookieRE.FindStringSubmatch(curlStr)
	if m == nil {
		return "", fmt.Errorf("no -b '...' cookie argument found in curl command")
	}
	return m[1], nil
}

// cookiesFromCurlFile reads a curl command from the given path (or stdin if "-")
// and parses the cookie string from it.
func cookiesFromCurlFile(path string) (string, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read curl file: %w", err)
	}
	return parseCurlCookies(string(raw))
}

func main() {
	curlFile := flag.String("curl-file", os.Getenv("PD_CURL_FILE"),
		"Path to a file containing the curl command copied from DevTools (or '-' for stdin).\n"+
			"The program will extract the -b '...' cookie from it automatically.\n"+
			"Get it: DevTools → Network → any request → right-click → Copy as cURL → paste into a file.")
	cookies := flag.String("cookies", os.Getenv("PD_COOKIES"),
		"Raw PagerDuty cookie string (fallback if --curl-file is not set).\n"+
			"Must be single-quoted to avoid shell issues with semicolons:\n"+
			"  export PD_COOKIES='_pagerduty_session=...; __Host-pagerduty-login=...'")
	subDomain := flag.String("site", defaultSubD, "Company subdomain")
	baseURL := flag.String("base-url", "", "PagerDuty account base URL")
	teamID  := flag.String("team", defaultTeamID, "PagerDuty team ID")
	days    := flag.Int("days", defaultDays, "Total days back to query")
	window  := flag.Int("window", defaultWindow, "Window size in days per dashboard (generates one file per window across --days range)")
	output  := flag.String("output", defaultOutput, "Output HTML file (used as-is for single window; date suffix added for multiple)")
	open    := flag.Bool("open", false, "Open dashboard(s) in browser after writing")
	flag.Parse()

	// --site and --team are always required; auth can be obtained via browser.
	var missing []string
	if *subDomain == "" {
		missing = append(missing, "--site")
	}
	if *teamID == "" {
		missing = append(missing, "--team")
	}
	if len(missing) > 0 {
		usageString(missing)
		os.Exit(1)
	}

	// baseURL is needed before auth so browser capture can navigate to the right host.
	if *baseURL == "" {
		*baseURL = fmt.Sprintf(defaultBaseURL, *subDomain)
	}

	cookieStr := *cookies

	// 1. Prefer explicit curl file.
	if *curlFile != "" {
		parsed, err := cookiesFromCurlFile(*curlFile)
		if err != nil {
			log.Printf("auth: curl-file %q unusable (%v) — falling back to browser capture", *curlFile, err)
		} else {
			cookieStr = parsed
			log.Printf("auth: loaded cookies from curl file %q", *curlFile)
		}
	}

	// 2. Fall back to browser capture when no cookies are available.
	if cookieStr == "" {
		curlPath := *curlFile
		if curlPath == "" {
			curlPath = "conf/pd.curl"
		}
		log.Printf("auth: no cookies found — launching browser capture")
		captured, err := pdcapture.Capture(*baseURL, curlPath)
		if err != nil {
			log.Fatalf("browser capture: %v", err)
		}
		cookieStr = captured
	}

	// Align until to start of tomorrow so today's incidents are always included
	// and date-string comparisons work cleanly without time-of-day edge cases.
	now := time.Now().UTC()
	until := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	since := until.AddDate(0, 0, -*days)

	if *window <= 0 {
		log.Fatalf("--window must be > 0")
	}

	// Build list of time windows (oldest first).
	type timeWindow struct{ start, end time.Time }
	var windows []timeWindow
	for t := since; t.Before(until); t = t.AddDate(0, 0, *window) {
		end := t.AddDate(0, 0, *window)
		if end.After(until) {
			end = until
		}
		windows = append(windows, timeWindow{t, end})
	}

	log.Printf("team=%s  range=%s to %s  window=%dd  dashboards=%d",
		*teamID, since.Format("2006-01-02"), until.Format("2006-01-02"), *window, len(windows))

	// Fetch the full range once; filter per window in memory.
	// On 401, the session has expired — relaunch the browser to refresh cookies and retry once.
	allIncidents, err := fetchAll(cookieStr, *baseURL, *teamID, since, until)
	if err != nil {
		if !strings.Contains(err.Error(), "401") {
			log.Fatalf("fetch: %v", err)
		}
		log.Printf("auth: session expired (HTTP 401) — relaunching browser to refresh cookies")
		curlPath := *curlFile
		if curlPath == "" {
			curlPath = "conf/pd.curl"
		}
		refreshed, captureErr := pdcapture.Capture(*baseURL, curlPath)
		if captureErr != nil {
			log.Fatalf("browser capture: %v", captureErr)
		}
		cookieStr = refreshed
		allIncidents, err = fetchAll(cookieStr, *baseURL, *teamID, since, until)
		if err != nil {
			log.Fatalf("fetch after re-auth: %v", err)
		}
	}
	log.Printf("fetched %d incidents total", len(allIncidents))

	tmpl, err := template.New("dash").Parse(dashboard.DashboardTemplate)
	if err != nil {
		log.Fatalf("parse template: %v", err)
	}

	outputBase := strings.TrimSuffix(*output, ".html")

	// Pre-compute filenames and human-readable labels for all windows so every
	// dashboard can render a populated window-selector dropdown.
	type windowMeta struct {
		start, end time.Time
		filename   string
	}
	metas := make([]windowMeta, len(windows))
	siblings := make([]dashLink, len(windows))
	for i, w := range windows {
		filename := *output
		if len(windows) > 1 {
			filename = fmt.Sprintf("%s_%s.html", outputBase, w.end.Format("2006-01-02"))
		}
		metas[i] = windowMeta{w.start, w.end, filename}
		siblings[i] = dashLink{
			Label:    fmt.Sprintf("%s – %s", w.start.Format("Jan 2"), w.end.Format("Jan 2")),
			Filename: filepath.Base(filename),
		}
	}

	for i, m := range metas {
		winIncs := incidentsInWindow(allIncidents, m.start, m.end)

		data := buildDash(winIncs, *teamID, m.start, m.end)
		data.CurrentFile = filepath.Base(m.filename)
		data.Siblings = siblings

		f, err := os.Create(m.filename)
		if err != nil {
			log.Fatalf("create %s: %v", m.filename, err)
		}
		if err := tmpl.Execute(f, data); err != nil {
			f.Close()
			log.Fatalf("render %s: %v", m.filename, err)
		}
		f.Close()

		abs, _ := filepath.Abs(m.filename)
		log.Printf("written: %s  (%d incidents)", abs, len(winIncs))

		if *open {
			openBrowser(abs)
		}
		_ = i
	}
}

func usageString(missing []string) {
	fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n\n", os.Args[0])
	fmt.Fprintln(os.Stderr, "Flags:")
	flag.PrintDefaults()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Missing required flags:")
	for _, m := range missing {
		fmt.Fprintf(os.Stderr, "  • %s\n", m)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Auth — easiest method (paste a curl from DevTools):")
	fmt.Fprintln(os.Stderr, "  1. Go to yoursite.pagerduty.com in Chrome")
	fmt.Fprintln(os.Stderr, "  2. Open DevTools (F12) → Network tab")
	fmt.Fprintln(os.Stderr, "  3. Click any XHR/Fetch request → right-click → Copy as cURL")
	fmt.Fprintln(os.Stderr, "  4. pbpaste > /tmp/pd.curl")
	fmt.Fprintln(os.Stderr, "  5. Re-run with:  --curl-file /tmp/pd.curl --site <subdomain> --team <ID>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Or export PD_CURL_FILE=/tmp/pd.curl and omit --curl-file.")
}

// fetchAll calls the PagerDuty Analytics raw incidents API using browser session
// cookies and paginates via cursor until all results are retrieved.
func fetchAll(cookieStr, baseURL, teamID string, since, until time.Time) ([]pdIncident, error) {
	apiURL := baseURL + analyticsPath
	var all []pdIncident
	cursor := ""

	for {
		req := analyticsRequest{
			Filters: analyticsFilters{
				CreatedAtStart: since.UTC().Format("2006-01-02T15:04:05Z"),
				CreatedAtEnd:   until.UTC().Format("2006-01-02T15:04:05Z"),
				TeamIDs:        []string{teamID},
			},
			TimeZone:      "UTC",
			Limit:         1000,
			StartingAfter: cursor,
		}

		body, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}

		httpReq, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "*/*")
		httpReq.Header.Set("Cookie", cookieStr)
		httpReq.Header.Set("Referer", baseURL+"/analytics/insights/")
		httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("http: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf(
				"HTTP 401 Unauthorized — session cookies have expired.\n" +
					"  1. Open yoursite.pagerduty.com in Chrome\n" +
					"  2. DevTools → Network → any request → right-click → Copy as cURL\n" +
					"  3. Paste into a file:  pbpaste > /tmp/pd.curl\n" +
					"  4. Re-run:  go run . --curl-file /tmp/pd.curl --open",
			)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("PagerDuty returned HTTP %d", resp.StatusCode)
		}

		var result analyticsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}

		all = append(all, result.Data...)
		if !result.More {
			break
		}
		cursor = result.Last
	}
	return all, nil
}

// incidentsInWindow returns incidents whose created_at falls in [start, end).
// Comparison is done on the YYYY-MM-DD date prefix to avoid timestamp format
// differences across PagerDuty API responses.
func incidentsInWindow(incidents []pdIncident, start, end time.Time) []pdIncident {
	startDate := start.UTC().Format("2006-01-02")
	endDate := end.UTC().Format("2006-01-02") // exclusive: end is always midnight of the next day
	var out []pdIncident
	for _, inc := range incidents {
		if len(inc.CreatedAt) < 10 {
			continue
		}
		d := inc.CreatedAt[:10]
		if d >= startDate && d < endDate {
			out = append(out, inc)
		}
	}
	return out
}

// buildDash processes raw incidents into the struct the HTML template needs.
func buildDash(incidents []pdIncident, teamID string, since, until time.Time) dashData {
	type parsed struct {
		raw   pdIncident
		alert string // normalized alertname
		site  string // forge_site value
	}

	var forgeIncs []parsed
	for _, inc := range incidents {
		m := forgeRE.FindStringSubmatch(inc.Description)
		if m == nil {
			continue
		}
		alert, site := m[2], m[3]
		if !siteRE.MatchString(site) {
			continue
		}
		forgeIncs = append(forgeIncs, parsed{
			raw:   inc,
			alert: normalizeAlertname(alert),
			site:  site,
		})
	}

	// ── Alert groups ──────────────────────────────────────────────────────────
	type agState struct {
		jsAlertGroup
		siteSet map[string]bool
	}
	agMap := map[string]*agState{}

	for _, f := range forgeIncs {
		ag, ok := agMap[f.alert]
		if !ok {
			ag = &agState{
				jsAlertGroup: jsAlertGroup{Name: f.alert, Cat: categorize(f.alert)},
				siteSet:      map[string]bool{},
			}
			agMap[f.alert] = ag
		}
		ag.Count++
		if f.raw.Status == "triggered" || f.raw.Status == "acknowledged" {
			ag.Active++
		}
		ag.siteSet[f.site] = true
	}

	alerts := make([]jsAlertGroup, 0, len(agMap))
	for _, ag := range agMap {
		for s := range ag.siteSet {
			ag.Sites = append(ag.Sites, s)
		}
		sort.Strings(ag.Sites)
		alerts = append(alerts, ag.jsAlertGroup)
	}
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].Count > alerts[j].Count })

	// ── Site totals ───────────────────────────────────────────────────────────
	siteMap := map[string]int{}
	for _, f := range forgeIncs {
		siteMap[f.site]++
	}

	// ── Timeline ──────────────────────────────────────────────────────────────
	dateMap := map[string]int{}
	for _, f := range forgeIncs {
		if len(f.raw.CreatedAt) >= 10 {
			dateMap[f.raw.CreatedAt[:10]]++
		}
	}
	dates := make([]string, 0, len(dateMap))
	for d := range dateMap {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	tlLabels := make([]string, len(dates))
	tlCounts := make([]int, len(dates))
	peakDay, peakCount := "", 0
	for i, d := range dates {
		t, _ := time.Parse("2006-01-02", d)
		tlLabels[i] = t.Format("Jan 2")
		tlCounts[i] = dateMap[d]
		if dateMap[d] > peakCount {
			peakCount = dateMap[d]
			peakDay = t.Format("Jan 2 2006")
		}
	}

	// ── Incident table rows ───────────────────────────────────────────────────
	activeCount := 0
	jsIncs := make([]jsIncident, 0, len(forgeIncs))
	for _, f := range forgeIncs {
		if f.raw.Status == "triggered" || f.raw.Status == "acknowledged" {
			activeCount++
		}
		svc := strings.TrimSuffix(strings.TrimSuffix(f.raw.ServiceName, "-PagerDuty"), "-slack")
		date := ""
		if len(f.raw.CreatedAt) >= 10 {
			date = f.raw.CreatedAt[:10]
		}
		jsIncs = append(jsIncs, jsIncident{
			Num:    fmt.Sprintf("%d", f.raw.IncidentNumber),
			Alert:  f.alert,
			Site:   f.site,
			Svc:    svc,
			Status: f.raw.Status,
			Urg:    f.raw.Urgency,
			Date:   date,
			TTR:    f.raw.SecondsToResolve,
		})
	}

	mustJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			log.Panicf("marshal: %v", err)
		}
		return string(b)
	}

	return dashData{
		FetchedAt:      until.Format("Jan 2 2006"),
		WindowStart:    since.Format("Jan 2"),
		WindowEnd:      until.Format("Jan 2 2006"),
		TeamID:         teamID,
		TotalIncidents: len(incidents),
		ForgeCount:     len(forgeIncs),
		ActiveCount:    activeCount,
		SiteCount:      len(siteMap),
		AlertTypeCount: len(agMap),
		PeakDay:        peakDay,
		PeakCount:      peakCount,
		AlertsJSON:     mustJSON(alerts),
		SiteDataJSON:   mustJSON(siteMap),
		TLLabelsJSON:   mustJSON(tlLabels),
		TLCountsJSON:   mustJSON(tlCounts),
		IncidentsJSON:  mustJSON(jsIncs),
	}
}

// normalizeAlertname strips site-based prefixes from Prometheus alertnames.
// e.g. "ytl-stuckInstanceCritical" → "stuckInstanceCritical"
//      "az06-stuckInstanceCritical" → "stuckInstanceCritical"
//      "pdx-lab-elektraSiteAgentCarbideAPIDown" → "elektraSiteAgentCarbideAPIDown"
// camelCase alertnames with no kebab prefix are returned unchanged.
func normalizeAlertname(name string) string {
	m := prefixRE.FindString(name)
	if m == "" {
		return name
	}
	cleaned := name[len(m):]
	if len(cleaned) < 4 {
		return name
	}
	return cleaned
}

// categorize assigns a display category based on alertname keywords.
func categorize(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "dpulogs") || strings.Contains(n, "nvlink") ||
		strings.Contains(n, "ibfabric") || strings.Contains(n, "ibpartition"):
		return "Hardware"
	case strings.Contains(n, "elektra") || strings.Contains(n, "carbide") ||
		strings.Contains(n, "vault"):
		return "Carbide"
	case strings.Contains(n, "k8s") || strings.Contains(n, "kubernetes") ||
		strings.Contains(n, "nmxm"):
		return "Kubernetes"
	default:
		return "NICo Health"
	}
}

func openBrowser(absPath string) {
	url := "file://" + absPath
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	if err := exec.Command(cmd, args...).Start(); err != nil {
		log.Printf("open browser: %v", err)
	}
}
