package plug

import (
	"bufio"
	"bytes"
	"context"
	"github.com/rs/zerolog"
	"net"
	"strings"
)

type Connect struct {
	log       zerolog.Logger
	conn      net.Conn
	ctx       context.Context
	cancel    context.CancelFunc
	writeChan chan map[string]string
}

func (c *Connect) read(r *bufio.Reader) (map[string]string, error) {
	m := make(map[string]string)
	var responseFollows bool
	var outputExist = false

	for {
		kv, _, err := r.ReadLine()
		if len(kv) == 0 {
			return m, err
		}

		var key string
		i := bytes.IndexByte(kv, ':')
		if i >= 0 {
			endKey := i
			for endKey > 0 && kv[endKey-1] == ' ' {
				endKey--
			}
			key = string(kv[:endKey])
		}

		if key == "" && !responseFollows {
			if err != nil {
				return m, err
			}

			continue
		}

		i++
		for i < len(kv) && (kv[i] == ' ' || kv[i] == '\t') {
			i++
		}
		value := string(kv[i:])

		if key == "Output" && !outputExist {
			m["RealOutput"] = value
			outputExist = true
		} else {
			m[key] = value
		}

		if err != nil {
			return m, err
		}
	}
}

func (c *Connect) reader() {
	bufReader := bufio.NewReader(c.conn)
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		request, err := c.read(bufReader)
		if err != nil {
			c.log.Info().Err(err).Msg("ошибка в tcp-соединении с клиентом, закрываем подключение")
			c.cancel()
			return
		}

		c.handle(request)
	}
}

func (c *Connect) hello() {
	if _, err := c.conn.Write(make([]byte, 100)); err != nil {
		c.log.Info().Err(err).Msg("ошибка в tcp-соединении с клиентом, закрываем подключение")
		c.cancel()
		return
	}
	c.log.Info().Msg("приветствуем новое подключение")
}

func (c *Connect) listen() {
	go c.reader()
	go c.writer()

	<-c.ctx.Done()
	_ = c.conn.Close()
	c.log.Info().Msg("закрыли подключение")
}

func (c *Connect) writer() {
	for {
		select {
		case msg := <-c.writeChan:
			c.log.Debug().Any("request", msg).Msg("исходящий запрос клиенту")
			if err := c.write(msg); err != nil {
				c.log.Info().Err(err).Msg("ошибка в tcp-соединении с клиентом, закрываем подключение")
				c.cancel()
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Connect) write(data map[string]string) error {
	if _, err := c.conn.Write(c.serialize(data)); err != nil {
		return err
	}

	return nil
}

func (c *Connect) serialize(data map[string]string) []byte {
	var outBuf bytes.Buffer

	for key := range data {
		outBuf.WriteString(key)
		outBuf.WriteString(": ")
		outBuf.WriteString(data[key])
		outBuf.WriteString("\r\n")
	}
	outBuf.WriteString("\r\n")

	return outBuf.Bytes()
}

func (c *Connect) handle(request map[string]string) {
	actionID := request["ActionID"]

	if len(actionID) == 0 {
		return
	}

	action := request["Action"]

	//место под различные обработчики
	switch strings.ToLower(action) {
	default:
		c.writeChan <- map[string]string{"ActionID": actionID, "Message": "Authentication accepted", "Response": "Success"}
	}
}
