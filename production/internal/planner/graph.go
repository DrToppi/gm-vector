package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var ErrIntegrationDisabled = errors.New("Teams module disabled by owner")

type GraphAdapter struct {
	Enabled bool
	Client  *http.Client
}
type BusyBlock struct {
	ID         string `json:"id"`
	EmployeeID string `json:"employee_id"`
	ProjectID  string `json:"project_id"`
	Date       string `json:"date"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Timezone   string `json:"timezone"`
}

func OAuthConfig(enabled bool, tenant, clientID, redirect string) (*oauth2.Config, error) {
	if !enabled {
		return nil, ErrIntegrationDisabled
	}
	if !validID.MatchString(tenant) || clientID == "" || !strings.HasPrefix(redirect, "https://") {
		return nil, errors.New("invalid OAuth configuration")
	}
	return &oauth2.Config{ClientID: clientID, RedirectURL: redirect, Scopes: []string{"offline_access", "https://graph.microsoft.com/Calendars.ReadWrite"}, Endpoint: oauth2.Endpoint{AuthURL: "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/authorize", TokenURL: "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token"}}, nil
}
func (g *GraphAdapter) call(ctx context.Context, method, path string, body any, etag string) (json.RawMessage, error) {
	if !g.Enabled {
		return nil, ErrIntegrationDisabled
	}
	if g.Client == nil || !strings.HasPrefix(path, "/me/") || strings.Contains(path, "..") {
		return nil, errors.New("invalid Graph request")
	}
	var data []byte
	var e error
	if body != nil {
		data, e = json.Marshal(body)
		if e != nil {
			return nil, e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, "https://graph.microsoft.com/v1.0"+path, bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	client := *g.Client
	client.Timeout = 20 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("Graph redirect denied") }
	res, e := client.Do(req)
	if e != nil {
		return nil, errors.New("Graph transport failed")
	}
	defer res.Body.Close()
	if res.StatusCode == 429 || res.StatusCode == 503 {
		return nil, fmt.Errorf("Graph retryable status %d; Retry-After %s", res.StatusCode, res.Header.Get("Retry-After"))
	}
	if res.StatusCode == 412 {
		return nil, errors.New("Graph event changed externally")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Graph status %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 2<<20))
}
func (g *GraphAdapter) ReadBusy(ctx context.Context, email, start, end string) (json.RawMessage, error) {
	return g.call(ctx, "POST", "/me/calendar/getSchedule", map[string]any{"schedules": []string{email}, "startTime": map[string]string{"dateTime": start, "timeZone": "Russian Standard Time"}, "endTime": map[string]string{"dateTime": end, "timeZone": "Russian Standard Time"}, "availabilityViewInterval": 30}, "")
}
func (g *GraphAdapter) Publish(ctx context.Context, b BusyBlock, remoteID, etag string) (json.RawMessage, error) {
	payload := map[string]any{"subject": "Занято — GMA Planner", "showAs": "busy", "isReminderOn": false, "sensitivity": "private", "start": map[string]string{"dateTime": b.Date + "T" + b.Start + ":00", "timeZone": "Russian Standard Time"}, "end": map[string]string{"dateTime": b.Date + "T" + b.End + ":00", "timeZone": "Russian Standard Time"}}
	if remoteID == "" {
		payload["transactionId"] = b.ID
		payload["extensions"] = []any{map[string]string{"@odata.type": "microsoft.graph.openTypeExtension", "extensionName": "com.gma.planner", "operationId": b.ID}}
		return g.call(ctx, "POST", "/me/events", payload, "")
	}
	if etag == "" {
		return nil, errors.New("event ETag required")
	}
	return g.call(ctx, "PATCH", "/me/events/"+url.PathEscape(remoteID), payload, etag)
}
func (g *GraphAdapter) DeleteOwned(ctx context.Context, remoteID, etag, storedOperationID, expectedOperationID string) error {
	if !g.Enabled {
		return ErrIntegrationDisabled
	}
	if remoteID == "" || etag == "" || storedOperationID == "" || storedOperationID != expectedOperationID {
		return errors.New("ownership/ETag required")
	}
	_, e := g.call(ctx, "DELETE", "/me/events/"+url.PathEscape(remoteID), nil, etag)
	return e
}
func BuildBusyBlocks(projects []Project) ([]BusyBlock, []string) {
	blocks := []BusyBlock{}
	warnings := []string{}
	for _, p := range projects {
		if p.CalculationState != "ready" {
			warnings = append(warnings, "Расчёт проекта не готов: публикация занятости отключена")
			continue
		}
		people := map[string]Participation{}
		for _, v := range p.People {
			people[v.EmployeeID] = v
		}
		totals := map[string]int{}
		for _, l := range p.Loads {
			totals[l.EmployeeID+"/"+l.Date] += l.Minutes
		}
		for key, n := range totals {
			parts := strings.Split(key, "/")
			v, ok := people[parts[0]]
			d, _ := date(parts[1])
			if !ok || !active(v, d) || n == 0 {
				continue
			}
			day := participationActivity(v, d)
			start := day.Start
			size, e := activityCapacity(day)
			if e != nil || n > size {
				warnings = append(warnings, key+": работа не помещается в окно")
				continue
			}
			t, _ := time.Parse("15:04", start)
			blocks = append(blocks, BusyBlock{ID: hash(p.ID + "/" + key), EmployeeID: parts[0], ProjectID: p.ID, Date: parts[1], Start: start, End: t.Add(time.Duration(n) * time.Minute).Format("15:04"), Timezone: "Europe/Moscow"})
		}
	}
	invalid := map[string]bool{}
	for i, a := range blocks {
		for _, b := range blocks[i+1:] {
			if a.EmployeeID == b.EmployeeID && a.Date == b.Date && a.Start < b.End && b.Start < a.End {
				invalid[a.ID] = true
				invalid[b.ID] = true
				warnings = append(warnings, a.EmployeeID+"/"+a.Date+": пересекаются блоки проектов")
			}
		}
	}
	out := []BusyBlock{}
	for _, b := range blocks {
		if !invalid[b.ID] {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	sort.Strings(warnings)
	return out, warnings
}
