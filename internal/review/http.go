package review

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web
var webFS embed.FS

// muxLike is satisfied by both *http.ServeMux and http.DefaultServeMux usage
// via a thin adapter — we keep the surface narrow so callers can pass either.
type muxLike interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// RegisterRoutes mounts /review/* (SPA) and /api/review/* (JSON+SSE) on the
// given mux. The SPA is served from the embedded web/ directory.
func (m *Manager) RegisterRoutes(mux muxLike) {
	static, err := fs.Sub(webFS, "web")
	if err == nil {
		mux.Handle("/review/static/", http.StripPrefix("/review/static/", http.FileServer(http.FS(static))))
	}

	mux.HandleFunc("/review", m.handleReviewRoot)
	mux.HandleFunc("/review/", m.handleReviewSPA)
	mux.HandleFunc("/api/review/sessions", m.handleListSessions)
	mux.HandleFunc("/api/review/", m.handleSessionAPI)
}

// StartStandalone tries to bind addr and serve only the review UI/API.
// Returns immediately if the port is already in use (a sibling clauder is
// presumably already serving). The server runs in the calling goroutine.
func (m *Manager) StartStandalone(addr string) error {
	mux := http.NewServeMux()
	m.RegisterRoutes(mux)
	srv := &http.Server{Addr: addr, Handler: mux}
	return srv.ListenAndServe()
}

func (m *Manager) handleReviewRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/review/", http.StatusFound)
}

// handleReviewSPA serves either the inbox (when path is /review/) or the
// session view (when path is /review/{sessionID}).
func (m *Manager) handleReviewSPA(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/review/")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "":
		serveEmbedded(w, r, "web/inbox.html", "text/html; charset=utf-8")
	case strings.HasPrefix(rest, "static/"):
		// Already handled by the StripPrefix file server.
		http.NotFound(w, r)
	default:
		// Anything that looks like an ID falls through to the session view.
		serveEmbedded(w, r, "web/session.html", "text/html; charset=utf-8")
	}
}

func serveEmbedded(w http.ResponseWriter, _ *http.Request, name, mime string) {
	data, err := webFS.ReadFile(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mime)
	_, _ = w.Write(data)
}

func (m *Manager) handleListSessions(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query()["status"]
	sessions, err := m.store.ListReviewSessionsByStatus(statusFilter, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// handleSessionAPI dispatches /api/review/{sessionID}/{action}
func (m *Manager) handleSessionAPI(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/review/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch action {
	case "", "state":
		m.handleGetState(w, r, sessionID)
	case "events":
		m.handleEventStream(w, r, sessionID)
	case "comments":
		m.handlePostComment(w, r, sessionID)
	case "approve":
		m.handlePost(w, r, sessionID, func(string) error { return m.Approve(sessionID) })
	case "cancel":
		m.handlePost(w, r, sessionID, func(string) error { return m.Cancel(sessionID) })
	case "request-revision":
		m.handleRequestRevision(w, r, sessionID)
	default:
		http.NotFound(w, r)
	}
}

func (m *Manager) handleGetState(w http.ResponseWriter, _ *http.Request, sessionID string) {
	state, err := m.GetState(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if state == nil {
		http.NotFound(w, nil)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (m *Manager) handlePostComment(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Body              string `json:"body"`
		AnchorSectionID   string `json:"anchor_section_id"`
		AnchorStartOffset int    `json:"anchor_start_offset"`
		AnchorEndOffset   int    `json:"anchor_end_offset"`
		ParentCommentID   string `json:"parent_comment_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	c, err := m.AddUserComment(sessionID, CommentOpts{
		Body:              body.Body,
		AnchorSectionID:   body.AnchorSectionID,
		AnchorStartOffset: body.AnchorStartOffset,
		AnchorEndOffset:   body.AnchorEndOffset,
		ParentCommentID:   body.ParentCommentID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (m *Manager) handleRequestRevision(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := m.RequestRevision(sessionID, body.Body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "requested"})
}

func (m *Manager) handlePost(w http.ResponseWriter, r *http.Request, _ string, fn func(string) error) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := fn(""); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleEventStream pushes review events to the SPA via Server-Sent Events.
func (m *Manager) handleEventStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := m.Subscribe(sessionID)
	defer cancel()

	// Send a hello so the client knows the stream is alive.
	_, _ = fmt.Fprintf(w, ":hello\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(ev)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, data)
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
