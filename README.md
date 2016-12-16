varnish-purge-proxy
===================

Proxy purge requests to multiple varnish servers

You must specify the following options:

`./varnish-purge-proxy --region=europe-west1 --project=my-google-project --credentials=/path/to/credentials.json`

You can also specify host and port to listen on:

`./varnish-purge-proxy --listen=127.0.0.1 --port=8000`

You can also specify the destination port to target:

`./varnish-purge-proxy --destport=6081`

varnish-purge-proxy will cache the IP lookup for 60 seconds, you can change this as follows:

`./varnish-purge-proxy --cache=120`

Authentication
--------------

A service account needs to be created in Google that has at least "Compute Network Viewer".

Building
--------

Build a binary by running:

`go build varnish-purge-proxy.go`
