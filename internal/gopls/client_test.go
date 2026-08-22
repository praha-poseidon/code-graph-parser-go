package gopls

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPositionConversions(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "unicode.go")
	contents := []byte("package sample\nvar 世界 = 1\n")
	if err := os.WriteFile(filename, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	server := &lspServer{files: map[string][]byte{filename: contents}}
	character, err := server.byteColumnToUTF16(filename, 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if character != 4 {
		t.Fatalf("UTF-16 character=%d, want 4", character)
	}
	column, err := server.utf16ColumnToByte(filename, 2, character)
	if err != nil {
		t.Fatal(err)
	}
	if column != 5 {
		t.Fatalf("byte column=%d, want 5", column)
	}
}

func TestReadFrame(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"result":null}`)
	framed := append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))), body...)
	got, err := readFrame(bufio.NewReader(bytes.NewReader(framed)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body=%q, want %q", got, body)
	}
}
