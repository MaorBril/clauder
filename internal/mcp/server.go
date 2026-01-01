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
	ServerVersion   = "0.1.0"
)

type Server struct {
	store      store.Store
	instanceID string
	workDir    string
	reader     *bufio.Reader
	writer     io.Writer
	mu         sync.Mutex
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

func NewServer(s store.Store, instanceID, workDir string) *Server {
	return &Server{
		store:      s,
		instanceID: instanceID,
		workDir:    workDir,
		reader:     bufio.NewReader(os.Stdin),
		writer:     os.Stdout,
	}
}

func (s *Server) Run() error {
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error", nil)
			continue
		}

		s.handleRequest(&req)
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
			Name:        "get_context",
			Description: "Get all relevant context for the current working directory. Call this at the start of a session to load persistent context.",
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
			Description: "Send a message to another running clauder instance. Use this to communicate with Claude Code sessions in other directories.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"to": {
						Type:        "string",
						Description: "The instance ID to send the message to",
					},
					"content": {
						Type:        "string",
						Description: "The message content",
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
	case "get_context":
		result = s.toolGetContext(params.Arguments)
	case "list_instances":
		result = s.toolListInstances(params.Arguments)
	case "send_message":
		result = s.toolSendMessage(params.Arguments)
	case "get_messages":
		result = s.toolGetMessages(params.Arguments)
	default:
		result = ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Unknown tool: " + params.Name}},
			IsError: true,
		}
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
	fmt.Fprintf(s.writer, "%s\n", data)
}
