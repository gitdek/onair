// Package archive asks On the Record, the meeting archive, what a stretch
// of a call connects to, through its read-only door on this Mac. It speaks
// MCP, JSON-RPC over HTTP, to a loopback address only: it refuses any
// other, dials nothing else, uses no proxy and follows no redirect, so
// nothing it sends leaves the Mac. It calls two tools, which only read:
// find_connections, and gather_context, which it never asks for the
// screen's moments, which the archive's owners may have a hosted model
// match, and asks only in plain words, never in the filters its search
// reads, such as in:screen. It decides nothing: what to make of the answer
// is internal/live's.
package archive

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gitdek/onair/internal/live"
)

// Door is On the Record's read-only door on this Mac.
const Door = "http://127.0.0.1:8797/mcp/readonly"

// protocol is the MCP version On Air asks for.
const protocol = "2025-06-18"

// Limits on what is read and sent.
const (
	maxAnswer = 4 << 20 // bytes of an answer
	maxText   = 50000   // characters of a question, find_connections' own limit
	perKind   = 5       // meetings, notebook entries and action items asked for
	maxTopic  = 2000    // characters of a topic, gather_context's own limit
	// briefItems is how many passages of the notes a brief is asked for:
	// enough to hold an answer, and few enough that the archive answers in
	// about two seconds.
	briefItems = 8
)

// Client asks On the Record. Several goroutines may use it at once.
type Client struct {
	url  string
	http *http.Client
	ids  atomic.Int64

	mu      sync.Mutex
	ready   bool   // the session was started
	session string // the session the server gave, if any
	version string // the protocol version it agreed to
}

// New returns a client for the door at address, which must be http or
// https to a loopback IP address, such as 127.0.0.1 or ::1. A name, even
// localhost, is refused, since it would have to be looked up.
func New(address string) (*Client, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("On the Record's address can't be read: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.User != nil || !loopback(u.Hostname()) {
		return nil, fmt.Errorf("On the Record's address must be a loopback address on this Mac, such as http://127.0.0.1:8797, not %s", u.Redacted())
	}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dial,
		TLSHandshakeTimeout: 2 * time.Second,
		MaxIdleConns:        2,
		IdleConnTimeout:     30 * time.Second,
	}
	return &Client{url: u.String(), http: &http.Client{
		Transport: transport,
		// A context sets each question's own time; this is a backstop.
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// dial connects to a loopback address and nothing else.
func dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || !loopback(host) {
		return nil, fmt.Errorf("On Air only asks On the Record on this Mac, not at %s", address)
	}
	return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, address)
}

func loopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Connections asks On the Record's find_connections what text connects
// to: earlier meetings, notebook entries and action items.
func (c *Client) Connections(ctx context.Context, text string) (live.Found, error) {
	if len(text) > maxText {
		text = text[len(text)-maxText:]
	}
	result, err := c.call(ctx, "find_connections", map[string]any{"text": text, "limit": perKind})
	if err != nil {
		return live.Found{}, err
	}
	var out found
	if err := decode(result, &out); err != nil {
		return live.Found{}, err
	}
	return out.live(), nil
}

// Answer asks On the Record's gather_context what it knows about a
// question, in plain words, for a brief of briefItems passages of the
// notes, with passages of the transcripts when transcript is true, and
// never the screen's moments.
func (c *Client) Answer(ctx context.Context, question string, transcript bool) (live.Brief, error) {
	result, err := c.call(ctx, "gather_context", map[string]any{"topic": topic(question), "max_items": briefItems,
		"include_transcript": transcript, "include_screen": false})
	if err != nil {
		return live.Brief{}, err
	}
	texts, _, err := content(result)
	if err != nil {
		return live.Brief{}, err
	}
	if len(texts) == 0 {
		return live.Brief{}, errors.New("On the Record's answer had no brief")
	}
	b := live.Brief{Points: points(texts[0])}
	if len(texts) > 1 {
		var counts struct {
			Passages int `json:"passages"`
		}
		if json.Unmarshal([]byte(texts[1]), &counts) == nil {
			b.Passages = counts.Passages
		}
	}
	return b, nil
}

// topic is a question as On the Record is asked about it: its words, with
// none of the marks its search reads as a filter, such as in:screen,
// person:name, @name, "a phrase" or -word, and maxTopic characters at most.
func topic(question string) string {
	words := strings.Fields(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:"“”„@`, r) {
			return ' '
		}
		return r
	}, question))
	for i, w := range words {
		words[i] = strings.TrimLeft(w, "-–—")
	}
	t := strings.Join(strings.Fields(strings.Join(words, " ")), " ")
	if r := []rune(t); len(r) > maxTopic {
		t = string(r[:maxTopic])
	}
	return t
}

// call calls a tool, starting a session first when there is none, and
// starting a new one, once, when the server has forgotten it.
func (c *Client) call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	for again := true; ; again = false {
		if err := c.start(ctx); err != nil {
			return nil, err
		}
		result, err := c.request(ctx, "tools/call", map[string]any{"name": tool, "arguments": args})
		if errors.Is(err, errForgotten) && again {
			c.forget()
			continue
		}
		return result, err
	}
}

var errForgotten = errors.New("On the Record no longer knows On Air's session")

// start starts a session, unless one is under way: initialize, and then
// the notification that On Air is ready.
func (c *Client) start(ctx context.Context) error {
	c.mu.Lock()
	ready := c.ready
	c.mu.Unlock()
	if ready {
		return nil
	}
	params := map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "On Air", "version": "0.1.0"}}
	result, header, err := c.post(ctx, message{JSONRPC: "2.0", ID: c.ids.Add(1), Method: "initialize", Params: params}, "", "")
	if err != nil {
		return err
	}
	var agreed struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(result, &agreed); err != nil {
		return fmt.Errorf("On the Record's greeting couldn't be read: %w", err)
	}
	session, version := header.Get("Mcp-Session-Id"), agreed.ProtocolVersion
	if version == "" {
		version = protocol
	}
	if _, _, err := c.post(ctx, message{JSONRPC: "2.0", Method: "notifications/initialized"}, session, version); err != nil {
		return err
	}
	c.mu.Lock()
	c.ready, c.session, c.version = true, session, version
	c.mu.Unlock()
	return nil
}

func (c *Client) forget() {
	c.mu.Lock()
	c.ready, c.session, c.version = false, "", ""
	c.mu.Unlock()
}

// request sends a request in the session and returns its result.
func (c *Client) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	session, version := c.session, c.version
	c.mu.Unlock()
	result, _, err := c.post(ctx, message{JSONRPC: "2.0", ID: c.ids.Add(1), Method: method, Params: params}, session, version)
	return result, err
}

// message is a JSON-RPC message, as sent and as answered.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// post sends one message and, for a request, reads its answer, as plain
// JSON or as an event stream. A notification's answer has no body.
func (c *Client) post(ctx context.Context, m message, session, version string) (json.RawMessage, http.Header, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, plain(ctx, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound && session != "":
		return nil, nil, errForgotten
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return nil, nil, fmt.Errorf("On the Record sent the question elsewhere (HTTP %d), and On Air doesn't follow", resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, nil, fmt.Errorf("On the Record answered HTTP %d", resp.StatusCode)
	}
	if m.ID == 0 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer))
		return nil, resp.Header, nil
	}
	answer, err := read(resp, m.ID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, plain(ctx, ctx.Err())
		}
		return nil, nil, err
	}
	if answer.Error != nil {
		return nil, nil, fmt.Errorf("On the Record refused the question: %s", clip(answer.Error.Message))
	}
	if len(answer.Result) == 0 {
		return nil, nil, errors.New("On the Record's answer had no result")
	}
	return answer.Result, resp.Header, nil
}

// read reads the answer to the request numbered id, from a JSON body or an
// event stream, and no more than maxAnswer bytes of either.
func read(resp *http.Response, id int64) (message, error) {
	limited := &io.LimitedReader{R: resp.Body, N: maxAnswer + 1}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media != "text/event-stream" {
		b, err := io.ReadAll(limited)
		if err != nil {
			return message{}, fmt.Errorf("On the Record's answer was cut off: %w", err)
		}
		if limited.N <= 0 {
			return message{}, errors.New("On the Record's answer was too long to read")
		}
		var m message
		if err := json.Unmarshal(b, &m); err != nil {
			return message{}, errors.New("On the Record's answer couldn't be read")
		}
		if m.ID != id {
			return message{}, errors.New("On the Record answered a different question")
		}
		return m, nil
	}
	// An event stream carries messages in data lines, and an empty line
	// ends each. The one that answers this request is the one wanted.
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64<<10), maxAnswer)
	var data strings.Builder
	for {
		more := scanner.Scan()
		text := scanner.Text()
		if !more || text == "" {
			if data.Len() > 0 {
				var m message
				if json.Unmarshal([]byte(data.String()), &m) == nil && m.ID == id && (m.Result != nil || m.Error != nil) {
					return m, nil
				}
				data.Reset()
			}
			if !more {
				break
			}
			continue
		}
		if rest, ok := strings.CutPrefix(text, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(rest, " "))
		}
	}
	if limited.N <= 0 {
		return message{}, errors.New("On the Record's answer was too long to read")
	}
	if err := scanner.Err(); err != nil {
		return message{}, fmt.Errorf("On the Record's answer was cut off: %w", err)
	}
	return message{}, errors.New("On the Record's answer ended without one")
}

// plain says why a question went unanswered, in words for the panel.
func plain(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded:
		return errors.New("On the Record didn't answer in time")
	case errors.Is(err, context.Canceled):
		return errors.New("the question was called off")
	case errors.Is(err, syscall.ECONNREFUSED):
		return errors.New("On the Record isn't answering on this Mac. Is its server running?")
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return errors.New("On the Record didn't answer in time")
	}
	return fmt.Errorf("On the Record couldn't be reached: %w", err)
}

// content reads a tool's result: the texts of its content, in order, and
// its structured content. A tool that failed says why in its text.
func content(result json.RawMessage) ([]string, json.RawMessage, error) {
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil, nil, errors.New("On the Record's answer couldn't be read")
	}
	var texts []string
	for _, c := range r.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	if r.IsError {
		why := "it gave no reason"
		if len(texts) > 0 && texts[0] != "" {
			why = texts[0]
		}
		return nil, nil, fmt.Errorf("On the Record couldn't answer: %s", clip(why))
	}
	return texts, r.Structured, nil
}

// decode reads a tool's result: its structured content, or else the JSON
// in its text.
func decode(result json.RawMessage, out any) error {
	texts, structured, err := content(result)
	if err != nil {
		return err
	}
	if len(structured) > 0 && string(structured) != "null" {
		if err := json.Unmarshal(structured, out); err != nil {
			return errors.New("On the Record's answer wasn't in the shape On Air reads")
		}
		return nil
	}
	text := ""
	if len(texts) > 0 {
		text = texts[0]
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return errors.New("On the Record's answer wasn't in the shape On Air reads")
	}
	return nil
}

// clip keeps what another program says short enough for the panel.
func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:199]) + "…"
	}
	return s
}
