package grpc

import (
	"encoding/json"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

type message struct {
	msg *dynamicpb.Message
}

type stream struct {
	vu     modules.VU
	client *Client

	logger logrus.FieldLogger

	methodDescriptor protoreflect.MethodDescriptor
	marshaler        protojson.MarshalOptions

	method string
	stream grpc.ClientStream

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
		raw, err := s.receive()

		if err != nil && !errors.Is(err, io.EOF) {
			if errors.Is(err, errCancelled) {
				err = nil
			}

			s.tq.Queue(func() error {
				return s.closeWithError(err)
			})

			return
		}

		msg, errConv := s.convert(raw)
		if errConv != nil {
			s.tq.Queue(func() error {
				return s.closeWithError(errConv)
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

var errCancelled = errors.New("cancelled by client (k6)")

// receive receives the message from the stream
// if the stream has been closed successfully, it returns io.EOF
// if the stream has been cancelled, it returns errCancelled
func (s *stream) receive() (*dynamicpb.Message, error) {
	msg := dynamicpb.NewMessage(s.methodDescriptor.Output())
	err := s.stream.RecvMsg(msg)

	// io.EOF means that the stream has been closed successfully
	if err == nil || errors.Is(err, io.EOF) {
		return msg, err
	}

	sterr := status.Convert(err)
	if sterr.Code() == codes.Canceled {
		return nil, errCancelled
	}

	return nil, err
}

// convert converts the message to the map[string]interface{} format
// which could be returned to the JS
// there is a lot of marshaling/unmarshaling here, but if we just pass the dynamic message
// the default Marshaller would be used, which would strip any zero/default values from the JSON.
// eg. given this message:
//
//	message Point {
//	   double x = 1;
//		  double y = 2;
//		  double z = 3;
//	}
//
// and a value like this:
// msg := Point{X: 6, Y: 4, Z: 0}
// would result in JSON output:
// {"x":6,"y":4}
// rather than the desired:
// {"x":6,"y":4,"z":0}
func (s *stream) convert(msg *dynamicpb.Message) (map[string]interface{}, error) {
	raw, err := s.marshaler.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the message: %w", err)
	}

	back := make(map[string]interface{})

	err = json.Unmarshal(raw, &back)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal the message: %w", err)
	}

	return back, err
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

				err := s.stream.SendMsg(msg.msg)
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
	msg, err := buildMessage(s.vu.Runtime(), s.methodDescriptor, input)
	if err != nil {
		s.vu.State().Logger.Warnf("can't build message for sending: %s", err)

		return
	}

	s.writeQueueCh <- message{msg: msg}
}

var errEmptyMessage = errors.New("message is empty")

// buildMessage builds a message from the input
func buildMessage(rt *goja.Runtime, md protoreflect.MethodDescriptor, input goja.Value) (*dynamicpb.Message, error) {
	if input == nil || goja.IsNull(input) || goja.IsUndefined(input) {
		return nil, errEmptyMessage
	}

	b, err := input.ToObject(rt).MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("can't marshal message: %w", err)
	}

	msg := dynamicpb.NewMessage(md.Input())
	if err := protojson.Unmarshal(b, msg); err != nil {
		return nil, fmt.Errorf("can't serialise request object to protocol buffer: %w", err)
	}

	return msg, nil
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
