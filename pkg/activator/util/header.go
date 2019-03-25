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

package util

import (
	"net/http"
	"net/http/httputil"

	"github.com/knative/serving/pkg/apis/serving"
)

var headersToRemove = []string{
	serving.ActivatorRevisionHeaderName,
	serving.ActivatorRevisionHeaderNamespace,
}

// SetupHeaderPruning will cause the http.ReverseProxy
// to not forward activator headers
func SetupHeaderPruning(p *httputil.ReverseProxy) {
	// Director is never null - otherwise ServeHTTP panics
	orig := p.Director
	p.Director = func(r *http.Request) {
		orig(r)

		for _, h := range headersToRemove {
			r.Header.Del(h)
		}
	}
}
