package proxy

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/stripe/stripe-cli/pkg/ansi"
)

//
// Public types
//

// EndpointConfig contains the optional configuration parameters of an EndpointClient.
type EndpointConfig struct {
	HTTPClient *http.Client

	Log *log.Logger

	ResponseHandler EndpointResponseHandler
}

// EndpointResponseHandler handles a response from the endpoint.
type EndpointResponseHandler interface {
	ProcessResponse(string, string, *http.Response)
}

// EndpointResponseHandlerFunc is an adapter to allow the use of ordinary
// functions as response handlers. If f is a function with the
// appropriate signature, ResponseHandler(f) is a
// ResponseHandler that calls f.
type EndpointResponseHandlerFunc func(string, string, *http.Response)

// ProcessResponse calls f(webhookID, resp).
func (f EndpointResponseHandlerFunc) ProcessResponse(webhookID, forwardURL string, resp *http.Response) {
	f(webhookID, forwardURL, resp)
}

// EndpointClient is the client used to POST webhook requests to the local endpoint.
type EndpointClient struct {
	// URL the client sends POST requests to
	URL string

	headers map[string]string

	connect bool

	events map[string]bool

	// Optional configuration parameters
	cfg *EndpointConfig
}

// SupportsEventType takes an event of a webhook and compares it to the internal
// list of supported events
func (c *EndpointClient) SupportsEventType(connect bool, eventType string) bool {
	if connect != c.connect {
		return false
	}

	// Endpoint supports all events, always return true
	if c.events["*"] || c.events[eventType] {
		return true
	}

	return false
}

// Post sends a message to the local endpoint.
func (c *EndpointClient) Post(webhookID string, body string, headers map[string]string) error {
	c.cfg.Log.WithFields(log.Fields{
		"prefix": "proxy.EndpointClient.Post",
	}).Debug("Forwarding event to local endpoint")

	req, err := http.NewRequest(http.MethodPost, c.URL, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Add(k, v)
	}

	// add custom headers
	for k, v := range c.headers {
		if strings.ToLower(k) == "host" {
			req.Host = v
		} else {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		color := ansi.Color(os.Stdout)
		localTime := time.Now().Format(timeLayout)

		errStr := fmt.Sprintf("%s            [%s] Failed to POST: %v\n",
			color.Faint(localTime),
			color.Red("ERROR"),
			err,
		)
		fmt.Println(errStr)

		return err
	}
	defer resp.Body.Close()

	c.cfg.ResponseHandler.ProcessResponse(webhookID, c.URL, resp)

	return nil
}

//
// Public functions
//

// NewEndpointClient returns a new EndpointClient.
func NewEndpointClient(url string, headers []string, connect bool, events []string, cfg *EndpointConfig) *EndpointClient {
	if cfg == nil {
		cfg = &EndpointConfig{}
	}
	if cfg.Log == nil {
		cfg.Log = &log.Logger{Out: ioutil.Discard}
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{
			Timeout: defaultTimeout,
		}
	}
	if cfg.ResponseHandler == nil {
		cfg.ResponseHandler = EndpointResponseHandlerFunc(func(string, string, *http.Response) {})
	}

	return &EndpointClient{
		URL:     url,
		headers: convertToMapAndSanitize(headers),
		connect: connect,
		events:  convertToMap(events),
		cfg:     cfg,
	}
}

//
// Private constants
//

const (
	defaultTimeout = 30 * time.Second
)

//
// Private functions
//

func convertToMap(events []string) map[string]bool {
	eventsMap := make(map[string]bool)
	for _, event := range events {
		eventsMap[event] = true
	}

	return eventsMap
}

func convertToMapAndSanitize(headers []string) map[string]string {
	reg := regexp.MustCompile("[\x00-\x1f]+")

	headerMap := make(map[string]string)

	for _, header := range headers {
		header = reg.ReplaceAllString(header, "")

		splitHeader := strings.SplitN(header, ":", 2)
		headerKey := strings.TrimSpace(splitHeader[0])
		headerVal := strings.TrimSpace(splitHeader[1])
		if headerKey != "" {
			headerMap[headerKey] = headerVal
		}
	}

	return headerMap
}
