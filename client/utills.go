package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	amiresponses "github.com/cripito/amilib/responses"
	amitools "github.com/cripito/amilib/tools"
)

const (
	AsteriskTimeOut = 60
)

func command(action string, id string, v ...interface{}) ([]byte, error) {
	if action == "" {
		return nil, errors.New("invalid Action")
	}
	return marshal(&struct {
		Action string `ami:"Action"`
		ID     string `ami:"ActionID, omitempty"`
		V      []interface{}
	}{Action: action, ID: id, V: v})
}

func (ami *Asterisk) Handle(r *amitools.Request) (*amitools.Request, error) {

	ami.tLogger.Debug().Msgf("ENQUEUEING %s", r.Payload)
	err := ami.fifo.Enqueue(r)
	if err != nil {
		return nil, err
	}

	ret, err := ami.read(context.Background(), r.ID)
	if err != nil {
		return nil, err
	}

	ami.tLogger.Debug().Msgf("We got %+v", ret)
	r.Payload = ret.PayLoad

	return r, nil
}

func (ami *Asterisk) send(ctx context.Context, action string, id string, v interface{}, node *amitools.Node) *amiresponses.ResponseData {
	b, err := command(action, id, v)
	if err != nil {
		return nil
	}

	p := amitools.Request{
		ID:      id,
		Type:    action,
		Node:    node,
		Payload: string(b),
	}

	ami.tLogger.Debug().Msgf("ENQUEUEING %s", b)

	err = ami.fifo.Enqueue(&p)
	if err != nil {
		return nil
	}

	ret, err := ami.read(ctx, id)
	if err != nil {
		return nil
	}

	return ret
}

func ParseString(input string) (map[string]string, error) {
	resp := make(map[string]string)

	lines := strings.Split(input, "\r\n")
	for _, line := range lines {
		keys := strings.SplitAfterN(line, ":", 2)
		if len(keys) == 2 {
			key := strings.TrimSpace(strings.Trim(keys[0], ":"))
			value := strings.TrimSpace(keys[1])
			resp[key] = value
		} else if strings.Contains(line, "\r\n\r\n") || line == "" {
			return resp, nil
		}
	}

	return resp, nil
}

func (ami *Asterisk) read(ctx context.Context, id string) (*amiresponses.ResponseData, error) {
	//var buffer bytes.Buffer

	ami.tLogger.Debug().Msgf("Entering read")
	if ctx.Err() != nil {
		ami.tLogger.Debug().Msgf("leaving with error")

		return nil, fmt.Errorf("response not found")
	}

	t := time.Now()
	for {

		if ctx.Err() != nil {
			ami.tLogger.Debug().Msg("We got error this thing")

			return nil, ctx.Err()
		}

		if ami.stop == nil {
			ami.tLogger.Debug().Msg("We stopped this thing")

			return nil, fmt.Errorf("we hit stop")
		}

		where := time.Since(t).Seconds()

		if where > AsteriskTimeOut {
			ami.tLogger.Debug().Msgf("leaving with error")

			return nil, fmt.Errorf("response not found")
		}

		// Retrieve item from map
		if tmp, ok := ami.responses.Load(id); ok {
			rsp := tmp.(*amiresponses.ResponseData)

			ami.responses.Delete(id)

			//ami.tLogger.Debug().Msgf("found  %+v", rsp)

			return rsp, nil
		}
	}

}
