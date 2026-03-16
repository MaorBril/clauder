package sessionlog

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIsEnabled(t *testing.T) {
	// Save and restore env
	orig := os.Getenv("CLAUDER_NO_SESSION_LOG")
	defer os.Setenv("CLAUDER_NO_SESSION_LOG", orig)

	os.Unsetenv("CLAUDER_NO_SESSION_LOG")
	if !IsEnabled() {
		t.Error("expected enabled when env var is unset")
	}

	os.Setenv("CLAUDER_NO_SESSION_LOG", "1")
	if IsEnabled() {
		t.Error("expected disabled when env var is set")
	}
}

func TestNewSessionLogger(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")

	sl, err := NewSessionLogger(sessDir, SessionOpts{
		WorkDir:      "/test/dir",
		InstanceName: "backend",
		ClaudeArgs:   []string{"--resume"},
		Version:      "1.0.0",
		PID:          12345,
	})
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer sl.Close()

	// Check that directory was created
	info, err := os.Stat(sessDir)
	if err != nil {
		t.Fatalf("sessions dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("sessions path is not a directory")
	}

	// Find the created file
	entries, _ := os.ReadDir(sessDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	// Read and verify header
	path := filepath.Join(sessDir, entries[0].Name())
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open session file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected header line")
	}

	var header headerRecord
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}

	if header.Type != "header" {
		t.Errorf("type = %q, want %q", header.Type, "header")
	}
	if header.WorkDir != "/test/dir" {
		t.Errorf("work_dir = %q, want %q", header.WorkDir, "/test/dir")
	}
	if header.InstanceName != "backend" {
		t.Errorf("instance_name = %q, want %q", header.InstanceName, "backend")
	}
	if header.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", header.Version, "1.0.0")
	}
	if header.PID != 12345 {
		t.Errorf("pid = %d, want %d", header.PID, 12345)
	}
}

func TestLogChunk(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")

	sl, err := NewSessionLogger(sessDir, SessionOpts{
		WorkDir:      "/test",
		InstanceName: "test",
		Version:      "1.0.0",
		PID:          1,
	})
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}

	sl.LogChunk(Input, []byte("hello"))
	sl.LogChunk(Output, []byte("world\x1b[31mred\x1b[0m"))
	sl.Close()

	// Read back the file
	entries, _ := os.ReadDir(sessDir)
	path := filepath.Join(sessDir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Skip header
	scanner.Scan()

	// Check input chunk
	if !scanner.Scan() {
		t.Fatal("expected input chunk line")
	}
	var chunk1 chunkRecord
	json.Unmarshal(scanner.Bytes(), &chunk1)
	if chunk1.Type != "chunk" {
		t.Errorf("chunk type = %q, want %q", chunk1.Type, "chunk")
	}
	if chunk1.Dir != Input {
		t.Errorf("dir = %q, want %q", chunk1.Dir, Input)
	}
	decoded, _ := base64.StdEncoding.DecodeString(chunk1.Data)
	if string(decoded) != "hello" {
		t.Errorf("data = %q, want %q", string(decoded), "hello")
	}
	if chunk1.Size != 5 {
		t.Errorf("size = %d, want %d", chunk1.Size, 5)
	}

	// Check output chunk (with ANSI sequences)
	if !scanner.Scan() {
		t.Fatal("expected output chunk line")
	}
	var chunk2 chunkRecord
	json.Unmarshal(scanner.Bytes(), &chunk2)
	if chunk2.Dir != Output {
		t.Errorf("dir = %q, want %q", chunk2.Dir, Output)
	}
	decoded2, _ := base64.StdEncoding.DecodeString(chunk2.Data)
	expected := "world\x1b[31mred\x1b[0m"
	if string(decoded2) != expected {
		t.Errorf("data = %q, want %q", string(decoded2), expected)
	}
}

func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")

	sl, err := NewSessionLogger(sessDir, SessionOpts{
		WorkDir: "/test",
		Version: "1.0.0",
		PID:     1,
	})
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}

	const goroutines = 10
	const writesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				sl.LogChunk(Output, []byte("data"))
			}
		}()
	}
	wg.Wait()
	sl.Close()

	// Verify all lines are valid JSONL
	entries, _ := os.ReadDir(sessDir)
	path := filepath.Join(sessDir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		var raw json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			t.Errorf("line %d: invalid JSON: %v", lineCount, err)
		}
	}

	// 1 header + goroutines*writesPerGoroutine chunks
	expected := 1 + goroutines*writesPerGoroutine
	if lineCount != expected {
		t.Errorf("line count = %d, want %d", lineCount, expected)
	}
}

func TestClosePreventsFurtherWrites(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")

	sl, err := NewSessionLogger(sessDir, SessionOpts{
		WorkDir: "/test",
		Version: "1.0.0",
		PID:     1,
	})
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}

	sl.LogChunk(Output, []byte("before"))
	sl.Close()
	sl.LogChunk(Output, []byte("after")) // should be a no-op

	// Verify only header + 1 chunk
	entries, _ := os.ReadDir(sessDir)
	path := filepath.Join(sessDir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}
	if lineCount != 2 { // header + 1 chunk
		t.Errorf("line count = %d, want 2", lineCount)
	}
}

func TestNilClaudeArgs(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")

	sl, err := NewSessionLogger(sessDir, SessionOpts{
		WorkDir: "/test",
		Version: "1.0.0",
		PID:     1,
	})
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	sl.Close()

	// Verify header has empty array, not null
	entries, _ := os.ReadDir(sessDir)
	path := filepath.Join(sessDir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan()
	var header headerRecord
	json.Unmarshal(scanner.Bytes(), &header)
	if header.ClaudeArgs == nil {
		t.Error("claude_args should be empty array, not null")
	}
}
