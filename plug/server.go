package plug

import (
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"net"
)

// Server эмулирующий работу сервера астериска
type Server struct {
	ctx     context.Context
	address string
	log     zerolog.Logger
}

func NewServer(ctx context.Context, port int, log zerolog.Logger) error {
	s := &Server{
		ctx:     ctx,
		address: fmt.Sprintf(":%v", port),
	}
	s.log = log.With().Str("target_server", s.address).Logger()

	return nil
}

func (s *Server) Connect() error {
	ln, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}

	defer ln.Close()
	s.log.Info().Msg("сервер запущен")

	for {
		// Accept a connection
		conn, err := ln.Accept()
		s.log.Info().Msg("принять подключение")
		if err != nil {
			s.log.Info().Err(err).Msg("ошибка принятия подключения")
			continue
		}

		// Handle the connection in a new goroutine
		go s.handleRequest(conn)
	}
}

func (s *Server) handleRequest(conn net.Conn) {
	c := Connect{
		log:       s.log,
		conn:      conn,
		writeChan: make(chan map[string]string, 1000),
	}

	c.ctx, c.cancel = context.WithCancel(s.ctx)

	c.hello()
	c.listen()
}
