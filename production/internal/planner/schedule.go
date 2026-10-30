package planner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

type graph struct {
	index    map[string]int
	children map[string][]string
	leaves   map[string][]string
	order    []string
	incoming map[string][]Link
}

func validateProject(p *Project) (graph, error) {
	return validateProjectBudget(p, newCalculationBudget(context.Background()))
}
func validateProjectBudget(p *Project, budget *calculationBudget) (graph, error) {
	g := graph{index: map[string]int{}, children: map[string][]string{}, leaves: map[string][]string{}, incoming: map[string][]Link{}}
	if checkText(p.Title, 200, true) != nil || checkText(p.Brief, 4000, false) != nil {
		return g, errors.New("проверьте название и описание проекта")
	}
	if _, e := date(p.Start); e != nil {
		return g, e
	}
	if e := validateCalendarVersions(p); e != nil {
		return g, e
	}
	if e := budget.take(1); e != nil {
		return g, e
	}
	if !contains([]string{"working", "calendar"}, p.Mode) {
		return g, errors.New("неверный режим длительности")
	}
	if len(p.Tasks) > 300 || len(p.Links) > 1500 {
		return g, errors.New("лимит: 300 задач и 1500 связей")
	}
	for i, t := range p.Tasks {
		if t.CustomerEmail != "" && !validEmail(t.CustomerEmail) {
			return g, errors.New("Укажите один корректный адрес электронной почты заказчика")
		}
		if !validID.MatchString(t.ID) {
			return g, errors.New("неверный ID задачи")
		}
		if _, ok := g.index[t.ID]; ok {
			return g, errors.New("повтор ID")
		}
		g.index[t.ID] = i
		if checkText(t.Title, 500, true) != nil || checkText(t.Description, 20000, false) != nil || checkText(t.Result, 20000, false) != nil || checkText(t.Brief, 4000, false) != nil || checkText(t.CustomerOwner, 300, false) != nil || len(t.OwnerLegacy) > 300 {
			return g, errors.New("неверные текстовые поля задачи")
		}
		if !contains([]string{"todo", "doing", "done"}, t.Status) || !contains([]string{"internal", "customer"}, t.Responsibility) || t.Planned < 0 || t.Planned > 60000000 || t.DurationDays < 0 || t.DurationDays > maxDurationDays {
			return g, errors.New("неверный статус, сторона или часы")
		}
		for _, s := range []string{t.RequestedStart, t.Start, t.End, t.CustomerDue, t.LegacyStart, t.LegacyEnd} {
			if s != "" {
				if _, e := date(s); e != nil {
					return g, e
				}
			}
		}
		if t.End != "" && t.Start != "" && t.End < t.Start {
			return g, errors.New("окончание раньше начала")
		}
		if t.Parent != "" {
			g.children[t.Parent] = append(g.children[t.Parent], t.ID)
		}
		seen := map[string]bool{}
		for _, a := range t.Assignments {
			if seen[a.EmployeeID] || a.Daily < 1 || a.Daily > 1440 {
				return g, errors.New("неверное назначение")
			}
			seen[a.EmployeeID] = true
		}
	}
	for _, t := range p.Tasks {
		id := t.ID
		seen := map[string]bool{}
		for depth := 0; id != ""; depth++ {
			if depth > 32 || seen[id] {
				return g, errors.New("цикл или глубина вложенности >32")
			}
			seen[id] = true
			i, ok := g.index[id]
			if !ok {
				return g, errors.New("неизвестный родитель")
			}
			id = p.Tasks[i].Parent
		}
	}
	var leaves func(string) []string
	leaves = func(id string) []string {
		if x, ok := g.leaves[id]; ok {
			return x
		}
		result := []string{}
		if len(g.children[id]) == 0 {
			result = append(result, id)
		} else {
			for _, c := range g.children[id] {
				result = append(result, leaves(c)...)
			}
		}
		g.leaves[id] = result
		return result
	}
	indeg := map[string]int{}
	edges := map[string]map[string]bool{}
	for _, t := range p.Tasks {
		leaves(t.ID)
		if len(g.children[t.ID]) == 0 {
			indeg[t.ID] = 0
			edges[t.ID] = map[string]bool{}
		}
	}
	seenLinks := map[string]bool{}
	for _, l := range p.Links {
		_, a := g.index[l.From]
		_, b := g.index[l.To]
		if !a || !b || l.From == l.To || !contains([]string{"after", "together", "delay"}, l.Mode) || l.Days < 0 || l.Days > 3650 || (l.Mode != "delay" && l.Days != 0) || seenLinks[l.From+"/"+l.To] {
			return g, errors.New("неверная или повторная связь")
		}
		seenLinks[l.From+"/"+l.To] = true
		for _, to := range leaves(l.To) {
			g.incoming[to] = append(g.incoming[to], l)
			for _, from := range leaves(l.From) {
				if e := budget.take(1); e != nil {
					return g, e
				}
				if from == to {
					return g, errors.New("контейнер связан со своим потомком")
				}
				if !edges[from][to] {
					edges[from][to] = true
					indeg[to]++
				}
			}
		}
	}
	queue := []string{}
	for id, n := range indeg {
		if n == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		g.order = append(g.order, id)
		for to := range edges[id] {
			indeg[to]--
			if indeg[to] == 0 {
				queue = append(queue, to)
			}
		}
		sort.Strings(queue)
	}
	if len(g.order) != len(indeg) {
		return g, errors.New("цикл зависимостей")
	}
	participants := map[string]bool{}
	for _, v := range p.People {
		if participants[v.EmployeeID] {
			return g, errors.New("повтор сотрудника")
		}
		participants[v.EmployeeID] = true
		if len(v.Week) > 7 {
			return g, errors.New("неверный недельный график")
		}
		for key, day := range v.Week {
			weekday, err := strconv.Atoi(key)
			if err != nil || weekday < 0 || weekday > 6 || strconv.Itoa(weekday) != key {
				return g, errors.New("неверный день недельного графика")
			}
			if _, err := activityCapacity(day); err != nil {
				return g, err
			}
		}
		if len(v.Exceptions) > 2000 {
			return g, errors.New("слишком много исключений")
		}
		for d, x := range v.Exceptions {
			if _, e := date(d); e != nil {
				return g, e
			}
			// Legacy exceptions have no quota. Keep them readable until the
			// participant is configured; they never create capacity meanwhile.
			if len(v.Week) > 0 {
				if _, err := activityCapacity(x); err != nil {
					return g, err
				}
			}
		}
	}
	for _, t := range p.Tasks {
		for _, a := range t.Assignments {
			if !participants[a.EmployeeID] {
				return g, errors.New("исполнитель не добавлен в проект")
			}
		}
	}
	return g, nil
}
func windowMinutes(start, end string) (int, error) {
	a, e := time.Parse("15:04", start)
	if e != nil {
		return 0, errors.New("неверное начало окна")
	}
	b, e := time.Parse("15:04", end)
	if e != nil || !b.After(a) {
		return 0, errors.New("окно должно завершаться после начала")
	}
	return int(b.Sub(a) / time.Minute), nil
}

// participationActivity resolves one project's explicit daily schedule. An
// exception replaces the weekly entry; legacy fields never imply a new quota.
func participationActivity(p Participation, d time.Time) Activity {
	if len(p.Week) == 0 {
		return Activity{}
	}
	if x, ok := p.Exceptions[fmtDate(d)]; ok {
		return x
	}
	return p.Week[strconv.Itoa(int(d.Weekday()))]
}
func active(p Participation, d time.Time) bool {
	return participationActivity(p, d).Active
}
func activityCapacity(v Activity) (int, error) {
	if !v.Active {
		return 0, nil
	}
	size, err := windowMinutes(v.Start, v.End)
	if err != nil {
		return 0, err
	}
	if v.Daily <= 0 || v.Daily > size {
		return 0, errors.New("активный день: квота должна быть больше нуля и помещаться в окно")
	}
	return v.Daily, nil
}
func employeeDayLimit(employee Employee, day Day) int {
	limit := employee.Daily
	if day.Shortened {
		limit -= 60
	}
	if limit < 0 {
		return 0
	}
	return limit
}

// invalidateSchedule hides non-publishable computed dates but preserves facts
// and manually owned customer ranges. The state is derived, never an input grant.
func invalidateSchedule(p *Project, state, warning string) {
	p.CalculationState = state
	p.Loads = []Load{}
	parents := map[string]bool{}
	for _, task := range p.Tasks {
		parents[task.Parent] = true
	}
	for i := range p.Tasks {
		task := &p.Tasks[i]
		if parents[task.ID] {
			task.Summary = true
		}
		if task.Status != "done" && (task.Responsibility != "customer" || parents[task.ID]) {
			task.Start, task.End = "", ""
		}
	}
	if warning != "" && !contains(p.Warnings, warning) {
		p.Warnings = append(p.Warnings, warning)
	}
	sort.Strings(p.Warnings)
}

func Calculate(p *Project, cal Calendar, employees map[string]Employee, now time.Time) error {
	return calculate(p, cal, employees, now, newCalculationBudget(context.Background()))
}
func calculate(p *Project, cal Calendar, employees map[string]Employee, now time.Time, budget *calculationBudget) (result error) {
	customerDates := customerInputRanges(p)
	// The cleanup must cover graph validation/budget failure as well as allocation.
	defer func() {
		if result != nil {
			invalidateSchedule(p, "blocked_capacity", result.Error())
			p.ProvisionalYears = nil
			restoreCustomerInputRanges(p, customerDates)
		}
	}()
	g, e := validateProjectBudget(p, budget)
	if e != nil {
		return e
	}
	p.Warnings = nil
	p.Loads = []Load{}
	p.CalculationState = "ready"
	p.ProvisionalYears = nil
	provisional := map[int]bool{}
	defer func() {
		if result == nil {
			for year := range provisional {
				p.ProvisionalYears = append(p.ProvisionalYears, year)
			}
			sort.Ints(p.ProvisionalYears)
			for _, year := range p.ProvisionalYears {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%d: provisional Monday–Friday calendar; verified holidays and transfers are not available", year))
			}
			sort.Strings(p.Warnings)
			if e := checkProjectResultSize(p, budget); e != nil {
				result = e
			}
		}
	}()
	ps := map[string]Participation{}
	for _, v := range p.People {
		if _, ok := employees[v.EmployeeID]; !ok {
			return errors.New("неизвестный сотрудник")
		}
		ps[v.EmployeeID] = v
	}
	for _, v := range p.People {
		if len(v.Week) == 0 {
			invalidateSchedule(p, "needs_participation", "Участие сотрудника не настроено: задайте квоты и окна по дням недели")
			rollupProject(p, g)
			invalidateSchedule(p, "needs_participation", "")
			return nil
		}
	}
	usedCapacity := map[string]int{}
	// Without any recurring day or future active exception, a valid graph is
	// a draft with no capacity. Index exceptions once rather than scanning up
	// to 2000 dates per participant on every allocated day of every task.
	type futureAvailability struct {
		recurring     bool
		lastException string
	}
	future := map[string]futureAvailability{}
	for id, person := range ps {
		availability := futureAvailability{}
		for _, day := range person.Week {
			availability.recurring = availability.recurring || day.Active
		}
		for day, activity := range person.Exceptions {
			if activity.Active && day > availability.lastException {
				availability.lastException = day
			}
		}
		future[id] = availability
	}
	hasFutureCapacity := func(t Task, from time.Time) (bool, error) {
		for _, assignment := range t.Assignments {
			if e := budget.take(1); e != nil {
				return false, e
			}
			if employees[assignment.EmployeeID].Daily <= 0 {
				continue
			}
			availability := future[assignment.EmployeeID]
			if availability.recurring || availability.lastException >= fmtDate(from) {
				return true, nil
			}
		}
		return false, nil
	}
	unavailable := func(title string) error {
		rollupProject(p, g)
		invalidateSchedule(p, "blocked_capacity", title+": недостаточно доступного времени; настройте квоты и дни участия")
		return nil
	}
	working := func(d time.Time) (bool, error) {
		if e := budget.take(1); e != nil {
			return false, e
		}
		v, preliminary, e := cal.schedulingDay(d)
		if e != nil {
			return false, e
		}
		if preliminary {
			provisional[d.Year()] = true
		}
		return v.Working, nil
	}
	nextWorking := func(d time.Time) (time.Time, error) {
		for d.Year() <= lastCalendarYear {
			ok, e := working(d)
			if e != nil {
				return d, e
			}
			if ok {
				return d, nil
			}
			d = d.AddDate(0, 0, 1)
		}
		return d, bad("No working date within supported calendar coverage")
	}
	for _, id := range g.order {
		t := &p.Tasks[g.index[id]]
		t.Summary = false
		t.Blocked = false
		start, _ := date(p.Start)
		if t.RequestedStart != "" {
			start, _ = date(t.RequestedStart)
		}
		missing := false
		for _, l := range g.incoming[id] {
			anchor := ""
			ready := true
			for _, from := range g.leaves[l.From] {
				pred := p.Tasks[g.index[from]]
				candidate := pred.End
				if l.Mode == "together" {
					candidate = pred.Start
					if pred.Status == "todo" {
						ready = false
					}
				} else if pred.Status != "done" {
					ready = false
				}
				if candidate == "" {
					missing = true
					continue
				}
				if anchor == "" || l.Mode == "together" && candidate < anchor || l.Mode != "together" && candidate > anchor {
					anchor = candidate
				}
			}
			if !ready {
				t.Blocked = true
			}
			if anchor == "" {
				continue
			}
			d, _ := date(anchor)
			if l.Mode != "together" {
				d = d.AddDate(0, 0, 1)
			}
			if l.Mode == "delay" {
				for n := 0; n < l.Days; {
					if p.Mode == "calendar" {
						if _, err := working(d); err != nil {
							return err
						}
						d = d.AddDate(0, 0, 1)
						n++
					} else {
						ok, err := working(d)
						if err != nil {
							return err
						}
						d = d.AddDate(0, 0, 1)
						if ok {
							n++
						}
					}
				}
			}
			d, e = nextWorking(d)
			if e != nil {
				return e
			}
			if d.After(start) {
				start = d
			}
			if fmtDate(now) < fmtDate(d) && l.Mode != "together" {
				t.Blocked = true
			}
		}
		if t.Status == "done" {
			continue
		}
		if missing {
			t.Blocked = true
			p.Warnings = append(p.Warnings, t.Title+": предшественник ещё не рассчитан")
			if t.DurationDays == 0 {
				t.Start = ""
				t.End = ""
				continue
			}
			p.Warnings = append(p.Warnings, t.Title+": ручной срок предварительный до расчёта предшественника")
		}
		if t.Planned > 0 && len(t.Assignments) > 0 {
			available, err := hasFutureCapacity(*t, start)
			if err != nil {
				return err
			}
			if !available {
				return unavailable(t.Title)
			}
		}
		var manualEnd time.Time
		if t.DurationDays > 0 {
			start, e = nextWorking(start)
			if e != nil {
				return e
			}
			manualEnd = start
			for n := 1; n < t.DurationDays; n++ {
				manualEnd = manualEnd.AddDate(0, 0, 1)
				if p.Mode == "calendar" {
					if _, err := working(manualEnd); err != nil {
						return err
					}
				}
				if p.Mode == "working" {
					manualEnd, e = nextWorking(manualEnd)
					if e != nil {
						return e
					}
				}
			}
			manualEnd, e = nextWorking(manualEnd)
			if e != nil {
				return e
			}
			t.Start = fmtDate(start)
			t.End = fmtDate(manualEnd)
			if t.Planned == 0 || len(t.Assignments) == 0 {
				if t.Responsibility != "customer" {
					p.Warnings = append(p.Warnings, t.Title+": срок задан вручную; для распределения трудозатрат укажите часы и исполнителей")
				}
				continue
			}
		}
		if t.DurationDays == 0 && t.Responsibility == "customer" && len(t.Assignments) == 0 {
			if t.Start != "" && t.End != "" {
				old, _ := date(t.Start)
				end, _ := date(t.End)
				span := int(end.Sub(old).Hours() / 24)
				if start.After(old) {
					t.Start = fmtDate(start)
					t.End = fmtDate(start.AddDate(0, 0, span))
				}
			}
			continue
		}
		if t.Planned == 0 || len(t.Assignments) == 0 {
			t.Start = ""
			t.End = ""
			p.Warnings = append(p.Warnings, t.Title+": черновик — укажите часы и исполнителей")
			continue
		}
		t.Start = ""
		t.End = ""
		remaining := t.Planned
		assignments := append([]Assignment(nil), t.Assignments...)
		sort.Slice(assignments, func(i, j int) bool { return assignments[i].EmployeeID < assignments[j].EmployeeID })
		for n := 0; remaining > 0; n++ {
			d := start.AddDate(0, 0, n)
			availableFuture, err := hasFutureCapacity(*t, d)
			if err != nil {
				return err
			}
			if !availableFuture {
				return unavailable(t.Title)
			}
			ok, err := working(d)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			available := []Assignment{}
			total := 0
			for _, a := range assignments {
				if e := budget.take(1); e != nil {
					return e
				}
				quota, err := activityCapacity(participationActivity(ps[a.EmployeeID], d))
				if err != nil {
					return err
				}
				limit := employeeDayLimit(employees[a.EmployeeID], cal.Days[fmtDate(d)])
				if quota > limit {
					quota = limit
				}
				quota -= usedCapacity[a.EmployeeID+"/"+fmtDate(d)]
				if quota > a.Daily {
					quota = a.Daily
				}
				if quota > 0 {
					available = append(available, Assignment{EmployeeID: a.EmployeeID, Daily: quota})
					total += quota
				}
			}
			if total == 0 {
				continue
			}
			work := total
			if work > remaining {
				work = remaining
			}
			shares := make([]int, len(available))
			used := 0
			for i, a := range available {
				shares[i] = work * a.Daily / total
				used += shares[i]
			}
			for i := 0; used < work; i = (i + 1) % len(shares) {
				shares[i]++
				used++
			}
			if t.Start == "" {
				t.Start = fmtDate(d)
			}
			t.End = fmtDate(d)
			remaining -= work
			for i, a := range available {
				if shares[i] > 0 {
					if len(p.Loads) >= budget.maxLoads {
						return budgetExceeded()
					}
					p.Loads = append(p.Loads, Load{a.EmployeeID, t.ID, fmtDate(d), shares[i]})
					usedCapacity[a.EmployeeID+"/"+fmtDate(d)] += shares[i]
				}
			}
		}
		if remaining > 0 {
			return unavailable(t.Title)
		}
		if !manualEnd.IsZero() {
			t.Start = fmtDate(start)
			if t.End < fmtDate(manualEnd) {
				t.End = fmtDate(manualEnd)
			}
			// DurationDays is the user's lower bound, not the derived span.
			// Keeping it lets a later capacity increase shorten the end again.
		}
	}
	rollupProject(p, g)
	sort.Strings(p.Warnings)
	return nil
}

func rollupProject(p *Project, g graph) {
	var rollup func(string)
	rollup = func(id string) {
		t := &p.Tasks[g.index[id]]
		if len(g.children[id]) == 0 {
			return
		}
		t.Summary = true
		t.Start = ""
		t.End = ""
		t.Planned = 0
		t.Actual = 0
		t.Blocked = false
		allDone := true
		anyDoing := false
		for _, child := range g.children[id] {
			rollup(child)
			c := p.Tasks[g.index[child]]
			if c.Start != "" && (t.Start == "" || c.Start < t.Start) {
				t.Start = c.Start
			}
			if c.End > t.End {
				t.End = c.End
			}
			t.Planned += c.Planned
			t.Actual += c.Actual
			t.Blocked = t.Blocked || c.Blocked
			allDone = allDone && c.Status == "done"
			anyDoing = anyDoing || c.Status == "doing"
		}
		t.Status = "todo"
		if allDone {
			t.Status = "done"
		} else if anyDoing {
			t.Status = "doing"
		}
	}
	for _, t := range p.Tasks {
		if t.Parent == "" {
			rollup(t.ID)
		}
	}
}
