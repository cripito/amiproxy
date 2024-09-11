package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cripito/amilib"
	amiresponses "github.com/cripito/amilib/responses"
	rid "github.com/cripito/amilib/rid"
	amitools "github.com/cripito/amilib/tools"
	"github.com/cripito/amiproxy/logs"
	"github.com/lrita/cmap"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/enriquebris/goconcurrentqueue"
	"github.com/rs/zerolog"
)

const (
	TYPE             = "tcp"
	chanGiveup       = 10 * time.Millisecond // timeout to giveup sending to a channel
	AstManagerHeader = "Asterisk Call Manager"
)

var (
	errNoAMI = errors.New("server doesn`t have AMI interface")
)

type Asterisk struct {
	// Asterisk ami configuration
	cfg *amilib.ConfigData

	//node *amitools.Node

	// Connection Handler
	conn net.Conn

	events chan *amitools.Event

	errors chan error

	stop chan struct{}
	wg   sync.WaitGroup

	queueChannel chan struct{}

	tickers *time.Ticker

	connected bool

	fifo     *goconcurrentqueue.FIFO
	fifodone chan struct{}

	responses cmap.Cmap

	//Log handler
	tLogger zerolog.Logger

	//buffer size
	receiveChunkSize int

	mu sync.Mutex
}

func New(c amilib.Config) *Asterisk {

	cfgdata, err := c.Get()
	if err != nil {
		return nil
	}

	ast := &Asterisk{
		cfg: cfgdata,

		//responses:    make(chan *amiresponses.Response),
		events:       make(chan *amitools.Event),
		stop:         make(chan struct{}),
		fifo:         goconcurrentqueue.NewFIFO(),
		fifodone:     make(chan struct{}),
		queueChannel: make(chan struct{}),

		connected: false,
	}

	return ast
}

func (as *Asterisk) Listen(ctx context.Context) error {

	as.wg.Add(2)

	go as.run(ctx)
	go as.reading(ctx)

	return nil
}

func (as *Asterisk) Connect(ctx context.Context) error {

	var err error

	as.tLogger.Trace().Msgf("connecting to %s", as.cfg.Server+":"+strconv.Itoa(as.cfg.Port))

	var dialer net.Dialer
	as.conn, err = dialer.Dial(TYPE, as.cfg.Server+":"+strconv.Itoa(as.cfg.Port))
	if err != nil {
		return err
	}

	reader := bufio.NewReader(as.conn)

	message, err := reader.ReadString('\n')
	if err == io.EOF {
		return err
	}

	if err != nil {
		return err
	}

	if !strings.Contains(message, AstManagerHeader) {
		return errNoAMI
	}

	as.connected = true

	return nil
}

func (ami *Asterisk) sendString(message string) error {
	ami.tLogger.Debug().Msgf("Sending %s", message)

	//ami.mu.Lock()
	//defer ami.mu.Unlock()

	//_, err := ami.conn.Write([]byte(message))
	_, err := fmt.Fprintf(ami.conn, "%s", message)

	return err
}

func (ami *Asterisk) Ping(ctx context.Context, actionID string, node *amitools.Node) (*amiresponses.ResponseData, error) {
	var ping = struct{}{}

	resp := ami.send(ctx, "Ping", actionID, ping, node)
	if resp != nil && resp.GetType() == amiresponses.Error {
		err := fmt.Errorf("error: %s", resp.GetMessage())

		ami.tLogger.Error().Msg(err.Error())

		return nil, err

	}

	if resp != nil && resp.Node == nil {
		resp.Node = node
	}

	if resp == nil {
		err := fmt.Errorf("error: %s", "response not found")

		ami.tLogger.Error().Msg(err.Error())

		return nil, err

	}

	return resp, nil
}

// CoreSHowSettings
func (ami *Asterisk) CoreSettings(ctx context.Context, actionID string, node *amitools.Node) (*amiresponses.ResponseData, error) {
	var settings = struct {
	}{}

	resp := ami.send(ctx, "coresettings", actionID, settings, node)
	if resp != nil && resp.GetType() == amiresponses.Error {
		err := fmt.Errorf("error: %s", resp.GetMessage())

		ami.tLogger.Error().Msg(err.Error())

		return nil, err

	}

	return resp, nil
}

// Login provides the login manager.
func (ami *Asterisk) Login(ctx context.Context, actionID string, node *amitools.Node) error {

	var events string = "system" //"system,call,log"
	var login = struct {
		Username string `ami:"Username"`
		Secret   string `ami:"Secret"`
		Events   string `ami:"Events,omitempty"`
		AuthType string `ami:"AuthType,omitempty"`
	}{Username: ami.cfg.UserName, Secret: ami.cfg.Password, Events: events, AuthType: "Plain"}

	resp := ami.send(ctx, "login", actionID, login, node)
	if resp != nil && resp.GetType() == amiresponses.Error {
		err := fmt.Errorf("error: %s", resp.GetMessage())

		ami.tLogger.Error().Msg(err.Error())

		return err

	}

	if resp == nil {
		err := fmt.Errorf("error: %s", "response not found")

		ami.tLogger.Error().Msg(err.Error())

		return err

	}

	return nil
}

func (as *Asterisk) run(ctx context.Context) {
	defer as.wg.Done()

	reader := bufio.NewReader(as.conn)
	buffer := bytes.NewBuffer(nil)

	for {

		if ctx.Err() != nil {
			as.tLogger.Debug().Msg("We got error this thing")
			as.errors <- ctx.Err()

			return
		}

		if as.stop == nil {
			as.tLogger.Debug().Msg("We stopped this thing")

			return
		}

		//logs.TLogger.Debug().Msgf("We have request %d?", as.fifo.GetLen())
		if as.fifo.GetLen() > 0 {

			itm, err := as.fifo.Dequeue()
			if err != nil {
				as.errors <- err

				return
			}

			payLoad := (itm.(*amitools.Request)).Payload

			err = as.sendString(payLoad)
			if err != nil {
				as.errors <- err

				return
			}
		}

		//logs.TLogger.Debug().Msgf("Anything to read %d?", reader.Buffered())
		if reader.Buffered() > 0 {

			//as.mu.Lock()
			message, err := reader.ReadString('\n')
			//as.mu.Unlock()

			if err != nil {
				as.errors <- err

				return
			}

			//logs.TLogger.Debug().Msgf("GOT %s", message)
			if message == "\r\n" {
				mapjson := buffer.String()
				buffer.Reset()

				m, err := ParseString(mapjson)
				if err != nil {
					logs.TLogger.Error().Msgf("error %s", err)

					as.errors <- err

					return
				}

				if _, found := m["Response"]; found {
					rsp := amiresponses.NewResponseData()

					if _, found := m["ActionID"]; found {
						rsp.ID = m["ActionID"]
					} else {
						aID, err := rid.New("action.inv" + strings.ReplaceAll(as.cfg.Server, ".", "-"))
						if err != nil {
							as.tLogger.Error().Msg(err.Error())

							as.errors <- err

							return
						}
						rsp.ID = aID
					}

					rsp.PayLoad = mapjson
					rsp.Message = m["Message"]
					rsp.Timestamp = time.Now().UTC().String()

					rsp.Type = amiresponses.Error

					if m["Response"] == "Success" {
						rsp.Type = amiresponses.Success
					}

					if strings.Contains(rsp.GetID(), "action.") {
						as.tLogger.Debug().Msgf("storing rsp %s", rsp.ID)
						as.responses.Store(rsp.GetID(), rsp)
					}

				} else {

					evt := amitools.Event{}
					evt.Type = m["Event"]
					evt.TimeStamp = timestamppb.New(time.Now())

					evt.PayLoad = mapjson

					if _, found := m["Uniqueid"]; found {
						evt.ID = strings.ReplaceAll(m["Uniqueid"], ".", ";")
					} else if _, found := m["DestUniqueid"]; found {
						evt.ID = strings.ReplaceAll(m["DestUniqueid"], ".", ";")
					}

					if evt.Type == "FullyBooted" {
						as.tLogger.Debug().Msg("We are loogged")

					}

					as.events <- &evt
				}

			} else {
				buffer.WriteString(message)
			}
		}

	}

}

func (as *Asterisk) reading(ctx context.Context) {
	defer as.wg.Done()

	reader := bufio.NewReader(as.conn)
	buffer := bytes.NewBuffer(nil)

	for {

		if ctx.Err() != nil {
			as.tLogger.Debug().Msg("We got error this thing")
			as.errors <- ctx.Err()

			return
		}

		if as.stop == nil {
			as.tLogger.Debug().Msg("We stopped this thing")

			return
		}

		//as.mu.Lock()
		message, err := reader.ReadString('\n')
		//as.mu.Unlock()

		if err != nil {
			as.errors <- err

			return
		}

		//logs.TLogger.Debug().Msgf("GOT %s", message)
		if message == "\r\n" {
			mapjson := buffer.String()
			buffer.Reset()

			m, err := ParseString(mapjson)
			if err != nil {
				logs.TLogger.Error().Msgf("error %s", err)

				as.errors <- err

				return
			}

			if _, found := m["Response"]; found {
				rsp := amiresponses.NewResponseData()

				if _, found := m["ActionID"]; found {
					rsp.ID = m["ActionID"]
				} else {
					aID, err := rid.New("action.inv" + strings.ReplaceAll(as.cfg.Server, ".", "-"))
					if err != nil {
						as.tLogger.Error().Msg(err.Error())

						as.errors <- err

						return
					}
					rsp.ID = aID
				}

				rsp.PayLoad = mapjson
				rsp.Message = m["Message"]
				rsp.Timestamp = time.Now().UTC().String()

				rsp.Type = amiresponses.Error

				if m["Response"] == "Success" {
					rsp.Type = amiresponses.Success
				}

				if strings.Contains(rsp.GetID(), "action.") {
					as.tLogger.Debug().Msgf("storing rsp %s", rsp.ID)
					as.responses.Store(rsp.GetID(), rsp)
				}

			} else {

				evt := amitools.Event{}
				evt.Type = m["Event"]
				evt.TimeStamp = timestamppb.New(time.Now())

				evt.PayLoad = mapjson

				if _, found := m["Uniqueid"]; found {
					evt.ID = strings.ReplaceAll(m["Uniqueid"], ".", ";")
				} else if _, found := m["DestUniqueid"]; found {
					evt.ID = strings.ReplaceAll(m["DestUniqueid"], ".", ";")
				} else if _, found := m["BridgeUniqueid"]; found {
					evt.ID = strings.ReplaceAll(m["BridgeUniqueid"], ".", ";")
				}

				if evt.Type == "FullyBooted" {
					as.tLogger.Debug().Msg("We are loogged")

				}

				as.events <- &evt
			}

		} else {
			buffer.WriteString(message)
		}

	}

}

func (as *Asterisk) SetLogHandler(log zerolog.Logger) {
	as.tLogger = log
}

// Logoff closes the current session with AMI.
func (as *Asterisk) Close(ctx context.Context) error {
	close(as.stop)
	//as.tickers.Stop()

	return nil
}

func (as *Asterisk) Ticket() *time.Ticker {
	return as.tickers
}

// Events returns an channel with events received from AMI.
func (as *Asterisk) Events() <-chan *amitools.Event {
	return as.events
}

func (as *Asterisk) Errors() <-chan error {
	return as.errors
}
