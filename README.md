# xk6-grpc

This extension is re-designing of the k6's GRPC module that also should bring new features like GRPC streaming.

The extension code copied from the original k6's GRPC module, so you could use [an original documentation](https://k6.io/docs/javascript-api/k6-net-grpc/).

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
