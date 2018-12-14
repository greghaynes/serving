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
	"net/http"

	"github.com/knative/serving/pkg/tracing/config"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	zipkin "github.com/openzipkin/zipkin-go-opentracing"
	"go.uber.org/zap"
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

func createSpanFromRequest(t opentracing.Tracer, logger *zap.SugaredLogger, r *http.Request, opName string) opentracing.Span {
	wireContext, _ := t.Extract(
		opentracing.HTTPHeaders,
		opentracing.HTTPHeadersCarrier(r.Header))

	// Create the span referring to the RPC client if available.
	// If wireContext == nil, a root span will be created.
	return t.StartSpan(
		opName,
		ext.RPCServerOption(wireContext))
}

func (t *ZipkinTracer) Close() {
	t.collector.Close()
}

type TracerRefGetter func(context.Context) *TracerRef

type spanHandler struct {
	logger   *zap.SugaredLogger
	opName   string
	next     http.Handler
	trGetter TracerRefGetter
}

func (h *spanHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tracerRef := h.trGetter(r.Context())
	if tracerRef == nil {
		h.logger.Error("Failed to get tracer")
		h.next.ServeHTTP(w, r)
		return
	}
	defer tracerRef.Done()
	tracer := tracerRef.Tracer.Tracer

	parentSpan := createSpanFromRequest(tracer, h.logger, r, h.opName)
	span := tracer.StartSpan(h.opName, opentracing.ChildOf(parentSpan.Context()))
	defer span.Finish()

	// store span in context
	ctx := opentracing.ContextWithSpan(r.Context(), span)

	// update request context to include our new span
	r = r.WithContext(ctx)
	h.next.ServeHTTP(w, r)
}

func HTTPSpanMiddleware(logger *zap.SugaredLogger, operationName string, getTr TracerRefGetter, next http.Handler) http.Handler {
	return &spanHandler{
		logger:   logger,
		opName:   operationName,
		next:     next,
		trGetter: getTr,
	}
}

func CreateChildSpanFromContext(t opentracing.Tracer, ctx context.Context, opName string) opentracing.Span {
	parentSpan := opentracing.SpanFromContext(ctx)
	if parentSpan != nil {
		return t.StartSpan(opName, opentracing.ChildOf(parentSpan.Context()))
	}
	return t.StartSpan(opName)
}
