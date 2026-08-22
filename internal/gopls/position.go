package gopls

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type wireLocation struct {
	URI                  string    `json:"uri"`
	Range                wireRange `json:"range"`
	TargetURI            string    `json:"targetUri"`
	TargetSelectionRange wireRange `json:"targetSelectionRange"`
}

type wireRange struct {
	Start wirePosition `json:"start"`
}

type wirePosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func (s *lspServer) decodeLocations(raw json.RawMessage) ([]Location, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var locations []wireLocation
	if err := json.Unmarshal(raw, &locations); err != nil {
		var single wireLocation
		if singleErr := json.Unmarshal(raw, &single); singleErr != nil {
			return nil, err
		}
		locations = []wireLocation{single}
	}
	results := make([]Location, 0, len(locations))
	for _, location := range locations {
		uri := location.URI
		position := location.Range.Start
		if uri == "" {
			uri = location.TargetURI
			position = location.TargetSelectionRange.Start
		}
		filename, err := uriPath(uri)
		if err != nil {
			return nil, err
		}
		column, err := s.utf16ColumnToByte(filename, position.Line+1, position.Character)
		if err != nil {
			return nil, err
		}
		results = append(results, Location{Filename: filename, Line: position.Line + 1, Column: column})
	}
	return results, nil
}

func (s *lspServer) fileContents(filename string) ([]byte, error) {
	filename, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	s.filesMu.Lock()
	contents, ok := s.files[filename]
	s.filesMu.Unlock()
	if ok {
		return contents, nil
	}
	contents, err = os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	s.filesMu.Lock()
	s.files[filename] = contents
	s.filesMu.Unlock()
	return contents, nil
}

func (s *lspServer) byteColumnToUTF16(filename string, line, column int) (int, error) {
	contents, err := s.fileContents(filename)
	if err != nil {
		return 0, err
	}
	lineBytes, err := sourceLine(contents, line)
	if err != nil {
		return 0, err
	}
	byteOffset := column - 1
	if byteOffset < 0 || byteOffset > len(lineBytes) {
		return 0, fmt.Errorf("column %d outside %s:%d", column, filename, line)
	}
	return len(utf16.Encode([]rune(string(lineBytes[:byteOffset])))), nil
}

func (s *lspServer) utf16ColumnToByte(filename string, line, character int) (int, error) {
	contents, err := s.fileContents(filename)
	if err != nil {
		return 0, err
	}
	lineBytes, err := sourceLine(contents, line)
	if err != nil {
		return 0, err
	}
	units := 0
	byteOffset := 0
	for byteOffset < len(lineBytes) && units < character {
		r, size := utf8.DecodeRune(lineBytes[byteOffset:])
		if r > 0xffff {
			units += 2
		} else {
			units++
		}
		byteOffset += size
	}
	if units != character {
		return 0, fmt.Errorf("UTF-16 column %d outside %s:%d", character, filename, line)
	}
	return byteOffset + 1, nil
}

func sourceLine(contents []byte, line int) ([]byte, error) {
	if line < 1 {
		return nil, fmt.Errorf("invalid line %d", line)
	}
	start := 0
	for current := 1; current < line; current++ {
		index := bytes.IndexByte(contents[start:], '\n')
		if index < 0 {
			return nil, fmt.Errorf("line %d does not exist", line)
		}
		start += index + 1
	}
	end := bytes.IndexByte(contents[start:], '\n')
	if end < 0 {
		end = len(contents) - start
	}
	return bytes.TrimSuffix(contents[start:start+end], []byte{'\r'}), nil
}

func pathURI(path string) string {
	abs, _ := filepath.Abs(path)
	value := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return (&url.URL{Scheme: "file", Path: value}).String()
}

func uriPath(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported URI %q", value)
	}
	path := filepath.FromSlash(u.Path)
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == filepath.Separator && path[2] == ':' {
		path = path[1:]
	}
	return filepath.Abs(path)
}
