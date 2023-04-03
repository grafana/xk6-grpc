import grpc from 'k6/x/grpc';
import { check } from 'k6';

let client = new grpc.Client();

// to run this sample, you need to start the grpc server first.
// to start the grpc server, run the following command in k6 repository's root:
// go run -mod=mod examples/grpc_server/*.go
// (golang should be installed)

export default () => {
  client.connect('127.0.0.1:10000', { plaintext: true, reflect: true });
  const response = client.invoke('main.FeatureExplorer/GetFeature', {
    latitude: 410248224,
    longitude: -747127767,
  });

  check(response, { 'status is OK': (r) => r && r.status === grpc.StatusOK });
  console.log(JSON.stringify(response.message));

  client.close();
};
