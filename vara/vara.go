package vara

import "errors"

var notImplemented = errors.New("not implemented")

type Modem struct{}

func NewModem() *Modem {
	return &Modem{}
}
