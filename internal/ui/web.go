package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/maorbril/clauder/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local development
	},
}

//go:embed templates/*
var templateFS embed.FS

// WebServer provides an HTTP-based dashboard
type WebServer struct {
	store           store.Store
	workDir         string
	refreshInterval time.Duration
	templates       *template.Template
}

// NewWebServer creates a new web server dashboard
func NewWebServer(s store.Store, workDir string, refreshInterval time.Duration) (*WebServer, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &WebServer{
		store:           s,
		workDir:         workDir,
		refreshInterval: refreshInterval,
		templates:       tmpl,
	}, nil
}

// APIResponse represents the JSON response for the API
type APIResponse struct {
	Instances []InstanceData `json:"instances"`
	Messages  []MessageData  `json:"messages"`
	Facts     []FactData     `json:"facts"`
	Stats     StatsData      `json:"stats"`
}

// InstanceData represents an instance in the API response
type InstanceData struct {
	ID              string        `json:"id"`
	ShortID         string        `json:"shortId"`
	Directory       string        `json:"directory"`
	ShortDir        string        `json:"shortDir"`
	ProjectName     string        `json:"projectName"`
	IsLeader        bool          `json:"isLeader"`
	IsIdle          bool          `json:"isIdle"`
	IsActive        bool          `json:"isActive"`
	NeedsAttention  bool          `json:"needsAttention"`
	AttentionReason string        `json:"attentionReason"`
	UnreadCount     int           `json:"unreadCount"`
	LastHeartbeat   string        `json:"lastHeartbeat"`
	HeartbeatAge    string        `json:"heartbeatAge"`
	RecentMessages  []MessageData `json:"recentMessages"`
}

// MessageData represents a message in the API response
type MessageData struct {
	ID           int64  `json:"id"`
	FromInstance string `json:"fromInstance"`
	ShortFrom    string `json:"shortFrom"`
	FromDir      string `json:"fromDir"`
	ShortFromDir string `json:"shortFromDir"`
	ToInstance   string `json:"toInstance"`
	ShortTo      string `json:"shortTo"`
	ToDir        string `json:"toDir"`
	ShortToDir   string `json:"shortToDir"`
	Content      string `json:"content"`
	Preview      string `json:"preview"`
	CreatedAt    string `json:"createdAt"`
	Age          string `json:"age"`
	IsRead       bool   `json:"isRead"`
}

// FactData represents a fact in the API response
type FactData struct {
	ID        int64    `json:"id"`
	Content   string   `json:"content"`
	Preview   string   `json:"preview"`
	Tags      []string `json:"tags"`
	SourceDir string   `json:"sourceDir"`
	ShortDir  string   `json:"shortDir"`
	IsLocal   bool     `json:"isLocal"`
	CreatedAt string   `json:"createdAt"`
}

// StatsData represents the statistics in the API response
type StatsData struct {
	TotalInstances   int `json:"totalInstances"`
	WorkingInstances int `json:"workingInstances"`
	TotalMessages    int `json:"totalMessages"`
	UnreadMessages   int `json:"unreadMessages"`
	TotalFacts       int `json:"totalFacts"`
	LocalFacts       int `json:"localFacts"`
}

// Start starts the web server on the given port
func (ws *WebServer) Start(port int) error {
	http.HandleFunc("/", ws.handleDashboard)
	http.HandleFunc("/api/data", ws.handleAPIData)
	http.HandleFunc("/api/launch", ws.handleLaunch)
	http.HandleFunc("/api/facts/delete", ws.handleDeleteFact)
	http.HandleFunc("/api/terminal", ws.handleTerminal)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Starting web dashboard at http://localhost%s", addr)
	return http.ListenAndServe(addr, nil)
}

func (ws *WebServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := struct {
		RefreshInterval int
		WorkDir         string
	}{
		RefreshInterval: int(ws.refreshInterval.Seconds()),
		WorkDir:         ws.workDir,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ws.templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleAPIData(w http.ResponseWriter, r *http.Request) {
	instances, err := ws.store.GetInstances()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all messages (up to 500)
	messages, err := ws.store.GetAllMessages(500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all facts (up to 500)
	facts, err := ws.store.GetFacts("", nil, "", 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build response
	response := ws.buildResponse(instances, messages, facts)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Directory string `json:"directory"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	dir := req.Directory
	if dir == "" {
		dir = ws.workDir
	}

	// Verify directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		http.Error(w, "Directory does not exist", http.StatusBadRequest)
		return
	}

	// Launch terminal with claude - use osascript on macOS
	script := fmt.Sprintf(`
		tell application "Terminal"
			activate
			do script "cd %q && claude"
		end tell
	`, dir)

	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to launch terminal: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "launched",
		"directory": dir,
	})
}

func (ws *WebServer) handleDeleteFact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid fact ID", http.StatusBadRequest)
		return
	}

	if err := ws.store.SoftDeleteFact(id); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete fact: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (ws *WebServer) handleTerminal(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = ws.workDir
	}

	// Verify directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		http.Error(w, "Directory does not exist", http.StatusBadRequest)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Create command
	cmd := exec.Command("claude")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Wait for initial size from client before starting PTY
	var initialSize pty.Winsize
	initialSize.Cols = 80
	initialSize.Rows = 24

	// Try to read initial size (with timeout)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if msgType, data, err := conn.ReadMessage(); err == nil && msgType == websocket.TextMessage {
		var size struct {
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		if json.Unmarshal(data, &size) == nil && size.Cols > 0 && size.Rows > 0 {
			initialSize.Cols = size.Cols
			initialSize.Rows = size.Rows
		}
	}
	conn.SetReadDeadline(time.Time{}) // Clear deadline

	log.Printf("Starting PTY with size: %dx%d", initialSize.Cols, initialSize.Rows)

	// Start PTY with the correct size from the beginning
	ptmx, err := pty.StartWithSize(cmd, &initialSize)
	if err != nil {
		log.Printf("Failed to start PTY: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v\r\n", err)))
		return
	}
	defer ptmx.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// PTY -> WebSocket
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("PTY read error: %v", err)
				}
				return
			}
			// Send as binary message (PTY output may contain non-UTF8 bytes)
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		}
	}()

	// WebSocket -> PTY
	go func() {
		defer wg.Done()
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				// Kill the process when WebSocket closes
				cmd.Process.Kill()
				return
			}

			// Handle resize messages (JSON with cols/rows)
			if msgType == websocket.TextMessage {
				var resize struct {
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if err := json.Unmarshal(data, &resize); err == nil && resize.Cols > 0 && resize.Rows > 0 {
					pty.Setsize(ptmx, &pty.Winsize{Rows: resize.Rows, Cols: resize.Cols})
					continue
				}
			}

			// Write input to PTY
			if _, err := ptmx.Write(data); err != nil {
				log.Printf("PTY write error: %v", err)
				return
			}
		}
	}()

	// Wait for command to finish
	cmd.Wait()
	wg.Wait()
}

// Helper functions

func shortenPath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}

	// Try to use home directory shorthand
	home := "~"
	if len(path) > 7 && path[:7] == "/Users/" {
		parts := make([]string, 0, 4)
		start := 0
		count := 0
		for i := 0; i < len(path) && count < 4; i++ {
			if path[i] == '/' {
				if i > start {
					parts = append(parts, path[start:i])
				}
				start = i + 1
				count++
			}
		}
		if start < len(path) && count < 4 {
			parts = append(parts, path[start:])
		}
		if len(parts) >= 4 {
			path = home + "/" + parts[3]
		}
	}

	if len(path) <= maxLen {
		return path
	}

	// Truncate from the start
	if maxLen <= 3 {
		return path[:maxLen]
	}
	return "..." + path[len(path)-maxLen+3:]
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		return fmt.Sprintf("%dh ago", hours)
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd ago", days)
}

func extractProjectName(path string) string {
	if path == "" {
		return "unknown"
	}
	// Find the last path segment
	lastSlash := -1
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			lastSlash = i
			break
		}
	}
	if lastSlash >= 0 && lastSlash < len(path)-1 {
		return path[lastSlash+1:]
	}
	return path
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

func (ws *WebServer) buildResponse(instances []store.Instance, messages []store.Message, facts []store.Fact) APIResponse {
	// Build instance ID -> directory map for message lookups
	instanceDirMap := make(map[string]string)
	for _, inst := range instances {
		instanceDirMap[inst.ID] = inst.Directory
	}

	// Count unread messages per instance and collect recent messages
	unreadPerInstance := make(map[string]int)
	messagesPerInstance := make(map[string][]store.Message)
	for _, msg := range messages {
		if msg.ReadAt == nil {
			unreadPerInstance[msg.ToInstance]++
		}
		// Collect messages to/from each instance (limit to 5 most recent)
		messagesPerInstance[msg.ToInstance] = append(messagesPerInstance[msg.ToInstance], msg)
		if msg.FromInstance != msg.ToInstance {
			messagesPerInstance[msg.FromInstance] = append(messagesPerInstance[msg.FromInstance], msg)
		}
	}

	// Convert instances
	instanceData := make([]InstanceData, len(instances))
	workingCount := 0
	for i, inst := range instances {
		shortID := inst.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		age := time.Since(inst.LastHeartbeat)
		isActive := age < 60*time.Second
		unreadCount := unreadPerInstance[inst.ID]
		needsAttention := unreadCount > 0 || !isActive || inst.IsIdle

		// Build attention reason
		var attentionReason string
		if needsAttention {
			reasons := []string{}
			if unreadCount > 0 {
				if unreadCount == 1 {
					reasons = append(reasons, "1 unread message")
				} else {
					reasons = append(reasons, fmt.Sprintf("%d unread messages", unreadCount))
				}
			}
			if !isActive {
				reasons = append(reasons, fmt.Sprintf("inactive %s", formatDuration(age)))
			}
			if inst.IsIdle {
				reasons = append(reasons, "idle")
			}
			attentionReason = joinStrings(reasons, ", ")
		}

		if isActive && !inst.IsIdle {
			workingCount++
		}

		// Get recent messages for this instance (up to 5)
		instMessages := messagesPerInstance[inst.ID]
		recentMessages := make([]MessageData, 0, 5)
		for j := 0; j < len(instMessages) && j < 5; j++ {
			msg := instMessages[j]
			shortFrom := msg.FromInstance
			if len(shortFrom) > 8 {
				shortFrom = shortFrom[:8]
			}
			shortTo := msg.ToInstance
			if len(shortTo) > 8 {
				shortTo = shortTo[:8]
			}
			fromDir := instanceDirMap[msg.FromInstance]
			if fromDir == "" {
				fromDir = "unknown"
			}
			toDir := instanceDirMap[msg.ToInstance]
			if toDir == "" {
				toDir = "unknown"
			}
			preview := msg.Content
			if len(preview) > 100 {
				preview = preview[:97] + "..."
			}
			recentMessages = append(recentMessages, MessageData{
				ID:           msg.ID,
				FromInstance: msg.FromInstance,
				ShortFrom:    shortFrom,
				FromDir:      fromDir,
				ShortFromDir: shortenPath(fromDir, 25),
				ToInstance:   msg.ToInstance,
				ShortTo:      shortTo,
				ToDir:        toDir,
				ShortToDir:   shortenPath(toDir, 25),
				Content:      msg.Content,
				Preview:      preview,
				CreatedAt:    msg.CreatedAt.Format(time.RFC3339),
				Age:          formatDuration(time.Since(msg.CreatedAt)),
				IsRead:       msg.ReadAt != nil,
			})
		}

		instanceData[i] = InstanceData{
			ID:              inst.ID,
			ShortID:         shortID,
			Directory:       inst.Directory,
			ShortDir:        shortenPath(inst.Directory, 40),
			ProjectName:     extractProjectName(inst.Directory),
			IsLeader:        inst.IsLeader,
			IsIdle:          inst.IsIdle,
			IsActive:        isActive,
			NeedsAttention:  needsAttention,
			AttentionReason: attentionReason,
			UnreadCount:     unreadCount,
			LastHeartbeat:   inst.LastHeartbeat.Format(time.RFC3339),
			HeartbeatAge:    formatDuration(age),
			RecentMessages:  recentMessages,
		}
	}

	// Convert messages
	messageData := make([]MessageData, len(messages))
	unread := 0
	for i, msg := range messages {
		shortFrom := msg.FromInstance
		if len(shortFrom) > 8 {
			shortFrom = shortFrom[:8]
		}
		shortTo := msg.ToInstance
		if len(shortTo) > 8 {
			shortTo = shortTo[:8]
		}

		// Look up directories for from/to instances
		fromDir := instanceDirMap[msg.FromInstance]
		if fromDir == "" {
			fromDir = "unknown"
		}
		toDir := instanceDirMap[msg.ToInstance]
		if toDir == "" {
			toDir = "unknown"
		}

		preview := msg.Content
		if len(preview) > 100 {
			preview = preview[:97] + "..."
		}

		isRead := msg.ReadAt != nil
		if !isRead {
			unread++
		}

		messageData[i] = MessageData{
			ID:           msg.ID,
			FromInstance: msg.FromInstance,
			ShortFrom:    shortFrom,
			FromDir:      fromDir,
			ShortFromDir: shortenPath(fromDir, 25),
			ToInstance:   msg.ToInstance,
			ShortTo:      shortTo,
			ToDir:        toDir,
			ShortToDir:   shortenPath(toDir, 25),
			Content:      msg.Content,
			Preview:      preview,
			CreatedAt:    msg.CreatedAt.Format(time.RFC3339),
			Age:          formatDuration(time.Since(msg.CreatedAt)),
			IsRead:       isRead,
		}
	}

	// Convert facts
	factData := make([]FactData, len(facts))
	localFacts := 0
	for i, fact := range facts {
		preview := fact.Content
		if len(preview) > 100 {
			preview = preview[:97] + "..."
		}

		isLocal := fact.SourceDir == ws.workDir
		if isLocal {
			localFacts++
		}

		factData[i] = FactData{
			ID:        fact.ID,
			Content:   fact.Content,
			Preview:   preview,
			Tags:      fact.Tags,
			SourceDir: fact.SourceDir,
			ShortDir:  shortenPath(fact.SourceDir, 30),
			IsLocal:   isLocal,
			CreatedAt: fact.CreatedAt.Format(time.RFC3339),
		}
	}

	return APIResponse{
		Instances: instanceData,
		Messages:  messageData,
		Facts:     factData,
		Stats: StatsData{
			TotalInstances:   len(instances),
			WorkingInstances: workingCount,
			TotalMessages:    len(messages),
			UnreadMessages:   unread,
			TotalFacts:       len(facts),
			LocalFacts:       localFacts,
		},
	}
}
