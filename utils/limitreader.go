package utils

import "io"

type ReaderAndByteReader interface {
	io.Reader
	io.ByteReader
}

// LimitByteReader returns a Reader that reads from r
// but stops with EOF after n bytes.
// The underlying implementation is a *LimitedReader.
func LimitByteReader(r ReaderAndByteReader, n int64) *LimitedByteReader {
	return &LimitedByteReader{r, n}
}

// A LimitedByteReader reads from R but limits the amount of
// data returned to just N bytes. Each call to Read
// updates N to reflect the new amount remaining.
// Read returns EOF when N <= 0 or when the underlying R returns EOF.
type LimitedByteReader struct {
	R ReaderAndByteReader // underlying reader
	N int64               // max bytes remaining
}

func (l *LimitedByteReader) Read(p []byte) (n int, err error) {
	if l.N <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > l.N {
		p = p[0:l.N]
	}
	n, err = l.R.Read(p)
	l.N -= int64(n)
	return
}

func (l *LimitedByteReader) ReadByte() (v byte, err error) {
	if l.N <= 0 {
		return 0, io.EOF
	}
	v, err = l.R.ReadByte()
	l.N--
	return
}

func (l *LimitedByteReader) Left() int64 {
	return l.N
}
