package gopls

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type lspServer struct {
	root      string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	reader    *bufio.Reader
	stderr    lockedBuffer
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan rpcResponse
	nextID    atomic.Int64
	done      chan error
	filesMu   sync.Mutex
	files     map[string][]byte
	versions  map[string]int
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(value)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type rpcResponse struct {
	result json.RawMessage
	err    error
}

func (c Client) start(ctx context.Context) (*lspServer, error) {
	if err := c.Available(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return nil, err
	}
	cacheDir := c.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(root, ".codegraph-cache", "gopls")
	}
	if !filepath.IsAbs(cacheDir) {
		cacheDir = filepath.Join(root, cacheDir)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create gopls cache: %w", err)
	}
	command := strings.TrimSpace(c.Command)
	if command == "" {
		command = "gopls"
	}
	cmd := exec.CommandContext(ctx, command, "serve")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOPLSCACHE="+cacheDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	server := &lspServer{
		root: root, cmd: cmd, stdin: stdin, reader: bufio.NewReader(stdout),
		pending: map[int64]chan rpcResponse{}, done: make(chan error, 1),
		files: map[string][]byte{}, versions: map[string]int{},
	}
	cmd.Stderr = &server.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start gopls serve: %w", err)
	}
	go server.readLoop()
	go func() { server.done <- cmd.Wait() }()
	return server, nil
}

func (s *lspServer) initialize(ctx context.Context) error {
	rootURI := pathURI(s.root)
	params := map[string]any{
		"processId":  os.Getpid(),
		"clientInfo": map[string]any{"name": "code-graph-parser-go"},
		"rootUri":    rootURI,
		"capabilities": map[string]any{
			"workspace":    map[string]any{"configuration": true, "workspaceFolders": true},
			"textDocument": map[string]any{"implementation": map[string]any{}},
		},
		"workspaceFolders": []map[string]any{{"uri": rootURI, "name": filepath.Base(s.root)}},
	}
	var ignored json.RawMessage
	if err := s.request(ctx, "initialize", params, &ignored); err != nil {
		return fmt.Errorf("initialize gopls: %w%s", err, s.stderrSuffix())
	}
	return s.notify("initialized", map[string]any{})
}

func (s *lspServer) openFiles(positions []Location) error {
	seen := map[string]bool{}
	for _, position := range positions {
		filename, err := filepath.Abs(position.Filename)
		if err != nil {
			return err
		}
		if seen[filename] {
			continue
		}
		seen[filename] = true
		contents, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		s.filesMu.Lock()
		previous, opened := s.files[filename]
		if opened && bytes.Equal(previous, contents) {
			s.filesMu.Unlock()
			continue
		}
		version := s.versions[filename] + 1
		s.files[filename] = contents
		s.versions[filename] = version
		s.filesMu.Unlock()
		if !opened {
			if err := s.notify("textDocument/didOpen", map[string]any{
				"textDocument": map[string]any{
					"uri": pathURI(filename), "languageId": "go", "version": version, "text": string(contents),
				},
			}); err != nil {
				return err
			}
			continue
		}
		if err := s.notify("textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": pathURI(filename), "version": version},
			"contentChanges": []map[string]any{{"text": string(contents)}},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *lspServer) implementation(ctx context.Context, at Location) ([]Location, error) {
	filename, err := filepath.Abs(at.Filename)
	if err != nil {
		return nil, err
	}
	character, err := s.byteColumnToUTF16(filename, at.Line, at.Column)
	if err != nil {
		return nil, err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathURI(filename)},
		"position":     map[string]any{"line": at.Line - 1, "character": character},
	}
	var raw json.RawMessage
	if err := s.request(ctx, "textDocument/implementation", params, &raw); err != nil {
		return nil, fmt.Errorf("gopls implementation %s:%d:%d: %w", filename, at.Line, at.Column, err)
	}
	return s.decodeLocations(raw)
}

func (s *lspServer) request(ctx context.Context, method string, params, result any) error {
	id := s.nextID.Add(1)
	responseCh := make(chan rpcResponse, 1)
	s.pendingMu.Lock()
	s.pending[id] = responseCh
	s.pendingMu.Unlock()
	if err := s.write(rpcMessage{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		s.removePending(id)
		return err
	}
	select {
	case response := <-responseCh:
		if response.err != nil {
			return response.err
		}
		if result != nil && len(response.result) > 0 && string(response.result) != "null" {
			return json.Unmarshal(response.result, result)
		}
		return nil
	case <-ctx.Done():
		s.removePending(id)
		return ctx.Err()
	case err := <-s.done:
		s.removePending(id)
		if err == nil {
			err = io.EOF
		}
		return fmt.Errorf("gopls exited: %w%s", err, s.stderrSuffix())
	}
}

func (s *lspServer) notify(method string, params any) error {
	return s.write(rpcMessage{JSONRPC: "2.0", Method: method, Params: params})
}

type rpcMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method,omitempty"`
	Params  any    `json:"params,omitempty"`
	Result  any    `json:"result,omitempty"`
}

func (s *lspServer) write(message rpcMessage) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := fmt.Fprintf(s.stdin, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = s.stdin.Write(body)
	return err
}

type incomingMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (s *lspServer) readLoop() {
	for {
		body, err := readFrame(s.reader)
		if err != nil {
			s.failPending(err)
			return
		}
		var message incomingMessage
		if err := json.Unmarshal(body, &message); err != nil {
			s.failPending(err)
			return
		}
		if message.Method != "" {
			if len(message.ID) > 0 && string(message.ID) != "null" {
				s.handleServerRequest(message)
			}
			continue
		}
		id, err := strconv.ParseInt(string(message.ID), 10, 64)
		if err != nil {
			continue
		}
		response := rpcResponse{result: message.Result}
		if message.Error != nil {
			response.err = fmt.Errorf("LSP %d: %s", message.Error.Code, message.Error.Message)
		}
		s.pendingMu.Lock()
		responseCh := s.pending[id]
		delete(s.pending, id)
		s.pendingMu.Unlock()
		if responseCh != nil {
			responseCh <- response
		}
	}
}

func (s *lspServer) handleServerRequest(message incomingMessage) {
	var result any = json.RawMessage("null")
	switch message.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(message.Params, &params)
		values := make([]any, len(params.Items))
		for index := range values {
			values[index] = map[string]any{}
		}
		result = values
	case "workspace/workspaceFolders":
		result = []map[string]any{{"uri": pathURI(s.root), "name": filepath.Base(s.root)}}
	case "workspace/applyEdit":
		result = map[string]any{"applied": false}
	}
	var id any
	_ = json.Unmarshal(message.ID, &id)
	_ = s.write(rpcMessage{JSONRPC: "2.0", ID: id, Result: result})
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, errors.New("LSP frame has no Content-Length")
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func (s *lspServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var ignored json.RawMessage
	_ = s.request(ctx, "shutdown", nil, &ignored)
	_ = s.notify("exit", nil)
	_ = s.stdin.Close()
}

func (s *lspServer) removePending(id int64) {
	s.pendingMu.Lock()
	delete(s.pending, id)
	s.pendingMu.Unlock()
}

func (s *lspServer) failPending(err error) {
	s.pendingMu.Lock()
	pending := s.pending
	s.pending = map[int64]chan rpcResponse{}
	s.pendingMu.Unlock()
	for _, responseCh := range pending {
		responseCh <- rpcResponse{err: err}
	}
}

func (s *lspServer) stderrSuffix() string {
	message := strings.TrimSpace(s.stderr.String())
	if message == "" {
		return ""
	}
	return ": " + message
}
