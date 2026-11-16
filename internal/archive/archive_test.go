package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// connections is find_connections' answer, in the shape On the Record's
// read-only door gives it, with made-up meetings.
const connections = `{
  "matched_terms": ["launch", "price"],
  "semantic": true,
  "reranked": true,
  "meetings": [
    {"id": "m-1", "title": "Pricing review", "date": "2026-11-03", "why": ["shares launch, price", "close in meaning (0.71)", "reranked #1"],
     "passages": [{"text": "we hold the price until March", "at": "12:31", "link": "http://127.0.0.1:8797/m/m-1?t=751"}],
     "link": "http://127.0.0.1:8797/m/m-1"},
    {"id": "m-2", "title": "Launch plan", "date": "2026-10-28", "why": ["shares launch"], "passages": [], "link": "http://127.0.0.1:8797/m/m-2"}
  ],
  "notebook": [
    {"id": "n-1", "title": "Price research", "tags": ["pricing"], "author": "Someone", "updated": "2026-11-02T10:00:00.000Z",
     "updated_by": "Someone", "version": 3, "meetings": [{"id": "m-1", "title": "Pricing review"}], "task_ids": [],
     "link": "http://127.0.0.1:8797/notebook/n-1", "excerpt": "Made-up notes.", "why": ["close in meaning (0.64)"]}
  ],
  "action_items": [
    {"id": "t-1", "rev": 4, "owner": "unassigned", "lane": "now", "text": "Decide the launch date", "status": "open", "source": "meeting notes",
     "meeting": {"id": "m-2", "title": "Launch plan", "date": "2026-10-28"}, "said_at": "1:30", "link": "http://127.0.0.1:8797/m/m-2?t=90"}
  ],
  "suggested_links": {"meeting_ids": ["m-1"], "task_ids": ["t-1"]}
}`

// door is a fake On the Record: it starts a session, takes the
// notification that On Air is ready, and answers find_connections as
// answer says. It refuses every other method and tool, and writes down
// what it was asked.
type door struct {
	mu      sync.Mutex
	methods []string
	texts   []string
	headers []http.Header
	session string // given at initialize, when set
	answer  func(w http.ResponseWriter, id json.RawMessage)
	forget  int // times to answer a call in the session with 404, as if it was forgotten
	// gathered is the arguments of each gather_context call, which brief
	// answers, or else made-up.
	gathered []map[string]any
	brief    func(w http.ResponseWriter, id json.RawMessage)
}

func (d *door) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string          `json:"name"`
			Raw       json.RawMessage `json:"arguments"`
			Arguments struct {
				Text  string `json:"text"`
				Limit int    `json:"limit"`
			} `json:"-"`
		} `json:"params"`
	}
	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&m) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var args map[string]any
	if len(m.Params.Raw) > 0 {
		json.Unmarshal(m.Params.Raw, &m.Params.Arguments)
		json.Unmarshal(m.Params.Raw, &args)
	}
	d.mu.Lock()
	d.methods = append(d.methods, m.Method+" "+m.Params.Name)
	d.headers = append(d.headers, r.Header.Clone())
	forget := d.forget > 0 && m.Method == "tools/call"
	if forget {
		d.forget--
	}
	d.mu.Unlock()
	switch {
	case forget:
		http.Error(w, "unknown session", http.StatusNotFound)
	case m.Method == "initialize":
		if d.session != "" {
			w.Header().Set("Mcp-Session-Id", d.session)
		}
		reply(w, m.ID, `{"protocolVersion":"2025-06-18","capabilities":{"tools":{"listChanged":false}},"serverInfo":{"name":"fake","version":"0"}}`)
	case m.Method == "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case m.Method == "tools/call" && m.Params.Name == "find_connections":
		d.mu.Lock()
		d.texts = append(d.texts, m.Params.Arguments.Text)
		d.mu.Unlock()
		if d.answer != nil {
			d.answer(w, m.ID)
			return
		}
		reply(w, m.ID, fmt.Sprintf(`{"content":[{"type":"text","text":%q}],"structuredContent":%s}`, connections, connections))
	case m.Method == "tools/call" && m.Params.Name == "gather_context":
		d.mu.Lock()
		d.gathered = append(d.gathered, args)
		d.mu.Unlock()
		if d.brief != nil {
			d.brief(w, m.ID)
			return
		}
		reply(w, m.ID, fmt.Sprintf(`{"content":[{"type":"text","text":%q},{"type":"text","text":%q}]}`, madeUpBrief, `{"topic":"x","meetings":["m-1","m-2"],"passages":4,"semantic":true}`))
	default:
		reply(w, m.ID, "", `{"code":-32601,"message":"not on this door"}`)
	}
}

func reply(w http.ResponseWriter, id json.RawMessage, result string, rpcErr ...string) {
	w.Header().Set("Content-Type", "application/json")
	if len(rpcErr) > 0 {
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":%s}`, id, rpcErr[0])
		return
	}
	fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, id, result)
}

func (d *door) asked() ([]string, []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.methods...), append([]string(nil), d.texts...)
}

func open(t *testing.T, d *door) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(d)
	t.Cleanup(server.Close)
	c, err := New(server.URL + "/mcp/readonly")
	if err != nil {
		t.Fatal(err)
	}
	return c, server
}

func ask(c *Client, timeout time.Duration) (live.Found, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Connections(ctx, "the launch date and the price")
}

func TestItAsksFindConnectionsAndReadsItsAnswer(t *testing.T) {
	d := &door{session: "session-1"}
	c, _ := open(t, d)
	found, err := ask(c, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !found.Semantic || len(found.Meetings) != 2 || len(found.Notes) != 1 || len(found.Items) != 1 {
		t.Fatalf("read %+v", found)
	}
	m := found.Meetings[0]
	if m.ID != "m-1" || m.Title != "Pricing review" || m.Date != "2026-11-03" || m.URL != "http://127.0.0.1:8797/m/m-1" || len(m.Why) != 3 ||
		len(m.Passages) != 1 || m.Passages[0].At != "12:31" || m.Passages[0].URL != "http://127.0.0.1:8797/m/m-1?t=751" {
		t.Fatalf("read the meeting as %+v", m)
	}
	if n := found.Notes[0]; n.ID != "n-1" || n.Title != "Price research" || n.Updated != "2026-11-02T10:00:00.000Z" || n.URL != "http://127.0.0.1:8797/notebook/n-1" || n.Why[0] != "close in meaning (0.64)" {
		t.Fatalf("read the notebook entry as %+v", n)
	}
	if i := found.Items[0]; i.ID != "t-1" || i.Text != "Decide the launch date" || i.Status != "open" || i.Owner != "unassigned" || i.Meeting != "Launch plan" || i.MeetingDate != "2026-10-28" || i.URL != "http://127.0.0.1:8797/m/m-2?t=90" {
		t.Fatalf("read the action item as %+v", i)
	}
	if strings.Join(found.StrongMeetings, ",") != "m-1" || strings.Join(found.StrongItems, ",") != "t-1" {
		t.Fatalf("read the suggestions as %v and %v", found.StrongMeetings, found.StrongItems)
	}
	// The session starts once, and is used after.
	if _, err := ask(c, time.Second); err != nil {
		t.Fatal(err)
	}
	methods, texts := d.asked()
	if strings.Join(methods, ", ") != "initialize , notifications/initialized , tools/call find_connections, tools/call find_connections" {
		t.Fatalf("asked %v", methods)
	}
	if texts[0] != "the launch date and the price" {
		t.Fatalf("sent %q", texts[0])
	}
	for i, h := range d.headers {
		if !strings.Contains(h.Get("Accept"), "application/json") || !strings.Contains(h.Get("Accept"), "text/event-stream") {
			t.Fatalf("request %d accepts %q", i, h.Get("Accept"))
		}
		if i > 0 && (h.Get("Mcp-Session-Id") != "session-1" || h.Get("MCP-Protocol-Version") != "2025-06-18") {
			t.Fatalf("request %d has session %q and version %q", i, h.Get("Mcp-Session-Id"), h.Get("MCP-Protocol-Version"))
		}
	}
}

func TestTheAnswersTextIsReadWhenItHasNoStructuredContent(t *testing.T) {
	d := &door{answer: func(w http.ResponseWriter, id json.RawMessage) {
		reply(w, id, fmt.Sprintf(`{"content":[{"type":"text","text":%q}]}`, connections))
	}}
	c, _ := open(t, d)
	found, err := ask(c, time.Second)
	if err != nil || len(found.Meetings) != 2 {
		t.Fatalf("read %+v, %v", found, err)
	}
}

func TestAnAnswerAsAnEventStreamIsRead(t *testing.T) {
	d := &door{answer: func(w http.ResponseWriter, id json.RawMessage) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"structuredContent\":%s}}\n\n", id, strings.ReplaceAll(connections, "\n", ""))
	}}
	c, _ := open(t, d)
	found, err := ask(c, time.Second)
	if err != nil || len(found.Meetings) != 2 || found.StrongMeetings[0] != "m-1" {
		t.Fatalf("read %+v, %v", found, err)
	}
}

func TestAForgottenSessionIsStartedAgainOnce(t *testing.T) {
	d := &door{session: "session-1", forget: 1}
	c, _ := open(t, d)
	if _, err := ask(c, time.Second); err != nil {
		t.Fatal(err)
	}
	methods, _ := d.asked()
	if strings.Join(methods, ", ") != "initialize , notifications/initialized , tools/call find_connections, initialize , notifications/initialized , tools/call find_connections" {
		t.Fatalf("asked %v", methods)
	}
	d.forget = 2
	if _, err := ask(c, time.Second); err == nil || !strings.Contains(err.Error(), "no longer knows") {
		t.Fatalf("a session forgotten twice gave %v", err)
	}
}

// Everything that can go wrong comes back as an error that says what, in
// words for the panel, and in good time.
func TestWhatGoesWrongComesBackAsAnError(t *testing.T) {
	big := strings.Repeat("x", maxAnswer+10)
	for name, tc := range map[string]struct {
		answer func(w http.ResponseWriter, id json.RawMessage)
		says   string
	}{
		"slow": {answer: func(w http.ResponseWriter, id json.RawMessage) { time.Sleep(2 * time.Second) },
			says: "On the Record didn't answer in time"},
		"a failed tool": {answer: func(w http.ResponseWriter, id json.RawMessage) {
			reply(w, id, `{"content":[{"type":"text","text":"text must be a string"}],"isError":true}`)
		}, says: "On the Record couldn't answer: text must be a string"},
		"an error": {answer: func(w http.ResponseWriter, id json.RawMessage) {
			reply(w, id, "", `{"code":-32602,"message":"unknown tool"}`)
		},
			says: "On the Record refused the question: unknown tool"},
		"HTTP 500": {answer: func(w http.ResponseWriter, id json.RawMessage) { http.Error(w, "broken", 500) },
			says: "On the Record answered HTTP 500"},
		"nonsense": {answer: func(w http.ResponseWriter, id json.RawMessage) { w.Write([]byte("<html>not json")) },
			says: "On the Record's answer couldn't be read"},
		"another shape": {answer: func(w http.ResponseWriter, id json.RawMessage) {
			reply(w, id, `{"structuredContent":{"meetings":"none"}}`)
		},
			says: "On the Record's answer wasn't in the shape On Air reads"},
		"too long": {answer: func(w http.ResponseWriter, id json.RawMessage) {
			reply(w, id, fmt.Sprintf(`{"content":[{"type":"text","text":%q}]}`, big))
		}, says: "On the Record's answer was too long to read"},
		"another question's answer": {answer: func(w http.ResponseWriter, id json.RawMessage) { reply(w, json.RawMessage("999"), `{}`) },
			says: "On the Record answered a different question"},
		"a stream that ends": {answer: func(w http.ResponseWriter, id json.RawMessage) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		}, says: "On the Record's answer ended without one"},
		"a redirect": {answer: func(w http.ResponseWriter, id json.RawMessage) {
			http.Redirect(w, &http.Request{}, "http://127.0.0.1:1/elsewhere", http.StatusTemporaryRedirect)
		}, says: "On the Record sent the question elsewhere (HTTP 307), and On Air doesn't follow"},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := open(t, &door{answer: tc.answer})
			start := time.Now()
			_, err := ask(c, 300*time.Millisecond)
			if err == nil || err.Error() != tc.says {
				t.Fatalf("got %v, not %q", err, tc.says)
			}
			if took := time.Since(start); took > time.Second {
				t.Fatalf("took %v", took)
			}
		})
	}
}

func TestADoorThatIsntThereSaysSo(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	c, err := New("http://" + address + "/mcp/readonly")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ask(c, time.Second); err == nil || err.Error() != "On the Record isn't answering on this Mac. Is its server running?" {
		t.Fatalf("a door that isn't there gave %v", err)
	}
}

func TestOnlyALoopbackAddressIsTaken(t *testing.T) {
	for _, ok := range []string{Door, "http://127.0.0.1:8797/mcp/readonly", "http://[::1]:8797/mcp/readonly", "https://127.0.0.2/x"} {
		if _, err := New(ok); err != nil {
			t.Fatalf("%s was refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://localhost:8797/mcp/readonly", "http://example.com/mcp", "http://10.0.0.1:8797/mcp", "http://192.168.1.2/",
		"ftp://127.0.0.1/x", "http://user:pw@127.0.0.1/", "127.0.0.1:8797", "http://[::ffff:8.8.8.8]/", "http://0.0.0.0:8797/"} {
		if _, err := New(bad); err == nil {
			t.Fatalf("%s was taken", bad)
		}
	}
	// Whatever asks to be dialed, only a loopback address is.
	for _, address := range []string{"93.184.216.34:80", "example.com:80", "[2001:db8::1]:443", "nonsense"} {
		if _, err := dial(context.Background(), "tcp", address); err == nil || !strings.Contains(err.Error(), "only asks On the Record on this Mac") {
			t.Fatalf("dialing %s gave %v", address, err)
		}
	}
}

func TestNoProxyIsUsed(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	d := &door{}
	c, _ := open(t, d)
	if _, err := ask(c, time.Second); err != nil {
		t.Fatalf("with a proxy set: %v", err)
	}
}
