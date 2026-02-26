# SpamBeGone -- Session Summary

## 1. Objective

-   Configure a Windows Scheduled Task to run `SpamBeGone.exe` every
    hour.

-   Capture all console output (stdout + stderr).

-   Create one log file per hour using the format:

        Log.yyyy.MM.dd.HH.txt

-   Ensure the task exits cleanly and reports a successful status.

------------------------------------------------------------------------

## 2. Context

### Original Setup

-   Scheduled task triggered hourly.

-   Initially ran:

        wscript.exe RunSpamBeGone.vbs

-   VBS launched the EXE asynchronously (`wait = False`).

-   Output redirection attempted at the `wscript` level.

-   Result: log files created but empty.

### Key Changes Made

#### Removed VBS Layer

-   Instead of running `wscript.exe`, the task now runs `cmd.exe`
    directly.
-   This allows proper output redirection from the EXE itself.

#### Implemented Timestamped Logging

Used delayed variable expansion to build a properly formatted timestamp:

    yyyy.MM.dd.HH

Final working command:

``` cmd
cmd /v:on /c "set ts=%date:~10,4%.%date:~4,2%.%date:~7,2%.%time:~0,2% & set ts=!ts: =0! & ""C:\Projects\SpamBeGone\SpamBeGone.exe"" > ""C:\Projects\SpamBeGone\Logs\Log.!ts!.txt"" 2>&1"
```

#### Important Configuration Notes

-   `/v:on` enables delayed expansion (`!ts!`).

-   `set ts=!ts: =0!` corrects leading space for single-digit hours.

-   Double internal quotes required inside `/c " ... "` block.

-   Log folder must already exist.

-   Working directory set to:

        C:\Projects\SpamBeGone\

------------------------------------------------------------------------

## 3. Key Takeaways / Insights

### A. Why Logs Were Empty

-   `wscript` exited immediately due to asynchronous EXE launch.
-   Redirection captured nothing because EXE output occurred after
    wrapper exited.

### B. Task Scheduler Quoting Is Fragile

-   Nested quoting must use doubled quotes inside `/c " ... "`.
-   Improper quoting can cause:
    -   Task stuck in "Running" state
    -   Exit code `-1073741510` (0xC000013A)
    -   Wrapper `cmd.exe` not terminating properly

### C. Exit Codes Matter

-   `0x0` → Success

-   `0xC000013A` → Forced termination (often quoting or manual end)

-   Use:

    ``` cmd
    schtasks /query /tn "\SpamBeGone" /v /fo list | findstr /i "Status Last Result"
    ```

### D. Delayed Expansion Behavior

-   Interactive cmd behaves differently than batch context.
-   `/v:on` ensures reliable expansion in Scheduled Task execution.

### E. Validation Strategy

Verified functionality by: 1. Running command manually via cmd. 2.
Confirming: - Log file name correct - Log contents correct - Task status
returns to Ready - Last Result = 0

------------------------------------------------------------------------

## 4. Open Issues / Next Steps

### Optional Enhancements

-   Add log rotation (delete logs older than X days).
-   Add minutes/seconds if multiple runs per hour are possible.
-   Add alerting if Last Result ≠ 0.
-   Append (`>>`) instead of overwrite (`>`) if desired.

### Current State

-   Scheduled task working as expected.
-   Produces correct hourly logs.
-   Clean exit behavior confirmed.
-   Operationally stable.

------------------------------------------------------------------------

**Status: Production-ready configuration achieved.**
