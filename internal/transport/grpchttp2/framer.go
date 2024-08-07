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
	"encoding/binary"
	"io"

	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/mem"
)

const initHeaderTableSize = 4096 // Default HTTP/2 header table size.

// FrameType represents the type of an HTTP/2 Frame.
// See [Frame Type].
//
// [Frame Type]: https://httpwg.org/specs/rfc7540.html#FrameType
type FrameType uint8

// Frame types defined in the HTTP/2 Spec.
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

// Flags defined in the HTTP/2 Spec.
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

// IsSet returns a boolean indicating whether the passed flag is set on this
// flag instance.
func (f Flag) IsSet(flag Flag) bool {
	return f&flag != 0
}

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

// Setting IDs defined in the HTTP/2 Spec.
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

// Frame represents an HTTP/2 Frame. This interface struct is only to be used
// on the read path of the Framer. The writing path expects the data to be
// passed individually, not using this type.
//
// Each concrete Frame type defined below implements the Frame interface.
type Frame interface {
	// Header returns the HTTP/2 9 byte header from the current Frame.
	Header() *FrameHeader
	// Free frees the underlying buffer if present so it can be reused by the
	// framer.
	//
	// TODO: Remove method from the interface once the mem package gets merged.
	// Free will be called on each mem.Buffer individually.
	Free()
}

// DataFrame is the representation of a [DATA frame]. DATA frames convey
// arbitrary, variable-length sequences of octets associated with a stream.
//
// [DATA frame]: https://httpwg.org/specs/rfc7540.html#DATA
type DataFrame struct {
	hdr  *FrameHeader
	free func()
	Data []byte
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *DataFrame) Header() *FrameHeader {
	return f.hdr
}

// Free frees the buffer containing the data in this frame.
func (f *DataFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

// HeadersFrame is the representation of a [HEADERS Frame]. The HEADERS frame
// is used to open a stream, and additionally carries a header block fragment.
//
// [HEADERS Frame]: https://httpwg.org/specs/rfc7540.html#HEADERS
type HeadersFrame struct {
	hdr      *FrameHeader
	free     func()
	HdrBlock []byte
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *HeadersFrame) Header() *FrameHeader {
	return f.hdr
}

// Free frees the buffer containing the header block in this frame.
func (f *HeadersFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

// RSTStreamFrame is the representation of a [RST_STREAM Frame]. There is no
// underlying byte array in this frame, so Free() is a no-op. The RST_STREAM
// frame allows for immediate termination of a stream
//
// [RST_STREAM Frame]: https://httpwg.org/specs/rfc7540.html#RST_STREAM
type RSTStreamFrame struct {
	hdr  *FrameHeader
	Code ErrCode
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *RSTStreamFrame) Header() *FrameHeader {
	return f.hdr
}

// Free is a no-op for RSTStreamFrame.
func (f *RSTStreamFrame) Free() {}

// SettingsFrame is the representation of a [SETTINGS Frame]. There is no
// underlying byte array in this frame, so Free() is a no-op.
//
// The SETTINGS frame conveys configuration parameters that affect how
// endpoints communicate, such as preferences and constraints on peer behavior.
//
// [SETTINGS Frame]: https://httpwg.org/specs/rfc7540.html#SETTINGS
type SettingsFrame struct {
	hdr      *FrameHeader
	Settings []Setting
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *SettingsFrame) Header() *FrameHeader {
	return f.hdr
}

// Free is a no-op for SettingsFrame.
func (f *SettingsFrame) Free() {}

// PingFrame is the representation of a [PING Frame]. The PING frame is a
// mechanism for measuring a minimal round-trip time from the sender, as well
// as determining whether an idle connection is still functional.
//
// [PING Frame]: https://httpwg.org/specs/rfc7540.html#PING
type PingFrame struct {
	hdr  *FrameHeader
	free func()
	Data []byte
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *PingFrame) Header() *FrameHeader {
	return f.hdr
}

// Free frees the buffer containing the data in this frame.
func (f *PingFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

// GoAwayFrame is the representation of a [GOAWAY Frame]. The GOAWAY frame is
// used to initiate shutdown of a connection or to signal serious error
// conditions.
//
// [GOAWAY Frame]: https://httpwg.org/specs/rfc7540.html#GOAWAY
type GoAwayFrame struct {
	hdr          *FrameHeader
	free         func()
	LastStreamID uint32
	ErrCode      ErrCode
	DebugData    []byte
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *GoAwayFrame) Header() *FrameHeader {
	return f.hdr
}

// Free frees the buffer containing the debug data in this frame.
func (f *GoAwayFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

// WindowUpdateFrame is the representation of a [WINDOW_UPDATE Frame]. The
// WINDOW_UPDATE frame is used to implement flow control.
//
// [WINDOW_UPDATE Frame]: https://httpwg.org/specs/rfc7540.html#WINDOW_UPDATE
type WindowUpdateFrame struct {
	hdr *FrameHeader
	Inc uint32
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *WindowUpdateFrame) Header() *FrameHeader {
	return f.hdr
}

// Free is a no-op for WindowUpdateFrame.
func (f *WindowUpdateFrame) Free() {}

// ContinuationFrame is the representation of a [CONTINUATION Frame]. The
// CONTINUATION frame is used to continue a sequence of header block fragments.
//
// [CONTINUATION Frame]: https://httpwg.org/specs/rfc7540.html#CONTINUATION
type ContinuationFrame struct {
	hdr      *FrameHeader
	free     func()
	HdrBlock []byte
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *ContinuationFrame) Header() *FrameHeader {
	return f.hdr
}

// Free frees the buffer containing the header block in this frame.
func (f *ContinuationFrame) Free() {
	if f.free != nil {
		f.free()
	}
}

// MetaHeadersFrame is the representation of one HEADERS frame and zero or more
// contiguous CONTINUATION frames and the decoding of their HPACK-encoded
// contents.  This frame type is not transmitted over the network and is only
// generated by the ReadFrame() function.
//
// Since there is no underlying buffer in this Frame, Free() is a no-op.
type MetaHeadersFrame struct {
	hdr    *FrameHeader
	Fields []hpack.HeaderField
	// Truncated indicates whether the MetaHeadersFrame has been truncated due
	// to being longer than the MaxHeaderListSize.
	Truncated bool
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *MetaHeadersFrame) Header() *FrameHeader {
	return f.hdr
}

// Free is a no-op for MetaHeadersFrame.
func (f *MetaHeadersFrame) Free() {}

// Framer encapsulates the functionality to read and write HTTP/2 frames.
type Framer interface {
	// ReadFrame returns grpchttp2.Frame. It is the caller's responsibility to
	// call Frame.Free() once it is done using it. Note that once the mem
	// package gets merged, this API will change in favor of Buffer.Free().
	ReadFrame() (Frame, error)
	// WriteData writes an HTTP/2 DATA frame to the stream.
	// TODO: Once the mem package gets merged, data will change type to
	// mem.BufferSlice.
	WriteData(streamID uint32, endStream bool, data ...[]byte) error
	// WriteData writes an HTTP/2 HEADERS frame to the stream.
	// TODO: Once the mem package gets merged, headerBlock will change type to
	// mem.Buffer.
	WriteHeaders(streamID uint32, endStream, endHeaders bool, headerBlocks []byte) error
	// WriteData writes an HTTP/2 RST_STREAM frame to the stream.
	WriteRSTStream(streamID uint32, code ErrCode) error
	// WriteSettings writes an HTTP/2 SETTINGS frame to the connection.
	WriteSettings(settings ...Setting) error
	// WriteSettingsAck writes an HTTP/2 SETTINGS frame with the ACK flag set.
	WriteSettingsAck() error
	// WritePing writes an HTTP/2 PING frame to the connection.
	WritePing(ack bool, data [8]byte) error
	// WriteGoAway writes an HTTP/2 GOAWAY frame to the connection.
	// TODO: Once the mem package gets merged, debugData will change type to
	// mem.Buffer.
	WriteGoAway(maxStreamID uint32, code ErrCode, debugData []byte) error
	// WriteWindowUpdate writes an HTTP/2 WINDOW_UPDATE frame to the stream.
	WriteWindowUpdate(streamID, inc uint32) error
	// WriteContinuation writes an HTTP/2 CONTINUATION frame to the stream.
	// TODO: Once the mem package gets merged, data will change type to
	// mem.Buffer.
	WriteContinuation(streamID uint32, endHeaders bool, headerBlock []byte) error
}

// framer implements the Framer interface.
type framer struct {
	dec               *hpack.Decoder
	hbuf              [9]byte
	w                 io.Writer
	r                 io.Reader
	pool              mem.BufferPool
	maxHeaderListSize uint32
	lastHeaderStream  uint32
}

func NewFramer(w io.Writer, r io.Reader, maxHeaderListSize uint32) *framer {
	hdrListSize := maxHeaderListSize
	if hdrListSize == 0 {
		hdrListSize = 16 << 20
	}
	return &framer{
		w:                 w,
		r:                 r,
		dec:               hpack.NewDecoder(initHeaderTableSize, nil),
		maxHeaderListSize: hdrListSize,
		pool:              mem.DefaultBufferPool(),
	}
}

func (f *framer) parseDataFrame(hdr *FrameHeader) (Frame, error) {
	if hdr.StreamID == 0 {
		return nil, connError(ErrCodeProtocol)
	}

	buf := f.pool.Get(int(hdr.Size))
	if _, err := io.ReadFull(f.r, buf); err != nil {
		return nil, err
	}

	df := &DataFrame{hdr: hdr, Data: buf}
	df.free = func() {
		f.pool.Put(buf)
	}
	return df, nil
}

func (f *framer) parseHeadersFrame(hdr *FrameHeader) (Frame, error) {
	if hdr.StreamID == 0 {
		return nil, connError(ErrCodeProtocol)
	}

	buf := f.pool.Get(int(hdr.Size))
	if _, err := io.ReadFull(f.r, buf); err != nil {
		return nil, err
	}
	hf := &HeadersFrame{hdr: hdr, HdrBlock: buf}
	hf.free = func() { f.pool.Put(buf) }
	return hf, nil
}

func (f *framer) parseRSTStreamFrame(hdr *FrameHeader) (Frame, error) {
	if hdr.StreamID == 0 {
		return nil, connError(ErrCodeProtocol)
	}
	if hdr.Size != 4 {
		return nil, connError(ErrCodeFrameSize)
	}
	code, err := f.readUint32()
	if err != nil {
		return nil, err
	}
	return &RSTStreamFrame{hdr: hdr, Code: ErrCode(code)}, nil
}

func (f *framer) parseSettingsFrame(hdr *FrameHeader) (Frame, error) {
	if hdr.StreamID != 0 {
		return nil, connError(ErrCodeProtocol)
	}
	if hdr.Size != 0 && hdr.Flags.IsSet(FlagSettingsAck) {
		return nil, connError(ErrCodeProtocol)
	}
	if hdr.Size%6 != 0 {
		return nil, connError(ErrCodeFrameSize)
	}
	buf := f.pool.Get(int(hdr.Size))
	if _, err := io.ReadFull(f.r, buf); err != nil {
		return nil, err
	}
	ss := make([]Setting, 0, hdr.Size/6)
	for i := 0; i < int(hdr.Size)/6; i += 6 {
		ss = append(ss, Setting{
			ID:    SettingID(binary.BigEndian.Uint16(buf[i : i+2])),
			Value: binary.BigEndian.Uint32(buf[i+2 : i+6]),
		})
	}

	return &SettingsFrame{hdr: hdr, Settings: ss}, nil
}

func (f *framer) parsePingFrame(hdr *FrameHeader) (Frame, error) {
	if hdr.StreamID != 0 {
		return nil, connError(ErrCodeProtocol)
	}
	if hdr.Size != 8 {
		return nil, connError(ErrCodeFrameSize)
	}

	buf := f.pool.Get(8)
	if _, err := io.ReadFull(f.r, buf); err != nil {
		return nil, err
	}

	pf := &PingFrame{hdr: hdr, Data: buf}
	pf.free = func() { f.pool.Put(buf) }
	return pf, nil
}

func (f *framer) parseGoAwayFrame(hdr *FrameHeader) (Frame, error) {
	if hdr.StreamID != 0 {
		return nil, connError(ErrCodeProtocol)
	}
	lastStream, err := f.readUint32()
	if err != nil {
		return nil, err
	}
	code, err := f.readUint32()
	if err != nil {
		return nil, err
	}
	// Do not count the 4 bytes of the ErrCode and the 4 bytes of the
	// LastStreamID.
	buf := f.pool.Get(int(hdr.Size) - 8)
	if _, err := io.ReadFull(f.r, buf); err != nil {
		return nil, err
	}
	gf := &GoAwayFrame{
		hdr:          hdr,
		LastStreamID: lastStream,
		ErrCode:      ErrCode(code),
		DebugData:    buf,
	}
	gf.free = func() { f.pool.Put(buf) }

	return gf, nil
}

func (f *framer) parseWindowUpdate(hdr *FrameHeader) (Frame, error) {
	inc, err := f.readUint32()
	if err != nil {
		return nil, err
	}

	if inc == 0 {
		if hdr.StreamID == 0 {
			return nil, connError(ErrCodeProtocol)
		}
		return nil, streamError{StreamID: hdr.StreamID, ErrCode: ErrCodeProtocol}
	}

	return &WindowUpdateFrame{
		hdr: hdr,
		Inc: inc,
	}, nil
}

func (f *framer) parseContinuationFrame(hdr *FrameHeader) (Frame, error) {
	if hdr.StreamID == 0 {
		return nil, connError(ErrCodeProtocol)
	}

	buf := f.pool.Get(int(hdr.Size))
	if _, err := io.ReadFull(f.r, buf); err != nil {
		return nil, err
	}
	cf := &ContinuationFrame{
		hdr:      hdr,
		HdrBlock: buf,
	}
	cf.free = func() { f.pool.Put(buf) }
	return cf, nil
}

func (f *framer) checkOrder(hdr FrameHeader) error {
	if f.lastHeaderStream != 0 {
		if hdr.Type != FrameTypeContinuation {
			return connError(ErrCodeProtocol)
		} else if f.lastHeaderStream != hdr.StreamID {
			return connError(ErrCodeProtocol)
		}
	} else if hdr.Type == FrameTypeContinuation {
		return connError(ErrCodeProtocol)
	}

	if hdr.Type == FrameTypeContinuation || hdr.Type == FrameTypeHeaders {
		if hdr.Flags.IsSet(FlagHeadersEndHeaders) {
			f.lastHeaderStream = 0
		} else {
			f.lastHeaderStream = hdr.StreamID
		}

	}

	return nil
}

func (f *framer) readMetaHeaders(frame *HeadersFrame) (Frame, error) {
	mh := &MetaHeadersFrame{hdr: frame.Header()}

	remainingSize := f.maxHeaderListSize
	f.dec.SetEmitEnabled(true)
	f.dec.SetEmitFunc(func(hf hpack.HeaderField) {
		size := hf.Size()
		if size > remainingSize {
			f.dec.SetEmitEnabled(false)
			mh.Truncated = true
			remainingSize = 0
			return
		}
		remainingSize -= size
		mh.Fields = append(mh.Fields, hf)
	})

	var currFr Frame = frame
	frag := frame.HdrBlock
	for {
		if _, err := f.dec.Write(frag); err != nil {
			return nil, connError(ErrCodeCompression)
		}

		if currFr.Header().Flags.IsSet(FlagHeadersEndHeaders) {
			break
		}

		fr, err := f.ReadFrame()
		if err != nil {
			return nil, err
		}

		currFr.Free()
		currFr = fr
		frag = fr.(*ContinuationFrame).HdrBlock
	}

	return mh, nil
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

func (f *framer) readUint32() (uint32, error) {
	buf := f.pool.Get(4)
	defer f.pool.Put(buf)
	if _, err := io.ReadFull(f.r, buf); err != nil {
		return 0, err
	}

	return uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]), nil
}

func (f *framer) ReadFrame() (Frame, error) {
	hdr, err := f.readHeader()
	if err != nil {
		return nil, err
	}
	var fr Frame
	switch hdr.Type {
	case FrameTypeData:
		fr, err = f.parseDataFrame(hdr)
	case FrameTypeHeaders:
		fr, err = f.parseHeadersFrame(hdr)
	case FrameTypeRSTStream:
		fr, err = f.parseRSTStreamFrame(hdr)
	case FrameTypeSettings:
		fr, err = f.parseSettingsFrame(hdr)
	case FrameTypePing:
		fr, err = f.parsePingFrame(hdr)
	case FrameTypeGoAway:
		fr, err = f.parseGoAwayFrame(hdr)
	case FrameTypeWindowUpdate:
		fr, err = f.parseWindowUpdate(hdr)
	case FrameTypeContinuation:
		fr, err = f.parseContinuationFrame(hdr)
	default:
		return nil, connError(ErrCodeProtocol)
	}

	if err != nil {
		return nil, err
	}

	if err := f.checkOrder(*hdr); err != nil {
		return nil, err
	}

	if fr.Header().Type == FrameTypeHeaders {
		fr, err = f.readMetaHeaders(fr.(*HeadersFrame))
	}
	return fr, err

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

func (f *framer) writeUint32(v uint32) error {
	buf := f.pool.Get(4)
	defer f.pool.Put(buf)
	buf[0] = byte(v >> 24)
	buf[1] = byte(v >> 16)
	buf[2] = byte(v >> 8)
	buf[3] = byte(v)
	_, err := f.w.Write(buf)
	return err
}

func (f *framer) writeUint16(v uint16) error {
	buf := f.pool.Get(2)
	defer f.pool.Put(buf)
	buf[0] = byte(v >> 8)
	buf[1] = byte(v)
	_, err := f.w.Write(buf)
	return err
}

func (f *framer) WriteData(streamID uint32, endStream bool, data mem.BufferSlice) error {
	var flag Flag
	if endStream {
		flag |= FlagDataEndStream
	}

	if err := f.writeHeader(uint32(data.Len()), FrameTypeData, flag, streamID); err != nil {
		return err
	}

	for _, buf := range data {
		f.w.Write(buf.ReadOnlyData())
	}

	return nil
}

func (f *framer) WriteHeaders(streamID uint32, endStream, endHeaders bool, headerBlock []byte) error {
	var flag Flag
	if endStream {
		flag |= FlagHeadersEndStream
	}
	if endHeaders {
		flag |= FlagHeadersEndHeaders
	}

	if err := f.writeHeader(uint32(len(headerBlock)), FrameTypeHeaders, flag, streamID); err != nil {
		return err
	}

	_, err := f.w.Write(headerBlock)
	return err
}

func (f *framer) WriteRSTStream(streamID uint32, code ErrCode) error {
	if err := f.writeHeader(4, FrameTypeRSTStream, 0, streamID); err != nil {
		return err
	}
	return f.writeUint32(uint32(code))
}

func (f *framer) WriteSettings(settings ...Setting) error {
	// Each setting is 6 bytes long.
	tl := len(settings) * 6

	if err := f.writeHeader(uint32(tl), FrameTypeSettings, 0, 0); err != nil {
		return err
	}

	for _, s := range settings {
		if err := f.writeUint16(uint16(s.ID)); err != nil {
			return err
		}
		if err := f.writeUint32(s.Value); err != nil {
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
	// maxStreamID + ErrCode + debugData
	tl := 4 + 4 + len(debugData)
	if err := f.writeHeader(uint32(tl), FrameTypeGoAway, 0, 0); err != nil {
		return err
	}
	if err := f.writeUint32(maxStreamID); err != nil {
		return err
	}
	if err := f.writeUint32(uint32(code)); err != nil {
		return err
	}
	_, err := f.w.Write(debugData)
	return err
}

func (f *framer) WriteWindowUpdate(streamID, incr uint32) error {
	if err := f.writeHeader(4, FrameTypeWindowUpdate, 0, streamID); err != nil {
		return err
	}
	return f.writeUint32(incr)
}

func (f *framer) WriteContinuation(streamID uint32, endHeaders bool, headerBlock []byte) error {
	var flag Flag
	if endHeaders {
		flag |= FlagHeadersEndHeaders
	}

	if err := f.writeHeader(uint32(len(headerBlock)), FrameTypeContinuation, flag, streamID); err != nil {
		return err
	}

	_, err := f.w.Write(headerBlock)
	return err
}
