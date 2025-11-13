# SpamBeGone

SpamBeGone is a Go-based email filtering application designed to help you manage your inbox by filtering out unwanted emails based on customizable blacklists and whitelists.

## Table of Contents
- [Features](#features)
- [Usage](#usage)
- [Configuration](#configuration)
- [License](#license)

## Features
- Connects to your email server using IMAP.
- Filters emails based on a blacklist of phrases.
- Supports a whitelist to exclude specific email addresses from filtering.
- Moves filtered emails to the trash folder.
- Logs filtering metrics to `TrashMetrics.txt`.

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
2. **Create `Blacklist.txt`**:
   - Add one or more phrases (e.g., words or sentences) that should be filtered from the Subject or Personal Name.
   - Example:
     ```
     Affordable Coverage
     Black Friday
     Landmark
     ```
3. **Create `Whitelist.txt`**:
   - Add email addresses that should be excluded from filtering.
   - Example:
     ```
     somejunk@spamforyou.com
     ```

## License
This project is licensed under [The Unlicense](https://unlicense.org/).