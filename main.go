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
	defer CloseConnection()
	fmt.Println("SpamBeGone v0.3")
	LoadWhitelist()
	LoadConfig()
	ConnectLogin()
	VerifyFolderAccess()
	ListMailboxes()
	SelectMailbox()
	FetchAndStoreEmails()
	ListMatchingEmails()
	MoveToTrash()
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
func ConnectLogin() {
	fmt.Println("*** ConnectLogin ***")
	var err error
	c, err = client.DialTLS(server, nil)
	if err != nil {
		fmt.Printf("failed to connect to server: %v\n", err)
		os.Exit(1)
	}
	if err := c.Login(email, password); err != nil {
		c.Logout()
		fmt.Printf("failed to login: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Connected and logged in successfully")
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
func MoveToTrash() {
	if !DoMoveToTrash {
		fmt.Println("DoMoveToTrash is disabled. Skipping MoveToTrash.")
		return
	}
	fmt.Println("*** MoveToTrash ***")
	if len(MatchingEmails) == 0 {
		fmt.Println("No emails to move to trash.")
		return
	}
	// Reselect the mailbox to refresh its state
	mbox, err := c.Select(SelectFolder, false)
	if err != nil {
		log.Fatalf("failed to reselect mailbox %s: %v", SelectFolder, err)
	}
	log.Printf("Mailbox %s reselected. Total messages: %d", SelectFolder, mbox.Messages)
	seqset := new(imap.SeqSet)
	for _, email := range MatchingEmails {
		log.Printf("Adding UID to sequence set: %d", email.UID)
		seqset.AddNum(email.UID)
	}
	if seqset.Empty() {
		log.Println("Sequence set is empty. No valid UIDs to process.")
		return
	}
	// Debugging: Log the sequence set before processing
	log.Printf("Sequence set for processing: %s", seqset.String())
	// Split the sequence set into smaller chunks to avoid rate limits
	chunks := SplitSequenceSet(seqset, 10) // Adjust chunk size as needed
	for i, chunk := range chunks {
		log.Printf("Processing chunk %d: %s", i+1, chunk.String())
		// Copy emails to the Trash folder
		if err := c.UidCopy(chunk, TrashFolder); err != nil {
			log.Printf("FATAL: chunk %d copy to %s failed: %s (%v)", i+1, TrashFolder, chunk.String(), err)
			// Show status of Trash/Bulk
			VerifyFolderCounts(TrashFolder, "Trash/Bulk Mail")
			// Clean logout before exiting
			CloseConnection()
			os.Exit(1)
		}
		log.Printf("Processed chunk %d: %s", i+1, chunk.String())
		// Introduce a small delay to avoid rate limits
		time.Sleep(2 * time.Second)
	}
	VerifyFolderCounts(TrashFolder, "Trash/Bulk Mail")
	// Reselect INBOX so session state is clean
	_, err = c.Select(SelectFolder, false)
	if err != nil {
		log.Printf("failed to reselect %s after folder verification: %v", SelectFolder, err)
	}
	// Mark original emails as deleted
	storeFlags := []interface{}{imap.DeletedFlag}
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	if err := c.UidStore(seqset, item, storeFlags, nil); err != nil {
		log.Printf("failed to mark emails as deleted: %v", err)
		return
	}
	// Expunge deleted emails
	if err := c.Expunge(nil); err != nil {
		log.Printf("failed to expunge emails: %v", err)
		return
	}
	// Confirm INBOX count after expunge
	mbox, err = c.Select(SelectFolder, false)
	if err != nil {
		log.Printf("failed to reselect %s after expunge: %v", SelectFolder, err)
	} else {
		log.Printf("Post-expunge: %s now contains %d messages.", SelectFolder, mbox.Messages)
	}
	log.Printf("%d emails moved to trash successfully.", len(MatchingEmails))
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
func CloseConnection() {
	fmt.Println("*** CloseConnection ***")
	if c == nil {
		return
	}
	if err := c.Logout(); err != nil {
		fmt.Printf("failed to logout: %v\n", err)
		os.Exit(1)
	}
}

// VerifyFolderCounts selects the given folders and logs their message counts.
// This is read-only and does not modify any messages.
func VerifyFolderCounts(folders ...string) {
	for _, folder := range folders {
		mbox, err := c.Select(folder, false)
		if err != nil {
			log.Printf("VerifyFolderCounts: failed to select %s: %v", folder, err)
			continue
		}
		log.Printf("VerifyFolderCounts: %s contains %d messages.", folder, mbox.Messages)
	}
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
