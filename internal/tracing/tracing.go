package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// InitTracer initializes an OpenTelemetry tracer provider with OTLP exporter.
// It returns a shutdown function that should be called on application exit.
func InitTracer(ctx context.Context, serviceName, otlpEndpoint string) (shutdown func(), err error) {
	if otlpEndpoint == "" {
		// Return a no-op shutdown if no endpoint is configured
		return func() {}, nil
	}

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(otlpEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service name
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider with AlwaysSample sampler
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Return shutdown function
	return func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			fmt.Printf("failed to shutdown tracer provider: %v\n", err)
		}
	}, nil
}

// StartSpan starts a new span with the given name.
// It returns a context with the span and the span itself.
func StartSpan(ctx context.Context, tracer trace.Tracer, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracer.Start(ctx, name, opts...)
}

// StartSpanWithAttributes starts a new span with the given name and attributes.
func StartSpanWithAttributes(ctx context.Context, tracer trace.Tracer, name string, attrs []attribute.KeyValue, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append(opts, trace.WithAttributes(attrs...))
	return tracer.Start(ctx, name, opts...)
}

// AddErrorToSpan adds an error to the span and records it.
func AddErrorToSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.message", err.Error()))
	}
}

// SpanEnder is a helper to defer span end with error handling
type SpanEnder struct {
	span trace.Span
}

// EndWithError ends the span and records the error if not nil
func (s *SpanEnder) EndWithError(err error) {
	if err != nil {
		AddErrorToSpan(s.span, err)
	}
	s.span.End()
}

// NewSpanEnder creates a new SpanEnder
func NewSpanEnder(span trace.Span) *SpanEnder {
	return &SpanEnder{span: span}
}

// WithSpan executes a function within a new span
func WithSpan(ctx context.Context, tracer trace.Tracer, name string, fn func(context.Context) error, opts ...trace.SpanStartOption) error {
	ctx, span := StartSpan(ctx, tracer, name, opts...)
	defer span.End()
	if err := fn(ctx); err != nil {
		AddErrorToSpan(span, err)
		return err
	}
	return nil
}
