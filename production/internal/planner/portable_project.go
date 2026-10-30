package planner

import (
	"encoding/json"
	"net/http"
	"time"
)

type PortableMetadata struct {
	PlanningTimezone string `json:"planning_timezone"`
	GeneratedAt      string `json:"generated_at"`
}
type PortableProject struct {
	Format        string           `json:"format"`
	SchemaVersion int              `json:"schema_version"`
	Metadata      PortableMetadata `json:"metadata"`
	Project       Project          `json:"project"`
}
type TimezoneAcknowledgement struct {
	Source string `json:"source"`
	Target string `json:"target"`
}
type PortableImportRequest struct {
	Document        PortableProject          `json:"document"`
	Preview         *bool                    `json:"preview"`
	Acknowledgement *TimezoneAcknowledgement `json:"timezone_acknowledgement,omitempty"`
}

func (a *App) exportPortableProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = capacityLock(ctx, tx, true); e != nil {
		return e
	}
	_, role, e := a.historyHeader(ctx, tx, u, r.PathValue("id"))
	if e != nil {
		return e
	}
	if role != "manager" {
		return denied()
	}
	p, e := readProject(ctx, tx, r.PathValue("id"))
	if e != nil {
		return e
	}
	if e = a.prepareProject(ctx, tx, &p); e != nil {
		return e
	}
	exported := snapshotProject(p)
	exported.CalculationState = p.CalculationState
	doc := PortableProject{Format: "gm-vector-project", SchemaVersion: 1, Metadata: PortableMetadata{a.planningTimezone(), time.Now().UTC().Format(time.RFC3339Nano)}, Project: exported}
	raw, e := json.Marshal(doc)
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, p.ID, "project.exported", requestID(r), map[string]string{"format": "portable"}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	w.Header().Set("Content-Disposition", `attachment; filename="gm-vector-project-portable.json"`)
	writeMaterializedJSON(w, raw)
	return nil
}
func (a *App) importPortableProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin || u.Token {
		return denied()
	}
	var in PortableImportRequest
	if e := decode(r, &in); e != nil {
		return e
	}
	if in.Preview == nil || in.Document.Format != "gm-vector-project" || in.Document.SchemaVersion != 1 || in.Document.Metadata.PlanningTimezone == "" {
		return bad("Invalid portable project document")
	}
	source, _, e := resolvePlanningTimezone(in.Document.Metadata.PlanningTimezone)
	if e != nil {
		return bad(e.Error())
	}
	if _, e = time.Parse(time.RFC3339Nano, in.Document.Metadata.GeneratedAt); e != nil {
		return bad("Invalid portable generation instant")
	}
	target := a.planningTimezone()
	mismatch := source != target
	if in.Acknowledgement != nil && (in.Acknowledgement.Source != source || in.Acknowledgement.Target != target) {
		return appError{422, "timezone_acknowledgement_invalid", "Planning timezone acknowledgement no longer matches source and target"}
	}
	if mismatch && !*in.Preview && in.Acknowledgement == nil {
		return appError{422, "timezone_acknowledgement_required", "Confirm the source and current target planning timezone before import"}
	}
	p, e := a.normalizedImport(r.Context(), in.Document.Project)
	if e != nil {
		return e
	}
	if *in.Preview {
		jsonResponse(w, 200, map[string]any{"project": p, "message": "A new project is created without identities or external bindings; date labels remain unchanged", "source_planning_timezone": source, "target_planning_timezone": target, "timezone_acknowledgement_required": mismatch})
		return nil
	}
	if e = a.Import(r.Context(), p, u.ID, requestID(r)); e != nil {
		return e
	}
	jsonResponse(w, 201, p)
	return nil
}
