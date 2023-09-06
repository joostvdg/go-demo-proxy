# go-demo-proxy
Proxies go-demo, so we can have distributed traces

## TODO

* call go-demo from go-demo-proxy
* have query parameters
* implement the same random error and delay as go-demo
  * with both, filter on query paremeter to enable/disable
  * this way, we can "discover" that specific flows are broken
* add distributed tracing
  * include the query parameters in the span
  * include the error in the span
  * include the user agent in the span
* add metrics
* events?

## Usage

```shell
export GO_DEMO_SERVICE_ONE_URL=http://go-demo.teal.run-01.h2o-2-13047.h2o.vmware.com
export GO_DEMO_SERVICE_TWO_URL=http://go-demo.tsm-2.h2o-2-13047.h2o.vmware.com
export SERVICE_NAME=go-demo-proxy
export SERVICE_VERSION=0.0.1
export WAVEFRONT_PROXY_HOSTNAME=localhost
export WAVEFRONT_PROXY_PORT=2878
export ZIPKIN_HOST=localhost
export ZIPKIN_PORT=9411
```

Testing for Kubernetes:

```shell
export MY_NODE_NAME="test-01"
export MY_CPU_REQUEST="100m"
export MY_CPU_LIMIT="200m"
export MY_MEM_REQUEST="100Mi"
export MY_MEM_LIMIT="200Mi"
```


## Wavefront Proxy

* https://docs.wavefront.com/proxies_installing.html

```shell
export WAVEFRONT_URL=https://<INSTANCE>.wavefront.com
export WAVEFRONT_TOKEN=<TOKEN>

```

```shell
docker run -d -p 9411:9411 openzipkin/zipkin
```

```shell
docker run -d\
  -e WAVEFRONT_URL=$WAVEFRONT_URL \
  -e WAVEFRONT_TOKEN=$WAVEFRONT_TOKEN \
  -p 2878:2878 \
  -p 9400:9400 \
  --name wavefront-proxy \
  wavefronthq/proxy:latest
```