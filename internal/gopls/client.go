// Package gopls provides the task-local LSP client used by incremental
// parsing. A Session can serve all file requests in one build task.
package gopls

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Location is a 1-based byte position, matching go/token positions.
type Location struct {
	Filename string
	Line     int
	Column   int
}

// QueryResult preserves the input order of a batch implementation request.
type QueryResult struct {
	Locations []Location
	Err       error
}

// Client configures either a one-shot or task-scoped gopls LSP server.
type Client struct {
	Command  string
	Root     string
	CacheDir string
}

// Session owns one gopls process and its in-memory workspace snapshot.
// Calls are serialized because the engine processes one file at a time.
type Session struct {
	server *lspServer
	mu     sync.Mutex
}

// ErrUnavailable means that the configured gopls executable is not installed.
var ErrUnavailable = errors.New("gopls executable is unavailable")

func (c Client) Available() error {
	command := strings.TrimSpace(c.Command)
	if command == "" {
		command = "gopls"
	}
	if strings.ContainsRune(command, filepath.Separator) {
		if info, err := os.Stat(command); err != nil || info.IsDir() {
			return fmt.Errorf("%w: %s", ErrUnavailable, command)
		}
		return nil
	}
	if _, err := exec.LookPath(command); err != nil {
		return fmt.Errorf("%w: %s", ErrUnavailable, command)
	}
	return nil
}

// Open starts and initializes one task-scoped gopls process.
func (c Client) Open(ctx context.Context) (*Session, error) {
	server, err := c.start(ctx)
	if err != nil {
		return nil, err
	}
	if err := server.initialize(ctx); err != nil {
		server.close()
		return nil, err
	}
	return &Session{server: server}, nil
}

// Implementations is the one-shot compatibility path.
func (c Client) Implementations(ctx context.Context, positions []Location, concurrency int) ([]QueryResult, error) {
	results := make([]QueryResult, len(positions))
	if len(positions) == 0 {
		return results, nil
	}
	session, err := c.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.Implementations(ctx, positions, concurrency)
}

// Implementations executes a batch against the existing workspace process.
func (s *Session) Implementations(ctx context.Context, positions []Location, concurrency int) ([]QueryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := make([]QueryResult, len(positions))
	if len(positions) == 0 {
		return results, nil
	}
	if err := s.server.openFiles(positions); err != nil {
		return nil, err
	}

	query := func(index int) {
		locations, queryErr := s.server.implementation(ctx, positions[index])
		results[index] = QueryResult{Locations: locations, Err: queryErr}
	}

	// The first request establishes the workspace snapshot. Remaining requests
	// share it and may run concurrently without starting more processes.
	query(0)
	if len(positions) == 1 {
		return results, nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(positions)-1 {
		concurrency = len(positions) - 1
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for index := range jobs {
				query(index)
			}
		}()
	}
	for index := 1; index < len(positions); index++ {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results, nil
}

// Close shuts down the task-local gopls process.
func (s *Session) Close() {
	if s == nil || s.server == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.server.close()
	s.server = nil
}
