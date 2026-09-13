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
- ✅ Support for multiple encodings (CP866, CP1251, CP1252, CP437, CP850)
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
| 0x26   | CP866    | Russian MS-DOS |
| 0x64, 0x65, 0xC9 | CP1251 | Russian Windows |
| 0x03   | CP1252   | Windows ANSI |
| 0x01   | CP437    | US MS-DOS |
| 0x02   | CP850    | International MS-DOS |

You can also specify any encoding manually using `WithEncoding()` or `WithDecoder()`.

## Supported Field Types

| Type | Description | Go Type |
|------|-------------|---------|
| C    | Character   | string  |
| N    | Numeric     | string  |
| D    | Date        | string (YYYYMMDD) |
| L    | Logical     | string ("true"/"false") |
| M    | Memo        | string  |
| F    | Float       | string  |

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