package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	kitgrpc "github.com/go-kit/kit/transport/grpc"
	"github.com/opentracing/opentracing-go"
	stdopentracing "github.com/opentracing/opentracing-go"
	"github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/uber/jaeger-client-go"
	jconfig "github.com/uber/jaeger-client-go/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/cage1016/add/internal/app/add/endpoints"
	"github.com/cage1016/add/internal/app/add/service"
	transportsgrpc "github.com/cage1016/add/internal/app/add/transports/grpc"
	pb "github.com/cage1016/add/pb/add"
)

const (
	defJaegerURL   = ""
	defZipkinV2URL = ""
	defServiceName = "add"
	defLogLevel    = "error"
	defServiceHost = "localhost"
	defGRPCPort    = "8181"

	envJaegerURL   = "QS_JAEGER_URL"
	envZipkinV2URL = "QS_ZIPKIN_V2_URL"
	envServiceName = "QS_SERVICE_NAME"
	envLogLevel    = "QS_LOG_LEVEL"
	envServiceHost = "QS_SERVICE_HOST"
	envGRPCPort    = "PORT"
)

type config struct {
	serviceName string
	logLevel    string
	serviceHost string
	grpcPort    string
	zipkinV2URL string
	jaegerURL   string
}

// Env reads specified environment variable. If no value has been found,
// fallback is returned.
func env(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = level.NewFilter(logger, level.AllowInfo())
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}
	cfg := loadConfig(logger)
	logger = log.With(logger, "service", cfg.serviceName)
	level.Info(logger).Log("version", service.Version, "commitHash", service.CommitHash, "buildTimeStamp", service.BuildTimeStamp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracer, closer := initJaeger(cfg.serviceName, cfg.jaegerURL, logger)
	defer closer.Close()

	zipkinTracer := initZipkin(cfg.serviceName, cfg.grpcPort, cfg.zipkinV2URL, logger)
	service := NewServer(logger)
	endpoints := endpoints.New(service, logger, tracer, zipkinTracer)

	hs := health.NewServer()
	hs.SetServingStatus(cfg.serviceName, healthgrpc.HealthCheckResponse_SERVING)

	wg := &sync.WaitGroup{}

	go startGRPCServer(ctx, wg, endpoints, tracer, zipkinTracer, cfg.grpcPort, hs, logger)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	cancel()
	wg.Wait()

	fmt.Println("main: all goroutines have told us they've finished")
}

func loadConfig(logger log.Logger) (cfg config) {
	cfg.serviceName = env(envServiceName, defServiceName)
	cfg.logLevel = env(envLogLevel, defLogLevel)
	cfg.serviceHost = env(envServiceHost, defServiceHost)
	cfg.grpcPort = env(envGRPCPort, defGRPCPort)
	cfg.zipkinV2URL = env(envZipkinV2URL, defZipkinV2URL)
	cfg.jaegerURL = env(envJaegerURL, defJaegerURL)
	return cfg
}

func NewServer(logger log.Logger) service.AddService {
	service := service.New(logger)
	return service
}

func initJaeger(svcName, url string, logger log.Logger) (opentracing.Tracer, io.Closer) {
	if url == "" {
		return opentracing.NoopTracer{}, ioutil.NopCloser(nil)
	}

	tracer, closer, err := jconfig.Configuration{
		ServiceName: svcName,
		Sampler: &jconfig.SamplerConfig{
			Type:  jaeger.SamplerTypeConst,
			Param: 1,
		},
		Reporter: &jconfig.ReporterConfig{
			LocalAgentHostPort: url,
			LogSpans:           true,
		},
	}.NewTracer()
	if err != nil {
		level.Error(logger).Log("msg", fmt.Sprintf("Failed to init Jaeger: %s", err))
		os.Exit(1)
	}

	opentracing.SetGlobalTracer(tracer)
	return tracer, closer
}

func initZipkin(serviceName, httpPort, zipkinV2URL string, logger log.Logger) (zipkinTracer *zipkin.Tracer) {
	var (
		err           error
		hostPort      = fmt.Sprintf("localhost:%s", httpPort)
		useNoopTracer = (zipkinV2URL == "")
		reporter      = zipkinhttp.NewReporter(zipkinV2URL)
	)
	zEP, _ := zipkin.NewEndpoint(serviceName, hostPort)
	zipkinTracer, err = zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(zEP), zipkin.WithNoopTracer(useNoopTracer))
	if err != nil {
		logger.Log("err", err)
		os.Exit(1)
	}
	if !useNoopTracer {
		logger.Log("tracer", "Zipkin", "type", "Native", "URL", zipkinV2URL)
	}

	return
}

func startGRPCServer(ctx context.Context, wg *sync.WaitGroup, endpoints endpoints.Endpoints, tracer stdopentracing.Tracer, zipkinTracer *zipkin.Tracer, port string, hs *health.Server, logger log.Logger) {
	wg.Add(1)
	defer wg.Done()

	p := fmt.Sprintf(":%s", port)
	listener, err := net.Listen("tcp", p)
	if err != nil {
		level.Error(logger).Log("protocol", "GRPC", "listen", port, "err", err)
		os.Exit(1)
	}

	var server *grpc.Server
	level.Info(logger).Log("protocol", "GRPC", "exposed", port)
	server = grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor))
	pb.RegisterAddServer(server, transportsgrpc.MakeGRPCServer(endpoints, tracer, zipkinTracer, logger))
	healthgrpc.RegisterHealthServer(server, hs)
	reflection.Register(server)

	go func() {
		// service connections
		err = server.Serve(listener)
		if err != nil {
			fmt.Printf("grpc serve : %s\n", err)
		}
	}()

	<-ctx.Done()

	// ignore error since it will be "Err shutting down server : context canceled"
	server.GracefulStop()

	fmt.Println("grpc server gracefully stopped")
}
