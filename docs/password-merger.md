# Password Merger

Open `/tools/password-merger/` from the resource library or application footer.
The tool operates without an account.
It reads Chrome, Vivaldi, Firefox, Safari, and Apple Passwords CSV exports in the browser.
The page contains export and import instructions with official help links.

## Local workflow

1. Export the website passwords from each source application.
2. Select each CSV under its source label.
3. Select **Merge files**.
4. Examine the file record counts and account totals.
5. For each password conflict, select one candidate.
6. Select **Download merged CSV**.
7. Use the destination import instructions on the page.
8. Examine the destination records before you remove unnecessary plaintext files.

The final file is `merged-passwords.csv`.
Its fields are `name,url,username,password,note`.
The tool does not read browser databases or change password managers.
It does not require the old PasswordMergeReceiver application.

## Compact layout

The initial view shows the file controls and the merge, download, and clear actions.
Detailed guides and CSV limits use expandable sections.
Password conflicts appear only when the imported files contain different passwords for one account.
A wide browser view shows candidates in two columns.
The Apple replacement confirmation stays inside its optional instructions.
Local app icons identify file sources, guides, record counts, and conflict candidates.
Text labels remain beside the icons.
Empty file controls show the source and **Choose files** button.
Selected filenames appear to the left of the button.
Long filenames use an ellipsis in the control. The file list shows each complete filename.
The buttons support keyboard selection and multiple files per source.

The icon source manifest is `frontend/manifests/password-merger-icons.json`.
Chrome, Firefox, Safari, and Apple Passwords icons came from the installed applications.
The Vivaldi icon came from its [official media page](https://vivaldi.com/press/).
The page credits Vivaldi Technologies and links the CC BY 4.0 license.
The PNG assets use a 128-pixel square size.

## Account rules

Each account uses one website origin and one exact username.
URL normalization preserves the scheme, subdomain, and nondefault port.
It normalizes host case, international host names, and default ports.
Usernames and passwords keep their exact case and whitespace.

Different URL paths on the same origin share an account when their usernames are equal.
Candidate labels show the source, file, CSV record number, and original normalized URL.

Equal nonempty passwords produce one candidate.
Different nonempty passwords require an explicit selection.
Empty passwords do not compete with nonempty candidates.
An account with only empty passwords produces one empty-password row.
Each account produces one final row.
The destination can reject accounts with empty passwords or usernames.

The result keeps all distinct notes, alternative titles, and alternative URLs in the note field.
Supplied setup URIs also stay in the note field for separate import.
The CSV does not automatically install verification codes.
The tool does not infer password age from export dates.
File and record order do not change the result.

## Privacy and failure rules

The tool uses local scripts and styles.
Its content security policy prohibits network connections.
It does not send file contents to application servers or analytics.
It does not store credentials in browser storage.
The tool holds selected files and results in browser memory until reset or page exit.

Malformed CSV, invalid UTF-8, duplicate headers, invalid URLs, and HTTP authentication accounts stop the complete batch.
An error creates no partial result.
A new file selection clears earlier results and conflict selections.
A reset also cancels the effect of pending file reads.

## Apple replacement

Apple CSV import does not replace existing saved passwords.
The page separates normal import from optional manual replacement.
Replacement instructions require acknowledgment of the Apple backup, final CSV, and excluded records.
The instructions name the Apple file picker, **Merge files**, and **Select each password to keep** controls.
**Download merged CSV** becomes available after each conflicting account has a selected password.
The instructions select only website password accounts present in the backup and final CSV.
Apple confirms each deletion before the final CSV import.
Passkeys, shared items, and other records absent from the CSV need separate handling.
The tool performs no deletions.

## Validation

Run `make test-password-merger-browser` for the focused browser contract.
Run `make ci` for repository validation.
The browser contract uses synthetic credentials and the real application server.
It examines downloaded CSV bytes and observes network, console, WebSocket, and browser storage activity.
It verifies all source guides, conflicts, empty-password accounts, exact values, reset behavior, errors, and responsive layout.
The production artifact test verifies that the static tool files are present.
