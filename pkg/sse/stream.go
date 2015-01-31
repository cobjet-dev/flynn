package sse

import (
	"encoding/json"
	"net/http"
	"reflect"
	"time"

	hh "github.com/flynn/flynn/pkg/httphelper"
)

type identifiable interface {
	GetID() string
}

type Stream struct {
	w         *Writer
	rw        http.ResponseWriter
	ch        interface{}
	closeChan chan struct{}
	doneChan  chan struct{}
	closed    bool
	Done      chan struct{}
}

func NewStream(w http.ResponseWriter, ch interface{}) *Stream {
	sw := NewWriter(w)
	return &Stream{rw: w, w: sw, ch: ch, closeChan: make(chan struct{}), doneChan: make(chan struct{}), Done: make(chan struct{})}
}

func ServeStream(w http.ResponseWriter, ch interface{}) *Stream {
	s := NewStream(w, ch)
	s.Serve()
	s.Wait()
	return s
}

func (s *Stream) Serve() {
	s.rw.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	s.rw.WriteHeader(200)
	s.w.Flush()

	wr := hh.FlushWriter{Writer: NewWriter(s.rw), Enabled: true}
	enc := json.NewEncoder(wr)

	go func() {
		if cw, ok := s.rw.(http.CloseNotifier); ok {
			<-cw.CloseNotify()
			s.Close()
		}
	}()

	closeChanValue := reflect.ValueOf(s.closeChan)
	chValue := reflect.ValueOf(s.ch)
	go func() {
		defer s.done()
		for {
			chosen, v, ok := reflect.Select([]reflect.SelectCase{
				{
					Dir:  reflect.SelectRecv,
					Chan: closeChanValue,
				},
				{
					Dir:  reflect.SelectRecv,
					Chan: reflect.ValueOf(time.After(30 * time.Second)),
				},
				{
					Dir:  reflect.SelectRecv,
					Chan: chValue,
				},
			})
			switch chosen {
			case 0:
				return
			case 1:
				s.sendKeepAlive()
			default:
				if ok {
					if err := s.send(enc, v.Interface()); err != nil {
						// TODO: report error
						return
					}
				} else {
					return
				}
			}
		}
	}()
}

func (s *Stream) Wait() {
	<-s.doneChan
}

func (s *Stream) done() {
	close(s.doneChan)
	close(s.Done)
	s.Close()
}

func (s *Stream) send(enc *json.Encoder, v interface{}) error {
	if i, ok := v.(identifiable); ok {
		s.w.writeID(i.GetID())
	}
	return enc.Encode(v)
}

func (s *Stream) sendKeepAlive() error {
	_, err := s.w.w.Write([]byte(":\n"))
	if err != nil {
		return err
	}
	s.w.Flush()
	return nil
}

func (s *Stream) Close() {
	if !s.closed {
		s.closed = true
		close(s.closeChan)
		s.Wait()
	}
}

func (s *Stream) CloseWithError(err error) {
	s.Close()
	s.w.Error(err)
}
