package pagesnap

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
)

// cdpReadLimit bounds one DevTools message. A screenshot arrives base64 in a
// single message, so this is the ceiling on the picture, with room over the
// largest one a platform is sent.
const cdpReadLimit = 32 << 20

// cdpMessage is a DevTools protocol frame: a reply carries ID, an event
// carries Method.
type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// cdpConn is the few calls this package makes over the DevTools protocol.
// Events go to onEvent on the reading goroutine, which must not block.
type cdpConn struct {
	ws      *websocket.Conn
	onEvent func(cdpMessage)
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan cdpMessage

	done    chan struct{}
	readErr error
}

func dialCDP(ctx context.Context, url string, onEvent func(cdpMessage)) (*cdpConn, error) {
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to the browser: %w", err)
	}
	ws.SetReadLimit(cdpReadLimit)
	c := &cdpConn{ws: ws, onEvent: onEvent, pending: map[int64]chan cdpMessage{}, done: make(chan struct{})}
	go c.read()
	return c, nil
}

func (c *cdpConn) read() {
	defer close(c.done)
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.readErr = err
			return
		}
		var m cdpMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		if m.Method != "" && c.onEvent != nil {
			c.onEvent(m)
		}
	}
}

// call sends method to the browser, or to the page attached as sessionID,
// and decodes the reply into result when result is not nil.
func (c *cdpConn) call(ctx context.Context, sessionID, method string, params, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan cdpMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if params == nil {
		params = struct{}{}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	frame, err := json.Marshal(cdpMessage{ID: id, Method: method, Params: raw, SessionID: sessionID})
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, frame); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		if result != nil {
			if err := json.Unmarshal(m.Result, result); err != nil {
				return fmt.Errorf("%s: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-c.done:
		return fmt.Errorf("%s: the browser connection closed: %v", method, c.readErr)
	}
}

func (c *cdpConn) close() {
	c.ws.CloseNow()
	<-c.done
}
