package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// JSON-RPC 2.0 types
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP Protocol types
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
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
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Server handles MCP protocol communication
type Server struct {
	reader   *bufio.Reader
	writer   io.Writer
	handlers map[string]func(json.RawMessage) (interface{}, error)
	mu       sync.Mutex
}

// NewServer creates a new MCP server
func NewServer() *Server {
	return &Server{
		reader:   bufio.NewReader(os.Stdin),
		writer:   os.Stdout,
		handlers: make(map[string]func(json.RawMessage) (interface{}, error)),
	}
}

// RegisterHandler registers a method handler
func (s *Server) RegisterHandler(method string, handler func(json.RawMessage) (interface{}, error)) {
	s.handlers[method] = handler
}

// Run starts the server main loop
func (s *Server) Run() error {
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error", err.Error())
			continue
		}

		s.handleRequest(req)
	}
}

func (s *Server) handleRequest(req JSONRPCRequest) {
	handler, ok := s.handlers[req.Method]
	if !ok {
		// For notifications (no ID), just ignore unknown methods
		if req.ID == nil {
			return
		}
		s.sendError(req.ID, -32601, "Method not found", req.Method)
		return
	}

	result, err := handler(req.Params)
	if err != nil {
		s.sendError(req.ID, -32603, "Internal error", err.Error())
		return
	}

	// Only send response if there's an ID (not a notification)
	if req.ID != nil {
		s.sendResult(req.ID, result)
	}
}

func (s *Server) sendResult(id interface{}, result interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", data)
}

func (s *Server) sendError(id interface{}, code int, message string, errData interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    errData,
		},
	}

	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", data)
}
