// Package dbf provides functionality for reading DBF (dBase, FoxPro, Visual FoxPro) files.
//
// The package supports various DBF file formats and encodings, with automatic
// encoding detection based on Language Driver ID when available.
//
// Basic usage:
//
//	reader, err := dbf.NewFromFile("data.dbf", dbf.WithCP866())
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer reader.Close()
//
//	records, err := reader.ReadAll()
//	if err != nil {
//		log.Fatal(err)
//	}
//
// For large files, use streaming to avoid loading everything into memory:
//
//	for reader.Next() {
//		record, err := reader.Read()
//		if err != nil {
//			log.Fatal(err)
//		}
//		// process record
//	}
package dbf

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

// Sentinel errors returned by the library.
var (
	ErrInvalidFileType     = errors.New("invalid file type")
	ErrInvalidHeaderSize   = errors.New("invalid header size")
	ErrInvalidRecordSize   = errors.New("invalid record size")
	ErrUnknownEncoding     = errors.New("unable to determine encoding")
	ErrInvalidTerminator   = errors.New("invalid field descriptor terminator")
	ErrFieldOutOfBounds    = errors.New("field exceeds record bounds")
	ErrReadBeforeNext      = errors.New("Read called before Next")
	ErrRecordSizeMismatch  = errors.New("record size mismatch")
	ErrUnsupportedType     = errors.New("unsupported field type")
	ErrInvalidDeletionFlag = errors.New("invalid deletion flag")
	ErrInvalidFieldName    = errors.New("invalid field name")
)

// FileType represents the type of DBF file format.
type FileType byte

// String returns a human-readable description of the file type.
func (ft FileType) String() string {
	switch ft {
	case FoxBASE:
		return "FoxBASE"
	case FoxBASEPlusNoMemo:
		return "FoxBASE+/Dbase III plus, no memo"
	case dBASEIVNoMemo:
		return "dBASE IV, no memo"
	case dBASEVNoMemo:
		return "dBASE 5, no memo"
	case VisualObjects:
		return "Visual Objects 1.0"
	case VisualFoxPro:
		return "Visual FoxPro"
	case VisualFoxProAI:
		return "Visual FoxPro, autoincrement enabled"
	case VisualFoxProVarchar:
		return "Visual FoxPro with field type Varchar or Varbinary"
	case dBASEIVTF:
		return "dBASE IV SQL table files, no memo"
	case dBASEIVSF:
		return "dBASE IV SQL system files, no memo"
	case dBASEIVMemo2:
		return "dBASE IV with memo"
	case FoxBASEPlusMemo:
		return "FoxBASE+/dBASE III PLUS, with memo"
	case VisualObjectsMemo:
		return "Visual Objects 1.0 with memo"
	case dBASEIVMemo:
		return "dBASE IV with memo"
	case dBASEIVSQL:
		return "dBASE IV with SQL table"
	case dBASEIVTFMemo:
		return "dBASE IV SQL table files with memo"
	case HiPerSix:
		return "HiPer-Six format with SMT memo file"
	case FoxPro2:
		return "FoxPro 2.x (or earlier) with memo"
	case FoxBASE2:
		return "FoxBASE"
	default:
		return fmt.Sprintf("Unknown (0x%02X)", byte(ft))
	}
}

// Supported DBF file type constants.
const (
	FoxBASE             FileType = 0x02
	FoxBASEPlusNoMemo   FileType = 0x03
	dBASEIVNoMemo       FileType = 0x04
	dBASEVNoMemo        FileType = 0x05
	VisualObjects       FileType = 0x07
	VisualFoxPro        FileType = 0x30
	VisualFoxProAI      FileType = 0x31
	VisualFoxProVarchar FileType = 0x32
	dBASEIVTF           FileType = 0x43
	dBASEIVSF           FileType = 0x63
	dBASEIVMemo2        FileType = 0x7B
	FoxBASEPlusMemo     FileType = 0x83
	VisualObjectsMemo   FileType = 0x87
	dBASEIVMemo         FileType = 0x8B
	dBASEIVSQL          FileType = 0x8E
	dBASEIVTFMemo       FileType = 0xCB
	HiPerSix            FileType = 0xE5
	FoxPro2             FileType = 0xF5
	FoxBASE2            FileType = 0xFB
)

const (
	metadataLength uint16 = 32 // size of DBF file header in bytes
	fieldLength    uint16 = 32 // size of field descriptor in bytes
)

// Field represents a single field definition in a DBF table.
type Field struct {
	Name          string // field name (max 11 characters)
	Type          byte   // field type (C=Character, N=Numeric, D=Date, L=Logical, M=Memo, F=Float, ...)
	MemoryAddress uint32 // memory address (reserved, not used in file-based DBF)
	Length        uint16 // field length in bytes; uint16 to support VFP character fields > 255 bytes
	DecimalCount  byte   // number of decimal places (for numeric fields)
	Flags         byte   // Visual FoxPro field flags (0x01 system, 0x02 nullable, 0x04 binary); 0 for other formats
}

// Visual FoxPro field flags (byte 18 of the field descriptor).
const (
	flagSystem   byte = 0x01 // hidden system column, e.g. _NullFlags
	flagNullable byte = 0x02 // column can store null values
	flagBinary   byte = 0x04 // binary column (Character, Varchar and Memo only)
)

// julianDayUnixEpoch is the Julian day number of 1970-01-01.
const julianDayUnixEpoch = 2440588

// TypeString returns a human-readable description of the field type.
func (f Field) TypeString() string {
	switch f.Type {
	case 'C':
		return "Character"
	case 'N':
		return "Numeric"
	case 'D':
		return "Date"
	case 'L':
		return "Logical"
	case 'M':
		return "Memo"
	case 'F':
		return "Float"
	case 'I':
		return "Integer"
	case 'B':
		return "Double"
	case 'Y':
		return "Currency"
	case 'T':
		return "DateTime"
	case 'G':
		return "General"
	case 'P':
		return "Picture"
	case 'V':
		return "Varchar"
	case 'Q':
		return "Varbinary"
	case 'W':
		return "Blob"
	case '0':
		return "Null flags"
	default:
		return fmt.Sprintf("Unknown (%c)", f.Type)
	}
}

// Reader provides methods for reading DBF files.
// It supports both streaming (Next/Read) and batch (ReadAll) reading modes.
//
// Reader is not safe for concurrent use.
type Reader struct {
	fileType          FileType
	lastUpdate        time.Time
	recordsCount      uint32
	headerBytesNumber uint16
	recordBytesNumber uint16
	fields            []Field
	fieldBits         []fieldBits // per-field positions in the VFP _NullFlags bitmap
	nullFlagsStart    int         // offset of the VFP _NullFlags field within a record
	nullFlagsEnd      int         // end offset of the _NullFlags field; 0 if the table has none

	decoder       *encoding.Decoder
	strict        bool
	reader        *bufio.Reader
	currentRecord uint32 // number of records consumed from the input by Next()
	pending       bool   // Next() loaded a record into buf that Read() has not yet returned
	buf           []byte // raw bytes of the record loaded by Next()
	err           error  // last error during reading

	file *os.File
}

// Option is a functional option for configuring a Reader.
type Option func(*Reader)

// WithDecoder sets a custom text encoding decoder for reading character fields.
// This is the most flexible option, allowing any encoding.Decoder to be used.
// A nil decoder is ignored; pass WithEncoding or WithCP* to set encoding explicitly.
//
// Errors returned by the decoder are not hidden: a failure on a field name
// is returned by New, and a failure on a field value by Read (and Err).
func WithDecoder(decoder *encoding.Decoder) Option {
	return func(r *Reader) {
		if decoder != nil {
			r.decoder = decoder
		}
	}
}

// WithEncoding sets the text encoding using a charmap.Charmap.
// This is a convenience wrapper around WithDecoder.
func WithEncoding(cm *charmap.Charmap) Option {
	return WithDecoder(cm.NewDecoder())
}

// WithCP866 sets the encoding to Code Page 866 (Russian MS-DOS).
// This is commonly used for Russian DBF files created in DOS.
func WithCP866() Option {
	return WithEncoding(charmap.CodePage866)
}

// WithCP1251 sets the encoding to Windows-1251 (Russian Windows).
// This is commonly used for Russian DBF files created in Windows.
func WithCP1251() Option {
	return WithEncoding(charmap.Windows1251)
}

// WithCP1252 sets the encoding to Windows-1252 (Western European).
// This is the default Windows encoding for Western European languages.
func WithCP1252() Option {
	return WithEncoding(charmap.Windows1252)
}

// WithStrict enables strict validation of record data. Currently a record
// whose deletion flag is neither ' ' (0x20) nor '*' (0x2A) stops reading
// with ErrInvalidDeletionFlag. By default any byte other than '*' marks
// an active record.
func WithStrict() Option {
	return func(r *Reader) {
		r.strict = true
	}
}

// New creates a new DBF Reader from an io.Reader.
//
// If no encoding is specified via options, the reader will attempt to
// auto-detect the encoding from the Language Driver ID byte in the DBF header.
// If auto-detection fails, an error is returned.
//
// Example:
//
//	file, _ := os.Open("data.dbf")
//	reader, err := dbf.New(file, dbf.WithCP866())
func New(r io.Reader, opts ...Option) (*Reader, error) {
	reader := &Reader{
		reader: bufio.NewReader(r),
	}

	// apply options
	for _, opt := range opts {
		opt(reader)
	}

	// read file metadata (header)
	if err := reader.readMetadata(); err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	// ensure we have an encoding
	if reader.decoder == nil {
		return nil, fmt.Errorf("%w: please specify encoding explicitly using WithCP866(), WithCP1251() or WithEncoding()", ErrUnknownEncoding)
	}

	// read field descriptors
	if err := reader.readFields(); err != nil {
		return nil, fmt.Errorf("read fields: %w", err)
	}

	return reader, nil
}

// NewFromFile creates a new DBF Reader from a file path.
// This is a convenience wrapper around New() for file-based reading.
//
// Example:
//
//	reader, err := dbf.NewFromFile("data.dbf", dbf.WithCP866())
func NewFromFile(path string, opts ...Option) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	reader, err := New(file, opts...)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	reader.file = file
	return reader, nil
}

// fieldBits holds a field's bit positions in the VFP _NullFlags bitmap;
// -1 means the field has no such bit.
type fieldBits struct {
	varLength int // set when a Varchar/Varbinary value is shorter than the field
	null      int // set when the value is null
}

// FileType returns the DBF file type identifier.
func (r *Reader) FileType() FileType {
	return r.fileType
}

// LastUpdate returns the date when the DBF file was last modified.
func (r *Reader) LastUpdate() time.Time {
	return r.lastUpdate
}

// RecordsCount returns the total number of records in the DBF file,
// including deleted records.
func (r *Reader) RecordsCount() uint32 {
	return r.recordsCount
}

// Fields returns a copy of the field definitions for the DBF table.
// Modifying the returned slice does not affect the Reader.
func (r *Reader) Fields() []Field {
	return slices.Clone(r.fields)
}

// FieldsCount returns the number of fields in the DBF table.
func (r *Reader) FieldsCount() int {
	return len(r.fields)
}

// readMetadata reads and parses the 32-byte DBF file header.
func (r *Reader) readMetadata() error {
	// read file type (1 byte)
	b, err := r.reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read file type: %w", err)
	}

	fileType := FileType(b)
	if !isValidFileType(fileType) {
		return fmt.Errorf("%w: 0x%02X", ErrInvalidFileType, b)
	}
	r.fileType = fileType

	// read last update date (3 bytes: YY MM DD)
	dateBytes := make([]byte, 3)
	if _, err := io.ReadFull(r.reader, dateBytes); err != nil {
		return fmt.Errorf("read last update date: %w", err)
	}

	// dBase stores year as offset from 1900; valid range is 1900–2155
	r.lastUpdate = time.Date(
		int(dateBytes[0])+1900,
		time.Month(dateBytes[1]),
		int(dateBytes[2]),
		0, 0, 0, 0,
		time.UTC,
	)

	// read record count (4 bytes, little-endian)
	recordsBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.reader, recordsBytes); err != nil {
		return fmt.Errorf("read records count: %w", err)
	}
	r.recordsCount = binary.LittleEndian.Uint32(recordsBytes)

	// read header size (2 bytes, little-endian)
	headerBytes := make([]byte, 2)
	if _, err := io.ReadFull(r.reader, headerBytes); err != nil {
		return fmt.Errorf("read header size: %w", err)
	}
	r.headerBytesNumber = binary.LittleEndian.Uint16(headerBytes)
	// the header must hold at least the metadata and the 0x0D terminator
	if r.headerBytesNumber < metadataLength+1 {
		return fmt.Errorf("%w: %d (must be >= %d)", ErrInvalidHeaderSize, r.headerBytesNumber, metadataLength+1)
	}

	// read record size (2 bytes, little-endian)
	recordBytes := make([]byte, 2)
	if _, err := io.ReadFull(r.reader, recordBytes); err != nil {
		return fmt.Errorf("read record size: %w", err)
	}
	r.recordBytesNumber = binary.LittleEndian.Uint16(recordBytes)
	if r.recordBytesNumber == 0 {
		return fmt.Errorf("%w: 0", ErrInvalidRecordSize)
	}

	// read reserved bytes (20 bytes)
	reserved := make([]byte, 20)
	if _, err := io.ReadFull(r.reader, reserved); err != nil {
		return fmt.Errorf("read reserved bytes: %w", err)
	}

	// byte 29 (index 17) contains the Language Driver ID
	// try to auto-detect encoding if not explicitly set
	if r.decoder == nil {
		languageDriverID := reserved[17]
		r.decoder = getDecoderByLDID(languageDriverID)
	}

	return nil
}

// readFields reads field descriptors until the 0x0D terminator, then skips
// any remaining header padding so the reader is positioned at the first record.
// Descriptors and the terminator must fit within the declared header size.
func (r *Reader) readFields() error {
	headerSize := int(r.headerBytesNumber)
	consumed := int(metadataLength)

	// header size gives an upper bound on the number of descriptors
	r.fields = make([]Field, 0, (headerSize-consumed-1)/int(fieldLength))

	// readMetadata guarantees room for the terminator, and each descriptor is
	// accepted only if the terminator still fits after it, so consumed < headerSize
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return fmt.Errorf("read field or terminator: %w", err)
		}
		if b == 0x0D {
			consumed++
			break
		}
		// the descriptor and the terminator after it must fit within the header
		if consumed+int(fieldLength) >= headerSize {
			return fmt.Errorf("%w: field descriptor at offset %d exceeds %d-byte header", ErrInvalidTerminator, consumed, headerSize)
		}
		if err := r.reader.UnreadByte(); err != nil {
			return fmt.Errorf("unread byte: %w", err)
		}

		field, err := r.readField()
		if err != nil {
			return fmt.Errorf("read field %d: %w", len(r.fields), err)
		}
		// Record.Data is keyed by name, so names must be non-empty and unique
		if field.Name == "" {
			return fmt.Errorf("%w: field %d has an empty name", ErrInvalidFieldName, len(r.fields))
		}
		for i, f := range r.fields {
			if f.Name == field.Name {
				return fmt.Errorf("%w: field %d duplicates name %q of field %d", ErrInvalidFieldName, len(r.fields), field.Name, i)
			}
		}
		r.fields = append(r.fields, field)
		consumed += int(fieldLength)
	}

	// skip any padding between terminator and start of record data
	// (e.g. Visual FoxPro backlink area or other extensions)
	if skip := headerSize - consumed; skip > 0 {
		if _, err := io.CopyN(io.Discard, r.reader, int64(skip)); err != nil {
			return fmt.Errorf("skip header padding: %w", err)
		}
	}

	// validate that declared field lengths are consistent with record size
	var total uint32 = 1 // deletion flag byte
	for _, f := range r.fields {
		total += uint32(f.Length)
	}
	if total != uint32(r.recordBytesNumber) {
		return fmt.Errorf("%w: fields sum to %d bytes but header declares %d", ErrRecordSizeMismatch, total, r.recordBytesNumber)
	}

	r.assignNullFlagBits()

	return nil
}

// assignNullFlagBits maps fields to bits of the VFP _NullFlags bitmap.
// Bits are assigned in field order: each Varchar/Varbinary field gets a
// var-length bit, followed by a null bit if the field is nullable.
func (r *Reader) assignNullFlagBits() {
	r.fieldBits = make([]fieldBits, len(r.fields))
	bit := 0
	offset := 1 // deletion flag
	for i, f := range r.fields {
		fb := fieldBits{varLength: -1, null: -1}
		if r.isVFP() {
			if f.Type == '0' {
				r.nullFlagsStart, r.nullFlagsEnd = offset, offset+int(f.Length)
			}
			if f.Type == 'V' || f.Type == 'Q' {
				fb.varLength = bit
				bit++
			}
			if f.Flags&flagNullable != 0 {
				fb.null = bit
				bit++
			}
		}
		r.fieldBits[i] = fb
		offset += int(f.Length)
	}
}

// isVFP reports whether the file is a Visual FoxPro table.
func (r *Reader) isVFP() bool {
	return r.fileType == VisualFoxPro || r.fileType == VisualFoxProAI || r.fileType == VisualFoxProVarchar
}

// readField reads a single 32-byte field descriptor.
func (r *Reader) readField() (Field, error) {
	fieldBytes := make([]byte, 32)
	if _, err := io.ReadFull(r.reader, fieldBytes); err != nil {
		return Field{}, fmt.Errorf("read field bytes: %w", err)
	}

	// field name (11 bytes, null-terminated)
	// the name ends at the first NUL; bytes after it may be garbage
	nameBytes := fieldBytes[0:11]
	if i := bytes.IndexByte(nameBytes, 0); i >= 0 {
		nameBytes = nameBytes[:i]
	}
	decodedName, err := r.decoder.Bytes(nameBytes)
	if err != nil {
		return Field{}, fmt.Errorf("decode name: %w", err)
	}

	fieldType := fieldBytes[11]
	var length uint16
	var decimalCount byte

	// visual FoxPro stores character field length as a little-endian uint16 in bytes 16-17.
	if fieldType == 'C' && r.isVFP() {
		length = binary.LittleEndian.Uint16(fieldBytes[16:18])
	} else {
		length = uint16(fieldBytes[16])
		decimalCount = fieldBytes[17]
	}

	field := Field{
		Name:          string(decodedName),
		Type:          fieldType,
		MemoryAddress: binary.LittleEndian.Uint32(fieldBytes[12:16]),
		Length:        length,
		DecimalCount:  decimalCount,
	}
	if r.isVFP() {
		field.Flags = fieldBytes[18]
	}

	return field, nil
}

// isValidFileType checks if the given file type is recognized.
func isValidFileType(ft FileType) bool {
	switch ft {
	case FoxBASE, FoxBASEPlusNoMemo, dBASEIVNoMemo, dBASEVNoMemo,
		VisualObjects, VisualFoxPro, VisualFoxProAI, VisualFoxProVarchar,
		dBASEIVTF, dBASEIVSF, dBASEIVMemo2, FoxBASEPlusMemo, VisualObjectsMemo,
		dBASEIVMemo, dBASEIVSQL, dBASEIVTFMemo, HiPerSix, FoxPro2, FoxBASE2:
		return true
	default:
		return false
	}
}

// getDecoderByLDID returns an appropriate text decoder based on the
// Language Driver ID byte from the DBF header.
// Returns nil if the Language Driver ID is not recognized.
func getDecoderByLDID(ldid byte) *encoding.Decoder {
	switch ldid {
	case 0x26, 0x65: // CP866 (Russian MS-DOS)
		return charmap.CodePage866.NewDecoder()
	case 0x64: // CP852 (Eastern European MS-DOS)
		return charmap.CodePage852.NewDecoder()
	case 0xC9: // CP1251 (Russian Windows)
		return charmap.Windows1251.NewDecoder()
	case 0x03: // CP1252 (Windows ANSI)
		return charmap.Windows1252.NewDecoder()
	case 0x01: // CP437 (US MS-DOS)
		return charmap.CodePage437.NewDecoder()
	case 0x02: // CP850 (International MS-DOS)
		return charmap.CodePage850.NewDecoder()
	default:
		return nil
	}
}

// String returns a string representation of the Reader for debugging.
func (r *Reader) String() string {
	return fmt.Sprintf(
		"DBF Reader:\n"+
			"  File Type: %s\n"+
			"  Last Update: %s\n"+
			"  Records Count: %d\n"+
			"  Fields Count: %d\n"+
			"  Header Size: %d bytes\n"+
			"  Record Size: %d bytes",
		r.fileType,
		r.lastUpdate.Format("2006-01-02"),
		r.recordsCount,
		len(r.fields),
		r.headerBytesNumber,
		r.recordBytesNumber,
	)
}

// Record represents a single record from the DBF file.
type Record struct {
	Deleted bool              // true if the record is marked as deleted
	Data    map[string]string // field values indexed by field name; "" for null values
	Null    map[string]bool   // true for Visual FoxPro fields holding null; nil if the record has none
}

// Next advances to the next record in the DBF file, reading its raw bytes
// from the input. It returns false when there are no more records or an
// error occurred. Use Err() to check for errors after the iteration completes.
//
// Each successful Next makes exactly one record available to Read. Calling
// Next again without Read skips the loaded record.
//
// Example:
//
//	for reader.Next() {
//		record, err := reader.Read()
//		if err != nil {
//			log.Fatal(err)
//		}
//		// process record
//	}
//	if err := reader.Err(); err != nil {
//		log.Fatal(err)
//	}
func (r *Reader) Next() bool {
	r.pending = false
	if r.err != nil || r.currentRecord >= r.recordsCount {
		return false
	}

	if r.buf == nil {
		r.buf = make([]byte, r.recordBytesNumber)
	}
	if _, err := io.ReadFull(r.reader, r.buf); err != nil {
		r.err = fmt.Errorf("read record %d: %w", r.currentRecord, err)
		return false
	}

	switch flag := r.buf[0]; {
	case flag == 0x1A:
		// end-of-file marker before the declared number of records
		r.err = fmt.Errorf("read record %d: end-of-file marker, header declares %d records: %w", r.currentRecord, r.recordsCount, io.ErrUnexpectedEOF)
		return false
	case r.strict && flag != 0x20 && flag != 0x2A:
		r.err = fmt.Errorf("%w: 0x%02X in record %d", ErrInvalidDeletionFlag, flag, r.currentRecord)
		return false
	}

	r.currentRecord++
	r.pending = true
	return true
}

// Read decodes the record loaded by the preceding successful Next() call.
// It returns ErrReadBeforeNext if there is no such record: Next() was not
// called, returned false, or its record has already been returned by Read().
func (r *Reader) Read() (*Record, error) {
	if r.err != nil {
		return nil, r.err
	}
	if !r.pending {
		return nil, ErrReadBeforeNext
	}
	r.pending = false

	recordBytes := r.buf
	record := &Record{
		Deleted: recordBytes[0] == 0x2A, // '*' marks deleted records
		Data:    make(map[string]string, len(r.fields)),
	}

	nullFlags := r.nullFlags(recordBytes)

	// parse individual fields
	offset := 1 // skip deletion flag
	for i, field := range r.fields {
		end := offset + int(field.Length)
		if end > len(recordBytes) {
			r.err = fmt.Errorf("%w: field %s (offset %d + length %d > record size %d)", ErrFieldOutOfBounds, field.Name, offset, field.Length, len(recordBytes))
			return nil, r.err
		}
		fieldData := recordBytes[offset:end]
		offset += int(field.Length)

		// hidden system columns such as _NullFlags are not returned
		if field.Flags&flagSystem != 0 {
			continue
		}

		bits := r.fieldBits[i]
		if bitSet(nullFlags, bits.null) {
			record.Data[field.Name] = ""
			if record.Null == nil {
				record.Null = make(map[string]bool)
			}
			record.Null[field.Name] = true
			continue
		}
		if bitSet(nullFlags, bits.varLength) {
			// the actual length of a shorter Varchar/Varbinary value is in the last byte
			n := int(fieldData[len(fieldData)-1])
			if n >= len(fieldData) {
				r.err = fmt.Errorf("%w: field %s var length %d exceeds field length %d", ErrFieldOutOfBounds, field.Name, n, field.Length)
				return nil, r.err
			}
			fieldData = fieldData[:n]
		}

		// decode field value
		value, err := r.decodeFieldValue(field, fieldData)
		if err != nil {
			r.err = fmt.Errorf("decode field %s: %w", field.Name, err)
			return nil, r.err
		}

		record.Data[field.Name] = value
	}

	return record, nil
}

// maxInitialCap limits the initial ReadAll allocation so that a malicious
// records count in the header cannot cause a huge preallocation.
const maxInitialCap = 1 << 20

// nullFlags returns the VFP _NullFlags bytes of a record, or nil if the table has none.
func (r *Reader) nullFlags(record []byte) []byte {
	return record[r.nullFlagsStart:r.nullFlagsEnd]
}

// bitSet reports whether bit n (LSB first) is set in flags.
// Bits outside the bitmap, including n < 0, are reported as unset.
func bitSet(flags []byte, n int) bool {
	if n < 0 || n/8 >= len(flags) {
		return false
	}
	return flags[n/8]&(1<<(n%8)) != 0
}

// ReadAll reads all remaining records from the DBF file into memory.
// If Next() has loaded a record that has not been returned by Read() yet,
// that record is included as the first element.
// This is convenient for small files but may consume significant memory for large files.
// For large files, consider using Next()/Read() for streaming access.
//
// Example:
//
//	records, err := reader.ReadAll()
//	if err != nil {
//		log.Fatal(err)
//	}
//	for _, record := range records {
//		fmt.Println(record.Data["NAME"])
//	}
func (r *Reader) ReadAll() ([]*Record, error) {
	capacity := min(r.recordsCount-r.currentRecord, maxInitialCap)
	if r.pending {
		capacity++
	}
	records := make([]*Record, 0, capacity)

	if r.pending {
		record, err := r.Read()
		if err != nil {
			return records, err
		}
		records = append(records, record)
	}

	for r.Next() {
		record, err := r.Read()
		if err != nil {
			return records, err
		}
		records = append(records, record)
	}

	if err := r.Err(); err != nil {
		return records, err
	}

	return records, nil
}

// Err returns any error that occurred during iteration.
// It should be called after Next() returns false to check for errors.
func (r *Reader) Err() error {
	return r.err
}

// decodeFieldValue decodes a field's raw bytes into a string based on its type.
func (r *Reader) decodeFieldValue(field Field, data []byte) (string, error) {
	if r.isVFP() {
		switch field.Type {
		case 'I': // 32-bit little-endian integer
			if len(data) != 4 {
				return "", fmt.Errorf("integer field length %d, want 4", len(data))
			}
			return strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(data))), 10), nil

		case 'B': // 64-bit little-endian IEEE 754 double
			if len(data) != 8 {
				return "", fmt.Errorf("double field length %d, want 8", len(data))
			}
			return strconv.FormatFloat(math.Float64frombits(binary.LittleEndian.Uint64(data)), 'f', -1, 64), nil

		case 'Y': // 64-bit little-endian integer scaled by 10000
			if len(data) != 8 {
				return "", fmt.Errorf("currency field length %d, want 8", len(data))
			}
			return formatCurrency(int64(binary.LittleEndian.Uint64(data))), nil

		case 'T': // Julian day number and milliseconds since midnight, both 32-bit little-endian
			if len(data) != 8 {
				return "", fmt.Errorf("datetime field length %d, want 8", len(data))
			}
			return formatDateTime(int32(binary.LittleEndian.Uint32(data[:4])), int32(binary.LittleEndian.Uint32(data[4:]))), nil

		case 'M', 'G', 'P', 'W': // 32-bit little-endian block number in the memo file
			if len(data) != 4 {
				return "", fmt.Errorf("memo field length %d, want 4", len(data))
			}
			if block := binary.LittleEndian.Uint32(data); block != 0 {
				return strconv.FormatUint(uint64(block), 10), nil
			}
			return "", nil

		case 'C': // binary character data is returned as is
			if field.Flags&flagBinary != 0 {
				return string(data), nil
			}

		case 'V': // varchar: length already resolved by the caller, no padding to trim
			if field.Flags&flagBinary != 0 {
				return string(data), nil
			}
			decoded, err := r.decoder.Bytes(data)
			if err != nil {
				return "", err
			}
			return string(decoded), nil

		case 'Q': // varbinary is returned as is
			return string(data), nil
		}
	}

	switch field.Type {
	case 'C': // character field: decode to UTF-8 first, then trim spaces
		decoded, err := r.decoder.Bytes(data)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(decoded)), nil

	case 'N', 'F': // numeric and Float fields (ASCII only)
		return string(bytes.TrimSpace(data)), nil

	case 'D': // date field (format: YYYYMMDD, ASCII only)
		return string(bytes.TrimSpace(data)), nil

	case 'L': // logical field (boolean)
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) > 0 {
			switch trimmed[0] {
			case 'T', 't', 'Y', 'y':
				return "true", nil
			case 'F', 'f', 'N', 'n':
				return "false", nil
			}
		}
		return "", nil

	case 'M', 'G', 'P', 'B': // memo, general, picture, binary: ASCII block number in the memo file
		return string(bytes.TrimSpace(data)), nil

	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedType, field.Type)
	}
}

// formatCurrency formats a VFP currency value stored as an integer scaled by 10000.
func formatCurrency(v int64) string {
	sign := ""
	u := uint64(v)
	if v < 0 {
		sign = "-"
		u = -u
	}
	return fmt.Sprintf("%s%d.%04d", sign, u/10000, u%10000)
}

// formatDateTime formats a VFP datetime as "2006-01-02T15:04:05",
// rounding to the nearest second. An all-zero value is returned as "".
func formatDateTime(julianDay, millis int32) string {
	if julianDay == 0 && millis == 0 {
		return ""
	}
	seconds := (int64(julianDay)-julianDayUnixEpoch)*86400 + (int64(millis)+500)/1000
	return time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05")
}

// Close closes the underlying file if the Reader was created with NewFromFile.
// It is a no-op for readers created with New. Close is safe to call multiple times.
func (r *Reader) Close() error {
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}
