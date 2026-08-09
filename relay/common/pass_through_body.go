package common

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	basecommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
)

const maxCapturedJSONKeyBytes = 256

type jsonFilterScope uint8

const (
	jsonFilterNone jsonFilterScope = iota
	jsonFilterRoot
	jsonFilterStreamOptions
)

type jsonByteRange struct {
	start int64
	end   int64
}

type passThroughJSONScanner struct {
	reader *bufio.Reader
	offset int64
}

func newPassThroughFilteredBody(storage basecommon.BodyStorage, settings dto.ChannelOtherSettings) (*filteredReplayableBody, error) {
	if settings.AllowServiceTier &&
		settings.AllowInferenceGeo &&
		settings.AllowSpeed &&
		!settings.DisableStore &&
		settings.AllowSafetyIdentifier &&
		settings.AllowIncludeObfuscation {
		return nil, nil
	}

	reader, err := storage.NewReader()
	if err != nil {
		return nil, err
	}
	ranges, scannedSize, scanErr := scanPassThroughJSON(reader, settings)
	closeErr := reader.Close()
	if scanErr != nil {
		return nil, scanErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if scannedSize != storage.Size() {
		return nil, fmt.Errorf("pass-through body size changed while scanning: got %d, want %d", scannedSize, storage.Size())
	}
	if len(ranges) == 0 {
		return nil, nil
	}

	ranges, filteredSize, err := normalizeJSONRanges(ranges, storage.Size())
	if err != nil {
		return nil, err
	}
	return &filteredReplayableBody{storage: storage, ranges: ranges, size: filteredSize}, nil
}

func scanPassThroughJSON(reader io.Reader, settings dto.ChannelOtherSettings) ([]jsonByteRange, int64, error) {
	scanner := &passThroughJSONScanner{reader: bufio.NewReader(reader)}
	if err := scanner.skipWhitespace(); err != nil && !errors.Is(err, io.EOF) {
		return nil, scanner.offset, err
	}
	first, err := scanner.peekByte()
	if err != nil {
		return nil, scanner.offset, fmt.Errorf("scan pass-through JSON: %w", err)
	}
	if first != '{' {
		if err := scanner.parseValue(); err != nil {
			return nil, scanner.offset, err
		}
		if err := scanner.requireEOF(); err != nil {
			return nil, scanner.offset, err
		}
		return nil, scanner.offset, nil
	}

	ranges, _, _, err := scanner.parseObject(jsonFilterRoot, settings)
	if err != nil {
		return nil, scanner.offset, err
	}
	if err := scanner.requireEOF(); err != nil {
		return nil, scanner.offset, err
	}
	return ranges, scanner.offset, nil
}

func (s *passThroughJSONScanner) parseObject(scope jsonFilterScope, settings dto.ChannelOtherSettings) ([]jsonByteRange, int, bool, error) {
	if err := s.expectByte('{'); err != nil {
		return nil, 0, false, err
	}

	var ranges []jsonByteRange
	keptMembers := 0
	changed := false
	lastKeptComma := int64(-1)

	for {
		memberStart := s.offset
		if err := s.skipWhitespace(); err != nil {
			return nil, 0, false, err
		}
		next, err := s.peekByte()
		if err != nil {
			return nil, 0, false, fmt.Errorf("scan JSON object: %w", err)
		}
		if next == '}' {
			_, _ = s.readByte()
			return ranges, keptMembers, changed, nil
		}

		key, err := s.parseString(scope != jsonFilterNone)
		if err != nil {
			return nil, 0, false, err
		}
		if err := s.skipWhitespace(); err != nil {
			return nil, 0, false, err
		}
		if err := s.expectByte(':'); err != nil {
			return nil, 0, false, err
		}
		if err := s.skipWhitespace(); err != nil {
			return nil, 0, false, err
		}

		removeMember := shouldRemoveJSONMember(scope, key, settings)
		var childRanges []jsonByteRange
		childChanged := false
		if scope == jsonFilterRoot && key == "stream_options" && !settings.AllowIncludeObfuscation {
			valueStart, peekErr := s.peekByte()
			if peekErr != nil {
				return nil, 0, false, peekErr
			}
			if valueStart == '{' {
				var childKept int
				childRanges, childKept, childChanged, err = s.parseObject(jsonFilterStreamOptions, settings)
				if err != nil {
					return nil, 0, false, err
				}
				if childChanged && childKept == 0 {
					removeMember = true
					childRanges = nil
				}
			} else if err := s.parseValue(); err != nil {
				return nil, 0, false, err
			}
		} else if err := s.parseValue(); err != nil {
			return nil, 0, false, err
		}

		valueEnd := s.offset
		if err := s.skipWhitespace(); err != nil {
			return nil, 0, false, err
		}
		commaStart := s.offset
		delimiter, err := s.readByte()
		if err != nil {
			return nil, 0, false, fmt.Errorf("scan JSON object delimiter: %w", err)
		}
		if delimiter != ',' && delimiter != '}' {
			return nil, 0, false, fmt.Errorf("invalid JSON object delimiter %q at byte %d", delimiter, s.offset-1)
		}

		if removeMember {
			changed = true
			if delimiter == ',' {
				ranges = append(ranges, jsonByteRange{start: memberStart, end: s.offset})
			} else if lastKeptComma >= 0 {
				ranges = append(ranges, jsonByteRange{start: lastKeptComma, end: valueEnd})
			} else {
				ranges = append(ranges, jsonByteRange{start: memberStart, end: valueEnd})
			}
		} else {
			keptMembers++
			if childChanged {
				changed = true
				ranges = append(ranges, childRanges...)
			}
			if delimiter == ',' {
				lastKeptComma = commaStart
			}
		}

		if delimiter == '}' {
			return ranges, keptMembers, changed, nil
		}
	}
}

func shouldRemoveJSONMember(scope jsonFilterScope, key string, settings dto.ChannelOtherSettings) bool {
	switch scope {
	case jsonFilterRoot:
		switch key {
		case "service_tier":
			return !settings.AllowServiceTier
		case "inference_geo":
			return !settings.AllowInferenceGeo
		case "speed":
			return !settings.AllowSpeed
		case "store":
			return settings.DisableStore
		case "safety_identifier":
			return !settings.AllowSafetyIdentifier
		}
	case jsonFilterStreamOptions:
		return key == "include_obfuscation" && !settings.AllowIncludeObfuscation
	}
	return false
}

func (s *passThroughJSONScanner) parseValue() error {
	if err := s.skipWhitespace(); err != nil {
		return err
	}
	next, err := s.peekByte()
	if err != nil {
		return fmt.Errorf("scan JSON value: %w", err)
	}
	switch next {
	case '{':
		_, _, _, err = s.parseObject(jsonFilterNone, dto.ChannelOtherSettings{})
		return err
	case '[':
		return s.parseArray()
	case '"':
		_, err = s.parseString(false)
		return err
	case 't':
		return s.consumeLiteral("true")
	case 'f':
		return s.consumeLiteral("false")
	case 'n':
		return s.consumeLiteral("null")
	default:
		if next == '-' || (next >= '0' && next <= '9') {
			return s.consumeNumber()
		}
		return fmt.Errorf("invalid JSON value at byte %d", s.offset)
	}
}

func (s *passThroughJSONScanner) parseArray() error {
	if err := s.expectByte('['); err != nil {
		return err
	}
	if err := s.skipWhitespace(); err != nil {
		return err
	}
	next, err := s.peekByte()
	if err != nil {
		return err
	}
	if next == ']' {
		_, _ = s.readByte()
		return nil
	}
	for {
		if err := s.parseValue(); err != nil {
			return err
		}
		if err := s.skipWhitespace(); err != nil {
			return err
		}
		delimiter, err := s.readByte()
		if err != nil {
			return err
		}
		switch delimiter {
		case ']':
			return nil
		case ',':
			continue
		default:
			return fmt.Errorf("invalid JSON array delimiter %q at byte %d", delimiter, s.offset-1)
		}
	}
}

func (s *passThroughJSONScanner) parseString(capture bool) (string, error) {
	if err := s.expectByte('"'); err != nil {
		return "", err
	}
	raw := make([]byte, 0, 32)
	if capture {
		raw = append(raw, '"')
	}
	overflow := false
	escaped := false
	for {
		value, err := s.readByte()
		if err != nil {
			return "", fmt.Errorf("scan JSON string: %w", err)
		}
		if capture && !overflow {
			if len(raw) >= maxCapturedJSONKeyBytes {
				overflow = true
				raw = nil
			} else {
				raw = append(raw, value)
			}
		}
		if escaped {
			escaped = false
			continue
		}
		if value == '\\' {
			escaped = true
			continue
		}
		if value == '"' {
			break
		}
		if value < 0x20 {
			return "", fmt.Errorf("invalid control character in JSON string at byte %d", s.offset-1)
		}
	}
	if !capture || overflow {
		return "", nil
	}
	var decoded string
	if err := basecommon.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("decode JSON object key: %w", err)
	}
	return decoded, nil
}

func (s *passThroughJSONScanner) consumeLiteral(literal string) error {
	for i := 0; i < len(literal); i++ {
		value, err := s.readByte()
		if err != nil {
			return err
		}
		if value != literal[i] {
			return fmt.Errorf("invalid JSON literal at byte %d", s.offset-1)
		}
	}
	return nil
}

func (s *passThroughJSONScanner) consumeNumber() error {
	consumed := false
	for {
		value, err := s.peekByte()
		if errors.Is(err, io.EOF) {
			if consumed {
				return nil
			}
			return err
		}
		if err != nil {
			return err
		}
		if (value >= '0' && value <= '9') || value == '-' || value == '+' || value == '.' || value == 'e' || value == 'E' {
			_, _ = s.readByte()
			consumed = true
			continue
		}
		if isJSONWhitespace(value) || value == ',' || value == ']' || value == '}' {
			return nil
		}
		return fmt.Errorf("invalid JSON number at byte %d", s.offset)
	}
}

func (s *passThroughJSONScanner) requireEOF() error {
	if err := s.skipWhitespace(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if _, err := s.peekByte(); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected data after JSON value at byte %d", s.offset)
	}
	return nil
}

func (s *passThroughJSONScanner) skipWhitespace() error {
	for {
		value, err := s.peekByte()
		if err != nil {
			return err
		}
		if !isJSONWhitespace(value) {
			return nil
		}
		_, _ = s.readByte()
	}
}

func isJSONWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func (s *passThroughJSONScanner) expectByte(expected byte) error {
	value, err := s.readByte()
	if err != nil {
		return err
	}
	if value != expected {
		return fmt.Errorf("expected %q at byte %d, got %q", expected, s.offset-1, value)
	}
	return nil
}

func (s *passThroughJSONScanner) peekByte() (byte, error) {
	data, err := s.reader.Peek(1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (s *passThroughJSONScanner) readByte() (byte, error) {
	value, err := s.reader.ReadByte()
	if err == nil {
		s.offset++
	}
	return value, err
}

func normalizeJSONRanges(ranges []jsonByteRange, sourceSize int64) ([]jsonByteRange, int64, error) {
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start < ranges[j].start
	})
	merged := make([]jsonByteRange, 0, len(ranges))
	for _, current := range ranges {
		if current.start < 0 || current.end <= current.start || current.end > sourceSize {
			return nil, 0, fmt.Errorf("invalid JSON filter range [%d,%d) for body size %d", current.start, current.end, sourceSize)
		}
		if len(merged) > 0 && current.start <= merged[len(merged)-1].end {
			if current.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = current.end
			}
			continue
		}
		merged = append(merged, current)
	}
	filteredSize := sourceSize
	for _, current := range merged {
		filteredSize -= current.end - current.start
	}
	return merged, filteredSize, nil
}

type filteredReplayableBody struct {
	storage basecommon.BodyStorage
	ranges  []jsonByteRange
	size    int64

	mu      sync.Mutex
	current io.ReadCloser
	closed  bool
}

func (b *filteredReplayableBody) Size() int64 {
	return b.size
}

func (b *filteredReplayableBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	if b.current == nil {
		reader, err := b.newReader()
		if err != nil {
			return 0, err
		}
		b.current = reader
	}
	return b.current.Read(p)
}

func (b *filteredReplayableBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	if b.current == nil {
		return nil
	}
	err := b.current.Close()
	b.current = nil
	return err
}

func (b *filteredReplayableBody) NewReader() (io.ReadCloser, error) {
	return b.newReader()
}

func (b *filteredReplayableBody) newReader() (io.ReadCloser, error) {
	source, err := b.storage.NewReader()
	if err != nil {
		return nil, err
	}
	return &rangeFilteringReader{source: source, ranges: b.ranges}, nil
}

type rangeFilteringReader struct {
	source   io.ReadCloser
	ranges   []jsonByteRange
	position int64
	index    int
}

func (r *rangeFilteringReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		for r.index < len(r.ranges) && r.position >= r.ranges[r.index].end {
			r.index++
		}
		if r.index < len(r.ranges) && r.position >= r.ranges[r.index].start {
			toSkip := r.ranges[r.index].end - r.position
			skipped, err := io.CopyN(io.Discard, r.source, toSkip)
			r.position += skipped
			if err != nil {
				return 0, fmt.Errorf("skip filtered JSON field: %w", err)
			}
			continue
		}

		limit := len(p)
		if r.index < len(r.ranges) {
			untilRange := r.ranges[r.index].start - r.position
			if untilRange < int64(limit) {
				limit = int(untilRange)
			}
		}
		if limit == 0 {
			continue
		}
		n, err := r.source.Read(p[:limit])
		r.position += int64(n)
		return n, err
	}
}

func (r *rangeFilteringReader) Close() error {
	return r.source.Close()
}
