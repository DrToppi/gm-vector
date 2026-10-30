package planner

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
)

type Project struct {
	CalendarVersions  map[string]string `json:"calendar_versions"`
	ProvisionalYears  []int             `json:"provisional_years,omitempty"`
	CalculationState  string            `json:"calculation_state,omitempty"`
	ID                string            `json:"id"`
	Title             string            `json:"title"`
	Brief             string            `json:"brief"`
	Start             string            `json:"start"`
	Mode              string            `json:"duration_mode"`
	CalendarID        string            `json:"calendar_id"`
	Revision          int64             `json:"revision"`
	ExternalReference string            `json:"external_reference"`
	Tasks             []Task            `json:"tasks"`
	Links             []Link            `json:"links"`
	People            []Participation   `json:"people"`
	Warnings          []string          `json:"warnings,omitempty"`
	Loads             []Load            `json:"loads,omitempty"`
}
type Task struct {
	DurationDays   int          `json:"duration_days"`
	ID             string       `json:"id"`
	Parent         string       `json:"parent"`
	Title          string       `json:"title"`
	Description    string       `json:"description"`
	Result         string       `json:"result"`
	Brief          string       `json:"brief"`
	Status         string       `json:"status"`
	RequestedStart string       `json:"requested_start"`
	Start          string       `json:"start"`
	End            string       `json:"end"`
	Planned        int          `json:"planned_minutes"`
	Actual         int          `json:"actual_minutes"`
	LegacyActual   int          `json:"legacy_actual_minutes"`
	Responsibility string       `json:"responsibility"`
	CustomerEmail  string       `json:"customer_email"`
	CustomerOwner  string       `json:"customer_owner"`
	CustomerDue    string       `json:"customer_due"`
	OwnerLegacy    string       `json:"owner_legacy"`
	LegacyStart    string       `json:"legacy_start"`
	LegacyEnd      string       `json:"legacy_end"`
	Assignments    []Assignment `json:"assignments"`
	Summary        bool         `json:"summary"`
	Blocked        bool         `json:"blocked"`
}
type Link struct {
	From string `json:"from"`
	To   string `json:"to"`
	Mode string `json:"mode"`
	Days int    `json:"days"`
}
type Assignment struct {
	EmployeeID string `json:"employee_id"`
	Daily      int    `json:"daily_minutes"`
}
type Participation struct {
	Week        map[string]Activity `json:"week,omitempty"`
	EmployeeID  string              `json:"employee_id"`
	Weekdays    []int               `json:"weekdays"`
	WindowStart string              `json:"window_start"`
	WindowEnd   string              `json:"window_end"`
	Exceptions  map[string]Activity `json:"exceptions"`
}
type Activity struct {
	Daily  int    `json:"daily_minutes"`
	Active bool   `json:"active"`
	Start  string `json:"start"`
	End    string `json:"end"`
}
type Employee struct {
	Email      string `json:"email"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Daily      int    `json:"daily_minutes"`
	UserID     string `json:"user_id"`
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
}
type Calendar struct {
	allowProvisional bool
	verifiedYears    map[int]bool
	ID               string         `json:"id"`
	Year             int            `json:"year"`
	Title            string         `json:"title"`
	Source           string         `json:"source"`
	Checksum         string         `json:"checksum"`
	Days             map[string]Day `json:"days"`
}
type Day struct {
	Working   bool `json:"working"`
	Shortened bool `json:"shortened"`
}
type Load struct {
	EmployeeID string `json:"employee_id"`
	TaskID     string `json:"task_id"`
	Date       string `json:"date"`
	Minutes    int    `json:"minutes"`
}
type User struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	Admin      bool   `json:"admin"`
	Active     bool   `json:"active"`
	EmployeeID string `json:"employee_id"`
}
type Principal struct {
	User
	CSRF     string
	Token    bool
	Projects []string
	Scopes   []string
}
type TimeEntry struct {
	ID         string `json:"id"`
	TaskID     string `json:"task_id"`
	EmployeeID string `json:"employee_id"`
	Date       string `json:"date"`
	Minutes    int    `json:"minutes"`
	Comment    string `json:"comment"`
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func newID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return hex.EncodeToString(b)
}
func date(s string) (time.Time, error) { return time.Parse("2006-01-02", s) }
func fmtDate(d time.Time) string       { return d.Format("2006-01-02") }
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func checkText(s string, max int, required bool) error {
	if len(s) > max || (required && strings.TrimSpace(s) == "") {
		return errors.New("проверьте длину текстового поля")
	}
	return nil
}

// TaskComment is immutable user text plus a server-owned delivery projection.
type TaskComment struct {
	ID             string `json:"id"`
	ProjectID      string `json:"project_id"`
	TaskID         string `json:"task_id"`
	AuthorID       string `json:"author_id"`
	AuthorName     string `json:"author_name"`
	Text           string `json:"text"`
	CreatedAt      string `json:"created_at"`
	SendToCustomer bool   `json:"send_to_customer"`
	Recipient      string `json:"recipient"`
	DeliveryState  string `json:"delivery_state"`
	DeliveryError  string `json:"delivery_error"`
}
