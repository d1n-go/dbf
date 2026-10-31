package dbf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// createMinimalDBF creates a minimal valid DBF file for testing
func createMinimalDBF() []byte {
	buf := new(bytes.Buffer)

	// header (32 bytes)
	buf.WriteByte(0x03) // file type: FoxBASE+/Dbase III plus, no memo
	buf.WriteByte(124)  // last update: year (124 = 2024)
	buf.WriteByte(1)    // month
	buf.WriteByte(15)   // day

	// number of records (4 bytes, little-endian)
	binary.Write(buf, binary.LittleEndian, uint32(2))

	// header size (2 bytes) - 32 (header) + 32 (field) + 1 (terminator)
	binary.Write(buf, binary.LittleEndian, uint16(32+32+1))

	// record size (2 bytes) - 1 (deletion flag) + 10 (NAME field)
	binary.Write(buf, binary.LittleEndian, uint16(11))

	// reserved (20 bytes)
	buf.Write(make([]byte, 20))

	// field descriptor (32 bytes)
	name := []byte("NAME")
	name = append(name, make([]byte, 11-len(name))...) // pad to 11 bytes
	buf.Write(name)

	buf.WriteByte('C')          // field type: Character
	buf.Write(make([]byte, 4))  // memory address (reserved)
	buf.WriteByte(10)           // field length
	buf.WriteByte(0)            // decimal count
	buf.Write(make([]byte, 14)) // reserved

	// field descriptor terminator
	buf.WriteByte(0x0D)

	// records
	// record 1 (not deleted)
	buf.WriteByte(0x20)           // not deleted
	buf.WriteString("John Doe  ") // 10 bytes

	// record 2 (deleted)
	buf.WriteByte(0x2A)           // deleted (*)
	buf.WriteString("Jane Smith") // 10 bytes

	return buf.Bytes()
}

// createDBFWithMultipleFields creates a DBF with multiple field types
func createDBFWithMultipleFields() []byte {
	buf := new(bytes.Buffer)

	// header
	buf.WriteByte(0x03)
	buf.WriteByte(124) // 2024
	buf.WriteByte(6)   // june
	buf.WriteByte(15)

	binary.Write(buf, binary.LittleEndian, uint32(1))         // 1 record
	binary.Write(buf, binary.LittleEndian, uint16(32+32*3+1)) // 3 fields
	binary.Write(buf, binary.LittleEndian, uint16(1+10+3+8))  // record size

	// reserved bytes (20 total) - add LDID for autodetect
	reserved := make([]byte, 20)
	reserved[17] = 0x26 // language Driver ID = CP866
	buf.Write(reserved)

	// field 1: NAME (Character)
	name := append([]byte("NAME"), make([]byte, 7)...)
	buf.Write(name)
	buf.WriteByte('C')
	buf.Write(make([]byte, 4))
	buf.WriteByte(10)
	buf.WriteByte(0)
	buf.Write(make([]byte, 14))

	// field 2: AGE (Numeric)
	age := append([]byte("AGE"), make([]byte, 8)...)
	buf.Write(age)
	buf.WriteByte('N')
	buf.Write(make([]byte, 4))
	buf.WriteByte(3)
	buf.WriteByte(0)
	buf.Write(make([]byte, 14))

	// field 3: BIRTHDATE (Date)
	birthdate := append([]byte("BIRTHDATE"), make([]byte, 2)...)
	buf.Write(birthdate)
	buf.WriteByte('D')
	buf.Write(make([]byte, 4))
	buf.WriteByte(8)
	buf.WriteByte(0)
	buf.Write(make([]byte, 14))

	buf.WriteByte(0x0D)

	// record
	buf.WriteByte(0x20)
	buf.WriteString("Alice     ") // 10 bytes
	buf.WriteString(" 25")        // 3 bytes
	buf.WriteString("19990115")   // 8 bytes

	return buf.Bytes()
}

func TestNew(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if dbf == nil {
		t.Fatal("New() returned nil")
	}

	if dbf.RecordsCount() != 2 {
		t.Errorf("Expected 2 records, got %d", dbf.RecordsCount())
	}

	if dbf.FieldsCount() != 1 {
		t.Errorf("Expected 1 field, got %d", dbf.FieldsCount())
	}
}

func TestNewWithOptions(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	// test with CP866
	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() with WithCP866() failed: %v", err)
	}
	if dbf == nil {
		t.Fatal("New() returned nil")
	}

	// test with custom encoding
	reader2 := bytes.NewReader(data)
	dbf2, err := New(reader2, WithEncoding(charmap.Windows1251))
	if err != nil {
		t.Fatalf("New() with WithEncoding() failed: %v", err)
	}
	if dbf2 == nil {
		t.Fatal("New() returned nil")
	}

	// test with custom decoder
	reader3 := bytes.NewReader(data)
	decoder := charmap.CodePage850.NewDecoder()
	dbf3, err := New(reader3, WithDecoder(decoder))
	if err != nil {
		t.Fatalf("New() with WithDecoder() failed: %v", err)
	}
	if dbf3 == nil {
		t.Fatal("New() returned nil")
	}
}

func TestMultipleOptions(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	// last option should win
	dbf, err := New(reader, WithCP866(), WithCP1251())
	if err != nil {
		t.Fatalf("New() with multiple options failed: %v", err)
	}
	if dbf == nil {
		t.Fatal("New() returned nil")
	}
}

func TestFileType(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if dbf.FileType() != FoxBASEPlusNoMemo {
		t.Errorf("Expected FileType FoxBASEPlusNoMemo (0x03), got %v", dbf.FileType())
	}

	if dbf.FileType().String() == "" {
		t.Error("FileType.String() returned empty string")
	}
}

func TestLastUpdate(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	expected := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	if !dbf.LastUpdate().Equal(expected) {
		t.Errorf("Expected LastUpdate %v, got %v", expected, dbf.LastUpdate())
	}
}

func TestFields(t *testing.T) {
	data := createDBFWithMultipleFields()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	fields := dbf.Fields()
	if len(fields) != 3 {
		t.Fatalf("Expected 3 fields, got %d", len(fields))
	}

	// check NAME field
	if fields[0].Name != "NAME" {
		t.Errorf("Expected field name 'NAME', got '%s'", fields[0].Name)
	}
	if fields[0].Type != 'C' {
		t.Errorf("Expected field type 'C', got '%c'", fields[0].Type)
	}
	if fields[0].Length != 10 {
		t.Errorf("Expected field length 10, got %d", fields[0].Length)
	}

	// check AGE field
	if fields[1].Name != "AGE" {
		t.Errorf("Expected field name 'AGE', got '%s'", fields[1].Name)
	}
	if fields[1].Type != 'N' {
		t.Errorf("Expected field type 'N', got '%c'", fields[1].Type)
	}

	// check BIRTHDATE field
	if fields[2].Name != "BIRTHDATE" {
		t.Errorf("Expected field name 'BIRTHDATE', got '%s'", fields[2].Name)
	}
	if fields[2].Type != 'D' {
		t.Errorf("Expected field type 'D', got '%c'", fields[2].Type)
	}
}

func TestFieldsReturnsCopy(t *testing.T) {
	r, err := New(bytes.NewReader(createDBFWithMultipleFields()), WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	fields := r.Fields()
	fields[0].Name = "MUTATED"
	fields[0].Length = 1

	if got := r.Fields()[0]; got.Name != "NAME" || got.Length != 10 {
		t.Errorf("Fields() exposed internal state: got %+v", got)
	}

	if !r.Next() {
		t.Fatal("Next() returned false")
	}
	record, err := r.Read()
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if _, ok := record.Data["MUTATED"]; ok {
		t.Error("record contains mutated field name")
	}
	if _, ok := record.Data["NAME"]; !ok {
		t.Error("record is missing NAME field")
	}
}

func TestFieldTypeString(t *testing.T) {
	tests := []struct {
		fieldType byte
		expected  string
	}{
		{'C', "Character"},
		{'N', "Numeric"},
		{'D', "Date"},
		{'L', "Logical"},
		{'M', "Memo"},
		{'F', "Float"},
		{'X', "Unknown (X)"},
	}

	for _, tt := range tests {
		field := Field{Type: tt.fieldType}
		if got := field.TypeString(); got != tt.expected {
			t.Errorf("Field type %c: expected '%s', got '%s'", tt.fieldType, tt.expected, got)
		}
	}
}

func TestReadAll(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	records, err := dbf.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("Expected 2 records, got %d", len(records))
	}

	// check first record (not deleted)
	if records[0].Deleted {
		t.Error("First record should not be deleted")
	}

	name := strings.TrimSpace(records[0].Data["NAME"])
	if name != "John Doe" {
		t.Errorf("Expected name 'John Doe', got '%s'", name)
	}

	// check second record (deleted)
	if !records[1].Deleted {
		t.Error("Second record should be deleted")
	}
}

func TestNextAndRead(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	count := 0
	for dbf.Next() {
		record, err := dbf.Read()
		if err != nil {
			t.Fatalf("Read() failed: %v", err)
		}

		if record == nil {
			t.Fatal("Read() returned nil record")
		}

		count++
	}

	if err := dbf.Err(); err != nil {
		t.Fatalf("Err() returned error: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected to read 2 records, got %d", count)
	}
}

func TestMultipleFieldTypes(t *testing.T) {
	data := createDBFWithMultipleFields()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	records, err := dbf.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	record := records[0]

	// check NAME
	name := strings.TrimSpace(record.Data["NAME"])
	if name != "Alice" {
		t.Errorf("Expected name 'Alice', got '%s'", name)
	}

	// check AGE
	age := strings.TrimSpace(record.Data["AGE"])
	if age != "25" {
		t.Errorf("Expected age '25', got '%s'", age)
	}

	// check BIRTHDATE
	birthdate := strings.TrimSpace(record.Data["BIRTHDATE"])
	if birthdate != "19990115" {
		t.Errorf("Expected birthdate '19990115', got '%s'", birthdate)
	}
}

func TestInvalidFileType(t *testing.T) {
	buf := new(bytes.Buffer)
	buf.WriteByte(0xFF) // invalid file type
	buf.Write(make([]byte, 100))

	_, err := New(bytes.NewReader(buf.Bytes()), WithCP866())
	if err == nil {
		t.Error("Expected error for invalid file type, got nil")
	}
}

func TestEncodingAutodetection(t *testing.T) {
	buf := new(bytes.Buffer)

	// valid header
	buf.WriteByte(0x03)
	buf.WriteByte(124) // 2024
	buf.WriteByte(1)   // january
	buf.WriteByte(15)

	binary.Write(buf, binary.LittleEndian, uint32(0))       // 0 records
	binary.Write(buf, binary.LittleEndian, uint16(32+32+1)) // 1 field
	binary.Write(buf, binary.LittleEndian, uint16(11))      // record size

	// reserved bytes (20 total)
	reserved := make([]byte, 20)
	reserved[17] = 0x26 // language Driver ID = CP866
	buf.Write(reserved)

	// field descriptor (нужен хотя бы один!)
	name := append([]byte("NAME"), make([]byte, 7)...)
	buf.Write(name)
	buf.WriteByte('C')
	buf.Write(make([]byte, 4))
	buf.WriteByte(10)
	buf.WriteByte(0)
	buf.Write(make([]byte, 14))

	// terminator
	buf.WriteByte(0x0D)

	// should auto-detect CP866 from LDID
	reader := bytes.NewReader(buf.Bytes())
	dbf, err := New(reader) // без явной кодировки!
	if err != nil {
		t.Fatalf("New() with autodetection failed: %v", err)
	}

	if dbf == nil {
		t.Fatal("New() returned nil")
	}

	// verify it detected the encoding correctly
	if dbf.FieldsCount() != 1 {
		t.Errorf("Expected 1 field, got %d", dbf.FieldsCount())
	}
}

func TestEncodingAutodetectionFails(t *testing.T) {
	buf := new(bytes.Buffer)

	// valid header
	buf.WriteByte(0x03)
	buf.WriteByte(124)
	buf.WriteByte(1)
	buf.WriteByte(15)

	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint16(32+32+1))
	binary.Write(buf, binary.LittleEndian, uint16(11))

	// reserved bytes with unknown LDID
	reserved := make([]byte, 20)
	reserved[17] = 0xFF // unknown Language Driver ID
	buf.Write(reserved)

	// field descriptor
	name := append([]byte("NAME"), make([]byte, 7)...)
	buf.Write(name)
	buf.WriteByte('C')
	buf.Write(make([]byte, 4))
	buf.WriteByte(10)
	buf.WriteByte(0)
	buf.Write(make([]byte, 14))

	buf.WriteByte(0x0D)

	// should fail - no explicit encoding and can't auto-detect
	reader := bytes.NewReader(buf.Bytes())
	_, err := New(reader)
	if err == nil {
		t.Error("Expected error when encoding can't be determined, got nil")
	}

	if !errors.Is(err, ErrUnknownEncoding) {
		t.Errorf("Expected ErrUnknownEncoding, got: %v", err)
	}
}

// writeHeader writes a 32-byte dBase III header with CP866 LDID.
func writeHeader(buf *bytes.Buffer, records uint32, headerSize, recordSize uint16) {
	buf.WriteByte(0x03)
	buf.Write([]byte{124, 1, 15})
	binary.Write(buf, binary.LittleEndian, records)
	binary.Write(buf, binary.LittleEndian, headerSize)
	binary.Write(buf, binary.LittleEndian, recordSize)
	reserved := make([]byte, 20)
	reserved[17] = 0x26
	buf.Write(reserved)
}

// writeCharField writes a 32-byte descriptor for a C field.
func writeCharField(buf *bytes.Buffer, name string, length byte) {
	b := make([]byte, 32)
	copy(b, name)
	b[11] = 'C'
	b[16] = length
	buf.Write(b)
}

func TestInvalidTerminator(t *testing.T) {
	buf := new(bytes.Buffer)
	writeHeader(buf, 0, 32+32+1, 1+10)
	writeCharField(buf, "NAME", 10)
	buf.WriteByte(0xFF) // should be 0x0D

	_, err := New(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrInvalidTerminator) {
		t.Errorf("New() = %v, want ErrInvalidTerminator", err)
	}
}

func TestHeaderTooSmallForTerminator(t *testing.T) {
	buf := new(bytes.Buffer)
	writeHeader(buf, 0, 32, 1+10)
	writeCharField(buf, "NAME", 10)
	buf.WriteByte(0x0D)

	_, err := New(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrInvalidHeaderSize) {
		t.Errorf("New() = %v, want ErrInvalidHeaderSize", err)
	}
}

func TestFieldDescriptorExceedsHeader(t *testing.T) {
	// header declares room for one descriptor without the terminator
	buf := new(bytes.Buffer)
	writeHeader(buf, 0, 32+32, 1+10)
	writeCharField(buf, "NAME", 10)
	buf.WriteByte(0x0D)

	_, err := New(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrInvalidTerminator) {
		t.Errorf("New() = %v, want ErrInvalidTerminator", err)
	}
}

func TestZeroLengthFieldsOutsideHeader(t *testing.T) {
	// 2048 zero-length descriptors after a header that declares no fields
	buf := new(bytes.Buffer)
	writeHeader(buf, 0, 33, 1)
	buf.Write(make([]byte, 2048*32))
	buf.WriteByte(0x0D)

	r, err := New(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrInvalidTerminator) {
		t.Errorf("New() = %v, want ErrInvalidTerminator", err)
	}
	if r != nil {
		t.Errorf("New() returned reader with %d fields", r.FieldsCount())
	}
}

// countingReader counts bytes read from the underlying reader.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestNewDoesNotReadPastHeader(t *testing.T) {
	// a stream of descriptors without a terminator must not be consumed to EOF
	const headerSize = 32 + 32 + 1
	buf := new(bytes.Buffer)
	writeHeader(buf, 0, headerSize, 1+10)
	for buf.Len() < 1<<20 {
		writeCharField(buf, "NAME", 10)
	}

	cr := &countingReader{r: bytes.NewReader(buf.Bytes())}
	if _, err := New(cr); !errors.Is(err, ErrInvalidTerminator) {
		t.Fatalf("New() = %v, want ErrInvalidTerminator", err)
	}
	// allow for bufio read-ahead
	if limit := headerSize + 4096; cr.n > limit {
		t.Errorf("New() read %d bytes, want <= %d", cr.n, limit)
	}
}

func FuzzNew(f *testing.F) {
	f.Add(createMinimalDBF())
	f.Add(createDBFWithMultipleFields())
	f.Add(createVFPDBFWithLongCharField(300))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		r, err := New(bytes.NewReader(data), WithCP866())
		if err != nil {
			return
		}
		if max := (int(r.headerBytesNumber) - 33) / 32; r.FieldsCount() > max {
			t.Fatalf("%d fields do not fit in %d-byte header", r.FieldsCount(), r.headerBytesNumber)
		}
		records, err := r.ReadAll()
		if err == nil && uint32(len(records)) != r.RecordsCount() {
			t.Fatalf("ReadAll() returned %d records without error, header declares %d", len(records), r.RecordsCount())
		}
	})
}

func TestEmptyFile(t *testing.T) {
	_, err := New(bytes.NewReader([]byte{}))
	if err == nil {
		t.Error("Expected error for empty file, got nil")
	}
}

func TestTruncatedFile(t *testing.T) {
	data := createMinimalDBF()
	truncated := data[:10] // only first 10 bytes

	_, err := New(bytes.NewReader(truncated))
	if err == nil {
		t.Error("Expected error for truncated file, got nil")
	}
}

func TestString(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	str := dbf.String()
	if str == "" {
		t.Error("String() returned empty string")
	}

	// check that string contains expected information
	if !strings.Contains(str, "Records Count: 2") {
		t.Error("String() doesn't contain record count")
	}
}

func TestEOFBehavior(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// read all records
	count := 0
	for dbf.Next() {
		_, err := dbf.Read()
		if err != nil {
			t.Fatalf("Read() failed: %v", err)
		}
		count++
	}

	// try to read past EOF
	if dbf.Next() {
		t.Error("Next() should return false after EOF")
	}

	// err() should not return io.EOF
	if err := dbf.Err(); err != nil {
		t.Errorf("Err() should return nil after normal EOF, got: %v", err)
	}
}

func TestZeroRecords(t *testing.T) {
	buf := new(bytes.Buffer)

	buf.WriteByte(0x03)
	buf.WriteByte(124)
	buf.WriteByte(1)
	buf.WriteByte(15)

	binary.Write(buf, binary.LittleEndian, uint32(0)) // 0 records
	binary.Write(buf, binary.LittleEndian, uint16(32+32+1))
	binary.Write(buf, binary.LittleEndian, uint16(11))
	buf.Write(make([]byte, 20))

	// field descriptor
	name := append([]byte("NAME"), make([]byte, 7)...)
	buf.Write(name)
	buf.WriteByte('C')
	buf.Write(make([]byte, 4))
	buf.WriteByte(10)
	buf.WriteByte(0)
	buf.Write(make([]byte, 14))
	buf.WriteByte(0x0D)

	reader := bytes.NewReader(buf.Bytes())
	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if dbf.RecordsCount() != 0 {
		t.Errorf("Expected 0 records, got %d", dbf.RecordsCount())
	}

	records, err := dbf.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if len(records) != 0 {
		t.Errorf("Expected 0 records from ReadAll(), got %d", len(records))
	}
}

func TestInvalidHeaderSize(t *testing.T) {
	buf := new(bytes.Buffer)
	buf.WriteByte(0x03)
	buf.Write(make([]byte, 3))                         // date
	buf.Write(make([]byte, 4))                         // record count
	binary.Write(buf, binary.LittleEndian, uint16(10)) // header < 32 (metadataLength)
	binary.Write(buf, binary.LittleEndian, uint16(11)) // record size
	buf.Write(make([]byte, 20))

	_, err := New(bytes.NewReader(buf.Bytes()), WithCP866())
	if !errors.Is(err, ErrInvalidHeaderSize) {
		t.Errorf("New() = %v, want ErrInvalidHeaderSize", err)
	}
}

func TestZeroRecordSize(t *testing.T) {
	buf := new(bytes.Buffer)
	buf.WriteByte(0x03)
	buf.Write(make([]byte, 3))
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint16(32+32+1))
	binary.Write(buf, binary.LittleEndian, uint16(0)) // record size = 0
	buf.Write(make([]byte, 20))

	_, err := New(bytes.NewReader(buf.Bytes()), WithCP866())
	if err == nil {
		t.Error("Expected error for record size = 0, got nil")
	}
}

func TestFieldExceedsRecordBounds(t *testing.T) {
	buf := new(bytes.Buffer)

	buf.WriteByte(0x03)
	buf.WriteByte(124)
	buf.WriteByte(1)
	buf.WriteByte(15)

	binary.Write(buf, binary.LittleEndian, uint32(1))       // 1 record
	binary.Write(buf, binary.LittleEndian, uint16(32+32+1)) // 1 field
	binary.Write(buf, binary.LittleEndian, uint16(5))       // record size = 5 (too small for 1+10)
	buf.Write(make([]byte, 20))

	// field with length=10, but record size declares only 5 bytes total
	name := append([]byte("NAME"), make([]byte, 7)...)
	buf.Write(name)
	buf.WriteByte('C')
	buf.Write(make([]byte, 4))
	buf.WriteByte(10) // declared length inconsistent with record size
	buf.WriteByte(0)
	buf.Write(make([]byte, 14))
	buf.WriteByte(0x0D)
	buf.Write(make([]byte, 5))

	// new() now catches the mismatch at construction time
	_, err := New(bytes.NewReader(buf.Bytes()), WithCP866())
	if err == nil {
		t.Error("Expected error for field lengths exceeding record size, got nil")
	}
	if !errors.Is(err, ErrRecordSizeMismatch) {
		t.Errorf("Expected ErrRecordSizeMismatch, got: %v", err)
	}
}

func TestReadWithoutNext(t *testing.T) {
	data := createMinimalDBF()
	reader := bytes.NewReader(data)

	dbf, err := New(reader, WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	_, err = dbf.Read()
	if err == nil {
		t.Error("Expected error when Read called before Next, got nil")
	}
}

func TestWithCP1252(t *testing.T) {
	data := createMinimalDBF()
	_, err := New(bytes.NewReader(data), WithCP1252())
	if err != nil {
		t.Fatalf("New() with WithCP1252() failed: %v", err)
	}
}

func TestFileTypeStringAllVariants(t *testing.T) {
	tests := []struct {
		ft       FileType
		contains string
	}{
		{FoxBASE, "FoxBASE"},
		{FoxBASEPlusNoMemo, "no memo"},
		{dBASEIVNoMemo, "dBASE IV"},
		{dBASEVNoMemo, "dBASE 5"},
		{VisualObjects, "Visual Objects"},
		{VisualFoxPro, "Visual FoxPro"},
		{VisualFoxProAI, "autoincrement"},
		{VisualFoxProVarchar, "Varchar"},
		{dBASEIVTF, "SQL table"},
		{dBASEIVSF, "SQL system"},
		{dBASEIVMemo2, "memo"},
		{FoxBASEPlusMemo, "with memo"},
		{VisualObjectsMemo, "Visual Objects"},
		{dBASEIVMemo, "dBASE IV with memo"},
		{dBASEIVSQL, "SQL table"},
		{dBASEIVTFMemo, "SQL table files with memo"},
		{HiPerSix, "HiPer-Six"},
		{FoxPro2, "FoxPro 2"},
		{FoxBASE2, "FoxBASE"},
		{FileType(0xAB), "Unknown"},
	}
	for _, tt := range tests {
		s := tt.ft.String()
		if !strings.Contains(s, tt.contains) {
			t.Errorf("FileType(0x%02X).String() = %q, want it to contain %q", byte(tt.ft), s, tt.contains)
		}
	}
}

// createVFPDBFWithLongCharField creates a VFP DBF with a C field whose length > 255.
func createVFPDBFWithLongCharField(fieldLen uint16) []byte {
	buf := new(bytes.Buffer)

	buf.WriteByte(byte(VisualFoxPro)) // VFP file type
	buf.WriteByte(124)
	buf.WriteByte(1)
	buf.WriteByte(1)

	binary.Write(buf, binary.LittleEndian, uint32(1))          // 1 record
	binary.Write(buf, binary.LittleEndian, uint16(32+32+1))    // 1 field
	binary.Write(buf, binary.LittleEndian, uint16(1+fieldLen)) // deletion + field

	reserved := make([]byte, 20)
	reserved[17] = 0x03 // CP1252
	buf.Write(reserved)

	// field descriptor with 2-byte length in bytes 16-17
	name := make([]byte, 11)
	copy(name, "LONGNAME")
	buf.Write(name)
	buf.WriteByte('C')
	buf.Write(make([]byte, 4))
	// VFP stores C length as little-endian uint16 across bytes 16-17
	binary.Write(buf, binary.LittleEndian, fieldLen)
	buf.Write(make([]byte, 14))
	buf.WriteByte(0x0D)

	// record: deletion flag + fieldLen spaces
	buf.WriteByte(0x20)
	value := make([]byte, fieldLen)
	copy(value, "Hello VFP")
	for i := len("Hello VFP"); i < int(fieldLen); i++ {
		value[i] = ' '
	}
	buf.Write(value)

	return buf.Bytes()
}

func TestVFPCharacterFieldLength(t *testing.T) {
	data := createVFPDBFWithLongCharField(300)
	r, err := New(bytes.NewReader(data), WithCP1252())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	fields := r.Fields()
	if len(fields) != 1 {
		t.Fatalf("Expected 1 field, got %d", len(fields))
	}
	if fields[0].Length != 300 {
		t.Errorf("Expected field length 300, got %d", fields[0].Length)
	}

	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}
	if records[0].Data["LONGNAME"] != "Hello VFP" {
		t.Errorf("Expected 'Hello VFP', got %q", records[0].Data["LONGNAME"])
	}
}

func TestHeaderPaddingSkip(t *testing.T) {
	buf := new(bytes.Buffer)

	buf.WriteByte(0x03)
	buf.WriteByte(124)
	buf.WriteByte(1)
	buf.WriteByte(15)

	// declare a larger header than just fields+terminator (extra 10 bytes padding)
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint16(32+32+1+10)) // 10 extra padding bytes
	binary.Write(buf, binary.LittleEndian, uint16(1+5))        // deletion + 5-byte field

	reserved := make([]byte, 20)
	reserved[17] = 0x26 // CP866
	buf.Write(reserved)

	name := make([]byte, 11)
	copy(name, "ID")
	buf.Write(name)
	buf.WriteByte('C')
	buf.Write(make([]byte, 4))
	buf.WriteByte(5)
	buf.WriteByte(0)
	buf.Write(make([]byte, 14))
	buf.WriteByte(0x0D)

	buf.Write(make([]byte, 10)) // padding between terminator and records

	// record
	buf.WriteByte(0x20)
	buf.WriteString("AB   ")

	r, err := New(bytes.NewReader(buf.Bytes()), WithCP866())
	if err != nil {
		t.Fatalf("New() with padded header failed: %v", err)
	}

	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}
	if records[0].Data["ID"] != "AB" {
		t.Errorf("Expected 'AB', got %q", records[0].Data["ID"])
	}
}

func TestStringFieldsCountWithPadding(t *testing.T) {
	for _, padding := range []int{0, 10, 32, 263} {
		buf := new(bytes.Buffer)
		buf.WriteByte(0x03)
		buf.WriteByte(124)
		buf.WriteByte(1)
		buf.WriteByte(15)
		binary.Write(buf, binary.LittleEndian, uint32(0))
		binary.Write(buf, binary.LittleEndian, uint16(32+32+1+padding))
		binary.Write(buf, binary.LittleEndian, uint16(1+5))
		reserved := make([]byte, 20)
		reserved[17] = 0x26
		buf.Write(reserved)

		name := make([]byte, 11)
		copy(name, "ID")
		buf.Write(name)
		buf.WriteByte('C')
		buf.Write(make([]byte, 4))
		buf.WriteByte(5)
		buf.WriteByte(0)
		buf.Write(make([]byte, 14))
		buf.WriteByte(0x0D)
		buf.Write(make([]byte, padding))

		r, err := New(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("padding %d: New() failed: %v", padding, err)
		}
		if r.FieldsCount() != 1 {
			t.Errorf("padding %d: FieldsCount() = %d, want 1", padding, r.FieldsCount())
		}
		if !strings.Contains(r.String(), "Fields Count: 1\n") {
			t.Errorf("padding %d: String() reports wrong fields count:\n%s", padding, r.String())
		}
	}
}

func TestGetDecoderByLDID(t *testing.T) {
	tests := []struct {
		ldid byte
		cm   *charmap.Charmap
		text string
	}{
		{0x26, charmap.CodePage866, "Привет"},
		{0x65, charmap.CodePage866, "Привет"},
		{0x64, charmap.CodePage852, "Łódź"},
		{0xC9, charmap.Windows1251, "Привет"},
		{0x03, charmap.Windows1252, "Café"},
		{0x01, charmap.CodePage437, "Café"},
		{0x02, charmap.CodePage850, "Café"},
	}
	for _, tt := range tests {
		dec := getDecoderByLDID(tt.ldid)
		if dec == nil {
			t.Errorf("getDecoderByLDID(0x%02X) returned nil, want decoder", tt.ldid)
			continue
		}
		encoded, err := tt.cm.NewEncoder().Bytes([]byte(tt.text))
		if err != nil {
			t.Fatalf("encode %q: %v", tt.text, err)
		}
		decoded, err := dec.Bytes(encoded)
		if err != nil {
			t.Fatalf("getDecoderByLDID(0x%02X) decode: %v", tt.ldid, err)
		}
		if string(decoded) != tt.text {
			t.Errorf("getDecoderByLDID(0x%02X) decoded %q, want %q", tt.ldid, decoded, tt.text)
		}
	}
	if getDecoderByLDID(0xFF) != nil {
		t.Error("getDecoderByLDID(0xFF) should return nil for unknown LDID")
	}
}

// createDBFWithLogicalAndFloat builds a DBF with L, F, M field types.
func createDBFWithLogicalAndFloat() []byte {
	buf := new(bytes.Buffer)

	buf.WriteByte(0x03)
	buf.WriteByte(124)
	buf.WriteByte(1)
	buf.WriteByte(15)

	// 6 records, 3 fields: ACTIVE(L,1), SCORE(F,8), NOTE(M,10)
	binary.Write(buf, binary.LittleEndian, uint32(6))
	binary.Write(buf, binary.LittleEndian, uint16(32+32*3+1))
	binary.Write(buf, binary.LittleEndian, uint16(1+1+8+10))
	buf.Write(make([]byte, 20))

	writeField := func(name string, typ byte, length byte) {
		b := make([]byte, 11)
		copy(b, name)
		buf.Write(b)
		buf.WriteByte(typ)
		buf.Write(make([]byte, 4))
		buf.WriteByte(length)
		buf.WriteByte(0)
		buf.Write(make([]byte, 14))
	}
	writeField("ACTIVE", 'L', 1)
	writeField("SCORE", 'F', 8)
	writeField("NOTE", 'M', 10)
	buf.WriteByte(0x0D)

	writeRecord := func(flag byte, active string, score string, note string) {
		buf.WriteByte(flag)
		buf.WriteString(active)
		buf.WriteString(score)
		buf.WriteString(note)
	}
	writeRecord(0x20, "T", "  3.14  ", "memo1     ")
	writeRecord(0x20, "t", "  2.71  ", "memo2     ")
	writeRecord(0x20, "Y", "  1.00  ", "          ")
	writeRecord(0x20, "F", "  0.00  ", "          ")
	writeRecord(0x20, "N", "  0.00  ", "          ")
	writeRecord(0x20, " ", "  0.00  ", "          ") // unknown logical value

	return buf.Bytes()
}

func TestDecodeFieldValueLogicalAndFloat(t *testing.T) {
	data := createDBFWithLogicalAndFloat()
	r, err := New(bytes.NewReader(data), WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}
	if len(records) != 6 {
		t.Fatalf("Expected 6 records, got %d", len(records))
	}

	trueCases := []int{0, 1, 2}
	falseCases := []int{3, 4}
	for _, i := range trueCases {
		if records[i].Data["ACTIVE"] != "true" {
			t.Errorf("record[%d] ACTIVE: expected \"true\", got %q", i, records[i].Data["ACTIVE"])
		}
	}
	for _, i := range falseCases {
		if records[i].Data["ACTIVE"] != "false" {
			t.Errorf("record[%d] ACTIVE: expected \"false\", got %q", i, records[i].Data["ACTIVE"])
		}
	}
	// unknown logical value → empty string
	if records[5].Data["ACTIVE"] != "" {
		t.Errorf("record[5] ACTIVE: expected \"\", got %q", records[5].Data["ACTIVE"])
	}

	// float field returned as trimmed string
	score := strings.TrimSpace(records[0].Data["SCORE"])
	if score != "3.14" {
		t.Errorf("record[0] SCORE: expected \"3.14\", got %q", score)
	}

	// memo field returned as trimmed string
	note := strings.TrimSpace(records[0].Data["NOTE"])
	if note != "memo1" {
		t.Errorf("record[0] NOTE: expected \"memo1\", got %q", note)
	}
}

func TestNewFromFileNotFound(t *testing.T) {
	_, err := NewFromFile("/nonexistent/path/file.dbf", WithCP866())
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

func TestNewFromFileAndClose(t *testing.T) {
	r, err := NewFromFile("test.dbf", WithCP866())
	if err != nil {
		t.Skip("test.dbf not available:", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	r, err := NewFromFile("test.dbf", WithCP866())
	if err != nil {
		t.Skip("test.dbf not available:", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close() should be a no-op, got: %v", err)
	}
}

func TestCloseWithoutFile(t *testing.T) {
	data := createMinimalDBF()
	r, err := New(bytes.NewReader(data), WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close() on io.Reader-backed reader returned error: %v", err)
	}
}

func TestErrAfterTruncation(t *testing.T) {
	data := createMinimalDBF()
	tests := []struct {
		name    string
		cut     int // bytes removed from the end
		wantErr error
	}{
		{"inside record", 5, io.ErrUnexpectedEOF},
		{"at record boundary", 11, io.EOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New(bytes.NewReader(data[:len(data)-tt.cut]), WithCP866())
			if err != nil {
				t.Fatalf("New() failed: %v", err)
			}
			count := 0
			for r.Next() {
				if _, err := r.Read(); err != nil {
					t.Fatalf("Read() failed: %v", err)
				}
				count++
			}
			if count != 1 {
				t.Errorf("read %d records, want 1", count)
			}
			err = r.Err()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Err() = %v, want wrapped %v", err, tt.wantErr)
			}
			if err == tt.wantErr {
				t.Error("Err() must wrap the I/O error with context")
			}
			if _, err := r.Read(); err == nil {
				t.Error("Read() after failed Next() should return an error")
			}
		})
	}
}

func TestNextTwiceSkipsRecord(t *testing.T) {
	r, err := New(bytes.NewReader(createMinimalDBF()), WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if !r.Next() || !r.Next() {
		t.Fatal("Next() returned false")
	}
	record, err := r.Read()
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if got := record.Data["NAME"]; got != "Jane Smith" {
		t.Errorf("Read() after two Next() = %q, want %q", got, "Jane Smith")
	}
	if r.Next() {
		t.Error("Next() should return false after the last record")
	}
	if _, err := r.Read(); !errors.Is(err, ErrReadBeforeNext) {
		t.Errorf("Read() after Next() returned false: got %v, want ErrReadBeforeNext", err)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestReadTwiceAfterNext(t *testing.T) {
	r, err := New(bytes.NewReader(createMinimalDBF()), WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if !r.Next() {
		t.Fatal("Next() returned false")
	}
	if _, err := r.Read(); err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if _, err := r.Read(); !errors.Is(err, ErrReadBeforeNext) {
		t.Errorf("second Read() = %v, want ErrReadBeforeNext", err)
	}
}

func TestReadAllAfterNext(t *testing.T) {
	r, err := New(bytes.NewReader(createMinimalDBF()), WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if !r.Next() {
		t.Fatal("Next() returned false")
	}
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("ReadAll() returned %d records, want 2", len(records))
	}
	if records[0].Data["NAME"] != "John Doe" || records[1].Data["NAME"] != "Jane Smith" {
		t.Errorf("ReadAll() returned wrong records: %v, %v", records[0].Data, records[1].Data)
	}
}

func TestReadAllAfterRead(t *testing.T) {
	r, err := New(bytes.NewReader(createMinimalDBF()), WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if !r.Next() {
		t.Fatal("Next() returned false")
	}
	if _, err := r.Read(); err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}
	if len(records) != 1 || records[0].Data["NAME"] != "Jane Smith" {
		t.Errorf("ReadAll() after Read() = %d records, want only Jane Smith", len(records))
	}
}

func TestNextWithoutReadNoError(t *testing.T) {
	r, err := New(bytes.NewReader(createMinimalDBF()), WithCP866())
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	count := 0
	for r.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("Next() returned true %d times, want 2", count)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

// createLargeDBF builds a DBF with n records and 3 fields (NAME C/10, AGE N/3, ACTIVE L/1).
func createLargeDBF(n int) []byte {
	buf := new(bytes.Buffer)

	buf.WriteByte(0x03)
	buf.WriteByte(124)
	buf.WriteByte(1)
	buf.WriteByte(1)

	binary.Write(buf, binary.LittleEndian, uint32(n))
	binary.Write(buf, binary.LittleEndian, uint16(32+32*3+1)) // 3 fields
	binary.Write(buf, binary.LittleEndian, uint16(1+10+3+1))  // deletion + NAME + AGE + ACTIVE

	reserved := make([]byte, 20)
	reserved[17] = 0x26 // CP866
	buf.Write(reserved)

	writeField := func(name string, typ byte, length byte) {
		b := make([]byte, 11)
		copy(b, name)
		buf.Write(b)
		buf.WriteByte(typ)
		buf.Write(make([]byte, 4))
		buf.WriteByte(length)
		buf.WriteByte(0)
		buf.Write(make([]byte, 14))
	}
	writeField("NAME", 'C', 10)
	writeField("AGE", 'N', 3)
	writeField("ACTIVE", 'L', 1)
	buf.WriteByte(0x0D)

	for i := 0; i < n; i++ {
		buf.WriteByte(0x20)
		buf.WriteString("Alice     ") // 10 bytes
		buf.WriteString(" 25")        // 3 bytes
		buf.WriteByte('T')            // 1 byte
	}

	return buf.Bytes()
}

// Benchmark tests
func BenchmarkNew(b *testing.B) {
	data := createMinimalDBF()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(data)
		_, err := New(reader, WithCP866())
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadAll(b *testing.B) {
	data := createLargeDBF(10_000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(data)
		dbf, _ := New(reader, WithCP866())
		_, err := dbf.ReadAll()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNextRead(b *testing.B) {
	data := createLargeDBF(10_000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(data)
		dbf, _ := New(reader, WithCP866())
		for dbf.Next() {
			_, err := dbf.Read()
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

type testField struct {
	name   string
	typ    byte
	length byte
	flags  byte
}

// buildDBF builds a DBF of the given file type with CP1251 LDID.
// Each record is the field data without the deletion flag.
func buildDBF(fileType byte, fields []testField, records ...[]byte) []byte {
	recordSize := 1
	for _, f := range fields {
		recordSize += int(f.length)
	}
	buf := new(bytes.Buffer)
	buf.WriteByte(fileType)
	buf.Write([]byte{124, 1, 15})
	binary.Write(buf, binary.LittleEndian, uint32(len(records)))
	binary.Write(buf, binary.LittleEndian, uint16(32+32*len(fields)+1))
	binary.Write(buf, binary.LittleEndian, uint16(recordSize))
	reserved := make([]byte, 20)
	reserved[17] = 0xC9
	buf.Write(reserved)
	for _, f := range fields {
		b := make([]byte, 32)
		copy(b, f.name)
		b[11] = f.typ
		b[16] = f.length
		b[18] = f.flags
		buf.Write(b)
	}
	buf.WriteByte(0x0D)
	for _, rec := range records {
		buf.WriteByte(0x20)
		buf.Write(rec)
	}
	return buf.Bytes()
}

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func le64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

func readAllData(t *testing.T, data []byte) []map[string]string {
	t.Helper()
	r, err := New(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}
	result := make([]map[string]string, len(records))
	for i, rec := range records {
		result[i] = rec.Data
	}
	return result
}

func TestVFPBinaryTypes(t *testing.T) {
	fields := []testField{
		{"INT", 'I', 4, 0},
		{"DBL", 'B', 8, 0},
		{"CUR", 'Y', 8, 0},
		{"DT", 'T', 8, 0},
		{"MEMO", 'M', 4, 0},
	}
	// 2024-01-15 10:30:00, stored 1 ms early as VFP sometimes does
	jd := uint32(julianDayUnixEpoch + time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix()/86400)
	ms := uint32((10*3600+30*60)*1000 - 1)

	rec1 := slices.Concat(le32(42), le64(math.Float64bits(3.14)), le64(123456), le32(jd), le32(ms), le32(7))
	minusFive := int64(-5)
	rec2 := slices.Concat(le32(0xFFFFFFFF), le64(math.Float64bits(-0.5)), le64(uint64(minusFive)), make([]byte, 8), le32(0))

	got := readAllData(t, buildDBF(0x30, fields, rec1, rec2))
	want := []map[string]string{
		{"INT": "42", "DBL": "3.14", "CUR": "12.3456", "DT": "2024-01-15T10:30:00", "MEMO": "7"},
		{"INT": "-1", "DBL": "-0.5", "CUR": "-0.0005", "DT": "", "MEMO": ""},
	}
	for i := range want {
		if !maps.Equal(got[i], want[i]) {
			t.Errorf("record %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestVFPNullFlagsAndVarchar(t *testing.T) {
	// bits: NUM null=0; NAME varlength=1, null=2
	fields := []testField{
		{"NUM", 'I', 4, flagNullable},
		{"NAME", 'V', 10, flagNullable},
		{"_NullFlags", '0', 1, flagSystem},
	}
	name, _ := charmap.Windows1251.NewEncoder().Bytes([]byte("Иван"))
	short := make([]byte, 10)
	copy(short, name)
	short[9] = byte(len(name))

	rec1 := slices.Concat(le32(5), short, []byte{0x02})
	rec2 := slices.Concat(le32(0), make([]byte, 10), []byte{0x05})
	rec3 := slices.Concat(le32(1), []byte("0123456789"), []byte{0x00})

	// empty, not null
	rec4 := slices.Concat(le32(0), []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, []byte{0x02})

	r, err := New(bytes.NewReader(buildDBF(0x32, fields, rec1, rec2, rec3, rec4)))
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}
	want := []struct {
		data map[string]string
		null map[string]bool
	}{
		{map[string]string{"NUM": "5", "NAME": "Иван"}, nil},
		{map[string]string{"NUM": "", "NAME": ""}, map[string]bool{"NUM": true, "NAME": true}},
		{map[string]string{"NUM": "1", "NAME": "0123456789"}, nil},
		{map[string]string{"NUM": "0", "NAME": ""}, nil},
	}
	for i, w := range want {
		if !maps.Equal(records[i].Data, w.data) {
			t.Errorf("record %d Data = %q, want %q", i, records[i].Data, w.data)
		}
		if !maps.Equal(records[i].Null, w.null) {
			t.Errorf("record %d Null = %v, want %v", i, records[i].Null, w.null)
		}
	}
}

func TestVFPVarcharInvalidLength(t *testing.T) {
	fields := []testField{
		{"NAME", 'V', 4, 0},
		{"_NullFlags", '0', 1, flagSystem},
	}
	data := buildDBF(0x32, fields, []byte{'a', 'b', 'c', 9, 0x01})
	r, err := New(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if _, err := r.ReadAll(); !errors.Is(err, ErrFieldOutOfBounds) {
		t.Errorf("ReadAll() = %v, want ErrFieldOutOfBounds", err)
	}
}

func TestVFPBinaryCharacter(t *testing.T) {
	fields := []testField{{"RAW", 'C', 4, flagBinary}, {"TXT", 'C', 4, 0}}
	got := readAllData(t, buildDBF(0x30, fields, []byte{0x00, 0xCF, ' ', ' ', 0xCF, ' ', ' ', ' '}))
	if got[0]["RAW"] != "\x00\xCF  " {
		t.Errorf("RAW = %q, want raw bytes", got[0]["RAW"])
	}
	if got[0]["TXT"] != "П" {
		t.Errorf("TXT = %q, want %q", got[0]["TXT"], "П")
	}
}

func TestDBaseMemoReference(t *testing.T) {
	fields := []testField{{"NOTES", 'M', 10, 0}}
	got := readAllData(t, buildDBF(0x83, fields, []byte("        12")))
	if got[0]["NOTES"] != "12" {
		t.Errorf("NOTES = %q, want %q", got[0]["NOTES"], "12")
	}
}

func TestUnsupportedFieldType(t *testing.T) {
	tests := []struct {
		name     string
		fileType byte
		typ      byte
	}{
		{"unknown type", 0x03, '@'},
		{"VFP integer in dBase file", 0x03, 'I'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildDBF(tt.fileType, []testField{{"X", tt.typ, 4, 0}}, []byte{1, 2, 3, 4})
			r, err := New(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("New() failed: %v", err)
			}
			if !r.Next() {
				t.Fatalf("Next() returned false: %v", r.Err())
			}
			if _, err := r.Read(); !errors.Is(err, ErrUnsupportedType) {
				t.Errorf("Read() = %v, want ErrUnsupportedType", err)
			}
		})
	}
}

func TestDeletionFlag(t *testing.T) {
	fields := []testField{{"X", 'C', 1, 0}}
	build := func(flags ...byte) []byte {
		records := make([][]byte, len(flags))
		for i := range flags {
			records[i] = []byte{'A' + byte(i)}
		}
		data := buildDBF(0x03, fields, records...)
		recordsStart := len(data) - 2*len(flags)
		for i, f := range flags {
			data[recordsStart+2*i] = f
		}
		return data
	}

	tests := []struct {
		name    string
		data    []byte
		opts    []Option
		count   int
		wantErr error
	}{
		{"active and deleted", build(0x20, 0x2A), nil, 2, nil},
		{"strict active and deleted", build(0x20, 0x2A), []Option{WithStrict()}, 2, nil},
		{"lenient unknown flag", build(0x00, 0xFF), nil, 2, nil},
		{"strict zero flag", build(0x20, 0x00), []Option{WithStrict()}, 1, ErrInvalidDeletionFlag},
		{"strict 0xFF flag", build(0xFF, 0x20), []Option{WithStrict()}, 0, ErrInvalidDeletionFlag},
		{"EOF marker before last record", build(0x20, 0x1A), nil, 1, io.ErrUnexpectedEOF},
		{"strict EOF marker", build(0x1A, 0x20), []Option{WithStrict()}, 0, io.ErrUnexpectedEOF},
		{"EOF marker after last record", append(build(0x20, 0x2A), 0x1A), nil, 2, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New(bytes.NewReader(tt.data), tt.opts...)
			if err != nil {
				t.Fatalf("New() failed: %v", err)
			}
			records, err := r.ReadAll()
			if len(records) != tt.count {
				t.Errorf("ReadAll() returned %d records, want %d", len(records), tt.count)
			}
			if tt.wantErr == nil && err != nil {
				t.Errorf("ReadAll() = %v, want nil", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("ReadAll() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
