package webhook

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/proxify/pkg/types"
	"net/http"
	"sync"
	"time"
)

var (
	ErrInvalidStatus = errors.New("invalid response status")
)

type Options struct {
	Url string `yaml:"url"`
}

type Session struct {
	ID        string
	Buffer    *bytes.Buffer
	CreatedAt time.Time
	Finished  bool
}

type Client struct {
	*sync.RWMutex
	Options *Options
	Http    *http.Client
	Session map[string]*Session
	changes chan string
}

func New(option *Options) (*Client, error) {
	client := &Client{
		RWMutex: new(sync.RWMutex),
		Options: option,
		Http:    &http.Client{},
		Session: make(map[string]*Session),
		changes: make(chan string),
	}
	go client.sendBackground()
	return client, nil
}

func (c *Client) Save(data types.OutputData) error {
	sessionID := data.Userdata.ID
	c.RLock()
	s, ok := c.Session[sessionID]
	c.RUnlock()
	if !ok {
		s = &Session{
			ID:        sessionID,
			Buffer:    bytes.NewBuffer(data.Data),
			CreatedAt: time.Now(),
		}
		c.Lock()
		c.Session[sessionID] = s
		c.Unlock()
	} else {
		if s.Finished {
			gologger.Error().Msgf("Session %s is marked as completed but has new data", sessionID)
		}
		s.Buffer.Write(data.Data)
	}
	if data.Userdata.HasResponse {
		s.Finished = true
	}
	c.changes <- sessionID
	return nil
}

func (c *Client) Send(b *bytes.Buffer) error {
	res, err := c.Http.Post(c.Options.Url, "application/octet-stream", b)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 && res.StatusCode != 204 {
		return fmt.Errorf("%v: %d", ErrInvalidStatus, res.StatusCode)
	}
	return nil
}

func (c *Client) sendBackground() {
	for {
		sessionID := <-c.changes
		c.RLock()
		s, ok := c.Session[sessionID]
		c.RUnlock()
		if ok {
			if s.Finished {
				err := c.Send(s.Buffer)
				if err != nil {
					gologger.Error().Msgf("Error while sending to webhook: %s", err)
				}
				//delete(c.Session, sessionID)
			}
		}
	}
}
