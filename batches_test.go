package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/responses"
)

// Exercise the real IMAP client and command serialization without a mail account.
func TestBatchIMAPCommands(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	local.SetDeadline(time.Now().Add(5 * time.Second))
	remote.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan error, 1)
	go func() {
		if _, err := fmt.Fprint(remote, "* PREAUTH [CAPABILITY IMAP4rev1 UIDPLUS] Ready\r\n"); err != nil {
			done <- err
			return
		}
		reader := bufio.NewReader(remote)
		for _, want := range []string{"SELECT INBOX", "UID COPY 101:110 \"Trash\"", "UID STORE 101:110 +FLAGS.SILENT (\\Deleted)", "UID EXPUNGE 101:110"} {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- err
				return
			}
			tag, command, _ := strings.Cut(strings.TrimSpace(line), " ")
			if command != want {
				done <- fmt.Errorf("wire command = %q, want %q", command, want)
				remote.Close()
				return
			}
			prefix := ""
			if strings.HasPrefix(want, "SELECT") {
				prefix = "* 20 EXISTS\r\n* OK [UIDVALIDITY 123] Valid UIDs\r\n"
			} else if strings.HasPrefix(want, "UID EXPUNGE") {
				prefix = "* 1 EXPUNGE\r\n"
			}
			if _, err := fmt.Fprintf(remote, "%s%s OK Done\r\n", prefix, tag); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	conn, err := client.New(local)
	if err != nil {
		t.Fatal(err)
	}
	m, _, _, _ := newTestMover(t)
	m.conn = conn
	if err := m.move(testChunks()[:1]); err != nil {
		t.Fatalf("%v; server: %v", err, <-done)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type fakeBatchClient struct {
	t         *testing.T
	events    *[]string
	validity  uint32
	noUIDPlus bool
	fail      string
	advance   func()
}

func (f *fakeBatchClient) record(event string) error {
	*f.events = append(*f.events, event)
	if event == f.fail {
		return errors.New("connection closed")
	}
	return nil
}

func (f *fakeBatchClient) Select(folder string, readonly bool) (*imap.MailboxStatus, error) {
	if folder != SelectFolder || readonly {
		f.t.Fatalf("unexpected selection: %s, %v", folder, readonly)
	}
	return &imap.MailboxStatus{UidValidity: f.validity}, f.record("select")
}

func (f *fakeBatchClient) Support(capability string) (bool, error) {
	if capability != "UIDPLUS" {
		f.t.Fatalf("unexpected capability: %s", capability)
	}
	return !f.noUIDPlus, f.record("capability")
}

func (f *fakeBatchClient) UidCopy(set *imap.SeqSet, dest string) error {
	if dest != TrashFolder {
		f.t.Fatalf("unexpected destination: %s", dest)
	}
	return f.record("copy " + set.String())
}

func (f *fakeBatchClient) UidStore(set *imap.SeqSet, item imap.StoreItem, value interface{}, messages chan *imap.Message) error {
	if item != imap.FormatFlagsOp(imap.AddFlags, true) || !reflect.DeepEqual(value, []interface{}{imap.DeletedFlag}) || messages != nil {
		f.t.Fatalf("unexpected flag update: %s, %v", item, value)
	}
	return f.record("delete " + set.String())
}

func (f *fakeBatchClient) Execute(command imap.Commander, handler responses.Handler) (*imap.StatusResp, error) {
	cmd := command.Command()
	if cmd.Name != "UID" || len(cmd.Arguments) != 2 || cmd.Arguments[0] != imap.RawString("EXPUNGE") {
		f.t.Fatalf("expected UID EXPUNGE, got %#v", cmd)
	}
	set, ok := cmd.Arguments[1].(*imap.SeqSet)
	if !ok {
		f.t.Fatalf("expected UID set, got %T", cmd.Arguments[1])
	}
	err := f.record("expunge " + set.String())
	if err == nil && f.advance != nil {
		f.advance()
	}
	return &imap.StatusResp{Type: imap.StatusRespOk}, err
}

func newTestMover(t *testing.T) (*batchMover, *fakeBatchClient, *[]string, *time.Time) {
	t.Helper()
	events := []string{}
	now := time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC)
	conn := &fakeBatchClient{t: t, events: &events, validity: 123}
	mover := &batchMover{
		conn: conn, connectedAt: now, uidValidity: 123,
		now:   func() time.Time { return now },
		pause: func(d time.Duration) { now = now.Add(d) },
		reconnect: func() (batchClient, time.Time, error) {
			events = append(events, "reconnect")
			return conn, now, nil
		},
	}
	return mover, conn, &events, &now
}

func testChunks() []*imap.SeqSet {
	set := new(imap.SeqSet)
	set.AddRange(101, 120)
	return SplitSequenceSet(set, 10)
}

func TestMoveCompletesEachBatchBeforeNextCopy(t *testing.T) {
	m, _, events, _ := newTestMover(t)
	if err := m.move(testChunks()); err != nil {
		t.Fatal(err)
	}
	want := []string{"select", "capability", "copy 101:110", "delete 101:110", "expunge 101:110", "copy 111:120", "delete 111:120", "expunge 111:120"}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func TestMoveStopsWithoutRetryOnFailure(t *testing.T) {
	for _, stage := range []string{"copy", "delete", "expunge"} {
		t.Run(stage, func(t *testing.T) {
			m, conn, events, _ := newTestMover(t)
			conn.fail = stage + " 111:120"
			err := m.move(testChunks())
			if err == nil || !strings.Contains(err.Error(), "10 messages completed in earlier batches") {
				t.Fatalf("unexpected error: %v", err)
			}
			if last := (*events)[len(*events)-1]; last != conn.fail {
				t.Fatalf("continued after failure: %v", *events)
			}
			if strings.Count(strings.Join(*events, ","), conn.fail) != 1 || strings.Contains(strings.Join(*events, ","), "reconnect") {
				t.Fatalf("retried failed operation: %v", *events)
			}
		})
	}
}

func TestMoveRefreshesAtFourMinutesBetweenBatches(t *testing.T) {
	m, conn, events, now := newTestMover(t)
	conn.advance = func() { *now = now.Add(238 * time.Second) }
	if err := m.move(testChunks()); err != nil {
		t.Fatal(err)
	}
	want := []string{"select", "capability", "copy 101:110", "delete 101:110", "expunge 101:110", "reconnect", "select", "capability", "copy 111:120", "delete 111:120", "expunge 111:120"}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

func TestMoveIncludesScanTimeInConnectionAge(t *testing.T) {
	for _, age := range []time.Duration{connectionRefreshAge - time.Second, connectionRefreshAge, connectionRefreshAge + time.Second} {
		t.Run(age.String(), func(t *testing.T) {
			m, _, events, now := newTestMover(t)
			*now = now.Add(age)
			if err := m.move(testChunks()[:1]); err != nil {
				t.Fatal(err)
			}
			got := strings.Contains(strings.Join(*events, ","), "reconnect")
			if got != (age >= connectionRefreshAge) {
				t.Fatalf("unexpected refresh at %s: %v", age, *events)
			}
		})
	}
}

func TestMoveUsesNewConnectionAndResetsAge(t *testing.T) {
	m, old, events, now := newTestMover(t)
	old.advance = func() { *now = now.Add(238 * time.Second) }
	reconnects := 0
	m.reconnect = func() (batchClient, time.Time, error) {
		reconnects++
		// Any further use of the old connection must fail this test.
		old.fail = "copy 111:120"
		fresh := &fakeBatchClient{t: t, events: events, validity: 123}
		return fresh, *now, nil
	}
	set := new(imap.SeqSet)
	set.AddRange(101, 130)
	if err := m.move(SplitSequenceSet(set, 10)); err != nil {
		t.Fatal(err)
	}
	if reconnects != 1 {
		t.Fatalf("reconnected %d times, want 1", reconnects)
	}
}

func TestMoveStopsOnUnsafeSession(t *testing.T) {
	for _, scenario := range []string{"missing UIDPLUS", "initial UIDVALIDITY mismatch", "reconnect UIDVALIDITY mismatch", "reconnect failed"} {
		t.Run(scenario, func(t *testing.T) {
			m, conn, events, now := newTestMover(t)
			switch scenario {
			case "missing UIDPLUS":
				conn.noUIDPlus = true
			case "initial UIDVALIDITY mismatch":
				conn.validity++
			default:
				*now = now.Add(connectionRefreshAge)
				m.reconnect = func() (batchClient, time.Time, error) {
					*events = append(*events, "reconnect")
					if scenario == "reconnect failed" {
						return nil, time.Time{}, fmt.Errorf("logout failed")
					}
					conn.validity++
					return conn, *now, nil
				}
			}
			if err := m.move(testChunks()); err == nil {
				t.Fatal("expected failure")
			}
			if strings.Contains(strings.Join(*events, ","), "copy") {
				t.Fatalf("copied despite unsafe session: %v", *events)
			}
		})
	}
}
