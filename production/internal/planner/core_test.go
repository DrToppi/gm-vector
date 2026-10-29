package planner

import (
	"context"
	"encoding/base32"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func testParticipationWeek(days []int, minutes int, start, end string) map[string]Activity {
	week := map[string]Activity{}
	for _, day := range days {
		week[strconv.Itoa(day)] = Activity{Active: true, Start: start, End: end, Daily: minutes}
	}
	return week
}

func fixture() (Project, Calendar, map[string]Employee) {
	c := Calendar{ID: "test", Year: 2026, Title: "Fixture", Source: "synthetic test", Days: map[string]Day{}}
	for d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() == 2026; d = d.AddDate(0, 0, 1) {
		c.Days[fmtDate(d)] = Day{Working: d.Weekday() != 0 && d.Weekday() != 6}
	}
	p := Project{ID: "p", Title: "Test", Start: "2026-10-23", Mode: "working", CalendarID: c.ID, Tasks: []Task{{ID: "a", Title: "A", Status: "todo", RequestedStart: "2026-10-23", Planned: 600, Responsibility: "internal", Assignments: []Assignment{{"e", 240}}}}, Links: []Link{}, People: []Participation{{EmployeeID: "e", Weekdays: []int{1, 2, 3, 4, 5}, WindowStart: "09:00", WindowEnd: "18:00", Week: testParticipationWeek([]int{1, 2, 3, 4, 5}, 480, "09:00", "18:00"), Exceptions: map[string]Activity{}}}}
	return p, c, map[string]Employee{"e": {ID: "e", Name: "Employee", Daily: 480}}
}
func TestWorkingCalendarAndRemainder(t *testing.T) {
	p, c, e := fixture()
	if err := Calculate(&p, c, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[0].End != "2026-10-27" {
		t.Fatal(p.Tasks[0].End)
	}
	sum := 0
	for _, l := range p.Loads {
		sum += l.Minutes
	}
	if sum != 600 || len(p.Loads) != 3 || p.Loads[2].Minutes != 120 {
		t.Fatal(p.Loads)
	}
	p.Mode = "calendar"
	if err := Calculate(&p, c, e, time.Now()); err != nil || p.Tasks[0].End != "2026-10-27" {
		t.Fatal("weekends must pause both modes")
	}
}
func TestActivityExceptionAndParallelRemainder(t *testing.T) {
	p, c, e := fixture()
	p.Start = "2026-10-19"
	p.Tasks[0].RequestedStart = p.Start
	p.People[0].Week = testParticipationWeek([]int{1, 3}, 480, "09:00", "18:00")
	p.Tasks[0].Planned = 480
	if err := Calculate(&p, c, e, time.Now()); err != nil || p.Tasks[0].End != "2026-10-21" {
		t.Fatal(p, err)
	}
	p.People[0].Exceptions["2026-10-21"] = Activity{Active: false}
	if err := Calculate(&p, c, e, time.Now()); err != nil || p.Tasks[0].End != "2026-10-26" {
		t.Fatal(p, err)
	}
	p, c, e = fixture()
	e["f"] = Employee{ID: "f", Daily: 480}
	v := p.People[0]
	v.EmployeeID = "f"
	p.People = append(p.People, v)
	p.Tasks[0].Assignments = append(p.Tasks[0].Assignments, Assignment{"f", 240})
	p.Tasks[0].Planned = 481
	if err := Calculate(&p, c, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, l := range p.Loads {
		sum += l.Minutes
	}
	if sum != 481 {
		t.Fatal("rounding inflated work", sum)
	}
}
func TestCyclesAndMissingCalendar(t *testing.T) {
	p, c, e := fixture()
	b := p.Tasks[0]
	b.ID = "b"
	p.Tasks = append(p.Tasks, b)
	p.Links = []Link{{"a", "b", "after", 0}, {"b", "a", "after", 0}}
	if Calculate(&p, c, e, time.Now()) == nil {
		t.Fatal("cycle accepted")
	}
	p.Links = nil
	p.Tasks[0].Parent = "b"
	p.Links = []Link{{"b", "a", "after", 0}}
	if Calculate(&p, c, e, time.Now()) == nil {
		t.Fatal("container to own leaf accepted")
	}
	p, c, e = fixture()
	delete(c.Days, "2026-10-23")
	if Calculate(&p, c, e, time.Now()) == nil {
		t.Fatal("missing calendar inferred")
	}
}
func TestDependencyAndSummary(t *testing.T) {
	p, c, e := fixture()
	p.Tasks[0].Status = "done"
	p.Tasks[0].Start = "2026-10-22"
	p.Tasks[0].End = "2026-10-23"
	b := p.Tasks[0]
	b.ID = "b"
	b.Status = "todo"
	b.Planned = 240
	b.Start = ""
	b.End = ""
	p.Tasks = append(p.Tasks, b)
	p.Links = []Link{{"a", "b", "after", 0}}
	if err := Calculate(&p, c, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[1].Start != "2026-10-26" {
		t.Fatal(p.Tasks[1])
	}
	p.Tasks = append(p.Tasks, Task{ID: "group", Title: "Summary", Status: "todo", Responsibility: "internal"})
	p.Tasks[0].Parent = "group"
	p.Tasks[1].Parent = "group"
	if err := Calculate(&p, c, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !p.Tasks[2].Summary || p.Tasks[2].Planned != 840 {
		t.Fatal(p.Tasks[2])
	}
}
func TestMemberChangeDoesNotMutateInput(t *testing.T) {
	old, _, _ := fixture()
	p := old
	p.Tasks = append([]Task(nil), old.Tasks...)
	p.Tasks[0].Status = "doing"
	if err := memberChange(Principal{User: User{EmployeeID: "e"}}, "member", old, p); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[0].Status != "doing" {
		t.Fatal("permission check mutated request")
	}
	p.Tasks[0].Title = "forbidden"
	if memberChange(Principal{User: User{EmployeeID: "e"}}, "member", old, p) == nil {
		t.Fatal("member modified title")
	}
}
func TestPasswordAndEncryption(t *testing.T) {
	s := "correct horse battery staple"
	h := passwordHash(s)
	if !passwordOK(s, h) || passwordOK("wrong", h) || passwordOK(s, "malformed") {
		t.Fatal("password verification")
	}
	key := random(32)
	enc, err := seal(key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := unseal(key, enc)
	if err != nil || string(plain) != "secret" {
		t.Fatal("AEAD")
	}
	enc[len(enc)-1] ^= 1
	if _, err = unseal(key, enc); err == nil {
		t.Fatal("tamper accepted")
	}
}
func TestTOTPReplay(t *testing.T) {
	secret := totpSecret()
	now := time.Unix(1700000000, 0)
	step := now.Unix() / 30
	code := totpAt(secret, step)
	if totpStep(secret, code, -1, now) != step || totpStep(secret, code, step, now) != -1 {
		t.Fatal("TOTP replay")
	}
}

type countingTransport struct{ calls int }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls++
	panic("network must not be reached")
}
func TestTeamsDisabledMakesNoRequests(t *testing.T) {
	c := &countingTransport{}
	g := GraphAdapter{Enabled: false, Client: &http.Client{Transport: c}}
	if _, err := g.ReadBusy(context.Background(), "x", "s", "e"); err != ErrIntegrationDisabled {
		t.Fatal(err)
	}
	if _, err := g.Publish(context.Background(), BusyBlock{}, "", ""); err != ErrIntegrationDisabled {
		t.Fatal(err)
	}
	if err := g.DeleteOwned(context.Background(), "a", "b", "c", "c"); err != ErrIntegrationDisabled {
		t.Fatal(err)
	}
	if _, err := OAuthConfig(false, "", "", ""); err != ErrIntegrationDisabled {
		t.Fatal(err)
	}
	if c.calls != 0 {
		t.Fatal(c.calls)
	}
}
func TestBusyBlocksRejectOverflowAndCrossProjectOverlap(t *testing.T) {
	p, c, e := fixture()
	if err := Calculate(&p, c, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	blocks, w := BuildBusyBlocks([]Project{p})
	if len(blocks) != 3 || len(w) != 0 {
		t.Fatal(blocks, w)
	}
	other := p
	other.ID = "other"
	blocks, w = BuildBusyBlocks([]Project{p, other})
	if len(blocks) != 0 || len(w) == 0 {
		t.Fatal("cross-project overlap accepted")
	}
	p.People[0].Week = testParticipationWeek([]int{1, 2, 3, 4, 5}, 60, "09:00", "10:00")
	blocks, w = BuildBusyBlocks([]Project{p})
	if len(blocks) != 0 || len(w) == 0 {
		t.Fatal("overflow truncated or stretched")
	}
}

func TestTOTPGroupedInputAndRFCVector(t *testing.T) {
	// RFC 6238 SHA-1 vector uses 8 digits: 94287082 at t=59, truncated to six.
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	now := time.Unix(59, 0)
	if got := totpAt(secret, 1); got != "287082" {
		t.Fatal("RFC vector mismatch", got)
	}
	if totpStep(secret, "287 082", -1, now) != 1 || totpStep(secret, "287\u00a0082", -1, now) != 1 {
		t.Fatal("grouped TOTP rejected")
	}
	if totpStep(secret, "287 082", 1, now) >= 0 {
		t.Fatal("normalization bypassed replay guard")
	}
	if totpStep(secret, "287x082", -1, now) >= 0 {
		t.Fatal("invalid code accepted")
	}
}

func TestManualDurationPreservesEffortAndExtendsForCapacity(t *testing.T) {
	p, c, e := fixture()
	p.Tasks[0].DurationDays = 2
	if err := Calculate(&p, c, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	task := p.Tasks[0]
	if task.Start != "2026-10-23" || task.End != "2026-10-27" || task.DurationDays != 2 || task.Planned != 600 || task.Assignments[0].Daily != 240 {
		t.Fatal(task)
	}
	sum := 0
	for _, l := range p.Loads {
		if l.Date > task.End {
			t.Fatal("load outside manual range")
		}
		sum += l.Minutes
	}
	if sum != 600 || len(p.Warnings) != 0 {
		t.Fatal("full effort must fit extended range", sum, p.Warnings)
	}
	p.Tasks[0].DurationDays = 5
	if err := Calculate(&p, c, e, time.Now()); err != nil || p.Tasks[0].End != "2026-10-29" {
		t.Fatal(p.Tasks[0], err)
	}
	p.Mode = "calendar"
	p.Tasks[0].DurationDays = 2
	if err := Calculate(&p, c, e, time.Now()); err != nil || p.Tasks[0].End != "2026-10-27" {
		t.Fatal("calendar duration must extend to fit effort", err)
	}
	p.Tasks[0].Assignments = nil
	p.Tasks[0].DurationDays = 1
	if err := Calculate(&p, c, e, time.Now()); err != nil || p.Tasks[0].Start != "2026-10-23" || p.Tasks[0].End != "2026-10-23" || p.Tasks[0].Planned != 600 {
		t.Fatal("manual draft dates", err)
	}
	p.Tasks[0].DurationDays = 0
	if err := Calculate(&p, c, e, time.Now()); err != nil || p.Tasks[0].Start != "" {
		t.Fatal("automatic draft", err)
	}
}

func TestManualDurationPropagatesDependencies(t *testing.T) {
	p, c, e := fixture()
	p.Tasks[0].DurationDays = 4
	b := p.Tasks[0]
	b.ID = "b"
	b.DurationDays = 2
	p.Tasks = append(p.Tasks, b)
	p.Links = []Link{{"a", "b", "after", 0}}
	if err := Calculate(&p, c, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[0].End != "2026-10-28" || p.Tasks[1].Start != "2026-10-29" || p.Tasks[1].End != "2026-11-02" || p.Tasks[1].Planned != 600 {
		t.Fatal(p.Tasks)
	}
}

func TestManualDurationWithDraftPredecessor(t *testing.T) {
	p, c, e := fixture()
	p.Tasks[0].Assignments = nil
	b := p.Tasks[0]
	b.ID = "b"
	b.DurationDays = 3
	p.Tasks = append(p.Tasks, b)
	p.Links = []Link{{"a", "b", "after", 0}}
	if err := Calculate(&p, c, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[0].Start != "" || p.Tasks[1].Start != "2026-10-23" || p.Tasks[1].End != "2026-10-27" || !p.Tasks[1].Blocked || p.Tasks[1].Planned != 600 {
		t.Fatal("manual duration must remain editable while predecessor is a draft", p.Tasks)
	}
}
