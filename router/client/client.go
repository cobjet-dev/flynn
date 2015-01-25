// Package client provides a client for the router API.
package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/router/types"
)

// ErrNotFound is returned when no route was found.
var ErrNotFound = errors.New("router: route not found")

type client struct {
	*httpclient.Client
}

// New uses the default discoverd client and returns a client.
func New() Client {
	return newRouterClient()
}

// NewWithHTTP does the same thing as New but uses the given *http.Client
func NewWithHTTP(http *http.Client) Client {
	c := newRouterClient()
	http.Transport = c.HTTP.Transport
	c.HTTP = http
	return c
}

func newRouterClient() *client {
	return &client{Client: &httpclient.Client{
		ErrNotFound: ErrNotFound,
		URL:         "http://router-api.discoverd:5000",
		HTTP:        http.DefaultClient,
	}}
}

// NewWithAddr uses addr as the specified API url and returns a client.
func NewWithAddr(addr string) Client {
	c := newRouterClient()
	c.URL = fmt.Sprintf("http://%s", addr)
	c.HTTP = http.DefaultClient
	return c
}

// Client is a client for the router API.
type Client interface {
	// CreateRoute creates a new route.
	CreateRoute(*router.Route) error
	// SetRoute updates an existing route. If the route does not exist, it
	// creates a new one.
	SetRoute(*router.Route) error
	// DeleteRoute deletes the route with the specified id.
	DeleteRoute(id string) error
	// GetRoute returns a route with the specified id.
	GetRoute(id string) (*router.Route, error)
	// ListRoutes returns a list of routes. If parentRef is not empty, routes
	// are filtered by the reference (ex: "controller/apps/myapp").
	ListRoutes(parentRef string) ([]*router.Route, error)
	// Closer allows closing the underlying transport connection.
	io.Closer
}

// HTTPError is returned when the server returns a status code that is different
// from 200, which is normally caused by an error.
type HTTPError struct {
	Response *http.Response
}

func (e HTTPError) Error() string {
	return fmt.Sprintf("router: expected http status 200, got %d", e.Response.StatusCode)
}

func (c *client) CreateRoute(r *router.Route) error {
	return c.Post("/routes", r, r)
}

func (c *client) SetRoute(r *router.Route) error {
	return c.Put("/routes", r, r)
}

func (c *client) DeleteRoute(id string) error {
	return c.Delete("/routes/" + id)
}

func (c *client) GetRoute(id string) (*router.Route, error) {
	res := &router.Route{}
	err := c.Get("/routes/"+id, res)
	return res, err
}

func (c *client) ListRoutes(parentRef string) ([]*router.Route, error) {
	path := "/routes"
	if parentRef != "" {
		q := make(url.Values)
		q.Set("parent_ref", parentRef)
		path += "?" + q.Encode()
	}
	var res []*router.Route
	err := c.Get(path, &res)
	return res, err
}
