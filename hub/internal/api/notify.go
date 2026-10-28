package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/notify"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// SendTest sends one short test message through a configured
// channel, once, without the retry a scheduled send gets. The UI
// settings page and POST /api/v1/notify/test share it. The error
// names the channel and the HTTP status; secrets stay in their
// files.
func SendTest(ctx context.Context, senders []notify.Sender, channel, user string, now time.Time) error {
	s := notify.ByName(senders, channel)
	if s == nil {
		return fmt.Errorf("%w: channel %s is not configured", store.ErrNotFound, channel)
	}
	text := fmt.Sprintf("jolitz hub test message, sent by %s at %s. The channel works.", user, now.UTC().Format("2006-01-02 15:04 UTC"))
	ctx, cancel := context.WithTimeout(ctx, notify.Timeout)
	defer cancel()
	return s.Send(ctx, notify.Message{Title: "jolitz hub test", Text: text})
}

// IsChannel reports whether name is one of the three channels.
func IsChannel(name string) bool {
	return name == "ntfy" || name == "matrix" || name == "webhook"
}

// notifyTest is POST /api/v1/notify/test {channel}, admin only.
func (s *server) notifyTest(w http.ResponseWriter, r *http.Request, u *store.User) {
	var in struct {
		Channel string `json:"channel"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !IsChannel(in.Channel) {
		fail(w, http.StatusBadRequest, "channel is ntfy, matrix or webhook")
		return
	}
	senders := notify.FromConfig(s.settings().Notify)
	if notify.ByName(senders, in.Channel) == nil {
		fail(w, http.StatusNotFound, "channel "+in.Channel+" is not configured in hub.toml")
		return
	}
	log.Printf("api: %s sends a test message via %s", u.Name, in.Channel)
	if err := SendTest(r.Context(), senders, in.Channel, u.Name, s.cfg.Store.Now()); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "channel": in.Channel})
}
