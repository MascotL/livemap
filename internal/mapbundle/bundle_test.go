package mapbundle

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenReadsImageAndSQLiteSegments(t *testing.T) {
	sqliteData := []byte("sqlite bytes")
	imageData := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

	header := make([]byte, headerSize)
	copy(header[:8], magic[:])
	header[8] = 2
	header[9] = 1
	binary.LittleEndian.PutUint32(header[10:14], 2)
	binary.LittleEndian.PutUint32(header[14:18], TypeSQLite)
	binary.LittleEndian.PutUint32(header[18:22], headerSize)
	binary.LittleEndian.PutUint32(header[22:26], uint32(len(sqliteData)))
	binary.LittleEndian.PutUint32(header[26:30], TypeImage)
	binary.LittleEndian.PutUint32(header[30:34], uint32(headerSize+len(sqliteData)))
	binary.LittleEndian.PutUint32(header[34:38], uint32(len(imageData)))

	path := filepath.Join(t.TempDir(), "world.map")
	data := append(header, sqliteData...)
	data = append(data, imageData...)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	bundle, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	major, minor := bundle.Version()
	if major != 2 || minor != 1 {
		t.Fatalf("version = %d.%d, want 2.1", major, minor)
	}

	db, err := bundle.SQLite()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	gotDB, err := io.ReadAll(db)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotDB, sqliteData) {
		t.Fatalf("sqlite = %q, want %q", gotDB, sqliteData)
	}

	img, err := bundle.Image()
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	gotImage, err := io.ReadAll(img)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotImage, imageData) {
		t.Fatalf("image = %v, want %v", gotImage, imageData)
	}
}
