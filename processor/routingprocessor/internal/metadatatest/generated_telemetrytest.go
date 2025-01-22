// Code generated by mdatagen. DO NOT EDIT.

package metadatatest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/multierr"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processortest"
)

type Telemetry struct {
	Reader       *sdkmetric.ManualReader
	SpanRecorder *tracetest.SpanRecorder

	meterProvider *sdkmetric.MeterProvider
	traceProvider *sdktrace.TracerProvider
}

func SetupTelemetry() Telemetry {
	reader := sdkmetric.NewManualReader()
	spanRecorder := new(tracetest.SpanRecorder)
	return Telemetry{
		Reader:       reader,
		SpanRecorder: spanRecorder,

		meterProvider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		traceProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder)),
	}
}
func (tt *Telemetry) NewSettings() processor.Settings {
	set := processortest.NewNopSettings()
	set.ID = component.NewID(component.MustNewType("routing"))
	set.TelemetrySettings = tt.NewTelemetrySettings()
	return set
}

func (tt *Telemetry) NewTelemetrySettings() component.TelemetrySettings {
	set := componenttest.NewNopTelemetrySettings()
	set.MeterProvider = tt.meterProvider
	set.MetricsLevel = configtelemetry.LevelDetailed
	set.TracerProvider = tt.traceProvider
	return set
}

func (tt *Telemetry) AssertMetrics(t *testing.T, expected []metricdata.Metrics, opts ...metricdatatest.Option) {
	var md metricdata.ResourceMetrics
	require.NoError(t, tt.Reader.Collect(context.Background(), &md))
	// ensure all required metrics are present
	for _, want := range expected {
		got := getMetric(want.Name, md)
		metricdatatest.AssertEqual(t, want, got, opts...)
	}

	// ensure no additional metrics are emitted
	require.Equal(t, len(expected), lenMetrics(md))
}

func (tt *Telemetry) Shutdown(ctx context.Context) error {
	return multierr.Combine(
		tt.meterProvider.Shutdown(ctx),
		tt.traceProvider.Shutdown(ctx),
	)
}

func getMetric(name string, got metricdata.ResourceMetrics) metricdata.Metrics {
	for _, sm := range got.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m
			}
		}
	}

	return metricdata.Metrics{}
}

func lenMetrics(got metricdata.ResourceMetrics) int {
	metricsCount := 0
	for _, sm := range got.ScopeMetrics {
		metricsCount += len(sm.Metrics)
	}

	return metricsCount
}
