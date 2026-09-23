package oggopus

import (
	"bytes"
	"io"
	"testing"
	"time"
)

// page builds an Ogg page from lacing values and data (no checksum: the
// reader does not verify it).
func page(lacing []byte, data []byte) []byte {
	h := make([]byte, 27)
	copy(h, "OggS")
	h[26] = byte(len(lacing))
	return append(append(h, lacing...), data...)
}

func TestSplitsPagesIntoPackets(t *testing.T) {
	a := append([]byte{0xFC}, bytes.Repeat([]byte{1}, 99)...)     // 100 bytes
	b := append([]byte{0xFC}, bytes.Repeat([]byte{2}, 119)...)    // 120 bytes
	long := append([]byte{0xFC}, bytes.Repeat([]byte{3}, 299)...) // 300 bytes: spans two pages
	var stream []byte
	stream = append(stream, page([]byte{19}, []byte("OpusHead........... "[:19]))...)
	stream = append(stream, page([]byte{8}, []byte("OpusTags"))...)
	stream = append(stream, page([]byte{100, 120}, append(append([]byte{}, a...), b...))...) // two packets on one page
	stream = append(stream, page([]byte{255}, long[:255])...)                                // continues...
	stream = append(stream, page([]byte{45}, long[255:])...)                                 // ...and ends here
	r := NewReader(bytes.NewReader(stream))
	var got [][]byte
	for {
		p, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p)
	}
	want := [][]byte{a, b, long}
	if len(got) != len(want) {
		t.Fatalf("got %d packets, want %d (headers must be skipped, pages split)", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("packet %d differs (len %d, want %d)", i, len(got[i]), len(want[i]))
		}
	}
}

func TestDuration(t *testing.T) {
	cases := []struct {
		p    []byte
		want time.Duration
	}{
		{[]byte{31<<3 | 0}, 20 * time.Millisecond},    // CELT 20 ms, one frame
		{[]byte{31<<3 | 1}, 40 * time.Millisecond},    // two frames
		{[]byte{31<<3 | 3, 3}, 60 * time.Millisecond}, // code 3, three frames
		{[]byte{1<<3 | 0}, 20 * time.Millisecond},     // SILK 20 ms
		{[]byte{16<<3 | 0}, 2500 * time.Microsecond},  // CELT 2.5 ms
		{nil, 0},
	}
	for _, c := range cases {
		if got := Duration(c.p); got != c.want {
			t.Errorf("Duration(%x) = %v, want %v", c.p, got, c.want)
		}
	}
}
