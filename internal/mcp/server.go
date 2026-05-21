package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/maorbril/clauder/internal/review"
	"github.com/maorbril/clauder/internal/store"
)

const (
	ProtocolVersion = "2024-11-05"
	ServerName      = "clauder"
	ServerVersion   = "0.13.0" // Keep in sync with cmd.Version
)

type Server struct {
	store       store.Store
	instanceID  string
	directoryID string
	workDir     string
	reader      *bufio.Reader
	writer      io.Writer
	mu          sync.Mutex
	reviewMgr   *review.Manager // shared with HTTP server so SSE fans out MCP-side events
}

// SetReviewManager wires a process-wide review.Manager into the MCP server so
// MCP-driven events (e.g. submit_plan_revision) reach connected SSE clients.
func (s *Server) SetReviewManager(m *review.Manager) {
	s.reviewMgr = m
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type InitializeParams struct {
	ProtocolVersion string      `json:"protocolVersion"`
	Capabilities    interface{} `json:"capabilities"`
	ClientInfo      ClientInfo  `json:"clientInfo"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    ServerCapability `json:"capabilities"`
	ServerInfo      ServerInfo       `json:"serverInfo"`
}

type ServerCapability struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Items       *Items   `json:"items,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type Items struct {
	Type string `json:"type"`
}

type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewServer(s store.Store, instanceID, directoryID, workDir string) *Server {
	return &Server{
		store:       s,
		instanceID:  instanceID,
		directoryID: directoryID,
		workDir:     workDir,
		reader:      bufio.NewReader(os.Stdin),
		writer:      os.Stdout,
	}
}

func (s *Server) Run() error {
	fmt.Fprintf(os.Stderr, "[clauder] MCP server ready, waiting for requests...\n")
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Fprintf(os.Stderr, "[clauder] EOF received, shutting down\n")
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error", nil)
			continue
		}

		fmt.Fprintf(os.Stderr, "[clauder] Received request: method=%s\n", req.Method)
		s.handleRequest(&req)
		fmt.Fprintf(os.Stderr, "[clauder] Finished handling: method=%s\n", req.Method)
	}
}

func (s *Server) handleRequest(req *Request) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "notifications/initialized":
		// Notifications do not expect responses.
	case "initialized":
		// Notifications do not expect responses.
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolCall(req)
	case "resources/list":
		s.sendResult(req.ID, map[string]interface{}{"resources": []interface{}{}})
	case "prompts/list":
		s.sendResult(req.ID, map[string]interface{}{"prompts": []interface{}{}})
	case "ping":
		s.sendResult(req.ID, map[string]interface{}{})
	default:
		if req.ID == nil {
			// Unknown notifications must not get error responses.
			return
		}
		s.sendError(req.ID, -32601, "Method not found", nil)
	}
}

func (s *Server) handleInitialize(req *Request) {
	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapability{
			Tools: &ToolsCapability{},
		},
		ServerInfo: ServerInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}
	s.sendResult(req.ID, result)
}

func (s *Server) handleToolsList(req *Request) {
	tools := []Tool{
		{
			Name:        "remember",
			Description: "Store a fact, decision, or piece of context for future sessions. Use this to persist important information that should be available across Claude Code sessions.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"fact": {
						Type:        "string",
						Description: "The fact, decision, or context to remember",
					},
					"tags": {
						Type:        "array",
						Description: "Optional tags to categorize this fact (e.g., 'architecture', 'decision', 'preference')",
						Items:       &Items{Type: "string"},
					},
				},
				Required: []string{"fact"},
			},
		},
		{
			Name:        "recall",
			Description: "Search and retrieve stored facts. Use this to find previously stored context, decisions, or information.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"query": {
						Type:        "string",
						Description: "Search query to find relevant facts (uses full-text search)",
					},
					"tags": {
						Type:        "array",
						Description: "Filter by tags",
						Items:       &Items{Type: "string"},
					},
					"current_dir_only": {
						Type:        "boolean",
						Description: "If true, only return facts from the current directory",
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of facts to return (default: 20)",
					},
				},
			},
		},
		{
			Name:        "forget",
			Description: "Delete a stored fact by ID. Requires user confirmation. First call without confirm to see the fact details, then call again with confirm=true to delete.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {
						Type:        "integer",
						Description: "The ID of the fact to delete (get this from recall results)",
					},
					"confirm": {
						Type:        "boolean",
						Description: "Set to true to confirm the deletion. If false or omitted, shows the fact details for confirmation.",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "get_context",
			Description: "Get all relevant context for the current working directory. Call this at the start of a session to load persistent context.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "get_global_context",
			Description: "Get all stored facts across ALL directories/repositories. Use this when you need context from other projects or want a complete view of everything stored in clauder, regardless of the current working directory.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "list_instances",
			Description: "List all running clauder instances across different directories. Use this to discover other Claude Code sessions you can communicate with.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "send_message",
			Description: "Send a message to another running clauder instance. Use a full instance ID (with :name suffix) to target a specific instance, or use a directory ID (without suffix) to broadcast to all instances in that directory.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"to": {
						Type:        "string",
						Description: "Target instance ID (specific) or directory ID (broadcast to all instances in directory)",
					},
					"content": {
						Type:        "string",
						Description: "The message content",
					},
					"broadcast": {
						Type:        "boolean",
						Description: "If true, send to all instances in the target directory (default: false)",
					},
				},
				Required: []string{"to", "content"},
			},
		},
		{
			Name:        "get_messages",
			Description: "Get messages sent to this instance from other clauder instances.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"unread_only": {
						Type:        "boolean",
						Description: "If true, only return unread messages (default: true)",
					},
				},
			},
		},
		{
			Name:        "compact_context",
			Description: "Get all stored facts with full metadata, formatted for context compaction. Returns every fact with its ID, content, tags, age, and size so you can analyze which facts to keep, delete, or merge. Use this when asked to \"organize your sock drawer\", \"compact context\", or clean up stale memories. Set global=true to review facts across ALL directories.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"global": {
						Type:        "boolean",
						Description: "If true, review facts across all directories (default: current directory only)",
					},
				},
			},
		},
		{
			Name:        "bulk_forget",
			Description: "Delete multiple facts at once by their IDs. Use this after analyzing facts from compact_context to efficiently remove stale or merged facts in a single operation.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"ids": {
						Type:        "array",
						Description: "Array of fact IDs to delete",
						Items:       &Items{Type: "integer"},
					},
				},
				Required: []string{"ids"},
			},
		},
		{
			Name:        "bulk_remember",
			Description: "Store multiple facts at once in a single operation. Use this after compaction to efficiently store merged/condensed facts instead of calling remember in a loop.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"facts": {
						Type:        "array",
						Description: "Array of facts to store. Each entry is an object with 'fact' (string, required) and 'tags' (array of strings, optional).",
						Items:       &Items{Type: "object"},
					},
				},
				Required: []string{"facts"},
			},
		},
		{
			Name:        "compress_facts",
			Description: "Atomically replace multiple facts with consolidated versions. Deletes the specified old facts and adds new ones in a single transaction - no partial state if something fails. Use this after compact_context to merge related facts, remove duplicates, and condense verbose facts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"delete_ids": {
						Type:        "array",
						Description: "IDs of facts to delete (the originals being replaced/removed)",
						Items:       &Items{Type: "integer"},
					},
					"new_facts": {
						Type:        "array",
						Description: "New consolidated facts to add. Each entry is an object with 'fact' (string, required) and 'tags' (array of strings, optional).",
						Items:       &Items{Type: "object"},
					},
				},
			},
		},
		{
			Name:        "update_fact",
			Description: "Update an existing fact's content and/or tags in place without deleting and re-creating it. Preserves the fact ID and creation date.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {
						Type:        "integer",
						Description: "The ID of the fact to update",
					},
					"content": {
						Type:        "string",
						Description: "New content for the fact",
					},
					"tags": {
						Type:        "array",
						Description: "New tags (replaces existing tags). If omitted, existing tags are preserved.",
						Items:       &Items{Type: "string"},
					},
				},
				Required: []string{"id", "content"},
			},
		},
		{
			Name:        "fact_stats",
			Description: "Get statistics about stored facts: counts, sizes, age distribution, and per-directory breakdown. Use this to understand the state of memory before deciding whether to compact.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "purge_deleted",
			Description: "Permanently remove all soft-deleted facts from the database to reclaim space. Requires confirmation. Soft-deleted facts are normally hidden but still stored; this removes them forever.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"confirm": {
						Type:        "boolean",
						Description: "Set to true to confirm permanent deletion. If false/omitted, shows what would be purged.",
					},
				},
			},
		},
		// Tamagotchi Pet tools
		{
			Name:        "pet_status",
			Description: "Check on your Tamagotchi pet! Your pet feeds on tokens you use in Claude Code. Every tool call feeds it. Watch it grow from an egg to an elder as you code! Call this to hatch your pet for the first time.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {
						Type:        "string",
						Description: "Name your pet (only used when hatching a new pet, default: 'Clawde')",
					},
				},
			},
		},
		{
			Name:        "pet_feed",
			Description: "Manually feed your pet some bonus tokens. Your pet is automatically fed when you use Claude Code tools, but you can give it a treat too!",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"tokens": {
						Type:        "integer",
						Description: "Number of bonus tokens to feed (default: 100)",
					},
				},
			},
		},
		{
			Name:        "pet_play",
			Description: "Play with your Tamagotchi pet to boost its happiness! Costs a little hunger though.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "pet_rename",
			Description: "Give your Tamagotchi pet a new name.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {
						Type:        "string",
						Description: "The new name for your pet (max 30 chars)",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "pet_revive",
			Description: "Revive your Tamagotchi pet if it has died from neglect. It comes back as a Jr. with reduced stats.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		// Plan-review tools
		{
			Name: "submit_plan_for_review",
			Description: "Submit a design/implementation plan for human review BEFORE writing any code. " +
				"Use this whenever you have produced a multi-step plan that the user should approve. " +
				"Returns a session_id and a URL where the user reviews the plan, adds inline comments, " +
				"and approves. After calling this tool you MUST stop and wait — do not start building. " +
				"The user's comments and approval arrive as clauder messages.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"plan_markdown": {
						Type:        "string",
						Description: "The plan, in markdown. Use clear ATX headings (## Section) so comments can anchor to sections.",
					},
					"title": {
						Type:        "string",
						Description: "Short title for the plan (defaults to first heading).",
					},
				},
				Required: []string{"plan_markdown"},
			},
		},
		{
			Name: "submit_plan_revision",
			Description: "Submit a revised version of the plan after the user requested changes. " +
				"Existing comments are re-anchored automatically. After this call you must still wait " +
				"for the user to approve before building.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"session_id":    {Type: "string", Description: "Session ID from submit_plan_for_review."},
					"plan_markdown": {Type: "string", Description: "The full revised plan in markdown."},
				},
				Required: []string{"session_id", "plan_markdown"},
			},
		},
		{
			Name: "patch_plan",
			Description: "Apply a small textual edit to the current plan revision. Provide a unique substring " +
				"(old_str) from the current plan and its replacement (new_str). Far cheaper than re-emitting " +
				"the entire plan via submit_plan_revision — prefer this for one-line tweaks, typo fixes, and " +
				"single-section rewrites. Errors if old_str is not present or matches more than one place; in " +
				"that case include more surrounding text to disambiguate.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"session_id": {Type: "string", Description: "Session ID from submit_plan_for_review."},
					"old_str":    {Type: "string", Description: "Substring to replace. Must appear exactly once in the current plan."},
					"new_str":    {Type: "string", Description: "Replacement text. May be empty to delete."},
				},
				Required: []string{"session_id", "old_str", "new_str"},
			},
		},
		{
			Name: "reply_to_comment",
			Description: "Reply to a user comment thread inside a plan-review session, without changing the plan. " +
				"Use this for clarifications. Use submit_plan_revision instead when the comment requires plan changes.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"session_id":        {Type: "string", Description: "Review session ID."},
					"parent_comment_id": {Type: "string", Description: "ID of the comment you are replying to."},
					"body":              {Type: "string", Description: "The reply text."},
				},
				Required: []string{"session_id", "parent_comment_id", "body"},
			},
		},
		{
			Name:        "get_review_plan",
			Description: "Return the current plan, status, and a summary of open comments for a review session.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"session_id": {Type: "string", Description: "Review session ID."},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:        "list_review_sessions",
			Description: "List recent plan-review sessions. Use mine_only=true to filter to sessions started by this instance.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"statuses": {
						Type:        "array",
						Description: "Optional filter: awaiting_review|revising|approved|cancelled",
						Items:       &Items{Type: "string"},
					},
					"mine_only": {
						Type:        "boolean",
						Description: "If true, only sessions owned by this instance.",
					},
				},
			},
		},
	}

	s.sendResult(req.ID, map[string]interface{}{"tools": tools})
}

func (s *Server) handleToolCall(req *Request) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	var result ToolResult

	switch params.Name {
	case "remember":
		result = s.toolRemember(params.Arguments)
	case "recall":
		result = s.toolRecall(params.Arguments)
	case "forget":
		result = s.toolForget(params.Arguments)
	case "get_context":
		result = s.toolGetContext(params.Arguments)
	case "get_global_context":
		result = s.toolGetGlobalContext(params.Arguments)
	case "list_instances":
		result = s.toolListInstances(params.Arguments)
	case "send_message":
		result = s.toolSendMessage(params.Arguments)
	case "get_messages":
		result = s.toolGetMessages(params.Arguments)
	case "compact_context":
		result = s.toolCompactContext(params.Arguments)
	case "bulk_forget":
		result = s.toolBulkForget(params.Arguments)
	case "bulk_remember":
		result = s.toolBulkRemember(params.Arguments)
	case "compress_facts":
		result = s.toolCompressFacts(params.Arguments)
	case "update_fact":
		result = s.toolUpdateFact(params.Arguments)
	case "fact_stats":
		result = s.toolFactStats(params.Arguments)
	case "purge_deleted":
		result = s.toolPurgeDeleted(params.Arguments)
	case "pet_status":
		result = s.toolPetStatus(params.Arguments)
	case "pet_feed":
		result = s.toolPetFeed(params.Arguments)
	case "pet_play":
		result = s.toolPetPlay(params.Arguments)
	case "pet_rename":
		result = s.toolPetRename(params.Arguments)
	case "pet_revive":
		result = s.toolPetRevive(params.Arguments)
	case "submit_plan_for_review":
		result = s.toolSubmitPlanForReview(params.Arguments)
	case "submit_plan_revision":
		result = s.toolSubmitPlanRevision(params.Arguments)
	case "patch_plan":
		result = s.toolPatchPlan(params.Arguments)
	case "reply_to_comment":
		result = s.toolReplyToComment(params.Arguments)
	case "get_review_plan":
		result = s.toolGetReviewPlan(params.Arguments)
	case "list_review_sessions":
		result = s.toolListReviewSessions(params.Arguments)
	default:
		result = ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Unknown tool: " + params.Name}},
			IsError: true,
		}
	}

	// Auto-feed the pet on non-pet tool calls
	if !strings.HasPrefix(params.Name, "pet_") {
		s.feedPetFromToolCall(params.Arguments, result)
	}

	// Log tool call summary to stderr for observability
	fmt.Fprintf(os.Stderr, "[clauder] tool=%s args=%s result=%s\n",
		params.Name, sanitizeArgs(params.Arguments), extractResultSummary(params.Name, params.Arguments, result))

	s.sendResult(req.ID, result)
}

func (s *Server) sendResult(id interface{}, result interface{}) {
	s.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) sendError(id interface{}, code int, message string, data interface{}) {
	s.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func (s *Server) send(resp Response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(s.writer, "%s\n", data)
}
