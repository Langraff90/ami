Client for Asterisk AMI

Example:

```
import (
    "context"
    "fmt"
    "github.com/Langraff90/ami"
    "github.com/Langraff90/ami/config"
    "github.com/Langraff90/ami/net"
    "github.com/rs/zerolog/log"
    "time"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())

    cfg := config.Config{
        Enabled:             true,
        Address:             "localhost",
        Username:            "5676",
        Password:            "some_password",
        TimeoutConnection:   10,
        KeepAliveConnection: 10,
        PingTimeout:         10,
        FailurePing:         10,
        TryConnectCount:     10,
        TryConnectTimeout:   10,
    }

    connect, err := net.NewConnect(ctx, cfg, log.Logger)
    if err != nil {
        cancel()
        panic(err)
    }

    client := ami.NewAmi(connect)
    request := map[string]string{
        "Action":   "Originate",
        "ActionID": "some_action_id",
        "Channel":  "some_channel",
        "Context":  "delivery",
        "Exten":    "some_client_phone",
        "Callerid": "some_caller_id",
        "Priority": "1",
    }

    response, err := client.Action(request, time.Second)
    if err != nil {
        cancel()
        panic(err)
    }

    fmt.Println(response)
    cancel()
}

```