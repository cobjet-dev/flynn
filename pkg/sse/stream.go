package sse

import (
	"encoding/json"
	"net/http"
	"reflect"

	hh "github.com/flynn/flynn/pkg/httphelper"
)

type Stream struct {
	w         *Writer
	closeChan chan struct{}
	doneChan  chan struct{}
	closed    bool
}

func ServeStream(w http.ResponseWriter, ch interface{}) *Stream {
	sw := NewWriter(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(200)
	sw.Flush()

	s := &Stream{w: sw, closeChan: make(chan struct{}), doneChan: make(chan struct{})}
	wr := hh.FlushWriter{Writer: NewWriter(w), Enabled: true}
	enc := json.NewEncoder(wr)

	go func() {
		if cw, ok := w.(http.CloseNotifier); ok {
			<-cw.CloseNotify()
			s.Close()
		}
	}()

	closeChanValue := reflect.ValueOf(s.closeChan)
	chValue := reflect.ValueOf(ch)
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
					Chan: chValue,
				},
			})
			switch chosen {
			case 0:
				return
			default:
				if ok {
					if err := enc.Encode(v.Interface()); err != nil {
						return
					}
				} else {
					return
				}
			}
		}
	}()

	s.wait()

	return s
}

func (s *Stream) wait() {
	<-s.doneChan
}

func (s *Stream) done() {
	close(s.doneChan)
	s.Close()
}

func (s *Stream) Close() {
	if !s.closed {
		s.closed = true
		close(s.closeChan)
		s.wait()
	}
}

func (s *Stream) CloseWithError(err error) {
	s.Close()
	s.w.Error(err)
}
