package grpchttp2

import (
	"io"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
)

const (
	http2InitHeaderTableSize = 4096
	http2MaxFrameLen         = 16384
)

type HTTP2bridge struct {
	framer *http2.Framer
	pool   grpc.SharedBufferPool
}

func NewHTTP2bridge(w io.Writer, r io.Reader, maxHeaderListSize uint32) *HTTP2bridge {
	fr := &HTTP2bridge{
		framer: http2.NewFramer(w, r),
		pool:   grpc.NewSharedBufferPool(),
	}

	fr.framer.SetReuseFrames()
	fr.framer.MaxHeaderListSize = maxHeaderListSize
	fr.SetMetaDecoder(hpack.NewDecoder(http2InitHeaderTableSize, nil))

	return fr

}

func (fr *HTTP2bridge) SetMetaDecoder(d *hpack.Decoder) {
	fr.framer.ReadMetaHeaders = d
}

func (fr *HTTP2bridge) ReadFrame() (Frame, error) {
	f, err := fr.framer.ReadFrame()

	if err != nil {
		return nil, err
	}

	hhdr := f.Header()
	hdr := &FrameHeader{
		Size:     hhdr.Length,
		Type:     FrameType(hhdr.Type),
		Flags:    Flags(hhdr.Flags),
		StreamID: hhdr.StreamID,
	}

	switch hdr.Type {
	case FrameTypeData:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.(*http2.DataFrame).Data())
		df := &DataFrame{
			hdr:  hdr,
			Data: buf,
		}
		df.free = func() {
			fr.pool.Put(&buf)
			df.Data = nil
		}
		return df, nil
	case FrameTypeHeaders:
		return fr.adaptHeadersFrame(f)
	case FrameTypeRSTStream:
		return &RSTStreamFrame{
			hdr:  hdr,
			Code: ErrCode(f.(*http2.RSTStreamFrame).ErrCode),
		}, nil
	case FrameTypeSettings:
		hsf := f.(*http2.SettingsFrame)
		buf := make([]Setting, 0, hsf.NumSettings())
		sf := &SettingsFrame{
			hdr:      hdr,
			settings: buf,
		}
		hsf.ForeachSetting(func(s http2.Setting) error {
			buf = append(buf, Setting{
				ID:    SettingID(s.ID),
				Value: s.Val,
			})
			return nil
		})
		return sf, nil
	case FrameTypePing:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.(*http2.PingFrame).Data[:])
		pf := &PingFrame{
			hdr:  hdr,
			Data: buf,
		}
		pf.free = func() {
			fr.pool.Put(&buf)
			pf.Data = nil
		}
		return pf, nil
	case FrameTypeGoAway:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.(*http2.GoAwayFrame).DebugData())
		gf := &GoAwayFrame{
			hdr:       hdr,
			DebugData: buf,
		}
		gf.free = func() {
			fr.pool.Put(&buf)
			gf.DebugData = nil
		}
		return gf, nil
	case FrameTypeWindowUpdate:
		return &WindowUpdateFrame{
			hdr: hdr,
			Inc: f.(*http2.WindowUpdateFrame).Increment,
		}, nil
	case FrameTypeContinuation:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.(*http2.ContinuationFrame).HeaderBlockFragment())
		return &ContinuationFrame{
			hdr:      hdr,
			HdrBlock: buf,
		}, nil
	}

	return nil, connError(ErrCodeProtocol)
}

func (fr *HTTP2bridge) adaptHeadersFrame(f http2.Frame) (Frame, error) {
	hhdr := f.Header()
	hdr := &FrameHeader{
		Size:     hhdr.Length,
		Type:     FrameTypeHeaders,
		Flags:    Flags(hhdr.Flags),
		StreamID: hhdr.StreamID,
	}

	switch f.(type) {
	case *http2.HeadersFrame:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.(*http2.HeadersFrame).HeaderBlockFragment())
		hf := &HeadersFrame{
			hdr:      hdr,
			HdrBlock: buf,
		}
		hf.free = func() {
			fr.pool.Put(&buf)
			hf.HdrBlock = nil
		}
		return hf, nil
	case *http2.MetaHeadersFrame:
		return &MetaHeadersFrame{
			hdr:    hdr,
			Fields: f.(*http2.MetaHeadersFrame).Fields,
		}, nil
	}

	return nil, connError(ErrCodeProtocol)
}

func (fr *HTTP2bridge) WriteData(streamID uint32, endStream bool, data ...[]byte) error {
	var localBuf [http2MaxFrameLen]byte
	off := 0

	for _, s := range data {
		off += copy(localBuf[off:], s)
	}

	return fr.framer.WriteData(streamID, endStream, localBuf[:off])
}

func (fr *HTTP2bridge) WriteHeaders(streamID uint32, endStream, endHeaders bool, data ...[]byte) error {
	var localBuf [http2MaxFrameLen]byte
	off := 0

	for _, s := range data {
		off += copy(localBuf[off:], s)
	}

	p := http2.HeadersFrameParam{
		StreamID:      streamID,
		EndStream:     endStream,
		EndHeaders:    endHeaders,
		BlockFragment: localBuf[:off],
	}

	return fr.framer.WriteHeaders(p)
}

func (fr *HTTP2bridge) WriteRSTStream(streamID uint32, code ErrCode) error {
	return fr.framer.WriteRSTStream(streamID, http2.ErrCode(code))
}

func (fr *HTTP2bridge) WriteSettings(settings ...Setting) error {
	ss := make([]http2.Setting, 0, len(settings))
	for _, s := range settings {
		ss = append(ss, http2.Setting{
			ID:  http2.SettingID(s.ID),
			Val: s.Value,
		})
	}

	return fr.framer.WriteSettings(ss...)
}

func (fr *HTTP2bridge) WriteSettingsAck() error {
	return fr.framer.WriteSettingsAck()
}

func (fr *HTTP2bridge) WritePing(ack bool, data [8]byte) error {
	return fr.framer.WritePing(ack, data)
}

func (fr *HTTP2bridge) WriteGoAway(maxStreamID uint32, code ErrCode, debugData []byte) error {
	return fr.framer.WriteGoAway(maxStreamID, http2.ErrCode(code), debugData)
}

func (fr *HTTP2bridge) WriteWindowUpdate(streamID, inc uint32) error {
	return fr.framer.WriteWindowUpdate(streamID, inc)
}

func (fr *HTTP2bridge) WriteContinuation(streamID uint32, endHeaders bool, headerBlock ...[]byte) error {
	var localBuf [http2MaxFrameLen]byte
	off := 0

	for _, s := range headerBlock {
		off += copy(localBuf[off:], s)
	}

	return fr.framer.WriteContinuation(streamID, endHeaders, localBuf[:off])
}
