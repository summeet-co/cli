package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chatwoot/cli/internal/config"
)

// canned 4-bucket /search response with deliberately distinct values per bucket
// so --only assertions can tell buckets apart.
const searchPayloadJSON = `{
	"payload": {
		"conversations": [{
			"id": 462,
			"account_id": 1,
			"created_at": 1700000000,
			"contact": {"id": 5, "name": "Ada Lovelace"},
			"inbox": {"id": 2, "name": "Support Email", "channel_type": "Channel::Email"}
		}],
		"contacts": [{"id": 5, "name": "Ada Lovelace", "email": "ada@example.com", "phone_number": "+100", "last_activity_at": 1700000000}],
		"messages": [{"id": 9, "content": "refund question", "conversation_id": 462, "created_at": 1700000000, "sender": {"name": "Bob Agent"}}],
		"articles": [{"id": 3, "title": "Billing guide", "locale": "en", "status": 1, "updated_at": 1700000000}]
	}
}`

// newSearchTestApp wires an App against an httptest server returning the canned
// payload, and lets the test inspect the request the command made.
func newSearchTestApp(t *testing.T, cli *CLI, inspect func(*http.Request)) (*App, *bytes.Buffer) {
	t.Helper()
	setupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/1/search" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if inspect != nil {
			inspect(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchPayloadJSON))
	}))
	t.Cleanup(server.Close)

	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(cli, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	out := &bytes.Buffer{}
	app.Printer.Writer = out
	return app, out
}

func TestSearchRendersAllSections(t *testing.T) {
	app, out := newSearchTestApp(t, &CLI{Output: "text"}, nil)

	if err := (&SearchCmd{Query: "ada", Page: 1}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{
		"CONVERSATIONS", "CONTACTS", "MESSAGES", "ARTICLES",
		"Ada Lovelace", "ada@example.com", "Support Email", "refund question", "Bob Agent", "Billing guide",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestSearchOnlyFiltersToSingleBucket(t *testing.T) {
	app, out := newSearchTestApp(t, &CLI{Output: "text"}, nil)

	if err := (&SearchCmd{Query: "refund", Only: "messages", Page: 1}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "refund question") {
		t.Fatalf("--only messages should show the message:\n%s", got)
	}
	for _, absent := range []string{"CONVERSATIONS", "Billing guide", "ada@example.com"} {
		if strings.Contains(got, absent) {
			t.Fatalf("--only messages should not show %q:\n%s", absent, got)
		}
	}
}

func TestSearchOnlyRestrictsJSONToBucket(t *testing.T) {
	app, out := newSearchTestApp(t, &CLI{Output: "json"}, nil)

	if err := (&SearchCmd{Query: "refund", Only: "messages", Page: 1}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got struct {
		Payload struct {
			Conversations []json.RawMessage `json:"conversations"`
			Contacts      []json.RawMessage `json:"contacts"`
			Messages      []json.RawMessage `json:"messages"`
			Articles      []json.RawMessage `json:"articles"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if len(got.Payload.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Payload.Messages))
	}
	if len(got.Payload.Conversations) != 0 || len(got.Payload.Contacts) != 0 || len(got.Payload.Articles) != 0 {
		t.Fatalf("--only messages must drop other buckets in JSON too: %#v", got.Payload)
	}
}

func TestSearchJSONOutput(t *testing.T) {
	app, out := newSearchTestApp(t, &CLI{Output: "json"}, nil)

	if err := (&SearchCmd{Query: "ada", Page: 1}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got struct {
		Payload struct {
			Conversations []struct {
				ID int `json:"id"`
			} `json:"conversations"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if len(got.Payload.Conversations) != 1 || got.Payload.Conversations[0].ID != 462 {
		t.Fatalf("unexpected JSON payload: %#v", got)
	}
}

func TestSearchQuietPrintsPrefixedIDs(t *testing.T) {
	app, out := newSearchTestApp(t, &CLI{Output: "text", Quiet: true}, nil)

	if err := (&SearchCmd{Query: "ada", Page: 1}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"conversation:462", "contact:5", "message:9", "article:3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("quiet output missing %q:\n%s", want, out.String())
		}
	}
}

func TestSearchNoResults(t *testing.T) {
	setupTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"payload": {"conversations": [], "contacts": [], "messages": [], "articles": []}}`))
	}))
	t.Cleanup(server.Close)
	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	out := &bytes.Buffer{}
	app.Printer.Writer = out

	if err := (&SearchCmd{Query: "nothing", Page: 1}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), `No results for "nothing"`) {
		t.Fatalf("unexpected no-results output: %s", out.String())
	}
}

func TestSearchDateRangeSendsSinceAndUntil(t *testing.T) {
	var since, until string
	app, _ := newSearchTestApp(t, &CLI{Output: "json"}, func(r *http.Request) {
		since = r.URL.Query().Get("since")
		until = r.URL.Query().Get("until")
	})

	cmd := &SearchCmd{Query: "ada", Page: 1, After: "2026-06-01", Before: "2026-06-02", TZ: "UTC"}
	if err := cmd.Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// bare --after -> 00:00:00 of the day; bare --before -> 23:59:59 of the day, in --tz.
	wantSince := strconv.FormatInt(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Unix(), 10)
	wantUntil := strconv.FormatInt(time.Date(2026, 6, 2, 23, 59, 59, 0, time.UTC).Unix(), 10)
	if since != wantSince {
		t.Errorf("since = %q, want %q (2026-06-01Z start)", since, wantSince)
	}
	if until != wantUntil {
		t.Errorf("until = %q, want %q (2026-06-02Z end)", until, wantUntil)
	}
}
