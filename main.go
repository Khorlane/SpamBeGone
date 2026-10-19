package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

var (
	// Configuration fields to be set from JSON config file
	server   = ""
	email    = ""
	password = ""
	// Global variables
	c              *client.Client
	connectedAt    time.Time
	mailbox        *imap.MailboxStatus
	MatchingEmails []Email
	// Constants
	SelectFolder = "INBOX"
	TrashFolder  = "Trash"
	// Switches
	DoMoveToTrash = true
	ShowMailboxes = true

	// Whitelist entries: email addresses, domains, or wildcard domain suffixes
	Whitelist []string
)

// Config struct for JSON configuration
var Config struct {
	Server   string `json:"server"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Define the Email struct
type Email struct {
	UID          uint32
	From         string
	Subject      string
	InternalDate string
}

func main() {
	if err := run(); err != nil {
		log.Printf("FATAL: %v", err)
		os.Exit(1)
	}
}

func run() (err error) {
	defer func() {
		if closeErr := CloseConnection(); err == nil {
			err = closeErr
		}
	}()
	fmt.Println("SpamBeGone v0.3")
	LoadWhitelist()
	LoadConfig()
	if err := ConnectLogin(); err != nil {
		return err
	}
	VerifyFolderAccess()
	ListMailboxes()
	SelectMailbox()
	FetchAndStoreEmails()
	ListMatchingEmails()
	return MoveToTrash()
}

// Read the whitelist from Whitelist.txt
func LoadWhitelist() {
	file, err := os.Open("Whitelist.txt")
	if err != nil {
		log.Fatalf("failed to load whitelist: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		line = strings.ToLower(line)
		Whitelist = append(Whitelist, line)
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("error reading whitelist: %v", err)
	}
}

// Load configuration from config.json
func LoadConfig() {
	fmt.Println("*** LoadConfig ***")
	configFile, err := os.Open("Config.json")
	if err != nil {
		log.Fatalf("failed to open Config.json: %v", err)
	}
	defer configFile.Close()
	if err := json.NewDecoder(configFile).Decode(&Config); err != nil {
		log.Fatalf("failed to parse Config.json: %v", err)
	}
	server = Config.Server
	email = Config.Email
	password = Config.Password
}

// Connect to the server and login
func ConnectLogin() error {
	fmt.Println("*** ConnectLogin ***")
	var err error
	c, err = client.DialTLS(server, nil)
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	connectedAt = time.Now()
	if err := c.Login(email, password); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	fmt.Println("Connected and logged in successfully")
	return nil
}

// List all available mailboxes
func ListMailboxes() {
	fmt.Println("*** ListMailboxes ***")
	if !ShowMailboxes {
		return
	}
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", mailboxes)
	}()
	fmt.Println("Available mailboxes:")
	for m := range mailboxes {
		fmt.Println("-", m.Name)
	}
	if err := <-done; err != nil {
		fmt.Printf("failed to list mailboxes: %v\n", err)
		os.Exit(1)
	}
}

// Select the specified mailbox and checks for messages
func SelectMailbox() {
	fmt.Println("*** SelectMailbox ***")
	mbox, err := c.Select(SelectFolder, false)
	if err != nil {
		fmt.Printf("failed to select mailbox %s: %v\n", SelectFolder, err)
		os.Exit(1)
	}
	if mbox.Messages == 0 {
		fmt.Println("No messages in the mailbox.")
		os.Exit(0)
	}
	fmt.Printf("Flags for %s: %v\n", SelectFolder, mbox.Flags)
	mailbox = mbox
}

// Fetch and store messages from valid senders outside the whitelist
func FetchAndStoreEmails() {
	fmt.Println("*** FetchAndStoreEmails ***")
	seqset := new(imap.SeqSet)
	seqset.AddRange(1, mailbox.Messages)

	messages := make(chan *imap.Message, mailbox.Messages)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchUid, imap.FetchInternalDate, imap.FetchEnvelope}, messages)
	}()
	for msg := range messages {
		emailAddress, fromDomain, ok := BuildFromEmailAddress(msg)
		if !ok || IsWhitelisted(emailAddress, fromDomain) {
			continue
		}
		from := emailAddress
		personalName := msg.Envelope.From[0].PersonalName
		if personalName != "" {
			from = fmt.Sprintf("%s <%s>", personalName, emailAddress)
		}
		// Add the email to the global in-memory data structure
		MatchingEmails = append(MatchingEmails, Email{
			UID:          msg.Uid,
			From:         from,
			Subject:      msg.Envelope.Subject,
			InternalDate: msg.InternalDate.Format("2006-01-02 15:04:05"),
		})
	}
	// Check for fetch errors
	if err := <-done; err != nil {
		log.Fatal(err)
	}
}

// Sort MatchingEmails by InternalDate ascending
func SortEmails() {
	sort.SliceStable(MatchingEmails, func(i, j int) bool {
		dateI, _ := time.Parse("2006-01-02 15:04:05", MatchingEmails[i].InternalDate)
		dateJ, _ := time.Parse("2006-01-02 15:04:05", MatchingEmails[j].InternalDate)
		return dateI.Before(dateJ)
	})
}

// List matching emails
func ListMatchingEmails() {
	fmt.Println("Matching Emails:")
	SortEmails()
	for _, email := range MatchingEmails {
		fmt.Printf("UID: %d, From: %s, Subject: %s, InternalDate: %s\n", email.UID, email.From, email.Subject, email.InternalDate)
	}
}

// Move emails in the Email struct from the INBOX to the TrashFolder
func MoveToTrash() error {
	if !DoMoveToTrash {
		fmt.Println("DoMoveToTrash is disabled. Skipping MoveToTrash.")
		return nil
	}
	fmt.Println("*** MoveToTrash ***")
	if len(MatchingEmails) == 0 {
		fmt.Println("No emails to move to trash.")
		return nil
	}
	seqset := new(imap.SeqSet)
	for _, email := range MatchingEmails {
		if email.UID == 0 {
			return fmt.Errorf("cannot move message with zero UID")
		}
		seqset.AddNum(email.UID)
	}
	mover := batchMover{
		conn:        c,
		connectedAt: connectedAt,
		uidValidity: mailbox.UidValidity,
		now:         time.Now,
		pause:       time.Sleep,
		reconnect: func() (batchClient, time.Time, error) {
			select {
			case <-c.LoggedOut():
				return nil, time.Time{}, fmt.Errorf("connection closed unexpectedly before planned refresh")
			default:
			}
			if err := CloseConnection(); err != nil {
				return nil, time.Time{}, err
			}
			if err := ConnectLogin(); err != nil {
				return nil, time.Time{}, err
			}
			return c, connectedAt, nil
		},
	}
	return mover.move(SplitSequenceSet(seqset, 10))
}

// Helper function to split a sequence set into smaller chunks
// SplitSequenceSet splits an IMAP SeqSet into a slice of SeqSets, each containing
// at most chunkSize UIDs, while preserving ranges by splitting them into sub-ranges.
//
// IMPORTANT: This function must handle seq.Start..seq.Stop ranges correctly.
// The IMAP library may compress a UID set into ranges (e.g., 506732:506734).
func SplitSequenceSet(in *imap.SeqSet, chunkSize uint32) []*imap.SeqSet {
	if in == nil {
		return nil
	}
	if chunkSize == 0 {
		// Defensive: treat as "no chunking"
		// (alternatively return nil or panic, but this is safest operationally)
		return []*imap.SeqSet{in}
	}

	var out []*imap.SeqSet
	var cur imap.SeqSet
	var curCount uint32

	flush := func() {
		if curCount == 0 {
			return
		}
		// Copy cur into a new SeqSet so we don't keep mutating the same backing store.
		tmp := &imap.SeqSet{}
		for _, s := range cur.Set {
			tmp.AddRange(s.Start, s.Stop)
		}
		out = append(out, tmp)
		cur = imap.SeqSet{}
		curCount = 0
	}

	for _, seq := range in.Set {
		start := seq.Start
		stop := seq.Stop
		if stop == 0 {
			// Some implementations may store a single value with Stop==0;
			// normalize to a single-value range.
			stop = start
		}
		if stop < start {
			// Normalize if reversed (shouldn't happen, but safe)
			start, stop = stop, start
		}

		// Walk the range and emit sub-ranges that fit in chunks.
		for start <= stop {
			remaining := chunkSize - curCount
			if remaining == 0 {
				flush()
				remaining = chunkSize
			}

			end := start + uint32(remaining) - 1
			if end > stop {
				end = stop
			}

			cur.AddRange(start, end)
			curCount += (end - start + 1)

			if curCount == chunkSize {
				flush()
			}

			if end == stop {
				break
			}
			start = end + 1
		}
	}

	flush()
	return out
}

// Debug function to verify folder accessibility
func VerifyFolderAccess() {
	fmt.Printf("Verifying access to folder: %s\n", TrashFolder)
	mbox, err := c.Select(TrashFolder, false)
	if err != nil {
		log.Fatalf("failed to access folder %s: %v", TrashFolder, err)
	}
	fmt.Printf("Access to folder %s verified. Total messages: %d\n", TrashFolder, mbox.Messages)
}

// Explicitly close the IMAP connection
func CloseConnection() error {
	if c == nil {
		return nil
	}
	fmt.Println("*** CloseConnection ***")
	conn := c
	c = nil
	if conn.State() == imap.LogoutState {
		return nil
	}
	if err := conn.Logout(); err != nil {
		conn.Terminate()
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// BuildFromEmailAddress returns "local@domain" in lowercase, plus the lowercase domain.
// ok=false if the From field is missing or malformed.
func BuildFromEmailAddress(msg *imap.Message) (emailAddress string, domain string, ok bool) {
	if msg == nil || msg.Envelope == nil || len(msg.Envelope.From) == 0 {
		return "", "", false
	}
	from := msg.Envelope.From[0]
	if from == nil {
		return "", "", false
	}
	local := strings.ToLower(strings.TrimSpace(from.MailboxName))
	host := strings.ToLower(strings.TrimSpace(from.HostName))
	if local == "" || host == "" {
		return "", "", false
	}
	emailAddress = local + "@" + host
	return emailAddress, host, true
}

// IsWhitelisted returns true if the sender is whitelisted by:
//  1. exact full email match (entry contains '@')
//  2. exact domain match (e.g. "gmail.com")
//  3. wildcard domain match "*wellsfargo.com" which matches:
//     wellsfargo.com, notify.wellsfargo.com, mail-wellsfargo.com
//     but NOT wellsfargo.somejunk.com
func IsWhitelisted(emailAddress, fromDomain string) bool {
	for _, w := range Whitelist {
		w = strings.TrimSpace(strings.ToLower(w))
		if w == "" {
			continue
		}

		// 1) Full email match
		if strings.Contains(w, "@") {
			if emailAddress == w {
				return true
			}
			continue
		}

		// 2) Wildcard domain match: "*wellsfargo.com"
		if strings.HasPrefix(w, "*") {
			base := strings.TrimPrefix(w, "*")
			if base != "" && strings.HasSuffix(fromDomain, base) {
				return true
			}
			continue
		}

		// 3) Exact domain match: "gmail.com"
		if fromDomain == w {
			return true
		}
	}
	return false
}
