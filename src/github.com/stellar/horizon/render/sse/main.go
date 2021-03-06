package sse

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/stellar/horizon/log"
	"golang.org/x/net/context"
)

// Event is the packet of data that gets sent over the wire to a connected
// client.
type Event struct {
	Data  interface{}
	Error error
	ID    string
	Event string
	Retry int
}

// SseEvent returns the SSE compatible form of the Event... itself.
func (e Event) SseEvent() Event {
	return e
}

// Upon initial stream creation, we send this event to inform the client
// that they may retry an errored connection after 1 second.
var helloEvent = Event{
	Data:  "hello",
	Event: "open",
	Retry: 1000,
}

// Upon successful completion of a query (i.e. the client didn't disconnect
// and we didn't error) we send a "Goodbye" event.  This is a dummy event
// so that we can set a low retry value so that the client will immediately
// recoonnect and request more data.  This helpes to give the feel of a infinite
// stream of data, even though we're actually responding in PAGE_SIZE chunks.
var goodbyeEvent = Event{
	Data:  "byebye",
	Event: "close",
	Retry: 10,
}

// Eventable represents an object that can be converted to an SSE compatible
// event.
type Eventable interface {
	// SseEvent returns the SSE compatible form of the implementer
	SseEvent() Event
}

// Streamer handles the work of turning a channel of Eventable objects
// into a http response to a client.  Construct one and call `ServeHTTP` to do
// so
type Streamer struct {
	Ctx  context.Context
	Data <-chan Eventable
}

func (s *Streamer) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if !WritePreamble(s.Ctx, w) {
		return
	}

	// wait for data and stream it as it becomes available
	// finish when either the client closes the connection
	// or the data provider closes the channel
	for {
		select {
		case eventable, more := <-s.Data:
			if !more {
				WriteEvent(s.Ctx, w, goodbyeEvent)
				return
			}
			WriteEvent(s.Ctx, w, eventable.SseEvent())
		case <-s.Ctx.Done():
			return
		}
	}
}

func WritePreamble(ctx context.Context, w http.ResponseWriter) bool {

	_, flushable := w.(http.Flusher)

	if !flushable {
		//TODO: render a problem struct instead of simple string
		http.Error(w, "Streaming Not Supported", http.StatusBadRequest)
		return false
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	WriteEvent(ctx, w, helloEvent)

	return true
}

// WriteEvent does the actual work of formatting an SSE compliant message
// sending it over the provided ResponseWriter and flushing.
func WriteEvent(ctx context.Context, w http.ResponseWriter, e Event) {
	if e.Error != nil {
		fmt.Fprint(w, "event: err\n")
		fmt.Fprintf(w, "data: %s\n\n", e.Error.Error())
		w.(http.Flusher).Flush()
		log.Error(ctx, e.Error)
		return
	}

	// TODO: add tests to ensure retry get's properly rendered
	if e.Retry != 0 {
		fmt.Fprintf(w, "retry: %d\n", e.Retry)
	}

	if e.ID != "" {
		fmt.Fprintf(w, "id: %s\n", e.ID)
	}

	if e.Event != "" {
		fmt.Fprintf(w, "event: %s\n", e.Event)
	}

	fmt.Fprintf(w, "data: %s\n\n", getJSON(e.Data))
	w.(http.Flusher).Flush()
}

func getJSON(val interface{}) string {
	js, err := json.Marshal(val)

	if err != nil {
		panic(err)
	}

	return string(js)
}
