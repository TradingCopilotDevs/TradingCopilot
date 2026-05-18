package httptransport

import (
	"net/http"
	"strconv"
	"time"

	applogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/logging"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http/jsonapi"
)

func (s *Server) listLogFiles(w http.ResponseWriter, r *http.Request) {
	files, cfg, err := s.loggingUsecase.ListFiles(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "log-files-load-failed", "Log files load failed", err.Error(), "")
		return
	}
	resources := make([]jsonapi.Resource, 0, len(files))
	for _, file := range files {
		resources = append(resources, logFileResource(file))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{
		Data: resources,
		Meta: map[string]any{
			"config": cfg,
		},
	})
}

func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	query, ok := logQueryFromRequest(w, r)
	if !ok {
		return
	}
	result, err := s.loggingUsecase.Query(r.Context(), query)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "logs-query-failed", "Logs query failed", err.Error(), "")
		return
	}
	resources := make([]jsonapi.Resource, 0, len(result.Entries))
	for _, entry := range result.Entries {
		resources = append(resources, logEntryResource(entry))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{
		Data:  resources,
		Links: jsonapi.PageLinks(r, result.NextCursor),
		Meta:  jsonapi.PageMeta(len(resources), result.NextCursor),
	})
}

func logQueryFromRequest(w http.ResponseWriter, r *http.Request) (applogging.Query, bool) {
	values := r.URL.Query()
	query := applogging.Query{
		Level:        values.Get("level"),
		Role:         values.Get("role"),
		Source:       values.Get("source"),
		Event:        values.Get("event"),
		Group:        values.Get("group"),
		Method:       values.Get("method"),
		Status:       values.Get("status"),
		Path:         values.Get("path"),
		Q:            values.Get("q"),
		File:         values.Get("file"),
		Cursor:       values.Get("page[cursor]"),
		Limit:        100,
		SlowOnly:     boolQuery(values.Get("slowOnly")),
		IncludeNoise: boolQuery(values.Get("includeNoise")),
	}
	if raw := values.Get("page[limit]"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeJSONAPIError(w, http.StatusBadRequest, "invalid-page-limit", "Invalid page limit", "page[limit] must be an integer", "page[limit]")
			return applogging.Query{}, false
		}
		query.Limit = parsed
	}
	if raw := values.Get("from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSONAPIError(w, http.StatusBadRequest, "invalid-from", "Invalid from", "from must be RFC3339", "from")
			return applogging.Query{}, false
		}
		parsed = parsed.UTC()
		query.From = &parsed
	}
	if raw := values.Get("to"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSONAPIError(w, http.StatusBadRequest, "invalid-to", "Invalid to", "to must be RFC3339", "to")
			return applogging.Query{}, false
		}
		parsed = parsed.UTC()
		query.To = &parsed
	}
	return query, true
}

func boolQuery(value string) bool {
	parsed, _ := strconv.ParseBool(value)
	return parsed
}

func logFileResource(file applogging.File) jsonapi.Resource {
	return jsonapi.NewResource("log-files", file.Name, map[string]any{
		"name":      file.Name,
		"sizeBytes": file.SizeBytes,
		"modified":  file.Modified.UTC().Format(time.RFC3339Nano),
		"active":    file.Active,
		"role":      file.Role,
	})
}

func logEntryResource(entry applogging.Entry) jsonapi.Resource {
	attrs := map[string]any{
		"time":       nil,
		"level":      entry.Level,
		"role":       entry.Role,
		"source":     entry.Source,
		"event":      entry.Event,
		"message":    entry.Message,
		"caller":     entry.Caller,
		"file":       entry.File,
		"group":      entry.Group,
		"method":     entry.Method,
		"path":       entry.Path,
		"status":     entry.Status,
		"durationMs": entry.DurationMS,
		"noise":      entry.Noise,
		"fields":     entry.Fields,
		"raw":        entry.Raw,
	}
	if !entry.Time.IsZero() {
		attrs["time"] = entry.Time.UTC().Format(time.RFC3339Nano)
	}
	if entry.Stacktrace != "" {
		attrs["stacktrace"] = entry.Stacktrace
	}
	return jsonapi.NewResource("log-entries", entry.ID, attrs)
}
