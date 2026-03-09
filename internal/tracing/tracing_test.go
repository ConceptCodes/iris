package tracing

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func newTestTracer() (*tracetest.SpanRecorder, trace.Tracer) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider()
	tp.RegisterSpanProcessor(recorder)
	return recorder, tp.Tracer("test")
}

func TestInitTracerWithoutEndpointReturnsNoopShutdown(t *testing.T) {
	shutdown, err := InitTracer(context.Background(), "test-service", "")
	if err != nil {
		t.Fatalf("InitTracer: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	shutdown()
}

func TestStartSpanCreatesSpan(t *testing.T) {
	recorder, tracer := newTestTracer()
	ctx, span := StartSpan(context.Background(), tracer, "span-name")
	if ctx == nil || span == nil {
		t.Fatal("expected context and span")
	}
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "span-name" {
		t.Fatalf("unexpected spans: %+v", spans)
	}
}

func TestStartSpanWithAttributesAddsAttributes(t *testing.T) {
	recorder, tracer := newTestTracer()
	_, span := StartSpanWithAttributes(context.Background(), tracer, "span-with-attrs", []attribute.KeyValue{
		attribute.String("key", "value"),
	})
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}
	attrs := spans[0].Attributes()
	if len(attrs) == 0 || attrs[0].Key != "key" || attrs[0].Value.AsString() != "value" {
		t.Fatalf("expected key=value attribute, got %+v", attrs)
	}
}

func TestAddErrorToSpanRecordsError(t *testing.T) {
	recorder, tracer := newTestTracer()
	_, span := StartSpan(context.Background(), tracer, "error-span")
	AddErrorToSpan(span, errors.New("boom"))
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}
	if len(spans[0].Events()) == 0 {
		t.Fatal("expected error event to be recorded")
	}
}

func TestSpanEnderEndsSpanAndRecordsError(t *testing.T) {
	recorder, tracer := newTestTracer()
	_, span := StartSpan(context.Background(), tracer, "ender")
	NewSpanEnder(span).EndWithError(errors.New("boom"))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}
	if len(spans[0].Events()) == 0 {
		t.Fatal("expected recorded error event")
	}
}

func TestWithSpanReturnsFunctionErrorAndRecordsIt(t *testing.T) {
	recorder, tracer := newTestTracer()
	err := WithSpan(context.Background(), tracer, "with-span", func(ctx context.Context) error {
		return errors.New("fail")
	})
	if err == nil || err.Error() != "fail" {
		t.Fatalf("expected fail error, got %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}
	if spans[0].Name() != "with-span" {
		t.Fatalf("unexpected span name: %s", spans[0].Name())
	}
	if len(spans[0].Events()) == 0 {
		t.Fatal("expected error to be recorded on span")
	}
}

func TestWithSpanSuccessReturnsNil(t *testing.T) {
	recorder, tracer := newTestTracer()
	err := WithSpan(context.Background(), tracer, "with-span-ok", func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "with-span-ok" {
		t.Fatalf("unexpected spans: %+v", spans)
	}
}
