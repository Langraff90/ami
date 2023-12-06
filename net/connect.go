package net

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/Langraff90/ami/config"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Connect struct {
	connected atomic.Bool
	conn      net.Conn
	ctx       context.Context
	cancel    context.CancelFunc
	pCtx      context.Context
	cfg       config.Config
	pingChan  chan struct{}
	writeChan chan map[string]string
	errChan   chan error
	rsp       sync.Map
	log       zerolog.Logger
}

func NewConnect(ctx context.Context, cfg config.Config, log zerolog.Logger) (*Connect, error) {
	c := &Connect{
		pCtx:      ctx,
		cfg:       cfg,
		pingChan:  make(chan struct{}),
		writeChan: make(chan map[string]string, 1000),
		errChan:   make(chan error),
	}

	c.log = log.With().Str("target_server", c.cfg.Address).Logger()

	if !cfg.Enabled {
		return c, nil
	}

	if err := c.connect(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Connect) Online() bool {
	return c.connected.Load()
}

func (c *Connect) Execute(data map[string]string, timeout time.Duration) map[string]string {
	if data["ActionID"] == "" {
		data["ActionID"] = uuid.New().String()
	}

	actionID := data["ActionID"]
	ch := make(chan map[string]string)
	c.rsp.Store(actionID, ch)

	c.writeChan <- data

	timer := time.AfterFunc(timeout, func() {
		rspCh, loaded := c.rsp.LoadAndDelete(actionID)
		if !loaded {
			return
		}

		rspCh.(chan map[string]string) <- map[string]string{"ActionID": actionID, "Message": "Timeout", "Response": "Error"}
	})

	rsp := <-ch
	timer.Stop()
	close(ch)

	return rsp
}

func (c *Connect) connect() error {
	var err error
	for i := 0; i < c.cfg.TryConnectCount; i++ {
		conn, cErr := c.open()
		if cErr != nil {
			err = cErr
			c.log.Error().Int("attempt_number", i).Err(cErr).Msg("попытка соединения с Asterisk")
			time.Sleep(c.cfg.TryConnectTimeout)
			continue
		}

		lErr := c.login(conn)
		if lErr != nil {
			c.log.Error().Int("attempt_number", i).Err(lErr).Msg("попытка аутентификации в Asterisk")
			_ = conn.Close()
			err = lErr
			time.Sleep(c.cfg.TryConnectTimeout)
			continue
		}

		c.log.Info().Msg("подключение к Asterisk успешно")

		c.ctx, c.cancel = context.WithCancel(c.pCtx)
		c.conn = conn
		c.connected.Store(true)

		go c.listen()

		return nil
	}

	return err
}

func (c *Connect) listen() {
	go c.reader()
	go c.writer()
	go c.pinger()

	select {
	case <-c.ctx.Done():
		_ = c.conn.Close()
		return
	case err := <-c.errChan:
		c.log.Info().Err(err).Msg("ошибка в tcp-соединении с Asterisk, пробуем реконнект")

		c.cancel()
		c.connected.Store(false)
		_ = c.conn.Close()
		cErr := c.connect()
		if cErr != nil {
			c.log.Error().Err(cErr).Msg("реконнект с Asterisk провален")
		}
	}
}

func (c *Connect) pinger() {
	ticker := time.NewTicker(c.cfg.KeepAliveConnection)
	defer ticker.Stop()
	ping := map[string]string{"Action": "Ping", "ActionID": "MagDeliveryPingID"}
	invalid := 0
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if !c.Online() {
				return
			}
			c.writeChan <- ping
			timer := time.NewTimer(c.cfg.PingTimeout)

			select {
			case <-c.ctx.Done():
				return
			case <-c.pingChan:
				timer.Stop()
				invalid = 0
				continue
			case <-timer.C:
				c.log.Error().Int("attempt_number", invalid).Msg("ping Asterisk провален, пробуем еще раз")
				invalid++
				if invalid == c.cfg.FailurePing {
					c.errChan <- errors.New("ping timeout")
					return
				}
				timer.Stop()
				continue
			}
		}
	}
}

func (c *Connect) writer() {
	for {
		select {
		case msg := <-c.writeChan:
			c.log.Debug().Any("request", msg).Msg("исходящий запрос в Asterisk")
			if err := c.write(msg); err != nil {
				c.errChan <- err
				return
			}
		case <-c.ctx.Done():
			return
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

		response, err := c.read(bufReader)
		if err != nil {
			c.errChan <- err
			return
		}

		actionID := response["ActionID"]
		if actionID == "MagDeliveryPingID" {
			c.pingChan <- struct{}{}
			c.log.Debug().Any("response", response).Msg("ответ на ping")
			continue
		}

		if len(actionID) == 0 {
			c.log.Trace().Any("data", response).Msg("входящие данные от Asterisk")
			continue
		}

		rspCh, loaded := c.rsp.LoadAndDelete(actionID)
		if !loaded {
			c.log.Info().Any("response", response).Msg("ответ от Asterisk отброшен")
			continue
		}

		rspCh.(chan map[string]string) <- response
		c.log.Debug().Any("response", response).Msg("ответ от Asterisk")
	}
}

func (c *Connect) open() (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   c.cfg.TimeoutConnection,
		KeepAlive: c.cfg.KeepAliveConnection,
	}

	conn, cErr := dialer.Dial("tcp", c.cfg.Address)
	if cErr != nil {
		return nil, cErr
	}

	greetings := make([]byte, 100)
	if _, err := conn.Read(greetings); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return conn, nil
}

func (c *Connect) login(conn net.Conn) error {
	var action = map[string]string{"Action": "Login", "ActionID": uuid.New().String(), "Username": c.cfg.Username, "Secret": c.cfg.Password}

	_, err := conn.Write(c.serialize(action))
	if err != nil {
		return err
	}

	result, err := c.read(bufio.NewReader(conn))
	if err != nil {
		return err
	}

	if result["Response"] != "Success" && result["Message"] != "Authentication accepted" {
		return errors.New(result["Message"])
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

func (c *Connect) write(data map[string]string) error {
	if _, err := c.conn.Write(c.serialize(data)); err != nil {
		return err
	}

	return nil
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

		if responseFollows && key != "Privilege" && key != "ActionID" {
			if string(kv) != "--END COMMAND--" {
				if len(m["CommandResponse"]) == 0 {
					m["CommandResponse"] = string(kv)
				} else {
					m["CommandResponse"] = fmt.Sprintf("%s\n%s", m["CommandResponse"], string(kv))
				}
			}

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

		if key == "Response" && value == "Follows" {
			responseFollows = true
		}

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

func (c *Connect) Enabled() bool {
	return c.cfg.Enabled
}
