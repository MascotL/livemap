package mapbundle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	headerSize = 38

	TypeSQLite uint32 = 1
	TypeImage  uint32 = 2
)

var (
	magic      = [8]byte{'g', 'a', 'm', 'e', ' ', 'm', 'a', 'p'}
	ErrNoImage = errors.New("map bundle image segment not found")
	ErrNoDB    = errors.New("map bundle sqlite segment not found")
)

type Bundle struct {
	path     string
	major    byte
	minor    byte
	segments map[uint32]Segment
	ordered  []Segment
}

type Segment struct {
	Type   uint32
	Offset int64
	Size   int64
}

func Open(path string) (*Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < headerSize {
		return nil, fmt.Errorf("map 文件太短: size=%d", info.Size())
	}

	header := make([]byte, headerSize)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, err
	}
	if string(header[:8]) != string(magic[:]) {
		return nil, fmt.Errorf("map 文件魔数不匹配")
	}

	fileCount := binary.LittleEndian.Uint32(header[10:14])
	if fileCount == 0 {
		return nil, fmt.Errorf("map 文件目录为空")
	}
	if fileCount > 2 {
		return nil, fmt.Errorf("map 文件目录数量不支持: count=%d", fileCount)
	}
	if int64(14+fileCount*12) > info.Size() {
		return nil, fmt.Errorf("map 文件目录越界: count=%d", fileCount)
	}

	ordered, err := parseSegments(header, fileCount, info.Size(), false)
	if err != nil {
		if swapped, swapErr := parseSegments(header, fileCount, info.Size(), true); swapErr == nil {
			ordered = swapped
		} else {
			return nil, err
		}
	}
	ordered = normalizeSegmentSizes(ordered, info.Size())
	segments, err := classifySegments(file, ordered)
	if err != nil {
		if swapped, swapErr := parseSegments(header, fileCount, info.Size(), true); swapErr == nil {
			swapped = normalizeSegmentSizes(swapped, info.Size())
			if swappedSegments, classifyErr := classifySegments(file, swapped); classifyErr == nil {
				ordered = swapped
				segments = swappedSegments
				err = nil
			}
		}
	}
	if err != nil {
		return nil, err
	}

	return &Bundle{
		path:     path,
		major:    header[8],
		minor:    header[9],
		segments: segments,
		ordered:  ordered,
	}, nil
}

func parseSegments(header []byte, fileCount uint32, fileSize int64, swapped bool) ([]Segment, error) {
	ordered := make([]Segment, 0, fileCount)
	for i := uint32(0); i < fileCount; i++ {
		base := 14 + i*12
		typ := binary.LittleEndian.Uint32(header[base : base+4])
		second := int64(binary.LittleEndian.Uint32(header[base+4 : base+8]))
		third := int64(binary.LittleEndian.Uint32(header[base+8 : base+12]))
		offset := second
		size := third
		if swapped {
			size = second
			offset = third
		}
		if offset < headerSize || size < 0 || offset+size > fileSize {
			return nil, fmt.Errorf("map 文件段越界: type=%d offset=%d size=%d fileSize=%d", typ, offset, size, fileSize)
		}
		ordered = append(ordered, Segment{Type: typ, Offset: offset, Size: size})
	}
	return ordered, nil
}

func normalizeSegmentSizes(segments []Segment, fileSize int64) []Segment {
	normalized := append([]Segment(nil), segments...)
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Offset < normalized[j].Offset
	})
	for i := range normalized {
		end := fileSize
		if i+1 < len(normalized) {
			end = normalized[i+1].Offset
		}
		span := end - normalized[i].Offset
		if span > 0 && normalized[i].Size != span {
			normalized[i].Size = span
		}
	}
	return normalized
}

func classifySegments(file *os.File, ordered []Segment) (map[uint32]Segment, error) {
	segments := make(map[uint32]Segment, len(ordered))
	for _, seg := range ordered {
		kind := detectSegment(file, seg)
		if kind == 0 {
			kind = seg.Type
		}
		if kind == TypeSQLite || kind == TypeImage {
			seg.Type = kind
			segments[kind] = seg
		}
	}
	if _, ok := segments[TypeSQLite]; !ok {
		return nil, ErrNoDB
	}
	if _, ok := segments[TypeImage]; !ok {
		return nil, ErrNoImage
	}
	return segments, nil
}

func detectSegment(file *os.File, seg Segment) uint32 {
	buf := make([]byte, 16)
	n, err := file.ReadAt(buf, seg.Offset)
	if err != nil && err != io.EOF {
		return 0
	}
	buf = buf[:n]
	if len(buf) >= 16 && string(buf[:16]) == "SQLite format 3\x00" {
		return TypeSQLite
	}
	if len(buf) >= 8 && bytesEqual(buf[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return TypeImage
	}
	if len(buf) >= 3 && buf[0] == 0xff && buf[1] == 0xd8 && buf[2] == 0xff {
		return TypeImage
	}
	return 0
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (b *Bundle) Version() (byte, byte) {
	if b == nil {
		return 0, 0
	}
	return b.major, b.minor
}

func (b *Bundle) Image() (io.ReadCloser, error) {
	return b.openSegment(TypeImage, ErrNoImage)
}

func (b *Bundle) SQLite() (io.ReadCloser, error) {
	return b.openSegment(TypeSQLite, ErrNoDB)
}

func (b *Bundle) Segment(typ uint32) (Segment, bool) {
	if b == nil {
		return Segment{}, false
	}
	seg, ok := b.segments[typ]
	return seg, ok
}

func (b *Bundle) openSegment(typ uint32, missing error) (io.ReadCloser, error) {
	if b == nil {
		return nil, missing
	}
	seg, ok := b.segments[typ]
	if !ok {
		return nil, missing
	}
	file, err := os.Open(b.path)
	if err != nil {
		return nil, err
	}
	return &segmentReader{File: file, reader: io.NewSectionReader(file, seg.Offset, seg.Size)}, nil
}

type segmentReader struct {
	*os.File
	reader *io.SectionReader
}

func (r *segmentReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}
