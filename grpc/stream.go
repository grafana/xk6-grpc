package grpc

import (
	"errors"
	"fmt"
	"io"
	"sync"

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

// message is a struct that
type message struct {
	isClosing bool
	msg       []byte
}

const (
	opened = iota + 1
	closing
	closed
)

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

	state int8
	done  chan struct{}

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
	ctx := s.vu.Context()
	wg := new(sync.WaitGroup)

	defer func() {
		wg.Wait()
		s.tq.Close()
	}()

	// read & write data from/to the stream
	wg.Add(2)
	go s.readData(wg)
	go s.writeData(wg)

	ctxDone := ctx.Done()
	for {
		select {
		case <-ctxDone:
			// VU is shutting down during an interrupt
			// stream events will not be forwarded to the VU
			s.tq.Queue(func() error {
				return s.closeWithError(nil)
			})
			return
		case <-s.done:
			return
		}
	}
}

func (s *stream) queueMessage(msg map[string]interface{}) {
	s.tq.Queue(func() error {
		rt := s.vu.Runtime()
		listeners := s.eventListeners.all(eventData)

		for _, messageListener := range listeners {
			if _, err := messageListener(rt.ToValue(msg)); err != nil {
				// TODO(olegbespalov) consider logging the error
				_ = s.closeWithError(err)

				return err
			}
		}
		return nil
	})
}

// readData reads data from the stream and forward them to the readDataChan
func (s *stream) readData(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		msg, err := s.stream.ReceiveConverted()

		if err != nil && !isRegularClosing(err) {
			s.logger.WithError(err).Debug("error while reading from the stream")

			s.tq.Queue(func() error {
				return s.closeWithError(err)
			})

			return
		}

		if len(msg) == 0 && isRegularClosing(err) {
			s.logger.WithError(err).Debug("stream is cancelled/finished")

			s.tq.Queue(func() error {
				return s.closeWithError(nil)
			})

			return
		}

		s.queueMessage(msg)
	}
}

func isRegularClosing(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, grpcext.ErrCanceled)
}

// writeData writes data to the stream
func (s *stream) writeData(wg *sync.WaitGroup) {
	defer wg.Done()

	writeChannel := make(chan message)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case msg, ok := <-writeChannel:
				if !ok {
					return
				}

				if msg.isClosing {
					err := s.stream.CloseSend()
					if err != nil {
						s.logger.WithError(err).Error("an error happened during stream closing")
					}

					s.tq.Queue(func() error {
						return s.closeWithError(err)
					})

					return
				}

				err := s.stream.Send(msg.msg)
				if err != nil {
					s.logger.WithError(err).Error("failed to send data to the stream")

					s.tq.Queue(func() error {
						return s.closeWithError(err)
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
				queue = append(queue, msg)
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
	if s.state != opened {
		return
	}

	if common.IsNullish(input) {
		s.logger.Warnf("can't send empty message")
		return
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
	if s.state == closed || s.state == closing {
		return
	}

	s.state = closing
	s.writeQueueCh <- message{isClosing: true}

	// TODO(olegbespalov): consider moving this somewhere closer to the stream closing
	// that could happen even without calling end(), e.g. when server closes the stream
	_ = s.callEventListeners(eventEnd)
}

func (s *stream) closeWithError(err error) error {
	select {
	case <-s.done:
		s.logger.WithError(err).Debug("connection is already closed")
		return nil
	default:
	}

	s.state = closed
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
		// TODO(olegbespalov): consider wrapping the error into an object
		// to provide more structure about error (like the error code and so on)

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
