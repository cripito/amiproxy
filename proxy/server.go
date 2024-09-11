package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/cripito/amilib"
	amiclient "github.com/cripito/amilib/client"
	rid "github.com/cripito/amilib/rid"
	amitools "github.com/cripito/amilib/tools"
	"github.com/cripito/amiproxy/client"
	"github.com/rs/zerolog"
)

func New(c amilib.Config) *Server {

	cfgdata, err := c.Get()
	if err != nil {
		return nil
	}

	s := &Server{
		cfg:            cfgdata,
		MBPrefix:       "ami.",
		readyCh:        make(chan struct{}),
		MBPostfix:      ".*",
		TopicSeparator: ".",

		settings: client.AsteriskSettings{},
	}

	return s
}

// Ready returns a channel which is closed when the Server is ready
func (s *Server) Ready() <-chan struct{} {
	if s.readyCh == nil {
		s.readyCh = make(chan struct{})
	}
	return s.readyCh
}

func (s *Server) SetLogHandler(log zerolog.Logger) {
	s.tLogger = log
}

func (s *Server) Listen(ctx context.Context) error {

	var err error

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.shutdown = make(chan os.Signal)
	signal.Notify(s.shutdown, os.Interrupt, syscall.SIGTERM)

	s.tLogger.Trace().Msgf("creating new amilib client %s", s.serverName)
	s.amiClient = amiclient.NewAmiClient(context.TODO(), amiclient.WithType(amitools.NodeType_PROXY), amiclient.WithNatsUri(s.cfg.NatsUrl))
	if s.amiClient == nil {
		err = fmt.Errorf("unable to create amiclient")
		s.tLogger.Error().Msg(err.Error())

		return err
	}
	s.amiClient.Node.IP = s.cfg.Server

	s.tLogger.Debug().Msgf("Launching  library server listener")
	s.amiClient.Listen(context.Background())

	s.tLogger.Trace().Msgf("creating new asterisk client %s", s.cfg.Server)
	s.ami = client.New(s.cfg)
	if s.ami == nil {
		err := fmt.Errorf("unable to create asterisk client")
		s.tLogger.Error().Msg(err.Error())

		return err
	}

	s.ami.SetLogHandler(s.tLogger)

	ctxC, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	err = s.ami.Connect(ctxC)
	if err != nil {
		s.tLogger.Error().Msg(err.Error())

		return err
	}

	s.amiClient.SetAmiRequestHandler(s.ami.Handle)

	err = s.ami.Listen(ctx)
	if err != nil {
		s.tLogger.Error().Msg(err.Error())

		return err
	}

	aID, err := rid.New("action.login")
	if err != nil {
		s.tLogger.Error().Msg(err.Error())

		return err
	}

	err = s.ami.Login(ctx, aID, s.amiClient.GetNode())
	if err != nil {
		s.tLogger.Error().Msg(err.Error())

		return err
	}

	if s.readyCh != nil {
		close(s.readyCh)
	}

	for {
		select {
		case <-s.shutdown:
			s.tLogger.Debug().Msg("GOT SHUTDOWN")

			s.ami.Close(ctx)

			return nil

		case <-s.ticker.C:
			aID, err := rid.New("action.ping")
			if err != nil {
				s.tLogger.Error().Msg(err.Error())

				return err
			}

			resp, err := s.ami.Ping(ctx, aID, s.amiClient.GetNode())
			if err != nil {
				s.tLogger.Error().Msg(err.Error())

				return err
			}

			data, err := json.Marshal(s.amiClient.GetNode())
			if err != nil {
				s.tLogger.Error().Msg(err.Error())

				return err
			}

			natsConn := s.amiClient.GetNats()
			if natsConn != nil {
				s.tLogger.Debug().Msgf("publishing in %s ", s.MBPrefix+"announce."+resp.Node.ID)

				err = natsConn.Publish(s.MBPrefix+"announce."+resp.Node.ID, data)
				if err != nil {
					s.tLogger.Error().Msg(err.Error())

					return err
				}

			}

		case evt := <-s.ami.Events():

			if evt.GetType() == amilib.FullyBooted.String() {
				aID, err := rid.New("action.coresettings")
				if err != nil {
					s.tLogger.Error().Msg(err.Error())

					return err
				}
				resp, err := s.ami.CoreSettings(ctx, aID, s.amiClient.GetNode())
				if err != nil {
					s.tLogger.Error().Msg(err.Error())

					return err
				}

				if resp != nil {

					s.ticker = *time.NewTicker(10 * time.Second)

					s.tLogger.Debug().Msgf("RESP: %+v", resp)
					resp.Node = s.amiClient.Node

					m, err := client.ParseString(resp.PayLoad)
					if err != nil {
						s.tLogger.Error().Msg(err.Error())

						return err
					}

					s.settings.AsteriskVersion = m["AsteriskVersion"]
					s.settings.SystemName = m["SystemName"]
					s.settings.Version = m["AMIversion"]
					s.settings.MaxCalls, err = strconv.Atoi(m["CoreMaxCalls"])
					if err != nil {
						s.tLogger.Error().Msg(err.Error())

						return err
					}

					data, err := json.Marshal(s.amiClient.GetNode())
					if err != nil {
						s.tLogger.Error().Msg(err.Error())

						return err
					}

					natsConn := s.amiClient.GetNats()
					if natsConn != nil {
						s.tLogger.Debug().Msgf("publishing in %s ", s.MBPrefix+"announce."+resp.Node.ID)

						err = natsConn.Publish(s.MBPrefix+"announce."+resp.Node.ID, data)
						if err != nil {
							s.tLogger.Error().Msg(err.Error())

							return err
						}

					}
				}

				s.connected = true
			}

			evt.Node = s.amiClient.GetNode()
			data, err := proto.Marshal(evt)
			if err != nil {
				s.tLogger.Error().Msg(err.Error())

				return err
			}

			natsConn := s.amiClient.GetNats()
			if natsConn != nil {
				s.tLogger.Debug().Msgf("publishing in %s", s.MBPrefix+"events"+s.TopicSeparator+evt.Type+s.TopicSeparator+evt.GetID())

				err = natsConn.Publish(s.MBPrefix+"events"+s.TopicSeparator+evt.GetType(), data)
				if err != nil {
					s.tLogger.Error().Msg(err.Error())

					return err
				}

			}

		case err := <-s.ami.Errors():
			s.tLogger.Error().Msg(err.Error())

			s.amiClient.Node.Status = amitools.StatusType_DOWN

			data, err := json.Marshal(s.amiClient.GetNode())
			if err != nil {
				s.tLogger.Error().Msg(err.Error())

				return err
			}

			natsConn := s.amiClient.GetNats()
			if natsConn != nil {
				s.tLogger.Debug().Msgf("publishing in %s ", s.MBPrefix+"announce."+s.amiClient.Node.ID)

				err = natsConn.Publish(s.MBPrefix+"announce."+s.amiClient.Node.ID, data)
				if err != nil {
					s.tLogger.Error().Msg(err.Error())

					return err
				}

			}

			return err
		}
	}

}

func (s *Server) Close() {

	s.tLogger.Debug().Msgf("Draining nats connection and stopping")

	s.amiClient.Close()

}
