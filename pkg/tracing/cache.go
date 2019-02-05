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
	"errors"
	"sync"

	"github.com/knative/serving/pkg/tracing/config"
)

// TracerRef performs reference counting for a given tracer. This is needed to
// allow for closing a tracer only after all it's traces are completed.
type TracerRef struct {
	Tracer *ZipkinTracer
	refs   sync.RWMutex
}

func (tr *TracerRef) Ref() {
	tr.refs.RLock()
}

func (tr *TracerRef) Done() {
	tr.refs.RUnlock()
}

func (tr *TracerRef) closeWhenUnused() {
	// Block until our tracer becomes unused
	tr.refs.Lock()
	tr.Tracer.Close()
}

// TracerCache manages tracer lifecycle and caches the most recently used based
// on Config. It also allows for the immediate creation of new tracers while
// outstanding traces exist for the current tracer by reference counting.
//
// Make sure to call Close() when exiting in order to flush outstanding traces.
type TracerCache struct {
	mutex     sync.Mutex
	tracerRef *TracerRef
	cfg       *config.Config
}

// GetTracer returns a TracerRef for the passed Config. Make sure to call
// Done on the returned TracerRef when you are done using it. If an invalid
// config is passed then a previous tracer is returned if it exists along with
// an error.
func (tc *TracerCache) GetTracer(cfg *config.Config) (*TracerRef, error) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()

	if cfg == nil {
		return nil, errors.New("Got nil tracer config")
	}

	if tc.cfg == nil || !tc.cfg.Equals(cfg) {
		tracer, err := CreateZipkinTracer(cfg)
		if err != nil {
			tc.tracerRef.refs.RLock()
			return tc.tracerRef, err
		}
		if tc.tracerRef != nil {
			// Close our tracer when references go to 0
			go tc.tracerRef.closeWhenUnused()
		}

		// Create our tracer
		tc.tracerRef = &TracerRef{
			Tracer: tracer,
		}
		tc.cfg = cfg
	}

	// Reference this tracer
	tc.tracerRef.Ref()
	return tc.tracerRef, nil
}

// Close closes the ZipkinTracer, flushing outstanding traces.
func (tc *TracerCache) Close() {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()

	if tc.tracerRef != nil {
		tc.tracerRef.closeWhenUnused()
	}

	tc.tracerRef = nil
	tc.cfg = nil
}

type cfgKey struct{}

func ToContext(ctx context.Context, tracer *ZipkinTracer) context.Context {
	return context.WithValue(ctx, cfgKey{}, tracer)
}

func FromContext(ctx context.Context) *ZipkinTracer {
	return ctx.Value(cfgKey{}).(*ZipkinTracer)
}
