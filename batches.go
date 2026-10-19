package main

import (
	"fmt"
	"log"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/responses"
)

const connectionRefreshAge = 4 * time.Minute

type batchClient interface {
	Select(string, bool) (*imap.MailboxStatus, error)
	Support(string) (bool, error)
	UidCopy(*imap.SeqSet, string) error
	UidStore(*imap.SeqSet, imap.StoreItem, interface{}, chan *imap.Message) error
	Execute(imap.Commander, responses.Handler) (*imap.StatusResp, error)
}

type batchMover struct {
	conn        batchClient
	connectedAt time.Time
	uidValidity uint32
	now         func() time.Time
	pause       func(time.Duration)
	reconnect   func() (batchClient, time.Time, error)
}

// prepare checks that saved UIDs still identify the original inbox messages and
// that expunging can be restricted to this batch, leaving other deleted mail alone.
func (m *batchMover) prepare() error {
	mbox, err := m.conn.Select(SelectFolder, false)
	if err != nil {
		return fmt.Errorf("select %s: %w", SelectFolder, err)
	}
	if m.uidValidity == 0 || mbox.UidValidity != m.uidValidity {
		return fmt.Errorf("%s UIDVALIDITY changed or missing (expected %d, got %d); stopping before using saved UIDs", SelectFolder, m.uidValidity, mbox.UidValidity)
	}
	ok, err := m.conn.Support("UIDPLUS")
	if err != nil {
		return fmt.Errorf("check UIDPLUS support: %w", err)
	}
	if !ok {
		return fmt.Errorf("server does not support UIDPLUS; cannot safely expunge only this batch")
	}
	return nil
}

func (m *batchMover) move(chunks []*imap.SeqSet) error {
	if err := m.prepare(); err != nil {
		return err
	}
	completed := uint32(0)
	for i, chunk := range chunks {
		// Also check before the first batch: scanning the inbox uses session time.
		if age := m.now().Sub(m.connectedAt); age >= connectionRefreshAge {
			log.Printf("Connection age %s reached four minutes; reconnecting before chunk %d.", age.Round(time.Second), i+1)
			conn, started, err := m.reconnect()
			if err != nil {
				return fmt.Errorf("planned reconnect before chunk %d: %w", i+1, err)
			}
			m.conn, m.connectedAt = conn, started
			if err := m.prepare(); err != nil {
				return fmt.Errorf("after reconnect: %w", err)
			}
		}
		started := m.now()
		log.Printf("Processing chunk %d/%d: %s", i+1, len(chunks), chunk)
		if err := m.conn.UidCopy(chunk, TrashFolder); err != nil {
			return fmt.Errorf("chunk %d copy to %s failed (%s): %w; copy outcome may be uncertain, no retry attempted; %d messages completed in earlier batches", i+1, TrashFolder, chunk, err, completed)
		}
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		if err := m.conn.UidStore(chunk, item, []interface{}{imap.DeletedFlag}, nil); err != nil {
			return fmt.Errorf("chunk %d copied but marking originals deleted failed (%s): %w; no retry attempted; %d messages completed in earlier batches", i+1, chunk, err, completed)
		}
		// Plain EXPUNGE would also delete unrelated messages marked by other clients.
		cmd := &imap.Command{Name: "UID", Arguments: []interface{}{imap.RawString("EXPUNGE"), chunk}}
		status, err := m.conn.Execute(cmd, nil)
		if err == nil {
			err = status.Err()
		}
		if err != nil {
			return fmt.Errorf("chunk %d copied but expunging originals failed (%s): %w; no retry attempted; %d messages completed in earlier batches", i+1, chunk, err, completed)
		}
		for _, seq := range chunk.Set {
			completed += seq.Stop - seq.Start + 1
		}
		log.Printf("Processed chunk %d: %s; copied and originals removed in %s (%d messages completed).", i+1, chunk, m.now().Sub(started).Round(time.Millisecond), completed)
		if i+1 < len(chunks) {
			m.pause(2 * time.Second)
		}
	}
	log.Printf("%d emails moved to trash successfully.", completed)
	return nil
}
