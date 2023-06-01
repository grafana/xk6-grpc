package grpc

import (
	"io"
	"net/url"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/modulestest"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/lib/fsext"
	"go.k6.io/k6/lib/testutils/httpmultibin"
	"go.k6.io/k6/metrics"
	"gopkg.in/guregu/null.v3"
)

const isWindows = runtime.GOOS == "windows"

// codeBlock represents an execution of a k6 script.
type codeBlock struct {
	code       string
	val        interface{}
	err        string
	windowsErr string
	asserts    func(*testing.T, *httpmultibin.HTTPMultiBin, chan metrics.SampleContainer, error)
}

type testcase struct {
	name       string
	setup      func(*httpmultibin.HTTPMultiBin)
	initString codeBlock // runs in the init context
	vuString   codeBlock // runs in the vu context
}

type testState struct {
	*modulestest.Runtime
	httpBin      *httpmultibin.HTTPMultiBin
	samples      chan metrics.SampleContainer
	logger       logrus.FieldLogger
	callRecorder *callRecorder
}

// callRecorder a helper type that records all calls
type callRecorder struct {
	sync.Mutex
	calls []string
}

// Call records a call
func (r *callRecorder) Call(text string) {
	r.Lock()
	defer r.Unlock()

	r.calls = append(r.calls, text)
}

// Len just returns the length of the calls
func (r *callRecorder) Len() int {
	r.Lock()
	defer r.Unlock()

	return len(r.calls)
}

// Recorded returns the recorded calls
func (r *callRecorder) Recorded() []string {
	r.Lock()
	defer r.Unlock()

	result := []string{}
	result = append(result, r.calls...)

	return result
}

// newTestState creates a new test state.
func newTestState(t *testing.T) testState {
	t.Helper()

	tb := httpmultibin.NewHTTPMultiBin(t)

	samples := make(chan metrics.SampleContainer, 1000)
	testRuntime := modulestest.NewRuntime(t)

	cwd, err := os.Getwd() //nolint:forbidigo
	require.NoError(t, err)
	fs := afero.NewOsFs()

	if isWindows {
		fs = fsext.NewTrimFilePathSeparatorFs(fs)
	}
	testRuntime.VU.InitEnvField.CWD = &url.URL{Path: cwd}
	testRuntime.VU.InitEnvField.FileSystems = map[string]afero.Fs{"file": fs}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.Out = io.Discard

	recorder := &callRecorder{
		calls: make([]string, 0),
	}

	ts := testState{
		Runtime:      testRuntime,
		httpBin:      tb,
		samples:      samples,
		logger:       logger,
		callRecorder: recorder,
	}

	m, ok := New().NewModuleInstance(ts.VU).(*ModuleInstance)
	require.True(t, ok)
	require.NoError(t, ts.VU.Runtime().Set("grpc", m.Exports().Named))
	require.NoError(t, ts.VU.Runtime().Set("call", recorder.Call))

	return ts
}

// ToInitContext moves the test state to the VU context.
func (ts *testState) ToVUContext() {
	registry := metrics.NewRegistry()
	root, err := lib.NewGroup("", nil)
	if err != nil {
		panic(err)
	}

	state := &lib.State{
		Group:     root,
		Dialer:    ts.httpBin.Dialer,
		TLSConfig: ts.httpBin.TLSClientConfig,
		Samples:   ts.samples,
		Options: lib.Options{
			SystemTags: metrics.NewSystemTagSet(
				metrics.TagName,
				metrics.TagURL,
			),
			UserAgent: null.StringFrom("k6-test"),
		},
		BuiltinMetrics: metrics.RegisterBuiltinMetrics(registry),
		Tags:           lib.NewVUStateTags(registry.RootTagSet()),
		Logger:         ts.logger,
	}

	ts.MoveToVUContext(state)
}
