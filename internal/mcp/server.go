package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/maorbril/clauder/internal/store"
)

const (
	ProtocolVersion = "2024-11-05"
	ServerName      = "clauder"
	ServerVersion   = "0.8.4" // Keep in sync with cmd.Version
)

type Server struct {
	store       store.Store
	instanceID  string
	directoryID string
	workDir     string
	reader      *bufio.Reader
	writer      io.Writer
	mu          sync.Mutex
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
	case "initialized":
		// No response needed
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
	skipMessageNotification := false

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
		skipMessageNotification = true // Don't remind about messages when they're already reading them
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
	default:
		result = ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Unknown tool: " + params.Name}},
			IsError: true,
		}
	}

	// Append unread message notification to non-error results
	if !result.IsError {
		result = s.appendNotifications(result, skipMessageNotification)
	}

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
