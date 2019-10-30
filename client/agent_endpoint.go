package client

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/hashicorp/nomad/command/agent/monitor"
	"github.com/hashicorp/nomad/helper"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/ugorji/go/codec"

	metrics "github.com/armon/go-metrics"
	log "github.com/hashicorp/go-hclog"
	cstructs "github.com/hashicorp/nomad/client/structs"
)

type Agent struct {
	c *Client
}

func NewAgentEndpoint(c *Client) *Agent {
	m := &Agent{c: c}
	m.c.streamingRpcs.Register("Agent.Monitor", m.monitor)
	return m
}

func (m *Agent) monitor(conn io.ReadWriteCloser) {
	defer metrics.MeasureSince([]string{"client", "monitor", "monitor"}, time.Now())
	defer conn.Close()

	// Decode arguments
	var args cstructs.MonitorRequest
	decoder := codec.NewDecoder(conn, structs.MsgpackHandle)
	encoder := codec.NewEncoder(conn, structs.MsgpackHandle)

	if err := decoder.Decode(&args); err != nil {
		handleStreamResultError(err, helper.Int64ToPtr(500), encoder)
		return
	}

	// Check acl
	if aclObj, err := m.c.ResolveToken(args.AuthToken); err != nil {
		handleStreamResultError(err, helper.Int64ToPtr(403), encoder)
		return
	} else if aclObj != nil && !aclObj.AllowAgentRead() {
		handleStreamResultError(structs.ErrPermissionDenied, helper.Int64ToPtr(403), encoder)
		return
	}

	logLevel := log.LevelFromString(args.LogLevel)
	if args.LogLevel == "" {
		logLevel = log.LevelFromString("INFO")
	}

	if logLevel == log.NoLevel {
		handleStreamResultError(errors.New("Unknown log level"), helper.Int64ToPtr(400), encoder)
		return
	}

	stopCh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer close(stopCh)
	defer cancel()

	monitor := monitor.New(512, m.c.logger, &log.LoggerOptions{
		JSONFormat: args.LogJSON,
		Level:      logLevel,
	})

	go func() {
		if _, err := conn.Read(nil); err != nil {
			// One end of the pipe explicitly closed, exit
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		}
	}()

	logCh := monitor.Start(stopCh)

	var streamErr error
OUTER:
	for {
		select {
		case log := <-logCh:
			var resp cstructs.StreamErrWrapper
			resp.Payload = log
			if err := encoder.Encode(resp); err != nil {
				streamErr = err
				break OUTER
			}
			encoder.Reset(conn)
		case <-ctx.Done():
			break OUTER
		}
	}

	if streamErr != nil {
		// Nothing to do as conn is closed
		if streamErr == io.EOF || strings.Contains(streamErr.Error(), "closed") {
			return
		}

		// Attempt to send the error
		encoder.Encode(&cstructs.StreamErrWrapper{
			Error: cstructs.NewRpcError(streamErr, helper.Int64ToPtr(500)),
		})
		return
	}
}
