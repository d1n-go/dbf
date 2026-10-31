# DBF Reader

[![CI](https://github.com/d1n-go/dbf/actions/workflows/ci.yml/badge.svg)](https://github.com/d1n-go/dbf/actions/workflows/ci.yml)
[![Coverage](https://codecov.io/gh/d1n-go/dbf/graph/badge.svg)](https://codecov.io/gh/d1n-go/dbf)
[![Go Reference](https://pkg.go.dev/badge/github.com/d1n-go/dbf.svg)](https://pkg.go.dev/github.com/d1n-go/dbf)
[![Release](https://img.shields.io/github/v/release/d1n-go/dbf)](https://github.com/d1n-go/dbf/releases)
[![License: MIT](https://img.shields.io/github/license/d1n-go/dbf)](LICENSE)

A pure Go library for reading DBF (dBase, FoxPro, Visual FoxPro) files with support for multiple encodings.

## Features

- ✅ Read DBF files in various formats (dBase III, FoxPro, Visual FoxPro)
- ✅ Automatic encoding detection from Language Driver ID
- ✅ Support for multiple encodings (CP866, CP1251, CP1252, CP437, CP850, CP852)
- ✅ Memory-efficient streaming for large files
- ✅ Simple, idiomatic Go API
- ✅ No external dependencies except `golang.org/x/text`

## Installation

```bash
go get github.com/d1n-go/dbf
```

## Quick Start

```go
package main

import (
    "fmt"
    "log"
    
    "github.com/d1n-go/dbf"
)

func main() {
    // Open DBF file with explicit encoding
    reader, err := dbf.NewFromFile("data.dbf", dbf.WithCP866())
    if err != nil {
        log.Fatal(err)
    }
    
    // Read all records
    records, err := reader.ReadAll()
    if err != nil {
        log.Fatal(err)
    }
    
    // Process records
    for i, record := range records {
        if record.Deleted {
            continue // Skip deleted records
        }
        fmt.Printf("Record %d: %v\n", i+1, record.Data)
    }
}
```

## Usage Examples

### Streaming Large Files

For large files, use streaming to avoid loading everything into memory:

```go
reader, err := dbf.NewFromFile("large.dbf", dbf.WithCP866())
if err != nil {
    log.Fatal(err)
}

for reader.Next() {
    record, err := reader.Read()
    if err != nil {
        log.Fatal(err)
    }
    
    // Process record
    name := record.Data["NAME"]
    fmt.Println(name)
}

if err := reader.Err(); err != nil {
    log.Fatal(err)
}
```

### Auto-detect Encoding

If the DBF file has a valid Language Driver ID, encoding can be auto-detected:

```go
reader, err := dbf.NewFromFile("data.dbf") // No encoding specified
if err != nil {
    log.Fatal(err)
}
```

### Specify Custom Encoding

```go
import "golang.org/x/text/encoding/charmap"

// Using convenience function
reader, err := dbf.NewFromFile("data.dbf", dbf.WithCP1251())

// Using charmap directly
reader, err := dbf.NewFromFile("data.dbf", dbf.WithEncoding(charmap.Windows1251))

// Using custom decoder
decoder := charmap.CodePage850.NewDecoder()
reader, err := dbf.NewFromFile("data.dbf", dbf.WithDecoder(decoder))
```

### Strict Mode

By default any deletion flag other than `*` marks an active record. With `WithStrict()`,
a record whose flag is neither ` ` nor `*` stops reading with `ErrInvalidDeletionFlag`:

```go
reader, err := dbf.NewFromFile("data.dbf", dbf.WithCP866(), dbf.WithStrict())
```

In both modes an end-of-file marker (`0x1A`) before the number of records declared
in the header is reported as an error wrapping `io.ErrUnexpectedEOF`.

### Read from io.Reader

```go
file, err := os.Open("data.dbf")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

reader, err := dbf.New(file, dbf.WithCP866())
if err != nil {
    log.Fatal(err)
}
```

### Access Field Metadata

```go
reader, err := dbf.NewFromFile("data.dbf", dbf.WithCP866())
if err != nil {
    log.Fatal(err)
}

fmt.Printf("File Type: %s\n", reader.FileType())
fmt.Printf("Last Update: %s\n", reader.LastUpdate())
fmt.Printf("Total Records: %d\n", reader.RecordsCount())

fmt.Println("\nFields:")
for i, field := range reader.Fields() {
    fmt.Printf("%d. %s (%s) - Length: %d\n",
        i+1,
        field.Name,
        field.TypeString(),
        field.Length,
    )
}
```

## Supported Encodings

The library automatically detects these encodings from Language Driver ID:

| LDID   | Encoding | Description |
|--------|----------|-------------|
| 0x26, 0x65 | CP866 | Russian MS-DOS |
| 0x64   | CP852    | Eastern European MS-DOS |
| 0xC9   | CP1251   | Russian Windows |
| 0x03   | CP1252   | Windows ANSI |
| 0x01   | CP437    | US MS-DOS |
| 0x02   | CP850    | International MS-DOS |

You can also specify any encoding manually using `WithEncoding()` or `WithDecoder()`.

## Supported Field Types

| Type | Description | Returned value |
|------|-------------|----------------|
| C    | Character   | decoded text, spaces trimmed; raw bytes if the VFP binary flag is set |
| N    | Numeric     | text as stored, e.g. `"123.45"` |
| F    | Float       | text as stored |
| D    | Date        | `YYYYMMDD` |
| L    | Logical     | `"true"`, `"false"` or `""` |
| M, G, P | Memo, General, Picture | memo block number, e.g. `"12"`; `""` if empty (memo file contents are not read) |
| B    | Double (VFP) | e.g. `"3.14"`; in dBase files, a memo block number like M |
| I    | Integer (VFP) | e.g. `"-42"` |
| Y    | Currency (VFP) | four decimal places, e.g. `"12.3400"` |
| T    | DateTime (VFP) | `2006-01-02T15:04:05`, `""` if empty |
| V    | Varchar (VFP) | decoded text, not trimmed |
| Q    | Varbinary (VFP) | raw bytes |
| W    | Blob (VFP)  | memo block number |

Visual FoxPro null values are returned as `""` and marked in `Record.Null`, so they can be told apart from empty values:

```go
if record.Null["PRICE"] {
    // PRICE is null
}
```

The hidden `_NullFlags` system field is not included in `Record.Data`.
Reading a field of any other type returns `ErrUnsupportedType`.

All field values are returned as strings. Parse them as needed:

```go
age, _ := strconv.Atoi(record.Data["AGE"])
price, _ := strconv.ParseFloat(record.Data["PRICE"], 64)
date, _ := time.Parse("20060102", record.Data["BIRTHDATE"])
```

## API Documentation

Full API documentation is available at [pkg.go.dev](https://pkg.go.dev/github.com/d1n-go/dbf).

## Testing

```bash
# Run tests
go test -v

# Run tests with coverage
go test -v -cover

# Run benchmarks
go test -bench=.
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Related Projects

- [go-dbase](https://github.com/Valentin-Kaiser/go-dbase) - Read and write DBF files
- [go-foxpro-dbf](https://github.com/SebastiaanKlippert/go-foxpro-dbf) - FoxPro DBF reader
- [go-dbf](https://github.com/LindsayBradford/go-dbf) - Another DBF library

## Acknowledgments

DBF format specification references:
- [ClicketyClick DBF Format](http://www.clicketyclick.dk/databases/xbase/format/)
- [dBase File Structure](http://www.independent-software.com/dbase-dbf-dbt-file-format.html)