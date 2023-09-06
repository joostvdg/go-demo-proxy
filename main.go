package main

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/middleware/http"
	"github.com/openzipkin/zipkin-go/model"
	reporterhttp "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/wavefronthq/wavefront-sdk-go/application"
	"github.com/wavefronthq/wavefront-sdk-go/senders"
	"golang.org/x/time/rate"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
)

func recordOpsMetrics() {
	go func() {
		for {
			opsProcessed.Inc()
			time.Sleep(2 * time.Second)
		}
	}()
}

var (
	opsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "myapp_processed_ops_total",
		Help: "The total number of processed events",
	})

	histogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: "http_server",
		Name:      "resp_time",
		Help:      "Request response time",
	}, []string{
		"service",
		"code",
		"method",
		"path",
	})

	httpListenAndServe = http.ListenAndServe

	wavefrontProxyEnabled = false
	sleep                 = time.Sleep
	serviceName           = "go-demo-proxy"
	zipkinHost            = "localhost"
	zipkinPort            = "9411"
	limiter               = rate.NewLimiter(5, 10)
	limitReachedTime      = time.Now().Add(time.Second * (-60))
	limitReached          = false
	zipkinClient          *zipkinhttp.Client

	goDemoServiceOneURL = "go-demo-service-one"
	goDemoServiceTwoURL = "go-demo-service-two"
)

var GitCommit string
var SemVer string

var tracer *zipkin.Tracer

var client *zipkinhttp.Client

func main() {
	recordOpsMetrics()
	logrus.SetFormatter(&logrus.JSONFormatter{})

	// get env variable for port
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if len(os.Getenv("SERVICE_NAME")) > 0 {
		serviceName = os.Getenv("SERVICE_NAME")
	}

	if len(os.Getenv("WAVEFRONT_PROXY_ENABLED")) > 0 && os.Getenv("WAVEFRONT_PROXY_ENABLED") == "true" {
		wavefrontProxyEnabled = true
		setupWavefrontProxy()
	}

	tracer, err := newTracer()
	if err != nil {
		log.Fatal(err)
		logrus.Error(err)
		logrus.Exit(1)
	}

	// create global zipkin traced http client
	client, err = zipkinhttp.NewClient(tracer, zipkinhttp.ClientTrace(true))
	if err != nil {
		log.Fatalf("unable to create client: %+v\n", err)
	}

	if len(os.Getenv("GO_DEMO_SERVICE_ONE_URL")) > 0 {
		goDemoServiceOneURL = os.Getenv("GO_DEMO_SERVICE_ONE_URL")
	}
	if len(os.Getenv("GO_DEMO_SERVICE_TWO_URL")) > 0 {
		goDemoServiceTwoURL = os.Getenv("GO_DEMO_SERVICE_TWO_URL")
	}

	logrus.Infof("Starting the application on port %s", port)
	mux := mux.NewRouter()
	mux.Use(zipkinhttp.NewServerMiddleware(
		tracer,
		zipkinhttp.SpanName("request")), // name for request span
	)
	mux.HandleFunc("/", VersionServer)
	mux.HandleFunc("/hello", HelloServer)
	mux.HandleFunc("/random-error", RandomErrorServer)
	mux.HandleFunc("/random-delay", RandomDelayServer)
	mux.HandleFunc("/version", VersionServer)
	mux.HandleFunc("/limiter", LimiterServer)
	mux.Handle("/metrics", promhttp.Handler())

	log.Fatal("ListenAndServe: ", httpListenAndServe(":8080", mux))
}

var wavefrontProxyHostname = "localhost"
var wavefrontProxyPort = "2878"

var wavefrontProxyTracePort = "9411"

func setupWavefrontProxy() {
	// The reporter sends traces to zipkin server
	if len(os.Getenv("WAVEFRONT_PROXY_HOSTNAME")) > 0 {
		wavefrontProxyHostname = os.Getenv("WAVEFRONT_PROXY_HOSTNAME")
	}
	if len(os.Getenv("WAVEFRONT_PROXY_PORT")) > 0 {
		wavefrontProxyPort = os.Getenv("WAVEFRONT_PROXY_PORT")
	}

	wavefrontProxyAddress := fmt.Sprintf("%s:%s", wavefrontProxyHostname, wavefrontProxyPort)
	wavefrontSender, err := senders.NewSender(
		wavefrontProxyAddress,
	)
	if err != nil {
		panic(err)
	}
	source := "go_sdk_example"

	app := application.New("sample app", "main.go")
	application.StartHeartbeatService(wavefrontSender, app, source)

	tags := make(map[string]string)
	tags["namespace"] = "default"
	tags["Kind"] = "Deployment"

	wavefrontSender.SendMetric("go-demo-proxy.startup", float64(1), time.Now().UnixNano(), source, map[string]string{"env": "test"})
	if err != nil {
		println("error:", err.Error())
	}

}

func init() {
	prometheus.MustRegister(histogram)
}

func newTracer() (*zipkin.Tracer, error) {
	// inspired by: https://medium.com/devthoughts/instrumenting-a-go-application-with-zipkin-b79cc858ac3e

	// The reporter sends traces to zipkin server
	if len(os.Getenv("ZIPKIN_HOST")) > 0 {
		zipkinHost = os.Getenv("ZIPKIN_HOST")
	}
	if len(os.Getenv("ZIPKIN_PORT")) > 0 {
		zipkinPort = os.Getenv("ZIPKIN_PORT")
	}

	endpointURL := fmt.Sprintf("http://%s:%s/api/v2/spans", zipkinHost, zipkinPort)
	reporter := reporterhttp.NewReporter(endpointURL)

	// Local endpoint represent the local service information
	localEndpoint := &model.Endpoint{ServiceName: serviceName, Port: 8080}

	// Sampler tells you which traces are going to be sampled or not. In this case we will record 100% (1.00) of traces.
	sampler, err := zipkin.NewCountingSampler(1)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	t, err := zipkin.NewTracer(
		reporter,
		zipkin.WithSampler(sampler),
		zipkin.WithLocalEndpoint(localEndpoint),
	)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	return t, err
}

func HelloServer(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { recordMetrics(start, req, http.StatusOK) }()
	span := zipkin.SpanFromContext(req.Context())
	logrus.WithFields(logrus.Fields{
		"method":  req.Method,
		"path":    req.RequestURI,
		"traceID": span.Context().TraceID,
	}).Info("Request received")

	// print all headers
	for name, values := range req.Header {
		for _, value := range values {
			fmt.Println(name, value)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hello, World!"))
}

func redirectRequest(w http.ResponseWriter, req *http.Request, url string, path string) {
	// create a new context with the span
	ctx := zipkin.NewContext(req.Context(), zipkin.SpanFromContext(req.Context()))
	span := zipkin.SpanFromContext(req.Context())
	span.Annotate(time.Now(), "redirect to "+url+path+" started")
	annotateSpanWithHeaders(span, req.Header)

	// create a new request from the existing request
	urlWithPath := fmt.Sprintf("%s/%s", url, path)
	newReq, err := http.NewRequestWithContext(ctx, "GET", urlWithPath, nil)
	if err != nil {
		logrus.Error(err)
	}
	code := http.StatusOK
	start := time.Now()
	defer func() { recordMetrics(start, req, code) }()

	span.Tag("redirect::"+url, urlWithPath)
	span.Tag("path", path)

	// add environment variables to the span
	// MY_NODE_NAME
	// MY_CPU_REQUEST
	// MY_CPU_LIMIT
	// MY_MEM_REQUEST
	// MY_MEM_LIMIT
	nodeName := os.Getenv("MY_NODE_NAME")
	if len(nodeName) > 0 {
		span.Tag("node", nodeName)
	}
	cpuRequest := os.Getenv("MY_CPU_REQUEST")
	if len(cpuRequest) > 0 {
		span.Tag("cpuRequest", cpuRequest)
	}
	cpuLimit := os.Getenv("MY_CPU_LIMIT")
	if len(cpuLimit) > 0 {
		span.Tag("cpuLimit", cpuLimit)
	}
	memRequest := os.Getenv("MY_MEM_REQUEST")
	if len(memRequest) > 0 {
		span.Tag("memRequest", memRequest)
	}
	memLimit := os.Getenv("MY_MEM_LIMIT")
	if len(memLimit) > 0 {
		span.Tag("memLimit", memLimit)
	}

	logrus.WithFields(logrus.Fields{
		"method":  req.Method,
		"path":    req.RequestURI,
		"traceID": span.Context().TraceID,
		"server":  urlWithPath,
	}).Info("Redirecting request")

	defer span.Annotate(time.Now(), "redirect to "+url+path+" finished")
	// add the queryParam query param to the new request
	q := newReq.URL.Query()

	newReq.URL.RawQuery = q.Encode()
	// make the request
	resp, err := client.DoWithAppSpan(newReq, "redirect")
	if err != nil {
		logrus.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Could not redirect request to " + url + path + "!"))
		return
	}
	defer resp.Body.Close()

	// read the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logrus.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Could not read response from " + url + path + "!"))
		return
	}

	// write the response body to the original response writer
	io.WriteString(w, string(body))
}

func annotateSpanWithHeaders(span zipkin.Span, header http.Header) {
	// add User-Agent header to span
	if len(header.Get("User-Agent")) > 0 {
		span.Tag("User-Agent", header.Get("User-Agent"))
	}
}

func RandomErrorServer(w http.ResponseWriter, req *http.Request) {
	code := http.StatusOK
	start := time.Now()
	defer func() { recordMetrics(start, req, code) }()
	span := zipkin.SpanFromContext(req.Context())
	logrus.WithFields(logrus.Fields{
		"method":  req.Method,
		"path":    req.RequestURI,
		"traceID": span.Context().TraceID,
	}).Info("Request received")

	client_id := req.URL.Query().Get("client_id")
	span.Tag("client_id", client_id)

	// throw dice to see where we redirect
	// 25% chance to redirect to go-demo-service-one
	// 25% chance to redirect to go-demo-service-two
	// 50% chance to redirect to both
	rand.Seed(time.Now().UnixNano())
	r := rand.Intn(100)
	if r < 25 {
		redirectRequest(w, req, goDemoServiceOneURL, "random-error")
	} else if r < 50 {
		redirectRequest(w, req, goDemoServiceTwoURL, "random-error")
	} else {
		redirectRequest(w, req, goDemoServiceOneURL, "random-error")
		redirectRequest(w, req, goDemoServiceTwoURL, "random-error")
	}
}

func RandomDelayServer(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { recordMetrics(start, req, http.StatusOK) }()
	span := zipkin.SpanFromContext(req.Context())
	logrus.WithFields(logrus.Fields{
		"method":  req.Method,
		"path":    req.RequestURI,
		"traceID": span.Context().TraceID,
	}).Info("Request received")

	// throw dice to see where we redirect
	// 25% chance to redirect to go-demo-service-one
	// 25% chance to redirect to go-demo-service-two
	// 50% chance to redirect to both
	rand.Seed(time.Now().UnixNano())
	r := rand.Intn(100)
	if r < 25 {
		redirectRequest(w, req, goDemoServiceOneURL, "random-delay")
	} else if r < 50 {
		redirectRequest(w, req, goDemoServiceTwoURL, "random-delay")
	} else {
		redirectRequest(w, req, goDemoServiceOneURL, "random-delay")
		redirectRequest(w, req, goDemoServiceTwoURL, "random-delay")
	}

	clientId := req.URL.Query().Get("client_id")
	span.Tag("client_id", clientId)

	// parse the client_id to an int
	clientIdInt, err := strconv.Atoi(clientId)
	if err != nil {
		logrus.Warnf("Could not parse client_id %s to int", clientId)
	} else {
		// if client_id is even, add an additional delay
		if clientIdInt%2 == 0 {
			calculateDelay(span)
		}
	}

}

func VersionServer(w http.ResponseWriter, req *http.Request) {
	logrus.Infof("%s request to %s", req.Method, req.RequestURI)
	release := req.Header.Get("release")
	if release == "" {
		release = "unknown"
	}

	// HOSTNAME, K_REVISION, K_SERVICE

	msg := fmt.Sprintf("Chart Version: %s; Image Version: %s; Release: %s, "+
		"SemVer: %s, GitCommit: %s,"+
		"Host: %s, Revision: %s, Service: %s\n",
		os.Getenv("CHART_VERSION"), os.Getenv("IMAGE_VERSION"), release,
		SemVer, GitCommit,
		os.Getenv("HOSTNAME"), os.Getenv("K_REVISION"), os.Getenv("K_SERVICE"))
	io.WriteString(w, msg)
}

func LimiterServer(w http.ResponseWriter, req *http.Request) {
	span := zipkin.SpanFromContext(req.Context())
	logrus.WithFields(logrus.Fields{
		"method":  req.Method,
		"path":    req.RequestURI,
		"traceID": span.Context().TraceID,
	}).Info("Request received")
	if limiter.Allow() == false {
		logrus.Info("Limiter in action")
		http.Error(w, http.StatusText(500), http.StatusTooManyRequests)
		limitReached = true
		limitReachedTime = time.Now()
		return
	} else if time.Since(limitReachedTime).Seconds() < 15 {
		logrus.Info("Cooling down after the limiter")
		http.Error(w, http.StatusText(500), http.StatusTooManyRequests)
		return
	}
	msg := fmt.Sprintf("Everything is OK\n")
	io.WriteString(w, msg)
}

func recordMetrics(start time.Time, req *http.Request, code int) {
	duration := time.Since(start)
	histogram.With(
		prometheus.Labels{
			"service": serviceName,
			"code":    fmt.Sprintf("%d", code),
			"method":  req.Method,
			"path":    req.URL.Path,
		},
	).Observe(duration.Seconds())
}

func calculateDelay(parentSpan zipkin.Span) {
	spanOptions := zipkin.Parent(parentSpan.Context())
	myTracer, err := newTracer()
	if err != nil {
		logrus.Error(err)
	}

	span := myTracer.StartSpan("delay", spanOptions)
	defer span.Finish()
	span.Annotate(time.Now(), "delay start")
	delay := rand.Intn(1500)
	sleep(time.Duration(delay) * time.Millisecond)
	span.Tag("delay", string(delay))
	span.Annotate(time.Now(), "delay finished")
}
