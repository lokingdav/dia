package sipcontroller

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
)

// NetstringEncoder encodes data into netstring format
type NetstringEncoder struct {
	w io.Writer
}

// NewNetstringEncoder creates a new netstring encoder
func NewNetstringEncoder(w io.Writer) *NetstringEncoder {
	return &NetstringEncoder{w: w}
}

// Encode writes data as a netstring: <length>:<data>,
func (e *NetstringEncoder) Encode(data []byte) error {
	// Format: <length>:<data>,
	header := fmt.Sprintf("%d:", len(data))
	if _, err := e.w.Write([]byte(header)); err != nil {
		return err
	}
	if _, err := e.w.Write(data); err != nil {
		return err
	}
	if _, err := e.w.Write([]byte{','}); err != nil {
		return err
	}
	return nil
}

// NetstringDecoder decodes netstring-framed data from a stream
type NetstringDecoder struct {
	r      io.Reader
	buffer []byte
}

// NewNetstringDecoder creates a new netstring decoder
func NewNetstringDecoder(r io.Reader) *NetstringDecoder {
	return &NetstringDecoder{
		r:      r,
		buffer: make([]byte, 0),
	}
}

// Decode reads and decodes the next netstring from the stream
// Returns the payload without the framing
func (d *NetstringDecoder) Decode() ([]byte, error) {
	// Read until we have a complete netstring
	for {
		// Try to parse from buffer
		if payload, remaining, ok := d.tryParse(); ok {
			d.buffer = remaining
			return payload, nil
		}

		// Need more data
		chunk := make([]byte, 4096)
		n, err := d.r.Read(chunk)
		if err != nil {
			return nil, err
		}
		d.buffer = append(d.buffer, chunk[:n]...)
	}
}

// tryParse attempts to parse a complete netstring from the buffer
// Returns (payload, remaining buffer, success)
func (d *NetstringDecoder) tryParse() ([]byte, []byte, bool) {
	if len(d.buffer) < 3 {
		return nil, nil, false
	}

	// Find the colon
	colonIdx := bytes.IndexByte(d.buffer, ':')
	if colonIdx == -1 {
		return nil, nil, false
	}

	// Parse length
	lengthStr := string(d.buffer[:colonIdx])
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		// Invalid netstring, skip until next valid start
		return nil, d.buffer[1:], false
	}

	// Check if we have enough data
	// Format: <length>:<data>,
	// Total needed: colonIdx + 1 (for colon) + length + 1 (for comma)
	totalNeeded := colonIdx + 1 + length + 1
	if len(d.buffer) < totalNeeded {
		return nil, nil, false
	}

	// Verify trailing comma
	if d.buffer[totalNeeded-1] != ',' {
		// Invalid netstring
		return nil, d.buffer[1:], false
	}

	// Extract payload
	payload := make([]byte, length)
	copy(payload, d.buffer[colonIdx+1:colonIdx+1+length])

	// Return remaining buffer
	remaining := d.buffer[totalNeeded:]

	return payload, remaining, true
}
