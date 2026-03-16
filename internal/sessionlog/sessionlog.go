package sessionlog

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	Input  = "input"
	Output = "output"
)

type headerRecord struct {
	Type         string   `json:"type"`
	Timestamp    string   `json:"ts"`
	WorkDir      string   `json:"work_dir"`
	InstanceName string   `json:"instance_name"`
	ClaudeArgs   []string `json:"claude_args"`
	Version      string   `json:"version"`
	PID          int      `json:"pid"`
}

type chunkRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"ts"`
	Dir       string `json:"dir"`
	Data      string `json:"data"`
	Size      int    `json:"size"`
}

// SessionOpts holds options for creating a new session logger.
type SessionOpts struct {
	WorkDir      string
	InstanceName string
	ClaudeArgs   []string
	Version      string
	PID          int
}

// SessionLogger writes session chunks to a JSONL file.
type SessionLogger struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	closed bool
}

// IsEnabled returns true if session logging is not disabled via environment variable.
func IsEnabled() bool {
	return os.Getenv("CLAUDER_NO_SESSION_LOG") == ""
}

// sanitizeFilename replaces characters that are unsafe in filenames.
func sanitizeFilename(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return r.Replace(name)
}

// NewSessionLogger creates a new session logger that writes to a JSONL file
// in the given sessions directory.
func NewSessionLogger(sessionsDir string, opts SessionOpts) (*SessionLogger, error) {
	if err := os.MkdirAll(sessionsDir, 0700); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	now := time.Now().UTC()
	namePart := sanitizeFilename(opts.InstanceName)
	filename := fmt.Sprintf("%s_%s.jsonl", now.Format("20060102T150405"), namePart)
	path := filepath.Join(sessionsDir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("create session file: %w", err)
	}

	w := bufio.NewWriterSize(f, 32*1024)

	header := headerRecord{
		Type:         "header",
		Timestamp:    now.Format(time.RFC3339),
		WorkDir:      opts.WorkDir,
		InstanceName: opts.InstanceName,
		ClaudeArgs:   opts.ClaudeArgs,
		Version:      opts.Version,
		PID:          opts.PID,
	}
	if header.ClaudeArgs == nil {
		header.ClaudeArgs = []string{}
	}

	data, err := json.Marshal(header)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("marshal header: %w", err)
	}
	w.Write(data)
	w.WriteByte('\n')
	w.Flush()

	return &SessionLogger{
		file:   f,
		writer: w,
	}, nil
}

// LogChunk writes a chunk record to the session log.
// Errors are logged to stderr but never returned — logging must not interrupt the PTY.
func (sl *SessionLogger) LogChunk(dir string, data []byte) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	if sl.closed {
		return
	}

	rec := chunkRecord{
		Type:      "chunk",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Dir:       dir,
		Data:      base64.StdEncoding.EncodeToString(data),
		Size:      len(data),
	}

	j, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[clauder] session log marshal error: %v\n", err)
		return
	}

	sl.writer.Write(j)
	sl.writer.WriteByte('\n')

	// Flush on input — input is infrequent and we want it persisted promptly
	if dir == Input {
		if err := sl.writer.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "[clauder] session log flush error: %v\n", err)
		}
	}
}

// Close flushes and closes the session log file.
func (sl *SessionLogger) Close() error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	if sl.closed {
		return nil
	}
	sl.closed = true

	if err := sl.writer.Flush(); err != nil {
		sl.file.Close()
		return err
	}
	return sl.file.Close()
}
