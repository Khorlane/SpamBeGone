// Test in Termianl Window using:
//   go run .
//   go run SpamBeGone.go
//   del SpamBeGone.exe;go build;./SpamBeGone.exe
// Bookmarks (via VSCode extension "Bookmarks")
//  Ctrl Alt K - Set/Clear
//  Ctrl Alt J - Jump to next bookmark

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
  "unicode"

  "github.com/emersion/go-imap"
  "github.com/emersion/go-imap/client"
)

var (
  // Configuration fields to be set from JSON config file
  server           = ""
  email            = ""
  password         = ""
  // Global variables
  c                *client.Client
  DebugEmail       = "debug@example.com"
  DebugUid         = uint32(0)
  mailbox          *imap.MailboxStatus
  MatchingEmails   []Email
  TrashMetrics     []TrashMetric
  ProgramStartTime = time.Now().Format("2006-01-02 15:04:05")
  // Constants
  SelectFolder     = "INBOX"
  TrashFolder      = "Trash"
  // Codes
  TrashCode        byte
  // Switches
  DoMoveToTrash    = true
  ShowMailboxes    = true

  // Global blacklist (phrases) and whitelist (email addresses)
  Blacklist []string
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
  TrashCode    byte
}

// Metrics struct
type TrashMetric struct {
  FilterPhrase string
  TrashCode    byte
  Count        int
}

func main() {
  defer CloseConnection()
  fmt.Println("SpamBeGone v0.3")
  LoadWhitelist()
  LoadBlacklist()
  InitTrashMetrics()
  LoadConfig()
  ConnectLogin()
  VerifyFolderAccess()
  ListMailboxes()
  SelectMailbox()
  CheckConvertStyledToASCII()
  FetchAndStoreEmails()
  ListMatchingEmails()
  WriteTrashMetrics()
  MoveToTrash()
  // fmt.Println("Press 'Enter' to continue...")
  // bufio.NewReader(os.Stdin).ReadBytes('\n')
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

// Read the blacklist from Blacklist.txt
func LoadBlacklist() {
  file, err := os.Open("Blacklist.txt")
  if err != nil {
    log.Fatalf("failed to load blacklist: %v", err)
  }
  defer file.Close()
  scanner := bufio.NewScanner(file)
  for scanner.Scan() {
    line := scanner.Text()
    line = strings.TrimSpace(line)
    line = strings.ToLower(line)
    Blacklist = append(Blacklist, line)
    // If the line contains two or more space-separated words, append another line without spaces
    if strings.Contains(line, " ") {
      Blacklist = append(Blacklist, strings.ReplaceAll(line, " ", ""))
    }
  }
  if err := scanner.Err(); err != nil {
    log.Fatalf("error reading blacklist: %v", err)
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
  server   = Config.Server
  email    = Config.Email
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

// Fetch and store emails with optional filtering
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
    DebugUid = 0
    gotMatch := false // Initialize GotMatch to false for each message
    for _, filterPhrase := range Blacklist {
      if MatchFilter(msg, filterPhrase) {
        gotMatch = true // Set GotMatch to true if a match is found
        break          // Exit the loop early since we found a match
      }
    }
    if !gotMatch {
      continue // Skip to the next message if no match was found
    }
    // Got a match, so we're going to send it to trash
    from := "Unknown"
    personalName := msg.Envelope.From[0].PersonalName
    emailAddress := fmt.Sprintf("%s@%s", msg.Envelope.From[0].MailboxName, msg.Envelope.From[0].HostName)
    if personalName != "" {
      from = fmt.Sprintf("%s <%s>", personalName, emailAddress)
    } else {
      from = emailAddress
    }
    // Add the email to the global in-memory data structure
    MatchingEmails = append(MatchingEmails, Email{
      UID:         msg.Uid,
      From:        from,
      Subject:     msg.Envelope.Subject,
      InternalDate: msg.InternalDate.Format("2006-01-02 15:04:05"),
      TrashCode:   TrashCode,
    })
  }
  // Check for fetch errors
  if err := <-done; err != nil {
    log.Fatal(err)
  }
}

// Helper function to check if an email matches the filter phrases
func MatchFilter(msg *imap.Message, filterPhrase string) bool {
  // Check if the email address is in the whitelist
  if len(msg.Envelope.From) > 0 {
    emailAddress := fmt.Sprintf("%s@%s", strings.ToLower(msg.Envelope.From[0].MailboxName), strings.ToLower(msg.Envelope.From[0].HostName))
    for _, whitelisted := range Whitelist {
      if emailAddress == whitelisted {
        return false // Skip filtering if the email is in the whitelist
      }
    }
  }
  // If the filter phrase is empty, match all emails
  if filterPhrase == "" {
    return true
  }
  // Ensure the message envelope is not nil
  if msg.Envelope == nil {
    return false
  }
  // Ensure the 'From' field is not empty
  if len(msg.Envelope.From) == 0 {
    return false
  }
  // Ensure the subject is not empty
  if msg.Envelope.Subject == "" {
    return false
  }
  // Check for unacceptable characters in PersonalName
  personalName := msg.Envelope.From[0].PersonalName
  if ContainsUnacceptable(personalName) {
    TrashCode = 1
    IncrementTrashMetric("Unacceptable", 1)
    return true
  }
  // Check for unacceptable characters in Subject
  if ContainsUnacceptable(msg.Envelope.Subject) {
    TrashCode = 2
    IncrementTrashMetric("Unacceptable", 2)
    return true
  }
  // Check if the PersonalName contains the filter phrase (case-insensitive)
  personalName = strings.ToLower(ConvertStyledToASCII(personalName))
  if strings.Contains(personalName, filterPhrase) {
    TrashCode = 3
    IncrementTrashMetric(filterPhrase, 3)
    return true
  }
  // Check if the subject contains the filter phrase (case-insensitive)
  subject := strings.ToLower(ConvertStyledToASCII(msg.Envelope.Subject))
  DebugSubject(personalName, subject, msg, "put trouble email address here")
  if strings.Contains(subject, filterPhrase) {
    TrashCode = 4
    IncrementTrashMetric(filterPhrase, 4)
    return true
  }
  // Check if the From Email Address contains the filter phrase (case-insensitive)
  emailAddress := fmt.Sprintf("%s@%s", strings.ToLower(msg.Envelope.From[0].MailboxName), strings.ToLower(msg.Envelope.From[0].HostName))
  if strings.Contains(emailAddress, filterPhrase) {
    TrashCode = 5
    IncrementTrashMetric(filterPhrase, 5)
    return true
  }
  // Fall through to return false if no match is found
  return false
}

// Helper function to check for unprintable characters, allowing emojis
func ContainsUnacceptable(input string) bool {
  for _, r := range input {
    if !unicode.IsPrint(r) {        // Character is not printable
      return true
    }
    if IsEmoji(r) {                 // Character is an emoji
      return true
    } 
    if r >= 0x0400 && r <= 0x04FF { // Character is in the Cyrillic range (U+0400–U+04FF)
      return true
    }
  }
  return false
}

// Helper function to check if a rune is an emoji
func IsEmoji(r rune) bool {
  // Emoji ranges based on Unicode standard
  return (r >= 0x1F600 && r <= 0x1F64F) || // Emoticons
         (r >= 0x1F300 && r <= 0x1F5FF) || // Miscellaneous Symbols and Pictographs
         (r >= 0x1F680 && r <= 0x1F6FF) || // Transport and Map Symbols
         (r >= 0x2600 && r <= 0x26FF)   || // Miscellaneous Symbols
         (r >= 0x2700 && r <= 0x27BF)   || // Dingbats
         (r >= 0x1F900 && r <= 0x1F9FF) || // Supplemental Symbols and Pictographs
         (r >= 0x1FA70 && r <= 0x1FAFF) || // Symbols and Pictographs Extended-A
         (r >= 0x1F1E6 && r <= 0x1F1FF)    // Flags
}

// Replace styled Unicode characters (e.g., Mathematical Monospace, Bold) with their ASCII equivalents
func ConvertStyledToASCII(input string) string {
  var builder strings.Builder
  for _, r := range input {
    switch {
      case isMathematicalAlphanumeric(r):
        builder.WriteRune(NormalizeMathAlphanumeric(r))
      case unicode.Is(unicode.Cyrillic, r):
        builder.WriteRune(NormalizeCyrillic(r))
      case r == 0x2019 || r == 0x2018:   // Map typographic apostrophes (U+2019, U+2018) to ASCII apostrophe ('')
        builder.WriteRune('\'')
      case r == 0x2014 || r == 0x2013:   // Map em dash (U+2014) to ASCII hyphen (-)
        builder.WriteRune('-')
      case r == 0x00A9:                  // Map copyright symbol (U+00A9) to ASCII (c)
        builder.WriteString("(c)")
      case r == '®':                     // Map registered trademark symbol (U+00AE) to ASCII (r)
        builder.WriteString("(r)")
      default:                           // Leave other characters unchanged
        builder.WriteRune(r)
    }
  }
  return builder.String()
}

// Sort MatchingEmails by TrashCode, then by InternalDate ascending
func SortEmails() {
  sort.SliceStable(MatchingEmails, func(i, j int) bool {
    if MatchingEmails[i].TrashCode != MatchingEmails[j].TrashCode {
      return MatchingEmails[i].TrashCode < MatchingEmails[j].TrashCode
    }
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
    fmt.Printf("TrashCode: %d, UID: %d, From: %s, Subject: %s, InternalDate: %s\n", email.TrashCode, email.UID, email.From, email.Subject, email.InternalDate)
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
      log.Printf("failed to copy emails to trash for chunk %d: %v", i+1, err)
      continue
    }
    // Introduce a small delay to avoid rate limits
    time.Sleep(2 * time.Second)
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
  log.Printf("%d emails moved to trash successfully.", len(MatchingEmails))
}

// Helper function to split a sequence set into smaller chunks
func SplitSequenceSet(seqset *imap.SeqSet, chunkSize int) []*imap.SeqSet {
  var chunks []*imap.SeqSet
  currentChunk := new(imap.SeqSet)
  count := 0
  for _, seq := range seqset.Set {
    currentChunk.AddNum(seq.Start)
    count++
    if count >= chunkSize {
      chunks = append(chunks, currentChunk)
      currentChunk = new(imap.SeqSet)
      count = 0
    }
  }
  if !currentChunk.Empty() {
    chunks = append(chunks, currentChunk)
  }
  return chunks
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
  if err := c.Logout(); err != nil {
    fmt.Printf("failed to logout: %v\n", err)
    os.Exit(1)
  }
}

// Loop through the messages in the inbox, converts the PersonalName and Subject to ASCII, and validates if they are ASCII characters.
func CheckConvertStyledToASCII() {
  fmt.Println("*** CheckConvertStyledToASCII ***")
  seqset := new(imap.SeqSet)
  seqset.AddRange(1, mailbox.Messages)
  messages := make(chan *imap.Message, mailbox.Messages)
  done := make(chan error, 1)
  go func() {
    done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchUid, imap.FetchInternalDate, imap.FetchEnvelope}, messages)
  }()
  for msg := range messages {
    if msg.Envelope == nil || len(msg.Envelope.From) == 0 {
      continue // Skip messages with no envelope or sender
    }
    personalName := ConvertStyledToASCII(msg.Envelope.From[0].PersonalName)
    subject := ConvertStyledToASCII(msg.Envelope.Subject)
    if !isASCII(personalName) || !isASCII(subject) {
      from := "Unknown"
      emailAddress := fmt.Sprintf("%s@%s", msg.Envelope.From[0].MailboxName, msg.Envelope.From[0].HostName)
      if msg.Envelope.From[0].PersonalName != "" {
        from = fmt.Sprintf("%s <%s>", msg.Envelope.From[0].PersonalName, emailAddress)
      } else {
        from = emailAddress
      }
      fmt.Printf("UID: %d, From: %s, Subject: %s, InternalDate: %s\n",
        msg.Uid, from, msg.Envelope.Subject, msg.InternalDate.Format("2006-01-02 15:04:05"))
    }
  }
  if err := <-done; err != nil {
    log.Fatal(err)
  }
}

// Helper function to check if a string contains only ASCII characters
func isASCII(input string) bool {
  for _, r := range input {
    if r > unicode.MaxASCII {
      return false
    }
  }
  return true
}

// Log the subject before and after normalization for a specific email address
func DebugSubject(PersonalName string, subject string, msg *imap.Message, targetEmail string) {
  if targetEmail == DebugEmail {
    return
  }
  if DebugUid == msg.Uid {
    return
  }
  DebugUid = msg.Uid
  if len(msg.Envelope.From) > 0 {
    emailAddress := fmt.Sprintf("%s@%s", msg.Envelope.From[0].MailboxName, msg.Envelope.From[0].HostName)
    if emailAddress == targetEmail {
      log.Printf("PersonalName before normalization: %s", msg.Envelope.From[0].PersonalName)
      log.Printf("PersonalName after normalization: %s", PersonalName)
      log.Printf("Subject before normalization: %s", msg.Envelope.Subject)
      log.Printf("Subject after normalization: %s", subject)
    }
  }
}

// Initialize TrashMetrics with entries from the Blacklist
func InitTrashMetrics() {
  fmt.Println("*** InitTrashMetrics ***")
  TrashMetrics = append(TrashMetrics, TrashMetric{
    FilterPhrase: "Unprintable",
    TrashCode:    byte(1),
    Count:        0,
  })
  TrashMetrics = append(TrashMetrics, TrashMetric{
    FilterPhrase: "Unprintable",
    TrashCode:    byte(2),
    Count:        0,
  })
  for _, phrase := range Blacklist {
    for trashCode := 3; trashCode <= 5; trashCode++ {
      TrashMetrics = append(TrashMetrics, TrashMetric{
        FilterPhrase: phrase,
        TrashCode:    byte(trashCode),
        Count:        0,
      })
    }
  }
}

// Increment TrashMetrics
func IncrementTrashMetric(filterPhrase string, trashCode byte) {
  for i := range TrashMetrics {
    if TrashMetrics[i].FilterPhrase == filterPhrase {
      if TrashMetrics[i].TrashCode == trashCode {
        TrashMetrics[i].Count++
        break
      }
    }
  }
}

// Write non-zero TrashMetrics
func WriteTrashMetrics() {
  fmt.Println("*** WriteTrashMetrics ***")
  file, err := os.OpenFile("TrashMetrics.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
  if err != nil {
    log.Fatalf("failed to open TrashMetrics.txt: %v", err)
  }
  defer file.Close()
  writer := bufio.NewWriter(file)
  for _, metric := range TrashMetrics {
    if metric.Count > 0 {
      _, err := writer.WriteString(fmt.Sprintf("%s, %s, %d, %d\n", ProgramStartTime, metric.FilterPhrase, metric.TrashCode, metric.Count))
      if err != nil {
        log.Fatalf("failed to write to TrashMetrics.txt: %v", err)
      }
    }
  }
  writer.Flush()
}

//
func isMathematicalAlphanumeric(r rune) bool {
  return (r >= 0x1D400 && r <= 0x1D7FF) // Mathematical Alphanumeric Symbols range
}

// Helper function to normalize Mathematical Unicode characters
func NormalizeMathAlphanumeric(r rune) rune {
  switch {
    case r >= 0x1D5D4 && r <= 0x1D5ED:   // Mathematical Sans-Serif Bold uppercase A-Z
      return r - 0x1D5D4 + 'A'
    case r >= 0x1D5EE && r <= 0x1D607:   // Mathematical Sans-Serif Bold lowercase a-z
      return r - 0x1D5EE + 'a'
    case r >= 0x1D400 && r <= 0x1D419:   // Mathematical Bold uppercase A-Z
      return r - 0x1D400 + 'A'
    case r >= 0x1D41A && r <= 0x1D433:   // Mathematical Bold lowercase a-z
      return r - 0x1D41A + 'a'
    case r >= 0x1D670 && r <= 0x1D689:   // Monospace uppercase A-Z
      return r - 0x1D670 + 'A'
    case r >= 0x1D68A && r <= 0x1D6A3:   // Monospace lowercase a-z
      return r - 0x1D68A + 'a'
  }
  return r
}

// Helper function to normalize Cyrillic Unicode characters
func NormalizeCyrillic(r rune) rune {
  switch {
    case r >= 0x0410 && r <= 0x042F:     // Cyrillic uppercase A-Z
      return r - 0x0410 + 'A'
    case r >= 0x0430 && r <= 0x044F:     // Cyrillic lowercase a-z
      return r - 0x0430 + 'a'
    case r == 0x0401:                    // Cyrillic uppercase Ё
      return 'E'
    case r == 0x0451:                    // Cyrillic lowercase ё
      return 'e'
    default:                             // Leave other characters unchanged
      return r
  }
}