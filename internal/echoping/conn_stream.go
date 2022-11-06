package echoping

import (
	"time"
)

type ConnStream interface {
	Read(b []byte) (n int, err error)

	Write(b []byte) (n int, err error)
	Close() error
	SetDeadline(t time.Time) error
}
