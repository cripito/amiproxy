package proxy

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/cripito/amilib"
	amiclient "github.com/cripito/amilib/client"
	"github.com/cripito/amiproxy/client"
	"github.com/rs/zerolog"
)

const (
	evtParameterList string = "AppData,Application,AccountCode,CallerIDName,CallerIDNum,ConnectedLineName,ConnectedLineNum,Context,Exten,Language,Linkedid,Priority,SystemName"
)

// Options describes the options for connecting to
// a native Asterisk AMI- server.
type Options struct {
	//Chatty
	Verbose *bool

	// Usually localhost
	Host string

	//Port to open the asterisk server
	Port int

	// Username for ARI authentication
	Username string

	// Password for ARI authentication
	Password string

	// Allow subscribe to all events in Asterisk Server
	Events string
}

var (
	Verbose *bool
)

// DefaultReconnectionAttemts is the default number of reconnection attempts
// It implements a hard coded fault tolerance for a starting NATS cluster
const DefaultReconnectionAttemts = 5

// DefaultReconnectionWait is the default wating time between each reconnection
// attempt
const DefaultReconnectionWait = 5 * time.Second

// Defaultreconnectbuffersize is the default size of the buffer for cache reconnect msgs
const DefaultReconnectBufferSize = 5 * 1024 * 1024

const JetStreamTActions = "amiproxy"

type Server struct {
	cfg *amilib.ConfigData

	//node *amitools.Node

	// MBPrefix is the string which should be prepended to all MessageBus subjects, sending and receiving.
	// It defaults to "ami.".
	MBPrefix string

	ConnectedServer string

	// MBPrefix is the string which should be add to the end to all MessageBus subjects, sending and receiving.
	// It defaults to ".>".
	MBPostfix string

	TopicSeparator string

	readyCh chan struct{}

	// cancel is the context cancel function, by which all subtended subscriptions may be terminated
	cancel context.CancelFunc

	mu sync.Mutex

	ami *client.Asterisk

	//nats server name
	serverName string

	Verbose *bool

	//Logger
	tLogger zerolog.Logger

	//shutdown channel
	shutdown chan os.Signal

	connected bool

	amiClient *amiclient.AMIClient

	settings client.AsteriskSettings

	ticker time.Ticker
}
