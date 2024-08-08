package grpchttp2

import (
	"testing"

	"golang.org/x/net/http2/hpack"
)

func (s) TestFramer_ReadFrame_Data(t *testing.T) {
	c := &testConn{}
	wantData := "test data"

	c.rbuf = append(c.rbuf, 0, 0, byte(len(wantData)), byte(FrameTypeData), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = append(c.rbuf, []byte(wantData)...)

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := wantHeader{
		wantSize:      len(wantData),
		wantFrameType: FrameTypeData,
		wantFlag:      0,
		wantStreamID:  1,
	}
	if err := checkReadHeader(f.Header(), wantHdr); err != nil {
		t.Errorf("ReadFrame(): %v", err)
	}
}

func (s) TestFramer_ReadFrame_RSTStream(t *testing.T) {
	c := &testConn{}
	c.rbuf = append(c.rbuf, 0, 0, 4, byte(FrameTypeRSTStream), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = appendUint32(c.rbuf, uint32(ErrCodeProtocol))

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := wantHeader{
		wantSize:      4,
		wantFrameType: FrameTypeRSTStream,
		wantFlag:      0,
		wantStreamID:  1,
	}
	if err := checkReadHeader(f.Header(), wantHdr); err != nil {
		t.Errorf("ReadFrame(): %v", err)
	}

	if rf := f.(*RSTStreamFrame); rf.ErrCode != ErrCodeProtocol {
		t.Errorf("ReadFrame(): Code: got %#x, want %#x", rf.ErrCode, ErrCodeProtocol)
	}
}

func (s) TestFramer_ReadFrame_Settings(t *testing.T) {
	c := &testConn{}
	s := Setting{ID: SettingsHeaderTableSize, Value: 200}
	c.rbuf = append(c.rbuf, 0, 0, 6, byte(FrameTypeSettings), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = append(c.rbuf, byte(s.ID>>8), byte(s.ID))
	c.rbuf = appendUint32(c.rbuf, s.Value)

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := wantHeader{
		wantSize:      6,
		wantFrameType: FrameTypeSettings,
		wantFlag:      0,
		wantStreamID:  0,
	}
	if err := checkReadHeader(f.Header(), wantHdr); err != nil {
		t.Errorf("ReadFrame(): %v", err)
	}

	if sf := f.(*SettingsFrame); sf.Settings[0] != s {
		t.Errorf("ReadFrame(): Settings: got %v, want %v", sf.Settings[0], s)
	}
}

func (s) TestFramer_ReadFrame_Ping(t *testing.T) {
	c := &testConn{}
	d := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	c.rbuf = append(c.rbuf, 0, 0, 8, byte(FrameTypePing), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = append(c.rbuf, d...)

	fr := NewFramer(c, c, 0, nil)
	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := wantHeader{
		wantSize:      8,
		wantFrameType: FrameTypePing,
		wantFlag:      0,
		wantStreamID:  0,
	}
	if err := checkReadHeader(f.Header(), wantHdr); err != nil {
		t.Errorf("ReadFrame(): %v", err)
	}

	for i, b := range d {
		if pf := f.(*PingFrame); b != pf.Data[i] {
			t.Errorf("ReadFrame(): Data[%d]: got %d, want %d", i, pf.Data[i], b)
		}
	}
}

func (s) TestFramer_ReadFrame_GoAway(t *testing.T) {
	c := &testConn{}
	d := "debug_data"
	// The length of data + 4 byte code + 4 byte streamID
	ln := len(d) + 8
	c.rbuf = append(c.rbuf, 0, 0, byte(ln), byte(FrameTypeGoAway), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = appendUint32(c.rbuf, 2)
	c.rbuf = appendUint32(c.rbuf, uint32(ErrCodeFlowControl))
	c.rbuf = append(c.rbuf, []byte(d)...)

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := wantHeader{
		wantSize:      ln,
		wantFrameType: FrameTypeGoAway,
		wantFlag:      0,
		wantStreamID:  0,
	}
	if err := checkReadHeader(f.Header(), wantHdr); err != nil {
		t.Errorf("ReadFrame(): %v", err)
	}
	gf := f.(*GoAwayFrame)
	if gf.LastStreamID != 2 {
		t.Errorf("ReadFrame(): LastStreamID: got %d, want %d", gf.LastStreamID, 2)
	}
	if gf.ErrCode != ErrCodeFlowControl {
		t.Errorf("ReadFrame(): Code: got %#x, want %#x", gf.ErrCode, ErrCodeFlowControl)
	}
	if string(gf.DebugData) != d {
		t.Errorf("ReadFrame(): DebugData: got %q, want %q", string(gf.DebugData), d)
	}
}

func (s) TestFramer_ReadFrame_WindowUpdate(t *testing.T) {
	c := &testConn{}
	c.rbuf = append(c.rbuf, 0, 0, 4, byte(FrameTypeWindowUpdate), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = appendUint32(c.rbuf, 100)

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := wantHeader{
		wantSize:      4,
		wantFrameType: FrameTypeWindowUpdate,
		wantFlag:      0,
		wantStreamID:  1,
	}
	if err := checkReadHeader(f.Header(), wantHdr); err != nil {
		t.Errorf("ReadFrame(): %v", err)
	}

	wf := f.(*WindowUpdateFrame)
	if wf.Inc != 100 {
		t.Errorf("ReadFrame(): Inc: got %d, want %d", wf.Inc, 100)
	}
}

func (s) TestFramer_ReadFrame_MetaHeaders(t *testing.T) {
	fields := []hpack.HeaderField{
		{Name: "foo", Value: "bar"},
		{Name: "baz", Value: "qux"},
	}

	c := &testConn{}
	enc := hpack.NewEncoder(c)
	for _, field := range fields {
		enc.WriteField(field)
	}
	half1 := c.wbuf[0 : len(c.wbuf)/2]
	half2 := c.wbuf[len(c.wbuf)/2:]

	c.rbuf = append(c.rbuf, 0, 0, byte(len(half1)), byte(FrameTypeHeaders), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	// Copy half of the data written by the encoder into the reading buf
	c.rbuf = append(c.rbuf, half1...)

	// Add Continuation Frame to test merging
	c.rbuf = append(c.rbuf, 0, 0, byte(len(half2)), byte(FrameTypeContinuation), byte(FlagContinuationEndHeaders))
	c.rbuf = appendUint32(c.rbuf, 1)
	// Copy data written by the encoder into the reading buf
	c.rbuf = append(c.rbuf, half2...)

	f := NewFramer(c, c, 0, nil)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	mf, ok := fr.(*MetaHeadersFrame)
	if !ok {
		t.Errorf("ReadFrame(): Type: expected MetaHeadersFrame, got %T", fr)
	}
	if len(mf.Fields) != 2 {
		t.Fatalf("ReadFrame(): Fields: got %d, want %d", len(mf.Fields), 1)
	}
	for i, field := range fields {
		if field.Name != mf.Fields[i].Name {
			t.Errorf("ReadFrame(): Fields[%d].Name: got %q, want %q", i, mf.Fields[i].Name, field.Name)
		}
		if field.Value != mf.Fields[i].Value {
			t.Errorf("ReadFrame(): Fields[%d].Value: got %q, want %q", i, mf.Fields[i].Value, field.Value)
		}
	}

}
