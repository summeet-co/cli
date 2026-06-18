package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chatwoot/cli/internal/sdk"
)

type SearchCmd struct {
	Query  string `arg:"" help:"Search query."`
	Page   int    `short:"p" default:"1" help:"Page number (applies to all result buckets)."`
	Only   string `help:"Restrict to one bucket: conversations, contacts, messages, or articles." enum:",conversations,contacts,messages,articles" default:""`
	After  string `help:"Lower bound (inclusive). Date 2006-01-02, datetime 2006-01-02T15:04, RFC3339, or epoch seconds. Bare dates use --tz."`
	Before string `help:"Upper bound (inclusive). Same formats as --after."`
	On     string `help:"Single day shorthand: --after 00:00:00 + --before 23:59:59 of this date. Mutually exclusive with --after/--before."`
	TZ     string `name:"tz" default:"UTC" help:"Timezone for interpreting bare dates in --after/--before/--on (default UTC; accepts IANA names like Australia/Sydney). Epoch seconds and RFC3339 'Z' timestamps are always UTC."`
}

func (c *SearchCmd) Run(app *App) error {
	since, until, err := c.timeBounds()
	if err != nil {
		return err
	}

	resp, err := app.Client.Search().Global(sdk.SearchOptions{
		Query: c.Query,
		Page:  c.Page,
		Since: since,
		Until: until,
	})
	if err != nil {
		return err
	}

	if app.Printer.Format == "json" && !app.Printer.Quiet {
		app.Printer.PrintJSON(payloadOnly(resp, c.Only))
		return nil
	}

	if app.Printer.Format == "csv" {
		if c.Only == "" {
			return fmt.Errorf("csv output needs a single bucket; use --only=<bucket> or -o json")
		}
		return printSearchSection(app, resp, c.Only, false)
	}

	if app.Printer.Quiet {
		return printSearchQuiet(app, resp, c.Only)
	}

	return printSearchText(app, c.Query, resp, c.Only)
}

func printSearchText(app *App, query string, resp *sdk.SearchResponse, only string) error {
	p := resp.Payload
	total := len(p.Conversations) + len(p.Contacts) + len(p.Messages) + len(p.Articles)

	if only != "" {
		return printSearchSection(app, resp, only, false)
	}

	if total == 0 {
		_, _ = fmt.Fprintf(app.Printer.Writer, "No results for %q.\n", query)
		return nil
	}

	first := true
	if len(p.Conversations) > 0 {
		first = printSectionHeader(app, first, "CONVERSATIONS")
		printConversationRows(app, p.Conversations)
	}
	if len(p.Contacts) > 0 {
		first = printSectionHeader(app, first, "CONTACTS")
		printContactRows(app, p.Contacts)
	}
	if len(p.Messages) > 0 {
		first = printSectionHeader(app, first, "MESSAGES")
		printMessageRows(app, p.Messages)
	}
	if len(p.Articles) > 0 {
		_ = printSectionHeader(app, first, "ARTICLES")
		printArticleRows(app, p.Articles)
	}
	return nil
}

// payloadOnly returns a response restricted to a single bucket so that --only is
// honored consistently across every output format (text, csv, quiet, and json).
// An empty bucket returns the response unchanged.
func payloadOnly(resp *sdk.SearchResponse, only string) *sdk.SearchResponse {
	if only == "" {
		return resp
	}
	out := &sdk.SearchResponse{}
	switch only {
	case "conversations":
		out.Payload.Conversations = resp.Payload.Conversations
	case "contacts":
		out.Payload.Contacts = resp.Payload.Contacts
	case "messages":
		out.Payload.Messages = resp.Payload.Messages
	case "articles":
		out.Payload.Articles = resp.Payload.Articles
	}
	return out
}

func printSearchSection(app *App, resp *sdk.SearchResponse, only string, prefixIDs bool) error {
	_ = prefixIDs // reserved for future use; quiet mode handles prefixing separately
	// In CSV mode an empty bucket must still emit a header row (valid CSV) rather
	// than the human-readable "No X found." message, so scripts get parseable output.
	prose := app.Printer.Format != "csv"
	switch only {
	case "conversations":
		if len(resp.Payload.Conversations) == 0 && prose {
			_, _ = fmt.Fprintln(app.Printer.Writer, "No conversations found.")
			return nil
		}
		printConversationRows(app, resp.Payload.Conversations)
	case "contacts":
		if len(resp.Payload.Contacts) == 0 && prose {
			_, _ = fmt.Fprintln(app.Printer.Writer, "No contacts found.")
			return nil
		}
		printContactRows(app, resp.Payload.Contacts)
	case "messages":
		if len(resp.Payload.Messages) == 0 && prose {
			_, _ = fmt.Fprintln(app.Printer.Writer, "No messages found.")
			return nil
		}
		printMessageRows(app, resp.Payload.Messages)
	case "articles":
		if len(resp.Payload.Articles) == 0 && prose {
			_, _ = fmt.Fprintln(app.Printer.Writer, "No articles found.")
			return nil
		}
		printArticleRows(app, resp.Payload.Articles)
	default:
		return fmt.Errorf("unknown bucket: %q", only)
	}
	return nil
}

func printSearchQuiet(app *App, resp *sdk.SearchResponse, only string) error {
	w := app.Printer.Writer
	p := resp.Payload

	if only != "" {
		switch only {
		case "conversations":
			for _, c := range p.Conversations {
				_, _ = fmt.Fprintln(w, c.ID)
			}
		case "contacts":
			for _, c := range p.Contacts {
				_, _ = fmt.Fprintln(w, c.ID)
			}
		case "messages":
			for _, m := range p.Messages {
				_, _ = fmt.Fprintln(w, m.ID)
			}
		case "articles":
			for _, a := range p.Articles {
				_, _ = fmt.Fprintln(w, a.ID)
			}
		default:
			return fmt.Errorf("unknown bucket: %q", only)
		}
		return nil
	}

	for _, c := range p.Conversations {
		_, _ = fmt.Fprintf(w, "conversation:%d\n", c.ID)
	}
	for _, c := range p.Contacts {
		_, _ = fmt.Fprintf(w, "contact:%d\n", c.ID)
	}
	for _, m := range p.Messages {
		_, _ = fmt.Fprintf(w, "message:%d\n", m.ID)
	}
	for _, a := range p.Articles {
		_, _ = fmt.Fprintf(w, "article:%d\n", a.ID)
	}
	return nil
}

func printSectionHeader(app *App, first bool, title string) bool {
	if !first {
		_, _ = fmt.Fprintln(app.Printer.Writer)
	}
	_, _ = fmt.Fprintln(app.Printer.Writer, title)
	return false
}

func printConversationRows(app *App, convs []sdk.ConversationSearchResult) {
	headers := []string{"ID", "Contact", "Inbox", "Channel", "Created"}
	rows := make([][]string, 0, len(convs))
	for _, c := range convs {
		contact := ""
		if c.Contact != nil {
			contact = c.Contact.Name
		}
		inboxName := ""
		channel := ""
		if c.Inbox != nil {
			inboxName = c.Inbox.Name
			channel = c.Inbox.ChannelType
		}
		rows = append(rows, []string{
			strconv.Itoa(c.ID),
			contact,
			inboxName,
			channel,
			formatTimestamp(c.CreatedAt),
		})
	}
	app.Printer.PrintTable(headers, rows)
}

func printContactRows(app *App, contacts []sdk.ContactSearchResult) {
	headers := []string{"ID", "Name", "Email", "Phone", "Last Activity"}
	rows := make([][]string, 0, len(contacts))
	for _, c := range contacts {
		rows = append(rows, []string{
			strconv.Itoa(c.ID),
			c.Name,
			c.Email,
			c.PhoneNumber,
			formatTimestamp(c.LastActivityAt),
		})
	}
	app.Printer.PrintTable(headers, rows)
}

func printMessageRows(app *App, messages []sdk.MessageSearchResult) {
	headers := []string{"ID", "Conv", "Sender", "Time", "Content"}
	rows := make([][]string, 0, len(messages))
	for _, m := range messages {
		sender := ""
		if m.Sender != nil {
			sender = m.Sender.Name
		}
		content := truncate(sanitizeCell(m.Content), 60)
		rows = append(rows, []string{
			strconv.Itoa(m.ID),
			strconv.Itoa(m.ConversationID),
			sender,
			formatTimestamp(m.CreatedAt),
			content,
		})
	}
	app.Printer.PrintTable(headers, rows)
}

func printArticleRows(app *App, articles []sdk.ArticleSearchResult) {
	headers := []string{"ID", "Title", "Locale", "Status", "Updated"}
	rows := make([][]string, 0, len(articles))
	for _, a := range articles {
		rows = append(rows, []string{
			strconv.Itoa(a.ID),
			truncate(sanitizeCell(a.Title), 60),
			a.Locale,
			a.Status,
			formatTimestamp(a.UpdatedAt),
		})
	}
	app.Printer.PrintTable(headers, rows)
}

// sanitizeCell collapses newlines, tabs, and carriage returns into single
// spaces so that tabwriter alignment stays intact.
func sanitizeCell(s string) string {
	r := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

// timeBounds resolves --after/--before/--on into epoch-second bounds passed to
// the Chatwoot search API (which filters server-side).
func (c *SearchCmd) timeBounds() (since, until int64, err error) {
	if c.After == "" && c.Before == "" && c.On == "" {
		return 0, 0, nil
	}
	loc, err := time.LoadLocation(c.TZ)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid --tz %q: %w", c.TZ, err)
	}
	if c.On != "" {
		if c.After != "" || c.Before != "" {
			return 0, 0, fmt.Errorf("--on is mutually exclusive with --after/--before")
		}
		if since, err = parseSearchTime(c.On, loc, false); err != nil {
			return 0, 0, err
		}
		if until, err = parseSearchTime(c.On, loc, true); err != nil {
			return 0, 0, err
		}
	} else {
		if c.After != "" {
			if since, err = parseSearchTime(c.After, loc, false); err != nil {
				return 0, 0, err
			}
		}
		if c.Before != "" {
			if until, err = parseSearchTime(c.Before, loc, true); err != nil {
				return 0, 0, err
			}
		}
	}
	if since > 0 && until > 0 && since > until {
		return 0, 0, fmt.Errorf("--after is later than --before")
	}
	if cutoff := time.Now().Add(-90 * 24 * time.Hour).Unix(); until > 0 && until < cutoff {
		_, _ = fmt.Fprintln(os.Stderr, "warning: window is entirely older than ~90 days; Chatwoot search is server-capped to the last ~90 days, results may be empty or widened.")
	}
	return since, until, nil
}

// parseSearchTime converts a CLI date/time string to epoch seconds.
// Accepts epoch seconds (all digits), RFC3339 (explicit zone), zone-less
// datetimes interpreted in loc, and bare dates. For a bare date, endOfDay
// selects 23:59:59 (upper bound) instead of 00:00:00 (lower bound).
func parseSearchTime(s string, loc *time.Location, endOfDay bool) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if isAllDigits(s) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid epoch %q: %w", s, err)
		}
		return n, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix(), nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.Unix(), nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t.Unix(), nil
	}
	return 0, fmt.Errorf("could not parse time %q (use 2006-01-02, 2006-01-02T15:04, RFC3339, or epoch seconds)", s)
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
