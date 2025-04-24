package eventsource

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

type testFlusher struct {
	in  bytes.Buffer
	out bytes.Buffer
}

func (f *testFlusher) Write(data []byte) (int, error) {
	return f.in.Write(data)
}

func (f *testFlusher) Flush() {
	io.Copy(&f.out, &f.in)
}

func TestEncoderFlush(t *testing.T) {
	t.Parallel()

	buf := &testFlusher{}
	enc := NewEncoder(buf)
	enc.WriteField("data", []byte("data"))
	enc.Flush()

	if buf.out.String() != "data: data\n\n" {
		t.Fatal("Encoder.Flush did not flush underlying writer")
	}
}

func TestWriteField(t *testing.T) {
	t.Parallel()

	table := []struct {
		field string
		value []byte
		out   string
		error
	}{
		{"data", []byte("data"), "data: data\n", nil},
		{"data", nil, "data\n", nil},
		{"\xFF\xFE\xFD", nil, "", ErrInvalidEncoding},
		{"data", []byte("\xFF\xFE\xFD"), "", ErrInvalidEncoding},
		{"data", []byte("a\nb\nc\n"), "data: a\ndata: b\ndata: c\ndata\n", nil},
		{"data", []byte("a\r\nb\r\nc"), "data: a\ndata: b\ndata: c\n", nil},
	}

	for i, tt := range table {
		buf := new(bytes.Buffer)

		err := NewEncoder(buf).WriteField(tt.field, tt.value)

		if tt.error != nil && err == tt.error {
			continue
		}

		if tt.error != err {
			t.Errorf("%d. expected err=%q, got %q", i, tt.error, err)
			continue
		}

		if buf.String() != tt.out {
			t.Errorf("%d. expected %q, got %q", i, tt.out, buf.String())
		}
	}
}

func TestWriteStream(t *testing.T) {
	t.Parallel()

	table := []struct {
		field string
		value []byte
		out   string
		error
	}{
		{"data", []byte("data"), "data: data\n", nil},
		{"data", nil, "data\n", nil},
		{"data", []byte("\n"), "data\n", nil},
		{"data", []byte("\r\n"), "data\n", nil},
		{"data", []byte("\n\n\n"), "data\ndata\ndata\n", nil},
		{"data", []byte("\r\nhi\r\n\r\n"), "data\ndata: hi\ndata\n", nil},
		{"\xFF\xFE\xFD", nil, "", ErrInvalidEncoding},
		{"data", []byte("\xFF\xFE\xFD"), "data: \uFFFD\n", nil},
		{"data", []byte("hello \xe4\xb8"), "data: hello \uFFFD\n", nil}, // Incomplete '世'
		{"data", []byte("\xffhello\xff\xfe world\xfd"), "data: \uFFFDhello\uFFFD world\uFFFD\n", nil},
		{"data", []byte("a\nb\nc\n"), "data: a\ndata: b\ndata: c\ndata\n", nil},
		{"data", []byte("a\r\nb\r\nc"), "data: a\ndata: b\ndata: c\n", nil},
	}

	for i, tt := range table {
		buf := new(bytes.Buffer)

		err := NewEncoder(buf).WriteStream(tt.field, bytes.NewReader(tt.value))

		if tt.error != nil && err == tt.error {
			continue
		}

		if tt.error != err {
			t.Errorf("%d. expected err=%q, got %q", i, tt.error, err)
			continue
		}

		if buf.String() != tt.out {
			t.Errorf("%d. expected %q, got %q", i, tt.out, buf.String())
		}
	}
}

// TestLargeBuffer tests handling of very large buffers
func TestLargeBuffer(t *testing.T) {
	// Create a large buffer with a mix of ASCII and multi-byte characters
	var builder strings.Builder
	for i := 0; i < 1000; i++ {
		builder.WriteString("Line " + string(rune(i%100+12345)) + " with some ASCII and multi-byte content")
		if i < 990 {
			builder.WriteString(" + ")
		} else {
			builder.WriteByte('\n')
		}
	}
	largeData := []byte(builder.String())

	// Test with a limited-size buffer to force multiple reads
	buf := new(bytes.Buffer)
	r := bytes.NewReader(largeData)

	// Use a small buffer size for the scanner to test buffer handling
	encoder := NewEncoder(buf)

	err := encoder.WriteStream("data", r)
	if err != nil {
		t.Fatalf("WriteStream failed: %v", err)
	}

	// Verify output by reading line by line
	result := buf.String()
	originalLines := bytes.Split(largeData, []byte("\n"))
	resultLines := strings.Split(result, "\n")

	// Check each line (except the last empty one)
	for i, line := range originalLines {
		if len(line) == 0 {
			continue
		}
		expected := "data: " + string(line)
		if i < len(resultLines) && resultLines[i] != expected {
			t.Errorf("line %d mismatch: got %q, want %q", i, resultLines[i], expected)
			break
		}
	}
}

func TestFindLastCompletePos(t *testing.T) {
	// examples of 1, 2, 3 and 4 byte utf8 characters
	const (
		r1 = '0'
		r2 = 'á'
		r3 = 'ᄅ'
		r4 = '𐅻'
	)

	for i, str1 := range []string{string(r1), string(r2), string(r3), string(r4)} {
		var pos int

		pos = findLastCompleteUTF8Position([]byte(str1))
		if pos != len(str1) {
			t.Errorf("%d. expected %d, got %d", i, len(str1), pos)
		}

		testStr := str1[:len(str1)-1] // cut off last byte
		pos = findLastCompleteUTF8Position([]byte(testStr))
		if pos != 0 {
			t.Errorf("%d. expected %d, got %d", i, len(str1), pos)
		}

		for j, str2 := range []string{string(r1), string(r2), string(r3), string(r4)} {
			pos = findLastCompleteUTF8Position([]byte(str1 + str2))
			if expected := len(str1) + len(str2); pos != expected {
				t.Errorf("%d+%d. expected %d, got %d", i, j, expected, pos)
			}

			testStr = str1 + str2[:len(str2)-1] // cut off last byte
			pos = findLastCompleteUTF8Position([]byte(testStr))
			if pos != len(str1) {
				t.Errorf("%d+%d. expected %d, got %d", i, j, len(str1), pos)
			}
		}
	}
}

func TestEncoderEncode(t *testing.T) {
	t.Parallel()

	table := []struct {
		Event
		expected string
	}{
		{Event{Type: "type"}, "event: type\ndata\n\n"},
		{Event{ID: "123"}, "id: 123\ndata\n\n"},
		{Event{Retry: "10000"}, "retry: 10000\ndata\n\n"},
		{Event{Data: []byte("data")}, "data: data\n\n"},
		{Event{ResetID: true}, "id\ndata\n\n"},
	}

	for i, tt := range table {
		buf := new(bytes.Buffer)

		if err := NewEncoder(buf).Encode(tt.Event); err != nil {
			t.Errorf("%d. write error: %q", i, err)
			continue
		}

		if buf.String() != tt.expected {
			t.Errorf("%d. expected %q, got %q", i, tt.expected, buf.String())
		}
	}
}
