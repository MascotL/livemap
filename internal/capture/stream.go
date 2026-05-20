package capture

import (
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"time"
)

const (
	frameMagic       = "RLM1"
	pixelFormatRGBA8 = 1
	frameHeaderSize  = 40
)

type Frame struct {
	Image     *image.RGBA
	FrameID   uint64
	Timestamp int64
}

func NewFrame(img *image.RGBA) *Frame {
	return &Frame{
		Image:     img,
		Timestamp: time.Now().UnixNano(),
	}
}

func WriteFrame(w io.Writer, frame *Frame) error {
	if frame == nil || frame.Image == nil {
		return fmt.Errorf("frame 为空")
	}

	img := frame.Image
	width := uint32(img.Rect.Dx())
	height := uint32(img.Rect.Dy())
	stride := uint32(img.Stride)
	payloadLen := uint32(len(img.Pix))

	var header [frameHeaderSize]byte
	copy(header[0:4], []byte(frameMagic))
	binary.LittleEndian.PutUint32(header[4:8], width)
	binary.LittleEndian.PutUint32(header[8:12], height)
	binary.LittleEndian.PutUint32(header[12:16], stride)
	binary.LittleEndian.PutUint32(header[16:20], pixelFormatRGBA8)
	binary.LittleEndian.PutUint64(header[20:28], frame.FrameID)
	binary.LittleEndian.PutUint64(header[28:36], uint64(frame.Timestamp))
	binary.LittleEndian.PutUint32(header[36:40], payloadLen)

	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(img.Pix)
	return err
}
