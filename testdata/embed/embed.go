package embed

type Base struct {
	ID int
}

type Child struct {
	Base
	Name string
}

type Reader interface {
	Read() string
}

type Writer interface {
	Reader
	Write(s string)
}
