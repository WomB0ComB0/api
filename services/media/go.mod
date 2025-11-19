module api/media

go 1.22

require (
	github.com/aws/aws-sdk-go-v2 v1.26.1
	github.com/aws/aws-sdk-go-v2/config v1.27.11
	github.com/aws/aws-sdk-go-v2/credentials v1.17.11
	github.com/aws/aws-sdk-go-v2/service/s3 v1.53.1
	github.com/go-chi/chi/v5 v5.0.12
	github.com/go-chi/cors v1.2.1
	github.com/go-chi/httprate v0.9.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/rs/zerolog v1.32.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.51.0
	go.opentelemetry.io/otel v1.26.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.26.0
	go.opentelemetry.io/otel/sdk v1.26.0
)
