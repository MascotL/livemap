package view

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	frameMagic       = "RLM1"
	frameHeaderSize  = 40
	pixelFormatRGBA8 = 1
)

type Frame struct {
	Width     int
	Height    int
	Stride    int
	FrameID   uint64
	Timestamp int64
	Pix       []byte
}

func ReadFrame(r io.Reader) (*Frame, error) {
	var header [frameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	if string(header[0:4]) != frameMagic {
		return nil, fmt.Errorf("未知帧头: %q", string(header[0:4]))
	}

	width := binary.LittleEndian.Uint32(header[4:8])
	height := binary.LittleEndian.Uint32(header[8:12])
	stride := binary.LittleEndian.Uint32(header[12:16])
	pixelFormat := binary.LittleEndian.Uint32(header[16:20])
	frameID := binary.LittleEndian.Uint64(header[20:28])
	timestamp := int64(binary.LittleEndian.Uint64(header[28:36]))
	payloadLen := binary.LittleEndian.Uint32(header[36:40])

	if pixelFormat != pixelFormatRGBA8 {
		return nil, fmt.Errorf("不支持的像素格式: %d", pixelFormat)
	}

	pix := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, pix); err != nil {
		return nil, err
	}

	return &Frame{
		Width:     int(width),
		Height:    int(height),
		Stride:    int(stride),
		FrameID:   frameID,
		Timestamp: timestamp,
		Pix:       pix,
	}, nil
}
