/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package l4

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RESP2 protocol type prefixes
const (
	RESPSimpleString = '+'
	RESPError        = '-'
	RESPInteger      = ':'
	RESPBulkString   = '$'
	RESPArray        = '*'
)

// Common errors for RESP protocol parsing
var (
	ErrInvalidRESP     = errors.New("invalid RESP data")
	ErrUnexpectedType  = errors.New("unexpected RESP type")
	ErrProtocolError   = errors.New("RESP protocol error")
	ErrNilBulkString   = errors.New("nil bulk string")
	ErrInvalidArrayLen = errors.New("invalid array length")
)

// RESPType represents the type of a RESP value
type RESPType byte

const (
	// RESPTypeSimpleString is a simple string response (+OK\r\n)
	RESPTypeSimpleString RESPType = RESPSimpleString
	// RESPTypeError is an error response (-ERR message\r\n)
	RESPTypeError RESPType = RESPError
	// RESPTypeInteger is an integer response (:1\r\n)
	RESPTypeInteger RESPType = RESPInteger
	// RESPTypeBulkString is a bulk string response ($6\r\nfoobar\r\n)
	RESPTypeBulkString RESPType = RESPBulkString
	// RESPTypeArray is an array response (*2\r\n...)
	RESPTypeArray RESPType = RESPArray
	// RESPTypeNil represents a nil/null value
	RESPTypeNil RESPType = 0
)

// RESPValue represents a parsed RESP protocol value
type RESPValue struct {
	Type    RESPType
	Str     string
	Int     int64
	Array   []RESPValue
	IsNil   bool
	RawData []byte // The raw bytes for this value (for forwarding)
}

// RESPReader reads and parses RESP protocol messages from a buffered reader
type RESPReader struct {
	reader *bufio.Reader
}

// NewRESPReader creates a new RESP protocol reader
func NewRESPReader(r io.Reader) *RESPReader {
	return &RESPReader{
		reader: bufio.NewReaderSize(r, 64*1024),
	}
}

// ReadValue reads a single RESP value from the stream
func (r *RESPReader) ReadValue() (RESPValue, error) {
	return r.readValue()
}

// ReadCommand reads a Redis command from the stream.
// It handles both inline commands and RESP array commands.
// Returns the command as an array of strings.
func (r *RESPReader) ReadCommand() ([]string, []byte, error) {
	// Peek at the first byte to determine format
	firstByte, err := r.reader.Peek(1)
	if err != nil {
		return nil, nil, fmt.Errorf("read command: %w", err)
	}

	if firstByte[0] == RESPArray {
		return r.readRESPCommand()
	}
	return r.readInlineCommand()
}

// readRESPCommand reads a full RESP array command and returns the command parts and raw bytes
func (r *RESPReader) readRESPCommand() ([]string, []byte, error) {
	val, err := r.readValue()
	if err != nil {
		return nil, nil, fmt.Errorf("read RESP command: %w", err)
	}

	if val.Type != RESPTypeArray {
		return nil, val.RawData, fmt.Errorf("%w: expected array, got %c", ErrUnexpectedType, byte(val.Type))
	}

	parts := make([]string, 0, len(val.Array))
	for i := range val.Array {
		if val.Array[i].Type == RESPTypeBulkString {
			parts = append(parts, val.Array[i].Str)
		} else {
			parts = append(parts, val.Array[i].Str)
		}
	}

	return parts, val.RawData, nil
}

// readInlineCommand reads an inline (plain text) Redis command
func (r *RESPReader) readInlineCommand() ([]string, []byte, error) {
	line, err := r.readLine()
	if err != nil {
		return nil, nil, fmt.Errorf("read inline command: %w", err)
	}

	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" {
		return nil, line, fmt.Errorf("%w: empty inline command", ErrProtocolError)
	}

	parts := strings.Fields(trimmed)
	// Build raw data with CRLF
	raw := make([]byte, 0, len(line)+2)
	raw = append(raw, line...)
	raw = append(raw, '\r', '\n')

	return parts, raw, nil
}

// readValue reads a single RESP value, tracking raw bytes
func (r *RESPReader) readValue() (RESPValue, error) {
	typeByte, err := r.reader.ReadByte()
	if err != nil {
		return RESPValue{}, fmt.Errorf("read type byte: %w", err)
	}

	switch typeByte {
	case RESPSimpleString:
		return r.readSimpleString()
	case RESPError:
		return r.readError()
	case RESPInteger:
		return r.readInteger()
	case RESPBulkString:
		return r.readBulkString()
	case RESPArray:
		return r.readArray()
	default:
		return RESPValue{}, fmt.Errorf("%w: unknown type prefix '%c' (0x%02x)", ErrInvalidRESP, typeByte, typeByte)
	}
}

// readSimpleString reads a simple string (+OK\r\n)
func (r *RESPReader) readSimpleString() (RESPValue, error) {
	line, err := r.readLine()
	if err != nil {
		return RESPValue{}, fmt.Errorf("read simple string: %w", err)
	}

	raw := make([]byte, 0, 1+len(line)+2)
	raw = append(raw, RESPSimpleString)
	raw = append(raw, line...)
	raw = append(raw, '\r', '\n')

	return RESPValue{
		Type:    RESPTypeSimpleString,
		Str:     string(line),
		RawData: raw,
	}, nil
}

// readError reads an error response (-ERR message\r\n)
func (r *RESPReader) readError() (RESPValue, error) {
	line, err := r.readLine()
	if err != nil {
		return RESPValue{}, fmt.Errorf("read error: %w", err)
	}

	raw := make([]byte, 0, 1+len(line)+2)
	raw = append(raw, RESPError)
	raw = append(raw, line...)
	raw = append(raw, '\r', '\n')

	return RESPValue{
		Type:    RESPTypeError,
		Str:     string(line),
		RawData: raw,
	}, nil
}

// readInteger reads an integer response (:123\r\n)
func (r *RESPReader) readInteger() (RESPValue, error) {
	line, err := r.readLine()
	if err != nil {
		return RESPValue{}, fmt.Errorf("read integer: %w", err)
	}

	val, err := strconv.ParseInt(string(line), 10, 64)
	if err != nil {
		return RESPValue{}, fmt.Errorf("parse integer %q: %w", string(line), err)
	}

	raw := make([]byte, 0, 1+len(line)+2)
	raw = append(raw, RESPInteger)
	raw = append(raw, line...)
	raw = append(raw, '\r', '\n')

	return RESPValue{
		Type:    RESPTypeInteger,
		Int:     val,
		Str:     string(line),
		RawData: raw,
	}, nil
}

// readBulkString reads a bulk string ($6\r\nfoobar\r\n or $-1\r\n for nil)
func (r *RESPReader) readBulkString() (RESPValue, error) {
	line, err := r.readLine()
	if err != nil {
		return RESPValue{}, fmt.Errorf("read bulk string length: %w", err)
	}

	length, err := strconv.ParseInt(string(line), 10, 64)
	if err != nil {
		return RESPValue{}, fmt.Errorf("parse bulk string length %q: %w", string(line), err)
	}

	// Nil bulk string
	if length < 0 {
		raw := make([]byte, 0, 1+len(line)+2)
		raw = append(raw, RESPBulkString)
		raw = append(raw, line...)
		raw = append(raw, '\r', '\n')

		return RESPValue{
			Type:    RESPTypeNil,
			IsNil:   true,
			RawData: raw,
		}, nil
	}

	// Read the bulk data plus trailing \r\n
	data := make([]byte, length+2)
	if _, err := io.ReadFull(r.reader, data); err != nil {
		return RESPValue{}, fmt.Errorf("read bulk string data: %w", err)
	}

	// Verify trailing \r\n
	if data[length] != '\r' || data[length+1] != '\n' {
		return RESPValue{}, fmt.Errorf("%w: bulk string missing CRLF terminator", ErrProtocolError)
	}

	raw := make([]byte, 0, 1+len(line)+2+int(length)+2)
	raw = append(raw, RESPBulkString)
	raw = append(raw, line...)
	raw = append(raw, '\r', '\n')
	raw = append(raw, data...)

	return RESPValue{
		Type:    RESPTypeBulkString,
		Str:     string(data[:length]),
		RawData: raw,
	}, nil
}

// readArray reads an array (*2\r\n... or *-1\r\n for nil)
func (r *RESPReader) readArray() (RESPValue, error) {
	line, err := r.readLine()
	if err != nil {
		return RESPValue{}, fmt.Errorf("read array length: %w", err)
	}

	count, err := strconv.ParseInt(string(line), 10, 64)
	if err != nil {
		return RESPValue{}, fmt.Errorf("parse array length %q: %w", string(line), err)
	}

	// Nil array
	if count < 0 {
		raw := make([]byte, 0, 1+len(line)+2)
		raw = append(raw, RESPArray)
		raw = append(raw, line...)
		raw = append(raw, '\r', '\n')

		return RESPValue{
			Type:    RESPTypeNil,
			IsNil:   true,
			RawData: raw,
		}, nil
	}

	if count > 1024*1024 {
		return RESPValue{}, fmt.Errorf("%w: array too large: %d elements", ErrInvalidArrayLen, count)
	}

	raw := make([]byte, 0, 1+len(line)+2)
	raw = append(raw, RESPArray)
	raw = append(raw, line...)
	raw = append(raw, '\r', '\n')

	elements := make([]RESPValue, 0, count)
	for i := int64(0); i < count; i++ {
		val, readErr := r.readValue()
		if readErr != nil {
			return RESPValue{}, fmt.Errorf("read array element %d: %w", i, readErr)
		}
		raw = append(raw, val.RawData...)
		elements = append(elements, val)
	}

	return RESPValue{
		Type:    RESPTypeArray,
		Array:   elements,
		RawData: raw,
	}, nil
}

// readLine reads a line terminated by \r\n, returning the line without the terminator
func (r *RESPReader) readLine() ([]byte, error) {
	line, err := r.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, fmt.Errorf("%w: line not terminated by CRLF", ErrProtocolError)
	}
	return line[:len(line)-2], nil
}

// RESPWriter writes RESP protocol messages
type RESPWriter struct {
	writer *bufio.Writer
}

// NewRESPWriter creates a new RESP protocol writer
func NewRESPWriter(w io.Writer) *RESPWriter {
	return &RESPWriter{
		writer: bufio.NewWriterSize(w, 64*1024),
	}
}

// WriteSimpleString writes a simple string response (+OK\r\n)
func (w *RESPWriter) WriteSimpleString(s string) error {
	if err := w.writer.WriteByte(RESPSimpleString); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(s); err != nil {
		return err
	}
	_, err := w.writer.WriteString("\r\n")
	return err
}

// WriteError writes an error response (-ERR message\r\n)
func (w *RESPWriter) WriteError(msg string) error {
	if err := w.writer.WriteByte(RESPError); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(msg); err != nil {
		return err
	}
	_, err := w.writer.WriteString("\r\n")
	return err
}

// WriteInteger writes an integer response (:123\r\n)
func (w *RESPWriter) WriteInteger(val int64) error {
	if err := w.writer.WriteByte(RESPInteger); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.FormatInt(val, 10)); err != nil {
		return err
	}
	_, err := w.writer.WriteString("\r\n")
	return err
}

// WriteBulkString writes a bulk string response ($6\r\nfoobar\r\n)
func (w *RESPWriter) WriteBulkString(s string) error {
	if err := w.writer.WriteByte(RESPBulkString); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(len(s))); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(s); err != nil {
		return err
	}
	_, err := w.writer.WriteString("\r\n")
	return err
}

// WriteNilBulkString writes a nil bulk string response ($-1\r\n)
func (w *RESPWriter) WriteNilBulkString() error {
	_, err := w.writer.WriteString("$-1\r\n")
	return err
}

// WriteArray writes an array header (*N\r\n) - elements must be written separately
func (w *RESPWriter) WriteArray(count int) error {
	if err := w.writer.WriteByte(RESPArray); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(count)); err != nil {
		return err
	}
	_, err := w.writer.WriteString("\r\n")
	return err
}

// WriteRaw writes raw bytes directly to the output
func (w *RESPWriter) WriteRaw(data []byte) error {
	_, err := w.writer.Write(data)
	return err
}

// Flush flushes the buffered writer
func (w *RESPWriter) Flush() error {
	return w.writer.Flush()
}

// EncodeCommand encodes a Redis command as a RESP array of bulk strings
func EncodeCommand(args ...string) []byte {
	var buf []byte
	buf = append(buf, '*')
	buf = append(buf, []byte(strconv.Itoa(len(args)))...)
	buf = append(buf, '\r', '\n')
	for _, arg := range args {
		buf = append(buf, '$')
		buf = append(buf, []byte(strconv.Itoa(len(arg)))...)
		buf = append(buf, '\r', '\n')
		buf = append(buf, []byte(arg)...)
		buf = append(buf, '\r', '\n')
	}
	return buf
}

// EncodeSimpleString encodes a RESP simple string
func EncodeSimpleString(s string) []byte {
	buf := make([]byte, 0, 1+len(s)+2)
	buf = append(buf, RESPSimpleString)
	buf = append(buf, []byte(s)...)
	buf = append(buf, '\r', '\n')
	return buf
}

// EncodeError encodes a RESP error
func EncodeError(msg string) []byte {
	buf := make([]byte, 0, 1+len(msg)+2)
	buf = append(buf, RESPError)
	buf = append(buf, []byte(msg)...)
	buf = append(buf, '\r', '\n')
	return buf
}
