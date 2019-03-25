/*
Copyright 2019 The Knative Authors

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

package queue

import (
	"bufio"
	"html/template"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/knative/pkg/websocket"
)

func RequestLogHandler(h http.Handler, out *os.File, template *template.Template) http.Handler {
	return &requestLogHandler{
		handler:  h,
		out:      out,
		template: template,
	}
}

type requestLogHandler struct {
	handler  http.Handler
	out      *os.File
	template *template.Template
}

type templateInput struct {
	Request         *http.Request
	ResponseLatency float64
	ResponseCode    int
}

func (h *requestLogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rw := &requestLogWriter{}
	startTime := time.Now()
	defer func() {
		err := recover()
		latency := time.Since(startTime).Seconds()
		if err != nil {
			t := &templateInput{r, latency, http.StatusInternalServerError}
			h.writeRequestLog(t)
			panic(err)
		} else {
			t := &templateInput{r, latency, rw.responseCode}
			h.writeRequestLog(t)
		}
	}()
	h.handler.ServeHTTP(rw, r)
}

func (h *requestLogHandler) writeRequestLog(t *templateInput) {
	h.template.Execute(h.out, t)
}

type requestLogWriter struct {
	writer       http.ResponseWriter
	wroteHeader  bool
	responseCode int

	// hijacked is whether this connection has been hijacked
	// by a Handler with the Hijacker interface.
	// This is guarded by a mutex in the default implementation.
	// To emulate the same behavior, we will use an int32 and
	// access to this field only through atomic calls.
	hijacked int32
}

var _ http.Flusher = (*requestLogWriter)(nil)

var _ http.ResponseWriter = (*requestLogWriter)(nil)

func (w *requestLogWriter) Flush() {
	w.writer.(http.Flusher).Flush()
}

// Hijack calls Hijack() on the wrapped http.ResponseWriter if it implements
// http.Hijacker interface, which is required for net/http/httputil/reverseproxy
// to handle connection upgrade/switching protocol.  Otherwise returns an error.
func (w *requestLogWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := websocket.HijackIfPossible(w.writer)
	if err != nil {
		atomic.StoreInt32(&w.hijacked, 1)
	}
	return c, rw, err
}

func (w *requestLogWriter) Header() http.Header {
	return w.writer.Header()
}

func (w *requestLogWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *requestLogWriter) WriteHeader(code int) {
	if w.wroteHeader || atomic.LoadInt32(&w.hijacked) == 1 {
		return
	}

	w.writer.WriteHeader(code)
	w.wroteHeader = true
	w.responseCode = code
}
