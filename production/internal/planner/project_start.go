package planner

// shiftProjectStart moves unchanged task anchors when the project's start is
// edited. It runs at the write/preview boundary, never during ordinary reads.
// Comparing with the persisted revision keeps preview -> save idempotent and
// preserves explicit per-task date edits in the same request.
func shiftProjectStart(old Project, p *Project) error {
	if old.Start == p.Start {
		return nil
	}
	before, err := date(old.Start)
	if err != nil {
		return err
	}
	after, err := date(p.Start)
	if err != nil {
		return err
	}
	days := int((after.Unix() - before.Unix()) / 86400)
	shift := func(value string) (string, error) {
		if value == "" {
			return "", nil
		}
		d, err := date(value)
		if err != nil {
			return "", err
		}
		return fmtDate(d.AddDate(0, 0, days)), nil
	}
	previous := make(map[string]Task, len(old.Tasks))
	parents := make(map[string]bool)
	for _, t := range old.Tasks {
		previous[t.ID] = t
	}
	for _, t := range p.Tasks {
		parents[t.Parent] = true
	}
	p.Tasks = append([]Task(nil), p.Tasks...)
	for i := range p.Tasks {
		t := &p.Tasks[i]
		b, exists := previous[t.ID]
		if !exists || parents[t.ID] {
			continue
		}
		manualDates := t.Status == "done" || t.Responsibility == "customer"
		explicitRange := manualDates && (t.Start != b.Start || t.End != b.End)
		if t.RequestedStart == b.RequestedStart && !explicitRange {
			if t.RequestedStart, err = shift(t.RequestedStart); err != nil {
				return err
			}
		}
		if manualDates && !explicitRange {
			start, err := shift(t.Start)
			if err != nil {
				return err
			}
			end, err := shift(t.End)
			if err != nil {
				return err
			}
			t.Start, t.End = start, end
		}
	}
	return nil
}
