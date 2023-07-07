# xk6-grpc

This extension is re-designing of the k6's GRPC module that also should bring new features like GRPC streaming (it's available in the k6 as `k6/experimental/grpc`).

The extension code copied from the original k6's GRPC module. The module documentation is available [here](https://k6.io/docs/javascript-api/k6-experimental/grpc/).

The new stream's functionality (more examples you can find in the `examples` folder):

```javascript
import { Stream } from 'k6/x/grpc'

// Stream(client, method)
// - client - already initialized and connected client with the loaded definitions
// - method - a gRPC method URL to invoke.
const stream = new Stream(
  client,
  'foo.BarService/sayHello'
)

// A `stream.on` method adds a new handler for different kind of the events.
// 
// Currently supported: 
// `data`  - triggered when the server send data to the stream
// `error` - an error occurs
// `end`   - triggered when the server has finished sending the data
// You could register multiple handlers for the same kind event.
stream.on('data', message => {
  // server send data, processing...
})

// Write data to the stream
stream.write({ message: 'foo' })

// Signals the server that the client has finished sending the data
stream.end()
```

## Requirements

* [Golang 1.19+](https://go.dev/)
* [Git](https://git-scm.com/)
* [xk6](https://github.com/grafana/xk6) (`go install go.k6.io/xk6/cmd/xk6@latest`)


## Getting started  

1. Build the k6's binary with the module:

  ```shell
  $ make build
  ```

2. Run the GRPC demo server in a separated terminal:

  ```shell
  $ make grpc-server-run
  ```

3. Run the example:

  ```shell
  $ ./k6 run examples/grpc_client_streaming.js
  ```

## Contributing

Contributing to this repository is following general k6's [contribution guidelines](https://github.com/grafana/k6/blob/master/CONTRIBUTING.md) since the long-term goal is to merge this extension into the main k6 repository.

However, since right now there are two modules, `k6/net/grpc` and `k6/experimental/grpc`, that have similar functionality (the experimental module started as a copy), and not sharing the code, there is a specialty exists that forces us to backport (or forward port) changes to shared functionality (e.g., bug fixes) between modules.

We don't expect every contributor to do that and are happy to do that for you, but if you want to do that by yourself, you can do that.