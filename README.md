## common-iface [![GoDoc](https://godoc.org/vbom.ml/common-iface?status.svg)](https://godoc.org/vbom.ml/common-iface)

`common-iface` is a small utility that prints out an interface type representing the common interface of the types passed on the command line.

For example, suppose you want to parse some data from either memory (contained in a `bytes.Reader`) or a buffered network connection (`bufio.Reader`). You want to know what methods will be available, so you run:
```go
$ common-iface bytes.Reader bufio.Reader
interface {
	Read(b []byte) (n int, err error)
	ReadByte() (byte, error)
	ReadRune() (ch rune, size int, err error)
	UnreadByte() error
	UnreadRune() error
	WriteTo(w io.Writer) (n int64, err error)
}
```
Or if you want to be able to handle both files and network connections:
```go
$ common-iface os.File net.Conn
interface {
	Close() error
	Read(b []byte) (n int, err error)
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Write(b []byte) (n int, err error)
}

```

This can then e.g. be copy-pasted into a source file to define a local interface type (and optionally trimmed down to remove unused methods).
