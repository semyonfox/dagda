package ingest

import (
	"bufio"
	"compress/gzip"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/semyonfox/dagda/engine/internal/model"
)

// Exported records carry the turn identities the readers already use, so a
// report on one machine can dedupe another machine's history against its own.
// They are unpriced: prices, filters and project grouping are worked out by
// the machine that runs the report.
//
// The file is gzipped gob, which tolerates fields another dagda version added
// or dropped, unlike the scan cache's strict layout. On 618k real records that is
// 30MB and a 2s read, against 274MB and 7s for the same records as JSON, and
// the size is what crosses the network.
const (
	recordsFormat  = "dagda-records"
	recordsVersion = 2
)

type recordsHeader struct {
	Format  string
	Version int
	// Readers fingerprints the parser code. Identities only match when both
	// machines derive them the same way.
	Readers string
}

// localKeyPrefix marks identities that only hold on one machine, such as a
// path plus a line number. Two machines can produce the same one for
// different calls, so they are dropped on export rather than risk merging
// unrelated usage.
const localKeyPrefix = "local:"

func localKey(kind string, parts ...string) string {
	return identityKey(localKeyPrefix+kind, parts...)
}

// portablePath names a source the same way on every machine that keeps it at
// the same place under the home dir, whatever the home dir or separator, so a
// store copied from /home/a to /Users/a keeps its identities.
//
// Windows and macOS file systems ignore case by default, so C:\Users\Me and
// c:\users\me are one store and get one name there.
func portablePath(path string) string {
	path = filepath.Clean(path)
	home := homeDir()
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		path, home = strings.ToLower(path), strings.ToLower(home)
	}
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !filepath.IsAbs(rel) &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

// WriteRecords writes deduplicated turns in the export format.
func WriteRecords(w io.Writer, turns []model.Turn) error {
	out := make([]model.Turn, len(turns))
	for i, t := range turns {
		if strings.HasPrefix(t.Key, localKeyPrefix) {
			t.Key = ""
		}
		if t.Endpoint != "" {
			// an endpoint that can't be reduced to its origin may carry a
			// credential, so it stays on this machine
			t.Endpoint, _ = endpointOrigin(t.Endpoint)
		}
		out[i] = t
	}
	zw, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err != nil {
		return err
	}
	enc := gob.NewEncoder(zw)
	if err := enc.Encode(recordsHeader{Format: recordsFormat, Version: recordsVersion, Readers: parserVersion()}); err != nil {
		return err
	}
	if err := enc.Encode(out); err != nil {
		return err
	}
	return zw.Close()
}

// ReadRecords loads records written by WriteRecords. They come from another
// machine, so every record is checked the way a scanner checks a log.
// sameReaders is false when that machine ran different parser code, so some
// identities may not line up.
func ReadRecords(r io.Reader) (turns []model.Turn, sameReaders bool, err error) {
	zr, err := gzip.NewReader(bufio.NewReader(r))
	if err != nil {
		return nil, false, errors.New("not a dagda export (written by dagda export)")
	}
	dec := gob.NewDecoder(zr)
	var header recordsHeader
	if err := dec.Decode(&header); err != nil || header.Format != recordsFormat {
		return nil, false, errors.New("not a dagda export (written by dagda export)")
	}
	if header.Version != recordsVersion {
		return nil, false, fmt.Errorf("export version %d is not supported; export again with this dagda version", header.Version)
	}
	if err := dec.Decode(&turns); err != nil {
		return nil, false, fmt.Errorf("damaged export: %w", err)
	}
	for i := range turns {
		t := &turns[i]
		if t.Agent == "" {
			return nil, false, fmt.Errorf("record %d has no agent", i+1)
		}
		usage, ok := t.Usage.SanitizeAggregate()
		if !ok {
			return nil, false, fmt.Errorf("record %d has implausible token counts", i+1)
		}
		t.Usage = usage
		if strings.HasPrefix(t.Key, localKeyPrefix) {
			t.Key = ""
		}
		if t.Endpoint != "" {
			t.Endpoint, _ = endpointOrigin(t.Endpoint)
		}
	}
	return turns, header.Readers == parserVersion(), nil
}
