package verity

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/parquet-go/parquet-go"
)

type FormatKind string

const (
	FormatCSV     FormatKind = "csv"
	FormatTSV     FormatKind = "tsv"
	FormatJSON    FormatKind = "json"
	FormatJSONL   FormatKind = "jsonl"
	FormatParquet FormatKind = "parquet"
)

type InputFormat struct {
	Kind FormatKind `json:"kind"`
	Gzip bool       `json:"gzip"`
}

func (f InputFormat) String() string {
	if f.Gzip {
		return string(f.Kind) + ".gz"
	}
	return string(f.Kind)
}

func DetectFormat(filename string, header []byte) (InputFormat, error) {
	name := strings.ToLower(strings.TrimSpace(filename))
	gzipped := strings.HasSuffix(name, ".gz") || (len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b)
	if strings.HasSuffix(name, ".gz") {
		name = strings.TrimSuffix(name, ".gz")
	}
	if len(header) >= 4 && string(header[:4]) == "PAR1" {
		return InputFormat{Kind: FormatParquet}, nil
	}
	var kind FormatKind
	switch filepath.Ext(name) {
	case ".csv":
		kind = FormatCSV
	case ".tsv":
		kind = FormatTSV
	case ".json":
		kind = FormatJSON
	case ".jsonl", ".ndjson":
		kind = FormatJSONL
	case ".parquet":
		kind = FormatParquet
	default:
		trimmed := strings.TrimSpace(string(header))
		switch {
		case strings.HasPrefix(trimmed, "["):
			kind = FormatJSON
		case strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, "\n{"):
			kind = FormatJSONL
		case strings.HasPrefix(trimmed, "{"):
			kind = FormatJSON
		case strings.Contains(trimmed, "\t"):
			kind = FormatTSV
		case strings.Contains(trimmed, ","):
			kind = FormatCSV
		default:
			return InputFormat{}, fmt.Errorf("unsupported or ambiguous input format for %q", filename)
		}
	}
	if kind == FormatParquet && gzipped {
		return InputFormat{}, errors.New("gzip-compressed Parquet is not supported")
	}
	return InputFormat{Kind: kind, Gzip: gzipped}, nil
}

type RecordStream interface {
	Next(context.Context) (map[string]any, error)
	Close() error
}

func OpenRecordStream(input io.Reader, format InputFormat) (RecordStream, error) {
	if format.Gzip {
		reader, err := gzip.NewReader(input)
		if err != nil {
			return nil, fmt.Errorf("open gzip input: %w", err)
		}
		stream, err := openUncompressedRecordStream(reader, InputFormat{Kind: format.Kind})
		if err != nil {
			reader.Close()
			return nil, err
		}
		return &wrappedRecordStream{RecordStream: stream, close: reader.Close}, nil
	}
	return openUncompressedRecordStream(input, format)
}

func openUncompressedRecordStream(input io.Reader, format InputFormat) (RecordStream, error) {
	switch format.Kind {
	case FormatCSV, FormatTSV:
		reader := csv.NewReader(input)
		reader.FieldsPerRecord = -1
		reader.ReuseRecord = true
		if format.Kind == FormatTSV {
			reader.Comma = '\t'
		}
		header, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("read delimited header: %w", err)
		}
		headers := append([]string(nil), header...)
		if len(headers) > 0 {
			headers[0] = strings.TrimPrefix(headers[0], "\ufeff")
		}
		seen := make(map[string]struct{}, len(headers))
		for index := range headers {
			headers[index] = strings.TrimSpace(headers[index])
			if headers[index] == "" {
				return nil, fmt.Errorf("column %d has an empty header", index+1)
			}
			if _, exists := seen[headers[index]]; exists {
				return nil, fmt.Errorf("duplicate header %q", headers[index])
			}
			seen[headers[index]] = struct{}{}
		}
		return &csvRecordStream{reader: reader, headers: headers}, nil
	case FormatJSON:
		buffered := bufio.NewReader(input)
		array, err := jsonInputIsArray(buffered)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(buffered)
		if array {
			token, err := decoder.Token()
			if err != nil || token != json.Delim('[') {
				return nil, fmt.Errorf("open JSON array: %w", err)
			}
		}
		return &jsonRecordStream{decoder: decoder, array: array}, nil
	case FormatJSONL:
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
		return &jsonlRecordStream{scanner: scanner}, nil
	case FormatParquet:
		readerAt, ok := input.(io.ReaderAt)
		if !ok {
			return nil, errors.New("Parquet input must support random access")
		}
		seeker, ok := input.(io.Seeker)
		if !ok {
			return nil, errors.New("Parquet input must be seekable")
		}
		current, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, fmt.Errorf("inspect Parquet offset: %w", err)
		}
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, fmt.Errorf("inspect Parquet size: %w", err)
		}
		if _, err := seeker.Seek(current, io.SeekStart); err != nil {
			return nil, fmt.Errorf("restore Parquet offset: %w", err)
		}
		file, err := parquet.OpenFile(readerAt, size)
		if err != nil {
			return nil, fmt.Errorf("open Parquet input: %w", err)
		}
		return &parquetRecordStream{reader: parquet.NewGenericReader[any](file)}, nil
	default:
		return nil, fmt.Errorf("unsupported input format %q", format.Kind)
	}
}

type wrappedRecordStream struct {
	RecordStream
	close func() error
}

func (s *wrappedRecordStream) Close() error {
	streamErr := s.RecordStream.Close()
	closeErr := s.close()
	return errors.Join(streamErr, closeErr)
}

type csvRecordStream struct {
	reader  *csv.Reader
	headers []string
	row     int
}

func (s *csvRecordStream) Next(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := s.reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("read delimited row %d: %w", s.row+2, err)
	}
	s.row++
	if len(values) != len(s.headers) {
		return nil, fmt.Errorf("row %d has %d values; expected %d", s.row+1, len(values), len(s.headers))
	}
	result := make(map[string]any, len(s.headers))
	for index, header := range s.headers {
		result[header] = values[index]
	}
	return result, nil
}

func (s *csvRecordStream) Close() error { return nil }

func jsonInputIsArray(reader *bufio.Reader) (bool, error) {
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return false, fmt.Errorf("read JSON input: %w", err)
		}
		if err := reader.UnreadByte(); err != nil {
			return false, fmt.Errorf("inspect JSON input: %w", err)
		}
		if value == ' ' || value == '\n' || value == '\r' || value == '\t' {
			if _, err := reader.ReadByte(); err != nil {
				return false, err
			}
			continue
		}
		if value != '[' && value != '{' {
			return false, fmt.Errorf("JSON input must start with an object or array")
		}
		return value == '[', nil
	}
}

type jsonRecordStream struct {
	decoder *json.Decoder
	array   bool
	done    bool
}

func (s *jsonRecordStream) Next(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.done {
		return nil, io.EOF
	}
	if s.array && !s.decoder.More() {
		if _, err := s.decoder.Token(); err != nil {
			return nil, fmt.Errorf("close JSON array: %w", err)
		}
		s.done = true
		return nil, io.EOF
	}
	var result map[string]any
	if err := s.decoder.Decode(&result); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("decode JSON record: %w", err)
	}
	if !s.array {
		s.done = true
	}
	if result == nil {
		return nil, errors.New("JSON record must be an object")
	}
	return result, nil
}

func (s *jsonRecordStream) Close() error { return nil }

type jsonlRecordStream struct {
	scanner *bufio.Scanner
	line    int
}

func (s *jsonlRecordStream) Next(ctx context.Context) (map[string]any, error) {
	for s.scanner.Scan() {
		s.line++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(bytesTrimSpace(s.scanner.Bytes())) == 0 {
			continue
		}
		var result map[string]any
		if err := json.Unmarshal(s.scanner.Bytes(), &result); err != nil {
			return nil, fmt.Errorf("decode JSONL line %d: %w", s.line, err)
		}
		if result == nil {
			return nil, fmt.Errorf("decode JSONL line %d: record must be an object", s.line)
		}
		return result, nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan JSONL line %d: %w", s.line+1, err)
	}
	return nil, io.EOF
}

func (s *jsonlRecordStream) Close() error { return nil }

type parquetRecordStream struct {
	reader *parquet.GenericReader[any]
}

func (s *parquetRecordStream) Next(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values := make([]any, 1)
	count, err := s.reader.Read(values)
	if count == 0 {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		if err != nil {
			return nil, fmt.Errorf("read Parquet row: %w", err)
		}
		return nil, io.ErrNoProgress
	}
	result, ok := values[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Parquet row decoded as %T, expected object", values[0])
	}
	return result, nil
}

func (s *parquetRecordStream) Close() error { return s.reader.Close() }
