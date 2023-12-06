package config

import "time"

type Config struct {
	Enabled             bool
	Address             string
	Username            string
	Password            string
	TimeoutConnection   time.Duration
	KeepAliveConnection time.Duration
	PingTimeout         time.Duration
	FailurePing         int
	TryConnectCount     int
	TryConnectTimeout   time.Duration
}
