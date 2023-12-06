package ami

import (
	"github.com/pkg/errors"
	"time"
)

var (
	NoConnection = errors.New("no connect for Asterisk")
	Timeout      = errors.New("request timeout")
)

type connect interface {
	Execute(data map[string]string, timeout time.Duration) map[string]string
	Online() bool
}

type Ami struct {
	conn connect
}

func NewAmi(conn connect) *Ami {
	return &Ami{
		conn: conn,
	}
}

func (c *Ami) Action(data map[string]string, timeout time.Duration) (map[string]string, error) {
	if !c.conn.Online() {
		return nil, NoConnection
	}

	response := c.conn.Execute(data, timeout)
	if response["Response"] == "Error" && response["Message"] == "Timeout" {
		return nil, Timeout
	}

	return response, nil
}
