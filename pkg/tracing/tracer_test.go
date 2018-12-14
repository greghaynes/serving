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
	"testing"

	"github.com/google/go-cmp/cmp"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"

	testlogging "github.com/knative/pkg/test/logging"
)

func TestCreateChildSpanFromContext(t *testing.T) {
	// Assert simple state attributes of creating span from ctx with no span
	tracer := mocktracer.New()
	span := CreateChildSpanFromContext(tracer, context.TODO(), "test").(*mocktracer.MockSpan)
	if pid := span.BaggageItem("parentID"); pid != "" {
		t.Errorf("Expected \"\" span parentID, got: %q", pid)
	}

	// Test that we create subspans of the existing span in ctx
	parSpan := tracer.StartSpan("test_subspan_parent").(*mocktracer.MockSpan)
	ctx := opentracing.ContextWithSpan(context.TODO(), parSpan)
	subSpan := CreateChildSpanFromContext(tracer, ctx, "test_subspan_child").(*mocktracer.MockSpan)
	if pid := parSpan.SpanContext.SpanID; pid != subSpan.ParentID {
		t.Errorf("Expected span parentID = %d, got: %d", pid, subSpan.ParentID)
	}

	if subSpan.OperationName != "test_subspan_child" {
		t.Errorf("Expected subSpan OperationName = test_subspan_child, got %v", subSpan.OperationName)
	}
}

type testHandler struct {
}

func (th *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte{'f', 'a', 'k', 'e'})
}

type fakeWriter struct {
	lastWrite *[]byte
}

func (fw fakeWriter) Header() http.Header {
	return http.Header{}
}

func (fw fakeWriter) Write(data []byte) (int, error) {
	*fw.lastWrite = data
	return len(data), nil
}

func (fw fakeWriter) WriteHeader(statusCode int) {
}

func TestHTTPSpanMiddleware(t *testing.T) {
	testlogging.InitializeLogger(true)
	logger := testlogging.GetContextLogger("TestSpanMiddleware")

	tracer := mocktracer.New()
	trGetter := func(ctx context.Context) *TracerRef {
		ref := &TracerRef{
			Tracer: &ZipkinTracer{
				Tracer: tracer,
			},
		}
		ref.Ref()
		return ref
	}

	th := testHandler{}
	mw := HTTPSpanMiddleware(logger.Logger, "test-span", trGetter, &th)

	// Assert we havent created any spans yet
	if diff := cmp.Diff([]*mocktracer.MockSpan{}, tracer.FinishedSpans()); diff != "" {
		t.Errorf("Got finished spans (-want, +got) = %v", diff)
	}

	var lastWrite []byte
	fw := fakeWriter{lastWrite: &lastWrite}
	mw.ServeHTTP(fw, &http.Request{Header: http.Header{}})

	// Assert our next handler was called
	if diff := cmp.Diff([]byte{'f', 'a', 'k', 'e'}, lastWrite); diff != "" {
		t.Errorf("Got http response (-want, +got) = %v", diff)
	}

	spans := tracer.FinishedSpans()
	if len(spans) != 1 {
		t.Errorf("Expected 1 finished span, got %d", len(spans))
	}
}
