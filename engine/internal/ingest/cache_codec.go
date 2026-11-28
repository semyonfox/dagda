package ingest

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/semyonfox/dagda/engine/internal/model"
)

// The scan cache is read on every report, once per cached log file, so its
// decoding is a large share of a warm run. gob spent a third of that
// re-reading type descriptors for each of thousands of small files, and
// allocated every repeated string again. This codec writes the fields in a
// fixed order and stores each distinct string once per file, so a session's
// model, project and session id decode to one shared string.
//
// The cache is only read by the build that wrote it (its namespace hashes the
// parser and model source), so the layout needs no versioning of its own.
// TestCacheCodecRoundTripsEveryTurnField fails when a Turn field is added
// without being encoded here.

var errCacheCodec = errors.New("damaged scan cache record")

type cacheEncoder struct {
	buf     []byte
	strings map[string]uint64
}

func (e *cacheEncoder) uint(v uint64) { e.buf = binary.AppendUvarint(e.buf, v) }
func (e *cacheEncoder) int(v int64)   { e.buf = binary.AppendVarint(e.buf, v) }

func (e *cacheEncoder) bool(v bool) {
	if v {
		e.buf = append(e.buf, 1)
	} else {
		e.buf = append(e.buf, 0)
	}
}

// string writes an index into the strings seen so far, or the next index and
// the bytes of a new one
func (e *cacheEncoder) string(s string) {
	if i, ok := e.strings[s]; ok {
		e.uint(i)
		return
	}
	i := uint64(len(e.strings))
	e.strings[s] = i
	e.uint(i)
	e.uint(uint64(len(s)))
	e.buf = append(e.buf, s...)
}

func encodeCacheRecord(record scanCacheRecord) ([]byte, error) {
	e := cacheEncoder{buf: make([]byte, 0, 64+len(record.Turns)*96), strings: map[string]uint64{}}
	e.uint(uint64(record.Version))
	e.string(record.Namespace)
	e.string(record.Fingerprint)
	e.uint(uint64(len(record.Turns)))
	for i := range record.Turns {
		t := &record.Turns[i]
		e.string(t.Key)
		e.string(t.SessionID)
		e.string(string(t.Agent))
		stamp, err := t.Timestamp.MarshalBinary()
		if err != nil {
			return nil, err
		}
		e.uint(uint64(len(stamp)))
		e.buf = append(e.buf, stamp...)
		e.string(t.Model)
		e.string(t.Provider)
		e.string(t.Endpoint)
		e.string(t.Project)
		e.string(t.CommandDir)
		u := t.Usage
		for _, v := range [...]int64{u.Input, u.Output, u.CacheRead, u.CacheWrite, u.CacheWrite1h, u.Reasoning, u.ContextTokens, u.Unclassified} {
			e.int(v)
		}
		e.string(string(t.RuntimeAgent))
		e.string(t.RuntimeSessionID)
		e.string(t.ExcludedReason)
		e.string(t.ServiceTier)
		e.bool(t.Subagent)
		e.bool(t.Aggregate)
		e.string(t.UnpricedReason)
		e.bool(t.Credits != nil)
		if t.Credits != nil {
			e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(*t.Credits))
		}
		e.bool(t.ReplayHeuristic)
	}
	// a fixed trailer catches a file cut short at a field boundary
	e.buf = append(e.buf, "end"...)
	return e.buf, nil
}

type cacheDecoder struct {
	buf     []byte
	strings []string
	err     error
}

func (d *cacheDecoder) uint() uint64 {
	v, n := binary.Uvarint(d.buf)
	if n <= 0 {
		d.fail()
		return 0
	}
	d.buf = d.buf[n:]
	return v
}

func (d *cacheDecoder) int() int64 {
	v, n := binary.Varint(d.buf)
	if n <= 0 {
		d.fail()
		return 0
	}
	d.buf = d.buf[n:]
	return v
}

func (d *cacheDecoder) bytes(n uint64) []byte {
	if n > uint64(len(d.buf)) {
		d.fail()
		return nil
	}
	b := d.buf[:n]
	d.buf = d.buf[n:]
	return b
}

func (d *cacheDecoder) bool() bool {
	b := d.bytes(1)
	return len(b) == 1 && b[0] == 1
}

func (d *cacheDecoder) string() string {
	i := d.uint()
	switch {
	case d.err != nil:
		return ""
	case i < uint64(len(d.strings)):
		return d.strings[i]
	case i == uint64(len(d.strings)):
		s := string(d.bytes(d.uint()))
		d.strings = append(d.strings, s)
		return s
	}
	d.fail()
	return ""
}

func (d *cacheDecoder) fail() {
	if d.err == nil {
		d.err = errCacheCodec
	}
	d.buf = nil
}

func decodeCacheRecord(raw []byte) (scanCacheRecord, error) {
	d := cacheDecoder{buf: raw}
	var record scanCacheRecord
	record.Version = int(d.uint())
	record.Namespace = d.string()
	record.Fingerprint = d.string()
	n := d.uint()
	// every turn takes well over one byte, so a larger count is corruption
	if d.err != nil || n > uint64(len(d.buf)) {
		return record, errCacheCodec
	}
	record.Turns = make([]model.Turn, n)
	for i := range record.Turns {
		t := &record.Turns[i]
		t.Key = d.string()
		t.SessionID = d.string()
		t.Agent = model.Agent(d.string())
		if err := t.Timestamp.UnmarshalBinary(d.bytes(d.uint())); err != nil {
			d.fail()
		}
		t.Model = d.string()
		t.Provider = d.string()
		t.Endpoint = d.string()
		t.Project = d.string()
		t.CommandDir = d.string()
		u := &t.Usage
		for _, v := range [...]*int64{&u.Input, &u.Output, &u.CacheRead, &u.CacheWrite, &u.CacheWrite1h, &u.Reasoning, &u.ContextTokens, &u.Unclassified} {
			*v = d.int()
		}
		t.RuntimeAgent = model.Agent(d.string())
		t.RuntimeSessionID = d.string()
		t.ExcludedReason = d.string()
		t.ServiceTier = d.string()
		t.Subagent = d.bool()
		t.Aggregate = d.bool()
		t.UnpricedReason = d.string()
		if d.bool() {
			b := d.bytes(8)
			if len(b) == 8 {
				credits := math.Float64frombits(binary.LittleEndian.Uint64(b))
				t.Credits = &credits
			}
		}
		t.ReplayHeuristic = d.bool()
		if d.err != nil {
			return record, d.err
		}
	}
	if string(d.bytes(3)) != "end" || len(d.buf) != 0 || d.err != nil {
		return record, errCacheCodec
	}
	return record, nil
}
