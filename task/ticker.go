package task

import (
	"github.com/casbin/casvisor/object"
	"time"
)

type Ticker struct {
}

func NewTicker() *Ticker {
	return &Ticker{}
}

func (t *Ticker) SetupTicker() {
	// delete unused session every hour
	unUsedSessionTicker := time.NewTicker(time.Hour)
	go func() {
		for range unUsedSessionTicker.C {
			t.deleteUnUsedSession()
		}
	}()
}

func (t *Ticker) deleteUnUsedSession() {
	sessions, err := object.GetSessionsByStatus([]string{object.NoConnect, object.Connecting})
	if err != nil {
		return
	}

	if len(sessions) > 0 {
		now := time.Now()
		for i := range sessions {
			if sessions[i].ConnectedTime != "" {
				connectedTime, err := time.ParseInLocation(time.RFC3339, sessions[i].ConnectedTime, time.Local)
				if err != nil {
					continue
				}
				if now.Sub(connectedTime).Hours() > 1 {
					object.DeleteSessionById(sessions[i].GetId())
				}
			} else {
				object.DeleteSessionById(sessions[i].GetId())
			}
		}
	}
}
