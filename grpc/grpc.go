// Package grpc is the root module of the k6-grpc extension.
package grpc

import (
	"errors"

	"github.com/dop251/goja"
	"github.com/mstoykov/k6-taskqueue-lib/taskqueue"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/js/modules"
	"google.golang.org/grpc/codes"
)

type (
	// RootModule is the global module instance that will create module
	// instances for each VU.
	RootModule struct{}

	// ModuleInstance represents an instance of the GRPC module for every VU.
	ModuleInstance struct {
		vu      modules.VU
		exports map[string]interface{}
	}
)

var (
	_ modules.Module   = &RootModule{}
	_ modules.Instance = &ModuleInstance{}
)

// New returns a pointer to a new RootModule instance.
func New() *RootModule {
	return &RootModule{}
}

// NewModuleInstance implements the modules.Module interface to return
// a new instance for each VU.
func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	mi := &ModuleInstance{
		vu:      vu,
		exports: make(map[string]interface{}),
	}

	mi.exports["Client"] = mi.NewClient
	mi.defineConstants()
	mi.exports["Stream"] = mi.stream

	return mi
}

// NewClient is the JS constructor for the grpc Client.
func (mi *ModuleInstance) NewClient(_ goja.ConstructorCall) *goja.Object {
	rt := mi.vu.Runtime()
	return rt.ToValue(&Client{vu: mi.vu}).ToObject(rt)
}

// defineConstants defines the constant variables of the module.
func (mi *ModuleInstance) defineConstants() {
	rt := mi.vu.Runtime()
	mustAddCode := func(name string, code codes.Code) {
		mi.exports[name] = rt.ToValue(code)
	}

	mustAddCode("StatusOK", codes.OK)
	mustAddCode("StatusCanceled", codes.Canceled)
	mustAddCode("StatusUnknown", codes.Unknown)
	mustAddCode("StatusInvalidArgument", codes.InvalidArgument)
	mustAddCode("StatusDeadlineExceeded", codes.DeadlineExceeded)
	mustAddCode("StatusNotFound", codes.NotFound)
	mustAddCode("StatusAlreadyExists", codes.AlreadyExists)
	mustAddCode("StatusPermissionDenied", codes.PermissionDenied)
	mustAddCode("StatusResourceExhausted", codes.ResourceExhausted)
	mustAddCode("StatusFailedPrecondition", codes.FailedPrecondition)
	mustAddCode("StatusAborted", codes.Aborted)
	mustAddCode("StatusOutOfRange", codes.OutOfRange)
	mustAddCode("StatusUnimplemented", codes.Unimplemented)
	mustAddCode("StatusInternal", codes.Internal)
	mustAddCode("StatusUnavailable", codes.Unavailable)
	mustAddCode("StatusDataLoss", codes.DataLoss)
	mustAddCode("StatusUnauthenticated", codes.Unauthenticated)
}

// Exports returns the exports of the grpc module.
func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{
		Named: mi.exports,
	}
}

// stream returns a new stream object
func (mi *ModuleInstance) stream(c goja.ConstructorCall) *goja.Object {
	rt := mi.vu.Runtime()

	client, ok := c.Argument(0).ToObject(rt).Export().(*Client)
	if !ok {
		common.Throw(rt, errors.New("the first argument must be a GRPC client"))
	}

	methodName := sanitizeMethodName(c.Argument(1).String())
	methodDescriptor, err := client.getMethodDescriptor(methodName)
	if err != nil {
		common.Throw(rt, err)
	}

	// the third argument is the params (optional)
	// TODO: abstract logic for parsing params
	// should be something similar to the Invoke function
	// in js/modules/k6/grpc/client.go

	tagsAndMeta := mi.vu.State().Tags.GetCurrentValues()

	s := &stream{
		vu:               mi.vu,
		client:           client,
		methodDescriptor: methodDescriptor,
		method:           methodName,
		logger:           mi.vu.State().Logger,

		tq: taskqueue.New(mi.vu.RegisterCallback),

		builtinMetrics: mi.vu.State().BuiltinMetrics,
		done:           make(chan struct{}),
		state:          opened,

		writeQueueCh: make(chan message),

		eventListeners: newEventListeners(),
		obj:            rt.NewObject(),
		tagsAndMeta:    &tagsAndMeta,
	}

	defineStream(rt, s)

	err = s.beginStream()
	if err != nil {
		s.tq.Close()

		common.Throw(rt, err)
	}

	return s.obj
}
