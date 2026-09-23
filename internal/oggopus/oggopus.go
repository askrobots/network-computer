// Package oggopus reads individual Opus packets out of an Ogg stream.
//
// An Ogg page can hold several Opus packets, and a packet can span pages.
// Sending a whole page as one RTP payload glues packets together into
// something that is not a valid Opus packet, which the receiver decodes as
// garbage and then has to conceal. RTP wants exactly one Opus packet each.
package oggopus

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"time"
)

// Reader yields Opus packets in order, skipping the OpusHead and OpusTags
// header packets.
type Reader struct {
	r       *bufio.Reader
	pending []byte   // a packet still continuing onto the next page
	queue   [][]byte // complete packets not yet returned
}

func NewReader(r io.Reader) *Reader { return &Reader{r: bufio.NewReaderSize(r, 64<<10)} }

// Next returns the next Opus packet.
func (o *Reader) Next() ([]byte, error) {
	for len(o.queue) == 0 {
		if err := o.readPage(); err != nil {
			return nil, err
		}
	}
	p := o.queue[0]
	o.queue = o.queue[1:]
	return p, nil
}

var errNotOgg = errors.New("oggopus: not an Ogg page")

func (o *Reader) readPage() error {
	var hdr [27]byte
	if _, err := io.ReadFull(o.r, hdr[:]); err != nil {
		return err
	}
	if string(hdr[:4]) != "OggS" {
		return errNotOgg
	}
	segs := make([]byte, int(hdr[26]))
	if _, err := io.ReadFull(o.r, segs); err != nil {
		return err
	}
	total := 0
	for _, l := range segs {
		total += int(l)
	}
	data := make([]byte, total)
	if _, err := io.ReadFull(o.r, data); err != nil {
		return err
	}
	off := 0
	for _, l := range segs {
		o.pending = append(o.pending, data[off:off+int(l)]...)
		off += int(l)
		if l < 255 { // a lacing value under 255 ends the packet
			p := o.pending
			o.pending = nil
			if !bytes.HasPrefix(p, []byte("OpusHead")) && !bytes.HasPrefix(p, []byte("OpusTags")) && len(p) > 0 {
				o.queue = append(o.queue, p)
			}
		}
	}
	return nil
}

// Duration is how much audio an Opus packet holds, from its TOC byte
// (RFC 6716 section 3.1). Zero if the packet is malformed.
func Duration(p []byte) time.Duration {
	if len(p) == 0 {
		return 0
	}
	toc := p[0]
	cfg := toc >> 3
	var frame time.Duration
	switch {
	case cfg < 12: // SILK: 10, 20, 40, 60 ms
		frame = []time.Duration{10, 20, 40, 60}[cfg%4] * time.Millisecond
	case cfg < 16: // Hybrid: 10, 20 ms
		frame = []time.Duration{10, 20}[cfg%2] * time.Millisecond
	default: // CELT: 2.5, 5, 10, 20 ms
		frame = []time.Duration{2500, 5000, 10000, 20000}[cfg%4] * time.Microsecond
	}
	frames := 1
	switch toc & 3 {
	case 1, 2:
		frames = 2
	case 3:
		if len(p) < 2 {
			return 0
		}
		frames = int(p[1] & 0x3f)
	}
	return time.Duration(frames) * frame
}
