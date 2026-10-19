# SpamBeGone

SpamBeGone is a Go-based email filtering application that keeps messages from whitelisted senders in your inbox and moves messages from other valid senders to Trash.

## Table of Contents
- [Features](#features)
- [Usage](#usage)
- [Configuration](#configuration)
- [License](#license)

## Features
- Connects to your email server using IMAP.
- Supports whitelist entries for email addresses, exact domains, and wildcard domain suffixes.
- Leaves messages with missing or malformed sender fields in the inbox.
- Moves messages from senders outside the whitelist to the trash folder.
- Logs matching messages, batch progress, totals, and errors to console output.

## Usage
1. **Build the application**:
   ```sh
   go build
   ```
2. **Run the application**:
   ```sh
   ./SpamBeGone
   ```

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

Run the program with the project directory as its working directory so it can find `Config.json` and `Whitelist.txt`. It does not require a blacklist or write a metrics file.

## License
This project is licensed under [The Unlicense](https://unlicense.org/).
