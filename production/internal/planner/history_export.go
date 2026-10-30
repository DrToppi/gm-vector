package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const historyExportMaxRows = 100000
const historyExportMaxBytes = 64 << 20
const historyExportBuildTimeout = 20 * time.Second

func historyExportLimit() error {
	return appError{422, "history_export_limit", "History export exceeds its resource budget; narrow the filters"}
}
func historyExportError(ctx context.Context, err error) error {
	var pg *pgconn.PgError
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &pg) && pg.Code == "57014" {
		return appError{504, "history_export_timeout", "History export timed out; narrow the filters"}
	}
	return err
}

type historyBuffer struct {
	bytes.Buffer
	max int
}

func (b *historyBuffer) append(raw []byte) error {
	if len(raw) > b.max-b.Len() {
		return historyExportLimit()
	}
	_, err := b.Write(raw)
	return err
}
func (a *App) exportTimeHistory(w http.ResponseWriter, r *http.Request, u Principal) error {
	f, _, e := parseHistoryFilters(r.URL.Query(), false)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(r.Context(), historyExportBuildTimeout)
	defer cancel()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return historyExportError(ctx, e)
	}
	defer tx.Rollback(context.Background())
	if e = capacityLock(ctx, tx, true); e != nil {
		return historyExportError(ctx, e)
	}
	h, role, e := a.historyHeader(ctx, tx, u, r.PathValue("id"))
	if e != nil {
		return e
	}
	if role != "manager" {
		return denied()
	}
	select {
	case a.historyExportSlot <- struct{}{}:
		defer func() { <-a.historyExportSlot }()
	default:
		return appError{429, "history_export_busy", "Another history export is in progress; try again later"}
	}
	ids, e := historyScope(ctx, tx, h.ProjectID, f, &h)
	if e != nil {
		return historyExportError(ctx, e)
	}
	args := historyArguments(h.ProjectID, ids, f)
	if e = historyTotals(ctx, tx, args, &h); e != nil {
		return historyExportError(ctx, e)
	}
	if h.TotalCount > historyExportMaxRows {
		return historyExportLimit()
	}
	meta := struct {
		HistoryCommon
		Format        string `json:"format"`
		SchemaVersion int    `json:"schema_version"`
		GeneratedAt   string `json:"generated_at"`
	}{h, "gm-vector-time-history", 1, time.Now().UTC().Format(time.RFC3339Nano)}
	raw, e := json.Marshal(meta)
	if e != nil {
		return e
	}
	buffer := historyBuffer{max: historyExportMaxBytes}
	if e = buffer.append(raw[:len(raw)-1]); e != nil {
		return e
	}
	if e = buffer.append([]byte(`,"items":[`)); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, "SELECT "+historyColumns+historyJoins+historyWhere+" ORDER BY te.work_date DESC,te.id ASC", args...)
	if e != nil {
		return historyExportError(ctx, e)
	}
	var count int64
	for rows.Next() {
		if e = ctx.Err(); e != nil {
			rows.Close()
			return historyExportError(ctx, e)
		}
		v, err := scanHistoryItem(rows)
		if err != nil {
			rows.Close()
			return historyExportError(ctx, err)
		}
		count++
		if count > historyExportMaxRows {
			rows.Close()
			return historyExportLimit()
		}
		entry, err := json.Marshal(v)
		if err != nil {
			rows.Close()
			return err
		}
		if count > 1 {
			if e = buffer.append([]byte(",")); e != nil {
				rows.Close()
				return e
			}
		}
		if e = buffer.append(entry); e != nil {
			rows.Close()
			return e
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return historyExportError(ctx, e)
	}
	if count != h.TotalCount {
		return appError{409, "history_changed", "History changed; export again"}
	}
	if e = buffer.append([]byte("]}\n")); e != nil {
		return e
	}
	if e = tx.Rollback(ctx); e != nil {
		return historyExportError(ctx, e)
	}
	if e = r.Context().Err(); e != nil {
		return e
	}
	cancel()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gm-vector-time-history.json"`)
	w.Header().Set("Content-Length", strconv.Itoa(buffer.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buffer.Bytes()) // Slot remains held until this write/cancellation ends.
	return nil
}
