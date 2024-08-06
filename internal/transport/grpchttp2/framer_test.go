package grpchttp2

import "testing"

func (s) TestReadFrame_Data(t *testing.T) {
	c := &testConn{}
	wantData := "test data"

	c.rbuf = append(c.rbuf, 0, 0, byte(len(wantData)), byte(FrameTypeData), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = append(c.rbuf, []byte(wantData)...)

	fr := NewFramer(c, c, 0)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	if err := checkReadHeader(f.Header(), 9, FrameTypeData, 0, 1); err != nil {
		t.Errorf("ReadFrame(): %v", err)
	}
}
