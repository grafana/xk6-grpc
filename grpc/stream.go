package grpc

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dop251/goja"
	"github.com/grafana/xk6-grpc/lib/netext/grpcext"
	"github.com/mstoykov/k6-taskqueue-lib/taskqueue"
	"github.com/sirupsen/logrus"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type message struct {
	msg []byte
}

type stream struct {
	vu     modules.VU
	client *Client

	logger logrus.FieldLogger

	methodDescriptor protoreflect.MethodDescriptor

	method string
	stream *grpcext.Stream

	tagsAndMeta    *metrics.TagsAndMeta
	tq             *taskqueue.TaskQueue
	builtinMetrics *metrics.BuiltinMetrics
	obj            *goja.Object // the object that is given to js to interact with the stream

	closed bool
	done   chan struct{}

	writeQueueCh chan message

	eventListeners *eventListeners
}

// defineStream defines the goja.Object that is given to js to interact with the Stream
func defineStream(rt *goja.Runtime, s *stream) {
	must(rt, s.obj.DefineDataProperty(
		"on", rt.ToValue(s.on), goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE))

	must(rt, s.obj.DefineDataProperty(
		"write", rt.ToValue(s.write), goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE))

	must(rt, s.obj.DefineDataProperty(
		"end", rt.ToValue(s.end), goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE))
}

func (s *stream) beginStream() error {
	req := &grpcext.StreamRequest{
		Method:           s.method,
		MethodDescriptor: s.methodDescriptor,
	}

	stream, err := s.client.conn.NewStream(s.vu.Context(), *req, metadata.MD{})
	if err != nil {
		return fmt.Errorf("failed to create a new stream: %w", err)
	}
	s.stream = stream

	go s.loop()

	return nil
}

func (s *stream) loop() {
	readDataChan := make(chan map[string]interface{})

	ctx := s.vu.Context()

	defer func() {
		ch := make(chan struct{})
		s.tq.Queue(func() error {
			defer close(ch)
			return s.closeWithError(nil)
		})

		select {
		case <-ch:
		case <-ctx.Done():
			// unfortunately it is really possible that k6 has been winding down the VU and the above code
			// to close the connection will just never be called as the event loop no longer executes the callbacks.
			// This ultimately needs a separate signal for when the eventloop will not execute anything after this.
			// To try to prevent leaking goroutines here we will try to figure out if we need to close the `done`
			// channel and wait a bit and close it if it isn't. This might be better off with more mutexes
			timer := time.NewTimer(time.Millisecond * 250)
			select {
			case <-s.done:
				// everything is fine
			case <-timer.C:
				close(s.done) // hopefully this means we won't double close
			}
			timer.Stop()
		}

		s.tq.Close()
	}()

	// read & write data from/to the stream
	go s.readData(readDataChan)
	go s.writeData()

	ctxDone := ctx.Done()
	for {
		select {
		case msg, ok := <-readDataChan:
			if !ok {
				return
			}

			rt := s.vu.Runtime()

			s.tq.Queue(func() error {
				listeners := s.eventListeners.all(eventData)

				for _, messageListener := range listeners {
					if _, err := messageListener(rt.ToValue(msg)); err != nil {
						// TODO log it?
						_ = s.closeWithError(err)

						return err
					}
				}
				return nil
			})
		case <-ctxDone:
			// VU is shutting down during an interrupt
			// stream events will not be forwarded to the VU
			s.tq.Queue(func() error {
				return s.closeWithError(nil)
			})
			ctxDone = nil // this is to block this branch and get through s.done
		}
	}
}

// readData reads data from the stream and forward them to the readDataChan
func (s *stream) readData(readDataChan chan map[string]interface{}) {
	defer func() {
		close(readDataChan)
	}()

	for {
		msg, err := s.stream.ReceiveConverted()
		if errors.Is(err, grpcext.ErrCancelled) {
			s.logger.Debug("stream is cancelled")

			err = nil
		}

		if err != nil && !errors.Is(err, io.EOF) {
			s.logger.WithError(err).Debug("error while reading from the stream")

			s.tq.Queue(func() error {
				return s.closeWithError(err)
			})

			return
		}

		// no message means that the stream has been closed
		if len(msg) == 0 && errors.Is(err, io.EOF) {
			_ = s.closeWithError(nil)
			return
		}

		select {
		case readDataChan <- msg:
		case <-s.done:
			if len(msg) > 0 {
				s.logger.Debug("stream is done, sending the last message")

				readDataChan <- msg
			}

			return
		}
	}
}

// writeData writes data to the stream
func (s *stream) writeData() {
	writeChannel := make(chan message)
	go func() {
		for {
			select {
			case msg, ok := <-writeChannel:
				if !ok {
					return
				}

				err := s.stream.Send(msg.msg)
				if err != nil {
					s.logger.WithError(err).Error("failed to send data to the stream")

					s.tq.Queue(func() error {
						closeErr := s.closeWithError(err)
						return closeErr
					})
					return
				}

			case <-s.done:
				return
			}
		}
	}()

	{
		defer func() {
			close(writeChannel)
		}()

		queue := make([]message, 0)
		var wch chan message
		var msg message
		for {
			wch = nil // this way if nothing to read it will just block
			if len(queue) > 0 {
				msg = queue[0]
				wch = writeChannel
			}
			select {
			case msg = <-s.writeQueueCh:
				select {
				case writeChannel <- msg:
				default:
					queue = append(queue, msg)
				}
			case wch <- msg:
				queue = queue[:copy(queue, queue[1:])]

			case <-s.done:
				return
			}
		}
	}
}

// on registers a listener for a certain event type
func (s *stream) on(event string, listener func(goja.Value) (goja.Value, error)) {
	if err := s.eventListeners.add(event, listener); err != nil {
		s.vu.State().Logger.Warnf("can't register %s event handler: %s", event, err)
	}
}

// write writes a message to the stream
func (s *stream) write(input goja.Value) {
	if input == nil || goja.IsNull(input) || goja.IsUndefined(input) {
		s.logger.Warnf("can't send empty message")
	}

	rt := s.vu.Runtime()

	b, err := input.ToObject(rt).MarshalJSON()
	if err != nil {
		s.logger.WithError(err).Warnf("can't marshal message")
	}

	s.writeQueueCh <- message{msg: b}
}

// end closes client the stream
func (s *stream) end() {
	var err error

	defer func() {
		_ = s.closeWithError(err)
	}()

	if err = s.stream.CloseSend(); err != nil {
		s.logger.WithError(err).Error("failed to close the stream")

		return
	}

	// TODO: move this ?
	_ = s.callEventListeners(eventEnd)
}

func (s *stream) closeWithError(err error) error {
	if s.closed {
		s.logger.WithError(err).Debug("connection is already closed")

		return nil
	}

	s.closed = true
	close(s.done)

	s.logger.WithError(err).Debug("connection closed")

	if err != nil {
		if errList := s.callErrorListeners(err); errList != nil {
			return errList
		}
	}

	return nil
}

func (s *stream) callErrorListeners(e error) error {
	rt := s.vu.Runtime()

	for _, errorListener := range s.eventListeners.all(eventError) {
		// TODO: :thinking: should error be wrapped in an object ?
		if _, err := errorListener(rt.ToValue(e)); err != nil {
			return err
		}
	}
	return nil
}

func (s *stream) callEventListeners(eventType string) error {
	rt := s.vu.Runtime()

	for _, listener := range s.eventListeners.all(eventType) {
		if _, err := listener(rt.ToValue(struct{}{})); err != nil {
			return err
		}
	}
	return nil
}

// must is a small helper that will panic if err is not nil.
func must(rt *goja.Runtime, err error) {
	if err != nil {
		common.Throw(rt, err)
	}
}
