# SpamBeGone

SpamBeGone is a Go-based email filtering application that keeps messages from whitelisted senders in your inbox and moves messages from other valid senders to Trash.

## Table of Contents
- [Features](#features)
- [Usage](#usage)
- [Configuration](#configuration)
- [Batch Processing](#batch-processing)
- [Windows Task Scheduler](#windows-task-scheduler)
- [License](#license)

## Features
- Connects to your email server using IMAP.
- Supports whitelist entries for email addresses, exact domains, and wildcard domain suffixes.
- Leaves messages with missing or malformed sender fields in the inbox.
- Moves messages from senders outside the whitelist to the trash folder.
- Logs matching messages, batch progress, totals, and errors to console output.

## Usage

Open Windows Task Scheduler, right-click the `SpamBeGone` task, and select **Run**. The task launches the existing `SpamBeGone.exe` and captures its output in `Logs`.

After changing the source code, rebuild the executable from the project directory:

```powershell
go build -o SpamBeGone.exe .
```

To run directly from PowerShell in the project directory:

```powershell
.\SpamBeGone.exe
```

A direct run writes output to the console; Task Scheduler provides the log-file redirection.

## Configuration
1. **Create `Config.json`**:
   ```json
   {
     "server":   "<YourEmailServer.com>:<Port>",
     "email":    "YourEmailAddress",
     "password": "YourPassword"
   }
   ```
2. **Create `Whitelist.txt`**:
   - Add one entry per line. Matching ignores case and surrounding whitespace; blank lines are ignored.
   - Use a full email address for one sender, a domain for that exact domain, or a leading `*` for a domain suffix.
   - Example:
     ```
     friend@example.com
     example.org
     *example.net
     ```
   - `example.org` matches only that domain. `*example.net` matches the apex domain and any domain ending in `example.net`, including `mail.example.net` and `mail-example.net`. `*.example.net` matches only subdomains.
   - An empty whitelist keeps no valid senders; all messages with usable sender addresses are selected for Trash.

Run the program with the project directory as its working directory so it can find `Config.json` and `Whitelist.txt`.

## Batch Processing

Messages are processed in batches of up to 10 UIDs. Each batch is copied to Trash, marked deleted in INBOX, and removed using `UID EXPUNGE` before the next batch starts. The server must support UIDPLUS so other messages marked deleted are left alone.

Between batches, the program pauses for two seconds. Before starting another batch, it refreshes connections that are at least four minutes old, including time spent logging in and scanning the inbox. After reconnecting, it checks INBOX's UIDVALIDITY before continuing with the remaining UIDs.

Unexpected disconnects and command failures stop the run with a nonzero exit code; operations are not retried. Completed batches remain moved. If a batch fails during copying or deletion, that batch may have copies in Trash and originals remaining in INBOX; the error log identifies the batch and the failing stage.

## Windows Task Scheduler

The `SpamBeGone` task runs `SpamBeGone.exe` through `cmd.exe` with this project directory as its working directory. Console output and errors are redirected to timestamped files in `Logs`; that directory must exist.

To inspect the registered task's status and last result:

```cmd
schtasks /query /tn "\SpamBeGone" /v /fo list
```

`SpamBeGone.xml` is an export of the registered task's configuration, including its command, arguments, working directory, triggers, and settings. Use **Import Task…** in Task Scheduler to restore or recreate it; review the account and paths when importing on another computer. The export does not include the executable, credentials, or whitelist.

Task Scheduler stores the active task configuration independently of this XML file. Re-export the task after changing its settings to keep the backup current.

## License
This project is licensed under [The Unlicense](https://unlicense.org/).
