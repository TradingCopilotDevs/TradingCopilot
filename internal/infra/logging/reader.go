package logging

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	applogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/logging"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
)

type Reader struct {
	settings config.Settings
}

var lumberjackTimestampSuffix = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}`)

type queryCursor struct {
	File   string `json:"file"`
	Offset int64  `json:"offset"`
}

func NewReader(settings config.Settings) Reader {
	return Reader{settings: settings}
}

func (r Reader) ListFiles(ctx context.Context) ([]applogging.File, applogging.ConfigSummary, error) {
	files, err := r.files(ctx)
	if err != nil {
		return nil, applogging.ConfigSummary{}, err
	}
	return files, configSummary(r.settings), nil
}

func (r Reader) Query(ctx context.Context, query applogging.Query) (applogging.Result, error) {
	files, err := r.files(ctx)
	if err != nil {
		return applogging.Result{}, err
	}
	if query.File != "" {
		filtered := files[:0]
		for _, file := range files {
			if file.Name == query.File {
				filtered = append(filtered, file)
			}
		}
		files = filtered
	}
	cursor, err := decodeQueryCursor(query.Cursor)
	if err != nil {
		return applogging.Result{}, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	matches := make([]applogging.Entry, 0, limit+1)
	matchCursors := make([]queryCursor, 0, limit+1)
	nextCursor := ""
	cursorFileReached := cursor == nil
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return applogging.Result{}, err
		}
		var before *int64
		if cursor != nil && !cursorFileReached {
			if file.Name != cursor.File {
				continue
			}
			cursorFileReached = true
			before = &cursor.Offset
		}
		stop, err := readFileBackward(ctx, file.Path, file.Name, before, func(entry applogging.Entry, position int64) bool {
			if !matchEntry(entry, query) {
				return false
			}
			matches = append(matches, entry)
			matchCursors = append(matchCursors, queryCursor{File: file.Name, Offset: position})
			if len(matches) > limit {
				nextCursor = encodeQueryCursor(matchCursors[limit-1])
				return true
			}
			return false
		})
		if err != nil {
			continue
		}
		if stop {
			return applogging.Result{Entries: matches[:limit], NextCursor: nextCursor}, nil
		}
	}
	return applogging.Result{Entries: matches}, nil
}

func (r Reader) files(ctx context.Context) ([]applogging.File, error) {
	entries, err := os.ReadDir(r.settings.LogDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []applogging.File{}, nil
		}
		return nil, err
	}
	files := make([]applogging.File, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || !isLogFile(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		files = append(files, applogging.File{
			Name:      name,
			Path:      filepath.Join(r.settings.LogDir, name),
			SizeBytes: info.Size(),
			Modified:  info.ModTime().UTC(),
			Active:    activeLogFile(name, ""),
			Role:      roleFromFileName(name),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Modified.Equal(files[j].Modified) {
			return files[i].Name > files[j].Name
		}
		return files[i].Modified.After(files[j].Modified)
	})
	return files, nil
}

func readFile(path string, name string) ([]applogging.Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var entries []applogging.Entry
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		entry := entryFromRaw(raw, name, int64(lineNumber))
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

func readFileBackward(ctx context.Context, path string, name string, before *int64, visit func(applogging.Entry, int64) bool) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	const chunkSize int64 = 64 * 1024
	pos := info.Size()
	if pos <= 0 {
		return false, nil
	}
	var tail []byte
	for pos > 0 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		readStart := pos - chunkSize
		if readStart < 0 {
			readStart = 0
		}
		chunk := make([]byte, pos-readStart)
		if _, err := file.ReadAt(chunk, readStart); err != nil {
			return false, err
		}
		data := append(chunk, tail...)
		end := len(data)
		if pos == info.Size() && end > 0 && data[end-1] == '\n' {
			end--
		}
		for i := end - 1; i >= 0; i-- {
			if data[i] != '\n' {
				continue
			}
			lineStart := readStart + int64(i+1)
			if stop, ok := visitLogLine(data[i+1:end], name, lineStart, before, visit); stop || ok {
				if stop {
					return true, nil
				}
			}
			end = i
		}
		tail = data[:end]
		pos = readStart
	}
	if len(tail) > 0 {
		if stop, _ := visitLogLine(tail, name, 0, before, visit); stop {
			return true, nil
		}
	}
	return false, nil
}

func visitLogLine(line []byte, fileName string, position int64, before *int64, visit func(applogging.Entry, int64) bool) (bool, bool) {
	if before != nil && position >= *before {
		return false, true
	}
	if len(strings.TrimSpace(string(line))) == 0 {
		return false, true
	}
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return false, true
	}
	return visit(entryFromRaw(raw, fileName, position), position), true
}

func entryFromRaw(raw map[string]any, fileName string, position int64) applogging.Entry {
	timestamp := parseLogTime(stringValue(raw["time"]))
	role := stringValue(raw["role"])
	if role == "" {
		role = roleFromFileName(fileName)
	}
	source := stringValue(raw["logger"])
	if explicit := stringValue(raw["source"]); explicit != "" {
		source = explicit
	}
	fields := map[string]any{}
	for key, value := range raw {
		switch key {
		case "time", "level", "msg", "message", "role", "logger", "source", "caller", "stacktrace",
			"event", "group", "method", "path", "status", "durationMs", "noise":
			continue
		default:
			fields[key] = value
		}
	}
	rawText, _ := json.Marshal(raw)
	sum := sha1.Sum([]byte(fmt.Sprintf("%s:%d:%s", fileName, position, string(rawText))))
	message := stringValue(raw["msg"])
	if message == "" {
		message = stringValue(raw["message"])
	}
	return applogging.Entry{
		ID:         hex.EncodeToString(sum[:]),
		Time:       timestamp,
		Level:      stringValue(raw["level"]),
		Role:       role,
		Source:     source,
		Event:      stringValueOrDefault(raw["event"], Placeholder),
		Message:    message,
		Caller:     stringValue(raw["caller"]),
		Stacktrace: stringValue(raw["stacktrace"]),
		File:       fileName,
		Group:      stringValueOrDefault(raw["group"], Placeholder),
		Method:     stringValueOrDefault(raw["method"], Placeholder),
		Path:       stringValue(raw["path"]),
		Status:     stringValueOrDefault(raw["status"], Placeholder),
		DurationMS: intPtrValue(raw["durationMs"]),
		Noise:      boolValue(raw["noise"]),
		Fields:     RedactMap(fields),
		Raw:        RedactMap(raw),
	}
}

func decodeQueryCursor(value string) (*queryCursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("invalid page cursor")
	}
	var cursor queryCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return nil, errors.New("invalid page cursor")
	}
	if strings.TrimSpace(cursor.File) == "" || cursor.Offset < 0 {
		return nil, errors.New("invalid page cursor")
	}
	return &cursor, nil
}

func encodeQueryCursor(cursor queryCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func matchEntry(entry applogging.Entry, query applogging.Query) bool {
	if entry.Noise && !query.IncludeNoise {
		return false
	}
	if query.From != nil && !entry.Time.IsZero() && entry.Time.Before(*query.From) {
		return false
	}
	if query.To != nil && !entry.Time.IsZero() && entry.Time.After(*query.To) {
		return false
	}
	if query.Level != "" && !strings.EqualFold(entry.Level, query.Level) {
		return false
	}
	if query.Role != "" && !strings.EqualFold(entry.Role, query.Role) {
		return false
	}
	if query.Source != "" && !strings.Contains(strings.ToLower(entry.Source), strings.ToLower(query.Source)) {
		return false
	}
	if query.Event != "" && !strings.EqualFold(entry.Event, query.Event) {
		return false
	}
	if query.Group != "" && !strings.EqualFold(entry.Group, query.Group) {
		return false
	}
	if query.Method != "" && !strings.EqualFold(entry.Method, query.Method) {
		return false
	}
	if query.Path != "" && !strings.Contains(strings.ToLower(entry.Path), strings.ToLower(query.Path)) {
		return false
	}
	if query.Status != "" && !strings.EqualFold(entry.Status, query.Status) {
		return false
	}
	if query.SlowOnly && (entry.DurationMS == nil || *entry.DurationMS < 1000) {
		return false
	}
	if query.Q != "" {
		haystack := strings.ToLower(entry.Message + " " + entry.Caller + " " + entry.File + " " + entry.Event + " " + entry.Group + " " + entry.Method + " " + entry.Status + " " + entry.Path + " " + fmt.Sprint(entry.Fields))
		if !strings.Contains(haystack, strings.ToLower(query.Q)) {
			return false
		}
	}
	return true
}

func configSummary(settings config.Settings) applogging.ConfigSummary {
	return applogging.ConfigSummary{
		Dir:                 settings.LogDir,
		Level:               settings.LogLevel,
		RotationMode:        settings.LogRotationMode,
		RotationSizeMB:      settings.LogRotationSizeMB,
		RotationTotalSizeMB: settings.LogRotationTotalSizeMB,
		RotationMaxAgeDays:  settings.LogRotationMaxAgeDays,
	}
}

func roleFromFileName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if before, _, ok := strings.Cut(base, "."); ok {
		return before
	}
	if loc := lumberjackTimestampSuffix.FindStringIndex(base); loc != nil {
		return base[:loc[0]]
	}
	return base
}

func parseLogTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z0700"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func stringValueOrDefault(value any, fallback string) string {
	if text := strings.TrimSpace(stringValue(value)); text != "" {
		return text
	}
	return fallback
}

func intPtrValue(value any) *int {
	parsed, ok := intValue(value)
	if !ok {
		return nil
	}
	return &parsed
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}
