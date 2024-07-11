/*
 *
 * Copyright 2024 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package grpchttp2 defines HTTP/2 types and a framer API and implementation.
package grpchttp2

import (
	"io"

	"golang.org/x/net/http2/hpack"
)

// FrameType represents the type of an HTTP/2 Frame.
// See [Frame Type].
//
// [Frame Type]: https://httpwg.org/specs/rfc7540.html#FrameType
type FrameType uint8

const (
	FrameTypeData         FrameType = 0x0
	FrameTypeHeaders      FrameType = 0x1
	FrameTypeRSTStream    FrameType = 0x3
	FrameTypeSettings     FrameType = 0x4
	FrameTypePing         FrameType = 0x6
	FrameTypeGoAway       FrameType = 0x7
	FrameTypeWindowUpdate FrameType = 0x8
	FrameTypeContinuation FrameType = 0x9
)

// Flag represents one or more flags set on an HTTP/2 Frame.
type Flag uint8

const (
	FlagDataEndStream          Flag = 0x1
	FlagDataPadded             Flag = 0x8
	FlagHeadersEndStream       Flag = 0x1
	FlagHeadersEndHeaders      Flag = 0x4
	FlagHeadersPadded          Flag = 0x8
	FlagHeadersPriority        Flag = 0x20
	FlagSettingsAck            Flag = 0x1
	FlagPingAck                Flag = 0x1
	FlagContinuationEndHeaders Flag = 0x4
)

// Setting represents the id and value pair of an HTTP/2 setting.
// See [Setting Format].
//
// [Setting Format]: https://httpwg.org/specs/rfc7540.html#SettingFormat
type Setting struct {
	ID    SettingID
	Value uint32
}

// SettingID represents the id of an HTTP/2 setting.
// See [Setting Values].
//
// [Setting Values]: https://httpwg.org/specs/rfc7540.html#SettingValues
type SettingID uint16

const (
	SettingsHeaderTableSize      SettingID = 0x1
	SettingsEnablePush           SettingID = 0x2
	SettingsMaxConcurrentStreams SettingID = 0x3
	SettingsInitialWindowSize    SettingID = 0x4
	SettingsMaxFrameSize         SettingID = 0x5
	SettingsMaxHeaderListSize    SettingID = 0x6
)

// FrameHeader is the 9 byte header of any HTTP/2 Frame.
// See [Frame Header].
//
// [Frame Header]: https://httpwg.org/specs/rfc7540.html#FrameHeader
type FrameHeader struct {
	// Size is the size of the frame's payload without the 9 header bytes.
	// As per the HTTP/2 spec, size can be up to 3 bytes, but only frames
	// up to 16KB can be processed without agreement.
	Size uint32
	// Type is a byte that represents the Frame Type.
	Type FrameType
	// Flags is a byte representing the flags set on this Frame.
	Flags Flag
	// StreamID is the ID for the stream which this frame is for. If the
	// frame is connection specific instead of stream specific, the
	// streamID is 0.
	StreamID uint32
}

// Frame represents an HTTP/2 Frame.
type Frame interface {
	Header() *FrameHeader
	// Free frees the underlying buffer if present so it can be reused by the
	// framer.
	Free()
}

type DataFrame struct {
	hdr  *FrameHeader
	free func()
	Data []byte
}

func (f *DataFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *DataFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

type HeadersFrame struct {
	hdr      *FrameHeader
	free     func()
	HdrBlock []byte
}

func (f *HeadersFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *HeadersFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

type RSTStreamFrame struct {
	hdr  *FrameHeader
	Code ErrCode
}

func (f *RSTStreamFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *RSTStreamFrame) Free() {}

type SettingsFrame struct {
	hdr      *FrameHeader
	settings []Setting
}

func (f *SettingsFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *SettingsFrame) Free() {}

type PingFrame struct {
	hdr  *FrameHeader
	free func()
	Data []byte
}

func (f *PingFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *PingFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

type GoAwayFrame struct {
	hdr          *FrameHeader
	free         func()
	LastStreamID uint32
	Code         ErrCode
	DebugData    []byte
}

func (f *GoAwayFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *GoAwayFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

type WindowUpdateFrame struct {
	hdr *FrameHeader
	Inc uint32
}

func (f *WindowUpdateFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *WindowUpdateFrame) Free() {}

type ContinuationFrame struct {
	hdr      *FrameHeader
	free     func()
	HdrBlock []byte
}

func (f *ContinuationFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *ContinuationFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

// MetaHeadersFrame is not a Frame Type that appears on the HTTP/2 Spec. It is
// a representation of the merging and decoding of all the Headers and
// Continuation frames on a Stream.
type MetaHeadersFrame struct {
	hdr    *FrameHeader
	Fields []hpack.HeaderField
}

func (f *MetaHeadersFrame) Header() *FrameHeader {
	return f.hdr
}

func (f *MetaHeadersFrame) Free() {}

// Framer encapsulates the functionality to read and write HTTP/2 frames.
type Framer interface {
	// SetMetaDecoder will set a decoder for the framer. When the decoder is
	// set, ReadFrame will parse the header values, merging all Headers and
	// Continuation frames.
	SetMetaDecoder(d *hpack.Decoder)
	// ReadFrame returns an HTTP/2 Frame. It is the caller's responsibility to
	// free the frame once it is done using it.
	ReadFrame() (Frame, error)
	WriteData(streamID uint32, endStream bool, data ...[]byte) error
	WriteHeaders(streamID uint32, endStream, endHeaders bool, headerBlock ...[]byte) error
	WriteRSTStream(streamID uint32, code ErrCode) error
	WriteSettings(settings ...Setting) error
	WriteSettingsAck() error
	WritePing(ack bool, data [8]byte) error
	WriteGoAway(maxStreamID uint32, code ErrCode, debugData []byte) error
	WriteWindowUpdate(streamID, inc uint32) error
	WriteContinuation(streamID uint32, endHeaders bool, headerBlock ...[]byte) error
}

// framer implements the Framer interface.
type framer struct {
	metaDecoder *hpack.Decoder
	hbuf        [9]byte
	w           io.Writer
	r           io.Reader
}

func NewFramer(w io.Writer, r io.Reader) Framer {
	return &framer{
		w: w,
		r: r,
	}
}

func (f *framer) writeHeader(size uint32, ft FrameType, flags Flag, streamID uint32) error {
	if size >= 1<<24 {
		return connError(ErrCodeFrameSize)
	}

	f.hbuf[0] = byte(size >> 16)
	f.hbuf[1] = byte(size >> 8)
	f.hbuf[2] = byte(size)
	f.hbuf[3] = byte(ft)
	f.hbuf[4] = byte(flags)
	f.hbuf[5] = byte(streamID >> 24)
	f.hbuf[6] = byte(streamID >> 16)
	f.hbuf[7] = byte(streamID >> 8)
	f.hbuf[8] = byte(streamID)

	_, err := f.w.Write(f.hbuf[:])

	return err
}

func (f *framer) readHeader() (*FrameHeader, error) {
	_, err := io.ReadFull(f.r, f.hbuf[:])
	if err != nil {
		return nil, err
	}

	return &FrameHeader{
		Size:     uint32(f.hbuf[0])<<16 | uint32(f.hbuf[1])<<8 | uint32(f.hbuf[2]),
		Type:     FrameType(f.hbuf[3]),
		Flags:    Flag(f.hbuf[4]),
		StreamID: uint32(f.hbuf[5])<<24 | uint32(f.hbuf[6])<<16 | uint32(f.hbuf[7])<<8 | uint32(f.hbuf[8]),
	}, nil
}

func (f *framer) writeUint32(v uint32) error {
	_, err := f.w.Write([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	return err
}

func (f *framer) writeUint16(v uint16) error {
	_, err := f.w.Write([]byte{byte(v >> 8), byte(v)})
	return err
}

func totalLen(d ...[]byte) int {
	l := 0
	for _, s := range d {
		l += len(s)
	}
	return l
}

func (f *framer) SetMetaDecoder(d *hpack.Decoder) {
	f.metaDecoder = d
}

func (f *framer) ReadFrame() (Frame, error)

func (f *framer) WriteData(streamID uint32, endStream bool, data ...[]byte) error {
	tl := totalLen(data...)

	var flag Flag
	if endStream {
		flag |= FlagDataEndStream
	}

	if err := f.writeHeader(uint32(tl), FrameTypeData, flag, streamID); err != nil {
		return err
	}

	for _, d := range data {
		_, err := f.w.Write(d)
		if err != nil {
			return err
		}
	}

	return nil
}

func (f *framer) WriteHeaders(streamID uint32, endStream, endHeaders bool, headerBlock ...[]byte) error {
	tl := totalLen(headerBlock...)

	var flag Flag
	if endStream {
		flag |= FlagHeadersEndStream
	}
	if endHeaders {
		flag |= FlagHeadersEndHeaders
	}

	if err := f.writeHeader(uint32(tl), FrameTypeHeaders, flag, streamID); err != nil {
		return err
	}

	for _, h := range headerBlock {
		_, err := f.w.Write(h)
		if err != nil {
			return err
		}
	}

	return nil
}

func (f *framer) WriteRSTStream(streamID uint32, code ErrCode) error {
	if err := f.writeHeader(4, FrameTypeRSTStream, 0, streamID); err != nil {
		return err
	}
	f.writeUint32(uint32(code))
	return nil
}

func (f *framer) WriteSettings(settings ...Setting) error {
	// Each setting is 6 bytes long.
	tl := len(settings) * 6

	f.writeHeader(uint32(tl), FrameTypeSettings, FlagSettingsAck, 0)

	for _, s := range settings {
		err := f.writeUint16(uint16(s.ID))
		if err != nil {
			return err
		}
		err = f.writeUint32(s.Value)
		if err != nil {
			return err
		}
	}

	return nil
}

func (f *framer) WriteSettingsAck() error {
	return f.writeHeader(0, FrameTypeSettings, FlagSettingsAck, 0)
}

func (f *framer) WritePing(ack bool, data [8]byte) error {
	var flag Flag
	if ack {
		flag |= FlagPingAck
	}

	return f.writeHeader(8, FrameTypePing, flag, 0)
}

func (f *framer) WriteGoAway(maxStreamID uint32, code ErrCode, debugData []byte) error {
	// maxStreamID + code + debugData
	tl := 4 + 4 + len(debugData)
	err := f.writeHeader(uint32(tl), FrameTypeGoAway, 0, 0)
	err = f.writeUint32(maxStreamID)
	err = f.writeUint32(uint32(code))
	_, err = f.w.Write(debugData)
	return err
}

func (f *framer) WriteWindowUpdate(streamID, incr uint32) error {
	err := f.writeHeader(4, FrameTypeWindowUpdate, 0, streamID)
	if err != nil {
		return err
	}
	err = f.writeUint32(incr)
	return err
}

func (f *framer) WriteContinuation(streamID uint32, endHeaders bool, headerBlock ...[]byte) error {
	tl := totalLen(headerBlock...)

	var flag Flag
	if endHeaders {
		flag |= FlagHeadersEndHeaders
	}

	err := f.writeHeader(uint32(tl), FrameTypeContinuation, flag, streamID)
	if err != nil {
		return err
	}

	for _, hb := range headerBlock {
		_, err := f.w.Write(hb)
		if err != nil {
			return err
		}
	}

	return nil
}
