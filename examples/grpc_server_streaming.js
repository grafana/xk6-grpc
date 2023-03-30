import { Client, Stream } from 'k6/x/grpc'
import { sleep } from 'k6';

let client = new Client();
client.load([], "./grpc_server/route_guide.proto")

const COORD_FACTOR = 1e7;

// to run this sample, you need to start the grpc server first.
// to start the grpc server, run the following command in k6 repository's root:
// go run -mod=mod examples/grpc_server/*.go
// (golang should be installed)

export default () => {
    client.connect("127.0.0.1:10000", { plaintext: true })

	const stream = new Stream(
		client,
		'main.FeatureExplorer/ListFeatures',
		null
	);

	stream.on('data', function(feature) {
		console.log('Found feature called "' + feature.name + '" at ' +
			feature.location.latitude/COORD_FACTOR + ', ' +
			feature.location.longitude/COORD_FACTOR);
	});

	stream.on('end', function() {
		// The server has finished sending
		client.close()
	});

	stream.on('error', function(e) {
		// An error has occurred and the stream has been closed.
		console.log('Error: ' + e);
	});

	// send a message to the server
	stream.write({
		lo: {
		  latitude: 400000000,
		  longitude: -750000000
		},
		hi: {
		  latitude: 420000000,
		  longitude: -730000000
		}
	});

	sleep(1);
}

