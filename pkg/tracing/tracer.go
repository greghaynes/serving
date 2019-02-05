/*
Copyright 2018 The Knative Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tracing

import (
	"context"

	"github.com/knative/serving/pkg/tracing/config"
	opentracing "github.com/opentracing/opentracing-go"
	zipkin "github.com/openzipkin/zipkin-go-opentracing"
)

const (
	serviceName = "activator"
	myHostPort  = "127.0.0.1:61001"
)

func createZipkinTracer(cfg *config.Config) (opentracing.Tracer, zipkin.Collector, error) {
	collector, err := zipkin.NewHTTPCollector(cfg.ZipkinCollectorEndpoint)
	if err != nil {
		return nil, nil, err
	}

	// create recorder.
	recorder := zipkin.NewRecorder(collector, cfg.Debug, myHostPort, serviceName)

	// create tracer.
	tracer, err := zipkin.NewTracer(
		recorder,
		zipkin.ClientServerSameSpan(true),
		zipkin.TraceID128Bit(true),
	)
	if err != nil {
		collector.Close()
		return nil, nil, err
	}

	return tracer, collector, nil
}

type ZipkinTracer struct {
	Tracer    opentracing.Tracer
	collector zipkin.Collector
}

func CreateZipkinTracer(cfg *config.Config) (*ZipkinTracer, error) {
	tracer, collector, err := createZipkinTracer(cfg)
	if err != nil {
		return nil, err
	}

	return &ZipkinTracer{
		Tracer:    tracer,
		collector: collector,
	}, nil
}

func (t *ZipkinTracer) Close() {
	t.collector.Close()
}

func CreateChildSpanFromContext(t opentracing.Tracer, ctx context.Context, opName string) (context.Context, opentracing.Span) {
	parentSpan := opentracing.SpanFromContext(ctx)
	var newSpan opentracing.Span
	if parentSpan != nil {
		newSpan = t.StartSpan(opName, opentracing.ChildOf(parentSpan.Context()))
	} else {
		newSpan = t.StartSpan(opName)
	}
	return opentracing.ContextWithSpan(ctx, newSpan), newSpan
}
